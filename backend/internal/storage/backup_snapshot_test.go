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

// A snapshot left by a restart or power cut holds space for good: it goes
// once the volume is mounted after a start and when a copy run begins,
// without another snapshot having to be made.
func TestLeftoverBackupSnapshotsAreRemovedAtStartAndBeforeARun(t *testing.T) {
	m, runner, point := shadowManager(t)
	ctx := context.Background()
	dir := filepath.Join(point, ".titanos", backupSourceFolder)
	leftover := func(name string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	gone := func(path string) bool { _, err := os.Stat(path); return os.IsNotExist(err) }

	// Nothing there: the volume is not touched, no folder is made.
	m.settled = true
	m.cleanBackupSnapshotsOnce(ctx)
	if !gone(dir) || len(runner.commands) != 0 {
		t.Fatalf("a volume without leftovers was changed: %v", runner.commands)
	}
	m.mu.Lock()
	m.backupCleaned = nil
	m.mu.Unlock()

	// After a start.
	first := leftover("20261101T000000-AAAAAAAA")
	m.cleanBackupSnapshotsOnce(ctx)
	if !gone(first) {
		t.Fatal("leftover not removed after the start")
	}
	// Once per start: later rounds of the loop leave the volume alone.
	second := leftover("20261102T000000-BBBBBBBB")
	before := len(runner.commands)
	m.cleanBackupSnapshotsOnce(ctx)
	if gone(second) || len(runner.commands) != before {
		t.Fatal("the volume was looked at again in a later round")
	}
	// When a copy run begins.
	if err := m.CleanBackupSnapshots(ctx, "pool"); err != nil || !gone(second) {
		t.Fatalf("leftover not removed before a run: %v", err)
	}
	// A snapshot in use stays.
	used, release, err := m.BackupSnapshot(ctx, "pool")
	if err != nil {
		t.Fatal(err)
	}
	third := leftover("20261103T000000-CCCCCCCC")
	if err := m.CleanBackupSnapshots(ctx, "pool"); err != nil || !gone(third) || gone(used) {
		t.Fatalf("cleanup removed a snapshot in use or kept a leftover: %v", err)
	}
	release()
	// Other volumes: nothing happens, no error.
	for _, volume := range []string{"data", "unknown"} {
		if err := m.CleanBackupSnapshots(ctx, volume); err != nil {
			t.Fatalf("%s: %v", volume, err)
		}
	}
}

// Other storage work holds the lock: a cancelled backup does not hang
// behind it, and releasing a snapshot gives up after its limit.
func TestBackupSnapshotHonoursTheContext(t *testing.T) {
	m, _, point := shadowManager(t)
	dir, release, err := m.BackupSnapshot(context.Background(), "pool")
	if err != nil {
		t.Fatal(err)
	}
	m.actions.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, _, err := m.BackupSnapshot(ctx, "pool"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("snapshot behind held storage work: %v", err)
	}
	if err := m.CleanBackupSnapshots(ctx, "pool"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cleanup behind held storage work: %v", err)
	}
	backupReleaseLimit = 100 * time.Millisecond
	t.Cleanup(func() { backupReleaseLimit = 2 * time.Minute })
	done := make(chan struct{})
	go func() { release(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("release hung behind held storage work")
	}
	m.actions.Unlock()
	// Not deleted in time: a leftover, removed by the next cleanup.
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("fixture: the snapshot should still be there")
	}
	if err := m.CleanBackupSnapshots(context.Background(), "pool"); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(filepath.Join(point, ".titanos", backupSourceFolder)); len(entries) != 0 {
		t.Fatalf("leftover kept: %v", entries)
	}
}
