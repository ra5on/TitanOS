package volumedata

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"slices"
	"sync"
	"syscall"

	"github.com/ra5on/TitanOS/backend/internal/naming"
)

// Home says where TitanOS keeps its own data (.titanos/<feature>) while no
// volume was chosen for a feature, and brings along what an earlier default
// volume holds.
//
// Until 0.76 the default was the first mounted volume by name, also when
// its disks were asleep. Choose now prefers a volume that never sleeps
// (storage.SystemDataVolume). So that nothing is lost or split over two
// volumes, Volume moves the feature's folder from the volume used before to
// the chosen one, once per start, feature and pair of volumes: from the
// first mounted volume by name (the old rule) and from the volume this
// start used so far (the choice changes when disk sleep is set differently
// or a volume is unlocked later). Later calls touch no other volume, so a
// sleeping volume is not woken again.
type Home struct {
	MountRoot string
	// Mounted returns the names of the mounted volumes.
	Mounted func() []string
	// Choose returns the default volume, "" while there is none (yet).
	Choose func() string

	mu       sync.Mutex
	features map[string]*homeFeature
}

type homeFeature struct {
	mu    sync.Mutex
	last  string
	tried map[string]bool
}

// Default is the default volume without moving anything, for features that
// read every volume anyway (copies of /config).
func (h *Home) Default() string {
	if h == nil || h.Choose == nil {
		return ""
	}
	return h.Choose()
}

// Volume returns the default volume for a feature, "" while there is none.
// The first time it is another one than before, the feature's folder is
// moved over (Adopt); what could not be moved stays where it is and is
// reported in the journal.
func (h *Home) Volume(feature string) string {
	volume := h.Default()
	if volume == "" || h.Mounted == nil {
		return volume
	}
	h.mu.Lock()
	if h.features == nil {
		h.features = map[string]*homeFeature{}
	}
	state := h.features[feature]
	if state == nil {
		state = &homeFeature{tried: map[string]bool{}}
		h.features[feature] = state
	}
	h.mu.Unlock()

	state.mu.Lock()
	defer state.mu.Unlock()
	mounted := h.Mounted()
	if !slices.Contains(mounted, volume) {
		return ""
	}
	sources := []string{state.last}
	if sorted := slices.Sorted(slices.Values(mounted)); len(sorted) > 0 {
		for _, name := range sorted {
			if naming.ValidVolume(name) {
				sources = append(sources, name)
				break
			}
		}
	}
	state.last = volume
	for _, from := range sources {
		key := from + "\x00" + volume
		if from == "" || from == volume || state.tried[key] || !slices.Contains(mounted, from) {
			continue
		}
		state.tried[key] = true
		moved, left, err := Adopt(h.MountRoot, from, volume, feature, mounted)
		switch {
		case err != nil:
			slog.Warn("system data of an earlier default volume not moved", "feature", feature, "from", from, "to", volume, "moved", moved, "err", err)
		case len(left) > 0:
			slog.Warn("system data of an earlier default volume partly left in place: the same names exist on both volumes", "feature", feature, "from", from, "to", volume, "moved", moved, "left", left)
		case moved > 0:
			slog.Info("system data moved to the volume that never sleeps", "feature", feature, "from", from, "to", volume, "entries", moved)
		}
	}
	return volume
}

// openExisting pins .titanos/<feature> of a mounted volume if it is there
// and private; nothing is created. fs.ErrNotExist without the folder.
func openExisting(mountRoot, volume, feature string, mounted []string) (*os.Root, error) {
	if !naming.ValidVolume(feature) {
		return nil, ErrUnavailable
	}
	root, err := OpenVolume(mountRoot, volume, mounted)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, part := range []string{".titanos", ".titanos/" + feature} {
		st, err := root.Lstat(part)
		if err != nil {
			return nil, err
		}
		sys, ok := st.Sys().(*syscall.Stat_t)
		if !ok || sys.Uid != uint32(os.Geteuid()) || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0022 != 0 {
			return nil, fmt.Errorf("unsafe private data directory %s", part)
		}
	}
	private, err := root.OpenRoot(".titanos/" + feature)
	if err != nil {
		return nil, err
	}
	if err := SameMount(root, private); err != nil {
		private.Close()
		return nil, err
	}
	return private, nil
}

