package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A VM started after the list of a run was made: with "skip" its disk is
// left out all the same, never opened, and the copy of the run before stays.
func TestSkipHoldsForAVMStartedAfterTheList(t *testing.T) {
	m, disk, target, _, run := vmJob(t, func(j *Job) { j.VMDisks = &VMDisks{Mode: VMDisksSkip} })
	m.LiveDisks = func() map[string]string { return nil }
	m.VMDiskFiles = func() map[string]string { return map[string]string{disk: "ha"} }
	asked := 0
	m.VMRunning = func(vm string) bool {
		asked++
		return asked > 1 // off while the list is made, running when the copy gets to it
	}
	state := run()
	if _, err := os.Stat(filepath.Join(target, "backup", "docs", "ha", "disk.qcow2")); err == nil {
		t.Fatal("the disk of a VM left out on purpose was copied")
	}
	if got := codes(state.Notices); len(got) != 1 || got[0] != NoticeVMDiskSkipped+":docs/ha/disk.qcow2" {
		t.Fatalf("notices %v", got)
	}
}

// Making room in dated runs comes before the disks of running VMs are
// copied; whether this run saves one is not known yet. The run holding the
// last copy of such a disk must not go for room.
func TestMakingRoomKeepsTheLastCopyOfAVMDisk(t *testing.T) {
	m, disk, target, log, _ := vmJob(t, nil)
	now := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m.Timezone = func() string { return "UTC" }
	job := Job{ID: "copy", Sources: []string{"docs"}, Destination: Destination{Type: "usb-copy", Folder: "backup"},
		Copy: &CopyOptions{Dated: true}, Keep: Keep{Mode: "rolling"}, VMDisks: &VMDisks{Mode: VMDisksConsistent, Raw: VMDisksRawPause}}
	runIt := func() {
		t.Helper()
		root, _ := os.OpenRoot(target)
		defer root.Close()
		if err := m.copyTo(context.Background(), job, root); err != nil {
			t.Fatal(err)
		}
	}
	// Day 1: the VM is off, its disk is copied.
	m.LiveDisks = func() map[string]string { return nil }
	runIt()
	// From day 2 on the VM runs and cannot be paused.
	m.LiveDisks = func() map[string]string { return map[string]string{disk: "ha"} }
	m.BeginVMDisks = func(ctx context.Context, vm string, opts VMDiskOptions) (VMDiskSession, error) {
		*log = append(*log, "begin")
		return nil, codedError{ProblemVMDiskPauseFailed}
	}
	now = time.Date(2026, 10, 2, 3, 0, 0, 0, time.UTC)
	runIt()
	// Day 3: the drive is short of room while more than one run is there.
	t.Cleanup(func() { freeSpace = statfsFree })
	freeSpace = func(root *os.Root) (uint64, bool) {
		if len(datedRuns(root, "backup", time.UTC)) > 1 {
			return 0, true
		}
		return 1 << 30, true
	}
	os.WriteFile(filepath.Join(filepath.Dir(disk), "notes.txt"), []byte("notes changed"), 0o600)
	now = time.Date(2026, 10, 3, 3, 0, 0, 0, time.UTC)
	root, _ := os.OpenRoot(target)
	err := m.copyTo(context.Background(), job, root)
	root.Close()
	if err == nil {
		t.Log("room was made without the run that holds the disk")
	}
	for _, r := range datedRuns(mustRoot(t, target), "backup", time.UTC) {
		if _, err := os.Stat(filepath.Join(target, "backup", r.name, "docs", "ha", "disk.qcow2")); err == nil {
			return
		}
	}
	t.Fatal("the only copy of the VM disk was removed to make room")
}

// A file this run could not read is in no version of it: the run that holds
// its last copy stays, whatever the number of versions to keep says.
func TestUnreadableFileKeepsItsLastVersion(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	m, source, target, now := copyEnv(t)
	os.WriteFile(filepath.Join(source, "doc"), []byte("doc"), 0o600)
	os.MkdirAll(filepath.Join(source, "fotos"), 0o700)
	os.WriteFile(filepath.Join(source, "fotos", "a.jpg"), []byte("photo"), 0o600)
	os.WriteFile(filepath.Join(source, "important"), []byte("only copy"), 0o600)
	job := Job{ID: "copy", Sources: []string{"docs"}, Destination: Destination{Type: "usb-copy", Folder: "b"}, Copy: &CopyOptions{Dated: true}, Keep: Keep{Last: 2}}
	if err := runCopy(t, m, job, target); err != nil {
		t.Fatal(err)
	}
	os.Chmod(filepath.Join(source, "important"), 0)
	os.Chmod(filepath.Join(source, "fotos"), 0)
	t.Cleanup(func() {
		os.Chmod(filepath.Join(source, "important"), 0o600)
		os.Chmod(filepath.Join(source, "fotos"), 0o700)
	})
	for day := 2; day <= 4; day++ {
		*now = time.Date(2026, 10, day, 3, 0, 0, 0, time.UTC)
		if err := runCopy(t, m, job, target); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"docs/important", "docs/fotos/a.jpg"} {
		found := false
		for _, r := range datedRuns(mustRoot(t, target), "b", time.UTC) {
			if _, err := os.Stat(filepath.Join(target, "b", r.name, filepath.FromSlash(want))); err == nil {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is in no version any more", want)
		}
	}
}

// A file of the source that became a folder: the folder is copied, the old
// file of the copy goes aside, never away.
func TestFileThatBecameAFolderIsCopied(t *testing.T) {
	_, source, target, run := copyFixture(t)
	os.WriteFile(filepath.Join(source, "a"), []byte("file"), 0o600)
	run()
	os.Remove(filepath.Join(source, "a"))
	os.MkdirAll(filepath.Join(source, "a"), 0o755)
	os.WriteFile(filepath.Join(source, "a", "new"), []byte("new data"), 0o600)
	state := run()
	if data, err := os.ReadFile(filepath.Join(target, "backup", "docs", "a", "new")); err != nil || string(data) != "new data" {
		t.Fatalf("the folder that was a file is not saved: %v (problems %v)", err, codes(state.Problems))
	}
	if !asideHolds(t, target, "docs/a", "file") {
		t.Fatal("the old file of the copy is gone")
	}
}

// A folder of the source that became a file: the file is copied, the old
// folder of the copy goes aside, never away.
func TestFolderThatBecameAFileIsCopied(t *testing.T) {
	_, source, target, run := copyFixture(t)
	os.MkdirAll(filepath.Join(source, "a"), 0o755)
	os.WriteFile(filepath.Join(source, "a", "old"), []byte("old"), 0o600)
	run()
	os.RemoveAll(filepath.Join(source, "a"))
	os.WriteFile(filepath.Join(source, "a"), []byte("now a file"), 0o600)
	state := run()
	if data, err := os.ReadFile(filepath.Join(target, "backup", "docs", "a")); err != nil || string(data) != "now a file" {
		t.Fatalf("the file that was a folder is not saved: %v (problems %v)", err, codes(state.Problems))
	}
	if !asideHolds(t, target, "docs/a/old", "old") {
		t.Fatal("the old folder of the copy is gone")
	}
}

// asideHolds tells whether the deleted folder of the copy holds rel with
// the content want.
func asideHolds(t *testing.T, target, rel, want string) bool {
	t.Helper()
	days, _ := os.ReadDir(filepath.Join(target, "backup", deletedFolder))
	for _, day := range days {
		if data, err := os.ReadFile(filepath.Join(target, "backup", deletedFolder, day.Name(), filepath.FromSlash(rel))); err == nil && string(data) == want {
			return true
		}
	}
	return false
}
