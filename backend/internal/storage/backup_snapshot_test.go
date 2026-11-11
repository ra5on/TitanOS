package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A backup gets a read-only snapshot of a Btrfs volume in a private folder
// of its own and removes it again; what an earlier run left is cleaned up.
func TestBackupSnapshotIsMadeAndRemoved(t *testing.T) {
	m, runner, point := shadowManager(t)
	ctx := context.Background()
	if _, _, err := m.BackupSnapshot(ctx, "data"); !errors.Is(err, ErrNotPossible) {
		t.Fatalf("snapshot of an ext4 volume: %v", err)
	}
	if _, _, err := m.BackupSnapshot(ctx, "gone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("snapshot of an unknown volume: %v", err)
	}
	// Left behind by a run that was cut short.
	stale := filepath.Join(point, ".titanos", backupSourceFolder, "20261101T000000-OLD")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	dir, release, err := m.BackupSnapshot(ctx, "pool")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dir) != filepath.Join(point, ".titanos", backupSourceFolder) {
		t.Fatalf("snapshot at %s", dir)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("no snapshot: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("leftover of an earlier run not removed")
	}
	if want := "btrfs subvolume snapshot -r " + point + " " + dir; !strings.Contains(strings.Join(runner.commands, "\n"), want) {
		t.Fatalf("no read-only snapshot of the volume: %v", runner.commands)
	}
	// Not among the snapshots of the user, and not a previous version.
	if list, err := m.Snapshots("pool"); err != nil || len(list) != 0 {
		t.Fatalf("listed as a snapshot: %v %v", list, err)
	}
	// A second run at the same time leaves the first one's snapshot alone.
	second, releaseSecond, err := m.BackupSnapshot(ctx, "pool")
	if err != nil || second == dir {
		t.Fatalf("second snapshot: %q %v", second, err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("a snapshot in use was removed by another run")
	}
	release()
	releaseSecond()
	if entries, _ := os.ReadDir(filepath.Dir(dir)); len(entries) != 0 {
		t.Fatalf("snapshots left after the runs: %v", entries)
	}
	m.ReadOnly = true
	if _, _, err := m.BackupSnapshot(ctx, "pool"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("development mode: %v", err)
	}
}
