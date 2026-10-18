// Package files is the file manager: listing, uploading, downloading,
// renaming and deleting files inside a share or volume.
//
// Every operation is confined to one root directory with os.Root, which
// refuses paths that leave it, also by way of symbolic links.
package files

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Root is a directory a user may work in.
type Root struct {
	ID     string `json:"id"`   // "share/fotos", "volume/data"
	Name   string `json:"name"` // fotos
	Kind   string `json:"kind"` // share, volume
	Dir    string `json:"-"`
	Volume string `json:"-"`
	// Writable is false for accounts with read-only access.
	Writable bool `json:"writable"`
	// Recycle moves deleted files to the share's recycle bin.
	Recycle bool `json:"recycle"`
	// Editable allows saving text files (SaveText) although the root is read-only.
	Editable bool `json:"editable"`
	// Hide lists top-level names that do not exist as far as users can tell;
	// with it, files called .env are hidden everywhere. The stick keeps
	// password hashes and keys there.
	Hide []string `json:"-"`
}

// secret reports whether a path is one the root hides.
func (r Root) secret(rel string) bool {
	if len(r.Hide) == 0 {
		return false
	}
	for i, part := range strings.Split(rel, "/") {
		if i == 0 && slices.Contains(r.Hide, part) || part == ".env" {
			return true
		}
	}
	return false
}

