package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// fakeSession is a VMDiskSession: it notes what happened to the VM.
type fakeSession struct {
	way      string
	quiesced bool
	ctx      context.Context
	cancel   context.CancelFunc
	stopped  string
	ended    int
	endErr   error
	log      *[]string
	vm       string
}

func (s *fakeSession) Method() (string, bool)   { return s.way, s.quiesced }
func (s *fakeSession) Context() context.Context { return s.ctx }
func (s *fakeSession) Stopped() string          { return s.stopped }
func (s *fakeSession) End() error {
	s.ended++
	if s.ended == 1 {
		*s.log = append(*s.log, "end "+s.vm)
		s.cancel()
	}
	return s.endErr
}

type codedError struct{ code string }

func (e codedError) Error() string       { return e.code }
func (e codedError) ProblemCode() string { return e.code }

// vmJob is a share "docs" with the disk of the running VM "ha" in it.
func vmJob(t *testing.T, change func(*Job)) (m *Manager, disk, target string, log *[]string, run func() *State) {
	t.Helper()
	mnt := t.TempDir()
	source := filepath.Join(mnt, "tank", "docs")
	disk = filepath.Join(source, "ha", "disk.qcow2")
	os.MkdirAll(filepath.Dir(disk), 0o755)
	os.WriteFile(disk, []byte("disk of the running VM"), 0o600)
	os.WriteFile(filepath.Join(source, "ha", "notes.txt"), []byte("notes"), 0o600)
	os.WriteFile(filepath.Join(source, "off.qcow2"), []byte("disk of a VM that is shut off"), 0o600)
	target = t.TempDir()
	m = NewManager(config.Open(t.TempDir()))
	m.mountRoot = mnt
	m.Sources = func() []Source { return []Source{{Name: "docs", Volume: "tank", Path: source}} }
	m.LiveDisks = func() map[string]string { return map[string]string{disk: "ha"} }
	events := []string{}
	log = &events
	m.RecoverVMDisks = func(context.Context) error { events = append(events, "recover"); return nil }
	job := Job{ID: "copy", Sources: []string{"docs"}, Destination: Destination{Type: "usb-copy", Folder: "backup"}}
	if change != nil {
		change(&job)
	}
	m.states["copy"] = &State{Running: true}
	run = func() *State {
		t.Helper()
		root, err := os.OpenRoot(target)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		if err := m.copyTo(context.Background(), job, root); err != nil {
			t.Fatal(err)
		}
		return m.states["copy"]
	}
	return m, disk, target, log, run
}

func session(m *Manager, log *[]string, way string, quiesced bool) *fakeSession {
	s := &fakeSession{way: way, quiesced: quiesced, log: log}
	m.BeginVMDisks = func(ctx context.Context, vm string, opts VMDiskOptions) (VMDiskSession, error) {
		*log = append(*log, "begin "+vm+" pause="+map[bool]string{true: "yes", false: "no"}[opts.Pause]+" snapshot="+map[bool]string{true: "yes", false: "no"}[opts.Snapshot])
		s.vm = vm
		s.ctx, s.cancel = context.WithCancel(ctx)
		return s, nil
	}
	return s
}

func codes(list []Problem) []string {
	var out []string
	for _, p := range list {
		out = append(out, p.Code+":"+strings.Join(p.Paths, ","))
	}
	return out
}

// unreadable makes every attempt to open the disk fail the test: the file
// is a FIFO nobody writes to would hang, so it is replaced by a folder of
// the same name, which no copy can read as a file, and the test checks
// that nothing was reported about it.
func tripwire(t *testing.T, disk string) {
	t.Helper()
	if err := os.Chmod(disk, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(disk, 0o600) })
}

