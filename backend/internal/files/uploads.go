package files

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var ErrVolumeChanged = errors.New("upload target volume is no longer available")

type upload struct {
	id, user              string
	uid                   int
	root                  Root
	name, part, conflict  string
	size                  int64
	offset                atomic.Int64
	device, inode         uint64
	partInode, partDevice uint64
	touched               time.Time
	busy                  bool
}
type UploadStatus struct {
	ID     string `json:"id"`
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
	Name   string `json:"name"`
	Busy   bool   `json:"busy,omitempty"`
}

const (
	ConflictFail    = ""
	ConflictReplace = "replace"
	ConflictRename  = "rename"
)

type uploadRecord struct {
	ID, User, Root, Name, Part, Conflict string
	Size                                 int64
}

func identity(r *os.Root) (uint64, uint64, error) {
	info, err := r.Stat(".")
	if err != nil {
		return 0, 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, ErrVolumeChanged
	}
	return uint64(stat.Dev), stat.Ino, nil
}
func (m *Manager) openUpload(u *upload) (*os.Root, error) {
	if m.Available != nil && !m.Available(u.root) {
		return nil, ErrVolumeChanged
	}
	r, err := os.OpenRoot(u.root.Dir)
	if err != nil {
		return nil, ErrVolumeChanged
	}
	dev, ino, err := identity(r)
	if err != nil || dev != u.device || ino != u.inode {
		r.Close()
		return nil, ErrVolumeChanged
	}
	return r, nil
}
func (u *upload) status() UploadStatus {
	return UploadStatus{ID: u.id, Offset: u.offset.Load(), Size: u.size, Name: path.Base(u.name), Busy: u.busy}
}
func (m *Manager) StartUpload(root Root, dir, name string, size int64, conflict, user string, uid int) (UploadStatus, error) {
	if !root.Writable {
		return UploadStatus{}, ErrReadOnly
	}
	dir, err := clean(dir)
	if err != nil || !validName(name) || size < 0 {
		return UploadStatus{}, ErrInvalid
	}
	if m.protected(root, path.Join(dir, name)) {
		return UploadStatus{}, ErrProtected
	}
	if conflict != "" && conflict != ConflictReplace && conflict != ConflictRename {
		return UploadStatus{}, ErrInvalid
	}
	if m.Available != nil && !m.Available(root) {
		return UploadStatus{}, ErrVolumeChanged
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return UploadStatus{}, mapError(err)
	}
	defer r.Close()
	if info, err := r.Stat(dir); err != nil || !info.IsDir() {
		return UploadStatus{}, ErrNotFound
	}
	dev, ino, err := identity(r)
	if err != nil {
		return UploadStatus{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune()
	_, free := Usage(root)
	for _, pending := range m.uploads {
		if pending.device == dev || root.Volume != "" && pending.root.Volume == root.Volume {
			remaining := uint64(max(int64(0), pending.size-pending.offset.Load()))
			free -= min(free, remaining)
		}
	}
	if uint64(size) > free {
		return UploadStatus{}, ErrNoSpace
	}
	target := path.Join(dir, name)
	if info, err := r.Lstat(target); err == nil {
		switch {
		case conflict == ConflictRename:
			ext := path.Ext(name)
			for n := 2; ; n++ {
				target = path.Join(dir, renameUpload(name, ext, n))
				if _, err := r.Lstat(target); errors.Is(err, fs.ErrNotExist) {
					break
				} else if err != nil {
					return UploadStatus{}, err
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
	part := uploadPartName(target, id)
	f, err := r.OpenFile(part, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return UploadStatus{}, mapError(err)
	}
	info, statErr := f.Stat()
	if statErr != nil {
		f.Close()
		r.Remove(part)
		return UploadStatus{}, statErr
	}
	partInode := info.Sys().(*syscall.Stat_t).Ino
	partDevice := uint64(info.Sys().(*syscall.Stat_t).Dev)
	if err = m.syncFile(f); err != nil {
		f.Close()
		r.Remove(part)
		return UploadStatus{}, mapError(err)
	}
	if err = f.Close(); err != nil {
		r.Remove(part)
		return UploadStatus{}, err
	}
	u := &upload{id: id, user: user, uid: uid, root: root, name: target, part: part, size: size, conflict: conflict, device: dev, inode: ino, partInode: partInode, partDevice: partDevice, touched: m.now()}
	record := uploadRecord{id, user, root.ID, target, part, conflict, size}
	b, err := json.Marshal(record)
	if err == nil {
		var meta *os.File
		meta, err = r.OpenFile(part+".json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			_, err = meta.Write(b)
			if err == nil {
				err = m.syncFile(meta)
			}
			err = errors.Join(err, meta.Close())
		}
	}
	if err != nil {
		r.Remove(part)
		r.Remove(part + ".json")
		return UploadStatus{}, mapError(err)
	}
	parent, err := r.Open(dir)
	if err == nil {
		err = errors.Join(parent.Sync(), parent.Close())
	}
	if err != nil {
		r.Remove(part)
		r.Remove(part + ".json")
		return UploadStatus{}, mapError(err)
	}
	m.uploads[id] = u
	m.noteUpload(root, id, part)
	return u.status(), nil
}
func (m *Manager) prune() {
	for id, u := range m.uploads {
		if !u.busy && m.now().Sub(u.touched) > uploadIdle {
			if r, err := m.openUpload(u); err == nil {
				_ = r.Remove(u.part)
				_ = r.Remove(u.part + ".json")
				r.Close()
			}
			m.forgetUpload(u.root, id)
			delete(m.uploads, id)
		}
	}
}
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
func (m *Manager) UploadStatus(id, user string) (UploadStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.uploads[id]
	if !ok || u.user != user {
		return UploadStatus{}, ErrNoUpload
	}
	return u.status(), nil
}
func (m *Manager) WriteChunk(id, user string, offset int64, data io.Reader) (UploadStatus, error) {
	return m.WriteChunkContext(context.Background(), id, user, offset, data)
}
func (m *Manager) WriteChunkContext(ctx context.Context, id, user string, offset int64, data io.Reader) (UploadStatus, error) {
	u, err := m.claim(id, user)
	if err != nil {
		return UploadStatus{}, err
	}
	defer m.release(u, false)
	if offset != u.offset.Load() {
		return UploadStatus{}, ErrMismatch
	}
	r, err := m.openUpload(u)
	if err != nil {
		return UploadStatus{}, err
	}
	defer r.Close()
	f, err := openPart(r, u, os.O_WRONLY)
	if err != nil {
		return UploadStatus{}, mapError(err)
	}
	defer f.Close()
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return UploadStatus{}, err
	}
	remaining := u.size - offset
	for remaining > 0 {
		window := int64(8 << 20)
		release := func() {}
		if m.Acquire != nil {
			window, release, err = m.Acquire(ctx)
			if err != nil {
				return u.status(), err
			}
		}
		if err = ctx.Err(); err != nil {
			release()
			return u.status(), err
		}
		n, copyErr := io.CopyBuffer(f, io.LimitReader(data, min(window, remaining)), make([]byte, 32<<10))
		syncErr := m.syncFile(f)
		if syncErr == nil { // Only durable bytes are acknowledged or evicted.
			start := u.offset.Load()
			u.offset.Add(n)
			page := int64(os.Getpagesize())
			end := (start + n) / page * page
			start = start / page * page
			if end > start {
				_ = unix.Fadvise(int(f.Fd()), start, end-start, unix.FADV_DONTNEED)
			}
		} else {
			_ = f.Truncate(u.offset.Load())
		}
		release()
		if err = errors.Join(copyErr, syncErr); err != nil {
			return u.status(), mapError(err)
		}
		remaining -= n
		if n < min(window, remaining+n) {
			break
		}
	}
	if err = f.Close(); err != nil {
		return u.status(), mapError(err)
	}
	return u.status(), nil
}
func (m *Manager) FinishUpload(id, user string) (UploadStatus, error) {
	u, err := m.claim(id, user)
	if err != nil {
		return UploadStatus{}, err
	}
	done := false
	defer func() { m.release(u, done) }()
	if u.offset.Load() != u.size {
		return UploadStatus{}, ErrIncomplete
	}
	if m.protected(u.root, u.name) {
		return UploadStatus{}, ErrProtected
	}
	r, err := m.openUpload(u)
	if err != nil {
		return UploadStatus{}, err
	}
	defer r.Close()
	f, err := openPart(r, u, os.O_WRONLY)
	if err != nil {
		return UploadStatus{}, mapError(err)
	}
	err = m.syncFile(f)
	if err == nil {
		_ = unix.Fadvise(int(f.Fd()), 0, 0, unix.FADV_DONTNEED)
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return UploadStatus{}, mapError(err)
	}
	if u.conflict == ConflictReplace {
		err = r.Rename(u.part, u.name)
	} else {
		err = publishNewFile(r, u.part, u.name)
	}
	if err != nil {
		return UploadStatus{}, mapError(err)
	}
	m.own(r, u.name, u.uid, false)
	_ = r.Remove(u.part + ".json")
	m.forgetUpload(u.root, u.id)
	// Persist the directory entry as well as the file data.
	parent, err := r.Open(path.Dir(u.name))
	if err == nil {
		err = parent.Sync()
		err = errors.Join(err, parent.Close())
	}
	done = true
	if err != nil {
		return UploadStatus{}, mapError(err)
	}
	return u.status(), nil
}
func (m *Manager) AbortUpload(id, user string) error {
	u, err := m.claim(id, user)
	if err != nil {
		return err
	}
	done := false
	defer func() { m.release(u, done) }()
	r, err := m.openUpload(u)
	if err != nil {
		return err
	}
	defer r.Close()
	if err = r.Remove(u.part); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return mapError(err)
	}
	if err = r.Remove(u.part + ".json"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return mapError(err)
	}
	m.forgetUpload(u.root, u.id)
	done = true
	return nil
}

// Discover only the current account's uploads in the requested directory.
// Metadata stays beside the part file on its data volume, never on the stick.
func (m *Manager) Uploads(root Root, dir, user string, uid int) ([]UploadStatus, error) {
	dir, err := clean(dir)
	if err != nil {
		return nil, err
	}
	if !root.Writable {
		return nil, ErrReadOnly
	}
	if m.Available != nil && !m.Available(root) {
		return nil, ErrVolumeChanged
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	dev, ino, err := identity(r)
	if err != nil {
		return nil, err
	}
	folder, err := r.Open(dir)
	if err != nil {
		return nil, err
	}
	defer folder.Close()
	result := []UploadStatus{}
	m.mu.Lock()
	defer m.mu.Unlock()
	for {
		entries, readErr := folder.ReadDir(128)
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), partSuffix+".json") {
				continue
			}
			metaPath := path.Join(dir, entry.Name())
			file, err := r.OpenFile(metaPath, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
			if err != nil {
				continue
			}
			metaInfo, metaErr := file.Stat()
			if metaErr != nil || !trustedPart(metaInfo) {
				file.Close()
				continue
			}
			var record uploadRecord
			err = json.NewDecoder(io.LimitReader(file, 8192)).Decode(&record)
			file.Close()
			if err != nil || record.User != user || record.Root != root.ID || len(record.ID) != 24 || record.Size < 0 {
				continue
			}
			if _, err = hex.DecodeString(record.ID); err != nil {
				continue
			}
			if path.Dir(record.Name) != dir || path.Dir(record.Part) != dir || record.Part+".json" != metaPath || !validName(path.Base(record.Name)) {
				continue
			}
			if record.Part != uploadPartName(record.Name, record.ID) && record.Part != path.Join(dir, "."+path.Base(record.Name)+"."+record.ID+partSuffix) {
				continue
			}
			if record.Conflict != "" && record.Conflict != ConflictRename && record.Conflict != ConflictReplace {
				continue
			}
			info, err := r.Lstat(record.Part)
			if err != nil || !trustedPart(info) || info.Size() > record.Size {
				continue
			}
			u := m.uploads[record.ID]
			if u == nil {
				u = &upload{id: record.ID, user: user, uid: uid, root: root, name: record.Name, part: record.Part, conflict: record.Conflict, size: record.Size, device: dev, inode: ino, partInode: info.Sys().(*syscall.Stat_t).Ino, partDevice: uint64(info.Sys().(*syscall.Stat_t).Dev), touched: m.now()}
				u.offset.Store(info.Size())
				m.uploads[u.id] = u
			}
			if u.user == user && u.root.ID == root.ID && u.name == record.Name {
				result = append(result, u.status())
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	return result, nil
}

// On recovery, compare the already received prefix in bounded requests before
// appending the reselected file. A different source can never silently splice.
func (m *Manager) VerifyChunk(id, user string, offset int64, data io.Reader) error {
	u, err := m.claim(id, user)
	if err != nil {
		return err
	}
	defer m.release(u, false)
	if offset < 0 || offset > u.offset.Load() {
		return ErrMismatch
	}
	r, err := m.openUpload(u)
	if err != nil {
		return err
	}
	defer r.Close()
	f, err := openPart(r, u, os.O_RDONLY)
	if err != nil {
		return err
	}
	defer f.Close()
	defer unix.Fadvise(int(f.Fd()), offset, min(int64(8<<20), u.offset.Load()-offset), unix.FADV_DONTNEED)
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	left := min(int64(8<<20), u.offset.Load()-offset)
	a, b := make([]byte, 32<<10), make([]byte, 32<<10)
	for left > 0 {
		n, readErr := io.ReadFull(data, a[:min(int64(len(a)), left)])
		if n > 0 {
			if _, err = io.ReadFull(f, b[:n]); err != nil || !bytes.Equal(a[:n], b[:n]) {
				return ErrMismatch
			}
			left -= int64(n)
		}
		if readErr != nil {
			return readErr
		}
	}
	return nil
}

func renameUpload(name, ext string, n int) string {
	return fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), n, ext)
}

// Upload internals are created by titand. Do not adopt or follow files replaced
// through SMB or another local writer, even when they remain inside the root.
func trustedPart(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode().IsRegular() && stat.Nlink == 1 && stat.Uid == uint32(os.Geteuid()) && info.Mode().Perm() == 0600
}
func openPart(root *os.Root, u *upload, flags int) (*os.File, error) {
	f, err := root.OpenFile(u.part, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !trustedPart(info) || info.Sys().(*syscall.Stat_t).Ino != u.partInode || uint64(info.Sys().(*syscall.Stat_t).Dev) != u.partDevice || info.Size() != u.offset.Load() {
		f.Close()
		return nil, ErrMismatch
	}
	return f, nil
}

func (m *Manager) UploadRoot(id, user string) (Root, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.uploads[id]
	if u == nil || u.user != user {
		return Root{}, ErrNoUpload
	}
	return u.root, nil
}

// A full-length target name must leave room for neither the part suffix nor metadata.
func uploadPartName(name, id string) string {
	return path.Join(path.Dir(name), ".titanos-upload-"+id+partSuffix)
}
