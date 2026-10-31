package files

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
)

// MaxWrite is the largest file WriteFile accepts.
const MaxWrite = 64 << 20

// WriteFile writes a whole file, as MCP clients create documents: it
// creates missing folders, and replaces an existing file only with
// replace, keeping the old contents as <name>.prev. The data goes to a
// hidden part file first and becomes visible at once when complete.
func (m *Manager) WriteFile(root Root, name string, data []byte, uid int, replace bool) error {
	if !root.Writable || root.Kind == "stick" {
		return ErrReadOnly
	}
	name, err := clean(name)
	if err != nil || name == "." || !validName(path.Base(name)) || root.secret(name) {
		return ErrInvalid
	}
	if len(data) > MaxWrite {
		return ErrNoSpace
	}
	if m.protected(root, name) || m.protected(root, name+".prev") {
		return ErrProtected
	}
	if m.Available != nil && !m.Available(root) {
		return ErrVolumeChanged
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return mapError(err)
	}
	defer r.Close()
	if err := m.makeParents(r, path.Dir(name), uid); err != nil {
		return mapError(err)
	}
	// Pin the folder and check where it really is before writing.
	parent, err := r.OpenRoot(path.Dir(name))
	if err != nil {
		return mapError(err)
	}
	defer parent.Close()
	directory, err := parent.Open(".")
	if err != nil {
		return mapError(err)
	}
	resolved, resolveErr := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", directory.Fd()))
	directory.Close()
	if resolveErr != nil {
		return mapError(resolveErr)
	}
	actual := filepath.Join(resolved, path.Base(name))
	rel, err := filepath.Rel(root.Dir, actual)
	if err != nil || root.secret(filepath.ToSlash(rel)) {
		return ErrInvalid
	}
	if m.Protected != nil && m.Protected(actual) {
		return ErrProtected
	}
	base := path.Base(name)
	existing, statErr := parent.Lstat(base)
	switch {
	case statErr == nil && !replace:
		return ErrExists
	case statErr == nil && !existing.Mode().IsRegular():
		return ErrInvalid
	}

	raw := make([]byte, 16)
	if _, err = rand.Read(raw); err != nil {
		return err
	}
	temp := ".titanos-write-" + hex.EncodeToString(raw) + partSuffix
	f, err := parent.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return mapError(err)
	}
	defer parent.Remove(temp)
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return mapError(err)
	}
	m.own(parent, temp, uid, false)
	if err = m.syncFile(f); err != nil {
		return mapError(err)
	}
	if err = f.Close(); err != nil {
		return mapError(err)
	}
	if statErr != nil {
		return mapError(publishNewFile(parent, temp, base))
	}
	// The old file stays beside the new one, as with the text editor.
	parent.Remove(base + ".prev")
	if err := parent.Link(base, base+".prev"); err != nil {
		// FAT and exFAT know no hard links: move the old file aside instead.
		if err := parent.Rename(base, base+".prev"); err != nil {
			return mapError(err)
		}
	}
	return mapError(parent.Rename(temp, base))
}
