package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"sort"
	"time"

	"golang.org/x/sys/unix"
)

// Testing a disk before it goes into a volume: read or overwrite all of
// it, then let the disk run its own long self-test. A disk that fails in
// its first days does so here and not as a member of a volume.

// Modes of a disk test.
const (
	TestRead  = "read"  // read everything; what is on the disk stays
	TestWrite = "write" // overwrite everything, read it back and compare
)

// Phases of a disk test.
const (
	PhaseWrite  = "write"
	PhaseVerify = "verify"
	PhaseRead   = "read"
	PhaseSmart  = "smart"
	PhaseDone   = "done"
)

// Results of a disk test.
const (
	TestPassed    = "passed"
	TestFailed    = "failed"
	TestCancelled = "cancelled"
)

const (
	testChunk = 4 << 20
	// testBlock is the unit that carries its own position, so that data
	// that ends up in the wrong place is noticed.
	testBlock = 4096
	// A disk with this many bad places has failed; there is no point in going on.
	testMaxErrors = 64
)

var (
	// ErrTestRunning means the disk is being tested already.
	ErrTestRunning = errors.New("storage: this disk is being tested")
	// ErrNoTest means there is no test of this disk.
	ErrNoTest = errors.New("storage: no test of this disk")
)

// TestNote is something a test found out. Kind names it for the web UI,
// Detail carries the numbers.
type TestNote struct {
	// Kind is io_errors, smart_unsupported, smart_failed, smart_unhealthy,
	// smart_counters or smart_aborted.
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// DiskTest is a running or finished test of one disk.
type DiskTest struct {
	Disk      string `json:"disk"`
	Model     string `json:"model"`
	Serial    string `json:"serial"`
	SizeBytes uint64 `json:"sizeBytes"`
	Mode      string `json:"mode"`
	Phase     string `json:"phase"`
	// Percent, speed and remaining time of the current phase.
	Percent             float64 `json:"percent"`
	SpeedBytesPerSecond uint64  `json:"speedBytesPerSecond"`
	RemainingSeconds    int64   `json:"remainingSeconds"`
	// Errors counts the places that could not be written or read, or
	// that came back different.
	Errors   int        `json:"errors"`
	Started  time.Time  `json:"started"`
	Finished *time.Time `json:"finished"`
	// Result is empty while the test runs.
	Result string     `json:"result"`
	Notes  []TestNote `json:"notes"`

	cancel context.CancelFunc
}

// blockFile is a disk as the test uses it.
type blockFile interface {
	io.ReaderAt
	io.WriterAt
	Sync() error
	Close() error
}

// openDisk opens a whole disk past the page cache, and exclusively: the
// kernel refuses if anything else holds it (a mount, an array).
func openDisk(path string, write bool) (blockFile, error) {
	flags := unix.O_RDONLY
	if write {
		flags = unix.O_RDWR
	}
	fd, err := unix.Open(path, flags|unix.O_DIRECT|unix.O_EXCL|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}

// alignedBuffer returns memory that starts on a page boundary, as reading
// and writing past the page cache need it.
func alignedBuffer(size int) ([]byte, func()) {
	buffer, err := unix.Mmap(-1, 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		return make([]byte, size), func() {}
	}
	return buffer, func() { unix.Munmap(buffer) }
}

// Tests returns the tests that are running or finished and not dismissed,
// sorted by disk.
func (m *Manager) Tests() []DiskTest {
	m.testMu.Lock()
	defer m.testMu.Unlock()
	list := make([]DiskTest, 0, len(m.tests))
	for _, test := range m.tests {
		copied := *test
		copied.Notes = append([]TestNote{}, test.Notes...)
		list = append(list, copied)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Disk < list[j].Disk })
	return list
}

// testing returns the disks that are being tested right now.
func (m *Manager) testing() map[string]bool {
	m.testMu.Lock()
	defer m.testMu.Unlock()
	busy := map[string]bool{}
	for name, test := range m.tests {
		if test.Result == "" {
			busy[name] = true
		}
	}
	return busy
}

// StartTest begins testing a disk. Writing destroys what is on it and
// needs the name of the disk once more as confirmation.
func (m *Manager) StartTest(ctx context.Context, name, mode, confirm string) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return ErrReadOnly
	}
	if mode != TestRead && mode != TestWrite {
		return fmt.Errorf("%w: unknown kind of test", ErrNotPossible)
	}
	status, err := m.Status(ctx)
	if err != nil {
		return err
	}
	var disk *Disk
	for i := range status.Disks {
		if status.Disks[i].Name == name {
			disk = &status.Disks[i]
		}
	}
	switch {
	case disk == nil:
		return fmt.Errorf("%w: %s not found", ErrDiskUnavailable, name)
	case disk.Testing:
		return ErrTestRunning
	case disk.Role == RoleStick || disk.Role == RoleMember || disk.Mounted:
		return fmt.Errorf("%w: %s is in use", ErrDiskUnavailable, name)
	}
	if m.Claimed != nil {
		if by := m.Claimed()[name]; by != "" {
			return fmt.Errorf("%w: %s belongs to %s", ErrDiskUnavailable, name, by)
		}
	}
	if mode == TestWrite && confirm != name {
		return ErrNotConfirmed
	}

	testCtx, cancel := context.WithCancel(context.Background())
	test := &DiskTest{
		Disk: disk.Name, Model: disk.Model, Serial: disk.Serial, SizeBytes: disk.SizeBytes, Mode: mode,
		Started: time.Now(), Notes: []TestNote{}, cancel: cancel,
	}
	m.testMu.Lock()
	if m.tests == nil {
		m.tests = map[string]*DiskTest{}
	}
	m.tests[name] = test
	m.testMu.Unlock()
	slog.Info("disk test started", "disk", name, "serial", disk.Serial, "mode", mode)
	go m.runTest(testCtx, test, disk.Path)
	return nil
}

