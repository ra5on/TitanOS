package files

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"syscall"
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
		// A deleted directory carries the deletion time for all its children.
		metadata := trashDetails(r, path.Join(p, item.Name()), info.ModTime())
		entry := Entry{Name: item.Name(), Dir: item.IsDir(), Modified: metadata.Deleted.UTC().Truncate(time.Second)}
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
		rel := trashDetails(r, p, time.Time{}).Original
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
			target, err = m.settle(r, target, ConflictRename)
			if err != nil {
				return err
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

type TrashEntry struct {
	Entry
	Allocated *int64 `json:"allocated,omitempty"`
	Path      string `json:"path"`
	Original  string `json:"original"`
}

// TrashFlat lists files across original folders without exposing another
// account's bin. It never follows symbolic links.
func (m *Manager) TrashFlat(ctx context.Context, root Root, user string, admin bool) ([]TrashEntry, error) {
	if root.Kind != "share" {
		return nil, ErrInvalid
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return nil, mapError(err)
	}
	defer r.Close()
	base := RecycleDir
	if !admin {
		base = path.Join(base, user)
	}
	result := []TrashEntry{}
	seen := make(map[[2]uint64]bool)
	err = fs.WalkDir(r.FS(), base, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			if p == base && os.IsNotExist(e) {
				return nil
			}
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		relative := strings.TrimPrefix(p, RecycleDir+"/")
		info, e := d.Info()
		if e != nil {
			return e
		}
		metadata := trashDetails(r, relative, info.ModTime())
		original := metadata.Original
		var allocated *int64
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			key := [2]uint64{uint64(st.Dev), st.Ino}
			bytes := int64(0)
			if !seen[key] {
				bytes = st.Blocks * 512
				seen[key] = true
			}
			allocated = &bytes
		}
		result = append(result, TrashEntry{Allocated: allocated, Entry: Entry{Name: d.Name(), Size: info.Size(), Modified: metadata.Deleted}, Path: relative, Original: original})
		if len(result) > 10000 {
			return ErrInvalid
		}
		return nil
	})
	return result, mapError(err)
}

type TrashRestorePreview struct {
	Path     string `json:"path"`
	Target   string `json:"target"`
	Conflict bool   `json:"conflict"`
}

func (m *Manager) TrashPreview(root Root, paths []string, user string, admin bool) ([]TrashRestorePreview, error) {
	if root.Kind != "share" || !root.Writable {
		return nil, ErrReadOnly
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return nil, mapError(err)
	}
	defer r.Close()
	result := []TrashRestorePreview{}
	seen := map[string]bool{}
	for _, p := range paths {
		p, err = trashPath(p, user, admin)
		if err != nil {
			return nil, err
		}
		target := trashDetails(r, p, time.Time{}).Original
		if target == "" {
			return nil, ErrInvalid
		}
		if m.protected(root, target) {
			return nil, ErrProtected
		}
		if _, err = r.Lstat(path.Join(RecycleDir, p)); err != nil {
			return nil, mapError(err)
		}
		_, err = r.Lstat(target)
		if err != nil && !os.IsNotExist(err) {
			return nil, mapError(err)
		}
		result = append(result, TrashRestorePreview{Path: p, Target: target, Conflict: err == nil || seen[target]})
		seen[target] = true
	}
	return result, nil
}
