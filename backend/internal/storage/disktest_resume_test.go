package storage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// gateDisk records where it was read and written and stops for good at
// blockAt, as a disk test does when the NAS loses power there.
type gateDisk struct {
	*memDisk
	blockAt       int64
	mu            sync.Mutex
	reads, writes []int64
}

func (d *gateDisk) hold(offset int64) {
	if d.blockAt >= 0 && offset >= d.blockAt {
		select {} // the "restart": this test never gets further
	}
}

func (d *gateDisk) ReadAt(p []byte, offset int64) (int, error) {
	d.mu.Lock()
	d.reads = append(d.reads, offset)
	d.mu.Unlock()
	d.hold(offset)
	return d.memDisk.ReadAt(p, offset)
}

func (d *gateDisk) WriteAt(p []byte, offset int64) (int, error) {
	d.mu.Lock()
	d.writes = append(d.writes, offset)
	d.mu.Unlock()
	d.hold(offset)
	return d.memDisk.WriteAt(p, offset)
}

func (d *gateDisk) first(list *[]int64) int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(*list) == 0 {
		return -1
	}
	return (*list)[0]
}

// withDataVolume gives the test a mounted data volume "data" under root
// (the root of an earlier test when it is to see the same checkpoints).
func withDataVolume(t *testing.T, e *testEnv, root string) *gateDisk {
	t.Helper()
	if root == "" {
		root = t.TempDir()
	}
	if err := os.MkdirAll(filepath.Join(root, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	e.m.mountRoot = root
	e.m.testStateVolumes = func() []string { return []string{"data"} }
	e.m.checkpointEvery = time.Nanosecond
	gate := &gateDisk{memDisk: e.disk, blockAt: -1}
	e.m.openDisk = func(path string, write bool) (blockFile, error) {
		e.opened = append(e.opened, path)
		return gate, nil
	}
	return gate
}

func statePath(e *testEnv) string {
	return filepath.Join(e.m.mountRoot, "data", ".titanos", testStateFeature, testStateFile)
}

func readState(t *testing.T, e *testEnv) (savedTests, bool) {
	t.Helper()
	data, err := os.ReadFile(statePath(e))
	if errors.Is(err, os.ErrNotExist) {
		return savedTests{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	var state savedTests
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state, true
}

func writeState(t *testing.T, e *testEnv, tests ...savedTest) {
	t.Helper()
	dir := filepath.Dir(statePath(e))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	os.Chmod(filepath.Dir(dir), 0700)
	data, _ := json.Marshal(savedTests{Version: testStateVersion, Tests: tests})
	if err := os.WriteFile(statePath(e), data, 0600); err != nil {
		t.Fatal(err)
	}
}

// waitState waits until the checkpoint shows the phase at least at offset.
func waitState(t *testing.T, e *testEnv, phase string, offset int64) savedTest {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if state, ok := readState(t, e); ok && len(state.Tests) == 1 && state.Tests[0].Phase == phase && state.Tests[0].Offset >= offset {
			return state.Tests[0]
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("no checkpoint in phase %s at %d", phase, offset)
	return savedTest{}
}

func waitDone(t *testing.T, e *testEnv) DiskTest {
	t.Helper()
	select {
	case test := <-e.done:
		return test
	case <-time.After(20 * time.Second):
		t.Fatal("the test did not finish")
		return DiskTest{}
	}
}

func TestReadTestContinuesByItselfFromItsCheckpoint(t *testing.T) {
	before := newDiskTestEnv(t)
	gate := withDataVolume(t, before, "")
	gate.blockAt = 8 << 20
	if err := before.m.StartTest(context.Background(), "sda", TestRead, "", true); err != nil {
		t.Fatal(err)
	}
	saved := waitState(t, before, PhaseRead, 8<<20)
	if saved.Serial != "NEW1" || saved.Model != "NEW DISK" || saved.SizeBytes != 10498048 || !saved.AutoResume || !saved.SmartKnown || saved.Mode != TestRead {
		t.Fatalf("checkpoint: %+v", saved)
	}
	if list := before.m.Tests(); !list[0].Persistent || list[0].NotPersistent != "" {
		t.Errorf("not marked as kept: %+v", list[0])
	}

	// After the restart the disk is called sda again; it is found by its serial.
	after := newDiskTestEnv(t)
	gateAfter := withDataVolume(t, after, before.m.mountRoot)
	interrupted := 0
	after.m.OnTestInterrupted = func(DiskTest) { interrupted++ }
	after.m.ResumeSavedTests(context.Background())
	test := waitDone(t, after)
	if test.Result != TestPassed || test.Resumes != 1 || test.Resumed == nil || !test.Started.Equal(saved.Started) || interrupted != 0 {
		t.Fatalf("continued test: %+v", test)
	}
	if first := gateAfter.first(&gateAfter.reads); first != 8<<20 {
		t.Errorf("continued reading at %d, not at the checkpoint", first)
	}
	if after.runner.ran("--test=long") == false {
		t.Error("the self-test was not run after continuing")
	}
	if state, ok := readState(t, after); ok && len(state.Tests) != 0 {
		t.Errorf("checkpoint left after the end: %+v", state)
	}
	// Reading it once more finds nothing to continue.
	again := newDiskTestEnv(t)
	withDataVolume(t, again, before.m.mountRoot)
	again.m.ResumeSavedTests(context.Background())
	if len(again.m.Tests()) != 0 {
		t.Errorf("a finished test came back: %+v", again.m.Tests())
	}
}

func TestWriteTestIsNeverContinuedWithoutBeingAsked(t *testing.T) {
	before := newDiskTestEnv(t)
	gate := withDataVolume(t, before, "")
	gate.blockAt = 8 << 20
	if err := before.m.StartTest(context.Background(), "sda", TestWrite, "sda", true); err != nil {
		t.Fatal(err)
	}
	saved := waitState(t, before, PhaseWrite, 8<<20)
	if saved.AutoResume {
		t.Error("a writing test was saved to continue by itself")
	}

	after := newDiskTestEnv(t)
	after.disk = before.disk // the same disk, with what was written so far
	gateAfter := withDataVolume(t, after, before.m.mountRoot)
	var waiting []DiskTest
	after.m.OnTestInterrupted = func(test DiskTest) { waiting = append(waiting, test) }
	// Even a checkpoint that claims otherwise does not continue writing.
	state, _ := readState(t, before)
	state.Tests[0].AutoResume = true
	writeState(t, after, state.Tests...)
	after.m.ResumeSavedTests(context.Background())
	list := after.m.Tests()
	if len(list) != 1 || list[0].Result != TestInterrupted || list[0].AutoResume || len(waiting) != 1 || list[0].Disk != "sda" {
		t.Fatalf("waiting test: %+v, told %d", list, len(waiting))
	}
	if gateAfter.first(&gateAfter.writes) != -1 {
		t.Fatal("the disk was written before anyone asked")
	}
	if after.m.TestsRunning() {
		t.Error("a waiting test counts as running and would hold off updates")
	}
	if err := after.m.ResumeTest(context.Background(), "sda", ""); !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("continued without confirmation: %v", err)
	}
	if err := after.m.ResumeTest(context.Background(), "sda", "sda"); err != nil {
		t.Fatal(err)
	}
	test := waitDone(t, after)
	// It passes only if the continued test knows the pattern written before.
	if test.Result != TestPassed || test.Errors != 0 || test.Resumes != 1 {
		t.Fatalf("continued write test: %+v", test)
	}
	if first := gateAfter.first(&gateAfter.writes); first != 8<<20 {
		t.Errorf("continued writing at %d, not at the checkpoint", first)
	}
}

func TestCheckpointsOfOtherDisksAreDropped(t *testing.T) {
	e := newDiskTestEnv(t)
	withDataVolume(t, e, "")
	started := time.Now().Add(-time.Hour).UTC()
	writeState(t, e,
		savedTest{Serial: "NEW1", Model: "OTHER", SizeBytes: 10498048, Mode: TestRead, Phase: PhaseRead, Offset: 4 << 20, Started: started, AutoResume: true},
		savedTest{Serial: "GONE", Model: "X", SizeBytes: 1, Mode: TestRead, Phase: PhaseRead, Started: started},
		savedTest{Serial: "BUSY1", Model: "BUSY", SizeBytes: 10498048, Mode: TestWrite, Phase: PhaseWrite, Started: started},
	)
	e.m.ResumeSavedTests(context.Background())
	if len(e.m.Tests()) != 0 || len(e.opened) != 0 {
		t.Fatalf("a checkpoint was taken for another disk: %+v", e.m.Tests())
	}
	kinds := map[string]string{}
	for _, notice := range e.m.TestNotices() {
		kinds[notice.Serial] = notice.Kind
		if !notice.Started.Equal(started) {
			t.Errorf("notice without the start: %+v", notice)
		}
	}
	if kinds["NEW1"] != "identity_mismatch" || kinds["GONE"] != "disk_missing" || kinds["BUSY1"] != "disk_in_use" {
		t.Errorf("notices: %v", kinds)
	}
	if state, ok := readState(t, e); ok && len(state.Tests) != 0 {
		t.Errorf("dropped checkpoints stayed: %+v", state)
	}
	e.m.DismissTestNotices()
	if len(e.m.TestNotices()) != 0 {
		t.Error("notices not dismissed")
	}

	// An unreadable file is dropped with a notice, too.
	e = newDiskTestEnv(t)
	withDataVolume(t, e, "")
	writeState(t, e)
	os.WriteFile(statePath(e), []byte("{broken"), 0600)
	e.m.ResumeSavedTests(context.Background())
	if notices := e.m.TestNotices(); len(notices) != 1 || notices[0].Kind != "state_unreadable" {
		t.Errorf("unreadable: %+v", notices)
	}
}

func TestADifferentDiskUnderTheSameNameIsNotContinued(t *testing.T) {
	e := newDiskTestEnv(t)
	withDataVolume(t, e, "")
	writeState(t, e, savedTest{Serial: "NEW1", Model: "NEW DISK", SizeBytes: 10498048, Mode: TestRead, Phase: PhaseRead, Offset: 4 << 20, Started: time.Now()})
	e.m.ResumeSavedTests(context.Background())
	// The disk was swapped while the test waited.
	e.m.testMu.Lock()
	e.m.tests["sda"].Serial = "SOMEONE"
	e.m.testMu.Unlock()
	if err := e.m.ResumeTest(context.Background(), "sda", ""); !errors.Is(err, ErrDiskUnavailable) {
		t.Fatalf("continued on another disk: %v", err)
	}
	if len(e.m.Tests()) != 0 || len(e.m.TestNotices()) != 1 || len(e.opened) != 0 {
		t.Errorf("tests %+v, notices %+v", e.m.Tests(), e.m.TestNotices())
	}
	if err := e.m.ResumeTest(context.Background(), "sdb", ""); !errors.Is(err, ErrNoTest) {
		t.Errorf("continuing nothing: %v", err)
	}
}

func TestSelfTestIsWatchedAgainOrStartedOnceMore(t *testing.T) {
	for name, tc := range map[string]struct {
		answers []string
		starts  bool
	}{
		"still running": {[]string{selfRunning, selfPassed}, false},
		"broken off":    {[]string{selfPassed, selfRunning, selfPassed}, true},
	} {
		e := newDiskTestEnv(t)
		withDataVolume(t, e, "")
		e.runner.selfTest = tc.answers
		writeState(t, e, savedTest{Serial: "NEW1", Model: "NEW DISK", SizeBytes: 10498048, Mode: TestRead, Phase: PhaseSmart, Started: time.Now(), AutoResume: true, SmartKnown: true})
		e.m.ResumeSavedTests(context.Background())
		test := waitDone(t, e)
		if test.Result != TestPassed || len(e.opened) != 0 {
			t.Errorf("%s: %+v, opened %v", name, test, e.opened)
		}
		if e.runner.ran("--test=long") != tc.starts {
			t.Errorf("%s: self-test started: %v", name, !tc.starts)
		}
	}
}

func TestDismissingAWaitingTestRemovesItsCheckpoint(t *testing.T) {
	e := newDiskTestEnv(t)
	withDataVolume(t, e, "")
	writeState(t, e, savedTest{Serial: "NEW1", Model: "NEW DISK", SizeBytes: 10498048, Mode: TestWrite, Phase: PhaseVerify, Offset: 4 << 20, Started: time.Now()})
	e.m.ResumeSavedTests(context.Background())
	if list := e.m.Tests(); len(list) != 1 || list[0].Percent < 39 || list[0].Percent > 41 {
		t.Fatalf("waiting test: %+v", list)
	}
	if err := e.m.StopTest("sda"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := readState(t, e); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the checkpoint stayed after dismissing")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestWithoutDataVolumeATestLivesInRAMOnly(t *testing.T) {
	e := newDiskTestEnv(t)
	e.m.testStateVolumes = func() []string { return nil }
	e.disk.slow = 5 * time.Millisecond
	if err := e.m.StartTest(context.Background(), "sda", TestRead, "", true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		list := e.m.Tests()
		if len(list) == 1 && list[0].NotPersistent == "no_volume" && !list[0].Persistent {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not marked: %+v", list)
		}
		time.Sleep(time.Millisecond)
	}
	e.m.StopTest("sda")
	waitDone(t, e)
	entries, _ := os.ReadDir(e.m.mountRoot)
	if len(entries) != 0 {
		t.Errorf("something was written without a volume: %v", entries)
	}
}

func TestCheckpointsAreWrittenOnlyEveryFewMinutes(t *testing.T) {
	m := &Manager{}
	test := &DiskTest{saved: time.Now()}
	if m.checkpointDue(test) {
		t.Error("a checkpoint right after the last one")
	}
	test.saved = time.Now().Add(-testCheckpointEvery)
	if !m.checkpointDue(test) {
		t.Error("no checkpoint after five minutes")
	}
	if testCheckpointEvery < 5*time.Minute {
		t.Error("checkpoints more often than every five minutes")
	}
}
