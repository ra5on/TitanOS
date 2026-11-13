package vm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// diskRunner plays libvirt and qemu-img for the disks of one VM: it knows
// whether the guest writes to an overlay and whether it is paused.
type diskRunner struct {
	*fakeRunner
	mu      sync.Mutex
	agent   bool
	backing map[string]bool   // images with a backing file of their own
	overlay map[string]string // target → overlay the guest writes to
	paused  bool
	failing string // a command containing this fails
	lines   []string
	// job is the block job QEMU runs per target: its percentage; pivot of
	// a job below 100 fails. commitFails makes blockcommit itself fail
	// (as if cut off) while leaving the job behind.
	job         map[string]int
	commitFails bool
	// onSnapshot runs when a snapshot is asked for, before it is taken.
	onSnapshot func(ctx context.Context)
	// lateSnapshot takes the snapshot although the command reports failure.
	lateSnapshot bool
	ioError      bool
	// backingOf names the image an overlay builds on (qemu-img info).
	backingOf func(file string) string
}

func (r *diskRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	line := name + " " + strings.Join(args, " ")
	r.lines = append(r.lines, line)
	if r.failing != "" && strings.Contains(line, r.failing) {
		return "", errors.New("failed: " + line)
	}
	has := func(word string) bool { return slices.Contains(args, word) }
	switch {
	case name == "qemu-img" && args[0] == "info":
		backing := ""
		if r.backing[args[len(args)-1]] {
			name := "base.qcow2"
			if r.backingOf != nil {
				name = r.backingOf(args[len(args)-1])
			}
			backing = `, "backing-filename": "` + name + `"`
		}
		return fmt.Sprintf(`{"virtual-size": %d, "format": "qcow2"%s}`, 20*gib, backing), nil
	case name == "qemu-img" && args[0] == "commit":
		return "", nil
	case name == "virsh" && has("domstate"):
		if r.paused && r.ioError {
			return "paused (I/O error)\n", nil
		}
		if r.paused {
			return "paused\n", nil
		}
		return "running\n", nil
	case name == "virsh" && has("qemu-agent-command"):
		if !r.agent {
			return "", errors.New("guest agent is not connected")
		}
		return `{"return":{}}`, nil
	case name == "virsh" && has("snapshot-create-as"):
		if has("--quiesce") && !r.agent {
			return "", errors.New("guest agent is not connected")
		}
		if r.onSnapshot != nil {
			r.mu.Unlock()
			r.onSnapshot(ctx)
			r.mu.Lock()
		}
		if err := ctx.Err(); err != nil && !r.lateSnapshot {
			return "", err
		}
		defer func() {
			if r.lateSnapshot {
				r.lateSnapshot = false
			}
		}()
		for i, arg := range args {
			if arg == "--diskspec" {
				target, rest, _ := strings.Cut(args[i+1], ",")
				file := rest[strings.Index(rest, "file=")+5:]
				if err := os.WriteFile(file, []byte("overlay"), 0o600); err != nil {
					return "", err
				}
				r.overlay[target] = file
			}
		}
		if r.lateSnapshot {
			return "", errors.New("virsh was cut off")
		}
		return "", nil
	case name == "virsh" && has("domblklist"):
		out := ""
		for target, file := range r.overlay {
			out += " " + target + "   " + file + "\n"
		}
		return out, nil
	case name == "virsh" && has("blockcommit"):
		if _, running := r.job[args[3]]; running {
			return "", errors.New("error: block copy still active: disk already in active block job")
		}
		if r.commitFails {
			r.job[args[3]] = 40
			return "", errors.New("virsh was cut off")
		}
		delete(r.overlay, args[3])
		return "", nil
	case name == "virsh" && has("blockjob") && has("--info"):
		percent, running := r.job[args[2]]
		if !running || !has("--raw") {
			return "", nil // the plain form goes to stderr; raw prints nothing without a job
		}
		r.job[args[2]] = min(100, percent+30) // it goes on in QEMU
		return fmt.Sprintf(" type=4\n bandwidth=0\n cur=%d\n end=100\n", percent), nil
	case name == "virsh" && has("blockjob") && has("--pivot"):
		if r.job[args[3]] < 100 {
			return "", errors.New("error: block job not ready for pivot yet")
		}
		delete(r.job, args[3])
		delete(r.overlay, args[3])
		return "", nil
	case name == "virsh" && has("suspend"):
		r.paused = true
		return "", nil
	case name == "virsh" && has("resume"):
		r.paused = false
		return "", nil
	}
	return r.fakeRunner.Run(ctx, stdin, name, args...)
}

func (r *diskRunner) ran(part string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, line := range r.lines {
		if strings.Contains(line, part) {
			n++
		}
	}
	return n
}

