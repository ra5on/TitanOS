package storage

import (
	"context"
	"crypto/rand"
	"errors"
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

// lockActions takes the lock of storage work, or gives up when ctx ends:
// a backup that is cancelled must not hang behind a long storage action.
func (m *Manager) lockActions(ctx context.Context) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for !m.actions.TryLock() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return nil
}

// backupSourceDir is the folder of the backup snapshots of a mounted
// Btrfs volume; with create it is made (private), without it must exist.
func (m *Manager) backupSourceDir(volume string, create bool) (string, error) {
	config, ok := m.find(volume)
	if !ok {
		return "", ErrNotFound
	}
	if config.Filesystem != "btrfs" || !slices.Contains(m.Mounted(), volume) {
		return "", ErrNotPossible
	}
	base := filepath.Join(m.MountPoint(volume), ".titanos")
	dir := filepath.Join(base, backupSourceFolder)
	if !create {
		if _, err := os.Lstat(dir); err != nil {
			return "", err
		}
	}
	if err := privateDir(base); err != nil {
		return "", err
	}
	return dir, privateDir(dir)
}

// removeBackupLeftovers deletes the snapshots in dir no run uses. The
// caller holds actions.
func (m *Manager) removeBackupLeftovers(ctx context.Context, volume, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var errs []error
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
			errs = append(errs, err)
			continue
		}
		slog.Info("leftover snapshot of a backup deleted", "volume", volume, "name", entry.Name())
	}
	return errors.Join(errs...)
}

// CleanBackupSnapshots deletes the snapshots a backup left behind on a
// volume when titand stopped or the power went: left alone, such a
// snapshot would hold the space of everything that changes afterwards for
// good. It is called for every Btrfs volume once it is mounted after a
// start, and for the volumes of a readable copy when its run begins.
// Snapshots a running backup reads from stay. A volume without such a
// folder, or one that is not Btrfs, is left untouched.
func (m *Manager) CleanBackupSnapshots(ctx context.Context, volume string) error {
	if m.ReadOnly {
		return nil
	}
	if err := m.lockActions(ctx); err != nil {
		return err
	}
	defer m.actions.Unlock()
	dir, err := m.backupSourceDir(volume, false)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, ErrNotPossible) || errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return m.removeBackupLeftovers(ctx, volume, dir)
}

// cleanBackupSnapshotsOnce runs CleanBackupSnapshots for every mounted
// Btrfs volume that was not looked at since the start (Run, once settled).
func (m *Manager) cleanBackupSnapshotsOnce(ctx context.Context) {
	mounted := m.Mounted()
	for _, volume := range m.configured() {
		if volume.Filesystem != "btrfs" || !slices.Contains(mounted, volume.Name) {
			continue
		}
		m.mu.Lock()
		done := m.backupCleaned[volume.Name]
		m.mu.Unlock()
		if done {
			continue
		}
		wait, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := m.CleanBackupSnapshots(wait, volume.Name)
		cancel()
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			continue // storage work is running: the next round tries again
		}
		// Looked at once per start, also when a snapshot could not be
		// deleted: the next backup of the volume tries again.
		m.mu.Lock()
		if m.backupCleaned == nil {
			m.backupCleaned = map[string]bool{}
		}
		m.backupCleaned[volume.Name] = true
		m.mu.Unlock()
	}
}

// BackupSnapshot makes a read-only snapshot of a mounted Btrfs volume for
// one run of a backup, so that files that change while they are read
// (disks of running VMs) can be copied as they were at one moment. It
// returns the folder of the snapshot (the volume's own subvolume, without
// the subvolumes below it) and release, which deletes the snapshot again.
// ErrNotPossible for a volume that is not Btrfs or not mounted. ctx ends
// the wait for other storage work and the snapshot command.
//
// A snapshot a run could not delete (titand stopped, power cut) is deleted
// after the next start, when a readable copy of the volume begins and when
// the next snapshot is made (CleanBackupSnapshots).
func (m *Manager) BackupSnapshot(ctx context.Context, volume string) (string, func(), error) {
	if m.ReadOnly {
		return "", nil, ErrReadOnly
	}
	if err := m.lockActions(ctx); err != nil {
		return "", nil, err
	}
	defer m.actions.Unlock()
	if m.GrowthPending() {
		return "", nil, ErrBusy
	}
	dir, err := m.backupSourceDir(volume, true)
	if err != nil {
		return "", nil, err
	}
	m.mu.Lock()
	if m.backupSnapshots == nil {
		m.backupSnapshots = map[string]bool{}
	}
	m.mu.Unlock()
	_ = m.removeBackupLeftovers(ctx, volume, dir) // reported in the journal; the snapshot of this run does not depend on it
	path := filepath.Join(dir, time.Now().UTC().Format("20060102T150405")+"-"+rand.Text()[:8])
	if _, err := m.runner.Run(ctx, "", "btrfs", "subvolume", "snapshot", "-r", m.MountPoint(volume), path); err != nil {
		return "", nil, fmt.Errorf("snapshot of %s for the backup: %w", volume, err)
	}
	m.mu.Lock()
	m.backupSnapshots[path] = true
	m.mu.Unlock()
	release := func() {
		// Also when the run was cancelled: the snapshot must not stay and
		// hold the space of everything that changes from now on. Bounded:
		// what cannot be deleted in time is a leftover for the next cleanup.
		ctx, cancel := context.WithTimeout(context.Background(), backupReleaseLimit)
		defer cancel()
		err := m.lockActions(ctx)
		if err == nil {
			_, err = m.runner.Run(ctx, "", "btrfs", "subvolume", "delete", path)
			m.actions.Unlock()
		}
		if err != nil {
			slog.Warn("snapshot of a backup not deleted; the next cleanup deletes it", "volume", volume, "path", path, "err", err)
		}
		m.mu.Lock()
		delete(m.backupSnapshots, path)
		m.mu.Unlock()
	}
	return path, release, nil
}

// backupReleaseLimit bounds deleting the snapshot of a run; tests shorten it.
var backupReleaseLimit = 2 * time.Minute