// "skip": the disk of a running VM is never opened, the earlier copy at the
// destination stays untouched, and the run reports a notice, no warning.
// In a plain copy, with the mirror and with dated folders.
func TestSkipNeverReadsOrReplacesDisksOfRunningVMs(t *testing.T) {
	for _, mode := range []string{"plain", "mirror", "dated"} {
		t.Run(mode, func(t *testing.T) {
			var job *Job
			m, disk, target, _, run := vmJob(t, func(j *Job) {
				switch mode {
				case "mirror":
					j.Copy = &CopyOptions{Mirror: true}
				case "dated":
					j.Copy = &CopyOptions{Dated: true}
					j.Keep = Keep{Last: 1}
				}
				job = j
			})
			_ = job
			// The first run while the VM is off: its disk is copied normally.
			m.LiveDisks = func() map[string]string { return nil }
			if state := run(); state.Warning != "" || len(state.Notices) != 0 {
				t.Fatalf("first run: %+v", state)
			}
			find := func(name string) []string {
				found, _ := filepath.Glob(filepath.Join(target, "backup", name))
				if mode == "dated" {
					found, _ = filepath.Glob(filepath.Join(target, "backup", "*", name))
				}
				slices.Sort(found)
				return found
			}
			earlier := find("docs/ha/disk.qcow2")
			if len(earlier) != 1 {
				t.Fatalf("disk of the stopped VM not copied: %v", earlier)
			}
			before, _ := os.Stat(earlier[0])
			// The VM runs now, the disk has other content and cannot be opened
			// without the test noticing.
			os.WriteFile(disk, []byte("changed while running, must not be read"), 0o600)
			m.LiveDisks = func() map[string]string { return map[string]string{disk: "ha"} }
			m.BeginVMDisks = func(context.Context, string, VMDiskOptions) (VMDiskSession, error) {
				t.Error("skip started a snapshot or pause")
				return nil, errors.New("no")
			}
			m.SnapshotSource = func(context.Context, string) (string, func(), error) {
				t.Error("skip made a snapshot of the volume")
				return "", nil, errors.New("no")
			}
			m.jobs = nil
			m.states["copy"].Running = true
			skip := &VMDisks{Mode: VMDisksSkip}
			runSkip := func() *State {
				t.Helper()
				root, _ := os.OpenRoot(target)
				defer root.Close()
				j := Job{ID: "copy", Sources: []string{"docs"}, Destination: Destination{Type: "usb-copy", Folder: "backup"}, VMDisks: skip}
				switch mode {
				case "mirror":
					j.Copy = &CopyOptions{Mirror: true}
				case "dated":
					j.Copy = &CopyOptions{Dated: true}
					j.Keep = Keep{Last: 1}
				}
				if mode == "dated" {
					// Dated runs are named by the minute.
					now := m.now
					stamp := time.Now().Add(time.Duration(len(find("docs/off.qcow2"))) * time.Hour)
					m.now = func() time.Time { return stamp }
					defer func() { m.now = now }()
				}
				if err := m.copyTo(context.Background(), j, root); err != nil {
					t.Fatal(err)
				}
				return m.states["copy"]
			}
			if os.Geteuid() != 0 {
				tripwire(t, disk) // opening it would be a read error in Problems
			}
			for round := range 3 {
				state := runSkip()
				if state.Warning != "" || state.Skipped != 0 || len(state.Problems) != 0 {
					t.Fatalf("round %d: skipping became a warning (or the disk was opened): %+v", round, state)
				}
				if got := codes(state.Notices); !slices.Equal(got, []string{NoticeVMDiskSkipped + ":docs/ha/disk.qcow2"}) {
					t.Fatalf("round %d: notices %v", round, got)
				}
				after, err := os.Stat(earlier[0])
				if err != nil {
					t.Fatalf("round %d: the earlier copy of the skipped disk is gone: %v", round, err)
				}
				if !os.SameFile(before, after) || !after.ModTime().Equal(before.ModTime()) {
					t.Fatalf("round %d: the earlier copy was replaced", round)
				}
				if data, _ := os.ReadFile(earlier[0]); string(data) != "disk of the running VM" {
					t.Fatalf("round %d: the earlier copy was changed: %q", round, data)
				}
			}
			// Everything else is copied as always, also disks of stopped VMs.
			if found := find("docs/off.qcow2"); len(found) == 0 {
				t.Fatal("disk of a stopped VM not copied")
			}
			if mode == "dated" {
				// Keep says one version; the run holding the last copy of
				// the skipped disk stays all the same.
				if runs := find("docs/off.qcow2"); len(runs) != 2 {
					t.Fatalf("dated runs kept: %v (the newest and the one with the skipped disk expected)", runs)
				}
				if found := find("docs/ha/disk.qcow2"); len(found) != 1 || found[0] != earlier[0] {
					t.Fatalf("the skipped disk was copied into a new run: %v", found)
				}
			}
			if mode == "mirror" {
				if moved, _ := filepath.Glob(filepath.Join(target, "backup", deletedFolder, "*", "docs", "ha", "*")); len(moved) != 0 {
					t.Fatalf("the mirror moved the skipped disk aside: %v", moved)
				}
			}
		})
	}
}