// diskFixture is a running VM "ha" with two disks on the volume "fast".
func diskFixture(t *testing.T, formats ...string) (*Manager, *diskRunner, VM) {
	t.Helper()
	m, base := newTestManager(t)
	r := &diskRunner{fakeRunner: base, agent: true, backing: map[string]bool{}, overlay: map[string]string{}, job: map[string]int{}}
	m.runner = r
	if len(formats) == 0 {
		formats = []string{"qcow2", "qcow2"}
	}
	vm := VM{Name: "ha", UUID: "11111111-2222-3333-4444-555555555555", Volume: "fast", Template: "linux"}
	for i, format := range formats {
		vm.Disks = append(vm.Disks, Disk{File: fmt.Sprintf("vdisk%d.%s", i+1, format), Format: format, SizeBytes: 20 * gib, Volume: "fast"})
	}
	dir := filepath.Join(m.mountRoot, "fast", "vms", "ha")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, disk := range vm.Disks {
		os.WriteFile(filepath.Join(dir, disk.File), []byte("image"), 0o600)
	}
	m.mu.Lock()
	m.vms["ha"] = vm
	m.mu.Unlock()
	running(t, m, "ha")
	free, poll := diskFree, blockJobPoll
	diskFree = func(string) (uint64, error) { return 100 * gib, nil }
	blockJobPoll = time.Millisecond
	t.Cleanup(func() { diskFree, blockJobPoll = free, poll })
	return m, r, vm
}

// abandon forgets a running backup as a restart of titand would: nothing
// is undone, the record and the overlays stay.
func abandon(m *Manager, b *DiskBackup) {
	if b.timer != nil {
		b.timer.Stop()
	}
	b.cancel()
	if b.watching != nil {
		<-b.watching
	}
	b.lock.Unlock()
	m.mu.Lock()
	m.diskBackups, m.diskBackupChecked = nil, nil
	m.mu.Unlock()
}

func record(m *Manager, vm VM) string {
	return filepath.Join(m.mountRoot, vm.Volume, ".titanos", diskBackupFeature, vm.UUID+".json")
}

func overlays(m *Manager) []string {
	found, _ := filepath.Glob(filepath.Join(m.mountRoot, "fast", "vms", "ha", "*"+OverlaySuffix))
	return found
}

func code(err error) string {
	var problem *DiskBackupError
	if errors.As(err, &problem) {
		return problem.Code
	}
	return fmt.Sprint(err)
}

func TestDiskBackupSnapshotWithAndWithoutGuestAgent(t *testing.T) {
	for _, agent := range []bool{true, false} {
		t.Run(fmt.Sprintf("agent=%v", agent), func(t *testing.T) {
			m, r, vm := diskFixture(t)
			r.agent = agent
			ctx := context.Background()
			b, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true})
			if err != nil {
				t.Fatal(err)
			}
			if b.Way != DiskWaySnapshot || b.Quiesced != agent {
				t.Fatalf("way %q quiesced %v", b.Way, b.Quiesced)
			}
			// On the disk before libvirt was asked, with both overlays.
			data, err := os.ReadFile(record(m, vm))
			if err != nil || !strings.Contains(string(data), `"vda"`) || !strings.Contains(string(data), `"vdb"`) {
				t.Fatalf("record: %s %v", data, err)
			}
			if got := overlays(m); len(got) != 2 || len(r.overlay) != 2 {
				t.Fatalf("overlays %v, in use %v", got, r.overlay)
			}
			if want := "--disk-only --atomic --no-metadata --reuse-external"; r.ran(want) == 0 {
				t.Fatalf("snapshot without %q: %v", want, r.lines)
			}
			if (r.ran("--quiesce") > 0) != agent {
				t.Fatalf("quiesce asked %d times with agent=%v", r.ran("--quiesce"), agent)
			}
			// The VM is held: no other operation, also not a second backup.
			if _, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true}); code(err) != DiskBusy {
				t.Fatalf("second backup of the same VM: %v", err)
			}
			if err := m.Action(ctx, "ha", "reboot"); !errors.Is(err, ErrSnapshotBusy) {
				t.Fatalf("restart during the backup: %v", err)
			}
			if err := b.End(); err != nil {
				t.Fatal(err)
			}
			if err := b.End(); err != nil { // harmless
				t.Fatal(err)
			}
			if r.ran("blockcommit ha vda --active --wait --pivot") != 1 || r.ran("blockcommit ha vdb --active --wait --pivot") != 1 {
				t.Fatalf("overlays not committed back once each: %v", r.lines)
			}
			if len(overlays(m)) != 0 || len(r.overlay) != 0 {
				t.Fatalf("VM left on an overlay: files %v, in use %v", overlays(m), r.overlay)
			}
			if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
				t.Fatal("record left after a clean end")
			}
			if err := m.Action(ctx, "ha", "reboot"); err != nil {
				t.Fatalf("VM still locked after the backup: %v", err)
			}
		})
	}
}

// A guest that does not freeze although its agent answers: taken without,
// and said so.
func TestDiskBackupFallsBackWhenQuiesceFails(t *testing.T) {
	m, r, _ := diskFixture(t)
	r.failing = "--quiesce"
	b, err := m.BeginDiskBackup(context.Background(), "ha", DiskBackupOptions{Snapshot: true})
	if err != nil || b.Quiesced || len(r.overlay) != 2 {
		t.Fatalf("%v quiesced=%v overlays=%v", err, b != nil && b.Quiesced, r.overlay)
	}
	if err := b.End(); err != nil || len(overlays(m)) != 0 {
		t.Fatalf("%v %v", err, overlays(m))
	}
}

