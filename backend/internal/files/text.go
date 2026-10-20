package files

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"syscall"
	"unicode/utf8"
)

const maxText = 512 << 10

var ErrTextConflict = errors.New("files: text changed since it was opened")
var ErrInvalidJSON = errors.New("files: invalid JSON content")
var ErrNotText = errors.New("files: not a supported UTF-8 text file")

type TextFile struct {
	Content string `json:"content"`
	Version string `json:"version"`
}

func textVersion(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// Only common text formats are offered; decoding is strict and bounded too.
func textName(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".txt", ".md", ".markdown", ".json", ".yaml", ".yml", ".ini", ".conf", ".cfg", ".toml", ".xml", ".csv", ".tsv", ".log", ".sh", ".bash", ".py", ".js", ".ts", ".html", ".css", ".sql", ".env":
		return true
	}
	return path.Base(name) == "Dockerfile" || path.Base(name) == "Makefile" || path.Base(name) == ".env"
}

func (m *Manager) openText(root Root, name string) (*os.Root, *os.File, os.FileInfo, []byte, error) {
	return m.openTextVersion(root, name, false)
}
func (m *Manager) openTextVersion(root Root, name string, previous bool) (*os.Root, *os.File, os.FileInfo, []byte, error) {
	if !root.Editable && !root.Writable {
		return nil, nil, nil, nil, ErrReadOnly
	}
	name, err := clean(name)
	if err != nil || name == "." || strings.HasSuffix(name, ".prev") {
		return nil, nil, nil, nil, ErrInvalid
	}
	if root.secret(name) {
		return nil, nil, nil, nil, ErrNotFound
	}
	if m.protected(root, name) {
		return nil, nil, nil, nil, ErrProtected
	}
	if root.Kind != "stick" && m.Available != nil && !m.Available(root) {
		return nil, nil, nil, nil, ErrVolumeChanged
	}
	originalName := name
	if previous {
		name += ".prev"
		if root.secret(name) || m.protected(root, name) {
			return nil, nil, nil, nil, ErrProtected
		}
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return nil, nil, nil, nil, mapError(err)
	}
	fail := func(e error) (*os.Root, *os.File, os.FileInfo, []byte, error) {
		r.Close()
		return nil, nil, nil, nil, e
	}
	info, err := r.Lstat(name)
	if err != nil {
		return fail(mapError(err))
	}
	if !info.Mode().IsRegular() {
		return fail(ErrInvalid)
	}
	if info.Size() > maxText {
		return fail(ErrNotText)
	}
	// Keep the opened inode pinned; never follow a symbolic link swapped in after Lstat.
	f, err := r.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fail(mapError(err))
	}
	if m.Protected != nil {
		resolved, e := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", f.Fd()))
		if e != nil {
			f.Close()
			return fail(mapError(e))
		}
		if m.Protected(resolved) {
			f.Close()
			return fail(ErrProtected)
		}
	}
	actual, err := f.Stat()
	if err != nil || !os.SameFile(info, actual) {
		f.Close()
		return fail(ErrTextConflict)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxText+1))
	if err != nil {
		f.Close()
		return fail(mapError(err))
	}
	if len(data) > maxText || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) || !textName(originalName) {
		f.Close()
		return fail(ErrNotText)
	}
	return r, f, actual, data, nil
}

func (m *Manager) ReadText(root Root, name string) (TextFile, error) {
	r, f, _, data, err := m.openText(root, name)
	if err != nil {
		return TextFile{}, err
	}
	defer r.Close()
	defer f.Close()
	return TextFile{Content: string(data), Version: textVersion(data)}, nil
}

// SaveText retains the original API for internal callers. Browser saves use a version.
func (m *Manager) SaveText(root Root, name, content string) error {
	_, err := m.SaveTextVersion(root, name, content, "")
	return err
}

