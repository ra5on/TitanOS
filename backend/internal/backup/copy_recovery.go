package backup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"
	"syscall"
)

// An undated copy is also browsed directly, and its destination may contain
// unrelated files. Keep its ordinary paths. Before changing them, publish a
// readable checkpoint of the affected top-level trees on the same target.
// Keep it across failed, cancelled and incomplete runs, including a restart.
const recoveryPrefix = ".titanos-recovery-"

type copyRecovery struct {
	target *os.Root
	base   string
}

func recoveryFor(job Job, target *os.Root) copyRecovery {
	return copyRecovery{target, path.Join(job.Destination.Folder, recoveryPrefix+url.PathEscape(job.ID))}
}

// The marker distinguishes our own work area from a pre-existing folder. All
// garbage collection is confined to staging names underneath this owned folder.
func (r copyRecovery) claim(job Job) error {
	if err := r.target.Mkdir(r.base, 0700); err == nil {
		f, err := r.target.OpenFile(path.Join(r.base, "owner"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = f.WriteString("TitanOS readable backup recovery v1\n" + job.ID + "\n")
		if err == nil {
			err = f.Sync()
		}
		return errors.Join(err, f.Close())
	} else if !errors.Is(err, fs.ErrExist) {
		return err
	}
	info, err := r.target.Lstat(r.base)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("recovery folder unavailable: %s", r.base)
	}
	file := path.Join(r.base, "owner")
	markerInfo, err := r.target.Lstat(file)
	if err != nil || !markerInfo.Mode().IsRegular() || markerInfo.Size() > 4096 {
		return fmt.Errorf("unrecognized backup recovery folder: %s", r.base)
	}
	data, err := r.target.ReadFile(file)
	if err != nil || string(data) != "TitanOS readable backup recovery v1\n"+job.ID+"\n" {
		return fmt.Errorf("unrecognized backup recovery folder: %s", r.base)
	}
	return nil
}

// Reclaim interrupted staging before the free-space estimate. In particular,
// a full FAT target must be able to release an unfinished physical copy.
func (r copyRecovery) discardStaging(job Job) error {
	if _, err := r.target.Lstat(r.base); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := r.claim(job); err != nil {
		return err
	}
	return r.target.RemoveAll(path.Join(r.base, "building"))
}

func (r copyRecovery) prepare(ctx context.Context, job Job) (bool, error) {
	names := slices.Clone(job.Sources)
	for _, volume := range job.Volumes {
		names = append(names, "Volume-"+volume)
	}
	for _, place := range job.Places {
		volume, _, _ := strings.Cut(place, "/")
		names = append(names, "Volume-"+volume)
	}
	slices.Sort(names)
	var present []string
	for _, name := range slices.Compact(names) {
		_, err := r.target.Lstat(path.Join(job.Destination.Folder, name))
		if err == nil {
			present = append(present, name)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
	}
	_, err := r.target.Lstat(r.base)
	if errors.Is(err, fs.ErrNotExist) && len(present) == 0 {
		return false, nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if err := r.claim(job); err != nil {
		return false, err
	}
	previous := path.Join(r.base, "previous")
	if info, err := r.target.Lstat(previous); err == nil {
		if !info.IsDir() {
			return false, fmt.Errorf("invalid recovery tree: %s", previous)
		}
		// Its predecessor might have lost the target after rename and before
		// sync. Confirm the checkpoint before proceeding with any live writes.
		return true, syncTarget(r.target)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	build := path.Join(r.base, "building")
	if err := r.target.Mkdir(build, 0700); err != nil {
		return false, err
	}
	defer r.target.RemoveAll(build) // on failure before publication, release any partially copied data
	// Hard links are safe: the copier publishes replacements by rename,
	// and never modifies old file contents in place. FAT/exFAT require a copy.
	hardlinks := canLink(r.target, build)
	for _, name := range present {
		from := path.Join(job.Destination.Folder, name)
		if err := r.clone(ctx, from, path.Join(build, name), hardlinks); err != nil {
			return false, fmt.Errorf("prepare readable recovery for %s: %w", from, err)
		}
	}
	if err := syncTarget(r.target); err != nil {
		return false, err
	}
	if err := r.target.Rename(build, previous); err != nil {
		return false, err
	}
	return true, syncTarget(r.target)
}

func (r copyRecovery) clone(ctx context.Context, from, to string, hardlinks bool) error {
	type directory struct {
		path string
		info fs.FileInfo
	}
	var dirs []directory
	err := fs.WalkDir(r.target.FS(), from, func(at string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		dest := to + strings.TrimPrefix(at, from)
		switch {
		case entry.IsDir():
			dirs = append(dirs, directory{dest, info})
			return r.target.Mkdir(dest, 0700)
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := r.target.Readlink(at)
			if err != nil {
				return err
			}
			return r.target.Symlink(link, dest)
		case info.Mode().IsRegular():
			if hardlinks {
				if err := r.target.Link(at, dest); err == nil {
					return nil
				} else if !errors.Is(err, syscall.EMLINK) {
					return err
				}
			}
			if _, err := copyFile(ctx, r.target, at, r.target, dest); err != nil {
				return err
			}
			return r.restoreMetadata(dest, info)
		default:
			return fmt.Errorf("cannot checkpoint special file %s", at)
		}
	})
	if err != nil {
		return err
	}
	for _, dir := range slices.Backward(dirs) {
		if err := r.restoreMetadata(dir.path, dir.info); err != nil {
			return err
		}
	}
	return nil
}

// On a filesystem without hard links a physical copy should carry the same
// POSIX permissions, owner and mtime that target exposed before the run.
func (r copyRecovery) restoreMetadata(dest string, want fs.FileInfo) error {
	got, err := r.target.Lstat(dest)
	if err != nil {
		return err
	}
	w, g := want.Sys().(*syscall.Stat_t), got.Sys().(*syscall.Stat_t)
	ownershipChanged := w.Uid != g.Uid || w.Gid != g.Gid
	if ownershipChanged {
		if err := r.target.Chown(dest, int(w.Uid), int(w.Gid)); err != nil {
			return err
		}
	}
	const mode = fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky
	if ownershipChanged || want.Mode()&mode != got.Mode()&mode {
		if err := r.target.Chmod(dest, want.Mode()&mode); err != nil {
			return err
		}
	}
	if !want.ModTime().Equal(got.ModTime()) {
		return r.target.Chtimes(dest, want.ModTime(), want.ModTime())
	}
	return nil
}

func (r copyRecovery) finish() error {
	// The caller synced the complete visible copy first. Rename the checkpoint
	// out of the way atomically. A crash during removal must never leave a
	// half-deleted tree that would be mistaken for the previous generation.
	build := path.Join(r.base, "building")
	if err := r.target.Rename(path.Join(r.base, "previous"), build); err != nil {
		return err
	}
	if err := syncTarget(r.target); err != nil {
		return err
	}
	return r.target.RemoveAll(build)
}
