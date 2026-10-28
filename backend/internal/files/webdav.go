package files

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"

	"golang.org/x/net/webdav"
)

// DAV is a share as WebDAV sees it (docs/WEBDAV.md): the same rules as the
// file manager. Everything goes through os.Root, so no path or link leads
// out of the share; internal files stay hidden; read-only access refuses
// writes; deleting moves into the recycle bin of the share when it has
// one; new files and folders belong to the account and the group users.
func (m *Manager) DAV(root Root, user string, uid int) webdav.FileSystem {
	return &davFS{m: m, root: root, user: user, uid: uid}
}

type davFS struct {
	m    *Manager
	root Root
	user string
	uid  int
}

// name turns a WebDAV path into one inside the root, or fails as "not
// found" for hidden and invalid names (so nothing internal is admitted).
func (d *davFS) name(raw string) (string, error) {
	p, err := clean(raw)
	if err != nil || d.root.secret(p) {
		return "", os.ErrNotExist
	}
	return p, nil
}

func (d *davFS) open() (*os.Root, error) { return os.OpenRoot(d.root.Dir) }

func (d *davFS) writable(p string) error {
	if !d.root.Writable {
		return os.ErrPermission
	}
	if p == "." || d.m.protected(d.root, p) {
		return os.ErrPermission
	}
	return nil
}

func (d *davFS) Mkdir(_ context.Context, raw string, _ os.FileMode) error {
	p, err := d.name(raw)
	if err != nil {
		return err
	}
	if err := d.writable(p); err != nil {
		return err
	}
	r, err := d.open()
	if err != nil {
		return err
	}
	defer r.Close()
	if err := r.Mkdir(p, 0o770); err != nil {
		return err
	}
	d.m.own(r, p, d.uid, true)
	return nil
}

func (d *davFS) OpenFile(_ context.Context, raw string, flag int, _ os.FileMode) (webdav.File, error) {
	p, err := d.name(raw)
	if err != nil {
		return nil, err
	}
	writes := flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0
	if writes {
		if err := d.writable(p); err != nil {
			return nil, err
		}
	}
	r, err := d.open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	created := false
	if flag&os.O_CREATE != 0 {
		if _, err := r.Lstat(p); errors.Is(err, fs.ErrNotExist) {
			created = true
		}
	}
	f, err := r.OpenFile(p, flag, 0o660)
	if err != nil {
		return nil, err
	}
	if created {
		d.m.own(r, p, d.uid, false)
	}
	return &davFile{File: f, fs: d, dir: p}, nil
}

func (d *davFS) RemoveAll(_ context.Context, raw string) error {
	p, err := d.name(raw)
	if err != nil {
		return err
	}
	if err := d.writable(p); err != nil {
		return err
	}
	r, err := d.open()
	if err != nil {
		return err
	}
	defer r.Close()
	if _, err := r.Lstat(p); err != nil {
		return err
	}
	if d.root.Recycle {
		return d.m.recycle(r, p, d.user)
	}
	return r.RemoveAll(p)
}

func (d *davFS) Rename(_ context.Context, rawOld, rawNew string) error {
	from, err := d.name(rawOld)
	if err != nil {
		return err
	}
	to, err := d.name(rawNew)
	if err != nil {
		return err
	}
	if err := d.writable(from); err != nil {
		return err
	}
	if err := d.writable(to); err != nil {
		return err
	}
	r, err := d.open()
	if err != nil {
		return err
	}
	defer r.Close()
	return r.Rename(from, to)
}

func (d *davFS) Stat(_ context.Context, raw string) (os.FileInfo, error) {
	p, err := d.name(raw)
	if err != nil {
		return nil, err
	}
	r, err := d.open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.Stat(p)
}

// davFile leaves internal files out of folder listings.
type davFile struct {
	*os.File
	fs  *davFS
	dir string
}

func (f *davFile) Readdir(count int) ([]fs.FileInfo, error) {
	items, err := f.File.Readdir(count)
	out := items[:0]
	for _, item := range items {
		if hidden(item.Name()) || f.fs.root.secret(path.Join(f.dir, item.Name())) {
			continue
		}
		out = append(out, item)
	}
	return out, err
}