// Adopt moves the entries of .titanos/<feature> on the volume from to the
// same folder on the volume to. Files and folders are copied (the volumes
// are different file systems), synced and only then removed at the source.
// An entry whose name exists at the destination already is not touched,
// nor is anything that is neither a regular file nor a folder: both stay
// at the source and are returned as left. A source folder that is missing
// or empty is no error. The emptied source folder is removed.
func Adopt(mountRoot, from, to, feature string, mounted []string) (moved int, left []string, err error) {
	if from == to {
		return 0, nil, nil
	}
	src, err := openExisting(mountRoot, from, feature, mounted)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil, nil
	}
	if err != nil {
		return 0, nil, err
	}
	defer src.Close()
	entries, err := readDir(src, ".")
	if err != nil || len(entries) == 0 {
		return 0, nil, err
	}
	dst, err := Open(mountRoot, to, feature, mounted)
	if err != nil {
		return 0, nil, err
	}
	defer dst.Close()
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() && !entry.IsDir() {
			left = append(left, name)
			continue
		}
		if there, err := dst.Lstat(name); err == nil {
			// A move that was cut off after the copy (power cut): the copy is
			// complete, it was synced before it got its name. Only the
			// source is still to go.
			if here, e := entry.Info(); e == nil && here.Mode().IsRegular() && there.Mode().IsRegular() &&
				here.Size() == there.Size() && here.ModTime().Equal(there.ModTime()) && sameContent(src, dst, name) {
				if err := src.Remove(name); err != nil {
					return moved, left, fmt.Errorf("%s: %w", name, err)
				}
				moved++
				continue
			}
			left = append(left, name)
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return moved, left, err
		}
		tmp := ".adopt-" + name
		if err := dst.RemoveAll(tmp); err != nil {
			return moved, left, err
		}
		complete, err := copyEntry(src, dst, name, tmp)
		if err == nil {
			err = dst.Rename(tmp, name)
		}
		if err != nil {
			dst.RemoveAll(tmp)
			return moved, left, fmt.Errorf("%s: %w", name, err)
		}
		if !complete {
			// Links or special files inside stay at the source with their folder.
			left = append(left, name)
			continue
		}
		if err := src.RemoveAll(name); err != nil {
			return moved, left, fmt.Errorf("%s: %w", name, err)
		}
		moved++
	}
	if dir, err := dst.Open("."); err == nil {
		err = dir.Sync()
		dir.Close()
		if err != nil {
			return moved, left, err
		}
	}
	if len(left) == 0 {
		if volume, err := OpenVolume(mountRoot, from, mounted); err == nil {
			// Only an empty folder goes; a failure leaves an empty folder behind.
			_ = volume.Remove(".titanos/" + feature)
			volume.Close()
		}
	}
	return moved, left, nil
}

// sameContent compares two files of the same size byte by byte.
func sameContent(src, dst *os.Root, name string) bool {
	a, err := src.Open(name)
	if err != nil {
		return false
	}
	defer a.Close()
	b, err := dst.Open(name)
	if err != nil {
		return false
	}
	defer b.Close()
	one, two := make([]byte, 64<<10), make([]byte, 64<<10)
	for {
		n, errA := io.ReadFull(a, one)
		k, errB := io.ReadFull(b, two)
		if n != k || !bytes.Equal(one[:n], two[:k]) {
			return false
		}
		if errA != nil || errB != nil {
			return errA != nil && errB != nil
		}
	}
}

func readDir(root *os.Root, name string) ([]fs.DirEntry, error) {
	dir, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return dir.ReadDir(-1)
}

// copyEntry copies a regular file or a folder with everything in it from
// src/name to dst/target, keeping mode and modification time. complete is
// false if something inside was neither file nor folder and was left out.
func copyEntry(src, dst *os.Root, name, target string) (complete bool, err error) {
	info, err := src.Lstat(name)
	if err != nil {
		return false, err
	}
	switch {
	case info.Mode().IsRegular():
		in, err := src.Open(name)
		if err != nil {
			return false, err
		}
		defer in.Close()
		out, err := dst.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return false, err
		}
		if _, err = io.Copy(out, in); err == nil {
			err = out.Sync()
		}
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return false, err
		}
	case info.IsDir():
		if err := dst.Mkdir(target, 0700); err != nil {
			return false, err
		}
		entries, err := readDir(src, name)
		if err != nil {
			return false, err
		}
		complete = true
		for _, entry := range entries {
			done, err := copyEntry(src, dst, name+"/"+entry.Name(), target+"/"+entry.Name())
			if err != nil {
				return false, err
			}
			complete = complete && done
		}
		if err := dst.Chmod(target, info.Mode().Perm()); err != nil {
			return false, err
		}
		if dir, err := dst.Open(target); err == nil {
			err = dir.Sync()
			dir.Close()
			if err != nil {
				return false, err
			}
		}
		return complete, dst.Chtimes(target, info.ModTime(), info.ModTime())
	default:
		return false, nil
	}
	return true, dst.Chtimes(target, info.ModTime(), info.ModTime())
}
