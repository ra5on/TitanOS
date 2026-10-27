package lxc

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Snapshots of a container are read-only Btrfs snapshots of its root
// filesystem in /mnt/<volume>/lxc/<name>/snapshots/<id>. They exist only
// for containers made on a Btrfs volume, whose root filesystem is a
// subvolume.

// Snapshot is a saved state of a container's root filesystem.
type Snapshot struct {
	ID      string    `json:"id"`
	Created time.Time `json:"created"`
}

func (m *Manager) snapshotDir(c Container) string {
	return filepath.Join(m.dir(c), "snapshots")
}

func (m *Manager) snapshotNames(c Container) ([]string, error) {
	entries, err := os.ReadDir(m.snapshotDir(c))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && snapshotRE.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// snapshotWork marks snapshot work as running, so that app updates and
// maintenance wait for it.
func (m *Manager) snapshotWork() func() {
	m.mu.Lock()
	m.snapshotOps++
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		m.snapshotOps--
		m.mu.Unlock()
	}
}

// Snapshots lists the snapshots of a container, newest first.
func (m *Manager) Snapshots(name string) ([]Snapshot, error) {
	c, err := m.get(name)
	if err != nil {
		return nil, err
	}
	if !c.Btrfs {
		return []Snapshot{}, nil
	}
	names, err := m.snapshotNames(c)
	if err != nil {
		return nil, err
	}
	list := make([]Snapshot, 0, len(names))
	for i := len(names) - 1; i >= 0; i-- {
		created, _ := time.ParseInLocation("20060102-150405", names[i], time.UTC)
		list = append(list, Snapshot{ID: names[i], Created: created})
	}
	return list, nil
}

func (m *Manager) snapshotTarget(name string) (Container, *syncUnlock, error) {
	if m.Disabled {
		return Container{}, nil, ErrDisabled
	}
	lock, err := m.operation(name)
	if err != nil {
		return Container{}, nil, err
	}
	c, err := m.get(name)
	if err != nil {
		lock.Unlock()
		return Container{}, nil, err
	}
	if m.creating(name) {
		lock.Unlock()
		return Container{}, nil, ErrCreating
	}
	if !c.Btrfs {
		lock.Unlock()
		return Container{}, nil, ErrNoSnapshots
	}
	return c, &syncUnlock{lock.Unlock, m.snapshotWork()}, nil
}

type syncUnlock struct{ unlock, done func() }

func (s *syncUnlock) release() { s.done(); s.unlock() }

// CreateSnapshot saves the root filesystem of a container. A running
// container is saved as it is on disk at that moment.
func (m *Manager) CreateSnapshot(ctx context.Context, name string) (Snapshot, error) {
	c, held, err := m.snapshotTarget(name)
	if err != nil {
		return Snapshot{}, err
	}
	defer held.release()
	if err := os.Mkdir(m.snapshotDir(c), 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return Snapshot{}, err
	}
	created := m.now().UTC().Truncate(time.Second)
	id := created.Format("20060102-150405")
	target := filepath.Join(m.snapshotDir(c), id)
	if _, err := os.Lstat(target); err == nil {
		return Snapshot{}, ErrBusy
	}
	if _, err := m.runner.Run(ctx, "", "btrfs", "subvolume", "snapshot", "-r", m.rootfs(c), target); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{ID: id, Created: created}, nil
}

func (m *Manager) snapshotPath(c Container, id string) (string, error) {
	if !snapshotRE.MatchString(id) {
		return "", ErrSnapshotNotFound
	}
	path := filepath.Join(m.snapshotDir(c), id)
	if info, err := os.Lstat(path); err != nil || !info.IsDir() {
		return "", ErrSnapshotNotFound
	}
	return path, nil
}

// DeleteSnapshot removes a snapshot.
func (m *Manager) DeleteSnapshot(ctx context.Context, name, id string) error {
	c, held, err := m.snapshotTarget(name)
	if err != nil {
		return err
	}
	defer held.release()
	path, err := m.snapshotPath(c, id)
	if err != nil {
		return err
	}
	_, err = m.runner.Run(ctx, "", "btrfs", "subvolume", "delete", path)
	return err
}

// RestoreSnapshot puts the root filesystem of a stopped container back to
// a snapshot. The snapshot stays; the state before is gone.
func (m *Manager) RestoreSnapshot(ctx context.Context, name, id string) error {
	c, held, err := m.snapshotTarget(name)
	if err != nil {
		return err
	}
	defer held.release()
	if m.running(name) != "" {
		return ErrRunning
	}
	path, err := m.snapshotPath(c, id)
	if err != nil {
		return err
	}
	rootfs := m.rootfs(c)
	restored, old := rootfs+".restore", rootfs+".old"
	for _, leftover := range []string{restored, old} {
		if _, err := os.Lstat(leftover); err == nil {
			// Left behind by a restore that broke off: a subvolume or empty.
			if _, err := m.runner.Run(ctx, "", "btrfs", "subvolume", "delete", leftover); err != nil {
				return err
			}
		}
	}
	// A writable copy first; the current state goes only once it exists.
	if _, err := m.runner.Run(ctx, "", "btrfs", "subvolume", "snapshot", path, restored); err != nil {
		return err
	}
	if err := os.Rename(rootfs, old); err != nil {
		return err
	}
	if err := os.Rename(restored, rootfs); err != nil {
		os.Rename(old, rootfs)
		return err
	}
	_, err = m.runner.Run(ctx, "", "btrfs", "subvolume", "delete", old)
	return err
}
