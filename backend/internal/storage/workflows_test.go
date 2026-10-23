package storage

import (
	"context"
	"errors"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrowthJournalFencesOtherMutationsAfterRestart(t *testing.T) {
	m, runner, store := newTestManager(t)
	p := GrowthPointer{ID: "0123456789abcdef01234567", Volume: "data", RecoveryVolume: "safe"}
	store.WriteJSON(growthFile, p)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if !m.GrowthPending() {
		t.Fatal("growth pointer lost")
	}
	if err := m.Create(context.Background(), CreateRequest{}); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if err := m.AddDisk(context.Background(), "data", "sde", true); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if len(runner.commands) != 0 {
		t.Fatal("mutations ran despite interrupted growth")
	}
	_, pending, err := m.GrowthStatus()
	if !pending || err == nil {
		t.Fatal("missing recovery volume hidden")
	}
	if err := m.RecoverGrowth(context.Background(), "data"); err == nil {
		t.Fatal("recovered without original backup")
	}
}
func snapshotFilesFixture(t *testing.T) (*Manager, string) {
	t.Helper()
	m, runner, _ := newTestManager(t)
	m.volumes = []VolumeConfig{{Name: "data", Filesystem: "btrfs"}, {Name: "safe", Filesystem: "xfs"}}
	for _, name := range []string{"data", "safe"} {
		point := m.MountPoint(name)
		os.MkdirAll(point, 0700)
		runner.mounted[point] = true
	}
	dir, err := m.snapshotDir("data")
	if err != nil {
		t.Fatal(err)
	}
	snap := filepath.Join(dir, "manual")
	os.MkdirAll(filepath.Join(snap, "photos"), 0700)
	os.WriteFile(filepath.Join(snap, "photos", "a.jpg"), []byte("photo"), 0600)
	os.MkdirAll(filepath.Join(snap, ".titanos", "assistant"), 0700)
	os.WriteFile(filepath.Join(snap, ".titanos", "assistant", "private"), []byte("secret"), 0600)
	os.Symlink("/etc", filepath.Join(snap, "link"))
	return m, snap
}
func TestSnapshotBrowseRestoreExcludesPrivateTreesAndNeverOverwrites(t *testing.T) {
	m, _ := snapshotFilesFixture(t)
	ctx := context.Background()
	listing, err := m.SnapshotFiles(ctx, "data", "manual", "", 0)
	if err != nil || len(listing.Entries) != 1 || listing.Entries[0].Path != "photos" {
		t.Fatal(listing, err)
	}
	for _, bad := range []string{"../etc", ".titanos/assistant", "link/passwd", "/etc/passwd"} {
		if _, err := m.SnapshotFiles(ctx, "data", "manual", bad, 0); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	out, err := m.RestoreSnapshotFiles(ctx, "data", "manual", SnapshotRestore{Paths: []string{"photos/a.jpg"}, TargetVolume: "safe"})
	if err != nil || out.Files != 1 || out.Bytes != 5 {
		t.Fatal(out, err)
	}
	b, err := os.ReadFile(filepath.Join(m.MountPoint("safe"), out.Folder, "photos", "a.jpg"))
	if err != nil || string(b) != "photo" {
		t.Fatal(string(b), err)
	}
	if _, err := m.RestoreSnapshotFiles(ctx, "data", "manual", SnapshotRestore{Paths: []string{".titanos/assistant/private"}, TargetVolume: "safe"}); err == nil {
		t.Fatal("private chat restored")
	}
}
func TestUsageIsBoundedAndDoesNotFollowLinksOrExposePrivateState(t *testing.T) {
	m, _ := snapshotFilesFixture(t)
	os.WriteFile(filepath.Join(m.MountPoint("data"), "public"), []byte("abc"), 0600)
	os.Symlink(t.TempDir(), filepath.Join(m.MountPoint("data"), "outside"))
	root, _ := volumedata.Open(m.mountRoot, "data", "monitoring", m.Mounted())
	root.WriteFile("secret", []byte("hidden"), 0600)
	root.Close()
	out, err := m.Usage(context.Background(), "data")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range out.Entries {
		if strings.Contains(e.Name, "monitoring") || strings.Contains(e.Name, "assistant") {
			t.Fatal("private paths exposed", out)
		}
	}
	if out.Scanned == 0 {
		t.Fatal("nothing scanned")
	}
}

func TestUsageSeparatesRecycleWithoutCountingItsFilesTwice(t *testing.T) {
	m, _ := snapshotFilesFixture(t)
	os.MkdirAll(filepath.Join(m.MountPoint("data"), "Photos", ".recycle"), 0700)
	os.WriteFile(filepath.Join(m.MountPoint("data"), "Photos", "live"), []byte("abc"), 0600)
	os.WriteFile(filepath.Join(m.MountPoint("data"), "Photos", ".recycle", "old"), []byte("old-data"), 0600)
	report, err := m.Usage(context.Background(), "data")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]UsageEntry{}
	for _, entry := range report.Entries {
		found[entry.Name] = entry
	}
	if found["Photos"].Bytes != 3 || found["Photos/.recycle"].Bytes != 8 || found["Photos/.recycle"].Kind != "recycle" {
		t.Fatal(found)
	}
}

type scheduleSnapshotRunner struct{ base *fakeRunner }

func (r scheduleSnapshotRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if name == "btrfs" && len(args) > 2 && args[0] == "subvolume" {
		target := args[len(args)-1]
		switch args[1] {
		case "snapshot":
			return "", os.Mkdir(target, 0700)
		case "delete":
			return "", os.RemoveAll(target)
		}
	}
	return r.base.Run(ctx, stdin, name, args...)
}
func TestScheduledRetentionNeverEnrollsManualNames(t *testing.T) {
	m, _ := snapshotFilesFixture(t)
	base := m.runner.(*fakeRunner)
	m.runner = scheduleSnapshotRunner{base}
	dir, err := m.snapshotDir("data")
	if err != nil {
		t.Fatal(err)
	}
	manual := "auto-test-20000101T000000-000000000"
	os.Mkdir(filepath.Join(dir, manual), 0700)
	for range 2 {
		if err := m.ScheduledSnapshot(context.Background(), "data", "test", 1); err != nil {
			t.Fatal(err)
		}
	}
	list, err := m.Snapshots("data")
	if err != nil {
		t.Fatal(err)
	}
	automatic := 0
	for _, s := range list {
		if strings.HasPrefix(s.Name, "auto-test-") {
			automatic++
		}
	}
	if automatic != 2 {
		t.Fatal("manual automatic-looking name was pruned", list)
	}
	if _, err := os.Stat(filepath.Join(dir, manual)); err != nil {
		t.Fatal(err)
	}
}