func TestDiskBackupRefusesWhatItMustNotTouch(t *testing.T) {
	ctx := context.Background()
	check := func(t *testing.T, m *Manager, r *diskRunner, vm VM, pause bool, want string) {
		t.Helper()
		if plan := m.PlanDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true, Pause: pause}); plan.Way != "" || plan.Reason != want {
			t.Fatalf("plan %+v, want reason %s", plan, want)
		}
		if _, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true, Pause: pause}); code(err) != want {
			t.Fatalf("begin: %v, want %s", err, want)
		}
		if r.ran("snapshot-create-as") != 0 || r.ran("suspend") != 0 || len(overlays(m)) != 0 {
			t.Fatalf("the VM was touched: %v", r.lines)
		}
		if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
			t.Fatal("record left although nothing was started")
		}
		if err := m.Action(ctx, "ha", "reboot"); err != nil {
			t.Fatalf("VM left locked: %v", err)
		}
	}
	t.Run("own snapshots", func(t *testing.T) {
		m, r, vm := diskFixture(t)
		r.backing[m.diskPath(vm, vm.Disks[1])] = true
		check(t, m, r, vm, true, DiskBackingChain) // also not paused
	})
	t.Run("whole disk of the NAS", func(t *testing.T) {
		m, r, vm := diskFixture(t)
		vm.Disks = append(vm.Disks, Disk{Path: "/dev/disk/by-id/ata-WHOLE", Format: "raw"})
		m.mu.Lock()
		m.vms["ha"] = vm
		m.mu.Unlock()
		check(t, m, r, vm, true, DiskPassthrough)
	})
	t.Run("not enough space", func(t *testing.T) {
		m, r, vm := diskFixture(t)
		diskFree = func(string) (uint64, error) { return 5 * gib, nil } // two disks need 4 GiB each
		check(t, m, r, vm, true, DiskNoSpace)
		if plan := m.PlanDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true}); plan.NeedBytes != 8*gib || plan.FreeBytes != 5*gib {
			t.Fatalf("plan %+v", plan)
		}
	})
	t.Run("raw image, skip", func(t *testing.T) {
		m, r, vm := diskFixture(t, "qcow2", "raw")
		check(t, m, r, vm, false, DiskRawImage)
	})
	t.Run("another operation runs", func(t *testing.T) {
		m, r, vm := diskFixture(t)
		lock, err := m.operation("ha")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true, Pause: true}); code(err) != DiskBusy {
			t.Fatalf("%v", err)
		}
		lock.Unlock()
		m.mu.Lock()
		m.snapshotJobs["ha"] = &Job{State: "running", Operation: "snapshot"}
		m.mu.Unlock()
		check(t, m, r, vm, true, DiskBusy)
	})
	t.Run("paused by its owner", func(t *testing.T) {
		m, r, vm := diskFixture(t)
		r.paused = true
		if _, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true, Pause: true}); code(err) != DiskBusy {
			t.Fatalf("%v", err)
		}
		if !r.paused || r.ran("resume") != 0 {
			t.Fatal("a VM its owner paused was resumed")
		}
		if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
			t.Fatal("record left")
		}
	})
	t.Run("stopped", func(t *testing.T) {
		m, _, _ := diskFixture(t)
		os.Remove(filepath.Join(m.pidDir, "ha.pid"))
		if _, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true, Pause: true}); code(err) != DiskBusy {
			t.Fatalf("%v", err)
		}
	})
}

func TestDiskBackupSnapshotThatFailsChangesNothing(t *testing.T) {
	m, r, vm := diskFixture(t)
	r.failing = "snapshot-create-as"
	if _, err := m.BeginDiskBackup(context.Background(), "ha", DiskBackupOptions{Snapshot: true}); code(err) != DiskSnapshotFailed {
		t.Fatalf("%v", err)
	}
	if len(overlays(m)) != 0 || r.ran("blockcommit") != 0 {
		t.Fatalf("something left or committed: %v %v", overlays(m), r.lines)
	}
	if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
		t.Fatal("record left after a snapshot that never happened")
	}
	if err := m.Action(context.Background(), "ha", "reboot"); err != nil {
		t.Fatalf("VM left locked: %v", err)
	}
}

// Raw images with leave to pause: paused, and resumed whatever happens.
func TestDiskBackupPauseResumesAlways(t *testing.T) {
	ctx := context.Background()
	t.Run("end", func(t *testing.T) {
		m, r, vm := diskFixture(t, "raw")
		b, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true, Pause: true})
		if err != nil || b.Way != DiskWayPause || !r.paused {
			t.Fatalf("%v way=%v paused=%v", err, b != nil && b.Way == DiskWayPause, r.paused)
		}
		if _, err := os.Stat(record(m, vm)); err != nil {
			t.Fatal("no record while the VM is paused")
		}
		if err := b.End(); err != nil || r.paused {
			t.Fatalf("not resumed: %v", err)
		}
		if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
			t.Fatal("record left")
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		m, r, _ := diskFixture(t, "raw")
		run, cancel := context.WithCancel(ctx)
		b, err := m.BeginDiskBackup(run, "ha", DiskBackupOptions{Snapshot: true, Pause: true})
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		if b.Context().Err() == nil {
			t.Fatal("the copy is not told to stop")
		}
		if err := b.End(); err != nil || r.paused {
			t.Fatalf("not resumed after a cancelled run: %v", err)
		}
	})
	t.Run("time limit", func(t *testing.T) {
		limit := pauseLimit
		pauseLimit = 50 * time.Millisecond
		t.Cleanup(func() { pauseLimit = limit })
		m, r, vm := diskFixture(t, "raw")
		b, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true, Pause: true})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-b.Context().Done():
		case <-time.After(5 * time.Second):
			t.Fatal("the pause has no upper bound")
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			r.mu.Lock()
			paused := r.paused
			r.mu.Unlock()
			if !paused {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if r.paused || b.Stopped() != DiskPauseTimeout {
			t.Fatalf("VM still paused after the limit (stopped=%q)", b.Stopped())
		}
		if err := b.End(); err != nil {
			t.Fatal(err)
		}
		if r.ran("resume") != 1 {
			t.Fatalf("resumed %d times", r.ran("resume"))
		}
		if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
			t.Fatal("record left")
		}
	})
	t.Run("suspend fails", func(t *testing.T) {
		m, r, vm := diskFixture(t, "raw")
		r.failing = "suspend"
		if _, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true, Pause: true}); code(err) != DiskPauseFailed {
			t.Fatalf("%v", err)
		}
		if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
			t.Fatal("record left")
		}
	})
}