// SaveTextVersion rejects stale edits and writes through a temporary file. The
// previous contents are retained beside the file, without following .prev links.
func (m *Manager) SaveTextVersion(root Root, name, content, version string) (TextFile, error) {
	m.textMu.Lock()
	defer m.textMu.Unlock()
	name, cleanErr := clean(name)
	if cleanErr != nil {
		return TextFile{}, cleanErr
	}
	if len(content) > maxText || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return TextFile{}, ErrInvalid
	}
	if strings.EqualFold(path.Ext(name), ".json") && !json.Valid([]byte(strings.TrimPrefix(content, "\ufeff"))) {
		return TextFile{}, ErrInvalidJSON
	}
	r, f, info, old, err := m.openText(root, name)
	if err != nil {
		return TextFile{}, err
	}
	defer r.Close()
	defer f.Close()
	name, _ = clean(name)
	if version != "" && textVersion(old) != version {
		return TextFile{}, ErrTextConflict
	}
	writeTemp := func(data []byte) (string, error) {
		raw := make([]byte, 8)
		if _, err := rand.Read(raw); err != nil {
			return "", err
		}
		temp := path.Join(path.Dir(name), ".titanos-edit-"+hex.EncodeToString(raw)+partSuffix)
		out, err := r.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return "", mapError(err)
		}
		ok := false
		defer func() {
			out.Close()
			if !ok {
				r.Remove(temp)
			}
		}()
		if _, err = out.Write(data); err != nil {
			return "", mapError(err)
		}
		// Preserve ownership and permission bits for shares and application configs.
		if stat, yes := info.Sys().(*syscall.Stat_t); yes && root.Kind != "stick" {
			if err = out.Chown(int(stat.Uid), int(stat.Gid)); err != nil {
				return "", mapError(err)
			}
		}
		if err = out.Chmod(info.Mode().Perm()); err != nil {
			return "", mapError(err)
		}
		if err = copyTextMetadata(f, out); err != nil {
			return "", mapError(err)
		}
		if err = m.syncFile(out); err != nil {
			return "", mapError(err)
		}
		if err = out.Close(); err != nil {
			return "", mapError(err)
		}
		ok = true
		return temp, nil
	}
	temp, err := writeTemp([]byte(content))
	if err != nil {
		return TextFile{}, err
	}
	defer r.Remove(temp)
	// An external writer may have changed the file while the edit was prepared.
	current, err := r.Lstat(name)
	if err != nil || !os.SameFile(info, current) || current.Size() != info.Size() || !current.ModTime().Equal(info.ModTime()) {
		return TextFile{}, ErrTextConflict
	}
	if m.protected(root, name) || m.protected(root, name+".prev") {
		return TextFile{}, ErrProtected
	}
	previous, err := writeTemp(old)
	if err != nil {
		return TextFile{}, err
	}
	defer r.Remove(previous)
	if err = r.Rename(previous, name+".prev"); err != nil {
		return TextFile{}, mapError(err)
	}
	if err = r.Rename(temp, name); err != nil {
		return TextFile{}, mapError(err)
	}
	parent, err := r.Open(path.Dir(name))
	if err != nil {
		return TextFile{}, mapError(err)
	}
	err = parent.Sync()
	closeErr := parent.Close()
	if err != nil {
		return TextFile{}, mapError(err)
	}
	if closeErr != nil {
		return TextFile{}, mapError(closeErr)
	}
	return TextFile{Content: content, Version: textVersion([]byte(content))}, nil
}

// ReadPreviousText uses the original file's type and authorization, but never
// follows a .prev symlink. Restore goes through the normal versioned save.
func (m *Manager) ReadPreviousText(root Root, name string) (TextFile, error) {
	r, f, _, data, err := m.openTextVersion(root, name, true)
	if err != nil {
		return TextFile{}, err
	}
	defer r.Close()
	defer f.Close()
	return TextFile{Content: string(data), Version: textVersion(data)}, nil
}
