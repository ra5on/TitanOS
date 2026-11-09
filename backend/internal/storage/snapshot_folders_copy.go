package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Copying a folder back out of a snapshot: merge, new folder, files, links,
// metadata (split from snapshot_folders.go, PLAN 75-12).

// restore carries out the job on the pinned descriptors.
func (r *folderRestore) restore(ctx context.Context) error {
	n := len(r.parts)
	switch {
	case r.req.Mode == RestoreCopy:
		target, err := r.newFolder(ctx, r.src(), r.liveDirs[n-1], copyName(r.req.Path, r.snapshot), r.req.Path, nil, true)
		if err != nil {
			return err
		}
		r.progress(func(j *SnapshotFolderJob) { j.Target = path.Join(path.Dir(r.req.Path), target) })
		return nil
	case r.dst() != nil:
		return r.merge(ctx, r.src(), r.dst(), r.req.Path)
	default:
		// The folder, perhaps also its parents, is gone: build the missing
		// chain under a temporary name and publish its top at once.
		k := len(r.liveDirs) - 1
		_, err := r.newFolder(ctx, r.srcDirs[k+1], r.liveDirs[k], r.parts[k], strings.Join(r.parts[:k+1], "/"), r.srcDirs[k+2:], false)
		return err
	}
}

// merge restores the contents of a snapshot folder into an existing live
// folder. The live folder's own metadata stays as it is.
func (r *folderRestore) merge(ctx context.Context, src, dst *os.File, rel string) error {
	return readNames(src, func(name string) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		child := path.Join(rel, name)
		if !safeSnapshotPath(child) {
			return nil
		}
		var st unix.Stat_t
		if err := unix.Fstatat(int(src.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		kind := st.Mode & unix.S_IFMT
		if kind != unix.S_IFDIR && kind != unix.S_IFREG && kind != unix.S_IFLNK {
			r.progress(func(j *SnapshotFolderJob) { j.Special++ })
			return nil
		}
		var live unix.Stat_t
		err := unix.Fstatat(int(dst.Fd()), name, &live, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(err, unix.ENOENT) {
			return r.create(ctx, src, dst, name, name, child, &st, false)
		}
		if err != nil {
			return err
		}
		liveDir := live.Mode&unix.S_IFMT == unix.S_IFDIR
		if kind == unix.S_IFDIR && liveDir {
			sub, err := openDirAt(src, name)
			if err != nil {
				return err
			}
			defer sub.Close()
			subDst, err := r.liveDir(dst, name)
			if err != nil {
				return err
			}
			defer subDst.Close()
			return r.merge(ctx, sub, subDst, child)
		}
		policy := r.req.Conflict
		// Replace only swaps a file or link for another; a folder is never
		// deleted to make room, nor replaced by a file. Both are kept.
		if policy == RestoreReplace && (kind == unix.S_IFDIR || liveDir) {
			policy = RestoreRename
		}
		switch policy {
		case RestoreSkip:
			return r.skip(ctx, src, name, child, &st)
		case RestoreReplace:
			return r.create(ctx, src, dst, name, name, child, &st, true)
		default:
			target, err := freeName(int(dst.Fd()), name, kind == unix.S_IFDIR)
			if err != nil {
				return err
			}
			r.progress(func(j *SnapshotFolderJob) { j.Renamed++ })
			return r.create(ctx, src, dst, name, target, child, &st, false)
		}
	})
}

// skip counts what a conflict leaves out, so that progress still reaches the end.
func (r *folderRestore) skip(ctx context.Context, src *os.File, name, rel string, st *unix.Stat_t) error {
	var c restoreCount
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		sub, err := openDirAt(src, name)
		if err != nil {
			return err
		}
		err = r.count(ctx, sub, nil, rel, &c)
		sub.Close()
		if err != nil {
			return err
		}
	case unix.S_IFREG:
		c.files, c.bytes = 1, st.Size
	default:
		c.links = 1
	}
	r.progress(func(j *SnapshotFolderJob) {
		j.Skipped += c.files + c.links
		j.Files += c.files + c.links
		j.Bytes += c.bytes
	})
	return nil
}

// create writes one entry under a temporary name and publishes it as target.
// replace swaps it for an existing file or link in one step.
func (r *folderRestore) create(ctx context.Context, src, dst *os.File, name, target, rel string, st *unix.Stat_t, replace bool) (err error) {
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		sub, err := openDirAt(src, name)
		if err != nil {
			return err
		}
		defer sub.Close()
		_, err = r.newFolder(ctx, sub, dst, target, rel, nil, false)
		return err
	case unix.S_IFREG:
		err = r.copyFile(ctx, src, dst, name, target, st, replace)
	default:
		err = r.copyLink(src, dst, name, target, st, replace)
	}
	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	return err
}