// StopTest cancels a running test, or dismisses a finished one.
func (m *Manager) StopTest(name string) error {
	m.testMu.Lock()
	defer m.testMu.Unlock()
	test, ok := m.tests[name]
	if !ok {
		return ErrNoTest
	}
	if test.Result == "" {
		test.cancel()
		return nil
	}
	delete(m.tests, name)
	return nil
}

func (m *Manager) updateTest(test *DiskTest, change func(*DiskTest)) {
	m.testMu.Lock()
	change(test)
	m.testMu.Unlock()
}

func (m *Manager) note(test *DiskTest, kind, detail string) {
	m.updateTest(test, func(t *DiskTest) { t.Notes = append(t.Notes, TestNote{Kind: kind, Detail: detail}) })
}

// runTest carries a test through its phases and records the result.
func (m *Manager) runTest(ctx context.Context, test *DiskTest, path string) {
	before := m.readSmart(ctx, path)
	failed := false

	if errs, err := m.surfaceTest(ctx, test, path); err != nil {
		if ctx.Err() == nil {
			m.note(test, "io_errors", err.Error())
			failed = true
		}
	} else if errs > 0 {
		m.note(test, "io_errors", fmt.Sprint(errs))
		failed = true
	}

	// A disk that could not even be read through need not test itself.
	if ctx.Err() == nil && !failed {
		if !m.smartSelfTest(ctx, test, path, before != nil) {
			failed = true
		}
	}
	if ctx.Err() == nil {
		if after := m.readSmart(ctx, path); after != nil {
			if !after.Healthy {
				m.note(test, "smart_unhealthy", "")
				failed = true
			}
			if before != nil && after.Errors > before.Errors {
				m.note(test, "smart_counters", fmt.Sprintf("%d → %d", before.Errors, after.Errors))
				failed = true
			}
		}
	}

	now := time.Now()
	m.updateTest(test, func(t *DiskTest) {
		t.Phase, t.Finished, t.Percent, t.SpeedBytesPerSecond, t.RemainingSeconds = PhaseDone, &now, 100, 0, 0
		switch {
		case ctx.Err() != nil:
			t.Result = TestCancelled
		case failed:
			t.Result = TestFailed
		default:
			t.Result = TestPassed
		}
	})
	test.cancel()
	slog.Info("disk test finished", "disk", test.Disk, "serial", test.Serial, "result", test.Result, "errors", test.Errors)
	if m.OnTestDone != nil {
		m.testMu.Lock()
		done := *test
		m.testMu.Unlock()
		m.OnTestDone(done)
	}
}

// readSmart reads the health of one disk; nil if it reports none.
func (m *Manager) readSmart(ctx context.Context, path string) *Smart {
	output, _ := m.runner.Run(ctx, "", "smartctl", "--json=c", "--health", "--attributes", "--info", path)
	return parseSmart(output)
}

