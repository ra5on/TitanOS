package files

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// MaxContent is the largest file PUT /api/v1/files/content takes (a web
// app saves one document from the browser's memory).
const MaxContent = 2 << 30

// ErrChanged is a file that changed since the web app opened it.
var ErrChanged = errors.New("files: the file changed since it was opened")

// ContentResult is the answer to a whole-file write.
type ContentResult struct {
	Modified time.Time `json:"modified"`
	// MTime is the modification time in nanoseconds since 1970, as text
	// (it does not fit a JSON number).
	MTime string `json:"mtime"`
	Size  int64  `json:"size"`
}

// Expectation is what a save expects of the file it replaces.
type Expectation struct {
	// None: the file must not exist yet.
	None bool
	At   time.Time
	// Seconds: At came from a listing, compared to the second.
	Seconds bool
}

// ParseExpect reads expect: "none", nanoseconds since 1970 or an
// RFC 3339 time (the "modified" of a listing, which is whole seconds).
func ParseExpect(expect string) (Expectation, error) {
	expect = strings.TrimSpace(expect)
	switch {
	case expect == "none":
		return Expectation{None: true}, nil
	case expect == "":
		return Expectation{}, ErrInvalid
	}
	if ns, err := strconv.ParseInt(expect, 10, 64); err == nil && ns > 0 {
		return Expectation{At: time.Unix(0, ns)}, nil
	}
	at, err := time.Parse(time.RFC3339Nano, expect)
	if err != nil {
		return Expectation{}, ErrInvalid
	}
	return Expectation{At: at, Seconds: at.Nanosecond() == 0}, nil
}

func (e Expectation) matches(mod time.Time) bool {
	if e.Seconds {
		return mod.Truncate(time.Second).Equal(e.At.Truncate(time.Second))
	}
	return mod.Equal(e.At)
}

