package shares

import (
	"errors"
	"io/fs"
	"path/filepath"
)

// GuardedDirectory reports whether writes to directory would land in a locked
// share. Callers may hold a volume root or an open directory reached via an alias.
func (m *Manager) GuardedDirectory(directory string) (bool, error) {
	m.mu.Lock()
	var locked []string
	for _, sh := range m.shares {
		if sh.GuardLocked != nil {
			locked = append(locked, m.Path(sh))
		}
	}
	loaded := m.loaded
	m.mu.Unlock()
	if !loaded {
		return false, ErrNotLoaded
	}
	if len(locked) == 0 {
		return false, nil
	}
	within := func(base, target string) bool {
		rel, err := filepath.Rel(base, target)
		return err == nil && filepath.IsLocal(rel)
	}
	target, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return false, err
	}
	for _, base := range locked {
		if within(base, directory) || within(base, target) {
			return true, nil
		}
		actual, err := filepath.EvalSymlinks(base)
		if errors.Is(err, fs.ErrNotExist) {
			continue // a guarded share on a disconnected volume
		}
		if err != nil {
			return false, err
		}
		if within(actual, target) {
			return true, nil
		}
	}
	return false, nil
}