// Without a stored choice nothing touches the VM: the disk is read as it
// is (as before) and the page is told that a choice is needed.
func TestNoChoiceTouchesNoVMAndAsksForOne(t *testing.T) {
	m, _, target, _, run := vmJob(t, nil)
	m.BeginVMDisks = func(context.Context, string, VMDiskOptions) (VMDiskSession, error) {
		t.Error("a VM was held still without a choice of the job")
		return nil, errors.New("no")
	}
	state := run()
	if got := codes(state.Notices); !slices.Equal(got, []string{NoticeVMDiskChoiceNeeded + ":docs/ha/disk.qcow2"}) {
		t.Fatalf("notices %v", got)
	}
	if state.Warning != "" {
		t.Fatalf("warning %q", state.Warning)
	}
	if data, _ := os.ReadFile(filepath.Join(target, "backup", "docs", "ha", "disk.qcow2")); string(data) != "disk of the running VM" {
		t.Fatalf("disk not copied as before: %q", data)
	}
}

// "consistent" without Btrfs: the VM is held still for its disks only,
// after everything else, and always let go again.
func TestConsistentHoldsTheVMOnlyForItsDisks(t *testing.T) {
	consistent := func(j *Job) { j.VMDisks = &VMDisks{Mode: VMDisksConsistent, LibvirtSnapshot: true} }
	t.Run("snapshot with guest agent", func(t *testing.T) {
		m, _, target, log, run := vmJob(t, consistent)
		s := session(m, log, "libvirt_snapshot", true)
		state := run()
		if !slices.Equal(*log, []string{"recover", "begin ha pause=no snapshot=yes", "end ha"}) || s.ended != 1 {
			t.Fatalf("log %v, ended %d", *log, s.ended)
		}
		if state.Warning != "" || len(state.Notices) != 0 {
			t.Fatalf("a quiesced snapshot needs no word: %+v", state)
		}
		for _, name := range []string{"ha/disk.qcow2", "ha/notes.txt", "off.qcow2"} {
			if _, err := os.Stat(filepath.Join(target, "backup", "docs", name)); err != nil {
				t.Fatalf("%s not copied: %v", name, err)
			}
		}
	})
	t.Run("snapshot without guest agent", func(t *testing.T) {
		m, _, _, log, run := vmJob(t, consistent)
		session(m, log, "libvirt_snapshot", false)
		state := run()
		if got := codes(state.Notices); !slices.Equal(got, []string{NoticeVMDiskCrashConsistent + ":docs/ha/disk.qcow2"}) || state.Warning != "" {
			t.Fatalf("notices %v warning %q: crash-consistent is a notice, no error", got, state.Warning)
		}
	})
	t.Run("pause allowed", func(t *testing.T) {
		m, _, _, log, run := vmJob(t, func(j *Job) { j.VMDisks = &VMDisks{Mode: VMDisksConsistent, Raw: VMDisksRawPause} })
		session(m, log, VMDiskWayPause, false)
		state := run()
		if !slices.Contains(*log, "begin ha pause=yes snapshot=no") || !slices.Contains(*log, "end ha") {
			t.Fatalf("log %v", *log)
		}
		if got := codes(state.Notices); !slices.Equal(got, []string{NoticeVMDiskPaused + ":docs/ha/disk.qcow2"}) {
			t.Fatalf("notices %v", got)
		}
	})
	// Skipped means skipped: a raw image (or a qcow2 one the job allows no
	// libvirt snapshot for) without leave to pause is not read at all.
	for _, reason := range []string{ProblemVMDiskRawImage, ProblemVMDiskSnapshotUnproven} {
		t.Run("left out: "+reason, func(t *testing.T) {
			m, disk, target, _, run := vmJob(t, func(j *Job) { j.VMDisks = &VMDisks{Mode: VMDisksConsistent} })
			m.BeginVMDisks = func(context.Context, string, VMDiskOptions) (VMDiskSession, error) {
				return nil, codedError{reason}
			}
			if os.Geteuid() != 0 {
				tripwire(t, disk)
			}
			state := run()
			if state.Warning != "" || len(state.Problems) != 0 {
				t.Fatalf("leaving it out became a warning (or the disk was opened): %+v", state)
			}
			if got := codes(state.Notices); !slices.Equal(got, []string{NoticeVMDiskSkipped + ":docs/ha/disk.qcow2"}) {
				t.Fatalf("notices %v", got)
			}
			if _, err := os.Stat(filepath.Join(target, "backup", "docs", "ha", "disk.qcow2")); !os.IsNotExist(err) {
				t.Fatal("a disk that was to be left out was copied")
			}
		})
	}
	for _, code := range []string{ProblemVMDiskBackingChain, ProblemVMDiskPassthrough, ProblemVMDiskNoSpace, ProblemVMDiskBusy, ProblemVMDiskSnapshotFailed} {
		t.Run(code, func(t *testing.T) {
			m, disk, target, _, run := vmJob(t, consistent)
			m.BeginVMDisks = func(context.Context, string, VMDiskOptions) (VMDiskSession, error) { return nil, codedError{code} }
			if os.Geteuid() != 0 {
				tripwire(t, disk)
			}
			state := run()
			if got := codes(state.Problems); !slices.Equal(got, []string{code + ":docs/ha/disk.qcow2"}) || state.Skipped != 1 || state.Warning == "" {
				t.Fatalf("problems %v, skipped %d, warning %q", got, state.Skipped, state.Warning)
			}
			if _, err := os.Stat(filepath.Join(target, "backup", "docs", "ha", "disk.qcow2")); !os.IsNotExist(err) {
				t.Fatal("a disk that could not be held still was copied all the same")
			}
			if _, err := os.Stat(filepath.Join(target, "backup", "docs", "ha", "notes.txt")); err != nil {
				t.Fatal("the rest was not copied")
			}
		})
	}
}

