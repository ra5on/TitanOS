package files

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

// The recycle bin of a share is .recycle/<account>/<path>, where Samba and
// Delete put what an account deleted, with the folders it was in. Paths in
// the bin start with the account: "alex/Fotos/a.jpg". Accounts see their own
// part only; administrators see everyone's.

// trashPath checks a path inside the bin and whether the account may use it.
// "" is the bin itself, which only administrators may list.
func trashPath(p, user string, admin bool) (string, error) {
	p = strings.Trim(p, "/")
	if p == "" || p == "." {
		if !admin {
			return "", ErrNotFound
		}
		return "", nil
	}
	if !fs.ValidPath(p) {
		return "", ErrInvalid
	}
	owner, _, _ := strings.Cut(p, "/")
	if owner != user && !admin {
		// The same answer as for a path that is not there.
		return "", ErrNotFound
	}
	return p, nil
}

// TrashList returns a folder of a share's recycle bin. A bin that does not
// exist yet is empty.
func (m *Manager) TrashList(root Root, p, user string, admin bool) ([]Entry, error) {
	if root.Kind != "share" {
		return nil, ErrInvalid
	}
	p, err := trashPath(p, user, admin)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return nil, mapError(err)
	}
	defer r.Close()
	f, err := r.Open(path.Join(RecycleDir, p))
	if err != nil {
		if os.IsNotExist(err) && !strings.Contains(p, "/") {
			return []Entry{}, nil
		}
		return nil, mapError(err)
	}
	defer f.Close()
	items, err := f.ReadDir(-1)
	if err != nil {
		return nil, mapError(err)
	}
	entries := []Entry{}
	for _, item := range items {
		info, err := item.Info()
		if err != nil || item.Type()&fs.ModeSymlink != 0 {
			continue
		}
		// Deleting touches the time; it is the time of deletion.
		entry := Entry{Name: item.Name(), Dir: item.IsDir(), Modified: info.ModTime().UTC().Truncate(time.Second)}
		if !entry.Dir {
			entry.Size = info.Size()
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

// TrashRestore moves files and folders from the bin back to where they were
// deleted. Missing folders on the way are created; if something with the
// same name is there now, the restored one gets the time in its name.
func (m *Manager) TrashRestore(root Root, paths []string, user string, uid int, admin bool) error {
	if root.Kind != "share" {
		return ErrInvalid
	}
	if !root.Writable {
		return ErrReadOnly
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return mapError(err)
	}
	defer r.Close()
	for _, p := range paths {
		p, err := trashPath(p, user, admin)
		if err != nil {
			return err
		}
		_, rel, _ := strings.Cut(p, "/")
		target, err := clean(rel)
		if err != nil || target == "." {
			return ErrInvalid
		}
		if m.protected(root, target) {
			return ErrProtected
		}
		source := path.Join(RecycleDir, p)
		if _, err := r.Lstat(source); err != nil {
			return mapError(err)
		}
		if err := m.makeParents(r, path.Dir(target), uid); err != nil {
			return mapError(err)
		}
		if _, err := r.Lstat(target); err == nil {
			ext := path.Ext(target)
			if info, err := r.Lstat(source); err == nil && info.IsDir() {
				ext = ""
			}
			target = fmt.Sprintf("%s (%s)%s", strings.TrimSuffix(target, ext), m.now().Format("2006-01-02 15.04.05"), ext)
			if _, err := r.Lstat(target); err == nil {
				return ErrExists
			}
		}
		if err := r.Rename(source, target); err != nil {
			return mapError(err)
		}
		m.pruneTrash(r, path.Dir(source))
	}
	return nil
}

// makeParents creates the folders of dir that are missing, like Mkdir.
func (m *Manager) makeParents(r *os.Root, dir string, uid int) error {
	if dir == "." {
		return nil
	}
	current := ""
	for part := range strings.SplitSeq(dir, "/") {
		current = path.Join(current, part)
		info, err := r.Lstat(current)
		if err == nil {
			if !info.IsDir() {
				return ErrExists
			}
			continue
		}
		if err := r.Mkdir(current, 0o770); err != nil {
			return err
		}
		m.own(r, current, uid, true)
	}
	return nil
}

// pruneTrash removes folders of the bin that were only left over from what
// was taken out, up to and including the account's own folder.
func (m *Manager) pruneTrash(r *os.Root, dir string) {
	for dir != RecycleDir && strings.HasPrefix(dir, RecycleDir+"/") {
		if r.Remove(dir) != nil {
			return // not empty
		}
		dir = path.Dir(dir)
	}
}

// TrashDelete deletes files and folders from the bin for good. A path of
// just the account empties its part of the bin.
func (m *Manager) TrashDelete(root Root, paths []string, user string, admin bool) error {
	if root.Kind != "share" {
		return ErrInvalid
	}
	if !root.Writable {
		return ErrReadOnly
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return mapError(err)
	}
	defer r.Close()
	for _, p := range paths {
		p, err := trashPath(p, user, admin)
		if err != nil {
			return err
		}
		if p == "" {
			return ErrInvalid
		}
		target := path.Join(RecycleDir, p)
		if _, err := r.Lstat(target); err != nil {
			return mapError(err)
		}
		if err := r.RemoveAll(target); err != nil {
			return mapError(err)
		}
		m.pruneTrash(r, path.Dir(target))
	}
	return nil
}