// titand restarts (or the power goes) while a VM runs on an overlay or is
// paused: the record brings it back, at the start, before a run and
// before the VM is started. Only what the record names is touched.
func TestDiskBackupLeftoversAreUndone(t *testing.T) {
	ctx := context.Background()
	// leave begins a backup and forgets it, as a restart of titand would.
	leave := func(t *testing.T, m *Manager, pause bool) {
		t.Helper()
		b, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true, Pause: pause})
		if err != nil {
			t.Fatal(err)
		}
		abandon(m, b)
	}
	t.Run("overlay, VM still running", func(t *testing.T) {
		m, r, vm := diskFixture(t)
		leave(t, m, false)
		// A snapshot of the user's own next to it is never touched.
		own := filepath.Join(m.mountRoot, "fast", "vms", "ha", "vdisk1.qcow2.mine.qcow2")
		os.WriteFile(own, []byte("the user's own overlay"), 0o600)
		if err := m.RecoverDiskBackups(ctx, false); err != nil {
			t.Fatal(err)
		}
		if r.ran("blockcommit") != 2 || len(r.overlay) != 0 || len(overlays(m)) != 0 {
			t.Fatalf("leftover overlay not committed back: %v", r.lines)
		}
		if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
			t.Fatal("record left")
		}
		if data, _ := os.ReadFile(own); string(data) != "the user's own overlay" {
			t.Fatal("a file the record does not name was touched")
		}
		// Once per start: the volume is not looked through again.
		before := len(r.lines)
		os.WriteFile(record(m, vm), []byte("{}"), 0o600)
		if err := m.RecoverDiskBackups(ctx, false); err != nil || len(r.lines) != before {
			t.Fatalf("looked again: %v", err)
		}
		os.Remove(record(m, vm))
	})
	t.Run("overlay, VM stopped (NAS went down)", func(t *testing.T) {
		m, r, vm := diskFixture(t)
		leave(t, m, false)
		os.Remove(filepath.Join(m.pidDir, "ha.pid"))
		r.overlay = map[string]string{}
		// Starting the VM first commits what the guest wrote to the overlay.
		m.kvm = m.mountRoot
		err := m.Start(ctx, "ha", []string{"data", "fast"})
		if r.ran("qemu-img commit -d -f qcow2") != 2 {
			t.Fatalf("overlay not committed before the start (%v): %v", err, r.lines)
		}
		commit, create := -1, -1
		for i, line := range r.lines {
			if strings.Contains(line, "qemu-img commit") && commit < 0 {
				commit = i
			}
			if strings.Contains(line, "virsh --quiet create") {
				create = i
			}
		}
		if create >= 0 && create < commit {
			t.Fatal("the VM was started from the image before the overlay was committed")
		}
		if len(overlays(m)) != 0 {
			t.Fatalf("overlay left: %v", overlays(m))
		}
		if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
			t.Fatal("record left")
		}
	})
	t.Run("overlay cannot be committed: the VM does not start", func(t *testing.T) {
		m, r, vm := diskFixture(t)
		leave(t, m, false)
		os.Remove(filepath.Join(m.pidDir, "ha.pid"))
		r.failing = "qemu-img commit"
		if err := m.Start(ctx, "ha", []string{"data", "fast"}); !errors.Is(err, ErrSnapshotRecovery) {
			t.Fatalf("start with an overlay that cannot be committed: %v", err)
		}
		if r.ran("virsh --quiet create") != 0 {
			t.Fatal("the VM was started from the image without the overlay's data")
		}
		if _, err := os.Stat(record(m, vm)); err != nil || len(overlays(m)) != 2 {
			t.Fatal("record or overlay lost although the undo failed")
		}
	})
	t.Run("VM left paused", func(t *testing.T) {
		m, r, vm := diskFixture(t, "raw")
		leave(t, m, true)
		if !r.paused {
			t.Fatal("fixture: not paused")
		}
		if err := m.RecoverDiskBackups(ctx, true); err != nil || r.paused {
			t.Fatalf("VM left paused: %v", err)
		}
		if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
			t.Fatal("record left")
		}
	})
	t.Run("undo fails: record stays, tried again", func(t *testing.T) {
		m, r, vm := diskFixture(t)
		b, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true})
		if err != nil {
			t.Fatal(err)
		}
		r.failing = "blockcommit"
		if err := b.End(); code(err) != DiskUndoFailed {
			t.Fatalf("%v", err)
		}
		if _, err := os.Stat(record(m, vm)); err != nil || len(overlays(m)) != 2 {
			t.Fatal("record or overlay removed although the VM still runs on the overlay")
		}
		r.failing = ""
		if err := m.RecoverDiskBackups(ctx, true); err != nil || len(overlays(m)) != 0 || len(r.overlay) != 0 {
			t.Fatalf("not undone at the next run: %v", err)
		}
	})
	t.Run("a running backup is not undone under it", func(t *testing.T) {
		m, r, _ := diskFixture(t)
		b, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true})
		if err != nil {
			t.Fatal(err)
		}
		_ = m.RecoverDiskBackups(ctx, true) // the VM is busy: left for later
		if r.ran("blockcommit") != 0 || len(overlays(m)) != 2 {
			t.Fatal("recovery pulled the overlay from under a running backup")
		}
		if err := b.End(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestFileDisksAndOverlayReserve(t *testing.T) {
	m, _, vm := diskFixture(t, "qcow2", "raw")
	disks := m.FileDisks()
	if len(disks) != 2 || !disks[0].Running || disks[0].VM != "ha" || disks[0].Path != m.diskPath(vm, vm.Disks[0]) || disks[1].Format != "raw" {
		t.Fatalf("%+v", disks)
	}
	for size, want := range map[int64]uint64{0: 4 * gib, 2 * gib: 2 * gib, 20 * gib: 4 * gib, 100 * gib: 10 * gib} {
		if got := overlayReserve(size); got != want {
			t.Errorf("reserve for %d: %d, want %d", size, got, want)
		}
	}
	if targets := diskTargets(VM{Template: "windows", Disks: make([]Disk, 2)}); !slices.Equal(targets, []string{"sda", "sdb"}) {
		t.Fatalf("targets %v", targets)
	}
}

// Shutting the VM down comes before its backup: the backup is told to let
// go, undoes the overlay, and the VM is then asked to shut down.
func TestStoppingAVMEndsItsDiskBackup(t *testing.T) {
	m, r, vm := diskFixture(t)
	ctx := context.Background()
	b, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true})
	if err != nil {
		t.Fatal(err)
	}
	// The copy: stops as soon as it is told, then lets go, as backup does.
	done := make(chan struct{})
	go func() {
		<-b.Context().Done()
		b.End()
		close(done)
	}()
	if err := m.Action(ctx, "ha", "shutdown"); err != nil {
		t.Fatalf("shutdown during a backup: %v", err)
	}
	<-done
	commit, shutdown := -1, -1
	for i, line := range r.lines {
		if strings.Contains(line, "blockcommit") && commit < 0 {
			commit = i
		}
		if strings.Contains(line, "virsh --quiet shutdown ha") {
			shutdown = i
		}
	}
	if commit < 0 || shutdown < commit {
		t.Fatalf("the VM was shut down while still on the overlay: %v", r.lines)
	}
	if len(overlays(m)) != 0 {
		t.Fatal("overlay left")
	}
	if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
		t.Fatal("record left")
	}
}

