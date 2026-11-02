package backup

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func copyEnv(t *testing.T) (*Manager, string, string, *time.Time) {
	t.Helper()
	source, target := t.TempDir(), t.TempDir()
	m := NewManager(config.Open(t.TempDir()))
	m.mountRoot = t.TempDir()
	m.Sources = func() []Source { return []Source{{Name: "docs", Volume: "tank", Path: source}} }
	now := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m.Timezone = func() string { return "UTC" }
	m.states["copy"] = &State{}
	return m, source, target, &now
}

func runCopy(t *testing.T, m *Manager, job Job, target string) error {
	t.Helper()
	root, _ := os.OpenRoot(target)
	defer root.Close()
	return m.copyTo(context.Background(), job, root)
}

func TestDatedRunsLinkUnchangedFilesAndKeepVersions(t *testing.T) {
	m, source, target, now := copyEnv(t)
	os.WriteFile(filepath.Join(source, "same"), []byte("same"), 0o600)
	os.WriteFile(filepath.Join(source, "edit"), []byte("v1"), 0o600)
	job := Job{ID: "copy", Sources: []string{"docs"}, Destination: Destination{Type: "usb-copy", Folder: "b"}, Copy: &CopyOptions{Dated: true}, Keep: Keep{Daily: 2}}
	for day := 1; day <= 3; day++ {
		*now = time.Date(2026, 10, day, 3, 0, 0, 0, time.UTC)
		if day == 2 {
			os.WriteFile(filepath.Join(source, "edit"), []byte("v2"), 0o600)
		}
		if err := runCopy(t, m, job, target); err != nil {
			t.Fatal(err)
		}
	}
	runs := datedRuns(mustRoot(t, target), "b", time.UTC)
	if len(runs) != 2 || runs[0].name != "2026-10-02_0300" || runs[1].name != "2026-10-03_0300" {
		t.Fatalf("runs %v", runs)
	}
	a, _ := os.Stat(filepath.Join(target, "b/2026-10-02_0300/docs/same"))
	b, _ := os.Stat(filepath.Join(target, "b/2026-10-03_0300/docs/same"))
	if !os.SameFile(a, b) {
		t.Fatal("unchanged file not linked")
	}
	if data, _ := os.ReadFile(filepath.Join(target, "b/2026-10-03_0300/docs/edit")); string(data) != "v2" {
		t.Fatal("changed file wrong")
	}
	if st := a.Sys().(*syscall.Stat_t); st.Nlink != 2 {
		t.Fatalf("links %d", st.Nlink)
	}
}

func TestDatedRunsMakeRoomOnlyWithLeave(t *testing.T) {
	m, source, target, now := copyEnv(t)
	os.WriteFile(filepath.Join(source, "f"), make([]byte, 1000), 0o600)
	job := Job{ID: "copy", Sources: []string{"docs"}, Destination: Destination{Type: "usb-copy", Folder: "b"}, Copy: &CopyOptions{Dated: true}, Keep: Keep{Mode: "rolling"}}
	for day := 1; day <= 3; day++ {
		*now = time.Date(2026, 10, day, 3, 0, 0, 0, time.UTC)
		os.WriteFile(filepath.Join(source, "f"), make([]byte, 1000+day), 0o600)
		if err := runCopy(t, m, job, target); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { freeSpace = statfsFree })
	room := 0
	freeSpace = func(root *os.Root) (uint64, bool) {
		room++
		if len(datedRuns(root, "b", time.UTC)) > 1 {
			return 10, true
		}
		return 1 << 20, true
	}
	*now = time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC)
	os.WriteFile(filepath.Join(source, "f"), make([]byte, 2000), 0o600)
	job.Keep = Keep{Daily: 30}
	if err := runCopy(t, m, job, target); err == nil || !strings.Contains(err.Error(), "not enough space") {
		t.Fatalf("without leave: %v", err)
	}
	job.Keep.WhenFull = "oldest"
	if err := runCopy(t, m, job, target); err != nil {
		t.Fatal(err)
	}
	runs := datedRuns(mustRoot(t, target), "b", time.UTC)
	if len(runs) != 2 || runs[0].name != "2026-10-03_0300" {
		t.Fatalf("runs %v", runs)
	}
}

