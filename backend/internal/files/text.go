package files

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path"
	"strings"
	"unicode/utf8"
)

// maxText is the largest file SaveText takes.
const maxText = 512 << 10

// SaveText replaces the content of a text file that exists, in a root that
// allows it. The old content is kept next to it as <name>.prev, once. Writing
// goes through a temporary file, so a failed save leaves the file as it was.
// A .json file must be valid JSON: a broken one can stop the NAS from starting.
func (m *Manager) SaveText(root Root, name, content string) error {
	if !root.Editable {
		return ErrReadOnly
	}
	name, err := clean(name)
	if err != nil || name == "." || strings.HasSuffix(name, ".prev") {
		return ErrInvalid
	}
	if root.secret(name) {
		return ErrNotFound
	}
	if len(content) > maxText || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return ErrInvalid
	}
	if strings.HasSuffix(name, ".json") && !json.Valid([]byte(content)) {
		return ErrInvalid
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return mapError(err)
	}
	defer r.Close()
	info, err := r.Lstat(name)
	if err != nil {
		return mapError(err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxText {
		return ErrInvalid
	}
	old, err := r.ReadFile(name)
	if err != nil {
		return mapError(err)
	}
	if err := r.WriteFile(name+".prev", old, 0o600); err != nil {
		return mapError(err)
	}
	raw := make([]byte, 6)
	rand.Read(raw)
	temp := path.Join(path.Dir(name), "."+path.Base(name)+"."+hex.EncodeToString(raw)+partSuffix)
	f, err := r.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return mapError(err)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		r.Remove(temp)
		return mapError(err)
	}
	if err := m.syncFile(f); err != nil {
		f.Close()
		r.Remove(temp)
		return mapError(err)
	}
	f.Close()
	if err := r.Rename(temp, name); err != nil {
		r.Remove(temp)
		return mapError(err)
	}
	return nil
}
