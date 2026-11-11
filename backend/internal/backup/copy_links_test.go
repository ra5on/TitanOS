package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// copyFixture is a share "docs" on the volume "tank" and a destination.
func copyFixture(t *testing.T) (m *Manager, source, target string, run func() *State) {
	t.Helper()
	mnt := t.TempDir()
	source = filepath.Join(mnt, "tank", "docs")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	target = t.TempDir()
	m = NewManager(config.Open(t.TempDir()))
	m.mountRoot = mnt
	m.Sources = func() []Source { return []Source{{Name: "docs", Volume: "tank", Path: source}} }
	job := Job{ID: "copy", Sources: []string{"docs"}, Destination: Destination{Type: "usb-copy", Folder: "backup"}}
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
	return m, source, target, run
}

// Symbolic links are data of their own: kept as links with the same
// target, never followed, and following the source when it changes.
func TestCopyKeepsSymbolicLinksAsLinks(t *testing.T) {
	_, source, target, run := copyFixture(t)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("not part of the share"), 0o600)
	os.MkdirAll(filepath.Join(source, "real"), 0o755)
	os.WriteFile(filepath.Join(source, "real", "file"), []byte("data"), 0o600)
	os.Symlink("real/file", filepath.Join(source, "relative"))
	os.Symlink(outside, filepath.Join(source, "outside"))
	os.Symlink("nowhere", filepath.Join(source, "dangling"))
	os.Symlink("real", filepath.Join(source, "folder"))
	state := run()
	copied := filepath.Join(target, "backup", "docs")
	for name, want := range map[string]string{"relative": "real/file", "outside": outside, "dangling": "nowhere", "folder": "real"} {
		if to, err := os.Readlink(filepath.Join(copied, name)); err != nil || to != want {
			t.Errorf("%s: link to %q (%v), want %q", name, to, err, want)
		}
	}
	if _, err := os.Lstat(filepath.Join(copied, "outside", "secret")); err == nil {
		// Reachable through the link on this machine, but not copied.
		if info, _ := os.Lstat(filepath.Join(copied, "outside")); info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("what a link points to was copied")
		}
	}
	if state.Warning != "" || state.Skipped != 0 || len(state.Problems) != 0 {
		t.Fatalf("links became a warning: %+v", state)
	}
	// Unchanged links stay as they are; a changed target follows.
	before, _ := os.Lstat(filepath.Join(copied, "relative"))
	os.Remove(filepath.Join(source, "dangling"))
	os.Symlink("elsewhere", filepath.Join(source, "dangling"))
	// A link that became a file, and a file that became a link.
	os.Remove(filepath.Join(source, "folder"))
	os.WriteFile(filepath.Join(source, "folder"), []byte("now a file"), 0o600)
	os.Remove(filepath.Join(source, "real", "file"))
	os.Symlink("../folder", filepath.Join(source, "real", "file"))
	state = run()
	after, _ := os.Lstat(filepath.Join(copied, "relative"))
	if !os.SameFile(before, after) {
		t.Error("an unchanged link was made anew")
	}
	if to, _ := os.Readlink(filepath.Join(copied, "dangling")); to != "elsewhere" {
		t.Errorf("changed link not followed: %q", to)
	}
	if data, err := os.ReadFile(filepath.Join(copied, "folder")); err != nil || string(data) != "now a file" {
		t.Errorf("link that became a file: %q %v", data, err)
	}
	if info, err := os.Lstat(filepath.Join(copied, "folder")); err != nil || !info.Mode().IsRegular() {
		t.Errorf("link that became a file is no regular file: %v", err)
	}
	if to, _ := os.Readlink(filepath.Join(copied, "real", "file")); to != "../folder" {
		t.Errorf("file that became a link: %q", to)
	}
	if state.Warning != "" || len(state.Problems) != 0 {
		t.Fatalf("changed links became a warning: %+v", state)
	}
	if entries, _ := os.ReadDir(copied); slices.ContainsFunc(entries, func(e os.DirEntry) bool { return strings.HasPrefix(e.Name(), ".titanos-copy-") }) {
		t.Error("temporary link left behind")
	}
}

