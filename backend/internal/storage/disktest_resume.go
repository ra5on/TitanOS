package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sort"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// Continuing a disk test after a restart of the NAS or of titand.
//
// A test keeps a checkpoint on a data volume, never on the stick:
// /mnt/<volume>/.titanos/disktests/tests.json. It names the disk by serial
// number, model and size (the /dev name changes between boots), the kind of
// test, the phase and how far that phase got. The checkpoint is written when
// a test starts, when its phase changes, at most every five minutes while a
// phase runs, and once more when the test ends (then without it). Without a
// mounted data volume, or for a disk without a serial number, a test lives
// in RAM only, as before, and ends with a restart.
//
// After a restart a read test that was started with AutoResume continues by
// itself; every other test waits as TestInterrupted for "continue" or
// "dismiss". A test that overwrites is never continued without being asked
// once more. A checkpoint whose disk is missing, is a different disk or is
// in use now is dropped with a TestNotice.

const (
	testStateFeature = "disktests"
	testStateFile    = "tests.json"
	testStateLimit   = 1 << 20
	testStateVersion = 1
	// testCheckpointEvery is the longest a running phase goes without a
	// checkpoint; what was done since is done again after a restart.
	testCheckpointEvery = 5 * time.Minute
)

// TestNotice tells about a checkpoint that was dropped after a restart.
type TestNotice struct {
	// Kind is disk_missing, identity_mismatch, disk_in_use or state_unreadable.
	Kind    string    `json:"kind"`
	Serial  string    `json:"serial,omitempty"`
	Model   string    `json:"model,omitempty"`
	Mode    string    `json:"mode,omitempty"`
	Started time.Time `json:"started"`
	Time    time.Time `json:"time"`
}

type savedTests struct {
	Version int         `json:"version"`
	Tests   []savedTest `json:"tests"`
}

// savedTest is the checkpoint of one test.
type savedTest struct {
	// Disk is the name the disk had, for the log only.
	Disk       string     `json:"disk"`
	Serial     string     `json:"serial"`
	Model      string     `json:"model"`
	SizeBytes  uint64     `json:"sizeBytes"`
	Mode       string     `json:"mode"`
	Phase      string     `json:"phase"`
	Offset     int64      `json:"offset"`
	Errors     int        `json:"errors"`
	Notes      []TestNote `json:"notes"`
	Started    time.Time  `json:"started"`
	Seed       uint64     `json:"seed"`
	AutoResume bool       `json:"autoResume"`
	// SmartKnown and SmartErrors are the counters before the test.
	SmartKnown  bool       `json:"smartKnown"`
	SmartErrors uint64     `json:"smartErrors"`
	Resumes     int        `json:"resumes"`
	Resumed     *time.Time `json:"resumed,omitempty"`
	Saved       time.Time  `json:"saved"`
}

// keepsCheckpoint reports whether a test belongs into the checkpoint file.
func keepsCheckpoint(test *DiskTest) bool {
	return (test.Mode == TestRead || test.Mode == TestWrite) && (test.Result == "" || test.Result == TestInterrupted)
}

func (m *Manager) checkpointInterval() time.Duration {
	if m.checkpointEvery > 0 {
		return m.checkpointEvery
	}
	return testCheckpointEvery
}

// checkpointDue reports whether a running phase should write its progress.
func (m *Manager) checkpointDue(test *DiskTest) bool {
	m.testMu.Lock()
	defer m.testMu.Unlock()
	return time.Since(test.saved) >= m.checkpointInterval()
}

func (m *Manager) stateVolumes() []string {
	if m.testStateVolumes != nil {
		return m.testStateVolumes()
	}
	volumes := m.Mounted()
	sort.Strings(volumes)
	return volumes
}

func (m *Manager) openTestState(volume string, mounted []string) (*os.Root, error) {
	return volumedata.Open(m.mountRoot, volume, testStateFeature, mounted)
}

