package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/viewer/proto"
)

// sanitizeName turns a browser-supplied file name into a safe base name
// for /profile/Hochgeladen. It returns "" if nothing usable remains.
func sanitizeName(name string) string {
	if !utf8.ValidString(name) {
		name = strings.ToValidUTF8(name, "_")
	}
	// keep only the last path element of either separator style
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			continue
		case strings.ContainsRune(`<>:"|?*`, r):
			b.WriteRune('_')
		case unicode.Is(unicode.Cf, r): // bidi overrides, zero-width chars
			continue
		default:
			b.WriteRune(r)
		}
	}
	s := strings.TrimSpace(b.String())
	s = strings.TrimLeft(s, ".")
	s = strings.TrimRight(s, ". ")
	for len(s) > 200 { // bytes; cut at a rune boundary, keep the extension
		ext := filepath.Ext(s)
		if len(ext) > 20 {
			ext = ""
		}
		stem := s[:len(s)-len(ext)]
		_, size := utf8.DecodeLastRuneInString(stem)
		s = stem[:len(stem)-size] + ext
	}
	if s == "" || s == "." || s == ".." {
		return ""
	}
	return s
}

// uniquePath returns dir/name, or "name (2).ext" … if it exists.
func uniquePath(dir, name string) string {
	p := filepath.Join(dir, name)
	if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
		return p
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; i < 10000; i++ {
		p = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
			return p
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, time.Now().UnixNano(), ext))
}

// upload is one file being received.
type upload struct {
	id      uint32
	name    string
	size    int64
	written int64
	f       *os.File
	tmp     string
}

// uploads tracks the uploads of one client.
type uploads struct {
	mu sync.Mutex
	m  map[uint32]*upload
}

func newUploads() *uploads { return &uploads{m: map[uint32]*upload{}} }

// begin opens a temp file for an announced upload.
func (u *uploads) begin(id uint32, name string, size int64) (*upload, string) {
	if size < 0 || size > proto.MaxUploadSize {
		return nil, proto.UploadTooLarge
	}
	clean := sanitizeName(name)
	if clean == "" {
		return nil, proto.UploadBadName
	}
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, proto.UploadFailed
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if old, ok := u.m[id]; ok {
		old.abort()
		delete(u.m, id)
	}
	if len(u.m) >= 16 {
		return nil, proto.UploadFailed
	}
	f, err := os.CreateTemp(uploadDir, ".upload-*.part")
	if err != nil {
		return nil, proto.UploadFailed
	}
	up := &upload{id: id, name: clean, size: size, f: f, tmp: f.Name()}
	u.m[id] = up
	return up, ""
}

// chunk writes data; done is true when the file is complete (then the
// final name is returned). errCode is set on failure (upload dropped).
func (u *uploads) chunk(id uint32, data []byte) (done bool, final string, errCode string) {
	u.mu.Lock()
	up, ok := u.m[id]
	u.mu.Unlock()
	if !ok {
		return false, "", proto.UploadFailed
	}
	if up.written+int64(len(data)) > up.size {
		u.drop(id)
		return false, "", proto.UploadTooLarge
	}
	if _, err := up.f.Write(data); err != nil {
		u.drop(id)
		return false, "", proto.UploadFailed
	}
	up.written += int64(len(data))
	if up.written < up.size {
		return false, "", ""
	}
	return u.finish(id)
}

func (u *uploads) finish(id uint32) (bool, string, string) {
	u.mu.Lock()
	up := u.m[id]
	delete(u.m, id)
	u.mu.Unlock()
	if err := up.f.Close(); err != nil {
		os.Remove(up.tmp)
		return false, "", proto.UploadFailed
	}
	dst := uniquePath(uploadDir, up.name)
	if err := os.Rename(up.tmp, dst); err != nil {
		os.Remove(up.tmp)
		return false, "", proto.UploadFailed
	}
	_ = os.Chmod(dst, 0o644)
	return true, filepath.Base(dst), ""
}

func (u *uploads) drop(id uint32) {
	u.mu.Lock()
	up, ok := u.m[id]
	delete(u.m, id)
	u.mu.Unlock()
	if ok {
		up.abort()
	}
}

func (up *upload) abort() {
	up.f.Close()
	os.Remove(up.tmp)
}

// abortAll removes unfinished uploads (client gone).
func (u *uploads) abortAll() {
	u.mu.Lock()
	defer u.mu.Unlock()
	for id, up := range u.m {
		up.abort()
		delete(u.m, id)
	}
}

// download is a finished file in /profile/Downloads.
type download struct {
	ID   string
	Name string
	Path string
	Size int64
}

// downloads watches the download directory by polling. A file counts as
// finished when it is not a Chromium temp file (.crdownload) and its
// size and mtime did not change between two polls.
type downloads struct {
	mu      sync.Mutex
	next    int
	byID    map[string]download
	known   map[string]string // path → "size/mtime" announced
	pending map[string]string // path → "size/mtime" seen once
}

func newDownloads() *downloads {
	d := &downloads{byID: map[string]download{}, known: map[string]string{}, pending: map[string]string{}}
	// files from earlier sessions are not announced again
	for _, f := range d.scan() {
		d.known[f.Path] = f.stamp
	}
	return d
}

type dlFile struct {
	Path  string
	Name  string
	Size  int64
	stamp string
}

func (d *downloads) scan() []dlFile {
	ents, err := os.ReadDir(downloadDir)
	if err != nil {
		return nil
	}
	var out []dlFile
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, ".") || strings.HasSuffix(n, ".crdownload") || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, dlFile{
			Path:  filepath.Join(downloadDir, n),
			Name:  n,
			Size:  info.Size(),
			stamp: strconv.FormatInt(info.Size(), 10) + "/" + strconv.FormatInt(info.ModTime().UnixNano(), 10),
		})
	}
	return out
}

// poll returns newly finished downloads.
func (d *downloads) poll() []download {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []download
	seen := map[string]bool{}
	for _, f := range d.scan() {
		seen[f.Path] = true
		if d.known[f.Path] == f.stamp {
			continue
		}
		if d.pending[f.Path] != f.stamp {
			d.pending[f.Path] = f.stamp
			continue
		}
		delete(d.pending, f.Path)
		d.known[f.Path] = f.stamp
		d.next++
		dl := download{ID: "d" + strconv.Itoa(d.next), Name: f.Name, Path: f.Path, Size: f.Size}
		d.byID[dl.ID] = dl
		out = append(out, dl)
	}
	for p := range d.pending {
		if !seen[p] {
			delete(d.pending, p)
		}
	}
	return out
}

func (d *downloads) get(id string) (download, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	dl, ok := d.byID[id]
	return dl, ok
}