// What was a link in the source is a real folder now, and the reverse:
// the copy follows on the next run instead of failing for every file below
// it each time. The link goes (never what it points to); a folder of the
// copy is moved aside, never deleted.
func TestCopyHealsLinkThatBecameFolderAndFolderThatBecameLink(t *testing.T) {
	_, source, target, run := copyFixture(t)
	copied := filepath.Join(target, "backup", "docs")
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "precious"), []byte("outside the copy"), 0o600)
	os.MkdirAll(filepath.Join(source, "real"), 0o755)
	os.WriteFile(filepath.Join(source, "real", "file"), []byte("data"), 0o600)
	// "was-link" points out of the copy; "was-dir" is a folder with a file.
	os.Symlink(outside, filepath.Join(source, "was-link"))
	os.MkdirAll(filepath.Join(source, "was-dir", "deep"), 0o755)
	os.WriteFile(filepath.Join(source, "was-dir", "deep", "kept"), []byte("kept"), 0o600)
	if state := run(); state.Warning != "" {
		t.Fatalf("first run: %+v", state)
	}
	// link → folder with files, folder → link.
	os.Remove(filepath.Join(source, "was-link"))
	os.MkdirAll(filepath.Join(source, "was-link", "sub"), 0o755)
	os.WriteFile(filepath.Join(source, "was-link", "a"), []byte("a"), 0o600)
	os.WriteFile(filepath.Join(source, "was-link", "sub", "b"), []byte("b"), 0o600)
	os.RemoveAll(filepath.Join(source, "was-dir"))
	os.Symlink("real", filepath.Join(source, "was-dir"))
	state := run()
	if state.Warning != "" || len(state.Problems) != 0 {
		t.Fatalf("the changed kind was not healed: %+v", state)
	}
	if info, err := os.Lstat(filepath.Join(copied, "was-link")); err != nil || !info.IsDir() {
		t.Fatalf("link that became a folder is no folder in the copy: %v", err)
	}
	for name, want := range map[string]string{"was-link/a": "a", "was-link/sub/b": "b"} {
		if data, err := os.ReadFile(filepath.Join(copied, name)); err != nil || string(data) != want {
			t.Fatalf("%s: %q %v", name, data, err)
		}
	}
	// Only the link went: nothing was written or deleted where it pointed.
	if entries, _ := os.ReadDir(outside); len(entries) != 1 || entries[0].Name() != "precious" {
		t.Fatalf("the target of the old link was touched: %v", entries)
	}
	if to, err := os.Readlink(filepath.Join(copied, "was-dir")); err != nil || to != "real" {
		t.Fatalf("folder that became a link: %q %v", to, err)
	}
	// The folder of the copy was moved aside inside the destination.
	moved, _ := filepath.Glob(filepath.Join(target, "backup", deletedFolder, "*", "docs", "was-dir", "deep", "kept"))
	if len(moved) != 1 {
		t.Fatalf("the folder of the copy was not moved aside: %v", moved)
	}
	if data, _ := os.ReadFile(moved[0]); string(data) != "kept" {
		t.Fatalf("moved folder lost its content: %q", data)
	}
	// The next run has nothing to heal and nothing to report.
	if state := run(); state.Warning != "" {
		t.Fatalf("third run: %+v", state)
	}
	// The same day again: the folder set aside before is not overwritten.
	os.Remove(filepath.Join(source, "was-dir"))
	os.MkdirAll(filepath.Join(source, "was-dir"), 0o755)
	os.WriteFile(filepath.Join(source, "was-dir", "second"), []byte("second"), 0o600)
	run()
	os.RemoveAll(filepath.Join(source, "was-dir"))
	os.Symlink("real", filepath.Join(source, "was-dir"))
	if state := run(); state.Warning != "" {
		t.Fatalf("fifth run: %+v", state)
	}
	if again, _ := filepath.Glob(filepath.Join(target, "backup", deletedFolder, "*", "docs", "was-dir-2", "second")); len(again) != 1 {
		t.Fatal("a second folder moved aside on the same day was lost or overwrote the first")
	}
	if data, _ := os.ReadFile(moved[0]); string(data) != "kept" {
		t.Fatal("the folder set aside first was overwritten")
	}
	// The destination folder itself being a link is still refused.
	os.Symlink(t.TempDir(), filepath.Join(target, "escape"))
	root, _ := os.OpenRoot(target)
	defer root.Close()
	if err := copyDirectoriesBelow(root, "escape", "escape/docs"); err == nil {
		t.Fatal("a destination folder that is a link was accepted")
	}
	if err := copyDirectoriesBelow(root, "backup", "escape/docs"); err == nil {
		t.Fatal("a link outside the job's folder was replaced")
	}
}

// Sockets, FIFOs and device files are no data: no warning, no entry.
func TestCopyLeavesSpecialFilesOutSilently(t *testing.T) {
	_, source, target, run := copyFixture(t)
	os.WriteFile(filepath.Join(source, "data"), []byte("data"), 0o600)
	if err := syscall.Mkfifo(filepath.Join(source, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: filepath.Join(source, "app.sock")}); err != nil {
		t.Skip("no unix socket in this folder:", err)
	}
	state := run()
	if state.Warning != "" || state.Skipped != 0 || len(state.Problems) != 0 {
		t.Fatalf("special files became a warning: %+v", state)
	}
	entries, _ := os.ReadDir(filepath.Join(target, "backup", "docs"))
	if len(entries) != 1 || entries[0].Name() != "data" {
		t.Fatalf("copied %v, want only the data file", entries)
	}
}

// vmFixture is copyFixture with the disk of a running VM in the share.
func vmFixture(t *testing.T) (m *Manager, disk, snapshot, target string, run func() *State) {
	t.Helper()
	m, source, target, run := copyFixture(t)
	disk = filepath.Join(source, "ha", "disk.qcow2")
	os.MkdirAll(filepath.Dir(disk), 0o755)
	os.WriteFile(disk, []byte("live state, changing"), 0o600)
	os.WriteFile(filepath.Join(source, "ha", "notes.txt"), []byte("live notes"), 0o600)
	// The snapshot: the volume "tank" as it was at one moment.
	snapshot = t.TempDir()
	os.MkdirAll(filepath.Join(snapshot, "docs", "ha"), 0o755)
	os.WriteFile(filepath.Join(snapshot, "docs", "ha", "disk.qcow2"), []byte("state of one moment"), 0o600)
	os.WriteFile(filepath.Join(snapshot, "docs", "ha", "notes.txt"), []byte("old notes"), 0o600)
	m.LiveFiles = func() []string { return []string{disk} }
	return m, disk, snapshot, target, run
}