// saveTests writes the checkpoints of all tests that can be continued. It
// is called at the start, at each change of phase, at most every few
// minutes while a phase runs and when a test ends.
func (m *Manager) saveTests() {
	if m.ReadOnly {
		return
	}
	m.testSaveMu.Lock()
	defer m.testSaveMu.Unlock()
	now := time.Now()
	state := savedTests{Version: testStateVersion, Tests: []savedTest{}}
	var kept []*DiskTest
	m.testMu.Lock()
	for _, test := range m.tests {
		if !keepsCheckpoint(test) {
			continue
		}
		if test.Serial == "" {
			test.Persistent, test.NotPersistent = false, "no_serial"
			continue
		}
		kept = append(kept, test)
		state.Tests = append(state.Tests, savedTest{
			Disk: test.Disk, Serial: test.Serial, Model: test.Model, SizeBytes: test.SizeBytes,
			Mode: test.Mode, Phase: test.Phase, Offset: test.offset, Errors: test.Errors,
			Notes: append([]TestNote{}, test.Notes...), Started: test.Started, Seed: test.seed,
			AutoResume: test.AutoResume, SmartKnown: test.smartKnown, SmartErrors: test.smartErrors,
			Resumes: test.Resumes, Resumed: test.Resumed, Saved: now,
		})
	}
	m.testMu.Unlock()
	sort.Slice(state.Tests, func(i, j int) bool { return state.Tests[i].Serial < state.Tests[j].Serial })

	mounted := m.stateVolumes()
	// Stay on the volume used so far; otherwise take the first one.
	volume := m.testStateVolume
	if !slices.Contains(mounted, volume) {
		volume = ""
		if len(mounted) > 0 {
			volume = mounted[0]
		}
	}
	mark := func(persistent bool, reason string) {
		m.testMu.Lock()
		for _, test := range kept {
			test.Persistent, test.NotPersistent = persistent, reason
			test.saved = now
		}
		m.testMu.Unlock()
	}
	var err error
	switch {
	case volume == "" && len(kept) > 0:
		mark(false, "no_volume")
	case volume == "":
	case len(kept) == 0 && !m.testStateFiles[volume]:
		// Nothing to keep, and no file there to clear.
	default:
		err = m.writeTestState(volume, mounted, state)
		if err != nil {
			slog.Warn("disk test: checkpoint not written", "volume", volume, "err", err)
			mark(false, "write_failed")
		} else {
			mark(true, "")
			m.testStateVolume = volume
			if m.testStateFiles == nil {
				m.testStateFiles = map[string]bool{}
			}
			m.testStateFiles[volume] = len(kept) > 0
		}
	}
	// Older files on other volumes would bring back outdated checkpoints.
	if err == nil {
		for other, present := range m.testStateFiles {
			if other == volume || !present || !slices.Contains(mounted, other) {
				continue
			}
			if root, e := m.openTestState(other, mounted); e == nil {
				if e := root.Remove(testStateFile); e == nil || errors.Is(e, os.ErrNotExist) {
					m.testStateFiles[other] = false
				}
				root.Close()
			}
		}
	}
}