// The copy fails half way, the run is cancelled, the pause runs out: the
// VM is let go in every case, and the earlier copy stays.
func TestVMIsAlwaysLetGo(t *testing.T) {
	consistent := func(j *Job) { j.VMDisks = &VMDisks{Mode: VMDisksConsistent, Raw: VMDisksRawPause} }
	t.Run("copy fails", func(t *testing.T) {
		m, disk, _, log, run := vmJob(t, consistent)
		s := session(m, log, "libvirt_snapshot", true)
		if os.Geteuid() == 0 {
			t.Skip("root reads every file")
		}
		tripwire(t, disk)
		state := run()
		if s.ended != 1 || !slices.Contains(*log, "end ha") {
			t.Fatalf("VM not let go after a failed copy: %v", *log)
		}
		if got := codes(state.Problems); !slices.Equal(got, []string{ProblemReadError + ":docs/ha/disk.qcow2"}) {
			t.Fatalf("problems %v", got)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		m, _, target, log, _ := vmJob(t, consistent)
		ctx, cancel := context.WithCancel(context.Background())
		s := &fakeSession{way: "libvirt_snapshot", log: log, vm: "ha"}
		m.BeginVMDisks = func(parent context.Context, vm string, _ VMDiskOptions) (VMDiskSession, error) {
			s.ctx, s.cancel = context.WithCancel(parent)
			cancel() // the person cancels while the VM is held
			return s, nil
		}
		root, _ := os.OpenRoot(target)
		defer root.Close()
		job := Job{ID: "copy", Sources: []string{"docs"}, Destination: Destination{Type: "usb-copy", Folder: "backup"}, VMDisks: &VMDisks{Mode: VMDisksConsistent}}
		if err := m.copyTo(ctx, job, root); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled run: %v", err)
		}
		if s.ended != 1 {
			t.Fatalf("VM let go %d times after a cancelled run", s.ended)
		}
	})
	t.Run("pause runs out", func(t *testing.T) {
		m, _, target, log, _ := vmJob(t, consistent)
		os.MkdirAll(filepath.Join(target, "backup", "docs", "ha"), 0o700)
		os.WriteFile(filepath.Join(target, "backup", "docs", "ha", "disk.qcow2"), []byte("copy of the run before"), 0o600)
		s := &fakeSession{way: VMDiskWayPause, log: log, vm: "ha"}
		m.BeginVMDisks = func(parent context.Context, vm string, _ VMDiskOptions) (VMDiskSession, error) {
			s.ctx, s.cancel = context.WithCancel(parent)
			s.stopped = ProblemVMDiskPauseTimeout // the limit was reached at once
			s.cancel()
			return s, nil
		}
		root, _ := os.OpenRoot(target)
		defer root.Close()
		job := Job{ID: "copy", Sources: []string{"docs"}, Destination: Destination{Type: "usb-copy", Folder: "backup"}, VMDisks: &VMDisks{Mode: VMDisksConsistent, Raw: VMDisksRawPause}}
		if err := m.copyTo(context.Background(), job, root); err != nil {
			t.Fatalf("a pause that ran out ended the whole run: %v", err)
		}
		state := m.states["copy"]
		if got := codes(state.Problems); !slices.Equal(got, []string{ProblemVMDiskPauseTimeout + ":docs/ha/disk.qcow2"}) || s.ended != 1 {
			t.Fatalf("problems %v, ended %d", got, s.ended)
		}
		if data, _ := os.ReadFile(filepath.Join(target, "backup", "docs", "ha", "disk.qcow2")); string(data) != "copy of the run before" {
			t.Fatalf("the copy of the run before was damaged: %q", data)
		}
	})
	t.Run("undo fails", func(t *testing.T) {
		m, _, _, log, run := vmJob(t, consistent)
		s := session(m, log, "libvirt_snapshot", true)
		s.endErr = codedError{ProblemVMDiskUndoFailed}
		state := run()
		if got := codes(state.Problems); !slices.Equal(got, []string{ProblemVMDiskUndoFailed + ":docs/ha/disk.qcow2"}) {
			t.Fatalf("problems %v", got)
		}
	})
}

