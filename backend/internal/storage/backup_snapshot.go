package storage

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// backupSourceFolder holds the snapshots backups read from, below
// .titanos on the volume. They are no snapshots of the user: not listed,
// not offered as previous versions, never kept beyond their run.
const backupSourceFolder = "backup-source"

// BackupSnapshot makes a read-only snapshot of a mounted Btrfs volume for
// one run of a backup, so that files that change while they are read
// (disks of running VMs) can be copied as they were at one moment. It
// returns the folder of the snapshot (the volume's own subvolume, without
// the subvolumes below it) and release, which deletes the snapshot again.
// ErrNotPossible for a volume that is not Btrfs or not mounted.
//
// A snapshot a run could not delete (titand stopped, power cut) is deleted
// the next time one is made for the volume.
func (m *Manager) BackupSnapshot(ctx context.Context, volume string) (string, func(), error) {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return "", nil, ErrReadOnly
	}
	if m.GrowthPending() {
		return "", nil, ErrBusy
	}
	config, ok := m.find(volume)
	if !ok {
		return "", nil, ErrNotFound
	}
	if config.Filesystem != "btrfs" || !slices.Contains(m.Mounted(), volume) {
		return "", nil, ErrNotPossible
	}
	base := filepath.Join(m.MountPoint(volume), ".titanos")
	if err := privateDir(base); err != nil {
		return "", nil, err
	}
	dir := filepath.Join(base, backupSourceFolder)
	if err := privateDir(dir); err != nil {
		return "", nil, err
	}
	m.mu.Lock()
	if m.backupSnapshots == nil {
		m.backupSnapshots = map[string]bool{}
	}
	m.mu.Unlock()
	// What an earlier run left behind.
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		m.mu.RLock()
		inUse := m.backupSnapshots[path]
		m.mu.RUnlock()
		if inUse || !entry.IsDir() {
			continue
		}
		if _, err := m.runner.Run(ctx, "", "btrfs", "subvolume", "delete", path); err != nil {
			slog.Warn("leftover snapshot of a backup not deleted", "volume", volume, "name", entry.Name(), "err", err)
		}
	}
	path := filepath.Join(dir, time.Now().UTC().Format("20060102T150405")+"-"+rand.Text()[:8])
	if _, err := m.runner.Run(ctx, "", "btrfs", "subvolume", "snapshot", "-r", m.MountPoint(volume), path); err != nil {
		return "", nil, fmt.Errorf("snapshot of %s for the backup: %w", volume, err)
	}
	m.mu.Lock()
	m.backupSnapshots[path] = true
	m.mu.Unlock()
	release := func() {
		// Also when the run was cancelled: the snapshot must not stay and
		// hold the space of everything that changes from now on.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		m.actions.Lock()
		defer m.actions.Unlock()
		if _, err := m.runner.Run(ctx, "", "btrfs", "subvolume", "delete", path); err != nil {
			slog.Warn("snapshot of a backup not deleted; the next backup of this volume deletes it", "volume", volume, "path", path, "err", err)
		}
		m.mu.Lock()
		delete(m.backupSnapshots, path)
		m.mu.Unlock()
	}
	return path, release, nil
}
