package files

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// CreateText publishes a new small editable file without replacing any existing entry.
func (m *Manager) CreateText(root Root, name string, uid int) error {
	if !root.Writable || root.Kind == "stick" {
		return ErrReadOnly
	}
	name, err := clean(name)
	if err != nil || name == "." || !validName(path.Base(name)) || root.secret(name) {
		return ErrInvalid
	}
	content := ""
	switch strings.ToLower(path.Ext(name)) {
	case ".txt", ".md":
	case ".json":
		content = "{}\n"
	default:
		return ErrNotText
	}
	if m.protected(root, name) {
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
	// Pin the destination directory and verify its resolved location before writing.
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
	name = path.Base(name)
	r = parent

	raw := make([]byte, 16)
	if _, err = rand.Read(raw); err != nil {
		return err
	}
	temp := path.Join(path.Dir(name), ".titanos-create-"+hex.EncodeToString(raw)+partSuffix)
	f, err := r.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return mapError(err)
	}
	defer r.Remove(temp)
	defer f.Close()
	if _, err = f.WriteString(content); err != nil {
		return mapError(err)
	}
	m.own(r, temp, uid, false)
	if err = m.syncFile(f); err != nil {
		return mapError(err)
	}
	if err = f.Close(); err != nil {
		return mapError(err)
	}
	if m.Protected != nil && m.Protected(actual) {
		return ErrProtected
	}
	return mapError(publishNewFile(r, temp, name))
}