func TestVMDisksSettingIsChecked(t *testing.T) {
	shares := []Source{{Name: "docs", Volume: "tank"}}
	base := Job{ID: "copy", Sources: []string{"docs"}, Every: "daily", Time: "03:00", Destination: Destination{Type: "usb-copy", DeviceID: "ABCD-1234", Folder: "backup"}}
	for name, c := range map[string]struct {
		set  *VMDisks
		kind string
		ok   bool
	}{
		"none":             {nil, "usb-copy", true},
		"skip":             {&VMDisks{Mode: "skip"}, "usb-copy", true},
		"consistent":       {&VMDisks{Mode: "consistent"}, "usb-copy", true},
		"consistent pause": {&VMDisks{Mode: "consistent", Raw: "pause"}, "usb-copy", true},
		"consistent skip":  {&VMDisks{Mode: "consistent", Raw: "skip"}, "usb-copy", true},
		"with libvirt":     {&VMDisks{Mode: "consistent", Raw: "pause", LibvirtSnapshot: true}, "usb-copy", true},
		"skip, libvirt":    {&VMDisks{Mode: "skip", LibvirtSnapshot: true}, "usb-copy", false},
		"unknown mode":     {&VMDisks{Mode: "always"}, "usb-copy", false},
		"unknown raw":      {&VMDisks{Mode: "consistent", Raw: "stop"}, "usb-copy", false},
		"skip with raw":    {&VMDisks{Mode: "skip", Raw: "pause"}, "usb-copy", false},
		"empty":            {&VMDisks{}, "usb-copy", false},
	} {
		job := base
		job.VMDisks, job.Destination.Type = c.set, c.kind
		if err := check(job, shares, []string{"tank"}); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// The preview for the job editor: every VM disk in the job's sources with
// the way a run would use now, as codes and values, without opening a file.
func TestPreviewVMDisks(t *testing.T) {
	mnt := t.TempDir()
	m := NewManager(config.Open(t.TempDir()))
	m.mountRoot = mnt
	m.mountsFile = filepath.Join(mnt, "mounts")
	os.WriteFile(m.mountsFile, []byte("x "+filepath.Join(mnt, "nvme")+" btrfs\nx "+filepath.Join(mnt, "tank")+" ext4\n"), 0o600)
	for _, dir := range []string{"nvme", "tank"} {
		os.MkdirAll(filepath.Join(mnt, dir), 0o755)
	}
	m.Sources = func() []Source {
		return []Source{{Name: "machines", Volume: "tank", Path: filepath.Join(mnt, "tank", "machines")}}
	}
	disks := []VMDiskInfo{
		{VM: "ha", File: filepath.Join(mnt, "nvme", "vms", "ha", "haos.qcow2"), Format: "qcow2", SizeBytes: 32 << 30, Running: true},
		{VM: "debian", File: filepath.Join(mnt, "tank", "machines", "debian", "vdisk1.qcow2"), Format: "qcow2", SizeBytes: 20 << 30, Running: true},
		{VM: "win", File: filepath.Join(mnt, "tank", "machines", "win", "c.raw"), Format: "raw", SizeBytes: 64 << 30, Running: true},
		{VM: "old", File: filepath.Join(mnt, "tank", "machines", "old", "vdisk1.qcow2"), Format: "qcow2", SizeBytes: 8 << 30},
		{VM: "chain", File: filepath.Join(mnt, "tank", "machines", "chain", "vdisk1.qcow2"), Format: "qcow2", Running: true},
		{VM: "tmp", File: filepath.Join(mnt, "tank", "machines", "tmp", "skip.qcow2"), Format: "qcow2", Running: true},
		{VM: "elsewhere", File: filepath.Join(mnt, "other", "vms", "x.qcow2"), Format: "qcow2", Running: true},
	}
	btrfs := func(volume string) bool { return volume == "nvme" }
	asked := map[string]int{}
	plan := func(_ context.Context, vm string, opts VMDiskOptions) VMDiskPlan {
		pause := opts.Pause
		asked[vm]++
		switch vm {
		case "debian":
			return VMDiskPlan{Way: VMDiskWaySnapshot, Agent: true}
		case "win":
			if pause {
				return VMDiskPlan{Way: VMDiskWayPause, Reason: ProblemVMDiskRawImage}
			}
			return VMDiskPlan{Reason: ProblemVMDiskRawImage}
		case "chain":
			return VMDiskPlan{Reason: ProblemVMDiskNoSpace, NeedBytes: 4 << 30, FreeBytes: 1 << 30}
		}
		t.Errorf("plan asked for %s", vm)
		return VMDiskPlan{}
	}
	job := Job{ID: "all", Sources: []string{"machines"}, Volumes: []string{"nvme"}, Skip: []string{"machines/tmp"}, Destination: Destination{Type: "usb-copy", Folder: "backup"}}
	ways := func(list []VMDiskPreview) map[string]string {
		out := map[string]string{}
		for _, d := range list {
			out[d.VM] = d.Way + "/" + d.Reason
		}
		return out
	}
	// No choice yet: nothing touches a VM, plan is not even asked.
	got := ways(m.PreviewVMDisks(context.Background(), job, disks, btrfs, plan))
	want := map[string]string{"ha": "btrfs_snapshot/", "debian": "as_is/", "win": "as_is/", "old": "copy/", "chain": "as_is/"}
	if len(got) != len(want) || len(asked) != 0 {
		t.Fatalf("no choice: %v, asked %v", got, asked)
	}
	for vm, way := range want {
		if got[vm] != way {
			t.Errorf("no choice, %s: %s, want %s", vm, got[vm], way)
		}
	}
	// Consistent, raw images skipped.
	job.VMDisks = &VMDisks{Mode: VMDisksConsistent}
	list := m.PreviewVMDisks(context.Background(), job, disks, btrfs, plan)
	got = ways(list)
	for vm, way := range map[string]string{"ha": "btrfs_snapshot/", "debian": "libvirt_snapshot/", "win": "skip/vm_disk_raw_image", "old": "copy/", "chain": "none/vm_disk_no_space"} {
		if got[vm] != way {
			t.Errorf("consistent, %s: %s, want %s", vm, got[vm], way)
		}
	}
	for _, d := range list {
		switch d.VM {
		case "ha":
			if d.Path != "Volume-nvme/vms/ha/haos.qcow2" || d.State != "running" || d.SizeBytes != 32<<30 || d.Format != "qcow2" {
				t.Errorf("%+v", d)
			}
		case "debian":
			if !d.Agent || d.Path != "machines/debian/vdisk1.qcow2" {
				t.Errorf("%+v", d)
			}
		case "chain":
			if d.NeedBytes != 4<<30 || d.FreeBytes != 1<<30 {
				t.Errorf("%+v", d)
			}
		case "old":
			if d.State != "stopped" {
				t.Errorf("%+v", d)
			}
		}
	}
	if asked["debian"] != 1 || asked["ha"] != 0 || asked["old"] != 0 {
		t.Fatalf("asked %v", asked)
	}
	// Consistent with leave to pause; and skip.
	job.VMDisks = &VMDisks{Mode: VMDisksConsistent, Raw: VMDisksRawPause}
	if got := ways(m.PreviewVMDisks(context.Background(), job, disks, btrfs, plan)); got["win"] != "pause/vm_disk_raw_image" {
		t.Errorf("pause: %v", got)
	}
	job.VMDisks = &VMDisks{Mode: VMDisksSkip}
	got = ways(m.PreviewVMDisks(context.Background(), job, disks, btrfs, plan))
	for vm, way := range map[string]string{"ha": "skip/", "debian": "skip/", "win": "skip/", "old": "copy/", "chain": "skip/"} {
		if got[vm] != way {
			t.Errorf("skip, %s: %s, want %s", vm, got[vm], way)
		}
	}
}

// A VM that is started while the run is under way: its disk was not live
// when the run began, but it is asked about again before it is opened.
func TestSkipAlsoHoldsForAVMStartedDuringTheRun(t *testing.T) {
	m, disk, target, _, run := vmJob(t, func(j *Job) { j.VMDisks = &VMDisks{Mode: VMDisksSkip} })
	m.LiveDisks = func() map[string]string { return nil } // nothing runs at the start
	m.VMDiskFiles = func() map[string]string { return map[string]string{disk: "ha"} }
	asked := 0
	m.VMRunning = func(vm string) bool {
		asked++
		return vm == "ha" // started since
	}
	if os.Geteuid() != 0 {
		tripwire(t, disk)
	}
	state := run()
	if asked == 0 {
		t.Fatal("the state of the VM was not asked for again")
	}
	if got := codes(state.Notices); !slices.Equal(got, []string{NoticeVMDiskSkipped + ":docs/ha/disk.qcow2"}) || state.Warning != "" {
		t.Fatalf("notices %v, warning %q: the disk of a VM started during the run was opened", got, state.Warning)
	}
	if _, err := os.Stat(filepath.Join(target, "backup", "docs", "ha", "disk.qcow2")); !os.IsNotExist(err) {
		t.Fatal("disk copied")
	}
	// Still off: copied like any file, and only disks are asked about.
	m.VMRunning = func(string) bool { return false }
	if os.Geteuid() != 0 {
		os.Chmod(disk, 0o600)
	}
	state = run()
	if len(state.Notices) != 0 {
		t.Fatalf("%+v", state.Notices)
	}
	if _, err := os.Stat(filepath.Join(target, "backup", "docs", "ha", "disk.qcow2")); err != nil {
		t.Fatal("disk of a stopped VM not copied")
	}
}

// The VM is let go early because the room for the overlay ran low: said
// with that code, the copy of the run before stays.
func TestLowSpaceDuringTheCopyIsReportedAsSuch(t *testing.T) {
	m, _, target, log, _ := vmJob(t, nil)
	os.MkdirAll(filepath.Join(target, "backup", "docs", "ha"), 0o700)
	os.WriteFile(filepath.Join(target, "backup", "docs", "ha", "disk.qcow2"), []byte("copy of the run before"), 0o600)
	s := &fakeSession{way: "libvirt_snapshot", log: log, vm: "ha"}
	m.BeginVMDisks = func(parent context.Context, vm string, _ VMDiskOptions) (VMDiskSession, error) {
		s.ctx, s.cancel = context.WithCancel(parent)
		s.stopped = ProblemVMDiskNoSpace
		s.cancel()
		return s, nil
	}
	root, _ := os.OpenRoot(target)
	defer root.Close()
	job := Job{ID: "copy", Sources: []string{"docs"}, Destination: Destination{Type: "usb-copy", Folder: "backup"}, VMDisks: &VMDisks{Mode: VMDisksConsistent, LibvirtSnapshot: true}}
	if err := m.copyTo(context.Background(), job, root); err != nil {
		t.Fatal(err)
	}
	state := m.states["copy"]
	if got := codes(state.Problems); !slices.Equal(got, []string{ProblemVMDiskNoSpace + ":docs/ha/disk.qcow2"}) || s.ended != 1 {
		t.Fatalf("problems %v, ended %d", got, s.ended)
	}
	if data, _ := os.ReadFile(filepath.Join(target, "backup", "docs", "ha", "disk.qcow2")); string(data) != "copy of the run before" {
		t.Fatalf("the copy of the run before was damaged: %q", data)
	}
}
