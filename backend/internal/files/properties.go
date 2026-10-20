package files

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"mime"
	"os"
	"path"
	"syscall"
	"time"
)

type Access struct {
	Read   bool   `json:"read"`
	Write  bool   `json:"write"`
	Delete bool   `json:"delete"`
	Reason string `json:"reason"`
}

type Properties struct {
	Version     string    `json:"version"`
	Access      Access    `json:"access"`
	Path        string    `json:"path"`
	Kind        string    `json:"kind"`
	Size        int64     `json:"size"`
	Changed     time.Time `json:"changed"`
	Accessed    time.Time `json:"accessed"`
	Modified    time.Time `json:"modified"`
	Permissions string    `json:"permissions"`
	UID         uint32    `json:"uid"`
	GID         uint32    `json:"gid"`
	SHA256      string    `json:"sha256,omitempty"`
}

func (m *Manager) Properties(ctx context.Context, root Root, p string, checksum bool) (Properties, error) {
	result := Properties{}
	p, err := clean(p)
	if err != nil || root.secret(p) {
		return result, ErrNotFound
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return result, mapError(err)
	}
	defer r.Close()
	info, err := r.Lstat(p)
	if err != nil {
		return result, mapError(err)
	}
	result = Properties{Version: entryVersion(info), Path: p, Size: info.Size(), Modified: info.ModTime(), Permissions: info.Mode().String(), Kind: mime.TypeByExtension(path.Ext(p))}
	result.Access = Access{Read: true, Write: root.Writable || root.Editable && info.Mode().IsRegular() && textName(p) && info.Size() <= maxText, Delete: root.Writable && p != "."}
	if !root.Writable {
		result.Access.Reason = "readOnly"
		if root.Kind == "share" {
			result.Access.Reason = "share"
		}
		if root.Kind == "stick" {
			result.Access.Reason = "stick"
		}
	}
	if m.protected(root, p) {
		result.Access.Write = false
		result.Access.Delete = false
		result.Access.Reason = "protected"
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		result.UID = stat.Uid
		result.GID = stat.Gid
		result.Changed = time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec)
		result.Accessed = time.Unix(stat.Atim.Sec, stat.Atim.Nsec)
	}
	if info.IsDir() {
		result.Kind = "directory"
	} else if info.Mode()&os.ModeSymlink != 0 {
		result.Kind = "symlink"
	} else if result.Kind == "" {
		result.Kind = "application/octet-stream"
	}
	if checksum {
		if !info.Mode().IsRegular() {
			return result, ErrInvalid
		}
		f, err := r.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return result, mapError(err)
		}
		defer f.Close()
		before, err := f.Stat()
		if err != nil {
			return result, err
		}
		if !before.Mode().IsRegular() {
			return result, ErrInvalid
		}
		hash := sha256.New()
		buf := make([]byte, 1<<20)
		for {
			if err = ctx.Err(); err != nil {
				return result, err
			}
			n, e := f.Read(buf)
			if n > 0 {
				hash.Write(buf[:n])
			}
			if e == io.EOF {
				break
			}
			if e != nil {
				return result, e
			}
		}
		after, err := f.Stat()
		if err != nil {
			return result, err
		}
		current, err := r.Lstat(p)
		if err != nil || !os.SameFile(info, before) || !os.SameFile(before, current) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
			return result, ErrTextConflict
		}
		result.SHA256 = fmt.Sprintf("%x", hash.Sum(nil))
	}
	return result, nil
}