// A backup cancelled while libvirt takes the snapshot: virsh is not
// killed with the backup (libvirt would finish the snapshot all the same
// and nobody would know), and the VM comes back off the overlay.
func TestCancelDuringSnapshotLeavesNoUnknownOverlay(t *testing.T) {
	m, r, vm := diskFixture(t)
	run, cancel := context.WithCancel(context.Background())
	var during error
	r.onSnapshot = func(ctx context.Context) {
		cancel() // the person cancels the backup right now
		during = ctx.Err()
	}
	b, err := m.BeginDiskBackup(run, "ha", DiskBackupOptions{Snapshot: true})
	if err != nil {
		t.Fatalf("the snapshot was given up with the backup: %v", err)
	}
	if during != nil {
		t.Fatalf("virsh ran on the context of the backup and would have been killed: %v", during)
	}
	if len(r.overlay) != 2 || b.Context().Err() == nil {
		t.Fatalf("overlays %v, copy told to stop: %v", r.overlay, b.Context().Err() != nil)
	}
	if err := b.End(); err != nil {
		t.Fatal(err)
	}
	if len(r.overlay) != 0 || len(overlays(m)) != 0 || r.ran("blockcommit") != 2 {
		t.Fatalf("VM left on an overlay after a cancelled backup: %v %v", r.overlay, r.lines)
	}
	if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
		t.Fatal("record left")
	}
}

