package plugins

import (
	"fmt"
	"slices"
)

// Plugins that must never be part of the system at the same time, for
// example two NVIDIA drivers that both bring a module called nvidia. A signed
// manifest may name them in the optional field "conflicts"; older TitanOS
// ignore it. The NVIDIA pair is known here as well, so that a manifest
// without the field (a local or older build) is held back all the same.
var knownConflicts = [][2]string{{"nvidia", "nvidia-legacy"}}

// conflicting reports whether a and b exclude each other. Either side
// naming the other is enough.
func conflicting(a, b Manifest) bool {
	if a.Name == b.Name {
		return false
	}
	if slices.Contains(a.Conflicts, b.Name) || slices.Contains(b.Conflicts, a.Name) {
		return true
	}
	for _, pair := range knownConflicts {
		if pair[0] == a.Name && pair[1] == b.Name || pair[0] == b.Name && pair[1] == a.Name {
			return true
		}
	}
	return false
}

// enabledConflict names an enabled, installed plugin that excludes manifest,
// or "" if there is none. Caller holds actions.
func (m *Manager) enabledConflict(manifest Manifest) string {
	settings := m.current()
	installed := m.installed(settings.Volume)
	for _, name := range settings.Enabled {
		if other, ok := installed[name]; ok && conflicting(manifest, other) {
			return name
		}
	}
	return ""
}

func conflictError(name, other string) error {
	return fmt.Errorf("%w: %s cannot run together with %s; switch %s off first", ErrConflict, name, other, other)
}

// withoutConflicts drops from wanted every plugin that excludes one enabled
// before it (order of settings.Enabled): settings written by a version
// without this check, or edited by hand, never lay both over the system.
func (m *Manager) withoutConflicts(enabled []string, wanted map[string]Manifest) {
	var kept []Manifest
	for _, name := range enabled {
		manifest, ok := wanted[name]
		if !ok {
			continue
		}
		clash := false
		for _, other := range kept {
			if conflicting(manifest, other) {
				clash = true
				break
			}
		}
		if clash {
			delete(wanted, name)
			m.problems[name] = "conflict"
			continue
		}
		kept = append(kept, manifest)
	}
}
