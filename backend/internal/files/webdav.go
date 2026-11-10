package files

import (
	"context"
	"errors"
	"io"
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

func (d *davFS) OpenFile(ctx context.Context, raw string, flag int, _ os.FileMode) (webdav.File, error) {
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
	// A whole new content (PUT, COPY onto a file) is written next to the
	// file and takes its name only when all of it is there: written into
	// the file itself, a lost connection or a full volume would leave it
	// cut short with the old version gone, and readers would see half a
	// file during every save.
	if flag&os.O_TRUNC != 0 && flag&(os.O_WRONLY|os.O_RDWR) != 0 {
		if info, err := r.Lstat(p); err == nil && !info.Mode().IsRegular() {
			return nil, fs.ErrInvalid
		} else if err != nil && (!errors.Is(err, fs.ErrNotExist) || flag&os.O_CREATE == 0) {
			return nil, err
		}
		part := path.Join(path.Dir(p), ".titanos-dav-"+randomID()+partSuffix)
		f, err := r.OpenFile(part, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o660)
		if err != nil {
			return nil, err
		}
		expected, _ := ctx.Value(davLengthKey{}).(int64)
		if _, known := ctx.Value(davLengthKey{}).(int64); !known {
			expected = -1
		}
		return &davWrite{File: f, fs: d, ctx: ctx, part: part, final: p, expected: expected}, nil
	}
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
	if d.root.recyclable(p) {
		return d.m.recycle(r, d.root, p, d.user)
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

type davLengthKey struct{}

// WithDAVLength notes how many bytes the body of a WebDAV PUT announced
// (-1 if it did not say): a file that arrives shorter is not put in place.
func WithDAVLength(ctx context.Context, length int64) context.Context {
	return context.WithValue(ctx, davLengthKey{}, length)
}

// davWrite is a file being saved over WebDAV: a part file that takes the
// name of the file on Close, if it is complete.
type davWrite struct {
	*os.File
	fs       *davFS
	ctx      context.Context
	part     string
	final    string
	expected int64
	failed   bool
	closed   bool
}

func (f *davWrite) Write(p []byte) (int, error) {
	n, err := f.File.Write(p)
	if err != nil {
		f.failed = true
	}
	return n, err
}

// Close puts the part file in place. What is incomplete is dropped and the
// file there stays as it was: a write failed (volume full), the client went
// away, or fewer bytes came than the request announced.
func (f *davWrite) Close() error {
	if f.closed {
		return fs.ErrClosed
	}
	f.closed = true
	info, statErr := f.File.Stat()
	syncErr := f.fs.m.syncFile(f.File)
	closeErr := f.File.Close()
	r, err := f.fs.open()
	if err != nil {
		return err
	}
	defer r.Close()
	drop := func(err error) error {
		_ = r.Remove(f.part) // cleanup, best effort: the sweep of part files takes what is left
		return err
	}
	switch {
	case f.failed, statErr != nil, syncErr != nil, closeErr != nil:
		return drop(errors.Join(statErr, syncErr, closeErr, io.ErrShortWrite))
	case f.ctx != nil && f.ctx.Err() != nil:
		return drop(f.ctx.Err())
	case f.expected >= 0 && info.Size() != f.expected:
		return drop(io.ErrUnexpectedEOF)
	}
	f.fs.m.own(r, f.part, f.fs.uid, false)
	if err := r.Rename(f.part, f.final); err != nil {
		return drop(err)
	}
	return nil
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