// surfaceTest reads the whole disk, or overwrites it and reads it back.
// It returns how many places were bad.
func (m *Manager) surfaceTest(ctx context.Context, test *DiskTest, path string) (int, error) {
	open := m.openDisk
	if open == nil {
		open = openDisk
	}
	write := test.Mode == TestWrite
	disk, err := open(path, write)
	if err != nil {
		return 0, err
	}
	defer disk.Close()
	size := int64(test.SizeBytes)

	expected, freeExpected := alignedBuffer(testChunk)
	defer freeExpected()
	got, freeGot := alignedBuffer(testChunk)
	defer freeGot()

	// The same pattern in every chunk, but each block starts with its own
	// position on the disk.
	var seed [32]byte
	binary.LittleEndian.PutUint64(seed[:], uint64(test.Started.UnixNano()))
	pattern := make([]byte, testChunk)
	rand.NewChaCha8(seed).Read(pattern)
	fill := func(offset int64, n int) {
		copy(expected[:n], pattern)
		for at := 0; at+8 <= n; at += testBlock {
			binary.LittleEndian.PutUint64(expected[at:], uint64(offset)+uint64(at))
		}
	}

	errs := 0
	bad := func() bool {
		errs++
		m.updateTest(test, func(t *DiskTest) { t.Errors = errs })
		return errs >= testMaxErrors
	}
	pass := func(phase string, each func(offset int64, n int) bool) bool {
		m.updateTest(test, func(t *DiskTest) { t.Phase, t.Percent, t.SpeedBytesPerSecond, t.RemainingSeconds = phase, 0, 0, -1 })
		started, last, lastOffset := time.Now(), time.Now(), int64(0)
		for offset := int64(0); offset < size; offset += testChunk {
			if ctx.Err() != nil {
				return false
			}
			n := int(min(testChunk, size-offset))
			if !each(offset, n) {
				return false
			}
			if now := time.Now(); now.Sub(last) >= time.Second || offset+int64(n) >= size {
				done := offset + int64(n)
				speed := float64(done-lastOffset) / now.Sub(last).Seconds()
				if average := float64(done) / now.Sub(started).Seconds(); now.Sub(started) > 10*time.Second {
					speed = average // steadier for the estimate
				}
				m.updateTest(test, func(t *DiskTest) {
					t.Percent = 100 * float64(done) / float64(size)
					t.SpeedBytesPerSecond = uint64(speed)
					if speed > 0 {
						t.RemainingSeconds = int64(float64(size-done) / speed)
					}
				})
				last, lastOffset = now, done
			}
		}
		return true
	}

	if write {
		ok := pass(PhaseWrite, func(offset int64, n int) bool {
			fill(offset, n)
			if _, err := disk.WriteAt(expected[:n], offset); err != nil {
				slog.Warn("disk test: write failed", "disk", test.Disk, "offset", offset, "err", err)
				return !bad()
			}
			return true
		})
		if err := disk.Sync(); err != nil && ctx.Err() == nil {
			bad()
		}
		if ok {
			pass(PhaseVerify, func(offset int64, n int) bool {
				fill(offset, n)
				if _, err := disk.ReadAt(got[:n], offset); err != nil {
					slog.Warn("disk test: read failed", "disk", test.Disk, "offset", offset, "err", err)
					return !bad()
				}
				if !bytes.Equal(got[:n], expected[:n]) {
					slog.Warn("disk test: data came back different", "disk", test.Disk, "offset", offset)
					return !bad()
				}
				return true
			})
		}
		// Leave an empty disk behind: nothing at its start or end that
		// could be taken for a partition table or a file system.
		clear(expected)
		for _, offset := range []int64{0, max(0, (size-testChunk)/testBlock*testBlock)} {
			n := int(min(testChunk, size-offset))
			if _, err := disk.WriteAt(expected[:n], offset); err != nil && ctx.Err() == nil && errs == 0 {
				bad()
			}
		}
		disk.Sync()
	} else {
		pass(PhaseRead, func(offset int64, n int) bool {
			if _, err := disk.ReadAt(got[:n], offset); err != nil {
				slog.Warn("disk test: read failed", "disk", test.Disk, "offset", offset, "err", err)
				return !bad()
			}
			return true
		})
	}
	disk.Close()
	if write {
		// Let the kernel and udev see that the disk is empty now.
		m.runner.Run(context.WithoutCancel(ctx), "", "blockdev", "--rereadpt", path)
		m.runner.Run(context.WithoutCancel(ctx), "", "udevadm", "settle")
	}
	return errs, nil
}

