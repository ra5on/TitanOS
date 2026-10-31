package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// snapRunner makes and deletes snapshot folders and tells their creation time.
type snapRunner struct {
	commands []string
	created  map[string]string
}

func (r *snapRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	r.commands = append(r.commands, name+" "+strings.Join(args, " "))
	if name != "btrfs" || len(args) < 3 {
		return "", nil
	}
	switch args[1] {
	case "snapshot":
		return "", os.MkdirAll(args[len(args)-1], 0o700)
	case "delete":
		return "", os.RemoveAll(args[len(args)-1])
	case "show":
		return "Name: x\n\tCreation time: \t\t" + r.created[filepath.Base(args[2])] + "\n\tUUID: 01234567-89ab-cdef-0123-456789abcdef\n", nil
	}
	return "", nil
}

func shadowManager(t *testing.T) (*Manager, *snapRunner, string) {
	t.Helper()
	m, _, _ := newTestManager(t)
	runner := &snapRunner{created: map[string]string{}}
	m.runner = runner
	m.volumes = []VolumeConfig{{Name: "pool", Level: "raid1", Filesystem: "btrfs", FilesystemUUID: "fs"}, {Name: "data", Level: "raid1", Filesystem: "ext4"}}
	point := m.MountPoint("pool")
	os.MkdirAll(filepath.Join(point, ".titanos", "snapshots"), 0o700)
	m.mounts = func() map[string]bool { return map[string]bool{point: true, m.MountPoint("data"): true} }
	return m, runner, point
}

func TestPreviousVersionsMirrorSnapshots(t *testing.T) {
	m, runner, point := shadowManager(t)
	ctx := context.Background()
	if m.ShadowDir("data") != "" || m.ShadowDir("pool") != filepath.Join(point, ".snapshots") {
		t.Fatal("shadow folder only for Btrfs")
	}
	snap := func(name, created string) {
		os.Mkdir(filepath.Join(point, ".titanos", "snapshots", name), 0o700)
		runner.created[name] = created
	}
	snap("before-update", "2026-10-27 13:00:00 +0100")
	snap("auto-daily-20261027T120000-000000001", "2026-10-27 13:00:00 +0100") // same second
	m.SyncShadowCopies(ctx, []string{"pool"})
	entries, _ := os.ReadDir(filepath.Join(point, ".snapshots"))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, " ") != "@GMT-2026.10.27-12.00.00 @GMT-2026.10.27-12.00.01" {
		t.Fatalf("mirrors: %v", names)
	}
	if info, _ := os.Stat(filepath.Join(point, ".snapshots")); info.Mode().Perm() != 0o755 {
		t.Fatalf("shadow folder mode %v: Samba reads it as the user", info.Mode().Perm())
	}
	// A second sync changes nothing.
	before := len(runner.commands)
	m.SyncShadowCopies(ctx, []string{"pool"})
	for _, c := range runner.commands[before:] {
		if !strings.Contains(c, "show") {
			t.Fatalf("second sync changed something: %v", runner.commands[before:])
		}
	}
	// The snapshot goes, its mirror follows.
	os.RemoveAll(filepath.Join(point, ".titanos", "snapshots", "before-update"))
	m.SyncShadowCopies(ctx, []string{"pool"})
	if entries, _ := os.ReadDir(filepath.Join(point, ".snapshots")); len(entries) != 1 {
		t.Fatalf("mirror of a deleted snapshot kept: %v", entries)
	}
	// No share wants previous versions any more: all mirrors go.
	m.SyncShadowCopies(ctx, nil)
	if _, err := os.Stat(filepath.Join(point, ".snapshots")); !os.IsNotExist(err) {
		t.Fatal("shadow folder kept although nothing wants it")
	}
}

func TestLockedSnapshotsStay(t *testing.T) {
	m, runner, point := shadowManager(t)
	ctx := context.Background()
	m.clock = func() time.Time { return time.Now() }
	for range 3 {
		if err := m.ScheduledSnapshot(ctx, "pool", "daily", 1, 30, nil); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	entries, _ := os.ReadDir(filepath.Join(point, ".titanos", "snapshots"))
	if len(entries) != 3 {
		t.Fatalf("retention deleted locked snapshots: %d left", len(entries))
	}
	name := entries[0].Name()
	if err := m.SnapshotAction(ctx, "pool", name, "delete", name); !errors.Is(err, ErrLocked) {
		t.Fatalf("locked snapshot deleted by hand: %v", err)
	}
	// After the lock the retention catches up.
	m.clock = func() time.Time { return time.Now().AddDate(0, 0, 31) }
	if err := m.ScheduledSnapshot(ctx, "pool", "daily", 1, 0, nil); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(filepath.Join(point, ".titanos", "snapshots")); len(entries) != 1 {
		t.Fatalf("retention after the lock: %d left (%v)", len(entries), runner.commands)
	}
}