// The snapshot command is cut off by its own time limit while libvirt
// still takes the snapshot: nothing is decided. The record stays and no
// overlay is removed until libvirt's block chain says what the VM uses.
func TestSnapshotThatWasCutOffIsNotDecidedEarly(t *testing.T) {
	limit, settle := snapshotLimit, snapshotSettle
	snapshotLimit, snapshotSettle = 50*time.Millisecond, time.Hour
	t.Cleanup(func() { snapshotLimit, snapshotSettle = limit, settle })
	ctx := context.Background()

	t.Run("the VM ended up on the overlay", func(t *testing.T) {
		m, r, vm := diskFixture(t)
		r.agent = false
		r.onSnapshot = func(ctx context.Context) { <-ctx.Done() } // virsh hangs until it is cut off
		r.lateSnapshot = true                                     // libvirt takes it all the same
		_, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true})
		// libvirt did switch: the undo sees it in the block chain and
		// commits back at once.
		if code(err) != DiskSnapshotFailed {
			t.Fatalf("%v", err)
		}
		if len(r.overlay) != 0 || len(overlays(m)) != 0 {
			t.Fatalf("VM left on the overlay: %v", r.overlay)
		}
		if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
			t.Fatal("record left although the VM is back on its image")
		}
	})
	t.Run("not on the overlay yet", func(t *testing.T) {
		m, r, vm := diskFixture(t)
		r.agent = false
		r.onSnapshot = func(ctx context.Context) { <-ctx.Done() }
		_, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true})
		if code(err) != DiskUndoFailed {
			t.Fatalf("an undecided snapshot was reported as %v", err)
		}
		if _, err := os.Stat(record(m, vm)); err != nil {
			t.Fatal("record removed although libvirt may still take the snapshot")
		}
		// libvirt finishes the snapshot after all.
		dir := filepath.Join(m.mountRoot, "fast", "vms", "ha")
		entries, _ := os.ReadFile(record(m, vm))
		for _, target := range []string{"vda", "vdb"} {
			index := strings.Index(string(entries), `"target":"`+target+`"`)
			rest := string(entries)[index:]
			rest = rest[strings.Index(rest, `"overlay":"`)+11:]
			file := rest[:strings.Index(rest, `"`)]
			if filepath.Dir(file) != dir {
				t.Fatalf("overlay %q not next to the disk", file)
			}
			os.WriteFile(file, []byte("overlay"), 0o600)
			r.overlay[target] = file
		}
		if err := m.RecoverDiskBackups(ctx, true); err != nil {
			t.Fatal(err)
		}
		if len(r.overlay) != 0 || len(overlays(m)) != 0 || r.ran("blockcommit") != 2 {
			t.Fatalf("late snapshot not committed back: %v", r.lines)
		}
	})
	t.Run("never taken: decided once the record is old enough", func(t *testing.T) {
		m, r, vm := diskFixture(t)
		r.agent = false
		r.onSnapshot = func(ctx context.Context) { <-ctx.Done() }
		if _, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true}); code(err) != DiskUndoFailed {
			t.Fatalf("%v", err)
		}
		if err := m.RecoverDiskBackups(ctx, true); err == nil {
			t.Fatal("decided although the record is young")
		}
		if _, err := os.Stat(record(m, vm)); err != nil {
			t.Fatal("record removed early")
		}
		snapshotSettle = 0
		if err := m.RecoverDiskBackups(ctx, true); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
			t.Fatal("record left for good")
		}
		if r.ran("blockcommit") != 0 {
			t.Fatal("something was committed although the VM never used an overlay")
		}
	})
}

// A commit that was cut off (titand restarted, the time limit) leaves its
// block job in QEMU. It is not started a second time, which QEMU refuses:
// it is waited for and then finished.
func TestInterruptedBlockCommitIsTakenUpAgain(t *testing.T) {
	m, r, vm := diskFixture(t)
	ctx := context.Background()
	b, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true})
	if err != nil {
		t.Fatal(err)
	}
	// The commit of the first disk is cut off at 40 %: the job runs on.
	r.commitFails = true
	limit := diskUndoLimit
	if err := b.End(); err != nil {
		t.Fatalf("a job that runs on in QEMU was not waited for: %v", err)
	}
	diskUndoLimit = limit
	if r.ran("blockjob ha vda --pivot") != 1 || r.ran("blockjob ha vdb --pivot") != 1 {
		t.Fatalf("job not finished with a pivot: %v", r.lines)
	}
	if r.ran("blockcommit ha vda") != 1 {
		t.Fatalf("blockcommit started %d times for one disk", r.ran("blockcommit ha vda"))
	}
	if len(r.overlay) != 0 || len(overlays(m)) != 0 {
		t.Fatalf("VM left on the overlay: %v", r.overlay)
	}
	// After a restart of titand: the job is found running.
	b, err = m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true})
	if err != nil {
		t.Fatal(err)
	}
	r.commitFails = false
	r.mu.Lock()
	r.job["vda"] = 10 // left by the titand before
	r.mu.Unlock()
	abandon(m, b)
	r.lines = nil
	if err := m.RecoverDiskBackups(ctx, false); err != nil {
		t.Fatal(err)
	}
	if r.ran("blockcommit ha vda") != 0 || r.ran("blockjob ha vda --pivot") != 1 || r.ran("blockjob ha vda --abort") != 0 {
		t.Fatalf("a running job was started again or aborted: %v", r.lines)
	}
	if len(r.overlay) != 0 || len(overlays(m)) != 0 {
		t.Fatalf("VM left on the overlay: %v", r.overlay)
	}
	if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
		t.Fatal("record left")
	}
}

// The overlay is never removed while libvirt still names it, whatever the
// commands said.
func TestOverlayOfARunningVMIsNeverRemoved(t *testing.T) {
	m, r, vm := diskFixture(t)
	b, err := m.BeginDiskBackup(context.Background(), "ha", DiskBackupOptions{Snapshot: true})
	if err != nil {
		t.Fatal(err)
	}
	r.failing = "blockcommit ha vdb"
	if err := b.End(); code(err) != DiskUndoFailed {
		t.Fatalf("%v", err)
	}
	if got := overlays(m); len(got) != 1 || r.overlay["vdb"] != got[0] {
		t.Fatalf("overlays on disk %v, in use %v", got, r.overlay)
	}
	if _, err := os.Stat(record(m, vm)); err != nil {
		t.Fatal("record removed although the VM still runs on an overlay")
	}
}

