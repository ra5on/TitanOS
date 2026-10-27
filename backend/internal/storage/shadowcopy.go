package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// Previous versions for Windows. Samba's shadow_copy2 needs snapshots named
// @GMT-YYYY.MM.DD-HH.MM.SS in a folder the connected user can traverse.
// The snapshots of TitanOS live in the private .titanos/snapshots (root
// only) under names chosen by people or schedules, so for every one of them
// a read-only snapshot of the snapshot is kept in /mnt/<volume>/.snapshots
// under its @GMT name. A snapshot of a read-only snapshot shares all of its
// blocks; it costs no space. The files inside keep their owners and modes,
// and Samba shows a share only its own folder of each snapshot.
//
// .titanos/shadowcopies/index.json maps @GMT names to the snapshots they
// mirror. Mirrors whose snapshot is gone are deleted on the next sync, and
// all of them when no share of the volume wants previous versions.

const shadowFolder = ".snapshots"

var gmtName = regexp.MustCompile(`^@GMT-\d{4}\.\d{2}\.\d{2}-\d{2}\.\d{2}\.\d{2}$`)

// ShadowDir is the folder with the @GMT snapshots of a Btrfs volume, "" for
// other volumes.
func (m *Manager) ShadowDir(volume string) string {
	v, ok := m.find(volume)
	if !ok || v.Filesystem != "btrfs" {
		return ""
	}
	return filepath.Join(m.mountRoot, volume, shadowFolder)
}

// SyncShadowCopies brings the @GMT mirrors of every mounted Btrfs volume in
// line with its snapshots; wanted lists the volumes that offer previous
// versions. It reads only the two snapshot folders.
func (m *Manager) SyncShadowCopies(ctx context.Context, wanted []string) {
	if m.ReadOnly {
		return
	}
	m.actions.Lock()
	defer m.actions.Unlock()
	mounted := m.Mounted()
	for _, volume := range m.configured() {
		if volume.Filesystem != "btrfs" || !slices.Contains(mounted, volume.Name) {
			continue
		}
		if err := m.syncShadow(ctx, volume.Name, slices.Contains(wanted, volume.Name), mounted); err != nil {
			slog.Warn("previous versions not brought up to date", "volume", volume.Name, "err", err)
		}
	}
}

func (m *Manager) syncShadow(ctx context.Context, volume string, wanted bool, mounted []string) error {
	root, err := volumedata.Open(m.mountRoot, volume, "shadowcopies", mounted)
	if err != nil {
		return err
	}
	defer root.Close()
	index := map[string]string{}
	if err := volumedata.ReadJSON(root, "index.json", &index, 1<<20); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !wanted && len(index) == 0 {
		return nil
	}
	dir := filepath.Join(m.mountRoot, volume, shadowFolder)
	snapshots := filepath.Join(m.mountRoot, volume, ".titanos", "snapshots")
	changed := false
	remove := func(gmt string) {
		if !gmtName.MatchString(gmt) {
			delete(index, gmt)
			return
		}
		if _, err := os.Lstat(filepath.Join(dir, gmt)); err == nil {
			if _, err := m.runner.Run(ctx, "", "btrfs", "subvolume", "delete", filepath.Join(dir, gmt)); err != nil {
				slog.Warn("previous version not deleted", "volume", volume, "name", gmt, "err", err)
				return
			}
		}
		delete(index, gmt)
		changed = true
	}
	for gmt, original := range index {
		if _, err := os.Lstat(filepath.Join(snapshots, original)); !wanted || err != nil {
			remove(gmt)
		}
	}
	if wanted {
		if err := os.Mkdir(dir, 0o755); err != nil && !os.IsExist(err) {
			return err
		}
		// Samba reads it as the connected user: traversable, not writable.
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is not a folder", dir)
		}
		os.Chmod(dir, 0o755)
		entries, _ := os.ReadDir(snapshots)
		mirrored := map[string]bool{}
		for _, original := range index {
			mirrored[original] = true
		}
		for _, entry := range entries {
			if !entry.IsDir() || mirrored[entry.Name()] || !snapshotName.MatchString(entry.Name()) {
				continue
			}
			created, err := m.snapshotCreated(ctx, filepath.Join(snapshots, entry.Name()))
			if err != nil {
				slog.Warn("snapshot time unknown, no previous version", "volume", volume, "snapshot", entry.Name(), "err", err)
				continue
			}
			gmt := created.UTC().Format("@GMT-2006.01.02-15.04.05")
			for index[gmt] != "" || exists(filepath.Join(dir, gmt)) {
				created = created.Add(time.Second)
				gmt = created.UTC().Format("@GMT-2006.01.02-15.04.05")
			}
			if _, err := m.runner.Run(ctx, "", "btrfs", "subvolume", "snapshot", "-r", filepath.Join(snapshots, entry.Name()), filepath.Join(dir, gmt)); err != nil {
				slog.Warn("previous version not created", "volume", volume, "snapshot", entry.Name(), "err", err)
				continue
			}
			index[gmt] = entry.Name()
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if len(index) == 0 && !wanted {
		os.Remove(dir)
	}
	return volumedata.WriteJSON(root, "index.json", index)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// snapshotCreated reads the creation time of a subvolume.
func (m *Manager) snapshotCreated(ctx context.Context, path string) (time.Time, error) {
	output, err := m.runner.Run(ctx, "", "btrfs", "subvolume", "show", path)
	if err != nil {
		return time.Time{}, err
	}
	for line := range strings.SplitSeq(output, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "Creation time:"); ok {
			return time.Parse("2006-01-02 15:04:05 -0700", strings.TrimSpace(value))
		}
	}
	return time.Time{}, errors.New("no creation time")
}

// --- locked snapshots ------------------------------------------------------

// ErrLocked means a snapshot may not be deleted yet.
var ErrLocked = errors.New("storage: the snapshot is locked")

// Snapshots of a schedule with a lock (Job.LockDays) may not be deleted
// before then, neither by hand nor by the retention: ransomware with the
// rights of an administrator account cannot remove them through TitanOS.
// .titanos/snaplocks/locks.json maps snapshot names to the end of the lock.

func (m *Manager) lockSnapshot(volume, name string, until time.Time) error {
	root, err := volumedata.Open(m.mountRoot, volume, "snaplocks", m.Mounted())
	if err != nil {
		return err
	}
	defer root.Close()
	locks := map[string]time.Time{}
	if err := volumedata.ReadJSON(root, "locks.json", &locks, 1<<20); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for old, end := range locks {
		if end.Before(m.now()) {
			delete(locks, old)
		}
	}
	locks[name] = until.UTC()
	return volumedata.WriteJSON(root, "locks.json", locks)
}

// lockedUntil reports the end of a snapshot's lock, zero if it has none.
func (m *Manager) lockedUntil(volume, name string) time.Time {
	root, err := volumedata.Open(m.mountRoot, volume, "snaplocks", m.Mounted())
	if err != nil {
		return time.Time{}
	}
	defer root.Close()
	locks := map[string]time.Time{}
	if volumedata.ReadJSON(root, "locks.json", &locks, 1<<20) != nil {
		return time.Time{}
	}
	if end := locks[name]; end.After(m.now()) {
		return end
	}
	return time.Time{}
}