// WriteContent replaces or creates a whole file from body (size bytes):
// the account's rights as for uploads, the data in a hidden part file in
// the same folder first, then an atomic rename. An existing file keeps
// its owner and mode, and its former contents as <name>.prev as with the
// text editor; it must still be the one the app opened (ErrChanged). A
// new file (expect none) must not exist and gets the account as owner.
func (m *Manager) WriteContent(root Root, name string, body io.Reader, size int64, uid int, expect Expectation) (ContentResult, error) {
	if !root.Writable || root.Kind == "stick" {
		return ContentResult{}, ErrReadOnly
	}
	if size < 0 || size > MaxContent {
		return ContentResult{}, ErrNoSpace
	}
	name, err := clean(name)
	if err != nil || name == "." || !validName(path.Base(name)) || root.secret(name) || strings.HasSuffix(name, ".prev") {
		return ContentResult{}, ErrInvalid
	}
	if m.Available != nil && !m.Available(root) {
		return ContentResult{}, ErrVolumeChanged
	}
	var linkInfo os.FileInfo
	if !expect.None {
		// A link is saved where it points, so saving keeps the link. The
		// listing shows the link's own time; either one matches.
		if info, err := os.Lstat(filepath.Join(root.Dir, filepath.FromSlash(name))); err == nil && info.Mode()&os.ModeSymlink != 0 {
			linkInfo = info
		}
		if name, err = root.followLink(name); err != nil {
			if errors.Is(err, ErrNotFound) {
				// Deleted or moved since it was opened.
				return ContentResult{}, ErrChanged
			}
			return ContentResult{}, err
		}
	}
	if m.protected(root, name) || m.protected(root, name+".prev") {
		return ContentResult{}, ErrProtected
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return ContentResult{}, mapError(err)
	}
	defer r.Close()
	parent, err := r.OpenRoot(path.Dir(name))
	if err != nil {
		return ContentResult{}, mapError(err)
	}
	defer parent.Close()
	// Pin the folder and check where it really is before writing.
	directory, err := parent.Open(".")
	if err != nil {
		return ContentResult{}, mapError(err)
	}
	resolved, resolveErr := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", directory.Fd()))
	directory.Close()
	if resolveErr != nil {
		return ContentResult{}, mapError(resolveErr)
	}
	actual := filepath.Join(resolved, path.Base(name))
	if rel, err := filepath.Rel(root.Dir, actual); err != nil || root.secret(filepath.ToSlash(rel)) {
		return ContentResult{}, ErrInvalid
	}
	if m.Protected != nil && m.Protected(actual) {
		return ContentResult{}, ErrProtected
	}
	base := path.Base(name)
	existing, statErr := parent.Lstat(base)
	switch {
	case expect.None && statErr == nil:
		return ContentResult{}, ErrExists
	case expect.None && !errors.Is(statErr, os.ErrNotExist):
		return ContentResult{}, mapError(statErr)
	case !expect.None && statErr != nil:
		if errors.Is(statErr, os.ErrNotExist) {
			// Deleted or moved since it was opened.
			return ContentResult{}, ErrChanged
		}
		return ContentResult{}, mapError(statErr)
	case !expect.None && !existing.Mode().IsRegular():
		return ContentResult{}, ErrInvalid
	case !expect.None && !expect.matches(existing.ModTime()) && (linkInfo == nil || !expect.matches(linkInfo.ModTime())):
		return ContentResult{}, ErrChanged
	}
	if free := freeBytes(resolved); free >= 0 && free < size+(16<<20) {
		return ContentResult{}, ErrNoSpace
	}

	raw := make([]byte, 16)
	if _, err = rand.Read(raw); err != nil {
		return ContentResult{}, err
	}
	temp := ".titanos-write-" + hex.EncodeToString(raw) + partSuffix
	f, err := parent.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ContentResult{}, mapError(err)
	}
	defer parent.Remove(temp)
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(body, size+1))
	if err != nil {
		return ContentResult{}, mapError(err)
	}
	if n != size {
		return ContentResult{}, ErrIncomplete
	}
	if expect.None {
		m.own(parent, temp, uid, false)
	} else {
		if stat, ok := existing.Sys().(*syscall.Stat_t); ok {
			if err := f.Chown(int(stat.Uid), int(stat.Gid)); err != nil {
				return ContentResult{}, mapError(err)
			}
		}
		if err := f.Chmod(existing.Mode().Perm()); err != nil {
			return ContentResult{}, mapError(err)
		}
	}
	if err = m.syncFile(f); err != nil {
		return ContentResult{}, mapError(err)
	}
	if err = f.Close(); err != nil {
		return ContentResult{}, mapError(err)
	}
	if expect.None {
		if err := publishNewFile(parent, temp, base); err != nil {
			return ContentResult{}, mapError(err)
		}
	} else {
		// An external writer may have changed the file meanwhile.
		current, err := parent.Lstat(base)
		if err != nil || !os.SameFile(existing, current) || !current.ModTime().Equal(existing.ModTime()) || current.Size() != existing.Size() {
			return ContentResult{}, ErrChanged
		}
		// The old file stays beside the new one, as with the text editor.
		parent.Remove(base + ".prev")
		if err := parent.Link(base, base+".prev"); err != nil {
			// FAT and exFAT know no hard links: move the old file aside instead.
			if err := parent.Rename(base, base+".prev"); err != nil {
				return ContentResult{}, mapError(err)
			}
		}
		if err := parent.Rename(temp, base); err != nil {
			return ContentResult{}, mapError(err)
		}
	}
	if dir, err := parent.Open("."); err == nil {
		_ = dir.Sync() // the rename is done; syncing the folder only makes it durable sooner
		dir.Close()
	}
	info, err := parent.Lstat(base)
	if err != nil {
		return ContentResult{}, mapError(err)
	}
	mod := info.ModTime()
	return ContentResult{Modified: mod.UTC(), MTime: strconv.FormatInt(mod.UnixNano(), 10), Size: info.Size()}, nil
}

// freeBytes is the space left for the account in dir, -1 if unknown.
func freeBytes(dir string) int64 {
	var st syscall.Statfs_t
	if syscall.Statfs(dir, &st) != nil {
		return -1
	}
	return int64(st.Bavail) * int64(st.Bsize)
}