// The guest writes more than was reserved: before the volume fills up the
// copy is stopped and the overlay committed back; a VM QEMU paused for the
// full disk goes on afterwards.
func TestLowSpaceStopsTheCopyAndBringsTheVMBack(t *testing.T) {
	watch := spaceWatch
	spaceWatch = 10 * time.Millisecond
	t.Cleanup(func() { spaceWatch = watch })
	m, r, vm := diskFixture(t)
	var mu sync.Mutex
	free := uint64(100 * gib)
	diskFree = func(string) (uint64, error) { mu.Lock(); defer mu.Unlock(); return free, nil }
	b, err := m.BeginDiskBackup(context.Background(), "ha", DiskBackupOptions{Snapshot: true})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-b.Context().Done():
		t.Fatal("copy stopped although there is room")
	case <-time.After(60 * time.Millisecond):
	}
	mu.Lock()
	free = 200 << 20
	mu.Unlock()
	select {
	case <-b.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the copy was not stopped when the room ran low")
	}
	if b.Stopped() != DiskNoSpace {
		t.Fatalf("stopped for %q", b.Stopped())
	}
	// QEMU paused the VM for the full disk meanwhile.
	r.mu.Lock()
	r.paused, r.ioError = true, true
	r.mu.Unlock()
	if err := b.End(); err != nil {
		t.Fatal(err)
	}
	if len(r.overlay) != 0 || len(overlays(m)) != 0 {
		t.Fatal("overlay not committed back")
	}
	if r.paused {
		t.Fatal("VM left paused after the space was back")
	}
	if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
		t.Fatal("record left")
	}
	// A VM paused for another reason is not resumed by a backup that
	// never ran short of space.
	mu.Lock()
	free = 100 * gib
	mu.Unlock()
	b, err = m.BeginDiskBackup(context.Background(), "ha", DiskBackupOptions{Snapshot: true})
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.paused, r.ioError = true, true
	r.mu.Unlock()
	resumes := r.ran("resume")
	if err := b.End(); err != nil {
		t.Fatal(err)
	}
	if r.ran("resume") != resumes {
		t.Fatal("a VM paused by a disk error was resumed by the backup")
	}
}