// selfTestState is what a disk says about its long self-test.
type selfTestState struct {
	running   bool
	remaining int // percent
	passed    bool
	detail    string
	// minutes the disk expects the test to take; 0 if it does not say.
	minutes int
}

func parseSelfTest(output string) (selfTestState, bool) {
	var data struct {
		ATA *struct {
			SelfTest struct {
				Status struct {
					Value     int    `json:"value"`
					String    string `json:"string"`
					Remaining int    `json:"remaining_percent"`
					Passed    *bool  `json:"passed"`
				} `json:"status"`
				Polling struct {
					Extended int `json:"extended"`
				} `json:"polling_minutes"`
			} `json:"self_test"`
		} `json:"ata_smart_data"`
		NVMe *struct {
			Current struct {
				Value int `json:"value"`
			} `json:"current_self_test_operation"`
			Completion int `json:"current_self_test_completion_percent"`
			Table      []struct {
				Result struct {
					Value  int    `json:"value"`
					String string `json:"string"`
				} `json:"self_test_result"`
			} `json:"table"`
		} `json:"nvme_self_test_log"`
	}
	if json.Unmarshal([]byte(output), &data) != nil {
		return selfTestState{}, false
	}
	switch {
	case data.ATA != nil:
		status := data.ATA.SelfTest.Status
		state := selfTestState{detail: status.String, minutes: data.ATA.SelfTest.Polling.Extended}
		// The upper four bits are 15 while a test runs.
		if status.Value>>4 == 15 {
			state.running, state.remaining = true, status.Remaining
			return state, true
		}
		state.passed = status.Value>>4 == 0 && (status.Passed == nil || *status.Passed)
		return state, true
	case data.NVMe != nil:
		if data.NVMe.Current.Value != 0 {
			return selfTestState{running: true, remaining: 100 - data.NVMe.Completion}, true
		}
		if len(data.NVMe.Table) == 0 {
			return selfTestState{}, false
		}
		newest := data.NVMe.Table[0].Result
		return selfTestState{passed: newest.Value == 0, detail: newest.String}, true
	}
	return selfTestState{}, false
}

// smartSelfTest lets the disk run its long self-test and waits for it.
// It returns false if the disk failed it.
func (m *Manager) smartSelfTest(ctx context.Context, test *DiskTest, path string, hasSmart bool) bool {
	m.updateTest(test, func(t *DiskTest) {
		t.Phase, t.Percent, t.SpeedBytesPerSecond, t.RemainingSeconds = PhaseSmart, 0, 0, -1
	})
	if !hasSmart {
		m.note(test, "smart_unsupported", "")
		return true
	}
	if _, err := m.runner.Run(ctx, "", "smartctl", "--json=c", "--test=long", path); err != nil && ctx.Err() == nil {
		// Some USB adapters pass the health on but no commands.
		m.note(test, "smart_unsupported", "")
		return true
	}
	interval := m.smartPoll
	if interval == 0 {
		interval = 30 * time.Second
	}
	started := time.Now()
	deadline := started.Add(96 * time.Hour)
	seen := false
	for {
		select {
		case <-ctx.Done():
			m.runner.Run(context.WithoutCancel(ctx), "", "smartctl", "--abort", path)
			return true
		case <-time.After(interval):
		}
		output, _ := m.runner.Run(ctx, "", "smartctl", "--json=c", "--capabilities", "--log=selftest", path)
		state, ok := parseSelfTest(output)
		switch {
		case !ok:
			if time.Since(started) > 5*time.Minute && !seen {
				m.note(test, "smart_unsupported", "")
				return true
			}
		case state.running:
			seen = true
			m.updateTest(test, func(t *DiskTest) {
				t.Percent = float64(100 - state.remaining)
				if state.minutes > 0 {
					t.RemainingSeconds = int64(state.minutes) * 60 * int64(state.remaining) / 100
				}
			})
		case !seen && time.Since(started) < 2*time.Minute:
			// The disk has not begun yet; what it reports is an older test.
		case state.passed:
			return true
		default:
			m.note(test, "smart_failed", state.detail)
			return false
		}
		if time.Now().After(deadline) {
			m.runner.Run(context.WithoutCancel(ctx), "", "smartctl", "--abort", path)
			m.note(test, "smart_aborted", "")
			return true
		}
	}
}

// TestsRunning reports whether a disk test is at work.
func (m *Manager) TestsRunning() bool {
	m.testMu.Lock()
	defer m.testMu.Unlock()
	for _, test := range m.tests {
		if test.Result == "" {
			return true
		}
	}
	return false
}