// publish renames a finished temporary entry into place.
func publish(dir int, temp, target string, replace bool) error {
	if replace {
		return unix.Renameat(dir, temp, dir, target)
	}
	return unix.Renameat2(dir, temp, dir, target, unix.RENAME_NOREPLACE)
}

// newFolder builds a folder under a temporary name in dst and publishes it.
// chain are snapshot folders to create inside, one per level, ending with
// the folder whose contents are restored. pickFree chooses the next free
// "keep both" name when target appeared meanwhile.
func (r *folderRestore) newFolder(ctx context.Context, src, dst *os.File, target, rel string, chain []*os.File, pickFree bool) (string, error) {
	temp := restoreTemp()
	dir := int(dst.Fd())
	if err := unix.Mkdirat(dir, temp, 0700); err != nil {
		return "", fmt.Errorf("%s: %w", rel, err)
	}
	done := false
	defer func() {
		if !done {
			_ = removeAllAt(dir, temp) // cleanup, best effort: nothing more can be done if it fails
		}
	}()
	made, err := openDirAt(dst, temp)
	if err != nil {
		return "", err
	}
	defer made.Close()
	if len(chain) > 0 {
		// Only the next folder of the missing chain is created here.
		next := chain[0]
		if _, err := r.newFolder(ctx, next, made, path.Base(next.Name()), path.Join(rel, path.Base(next.Name())), chain[1:], false); err != nil {
			return "", err
		}
	} else if err := r.merge(ctx, src, made, rel); err != nil {
		return "", err
	}
	if err := applyMeta(src, made); err != nil {
		return "", err
	}
	if err := made.Sync(); err != nil {
		return "", err
	}
	if pickFree {
		for {
			free, err := freeName(dir, target, true)
			if err != nil {
				return "", err
			}
			err = publish(dir, temp, free, false)
			if errors.Is(err, unix.EEXIST) {
				continue
			}
			if err != nil {
				return "", err
			}
			target = free
			break
		}
	} else if err := publish(dir, temp, target, false); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return "", fmt.Errorf("%w: %s appeared on the volume meanwhile", ErrExists, path.Join(path.Dir(rel), target))
		}
		return "", err
	}
	done = true
	return target, dst.Sync()
}

// applyMeta copies owner, mode, extended attributes (and with them ACLs) and
// times from a snapshot entry to its restored copy.
func applyMeta(src, dst *os.File) error {
	var st unix.Stat_t
	if err := unix.Fstat(int(src.Fd()), &st); err != nil {
		return err
	}
	if err := unix.Fchown(int(dst.Fd()), int(st.Uid), int(st.Gid)); err != nil && !(errors.Is(err, unix.EPERM) && os.Geteuid() != 0) {
		return err
	}
	if err := copyXattrs(int(src.Fd()), int(dst.Fd())); err != nil {
		return err
	}
	// After chown, which clears set-user-ID bits.
	if err := unix.Fchmod(int(dst.Fd()), st.Mode&07777); err != nil {
		return err
	}
	return futimens(int(dst.Fd()), st.Atim, st.Mtim)
}

