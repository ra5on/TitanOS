package storage

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// LongSMART runs the long SMART self-test of a disk; see SelfTest.
func (m *Manager) LongSMART(ctx context.Context, serial string) error {
	return m.SelfTest(ctx, serial, "long")
}

// ShortSMART runs the short SMART self-test (about two minutes).
func (m *Manager) ShortSMART(ctx context.Context, serial string) error {
	return m.SelfTest(ctx, serial, "short")
}

// SelfTest runs a SMART self-test ("short" or "long"). It addresses the
// unique serial number, not the Linux device name which can change after a
// reboot. A previous test result is never accepted as this run.
func (m *Manager) SelfTest(ctx context.Context, serial, kind string) (result error) {
	if kind != "short" && kind != "long" {
		return ErrNotPossible
	}
	m.actions.Lock()
	if m.ReadOnly || m.GrowthPending() {
		m.actions.Unlock()
		return ErrNotPossible
	}
	status, err := m.Status(ctx)
	if err != nil {
		m.actions.Unlock()
		return err
	}
	var disk Disk
	found := 0
	for _, d := range status.Disks {
		if d.Serial == serial && serial != "" {
			disk = d
			found++
		}
	}
	if found != 1 || disk.Testing || disk.Role == RoleStick {
		m.actions.Unlock()
		return ErrDiskUnavailable
	}
	if m.Claimed != nil && m.Claimed()[disk.Name] != "" {
		m.actions.Unlock()
		return ErrDiskUnavailable
	}
	for _, v := range status.Volumes {
		if v.Name == disk.Volume && v.State != VolumeOK {
			m.actions.Unlock()
			return ErrBusy
		}
	}
	testCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	test := &DiskTest{Disk: disk.Name, Serial: disk.Serial, Model: disk.Model, SizeBytes: disk.SizeBytes, Mode: "smart-" + kind, Phase: PhaseSmart, Started: time.Now(), RemainingSeconds: -1, cancel: cancel, Notes: []TestNote{}}
	m.testMu.Lock()
	if m.tests == nil {
		m.tests = map[string]*DiskTest{}
	}
	m.tests[disk.Name] = test
	m.testMu.Unlock()
	m.actions.Unlock()
	defer func() {
		m.updateTest(test, func(t *DiskTest) {
			now := time.Now()
			t.Finished = &now
			t.Phase = PhaseDone
			t.Result = TestPassed
			if result != nil {
				t.Result = TestFailed
				t.Notes = append(t.Notes, TestNote{Kind: "smart_failed", Detail: result.Error()})
			}
			if errors.Is(result, context.Canceled) {
				t.Result = TestCancelled
			}
		})
		if m.OnTestDone != nil {
			m.OnTestDone(m.testSnapshot(test))
		}
	}()
	command := func(args ...string) (string, error) {
		call, stop := context.WithTimeout(testCtx, 30*time.Second)
		defer stop()
		return m.runner.Run(call, "", "smartctl", args...)
	}
	before, _ := command("--json=c", "--capabilities", "--log=selftest", disk.Path)
	if state, ok := parseSelfTest(before); ok && state.running {
		return errors.New("a SMART self-test is already running")
	}
	if _, err := command("--json=c", "--test="+kind, disk.Path); err != nil {
		return fmt.Errorf("%s SMART test could not start: %w", kind, err)
	}
	interval := m.smartPoll
	if interval == 0 {
		interval = 30 * time.Second
		if kind == "short" {
			// A short test can be over within a minute: look often enough
			// to see it running.
			interval = 5 * time.Second
		}
	}
	seen := false
	started := time.Now()
	for {
		select {
		case <-testCtx.Done():
			// Abort only the same physical disk; a replaced /dev name is never reused.
			fresh, e := m.Status(context.WithoutCancel(testCtx))
			if e == nil {
				for _, d := range fresh.Disks {
					if d.Serial == serial && d.Path == disk.Path {
						cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
						m.runner.Run(cleanup, "", "smartctl", "--abort", disk.Path)
						stop()
					}
				}
			}
			return testCtx.Err()
		case <-time.After(interval):
		}
		fresh, e := m.Status(testCtx)
		if e != nil {
			return e
		}
		same := false
		for _, d := range fresh.Disks {
			if d.Serial == serial && d.Path == disk.Path {
				same = true
			}
		}
		if !same {
			return ErrDiskUnavailable
		}
		output, e := command("--json=c", "--capabilities", "--log=selftest", disk.Path)
		state, ok := parseSelfTest(output)
		if e != nil && !ok {
			return e
		}
		if !ok {
			return errors.New("SMART self-test status unavailable")
		}
		if state.running {
			seen = true
			m.updateTest(test, func(t *DiskTest) { t.Percent = float64(100 - state.remaining) })
		} else if seen {
			if !state.passed {
				return fmt.Errorf("SMART test failed: %s", state.detail)
			}
			return nil
		}
		if !seen && time.Since(started) > 5*time.Minute {
			return errors.New("SMART test start was not observed; previous results were not accepted")
		}
		if time.Since(started) > 96*time.Hour {
			return errors.New("SMART test did not finish within 96 hours")
		}
	}
}
func (m *Manager) testSnapshot(t *DiskTest) DiskTest {
	m.testMu.Lock()
	defer m.testMu.Unlock()
	return *t
}
func (m *Manager) MaintenanceCheck(ctx context.Context, name string) error {
	if err := m.Check(ctx, name, true); err != nil {
		return err
	}
	started := time.Now()
	observed := false
	for {
		select {
		case <-ctx.Done():
			cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
			defer stop()
			_ = m.Check(cleanup, name, false)
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
		status, err := m.Status(ctx)
		if err != nil {
			return err
		}
		found := false
		for _, v := range status.Volumes {
			if v.Name != name {
				continue
			}
			found = true
			if v.State == VolumeChecking {
				observed = true
				continue
			}
			if v.State != VolumeOK {
				return ErrBusy
			}
			if !observed && time.Since(started) < 30*time.Second {
				continue
			}
			if v.Mismatches > 0 {
				return fmt.Errorf("check found %d mismatching blocks", v.Mismatches)
			}
			return nil
		}
		if !found {
			return ErrNotFound
		}
	}
}
