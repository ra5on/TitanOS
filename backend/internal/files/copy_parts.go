package files

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"syscall"

	"golang.org/x/sys/unix"
)

// A copy writes each file as .titanos-copy-<id>.titanos-part next to its
// target. When titand stops in the middle, the part stays. For targets on a
// data volume the copy leaves a note like an upload does (sweep.go), and
// the hourly sweep removes parts that are older than a day and do not
// belong to a running job. USB drives have no notes; their parts go when a
// move or a new copy meets them there.

// holdCopyPart marks a part file as in use and leaves a note for the sweep.
func (m *Manager) holdCopyPart(root Root, id, part string) {
	m.mu.Lock()
	if m.copyParts == nil {
		m.copyParts = map[string]bool{}
	}
	m.copyParts[id] = true
	m.mu.Unlock()
	if root.Dir != "" {
		m.noteUpload(root, id, part)
	}
}

// releaseCopyPart forgets a part file that was published or removed.
func (m *Manager) releaseCopyPart(root Root, id string) {
	m.mu.Lock()
	delete(m.copyParts, id)
	m.mu.Unlock()
	if root.Dir != "" {
		m.forgetUpload(root, id)
	}
}

func (m *Manager) copyPartActive(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.copyParts[id]
}

// stalePart reports whether p is a part file of titand (a copy or an
// upload) that nobody has written to for a day and no running job holds.
func (m *Manager) stalePart(info fs.FileInfo, name string) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) || m.now().Sub(info.ModTime()) <= uploadIdle {
		return false
	}
	if match := copyPartName.FindStringSubmatch(name); match != nil {
		return !m.copyPartActive(match[1])
	}
	return false
}

// removeStalePart removes p if it is a stale part file and reports whether
// it is gone.
func (m *Manager) removeStalePart(r *os.Root, p string) bool {
	info, err := r.Lstat(p)
	if err != nil || !m.stalePart(info, path.Base(p)) {
		return false
	}
	return r.Remove(p) == nil
}

// renameNoReplace renames without replacing what may have appeared at the
// target. Filesystems that do not know the flag (some FUSE drives) get a
// plain rename after a check.
func renameNoReplace(r *os.Root, from, to string) error {
	err := publishNewFile(r, from, to)
	if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS) {
		if _, e := r.Lstat(to); errors.Is(e, fs.ErrNotExist) {
			return r.Rename(from, to)
		}
	}
	return err
}
