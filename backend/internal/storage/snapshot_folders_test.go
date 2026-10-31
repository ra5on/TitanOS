package storage

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

var restoreMtime = time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC)

// folderFixture builds a snapshot "manual" of volume "data" with a folder
// tree and a live volume that differs from it.
func folderFixture(t *testing.T) (*Manager, string, string) {
	t.Helper()
	m, snap := snapshotFilesFixture(t)
	m.runner = scheduleSnapshotRunner{m.runner.(*fakeRunner)}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(snap, "photos", "sub", "deep"), 0750))
	must(os.WriteFile(filepath.Join(snap, "photos", "sub", "b.txt"), []byte("bee"), 0640))
	must(os.WriteFile(filepath.Join(snap, "photos", "sub", "deep", "c.txt"), []byte("sea"), 0600))
	must(os.Symlink("../../../etc/passwd", filepath.Join(snap, "photos", "escape")))
	must(os.MkdirAll(filepath.Join(snap, "photos", ".recycle", "alex"), 0700))
	must(os.WriteFile(filepath.Join(snap, "photos", ".recycle", "alex", "old"), []byte("old"), 0600))
	must(unix.Mkfifo(filepath.Join(snap, "photos", "pipe"), 0600))
	for _, p := range []string{"photos/sub/b.txt", "photos/sub/deep/c.txt", "photos/sub/deep", "photos/sub"} {
		must(os.Chtimes(filepath.Join(snap, p), restoreMtime, restoreMtime))
	}
	live := m.MountPoint("data")
	must(os.MkdirAll(filepath.Join(live, "photos"), 0755))
	must(os.WriteFile(filepath.Join(live, "photos", "a.jpg"), []byte("CHANGED"), 0600))
	return m, snap, live
}

// treeSum fingerprints names, types, modes, sizes, times and contents.
func treeSum(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		info, _ := os.Lstat(p)
		rel, _ := filepath.Rel(root, p)
		line := fmt.Sprintf("%s %v %d %d", rel, info.Mode(), info.Size(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			data, _ := os.ReadFile(p)
			line += fmt.Sprintf(" %x", sha256.Sum256(data))
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(p)
			line += " -> " + target
		}
		lines = append(lines, line)
		return nil
	})
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func noTemp(t *testing.T, root string) {
	t.Helper()
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && strings.Contains(d.Name(), ".titanos-restore-") {
			t.Errorf("temporary entry left behind: %s", p)
		}
		return nil
	})
}

func runFolderRestore(t *testing.T, m *Manager, req SnapshotFolderRequest) SnapshotFolderJob {
	t.Helper()
	ctx := context.Background()
	preview, err := m.PreviewSnapshotFolder(ctx, "data", "manual", req)
	if err != nil {
		t.Fatal(err)
	}
	req.UUID = preview.UUID
	job, err := m.StartSnapshotFolderRestore(ctx, "alex", "data", "manual", req)
	if err != nil {
		t.Fatal(err)
	}
	return waitRestore(t, m, job.ID)
}

