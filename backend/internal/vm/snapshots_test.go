package vm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func snapshotFixture(t *testing.T) (*Manager, VM) {
	t.Helper()
	m, _ := newTestManager(t)
	v := VM{Name: "home-assistant", DisplayName: "Home Assistant", UUID: newUUID(), Volume: "data", Template: "linux", MemoryMiB: 512, VCPUs: 1, Disks: []Disk{{Volume: "data", File: "disk.raw", Format: "raw", SizeBytes: 4096}}}
	dir := m.dir(v.Volume, v.folder())
	if err := os.MkdirAll(dir, 0o770); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"disk.raw", "nvram.fd"} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte("original "+file), 0o660); err != nil {
			t.Fatal(err)
		}
	}
	m.vms[v.Name] = v
	if err := m.store.WriteJSON(configDir+"/"+v.Name+".json", v); err != nil {
		t.Fatal(err)
	}
	return m, v
}
func waitSnapshot(t *testing.T, m *Manager, v VM) SnapshotStatus {
	t.Helper()
	end := time.Now().Add(10 * time.Second)
	for time.Now().Before(end) {
		s, e := m.SnapshotStatus(v.Name, v.volumes())
		if e != nil {
			t.Fatal(e)
		}
		if s.Job == nil || s.Job.State != "running" {
			return s
		}
		time.Sleep(time.Millisecond * 10)
	}
	t.Fatal("snapshot job timed out")
	return SnapshotStatus{}
}
func captureForTest(t *testing.T, m *Manager, v VM, name string) Snapshot {
	t.Helper()
	if err := m.CreateSnapshot(context.Background(), v.Name, SnapshotRequest{Name: name}, v.volumes()); err != nil {
		t.Fatal(err)
	}
	s := waitSnapshot(t, m, v)
	if s.Job != nil || len(s.Snapshots) == 0 || s.Pending {
		t.Fatalf("capture: %+v", s)
	}
	return s.Snapshots[0]
}
func TestSnapshotDiskRestoreIsIndependentAndConfirmed(t *testing.T) {
	m, v := snapshotFixture(t)
	ctx := context.Background()
	snap := captureForTest(t, m, v, "Before update")
	disk := m.diskPath(v, v.Disks[0])
	os.WriteFile(disk, []byte("changed disk"), 0o660)
	if err := m.RestoreSnapshot(ctx, v.Name, snap.ID, "", false, v.volumes()); !errors.Is(err, ErrNotConfirmed) {
		t.Fatal(err)
	}
	if err := m.Delete(v.Name, true, v.Name); !errors.Is(err, ErrSnapshotExists) {
		t.Fatal(err)
	}
	if err := m.RestoreSnapshot(ctx, v.Name, snap.ID, v.Label(), false, v.volumes()); err != nil {
		t.Fatal(err)
	}
	s := waitSnapshot(t, m, v)
	if s.Job != nil || s.Pending {
		t.Fatalf("restore: %+v", s)
	}
	content, _ := os.ReadFile(disk)
	if string(content) != "original disk.raw" {
		t.Fatalf("wrong restored disk: %s", content)
	}
	os.WriteFile(disk, []byte("independent"), 0o660)
	archived, _ := os.ReadFile(filepath.Join(m.snapshotRoot(v), snap.ID, snap.Files[0].File))
	if string(archived) != "original disk.raw" {
		t.Fatal("snapshot mutated with current disk")
	}
	if err := m.DeleteSnapshot(v.Name, snap.ID, "wrong", v.volumes()); !errors.Is(err, ErrNotConfirmed) {
		t.Fatal(err)
	}
	if err := m.DeleteSnapshot(v.Name, snap.ID, snap.Name, v.volumes()); err != nil {
		t.Fatal(err)
	}
	content, _ = os.ReadFile(disk)
	if string(content) != "independent" {
		t.Fatal("deletion affected live disk")
	}
}
func TestSnapshotTPMRestoreReplacesWholeState(t *testing.T) {
	m, v := snapshotFixture(t)
	v.TPM = true
	m.vms[v.Name] = v
	dir := filepath.Join(m.dir(v.Volume, v.folder()), "tpm")
	os.Mkdir(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "permall"), []byte("original TPM"), 0o600)
	snap := captureForTest(t, m, v, "TPM")
	os.WriteFile(filepath.Join(dir, "permall"), []byte("changed"), 0o600)
	os.WriteFile(filepath.Join(dir, "extra"), []byte("later"), 0o600)
	if err := m.RestoreSnapshot(context.Background(), v.Name, snap.ID, v.Name, false, v.volumes()); err != nil {
		t.Fatal(err)
	}
	s := waitSnapshot(t, m, v)
	if s.Job != nil {
		t.Fatalf("%+v", s.Job)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "permall"))
	if string(data) != "original TPM" {
		t.Fatal("TPM not restored")
	}
	if _, e := os.Stat(filepath.Join(dir, "extra")); !os.IsNotExist(e) {
		t.Fatal("extra TPM file survived")
	}
}
func TestSnapshotRefusesUnsafeGuestsAndSymlinks(t *testing.T) {
	for _, kind := range []string{"pci", "usb", "folder", "block", "source", "iso"} {
		t.Run(kind, func(t *testing.T) {
			m, v := snapshotFixture(t)
			switch kind {
			case "iso":
				v.ISO = "/mnt/data/install.iso"
			case "pci":
				v.PCI = []string{"0000:01:00.0"}
			case "usb":
				v.USB = []string{"1111:2222"}
			case "folder":
				v.Folders = []Folder{{Path: "/mnt/data"}}
			case "block":
				v.Disks[0].Path = "/dev/disk/by-id/whole"
			case "source":
				v.Disks[0].Source = "copying"
			}
			m.vms[v.Name] = v
			if e := m.CreateSnapshot(context.Background(), v.Name, SnapshotRequest{Name: "test", Memory: true, AllowPause: true}, v.volumes()); !errors.Is(e, ErrSnapshotUnsupported) {
				t.Fatalf("accepted %s: %v", kind, e)
			}
		})
	}
	m, v := snapshotFixture(t)
	disk := m.diskPath(v, v.Disks[0])
	os.Remove(disk)
	os.Symlink("/etc/hostname", disk)
	if e := m.CreateSnapshot(context.Background(), v.Name, SnapshotRequest{Name: "test"}, v.volumes()); e == nil {
		t.Fatal("followed source symlink")
	}
}
func TestSnapshotMutationsSerializeAndPendingPreventsBoot(t *testing.T) {
	m, v := snapshotFixture(t)
	lock, _ := m.operation(v.Name)
	if e := m.Start(context.Background(), v.Name, v.volumes()); !errors.Is(e, ErrSnapshotBusy) {
		t.Fatal(e)
	}
	if e := m.Grow(context.Background(), v.Name, 0, 40960); !errors.Is(e, ErrSnapshotBusy) {
		t.Fatal(e)
	}
	lock.Unlock()
	config.Open(m.snapshotRoot(v)).WriteJSON("operation.json", snapshotOperation{Kind: "create", ID: newUUID(), Original: v})
	if e := m.Start(context.Background(), v.Name, v.volumes()); !errors.Is(e, ErrSnapshotRecovery) {
		t.Fatal(e)
	}
	if e := m.Delete(v.Name, false, ""); !errors.Is(e, ErrSnapshotRecovery) {
		t.Fatal(e)
	}
}
func TestInterruptedSnapshotRestoreRollsBackAllFiles(t *testing.T) {
	m, v := snapshotFixture(t)
	snap := captureForTest(t, m, v, "checkpoint")
	disk := m.diskPath(v, v.Disks[0])
	os.WriteFile(disk, []byte("new work"), 0o660)
	op := snapshotOperation{Kind: "restore", Phase: "switching", ID: snap.ID, Original: v, Files: snap.Files, Existing: []bool{true, true}}
	for _, f := range snap.Files {
		if err := copySnapshotTree(filepath.Join(m.snapshotRoot(v), snap.ID, f.File), snapshotStaging(f, snap.ID, ".new")); err != nil {
			t.Fatal(err)
		}
	}
	first := snap.Files[0]
	os.Rename(first.Target, snapshotStaging(first, snap.ID, ".old"))
	os.Rename(snapshotStaging(first, snap.ID, ".new"), first.Target)
	config.Open(m.snapshotRoot(v)).WriteJSON("operation.json", op)
	// Simulate a fresh daemon; cached jobs and locks are deliberately gone.
	m2, _ := newTestManager(t)
	m2.mountRoot = m.mountRoot
	m2.store = m.store
	m2.Load()
	if e := m2.RecoverSnapshot(context.Background(), v.Name, v.Name, false, v.volumes()); e != nil {
		t.Fatal(e)
	}
	got, _ := os.ReadFile(disk)
	if string(got) != "new work" {
		t.Fatalf("rollback lost latest disk: %s", got)
	}
	if m2.pendingSnapshot(v) {
		t.Fatal("recovery left journal")
	}
}
func TestSnapshotRejectsDamagedArchivesBeforeChangingDisk(t *testing.T) {
	m, v := snapshotFixture(t)
	s := captureForTest(t, m, v, "test")
	source := filepath.Join(m.snapshotRoot(v), s.ID, s.Files[0].File)
	os.WriteFile(source, []byte("short"), 0o600)
	if e := m.RestoreSnapshot(context.Background(), v.Name, s.ID, v.Name, false, v.volumes()); !errors.Is(e, ErrBadImage) {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(m.diskPath(v, v.Disks[0]))
	if string(data) != "original disk.raw" {
		t.Fatal("changed target before validation")
	}
}

type snapshotRunner struct {
	base        *fakeRunner
	m           *Manager
	v           VM
	failRestore bool
}

func (r *snapshotRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if name == "virsh" && len(args) > 1 {
		switch args[1] {
		case "save":
			os.Remove(filepath.Join(r.m.pidDir, r.v.Name+".pid"))
			return "", os.WriteFile(args[3], []byte("memory image"), 0o600)
		case "restore":
			if r.failRestore {
				return "", fmt.Errorf("restore failed")
			}
			return "", os.WriteFile(filepath.Join(r.m.pidDir, r.v.Name+".pid"), []byte(fmt.Sprint(os.Getpid())), 0o600)
		case "save-image-dumpxml":
			return "<domain/>", nil
		}
	}
	return r.base.Run(ctx, stdin, name, args...)
}
func TestSnapshotRAMCaptureResumesAndAmbiguousRecoveryNeverReplays(t *testing.T) {
	if _, e := os.Stat(firmware(VM{}).code); e != nil {
		t.Skip("OVMF is not installed")
	}
	m, v := snapshotFixture(t)
	runner := &snapshotRunner{base: m.runner.(*fakeRunner), m: m, v: v}
	m.runner = runner
	pid := filepath.Join(m.pidDir, v.Name+".pid")
	os.WriteFile(pid, []byte(fmt.Sprint(os.Getpid())), 0o600)
	if e := m.CreateSnapshot(context.Background(), v.Name, SnapshotRequest{Name: "RAM", Memory: true}, v.volumes()); !errors.Is(e, ErrNotConfirmed) {
		t.Fatal(e)
	}
	if e := m.CreateSnapshot(context.Background(), v.Name, SnapshotRequest{Name: "RAM", Memory: true, AllowPause: true}, v.volumes()); e != nil {
		t.Fatal(e)
	}
	s := waitSnapshot(t, m, v)
	if s.Job != nil || s.Pending || !s.Snapshots[0].Memory || m.pid(v.Name) == 0 {
		t.Fatalf("RAM capture: %+v", s)
	}
	os.Remove(pid)
	snap := s.Snapshots[0]
	op := snapshotOperation{Kind: "create", Phase: "resume-started", ID: snap.ID, Original: v, Memory: true}
	config.Open(m.snapshotRoot(v)).WriteJSON("operation.json", op)
	if e := m.RecoverSnapshot(context.Background(), v.Name, v.Name, false, v.volumes()); !errors.Is(e, ErrSnapshotRecovery) {
		t.Fatal(e)
	}
	if m.pid(v.Name) != 0 {
		t.Fatal("replayed possibly consumed RAM")
	}
	if e := m.RecoverSnapshot(context.Background(), v.Name, v.Name, true, v.volumes()); e != nil {
		t.Fatal(e)
	}
	if m.pid(v.Name) != 0 || m.pendingSnapshot(v) {
		t.Fatal("discard should allow cold boot, not replay RAM")
	}
}
func TestSnapshotScheduleRetentionAndRestartDeduplication(t *testing.T) {
	m, v := snapshotFixture(t)
	manual := captureForTest(t, m, v, "manual")
	now := time.Date(2026, 10, 16, 3, 0, 0, 0, time.UTC)
	schedule := SnapshotSchedule{Enabled: true, Time: "03:00", Days: []int{5, 6}, Keep: 1}
	if e := m.SetSnapshotSchedule(v.Name, schedule); e != nil {
		t.Fatal(e)
	}
	m.runSnapshotSchedules(context.Background(), v.volumes(), now)
	s := waitSnapshot(t, m, v)
	if s.Job != nil || len(s.Snapshots) != 2 {
		t.Fatalf("first schedule %+v", s)
	}
	m.runSnapshotSchedules(context.Background(), v.volumes(), now)
	s = waitSnapshot(t, m, v)
	if len(s.Snapshots) != 2 {
		t.Fatal("duplicate")
	}
	m.Load()
	m.runSnapshotSchedules(context.Background(), v.volumes(), now)
	s = waitSnapshot(t, m, v)
	if len(s.Snapshots) != 2 {
		t.Fatal("restart duplicate")
	}
	m.runSnapshotSchedules(context.Background(), v.volumes(), now.Add(24*time.Hour))
	s = waitSnapshot(t, m, v)
	if len(s.Snapshots) != 2 {
		t.Fatalf("retention: %+v", s)
	}
	found := false
	for _, snap := range s.Snapshots {
		found = found || snap.ID == manual.ID
	}
	if !found {
		t.Fatal("removed manual snapshot")
	}
	for _, bad := range []SnapshotSchedule{{Enabled: true, Time: "25:00", Days: []int{1}, Keep: 1}, {Enabled: true, Time: "03:00", Days: []int{1, 1}, Keep: 1}, {Enabled: true, Time: "03:00", Days: []int{1}, Keep: 0}, {Enabled: true, Time: "03:00", Days: []int{1}, Keep: 1, Memory: true}} {
		if validSnapshotSchedule(bad) {
			t.Fatal("accepted invalid schedule")
		}
	}
}
func TestSparseSnapshotCopyAndSymlinkRefusal(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
	f, _ := os.Create(src)
	f.WriteAt([]byte("start"), 0)
	f.WriteAt([]byte("end"), 32<<20)
	f.Close()
	if e := copySnapshotFile(src, dst); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(dst)
	if len(data) != (32<<20)+3 || !strings.HasPrefix(string(data), "start") || string(data[len(data)-3:]) != "end" {
		t.Fatal("sparse copy damaged")
	}
	link := filepath.Join(dir, "link")
	os.Symlink(src, link)
	if e := copySnapshotFile(link, filepath.Join(dir, "bad")); e == nil {
		t.Fatal("followed symlink")
	}
}

func TestIncompleteSnapshotCanBeExplicitlyRemoved(t *testing.T) {
	m, v := snapshotFixture(t)
	s := captureForTest(t, m, v, "valid")
	os.WriteFile(filepath.Join(m.snapshotRoot(v), s.ID, "snapshot.json"), []byte("broken json"), 0o600)
	status, e := m.SnapshotStatus(v.Name, v.volumes())
	if e != nil || len(status.Snapshots) != 1 || !status.Snapshots[0].Incomplete {
		t.Fatalf("lost damaged snapshot: %+v %v", status, e)
	}
	if e = m.DeleteSnapshot(v.Name, s.ID, s.ID, v.volumes()); e != nil {
		t.Fatal(e)
	}
}

func TestVMFileProtectionIncludesDirectImagesAndAncestors(t *testing.T) {
	m, v := snapshotFixture(t)
	external := filepath.Join(m.mountRoot, "data", "images", "ha.qcow2")
	os.MkdirAll(filepath.Dir(external), 0755)
	os.WriteFile(external, []byte("image"), 0600)
	v.Disks = append(v.Disks, Disk{Path: external, Format: "qcow2"})
	m.vms[v.Name] = v
	for _, path := range []string{external, filepath.Dir(external), m.mountRoot, m.dir(v.Volume, v.folder()), filepath.Join(m.dir(v.Volume, v.folder()), "tpm", "state")} {
		if !m.ProtectFile(path) {
			t.Errorf("unprotected VM path: %s", path)
		}
	}
	alias := filepath.Join(m.mountRoot, "data", "alias")
	os.Symlink(filepath.Dir(external), alias)
	if !m.ProtectFile(filepath.Join(alias, "ha.qcow2")) {
		t.Fatal("symlink bypass")
	}
	for _, path := range []string{external + ".backup", filepath.Join(m.mountRoot, "data", "photos"), m.dir(v.Volume, v.folder()) + "-other"} {
		if m.ProtectFile(path) {
			t.Errorf("protected unrelated path: %s", path)
		}
	}
	delete(m.vms, v.Name)
	if m.ProtectFile(external) {
		t.Fatal("removed VM still protects image")
	}
}

func TestConcurrentSnapshotSpaceCannotBeClaimedTwice(t *testing.T) {
	m, v := snapshotFixture(t)
	path := filepath.Join(m.mountRoot, v.Volume)
	free, err := freeBytes(path)
	if err != nil {
		t.Fatal(err)
	}
	release, err := m.reserveSnapshotSpace(map[string]int64{path: free * 3 / 4})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.reserveSnapshotSpace(map[string]int64{m.dir(v.Volume, v.folder()): free / 2}); !errors.Is(err, ErrNoSpace) {
		t.Fatalf("overcommitted same filesystem via another path: %v", err)
	}
	release()
	release()
	next, err := m.reserveSnapshotSpace(map[string]int64{path: free * 3 / 4})
	if err != nil {
		t.Fatal("reservation leaked", err)
	}
	next()
	if _, err = m.reserveSnapshotSpace(map[string]int64{path: 1 << 62}); !errors.Is(err, ErrNoSpace) {
		t.Fatal(err)
	}
	next, err = m.reserveSnapshotSpace(map[string]int64{path: free / 2})
	if err != nil {
		t.Fatal(err)
	}
	next()
}

func TestSnapshotFailuresResumeOrKeepRecoveryAndReleaseSpace(t *testing.T) {
	if _, err := os.Stat(firmware(VM{}).code); err != nil {
		t.Skip("OVMF is not installed")
	}
	for _, failure := range []string{"copy", "resume"} {
		t.Run(failure, func(t *testing.T) {
			m, v := snapshotFixture(t)
			runner := &snapshotRunner{base: m.runner.(*fakeRunner), m: m, v: v, failRestore: failure == "resume"}
			if failure == "copy" {
				runner.base.fail = "qemu-img info"
			}
			m.runner = runner
			os.WriteFile(filepath.Join(m.pidDir, v.Name+".pid"), []byte(fmt.Sprint(os.Getpid())), 0600)
			if err := m.CreateSnapshot(context.Background(), v.Name, SnapshotRequest{Name: "failure", Memory: true, AllowPause: true}, v.volumes()); err != nil {
				t.Fatal(err)
			}
			status := waitSnapshot(t, m, v)
			if status.Job == nil || status.Job.State != "failed" {
				t.Fatalf("failure not visible: %+v", status)
			}
			m.snapshotSpaceMu.Lock()
			reserved := len(m.snapshotSpace)
			m.snapshotSpaceMu.Unlock()
			if reserved != 0 {
				t.Fatal("failed snapshot leaked disk reservation")
			}
			if failure == "copy" {
				if status.Pending || m.pid(v.Name) == 0 || len(status.Snapshots) != 0 {
					t.Fatalf("copy failure did not resume original guest: %+v", status)
				}
			} else {
				if !status.Pending || m.pid(v.Name) != 0 {
					t.Fatal("failed resume lost recovery state")
				}
				if err := m.RecoverSnapshot(context.Background(), v.Name, v.Name, false, v.volumes()); !errors.Is(err, ErrSnapshotRecovery) {
					t.Fatal("ambiguous RAM restore replayed", err)
				}
			}
		})
	}
}

func TestSnapshotScheduleUsesCurrentTimezoneAndSkipsRepeatedDSTHour(t *testing.T) {
	m, v := snapshotFixture(t)
	zone := "Europe/Berlin"
	m.Timezone = func() string { return zone }
	schedule := SnapshotSchedule{Enabled: true, Time: "02:30", Days: []int{0}, Keep: 5}
	if err := m.SetSnapshotSchedule(v.Name, schedule); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)
	m.runSnapshotSchedules(context.Background(), v.volumes(), now)
	if s := waitSnapshot(t, m, v); len(s.Snapshots) != 1 {
		t.Fatalf("ignored NAS timezone: %+v", s)
	}
	m.runSnapshotSchedules(context.Background(), v.volumes(), now.Add(time.Hour))
	if s := waitSnapshot(t, m, v); len(s.Snapshots) != 1 {
		t.Fatal("repeated DST hour ran twice")
	}
	zone = "UTC"
	next := now.Add(7 * 24 * time.Hour)
	m.runSnapshotSchedules(context.Background(), v.volumes(), next)
	if s := waitSnapshot(t, m, v); len(s.Snapshots) != 1 {
		t.Fatal("ignored new timezone")
	}
	m.runSnapshotSchedules(context.Background(), v.volumes(), next.Add(2*time.Hour))
	if s := waitSnapshot(t, m, v); len(s.Snapshots) != 2 {
		t.Fatalf("new timezone did not take effect: %+v", s)
	}
}
