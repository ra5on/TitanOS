// Package files is the file manager: listing, uploading, downloading,
// renaming and deleting files inside a share or volume.
//
// Every operation is confined to one root directory with os.Root, which
// refuses paths that leave it, also by way of symbolic links.
package files

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Root is a directory a user may work in.
type Root struct {
	ID   string `json:"id"`   // "share/fotos", "volume/data"
	Name string `json:"name"` // fotos
	Kind string `json:"kind"` // share, volume
	Dir  string `json:"-"`
	// Writable is false for accounts with read-only access.
	Writable bool `json:"writable"`
	// Recycle moves deleted files to the share's recycle bin.
	Recycle bool `json:"recycle"`
}

// Entry is a file or folder in a listing.
type Entry struct {
	Name     string    `json:"name"`
	Dir      bool      `json:"dir"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

var (
	ErrNotFound   = errors.New("files: no such file or folder")
	ErrExists     = errors.New("files: a file or folder with this name exists")
	ErrInvalid    = errors.New("files: invalid name or path")
	ErrReadOnly   = errors.New("files: no write access")
	ErrNoSpace    = errors.New("files: not enough free space")
	ErrMismatch   = errors.New("files: upload does not continue where it stopped")
	ErrIncomplete = errors.New("files: upload is not complete")
	ErrConfirm    = errors.New("files: deleting for good was not confirmed")
	ErrNoUpload   = errors.New("files: no such upload")
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
	return name == RecycleDir || strings.HasSuffix(name, partSuffix)
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
	GID int

	mu      sync.Mutex
	uploads map[string]*upload
	now     func() time.Time
}

// NewManager returns a file manager that gives new files the group gid.
func NewManager(gid int) *Manager {
	return &Manager{GID: gid, uploads: map[string]*upload{}, now: time.Now}
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
		if hidden(item.Name()) {
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
		// Names in the archive start at the selected item, not at the root.
		prefix := path.Dir(p)
		err = fs.WalkDir(r.FS(), p, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if hidden(entry.Name()) {
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

// upload is a file on its way in. It is written to a hidden part file in
// the target folder and only gets its real name once it is complete.
type upload struct {
	id, user   string
	uid        int
	root       Root
	name, part string // paths relative to the root
	size       int64
	offset     int64
	touched    time.Time
	busy       bool
}

// UploadStatus tells a client where to continue.
type UploadStatus struct {
	ID     string `json:"id"`
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
	// Name is the final name; it differs from the requested one when a
	// free name had to be found.
	Name string `json:"name"`
}

// Conflict modes for a file that already exists.
const (
	ConflictFail    = ""
	ConflictReplace = "replace"
	ConflictRename  = "rename"
)

// StartUpload prepares an upload of size bytes to dir/name.
func (m *Manager) StartUpload(root Root, dir, name string, size int64, conflict, user string, uid int) (UploadStatus, error) {
	if !root.Writable {
		return UploadStatus{}, ErrReadOnly
	}
	dir, err := clean(dir)
	if err != nil || !validName(name) || size < 0 {
		return UploadStatus{}, ErrInvalid
	}
	if _, free := Usage(root); uint64(size) > free {
		return UploadStatus{}, ErrNoSpace
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return UploadStatus{}, mapError(err)
	}
	defer r.Close()
	if info, err := r.Stat(dir); err != nil || !info.IsDir() {
		return UploadStatus{}, ErrNotFound
	}

	target := path.Join(dir, name)
	if info, err := r.Lstat(target); err == nil {
		switch {
		case conflict == ConflictRename:
			ext := path.Ext(name)
			for n := 2; ; n++ {
				target = path.Join(dir, fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), n, ext))
				if _, err := r.Lstat(target); err != nil {
					break
				}
			}
		case conflict == ConflictReplace && !info.IsDir():
		default:
			return UploadStatus{}, ErrExists
		}
	}

	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return UploadStatus{}, err
	}
	id := hex.EncodeToString(raw)
	part := path.Join(dir, "."+path.Base(target)+"."+id+partSuffix)
	f, err := r.OpenFile(part, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return UploadStatus{}, mapError(err)
	}
	f.Close()

	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	m.uploads[id] = &upload{id: id, user: user, uid: uid, root: root, name: target, part: part, size: size, touched: m.now()}
	return UploadStatus{ID: id, Size: size, Name: path.Base(target)}, nil
}

// prune forgets uploads nobody continued and removes their part files.
// The caller holds the lock.
func (m *Manager) prune() {
	for id, u := range m.uploads {
		if !u.busy && m.now().Sub(u.touched) > uploadIdle {
			os.Remove(filepath.Join(u.root.Dir, filepath.FromSlash(u.part)))
			delete(m.uploads, id)
		}
	}
}

// claim returns the upload for exclusive use; release gives it back.
func (m *Manager) claim(id, user string) (*upload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.uploads[id]
	if !ok || u.user != user {
		return nil, ErrNoUpload
	}
	if u.busy {
		return nil, ErrMismatch
	}
	u.busy = true
	return u, nil
}

func (m *Manager) release(u *upload, done bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u.busy = false
	u.touched = m.now()
	if done {
		delete(m.uploads, u.id)
	}
}

// UploadStatus returns how much of an upload has arrived.
func (m *Manager) UploadStatus(id, user string) (UploadStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.uploads[id]
	if !ok || u.user != user {
		return UploadStatus{}, ErrNoUpload
	}
	return UploadStatus{ID: id, Offset: u.offset, Size: u.size, Name: path.Base(u.name)}, nil
}

// WriteChunk appends data to an upload. offset must be where the upload
// stands; a client that lost track asks UploadStatus first.
func (m *Manager) WriteChunk(id, user string, offset int64, data io.Reader) (UploadStatus, error) {
	u, err := m.claim(id, user)
	if err != nil {
		return UploadStatus{}, err
	}
	defer m.release(u, false)
	if offset != u.offset {
		return UploadStatus{}, ErrMismatch
	}
	r, err := os.OpenRoot(u.root.Dir)
	if err != nil {
		return UploadStatus{}, mapError(err)
	}
	defer r.Close()
	f, err := r.OpenFile(u.part, os.O_WRONLY, 0)
	if err != nil {
		return UploadStatus{}, mapError(err)
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return UploadStatus{}, err
	}
	// Never more than announced, whatever the client sends.
	written, err := io.Copy(f, io.LimitReader(data, u.size-u.offset))
	u.offset += written
	if err != nil {
		// Keep what arrived: the client continues from there.
		f.Truncate(u.offset)
		return UploadStatus{ID: id, Offset: u.offset, Size: u.size, Name: path.Base(u.name)}, mapError(err)
	}
	return UploadStatus{ID: id, Offset: u.offset, Size: u.size, Name: path.Base(u.name)}, nil
}

// FinishUpload gives a complete upload its real name.
func (m *Manager) FinishUpload(id, user string) (UploadStatus, error) {
	u, err := m.claim(id, user)
	if err != nil {
		return UploadStatus{}, err
	}
	done := false
	defer func() { m.release(u, done) }()
	if u.offset != u.size {
		return UploadStatus{}, ErrIncomplete
	}
	r, err := os.OpenRoot(u.root.Dir)
	if err != nil {
		return UploadStatus{}, mapError(err)
	}
	defer r.Close()
	if f, err := r.OpenFile(u.part, os.O_WRONLY, 0); err == nil {
		f.Sync()
		f.Close()
	}
	m.own(r, u.part, u.uid, false)
	if err := r.Rename(u.part, u.name); err != nil {
		return UploadStatus{}, mapError(err)
	}
	done = true
	return UploadStatus{ID: id, Offset: u.offset, Size: u.size, Name: path.Base(u.name)}, nil
}

// AbortUpload drops an upload and what has arrived of it.
func (m *Manager) AbortUpload(id, user string) error {
	u, err := m.claim(id, user)
	if err != nil {
		return err
	}
	defer m.release(u, true)
	if r, err := os.OpenRoot(u.root.Dir); err == nil {
		r.Remove(u.part)
		r.Close()
	}
	return nil
}