func waitRestore(t *testing.T, m *Manager, id string) SnapshotFolderJob {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, job := range m.SnapshotRestores() {
			if job.ID == id && job.State != RestoreRunning {
				return job
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("restore did not finish")
	return SnapshotFolderJob{}
}

func read(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSnapshotFolderRestoreConflictPolicies(t *testing.T) {
	m, snap, live := folderFixture(t)
	before := treeSum(t, snap)
	ctx := context.Background()

	preview, err := m.PreviewSnapshotFolder(ctx, "data", "manual", SnapshotFolderRequest{Path: "photos", Mode: RestoreOriginal, Conflict: RestoreSkip})
	if err != nil {
		t.Fatal(err)
	}
	// a.jpg, b.txt, c.txt; the link; the pipe; .recycle is never counted.
	if preview.Files != 3 || preview.Links != 1 || preview.Special != 1 || preview.Folders != 2 || preview.Conflicts != 1 || preview.Bytes != 11 || preview.UUID == "" || preview.Target != "photos" {
		t.Fatalf("%+v", preview)
	}
	if _, err := os.Stat(filepath.Join(live, "photos", "sub")); !os.IsNotExist(err) {
		t.Fatal("preview wrote to the volume")
	}

	job := runFolderRestore(t, m, SnapshotFolderRequest{Path: "photos", Mode: RestoreOriginal, Conflict: RestoreSkip})
	if job.State != RestoreDone || job.Skipped != 1 || job.Copied != 3 || job.TotalFiles != 4 || job.Files != 4 || job.Special != 1 {
		t.Fatalf("%+v", job)
	}
	if read(t, filepath.Join(live, "photos", "a.jpg")) != "CHANGED" {
		t.Fatal("skip overwrote an existing file")
	}
	if read(t, filepath.Join(live, "photos", "sub", "deep", "c.txt")) != "sea" {
		t.Fatal("subfolder not restored")
	}
	info, _ := os.Stat(filepath.Join(live, "photos", "sub", "b.txt"))
	if info.Mode().Perm() != 0640 || !info.ModTime().Equal(restoreMtime) {
		t.Fatalf("metadata lost: %v %v", info.Mode(), info.ModTime())
	}
	dir, _ := os.Stat(filepath.Join(live, "photos", "sub"))
	if dir.Mode().Perm() != 0750 || !dir.ModTime().Equal(restoreMtime) {
		t.Fatalf("folder metadata lost: %v %v", dir.Mode(), dir.ModTime())
	}
	if target, err := os.Readlink(filepath.Join(live, "photos", "escape")); err != nil || target != "../../../etc/passwd" {
		t.Fatal("link not restored as a link", target, err)
	}
	for _, p := range []string{".recycle", "pipe"} {
		if _, err := os.Lstat(filepath.Join(live, "photos", p)); !os.IsNotExist(err) {
			t.Fatal("restored", p)
		}
	}
	if live, _ := os.Stat(filepath.Join(live, "photos")); live.Mode().Perm() != 0755 {
		t.Fatal("existing folder metadata changed")
	}

	job = runFolderRestore(t, m, SnapshotFolderRequest{Path: "photos", Mode: RestoreOriginal, Conflict: RestoreRename})
	if job.State != RestoreDone || job.Renamed != 4 {
		t.Fatalf("%+v", job)
	}
	if read(t, filepath.Join(live, "photos", "a (2).jpg")) != "photo" || read(t, filepath.Join(live, "photos", "a.jpg")) != "CHANGED" || read(t, filepath.Join(live, "photos", "sub", "b (2).txt")) != "bee" {
		t.Fatal("keep both did not keep both")
	}

	job = runFolderRestore(t, m, SnapshotFolderRequest{Path: "photos", Mode: RestoreOriginal, Conflict: RestoreReplace})
	if job.State != RestoreDone || job.Replaced == 0 {
		t.Fatalf("%+v", job)
	}
	if read(t, filepath.Join(live, "photos", "a.jpg")) != "photo" {
		t.Fatal("replace kept the old file")
	}
	if treeSum(t, snap) != before {
		t.Fatal("snapshot changed")
	}
	noTemp(t, live)
}

func TestSnapshotFolderRestoreRecreatesMissingChainAndNewFolder(t *testing.T) {
	m, _, live := folderFixture(t)
	os.RemoveAll(filepath.Join(live, "photos"))
	job := runFolderRestore(t, m, SnapshotFolderRequest{Path: "photos/sub/deep", Mode: RestoreOriginal, Conflict: RestoreSkip})
	if job.State != RestoreDone || job.Copied != 1 {
		t.Fatalf("%+v", job)
	}
	if read(t, filepath.Join(live, "photos", "sub", "deep", "c.txt")) != "sea" {
		t.Fatal("missing parents not recreated")
	}
	if entries, _ := os.ReadDir(filepath.Join(live, "photos", "sub")); len(entries) != 1 {
		t.Fatal("restored more than the chosen folder", entries)
	}
	if info, _ := os.Stat(filepath.Join(live, "photos", "sub")); info.Mode().Perm() != 0750 {
		t.Fatal("parent metadata not taken from the snapshot")
	}

	for i, want := range []string{"photos/sub (manual)", "photos/sub (manual) (2)"} {
		preview, err := m.PreviewSnapshotFolder(context.Background(), "data", "manual", SnapshotFolderRequest{Path: "photos/sub", Mode: RestoreCopy})
		if err != nil || preview.Target != want || preview.Conflicts != 0 {
			t.Fatal(i, preview, err)
		}
		job := runFolderRestore(t, m, SnapshotFolderRequest{Path: "photos/sub", Mode: RestoreCopy})
		if job.State != RestoreDone || job.Target != want {
			t.Fatalf("%+v", job)
		}
		if read(t, filepath.Join(live, want, "deep", "c.txt")) != "sea" || read(t, filepath.Join(live, want, "b.txt")) != "bee" {
			t.Fatal("copy incomplete")
		}
	}
	if read(t, filepath.Join(live, "photos", "sub", "deep", "c.txt")) != "sea" {
		t.Fatal("copy touched the original")
	}
	os.RemoveAll(filepath.Join(live, "photos"))
	if _, err := m.PreviewSnapshotFolder(context.Background(), "data", "manual", SnapshotFolderRequest{Path: "photos/sub", Mode: RestoreCopy}); !errors.Is(err, ErrNotPossible) {
		t.Fatal("copy next to a missing parent", err)
	}
	noTemp(t, live)
}

func TestSnapshotFolderRestorePathSafety(t *testing.T) {
	m, snap, live := folderFixture(t)
	ctx := context.Background()
	for _, bad := range []SnapshotFolderRequest{
		{Path: "", Mode: RestoreOriginal, Conflict: RestoreSkip},
		{Path: ".", Mode: RestoreOriginal, Conflict: RestoreSkip},
		{Path: "../data", Mode: RestoreOriginal, Conflict: RestoreSkip},
		{Path: "/photos", Mode: RestoreOriginal, Conflict: RestoreSkip},
		{Path: "photos/../photos", Mode: RestoreOriginal, Conflict: RestoreSkip},
		{Path: ".titanos/notify", Mode: RestoreOriginal, Conflict: RestoreSkip},
		{Path: "photos/.recycle", Mode: RestoreOriginal, Conflict: RestoreSkip},
		{Path: "link", Mode: RestoreOriginal, Conflict: RestoreSkip},
		{Path: "photos/escape", Mode: RestoreOriginal, Conflict: RestoreSkip},
		{Path: "photos/a.jpg", Mode: RestoreOriginal, Conflict: RestoreSkip},
		{Path: "photos", Mode: RestoreOriginal, Conflict: "merge"},
		{Path: "photos", Mode: "rollback"},
	} {
		if _, err := m.PreviewSnapshotFolder(ctx, "data", "manual", bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
		if _, err := m.StartSnapshotFolderRestore(ctx, "alex", "data", "manual", bad); err == nil {
			t.Fatalf("started %+v", bad)
		}
	}

	// A live folder on the way that became a symlink is never followed.
	outside := t.TempDir()
	os.RemoveAll(filepath.Join(live, "photos"))
	if err := os.Symlink(outside, filepath.Join(live, "photos")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PreviewSnapshotFolder(ctx, "data", "manual", SnapshotFolderRequest{Path: "photos/sub", Mode: RestoreOriginal, Conflict: RestoreReplace}); !errors.Is(err, ErrNotPossible) {
		t.Fatal("followed a live symlink", err)
	}
	// Inside the target a symlink in place of a folder is kept and the
	// folder lands next to it, even with replace.
	os.Remove(filepath.Join(live, "photos"))
	os.MkdirAll(filepath.Join(live, "photos"), 0755)
	os.Symlink(outside, filepath.Join(live, "photos", "sub"))
	job := runFolderRestore(t, m, SnapshotFolderRequest{Path: "photos", Mode: RestoreOriginal, Conflict: RestoreReplace})
	if job.State != RestoreDone {
		t.Fatalf("%+v", job)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatal("wrote through a symlink outside the target", entries)
	}
	if target, _ := os.Readlink(filepath.Join(live, "photos", "sub")); target != outside {
		t.Fatal("replaced a link by a folder")
	}
	if read(t, filepath.Join(live, "photos", "sub (2)", "b.txt")) != "bee" {
		t.Fatal("folder not kept next to the link")
	}
	// Excluded names are not restored, and the snapshot holds the private tree.
	if _, err := os.Stat(filepath.Join(snap, ".titanos", "notify", "private")); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotFolderRestorePreservesExtendedAttributes(t *testing.T) {
	m, snap, live := folderFixture(t)
	file := filepath.Join(snap, "photos", "sub", "b.txt")
	if err := unix.Setxattr(file, "user.titanos.test", []byte("kept"), 0); err != nil {
		t.Skip("no user xattrs here:", err)
	}
	runFolderRestore(t, m, SnapshotFolderRequest{Path: "photos", Mode: RestoreOriginal, Conflict: RestoreSkip})
	buf := make([]byte, 16)
	n, err := unix.Getxattr(filepath.Join(live, "photos", "sub", "b.txt"), "user.titanos.test", buf)
	if err != nil || string(buf[:n]) != "kept" {
		t.Fatal("xattr lost", err)
	}
	if st, _ := os.Stat(filepath.Join(live, "photos", "sub", "b.txt")); st.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		t.Fatal("owner not kept")
	}
}

func TestSnapshotFolderRestoreJobLifecycle(t *testing.T) {
	m, _, live := folderFixture(t)
	ctx := context.Background()
	if _, err := m.StartSnapshotFolderRestore(ctx, "alex", "data", "manual", SnapshotFolderRequest{Path: "photos", Mode: RestoreCopy}); !errors.Is(err, ErrNotPossible) {
		t.Fatal("started without preview", err)
	}
	if _, err := m.StartSnapshotFolderRestore(ctx, "alex", "data", "manual", SnapshotFolderRequest{UUID: "00000000-0000-0000-0000-000000000000", Path: "photos", Mode: RestoreCopy}); !errors.Is(err, ErrNotPossible) {
		t.Fatal("started for another snapshot", err)
	}
	gate, entered := make(chan struct{}), make(chan struct{})
	var once sync.Once
	m.restoreStep = func(string) {
		once.Do(func() { close(entered) })
		<-gate
	}
	var finished []SnapshotFolderJob
	var mu sync.Mutex
	m.OnSnapshotRestoreDone = func(job SnapshotFolderJob) { mu.Lock(); finished = append(finished, job); mu.Unlock() }
	preview, err := m.PreviewSnapshotFolder(ctx, "data", "manual", SnapshotFolderRequest{Path: "photos", Mode: RestoreCopy})
	if err != nil {
		t.Fatal(err)
	}
	job, err := m.StartSnapshotFolderRestore(ctx, "alex", "data", "manual", SnapshotFolderRequest{UUID: preview.UUID, Path: "photos", Mode: RestoreCopy})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if !m.SnapshotRestoresRunning() || m.ActionsRunning() {
		t.Fatal("app updates would not wait, or storage actions are blocked")
	}
	if _, err := m.StartSnapshotFolderRestore(ctx, "alex", "data", "manual", SnapshotFolderRequest{UUID: preview.UUID, Path: "photos", Mode: RestoreCopy}); !errors.Is(err, ErrBusy) {
		t.Fatal("second restore", err)
	}
	if err := m.SnapshotAction(ctx, "data", "manual", "delete", "manual"); !errors.Is(err, ErrBusy) {
		t.Fatal("snapshot deleted while restoring", err)
	}
	if err := m.Delete(ctx, "data", "data"); !errors.Is(err, ErrBusy) {
		t.Fatal("volume deleted while restoring", err)
	}
	if err := m.ForgetSnapshotRestore(job.ID); !errors.Is(err, ErrBusy) {
		t.Fatal("running job forgotten", err)
	}
	if err := m.CancelSnapshotRestore(job.ID); err != nil {
		t.Fatal(err)
	}
	close(gate)
	done := waitRestore(t, m, job.ID)
	if done.State != RestoreCancelled || done.Finished == nil {
		t.Fatalf("%+v", done)
	}
	if _, err := os.Stat(filepath.Join(live, "photos (manual)")); !os.IsNotExist(err) {
		t.Fatal("cancelled copy became visible")
	}
	noTemp(t, live)
	if m.SnapshotRestoresRunning() {
		t.Fatal("still running")
	}
	mu.Lock()
	if len(finished) != 1 || finished[0].ID != job.ID {
		t.Fatal("no notification", finished)
	}
	mu.Unlock()
	if err := m.ForgetSnapshotRestore(job.ID); err != nil || len(m.SnapshotRestores()) != 0 {
		t.Fatal("forget", err)
	}
	if err := m.CancelSnapshotRestore(job.ID); !errors.Is(err, ErrNoRestore) {
		t.Fatal(err)
	}

	// A failure is reported with its cause.
	m.restoreStep = nil
	os.Chmod(filepath.Join(live, "photos"), 0500)
	t.Cleanup(func() { os.Chmod(filepath.Join(live, "photos"), 0755) })
	if os.Geteuid() != 0 {
		failed := runFolderRestore(t, m, SnapshotFolderRequest{Path: "photos", Mode: RestoreOriginal, Conflict: RestoreSkip})
		if failed.State != RestoreFailed || failed.Error == "" {
			t.Fatalf("%+v", failed)
		}
	}
}

func TestSnapshotRestoreListKeepsTwentyFinished(t *testing.T) {
	m, _, _ := folderFixture(t)
	for i := range 25 {
		m.restores = append(m.restores, &SnapshotFolderJob{ID: fmt.Sprint(i), State: RestoreDone})
	}
	m.restores = append(m.restores, &SnapshotFolderJob{ID: "running", State: RestoreRunning})
	m.trimRestores()
	list := m.SnapshotRestores()
	if len(list) != 21 || list[0].ID != "running" || list[len(list)-1].ID != "5" {
		t.Fatal(len(list), list[0].ID, list[len(list)-1].ID)
	}
	if got := keepBothName(".bashrc", 2, false); got != ".bashrc (2)" {
		t.Fatal(got)
	}
	if got := keepBothName("a.tar.gz", 3, false); got != "a.tar (3).gz" {
		t.Fatal(got)
	}
	if got := keepBothName("v1.0", 2, true); got != "v1.0 (2)" {
		t.Fatal(got)
	}
}