func (m *Manager) writeTestState(volume string, mounted []string, state savedTests) error {
	root, err := m.openTestState(volume, mounted)
	if err != nil {
		return err
	}
	defer root.Close()
	if len(state.Tests) == 0 {
		if err := root.Remove(testStateFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return volumedata.WriteJSON(root, testStateFile, state)
}

// readTestState reads the checkpoints from the volumes not looked at yet.
func (m *Manager) readTestState() ([]savedTest, []TestNotice) {
	m.testSaveMu.Lock()
	defer m.testSaveMu.Unlock()
	mounted := m.stateVolumes()
	var found []savedTest
	var notices []TestNotice
	for _, volume := range mounted {
		if m.testStateScanned[volume] {
			continue
		}
		if m.testStateScanned == nil {
			m.testStateScanned = map[string]bool{}
		}
		m.testStateScanned[volume] = true
		root, err := m.openTestState(volume, mounted)
		if err != nil {
			slog.Warn("disk test: checkpoints not readable", "volume", volume, "err", err)
			continue
		}
		var state savedTests
		err = volumedata.ReadJSON(root, testStateFile, &state, testStateLimit)
		root.Close()
		switch {
		case errors.Is(err, os.ErrNotExist):
			continue
		case err == nil && state.Version != testStateVersion:
			err = fmt.Errorf("unknown version %d", state.Version)
		}
		if m.testStateFiles == nil {
			m.testStateFiles = map[string]bool{}
		}
		m.testStateFiles[volume] = true
		if err != nil {
			slog.Warn("disk test: checkpoints dropped", "volume", volume, "err", err)
			notices = append(notices, TestNotice{Kind: "state_unreadable", Time: time.Now()})
			continue
		}
		if m.testStateVolume == "" {
			m.testStateVolume = volume
		}
		found = append(found, state.Tests...)
	}
	// The newest checkpoint of a disk wins.
	sort.SliceStable(found, func(i, j int) bool { return found[i].Saved.After(found[j].Saved) })
	return found, notices
}

// ResumeSavedTests picks up the checkpoints on data volumes mounted since
// it last ran: a matching disk gets its test back, as interrupted or, for a
// read test started with AutoResume, running again. It is called by Run
// once the volumes are settled, and again when another volume appears.
func (m *Manager) ResumeSavedTests(ctx context.Context) {
	if m.ReadOnly {
		return
	}
	found, notices := m.readTestState()
	if len(found) == 0 && len(notices) == 0 {
		return
	}
	m.actions.Lock()
	status, err := m.Status(ctx)
	if err != nil {
		m.actions.Unlock()
		slog.Warn("disk test: checkpoints not checked", "err", err)
		// Look at these volumes again on the next round.
		m.testSaveMu.Lock()
		m.testStateScanned = nil
		m.testSaveMu.Unlock()
		return
	}
	var claimed map[string]string
	if m.Claimed != nil {
		claimed = m.Claimed()
	}
	var auto, waiting []string
	seen := map[string]bool{}
	m.testMu.Lock()
	if m.tests == nil {
		m.tests = map[string]*DiskTest{}
	}
	for _, existing := range m.tests {
		seen[existing.Serial] = true
	}
	for _, saved := range found {
		if saved.Serial == "" || seen[saved.Serial] || (saved.Mode != TestRead && saved.Mode != TestWrite) {
			continue
		}
		seen[saved.Serial] = true
		notice := TestNotice{Serial: saved.Serial, Model: saved.Model, Mode: saved.Mode, Started: saved.Started, Time: time.Now()}
		disk, kind := matchSavedDisk(status.Disks, saved, claimed)
		if kind != "" {
			notice.Kind = kind
			notices = append(notices, notice)
			slog.Warn("disk test: checkpoint dropped", "serial", saved.Serial, "reason", kind)
			continue
		}
		if existing := m.tests[disk.Name]; existing != nil && existing.Result == "" {
			continue
		}
		size := int64(max(saved.SizeBytes, 1))
		test := &DiskTest{
			Disk: disk.Name, Model: saved.Model, Serial: saved.Serial, SizeBytes: saved.SizeBytes,
			Mode: saved.Mode, Phase: saved.Phase, Errors: saved.Errors, Started: saved.Started,
			Notes: append([]TestNote{}, saved.Notes...), Result: TestInterrupted,
			AutoResume: saved.AutoResume && saved.Mode == TestRead, Persistent: true,
			Resumed: saved.Resumed, Resumes: saved.Resumes, RemainingSeconds: -1,
			cancel: func() {}, seed: saved.Seed, offset: min(max(saved.Offset, 0), size),
			smartKnown: saved.SmartKnown, smartErrors: saved.SmartErrors, saved: time.Now(),
		}
		if test.Notes == nil {
			test.Notes = []TestNote{}
		}
		if test.Phase != PhaseSmart {
			test.Percent = 100 * float64(test.offset) / float64(size)
		}
		m.tests[disk.Name] = test
		slog.Info("disk test: checkpoint found", "disk", disk.Name, "serial", saved.Serial, "mode", saved.Mode, "phase", saved.Phase, "offset", test.offset)
		if test.AutoResume {
			auto = append(auto, disk.Name)
		} else {
			waiting = append(waiting, disk.Name)
		}
	}
	m.testNotices = append(m.testNotices, notices...)
	if len(m.testNotices) > 20 {
		m.testNotices = m.testNotices[len(m.testNotices)-20:]
	}
	m.testMu.Unlock()
	m.actions.Unlock()

	for _, name := range auto {
		if err := m.ResumeTest(ctx, name, ""); err != nil {
			slog.Warn("disk test: not continued", "disk", name, "err", err)
			waiting = append(waiting, name)
		}
	}
	// Dropped checkpoints leave the file.
	m.saveTests()
	if m.OnTestInterrupted != nil {
		for _, name := range waiting {
			m.testMu.Lock()
			test, ok := m.tests[name]
			var copied DiskTest
			if ok {
				copied = *test
			}
			m.testMu.Unlock()
			if ok && copied.Result == TestInterrupted {
				m.OnTestInterrupted(copied)
			}
		}
	}
}

// matchSavedDisk finds the disk of a checkpoint by its serial number and
// checks that model and size still fit. It returns why not otherwise.
func matchSavedDisk(disks []Disk, saved savedTest, claimed map[string]string) (*Disk, string) {
	var match *Disk
	for i := range disks {
		if disks[i].Serial == saved.Serial {
			if match != nil {
				return nil, "identity_mismatch" // two disks with one serial
			}
			match = &disks[i]
		}
	}
	switch {
	case match == nil:
		return nil, "disk_missing"
	case match.Model != saved.Model || match.SizeBytes != saved.SizeBytes:
		return nil, "identity_mismatch"
	case match.Role == RoleStick || match.Role == RoleMember || match.Mounted || claimed[match.Name] != "":
		return nil, "disk_in_use"
	}
	return match, ""
}

// ResumeTest continues an interrupted test from its checkpoint. A test that
// overwrites needs the name of the disk once more, like at its start.
func (m *Manager) ResumeTest(ctx context.Context, name, confirm string) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	m.testMu.Lock()
	test := m.tests[name]
	waiting := test != nil && test.Result == TestInterrupted
	m.testMu.Unlock()
	if !waiting {
		return ErrNoTest
	}
	disk, err := m.testableDisk(ctx, name)
	if err != nil {
		return err
	}
	if disk.Serial != test.Serial || disk.Model != test.Model || disk.SizeBytes != test.SizeBytes {
		// Another disk sits under this name now; the checkpoint is not for it.
		m.testMu.Lock()
		delete(m.tests, name)
		m.testNotices = append(m.testNotices, TestNotice{Kind: "identity_mismatch", Serial: test.Serial, Model: test.Model, Mode: test.Mode, Started: test.Started, Time: time.Now()})
		m.testMu.Unlock()
		go m.saveTests()
		return fmt.Errorf("%w: %s is not the disk that was tested", ErrDiskUnavailable, name)
	}
	if test.Mode == TestWrite && confirm != name {
		return ErrNotConfirmed
	}
	testCtx, cancel := context.WithCancel(context.Background())
	now := time.Now()
	m.updateTest(test, func(t *DiskTest) {
		t.Result, t.Finished, t.cancel, t.resume = "", nil, cancel, true
		t.Resumed, t.Resumes = &now, t.Resumes+1
	})
	slog.Info("disk test continued", "disk", name, "serial", test.Serial, "mode", test.Mode, "phase", test.Phase, "offset", test.offset)
	go m.runTest(testCtx, test, disk.Path)
	return nil
}

// TestNotices returns the checkpoints that were dropped after a restart.
func (m *Manager) TestNotices() []TestNotice {
	m.testMu.Lock()
	defer m.testMu.Unlock()
	return append([]TestNotice{}, m.testNotices...)
}

// DismissTestNotices forgets them.
func (m *Manager) DismissTestNotices() {
	m.testMu.Lock()
	m.testNotices = nil
	m.testMu.Unlock()
}
