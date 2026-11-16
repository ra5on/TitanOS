package storage

import (
	"errors"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
	"golang.org/x/sys/unix"
)

// What a restore from a snapshot leaves when titand stops halfway (a power
// cut, an app update), and how the next start finds it (1.03):
//
//   - Single files are copied into .titanos/restores/<24 hex> on the target
//     volume (application restores of backups use the same folder and the
//     same kind of name). Up to 10 GiB, invisible.
//   - Folders are written under temporary names
//     .titanos-restore-<24 hex>.titanos-part next to where they belong;
//     each such job keeps a note .titanos/restores/folder-<id>.json naming
//     the deepest folder that existed when it started. Only below that
//     folder is looked. Restores started before 1.03 have no note; their
//     temporary entries are not found.
//
// Running work holds the folder .titanos/restores shared (flock); the sweep
// takes it exclusively and else tries again, so it never removes what a
// running restore writes. Once per volume and start of titand.
const (
	restoresFeature     = "restores"
	folderJournalPrefix = "folder-"
)

var (
	restoreStage   = regexp.MustCompile(`^[0-9a-f]{24}$`)
	folderJournal  = regexp.MustCompile(`^folder-[0-9a-f]{24}\.json$`)
	restoreTempRE  = regexp.MustCompile(`^\.titanos-restore-[0-9a-f]{24}\.titanos-part$`)
	restoreSweepMu sync.Mutex
)

// cleanRestoresOnce sweeps every mounted volume not swept since the start
// in the background (Run, once settled).
func (m *Manager) cleanRestoresOnce() {
	for _, volume := range m.Mounted() {
		m.mu.Lock()
		if m.restoresCleaned == nil {
			m.restoresCleaned = map[string]bool{}
		}
		started := m.restoresCleaned[volume]
		m.restoresCleaned[volume] = true
		m.mu.Unlock()
		if started {
			continue
		}
		go func() {
			if !m.cleanRestores(volume) {
				// Busy or failed: the next round tries again.
				m.mu.Lock()
				delete(m.restoresCleaned, volume)
				m.mu.Unlock()
			}
		}()
	}
}

// cleanRestores removes what interrupted restores left on a volume. It
// reports whether the volume was looked at (false: a restore runs, or the
// folder could not be read).
func (m *Manager) cleanRestores(volume string) bool {
	restoreSweepMu.Lock()
	defer restoreSweepMu.Unlock()
	root, err := volumedata.OpenExisting(m.mountRoot, volume, restoresFeature, m.Mounted())
	if errors.Is(err, os.ErrNotExist) {
		return true // nothing was ever restored here
	}
	if err != nil {
		slog.Warn("leftovers of restores not looked at", "volume", volume, "err", err)
		return false
	}
	defer root.Close()
	release, ok, err := volumedata.TryExclusive(root)
	if err != nil || !ok {
		return false
	}
	defer release()
	dir, err := root.Open(".")
	if err != nil {
		return false
	}
	entries, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return false
	}
	done := true
	for _, e := range entries {
		switch {
		case restoreStage.MatchString(e.Name()) && e.Type() == os.ModeDir:
			if err := root.RemoveAll(e.Name()); err != nil {
				slog.Warn("staging folder of an interrupted restore not removed", "volume", volume, "folder", e.Name(), "err", err)
				done = false
				continue
			}
			slog.Info("staging folder of an interrupted restore removed", "volume", volume, "folder", e.Name())
		case folderJournal.MatchString(e.Name()) && e.Type().IsRegular():
			var note restoreJournal
			if err := volumedata.ReadJSON(root, e.Name(), &note, 64<<10); err != nil {
				slog.Warn("note of an interrupted folder restore not readable", "volume", volume, "note", e.Name(), "err", err)
				continue
			}
			removed, err := m.removeRestoreTemps(volume, note.Base)
			if err != nil {
				slog.Warn("temporary entries of an interrupted folder restore not all removed", "volume", volume, "folder", note.Base, "removed", removed, "err", err)
				done = false
				continue
			}
			slog.Info("interrupted folder restore cleaned up", "volume", volume, "folder", note.Base, "removed", removed)
			if err := root.Remove(e.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
				done = false
			}
		}
	}
	return done
}

// removeRestoreTemps removes the temporary entries of folder restores in
// and below base on a volume. It follows no link, stays on the volume's
// mount and touches nothing but entries of exactly that name.
func (m *Manager) removeRestoreTemps(volume, base string) (int, error) {
	if base != "" && !safeSnapshotPath(base) {
		return 0, ErrNotPossible
	}
	volRoot, err := volumedata.OpenVolume(m.mountRoot, volume, m.Mounted())
	if err != nil {
		return 0, err
	}
	top, err := volRoot.Open(".")
	volRoot.Close()
	if err != nil {
		return 0, err
	}
	defer top.Close()
	mount, err := mountOf(top)
	if err != nil {
		return 0, err
	}
	walker := &folderRestore{mount: mount}
	dir := top
	if base != "" {
		for _, part := range strings.Split(base, "/") {
			next, err := walker.liveDir(dir, part)
			if dir != top {
				dir.Close()
			}
			if errors.Is(err, os.ErrNotExist) {
				return 0, nil // the folder is gone, and with it what was in it
			}
			if err != nil {
				return 0, err
			}
			dir = next
		}
		defer dir.Close()
	}
	removed := 0
	err = sweepRestoreTemps(walker, dir, &removed)
	return removed, err
}

func sweepRestoreTemps(walker *folderRestore, dir *os.File, removed *int) error {
	var subdirs []string
	err := readNames(dir, func(name string) error {
		if restoreTempRE.MatchString(name) {
			if err := removeAllAt(int(dir.Fd()), name); err != nil {
				return err
			}
			*removed++
			return nil
		}
		var st unix.Stat_t
		if err := unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err == nil && st.Mode&unix.S_IFMT == unix.S_IFDIR {
			subdirs = append(subdirs, name)
		}
		return nil
	})
	if err != nil {
		return err
	}
	slices.Sort(subdirs)
	for _, name := range subdirs {
		sub, err := walker.liveDir(dir, name)
		if err != nil {
			continue // another mount, or gone meanwhile: not ours to look at
		}
		err = sweepRestoreTemps(walker, sub, removed)
		sub.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
