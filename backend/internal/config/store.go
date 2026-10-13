// Package config reads and writes the persistent configuration in /config,
// which lives on the USB stick.
//
// The stick is only mounted while it is being used (see /etc/fstab in the
// image). Nothing in here keeps a file or directory open, and callers must
// not touch the store on a timer: every access wakes the stick up.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Store is a directory holding the configuration. All names are slash
// separated paths relative to that directory.
type Store struct {
	dir string
}

// Open returns the store in dir. It does not access the directory.
func Open(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) path(name string) (string, error) {
	if !fs.ValidPath(name) || name == "." {
		return "", fmt.Errorf("config: invalid name %q", name)
	}
	return filepath.Join(s.dir, filepath.FromSlash(name)), nil
}

// Read returns the content of a file. A missing file is fs.ErrNotExist.
func (s *Store) Read(name string) ([]byte, error) {
	p, err := s.path(name)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// ReadJSON decodes a JSON file into v.
func (s *Store) ReadJSON(name string, v any) error {
	data, err := s.Read(name)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("config: %s: %w", name, err)
	}
	return nil
}

// Write replaces a file atomically: the content goes to a temporary file in
// the same directory, is flushed to the stick and then renamed over the old
// file. A power cut leaves either the old or the new content, never half.
func (s *Store) Write(name string, data []byte) error {
	p, err := s.path(name)
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return err
	}
	return syncDir(dir)
}

// WriteJSON encodes v as indented JSON and writes it atomically.
func (s *Store) WriteJSON(name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return s.Write(name, append(data, '\n'))
}

// Remove deletes a file. A missing file is not an error.
func (s *Store) Remove(name string) error {
	p, err := s.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return syncDir(filepath.Dir(p))
}

// RemoveAll deletes a directory with everything in it. A missing directory
// is not an error.
func (s *Store) RemoveAll(name string) error {
	p, err := s.path(name)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(p); err != nil {
		return err
	}
	return syncDir(filepath.Dir(p))
}

// List returns the names of the entries in a directory. A missing directory
// is empty. It fails if the store itself is not there (stick missing).
func (s *Store) List(name string) ([]string, error) {
	if _, err := os.Stat(s.dir); err != nil {
		return nil, err
	}
	p, err := s.path(name)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	// Some filesystems cannot sync a directory; the data itself is already flushed.
	if err := d.Sync(); err != nil && !errors.Is(err, errors.ErrUnsupported) && !isInvalid(err) {
		return err
	}
	return nil
}