// Entry is a file or folder in a listing.
type Entry struct {
	Name     string    `json:"name"`
	Dir      bool      `json:"dir"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

var (
	ErrNameTooLong = errors.New("files: file name or path too long")
	ErrNotFound    = errors.New("files: no such file or folder")
	ErrExists      = errors.New("files: a file or folder with this name exists")
	ErrInvalid     = errors.New("files: invalid name or path")
	ErrProtected   = errors.New("files: path is managed by a virtual machine")
	ErrReadOnly    = errors.New("files: no write access")
	ErrNoSpace     = errors.New("files: not enough free space")
	ErrMismatch    = errors.New("files: upload does not continue where it stopped")
	ErrIncomplete  = errors.New("files: upload is not complete")
	ErrConfirm     = errors.New("files: deleting for good was not confirmed")
	ErrNoUpload    = errors.New("files: no such upload")
)

const (
	// RecycleDir is the recycle bin of a share. It is managed by Samba and
	// titand and hidden in the file manager.
	RecycleDir = ".recycle"
	// partSuffix marks a file that is still being uploaded.
	partSuffix = ".titanos-part"
	// uploadIdle is how long an unfinished upload is kept for resuming.
	uploadIdle = 24 * time.Hour
)

// hidden reports whether a name is internal and not shown.
func hidden(name string) bool {
	return name == ".titanos" || strings.Contains(name, ".titanos-snapshot-") || name == RecycleDir || strings.HasSuffix(name, partSuffix) || strings.HasSuffix(name, partSuffix+".json")
}

// clean checks a slash-separated path relative to a root. "" is the root.
func clean(p string) (string, error) {
	if p == "" || p == "." || p == "/" {
		return ".", nil
	}
	p = strings.Trim(p, "/")
	if !fs.ValidPath(p) {
		return "", ErrInvalid
	}
	for part := range strings.SplitSeq(p, "/") {
		if hidden(part) {
			return "", ErrInvalid
		}
	}
	return p, nil
}

// validName checks a single file or folder name.
func validName(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= 255 &&
		!strings.ContainsAny(name, "/\x00") && !hidden(name)
}

func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrNotExist):
		return ErrNotFound
	case errors.Is(err, fs.ErrExist):
		return ErrExists
	case errors.Is(err, syscall.ENAMETOOLONG):
		return ErrNameTooLong
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT):
		return ErrNoSpace
	case errors.Is(err, syscall.ENOTEMPTY):
		return ErrExists
	}
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) && strings.Contains(pathErr.Err.Error(), "escapes") {
		return ErrInvalid
	}
	return err
}

// Manager carries out file operations and keeps track of running uploads.
type Manager struct {
	// GID is the group files and folders are given ("users").
	Protected func(string) bool
	GID       int
	Available func(Root) bool
	Acquire   func(context.Context) (int64, func(), error)
	syncFile  func(*os.File) error

	jobs    jobs
	mu      sync.Mutex
	textMu  sync.Mutex
	uploads map[string]*upload
	now     func() time.Time
}

// NewManager returns a file manager that gives new files the group gid.
func NewManager(gid int) *Manager {
	return &Manager{GID: gid, uploads: map[string]*upload{}, now: time.Now, syncFile: func(f *os.File) error { return f.Sync() }}
}

func (m *Manager) protected(root Root, p string) bool {
	return m.Protected != nil && m.Protected(filepath.Join(root.Dir, p))
}

// Usage returns the total and free bytes of the filesystem a root is on.
func Usage(root Root) (total, free uint64) {
	var stat syscall.Statfs_t
	if syscall.Statfs(root.Dir, &stat) != nil {
		return 0, 0
	}
	return stat.Blocks * uint64(stat.Bsize), stat.Bavail * uint64(stat.Bsize)
}

// List returns the content of a folder, folders first.
func (m *Manager) List(root Root, dir string) ([]Entry, error) {
	dir, err := clean(dir)
	if err != nil {
		return nil, err
	}
	if root.secret(dir) {
		return nil, ErrNotFound
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return nil, mapError(err)
	}
	defer r.Close()
	f, err := r.Open(dir)
	if err != nil {
		return nil, mapError(err)
	}
	defer f.Close()
	items, err := f.ReadDir(-1)
	if err != nil {
		return nil, mapError(err)
	}
	entries := []Entry{}
	for _, item := range items {
		if hidden(item.Name()) || root.secret(path.Join(dir, item.Name())) {
			continue
		}
		info, err := item.Info()
		if err != nil {
			continue // vanished in between
		}
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

// own gives a new file or folder to the acting account and the share's group.
func (m *Manager) own(r *os.Root, name string, uid int, dir bool) {
	r.Lchown(name, uid, m.GID)
	if dir {
		r.Chmod(name, 0o770|os.ModeSetgid)
	} else {
		r.Chmod(name, 0o660)
	}
}

// Mkdir creates a folder.
func (m *Manager) Mkdir(root Root, name string, uid int) error {
	if !root.Writable {
		return ErrReadOnly
	}
	name, err := clean(name)
	if err != nil || name == "." || !validName(path.Base(name)) {
		return ErrInvalid
	}
	if m.protected(root, name) {
		return ErrProtected
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return mapError(err)
	}
	defer r.Close()
	if err := r.Mkdir(name, 0o770); err != nil {
		return mapError(err)
	}
	m.own(r, name, uid, true)
	return nil
}

// Rename moves a file or folder to a new name or folder inside the same
// root. It never replaces something that exists.
func (m *Manager) Rename(root Root, from, to string) error {
	if !root.Writable {
		return ErrReadOnly
	}
	from, errFrom := clean(from)
	to, errTo := clean(to)
	if errFrom != nil || errTo != nil || from == "." || to == "." || !validName(path.Base(to)) {
		return ErrInvalid
	}
	if to == from {
		return nil
	}
	if strings.HasPrefix(to+"/", from+"/") {
		return ErrInvalid // a folder cannot move into itself
	}
	if m.protected(root, from) || m.protected(root, to) {
		return ErrProtected
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return mapError(err)
	}
	defer r.Close()
	if _, err := r.Lstat(from); err != nil {
		return mapError(err)
	}
	if _, err := r.Lstat(to); err == nil {
		return ErrExists
	}
	return mapError(r.Rename(from, to))
}

// Delete removes files and folders. In a root with a recycle bin they are
// moved there; anywhere else the caller has to confirm deleting for good.
func (m *Manager) Delete(root Root, paths []string, user string, permanent bool) error {
	if !root.Writable {
		return ErrReadOnly
	}
	if !root.Recycle && !permanent {
		return ErrConfirm
	}
	for _, p := range paths {
		if m.protected(root, p) {
			return ErrProtected
		}
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return mapError(err)
	}
	defer r.Close()
	for _, p := range paths {
		p, err := clean(p)
		if err != nil || p == "." {
			return ErrInvalid
		}
		if _, err := r.Lstat(p); err != nil {
			return mapError(err)
		}
		if permanent || !root.Recycle {
			if err := r.RemoveAll(p); err != nil {
				return mapError(err)
			}
			continue
		}
		if err := m.recycle(r, p, user); err != nil {
			return mapError(err)
		}
	}
	return nil
}

// recycle moves p to .recycle/<user>/<p>, the same place Samba uses.
func (m *Manager) recycle(r *os.Root, p, user string) error {
	target := path.Join(RecycleDir, user, p)
	if err := r.MkdirAll(path.Dir(target), 0o770); err != nil {
		return err
	}
	// Something with this name may already be in the bin.
	if _, err := r.Lstat(target); err == nil {
		ext := path.Ext(target)
		target = fmt.Sprintf("%s (%s)%s", strings.TrimSuffix(target, ext), m.now().Format("2006-01-02 15.04.05"), ext)
	}
	if err := r.Rename(p, target); err != nil {
		return err
	}
	// The time of deletion decides when the bin drops the file.
	now := m.now()
	r.Chtimes(target, now, now)
	return nil
}

// Open opens a file for download.
func (m *Manager) Open(root Root, name string) (*os.File, fs.FileInfo, error) {
	name, err := clean(name)
	if err != nil || name == "." {
		return nil, nil, ErrInvalid
	}
	if root.secret(name) {
		return nil, nil, ErrNotFound
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return nil, nil, mapError(err)
	}
	defer r.Close()
	f, err := r.Open(name)
	if err != nil {
		return nil, nil, mapError(err)
	}
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		f.Close()
		return nil, nil, ErrInvalid
	}
	return f, info, nil
}

// Zip writes the given files and folders as a zip archive to w, without
// storing it anywhere in between. Symbolic links are left out.
func (m *Manager) Zip(w io.Writer, root Root, paths []string) error {
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return mapError(err)
	}
	defer r.Close()
	archive := zip.NewWriter(w)
	for _, p := range paths {
		p, err := clean(p)
		if err != nil || p == "." {
			return ErrInvalid
		}
		if root.secret(p) {
			return ErrNotFound
		}
		// Names in the archive start at the selected item, not at the root.
		prefix := path.Dir(p)
		err = fs.WalkDir(r.FS(), p, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if hidden(entry.Name()) || root.secret(name) {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			info, err := entry.Info()
			if err != nil || info.Mode()&fs.ModeSymlink != 0 {
				return nil
			}
			rel := strings.TrimPrefix(strings.TrimPrefix(name, prefix), "/")
			header := &zip.FileHeader{Name: rel, Modified: info.ModTime(), Method: zip.Store}
			if entry.IsDir() {
				header.Name += "/"
				_, err := archive.CreateHeader(header)
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			out, err := archive.CreateHeader(header)
			if err != nil {
				return err
			}
			in, err := r.Open(name)
			if err != nil {
				return nil // vanished in between
			}
			defer in.Close()
			_, err = io.Copy(out, in)
			return err
		})
		if err != nil {
			return mapError(err)
		}
	}
	return archive.Close()
}