func TestMirrorMovesDeletedFilesAside(t *testing.T) {
	m, source, target, now := copyEnv(t)
	os.MkdirAll(filepath.Join(source, "sub"), 0o700)
	os.WriteFile(filepath.Join(source, "keep"), []byte("k"), 0o600)
	os.WriteFile(filepath.Join(source, "sub/gone"), []byte("g"), 0o600)
	job := Job{ID: "copy", Sources: []string{"docs"}, Destination: Destination{Type: "usb-copy", Folder: "b"}, Copy: &CopyOptions{Mirror: true, MirrorDays: 7}}
	if err := runCopy(t, m, job, target); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(source, "sub"))
	if err := runCopy(t, m, job, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "b/docs/sub")); !os.IsNotExist(err) {
		t.Fatal("deleted folder still in the copy")
	}
	if data, _ := os.ReadFile(filepath.Join(target, "b/_gelöscht/2026-10-01/docs/sub/gone")); string(data) != "g" {
		t.Fatal("deleted file not kept aside")
	}
	// A share that is not there leaves its copy alone.
	m.Sources = func() []Source { return nil }
	job.Sources = []string{"docs", "other"}
	m.Sources = func() []Source { return []Source{{Name: "other", Volume: "tank", Path: t.TempDir()}} }
	if err := runCopy(t, m, job, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "b/docs/keep")); err != nil {
		t.Fatal("copy of a missing share was touched")
	}
	// After the days, what was set aside goes for good.
	*now = now.AddDate(0, 0, 8)
	if err := runCopy(t, m, job, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "b/_gelöscht/2026-10-01")); !os.IsNotExist(err) {
		t.Fatal("old deletions not emptied")
	}
}

func mustRoot(t *testing.T, dir string) *os.Root {
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root
}

func TestCopyTakesWholeVolumesAndFoldersOutsideShares(t *testing.T) {
	m, _, target, _ := copyEnv(t)
	vol := filepath.Join(m.mountRoot, "tank")
	for _, dir := range []string{"scans/alt", ".titanos/backup", ".snapshots/x", "appdata"} {
		os.MkdirAll(filepath.Join(vol, dir), 0o700)
	}
	os.WriteFile(filepath.Join(vol, "scans/alt/a.pdf"), []byte("a"), 0o600)
	os.WriteFile(filepath.Join(vol, ".titanos/backup/secret"), []byte("s"), 0o600)
	os.WriteFile(filepath.Join(vol, ".snapshots/x/old"), []byte("o"), 0o600)
	os.WriteFile(filepath.Join(vol, "appdata/db"), []byte("d"), 0o600)
	mounts := filepath.Join(t.TempDir(), "mounts")
	os.WriteFile(mounts, []byte("/dev/md0 "+vol+" ext4 rw 0 0\n"), 0o644)
	m.mountsFile = mounts
	m.Sources = func() []Source { return nil }

	job := Job{ID: "copy", Places: []string{"tank/scans"}, Destination: Destination{Type: "usb-copy", Folder: "b"}}
	if err := runCopy(t, m, job, target); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(target, "b/Volume-tank/scans/alt/a.pdf")); string(data) != "a" {
		t.Fatal("folder outside shares not copied")
	}
	if _, err := os.Stat(filepath.Join(target, "b/Volume-tank/appdata")); !os.IsNotExist(err) {
		t.Fatal("more than the chosen folder copied")
	}
	job.Places, job.Volumes = nil, []string{"tank"}
	if err := runCopy(t, m, job, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "b/Volume-tank/appdata/db")); err != nil {
		t.Fatal("whole volume not copied")
	}
	for _, never := range []string{".titanos", ".snapshots"} {
		if _, err := os.Stat(filepath.Join(target, "b/Volume-tank", never)); !os.IsNotExist(err) {
			t.Fatalf("%s copied", never)
		}
	}
	if checkSelection(Job{Volumes: []string{"tank"}, Places: []string{"tank/scans"}}) == nil {
		t.Fatal("folder on a whole volume accepted twice")
	}
	if checkSelection(Job{Places: []string{"tank/.titanos"}}) == nil {
		t.Fatal("private data accepted")
	}
}

func TestResticSelectionOfVolumesAndLXC(t *testing.T) {
	m, _, _, _ := copyEnv(t)
	vol := filepath.Join(m.mountRoot, "tank")
	os.MkdirAll(filepath.Join(vol, "lxc"), 0o700)
	mounts := filepath.Join(t.TempDir(), "mounts")
	os.WriteFile(mounts, []byte("/dev/md0 "+vol+" ext4 rw 0 0\n"), 0o644)
	m.mountsFile = mounts
	sel := m.selection(Job{Volumes: []string{"tank", "gone"}, Extras: []string{"lxc"}, Destination: Destination{Type: "s3"}})
	if !slices.Contains(sel.paths, vol) || !slices.Contains(sel.paths, filepath.Join(vol, "lxc")) {
		t.Fatalf("paths %v", sel.paths)
	}
	if !slices.Contains(sel.excludes, filepath.Join(vol, ".titanos")) || !slices.Contains(sel.missing, "gone") {
		t.Fatalf("excludes %v missing %v", sel.excludes, sel.missing)
	}
}
