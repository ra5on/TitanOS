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
			backing = `, "backing-filename": "base.qcow2"`
		}
		return fmt.Sprintf(`{"virtual-size": %d, "format": "qcow2"%s}`, 20*gib, backing), nil
	case name == "qemu-img" && args[0] == "commit":
		return "", nil
	case name == "virsh" && has("domstate"):
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
		return "", nil
	case name == "virsh" && has("domblklist"):
		out := ""
		for target, file := range r.overlay {
			out += " " + target + "   " + file + "\n"
		}
		return out, nil
	case name == "virsh" && has("blockcommit"):
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
	r := &diskRunner{fakeRunner: base, agent: true, backing: map[string]bool{}, overlay: map[string]string{}}
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
	free := diskFree
	diskFree = func(string) (uint64, error) { return 100 * gib, nil }
	t.Cleanup(func() { diskFree = free })
	return m, r, vm
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
			b, err := m.BeginDiskBackup(ctx, "ha", false)
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
			if want := "--disk-only --atomic --no-metadata"; r.ran(want) == 0 {
				t.Fatalf("snapshot without %q: %v", want, r.lines)
			}
			if (r.ran("--quiesce") > 0) != agent {
				t.Fatalf("quiesce asked %d times with agent=%v", r.ran("--quiesce"), agent)
			}
			// The VM is held: no other operation, also not a second backup.
			if _, err := m.BeginDiskBackup(ctx, "ha", false); code(err) != DiskBusy {
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
	b, err := m.BeginDiskBackup(context.Background(), "ha", false)
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
		if plan := m.PlanDiskBackup(ctx, "ha", pause); plan.Way != "" || plan.Reason != want {
			t.Fatalf("plan %+v, want reason %s", plan, want)
		}
		if _, err := m.BeginDiskBackup(ctx, "ha", pause); code(err) != want {
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
		if plan := m.PlanDiskBackup(ctx, "ha", false); plan.NeedBytes != 8*gib || plan.FreeBytes != 5*gib {
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
		if _, err := m.BeginDiskBackup(ctx, "ha", true); code(err) != DiskBusy {
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
		if _, err := m.BeginDiskBackup(ctx, "ha", true); code(err) != DiskBusy {
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
		if _, err := m.BeginDiskBackup(ctx, "ha", true); code(err) != DiskBusy {
			t.Fatalf("%v", err)
		}
	})
}

func TestDiskBackupSnapshotThatFailsChangesNothing(t *testing.T) {
	m, r, vm := diskFixture(t)
	r.failing = "snapshot-create-as"
	if _, err := m.BeginDiskBackup(context.Background(), "ha", false); code(err) != DiskSnapshotFailed {
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
		b, err := m.BeginDiskBackup(ctx, "ha", true)
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
		b, err := m.BeginDiskBackup(run, "ha", true)
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
		b, err := m.BeginDiskBackup(ctx, "ha", true)
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
		if r.paused || !b.Expired() {
			t.Fatalf("VM still paused after the limit (expired=%v)", b.Expired())
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
		if _, err := m.BeginDiskBackup(ctx, "ha", true); code(err) != DiskPauseFailed {
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
		b, err := m.BeginDiskBackup(ctx, "ha", pause)
		if err != nil {
			t.Fatal(err)
		}
		if b.timer != nil {
			b.timer.Stop()
		}
		b.lock.Unlock()
		m.mu.Lock()
		m.diskBackups = nil
		m.diskBackupChecked = nil
		m.mu.Unlock()
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
		b, err := m.BeginDiskBackup(ctx, "ha", false)
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
		b, err := m.BeginDiskBackup(ctx, "ha", false)
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
	b, err := m.BeginDiskBackup(ctx, "ha", false)
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