// Every operation on the disks of a VM first undoes what a backup left;
// an unusable record never blocks the VM for good.
func TestLeftoverRecordAndDiskOperations(t *testing.T) {
	ctx := context.Background()
	leave := func(t *testing.T, m *Manager) {
		t.Helper()
		b, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true})
		if err != nil {
			t.Fatal(err)
		}
		abandon(m, b)
	}
	stop := func(m *Manager, r *diskRunner) {
		os.Remove(filepath.Join(m.pidDir, "ha.pid"))
		r.mu.Lock()
		r.overlay = map[string]string{}
		r.mu.Unlock()
	}
	t.Run("grow commits the overlay first", func(t *testing.T) {
		m, r, vm := diskFixture(t)
		leave(t, m)
		stop(m, r)
		_ = m.Grow(ctx, "ha", 0, 40*gib) // whatever it says about the size
		commit, resize := -1, -1
		for i, line := range r.lines {
			if strings.Contains(line, "qemu-img commit") && commit < 0 {
				commit = i
			}
			if strings.Contains(line, "qemu-img resize") {
				resize = i
			}
		}
		if commit < 0 || resize >= 0 && resize < commit {
			t.Fatalf("the disk was grown while an overlay held the guest's last writes: %v", r.lines)
		}
		if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
			t.Fatal("record left")
		}
	})
	t.Run("operations are refused while the undo fails", func(t *testing.T) {
		m, r, _ := diskFixture(t)
		leave(t, m)
		stop(m, r)
		r.failing = "qemu-img commit"
		if err := m.Grow(ctx, "ha", 0, 40*gib); !errors.Is(err, ErrSnapshotRecovery) {
			t.Fatalf("grow: %v", err)
		}
		if err := m.Clone(ctx, "ha", CloneRequest{Name: "copy"}, []string{"data", "fast"}); !errors.Is(err, ErrSnapshotRecovery) {
			t.Fatalf("clone: %v", err)
		}
		if err := m.Delete("ha", true, "ha"); !errors.Is(err, ErrSnapshotRecovery) {
			t.Fatalf("delete: %v", err)
		}
		if _, err := m.BackupLease("ha", []string{"data", "fast"}); !errors.Is(err, ErrSnapshotRecovery) {
			t.Fatalf("application backup: %v", err)
		}
		if len(overlays(m)) != 2 {
			t.Fatal("overlay lost")
		}
	})
	t.Run("unreadable record, nothing left: set aside, VM free", func(t *testing.T) {
		m, r, vm := diskFixture(t)
		stop(m, r)
		os.MkdirAll(filepath.Dir(record(m, vm)), 0o700)
		os.WriteFile(record(m, vm), []byte("{not json"), 0o600)
		lock, err := m.diskOperation(ctx, "ha")
		if err != nil {
			t.Fatalf("an unreadable record without overlays blocked the VM: %v", err)
		}
		lock.Unlock()
		if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
			t.Fatal("unusable record not set aside")
		}
		if kept, _ := filepath.Glob(record(m, vm)[:len(record(m, vm))-5] + ".unreadable-*"); len(kept) != 1 {
			t.Fatalf("record not kept for a look: %v", kept)
		}
	})
	t.Run("unreadable record with overlays: held until repaired", func(t *testing.T) {
		m, r, vm := diskFixture(t)
		leave(t, m)
		stop(m, r)
		os.WriteFile(record(m, vm), []byte(`{"uuid":"other","vm":"other"}`), 0o600)
		var told []string
		m.Notify = func(severity, kind, subject string) { told = append(told, kind+":"+subject) }
		m.kvm = m.mountRoot
		if err := m.Start(ctx, "ha", []string{"data", "fast"}); !errors.Is(err, ErrDiskBackupRecord) {
			t.Fatalf("start: %v", err)
		}
		if r.ran("virsh --quiet create") != 0 || r.ran("qemu-img commit") != 0 {
			t.Fatalf("something was done on a guess: %v", r.lines)
		}
		if !slices.Contains(told, "vm_backup_undo_failed:ha") {
			t.Fatalf("nobody was told: %v", told)
		}
		// PLAN 80-2: the list shows the hold without another start.
		held := func() bool {
			for _, s := range m.List([]string{"data", "fast"}) {
				if s.Name == "ha" {
					return s.BackupHeld
				}
			}
			t.Fatal("VM missing from the list")
			return false
		}
		if !held() {
			t.Fatal("the list does not show the VM as held")
		}
		// The repair checks that each overlay builds on its disk.
		r.backing = map[string]bool{}
		if err := m.RepairDiskBackup(ctx, "ha"); !errors.Is(err, ErrDiskBackupRecord) || len(overlays(m)) != 2 {
			t.Fatalf("an overlay that does not name its disk was committed: %v", err)
		}
		for _, file := range overlays(m) {
			r.backing[file] = true
		}
		r.backingOf = func(file string) string { return file[:strings.LastIndex(file[:len(file)-len(OverlaySuffix)], ".")] }
		if err := m.RepairDiskBackup(ctx, "ha"); err != nil {
			t.Fatal(err)
		}
		if r.ran("qemu-img commit -d -f qcow2") != 2 || len(overlays(m)) != 0 {
			t.Fatalf("not repaired: %v", r.lines)
		}
		if held() {
			t.Fatal("the list still shows the VM as held after the repair")
		}
		if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
			t.Fatal("unusable record still in the way")
		}
		lock, err := m.diskOperation(ctx, "ha")
		if err != nil {
			t.Fatalf("VM still held after the repair: %v", err)
		}
		lock.Unlock()
	})
	t.Run("repair of a running VM uses libvirt's block chain", func(t *testing.T) {
		m, r, vm := diskFixture(t)
		leave(t, m)
		os.WriteFile(record(m, vm), []byte("garbage"), 0o600)
		if err := m.RepairDiskBackup(ctx, "ha"); err != nil {
			t.Fatal(err)
		}
		if r.ran("blockcommit") != 2 || len(r.overlay) != 0 {
			t.Fatalf("not committed back: %v", r.lines)
		}
	})
}

// Asking how a VM could be saved is cached briefly and bounded; the
// libvirt snapshot is only planned where the job allows it.
func TestPlanDiskBackupCachedAndOptIn(t *testing.T) {
	m, r, _ := diskFixture(t)
	ctx := context.Background()
	now := time.Now()
	m.now = func() time.Time { return now }
	for range 5 {
		if plan := m.PlanDiskBackupCached(ctx, "ha", DiskBackupOptions{Snapshot: true}); plan.Way != DiskWaySnapshot || !plan.Agent {
			t.Fatalf("%+v", plan)
		}
	}
	if r.ran("guest-ping") != 1 || r.ran("qemu-img info") != 2 {
		t.Fatalf("asked again and again: %d pings, %d infos", r.ran("guest-ping"), r.ran("qemu-img info"))
	}
	now = now.Add(planMaxAge + time.Second)
	m.PlanDiskBackupCached(ctx, "ha", DiskBackupOptions{Snapshot: true})
	if r.ran("guest-ping") != 2 {
		t.Fatal("an old answer was reused")
	}
	// Not allowed by the job: named as such, the guest is not even asked.
	pings := r.ran("guest-ping")
	if plan := m.PlanDiskBackup(ctx, "ha", DiskBackupOptions{}); plan.Way != "" || plan.Reason != DiskSnapshotUnproven {
		t.Fatalf("%+v", plan)
	}
	if plan := m.PlanDiskBackup(ctx, "ha", DiskBackupOptions{Pause: true}); plan.Way != DiskWayPause || plan.Reason != DiskSnapshotUnproven {
		t.Fatalf("%+v", plan)
	}
	if r.ran("guest-ping") != pings {
		t.Fatal("guest agent asked for a way that is not allowed")
	}
	if _, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{}); code(err) != DiskSnapshotUnproven || r.ran("snapshot-create-as") != 0 {
		t.Fatalf("%v", err)
	}
	if !m.Running("ha") || m.Running("gone") {
		t.Fatal("Running")
	}
}