// futimens sets the times of an open file (utimensat with a NULL path).
func futimens(fd int, atime, mtime unix.Timespec) error {
	times := [2]unix.Timespec{atime, mtime}
	_, _, errno := unix.Syscall6(unix.SYS_UTIMENSAT, uintptr(fd), 0, uintptr(unsafe.Pointer(&times[0])), 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func copyXattrs(src, dst int) error {
	size, err := unix.Flistxattr(src, nil)
	if errors.Is(err, unix.ENOTSUP) || size == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	list := make([]byte, size)
	if size, err = unix.Flistxattr(src, list); err != nil {
		return err
	}
	for _, name := range strings.Split(string(list[:size]), "\x00") {
		if name == "" {
			continue
		}
		n, err := unix.Fgetxattr(src, name, nil)
		if err != nil {
			return err
		}
		value := make([]byte, n)
		if n, err = unix.Fgetxattr(src, name, value); err != nil {
			return err
		}
		if err := unix.Fsetxattr(dst, name, value[:n], 0); err != nil {
			// Unprivileged tests cannot set trusted/security names; titand runs as root.
			if errors.Is(err, unix.ENOTSUP) || (errors.Is(err, unix.EPERM) && os.Geteuid() != 0) {
				continue
			}
			return fmt.Errorf("extended attribute %s: %w", name, err)
		}
	}
	return nil
}

// copyFile clones a file (reflink) or copies it when the filesystem cannot.
func (r *folderRestore) copyFile(ctx context.Context, srcDir, dstDir *os.File, name, target string, st *unix.Stat_t, replace bool) error {
	if r.m != nil && r.m.restoreStep != nil {
		r.m.restoreStep(name)
	}
	fd, err := unix.Openat(int(srcDir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	in := os.NewFile(uintptr(fd), name)
	defer in.Close()
	var now unix.Stat_t
	if err := unix.Fstat(fd, &now); err != nil {
		return err
	}
	if now.Mode&unix.S_IFMT != unix.S_IFREG || now.Ino != st.Ino || now.Dev != st.Dev {
		return fmt.Errorf("snapshot file %s changed", name)
	}
	temp := restoreTemp()
	dir := int(dstDir.Fd())
	ofd, err := unix.Openat(dir, temp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	out := os.NewFile(uintptr(ofd), temp)
	done := false
	defer func() {
		out.Close()
		if !done {
			_ = unix.Unlinkat(dir, temp, 0) // cleanup, best effort: nothing more can be done if it fails
		}
	}()
	cloned := unix.IoctlFileClone(ofd, fd) == nil
	if cloned {
		r.progress(func(j *SnapshotFolderJob) { j.Bytes += now.Size })
	} else {
		var copied int64
		for copied < now.Size {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			n, err := io.CopyN(out, in, min(8<<20, now.Size-copied))
			copied += n
			r.progress(func(j *SnapshotFolderJob) { j.Bytes += n })
			if err != nil {
				return err
			}
		}
	}
	if err := applyMeta(in, out); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := publish(dir, temp, target, replace); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("%w: %s appeared on the volume meanwhile", ErrExists, target)
		}
		return err
	}
	done = true
	r.progress(func(j *SnapshotFolderJob) {
		j.Files++
		j.Copied++
		if cloned {
			j.Cloned++
		}
		if replace {
			j.Replaced++
		}
	})
	return nil
}

// copyLink recreates a symbolic link with its owner and times. The link is
// copied as it is and never followed.
func (r *folderRestore) copyLink(srcDir, dstDir *os.File, name, target string, st *unix.Stat_t, replace bool) error {
	buf := make([]byte, 4096)
	n, err := unix.Readlinkat(int(srcDir.Fd()), name, buf)
	if err != nil {
		return err
	}
	if n >= len(buf) {
		return fmt.Errorf("%w: link %s is too long", ErrNotPossible, name)
	}
	temp := restoreTemp()
	dir := int(dstDir.Fd())
	if err := unix.Symlinkat(string(buf[:n]), dir, temp); err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			_ = unix.Unlinkat(dir, temp, 0) // cleanup, best effort: nothing more can be done if it fails
		}
	}()
	if err := unix.Fchownat(dir, temp, int(st.Uid), int(st.Gid), unix.AT_SYMLINK_NOFOLLOW); err != nil && !(errors.Is(err, unix.EPERM) && os.Geteuid() != 0) {
		return err
	}
	if err := unix.UtimesNanoAt(dir, temp, []unix.Timespec{st.Atim, st.Mtim}, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if err := publish(dir, temp, target, replace); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("%w: %s appeared on the volume meanwhile", ErrExists, target)
		}
		return err
	}
	done = true
	r.progress(func(j *SnapshotFolderJob) {
		j.Files++
		j.Copied++
		if replace {
			j.Replaced++
		}
	})
	return nil
}