// The disk of a running VM is read from the snapshot of its volume; every
// other file is read as it is, and the snapshot goes when the run ends.
func TestCopyReadsDisksOfRunningVMsFromASnapshot(t *testing.T) {
	m, _, snapshot, target, run := vmFixture(t)
	asked, released := 0, 0
	m.SnapshotSource = func(_ context.Context, volume string) (string, func(), error) {
		asked++
		if volume != "tank" {
			t.Errorf("snapshot of %q asked for", volume)
		}
		return snapshot, func() { released++ }, nil
	}
	state := run()
	copied := filepath.Join(target, "backup", "docs", "ha")
	if data, _ := os.ReadFile(filepath.Join(copied, "disk.qcow2")); string(data) != "state of one moment" {
		t.Fatalf("disk of the running VM not read from the snapshot: %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(copied, "notes.txt")); string(data) != "live notes" {
		t.Fatalf("another file was read from the snapshot: %q", data)
	}
	if asked != 1 || released != 1 {
		t.Fatalf("snapshot made %d times and removed %d times, want once each", asked, released)
	}
	if state.Warning != "" || len(state.Problems) != 0 {
		t.Fatalf("warning for a disk that was saved from the snapshot: %+v", state)
	}
	// No VM runs: no snapshot at all.
	m.LiveFiles = func() []string { return nil }
	run()
	if asked != 1 {
		t.Fatal("a snapshot was made although no VM runs")
	}
}

// Where no snapshot can be made (ext4, XFS) the disk is read as it is; if
// it changed meanwhile, the run says so with a code of its own and keeps
// the copy of the run before.
func TestCopyReportsDiskOfRunningVMThatChanged(t *testing.T) {
	m, disk, _, target, run := vmFixture(t)
	m.SnapshotSource = func(context.Context, string) (string, func(), error) {
		return "", nil, errors.New("not possible: no Btrfs")
	}
	state := run()
	copied := filepath.Join(target, "backup", "docs", "ha", "disk.qcow2")
	if data, _ := os.ReadFile(copied); string(data) != "live state, changing" || state.Warning != "" {
		t.Fatalf("quiet disk not copied as it is: %q %+v", data, state)
	}
	// The guest writes while the copy runs: the limiter's pause is where
	// the test lets it.
	os.WriteFile(disk, []byte("a newer state, being written"), 0o600)
	paceSleep = func(context.Context, time.Duration) error { return nil }
	wrote := false
	paceNow = func() time.Time {
		if !wrote {
			wrote = true
			stamp := time.Now().Add(time.Hour)
			os.Chtimes(disk, stamp, stamp)
		}
		return time.Now()
	}
	t.Cleanup(func() { paceNow, paceSleep = time.Now, sleepContext })
	paced := Job{ID: "copy", Sources: []string{"docs"}, Destination: Destination{Type: "usb-copy", Folder: "backup"}, Bandwidth: &Bandwidth{Limit: 1}}
	root, _ := os.OpenRoot(target)
	defer root.Close()
	if err := m.copyTo(context.Background(), paced, root); err != nil {
		t.Fatal(err)
	}
	state = m.states["copy"]
	if len(state.Problems) != 1 || state.Problems[0].Code != ProblemVMDiskChanged || !slices.Equal(state.Problems[0].Paths, []string{"docs/ha/disk.qcow2"}) || state.Problems[0].Count != 1 {
		t.Fatalf("problems %+v", state.Problems)
	}
	if !strings.Contains(state.Warning, "disks of running VMs") || state.Skipped != 1 {
		t.Fatalf("warning %q skipped %d", state.Warning, state.Skipped)
	}
	if data, _ := os.ReadFile(copied); string(data) != "live state, changing" {
		t.Fatalf("the copy of the run before was damaged: %q", data)
	}
}

// A file the snapshot does not hold (it lies in a subvolume of its own)
// is read as it is rather than left out.
func TestCopyFallsBackWhenSnapshotLacksTheFile(t *testing.T) {
	m, _, snapshot, target, run := vmFixture(t)
	os.Remove(filepath.Join(snapshot, "docs", "ha", "disk.qcow2"))
	m.SnapshotSource = func(context.Context, string) (string, func(), error) { return snapshot, func() {}, nil }
	state := run()
	if data, _ := os.ReadFile(filepath.Join(target, "backup", "docs", "ha", "disk.qcow2")); string(data) != "live state, changing" {
		t.Fatalf("disk missing from the snapshot was not copied: %q", data)
	}
	if state.Warning != "" {
		t.Fatalf("warning %q", state.Warning)
	}
}
