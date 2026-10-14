package plugins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var releaseVersion = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:-[a-z0-9.]+)?$`)

// Each release has a complete immutable-on-publication set. Choosing a set
// depends on the running OS version, not on the next boot slot. Old TitanOS
// versions still see the original files at the root of the plugin directory.
func (m *Manager) baseDir(volume string) string {
	return filepath.Join(m.mountRoot, volume, ".titanos", "plugins")
}

func (m *Manager) releaseDir(volume, version string) string {
	return filepath.Join(m.baseDir(volume), "releases", version)
}

// PrepareUpdate is called before the updater writes a bootable target slot.
// fetch and download use the updater's bounded, resumable HTTP implementation.
func (m *Manager) PrepareUpdate(ctx context.Context, version, kernel, base string, assets map[string]string, mounted []string,
	fetch func(context.Context, string, int64) ([]byte, error),
	download func(context.Context, string, string, string, int64) error) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.Disabled {
		return ErrDisabled
	}
	if !releaseVersion.MatchString(version) || version == m.Release {
		return ErrInvalid
	}
	settings := m.current()
	if settings.Volume == "" {
		return m.store.WriteJSON(releaseSettingsPath(version), settings)
	}
	if !slices.Contains(mounted, settings.Volume) {
		return ErrUnknownVolume
	}
	installed := m.installed(settings.Volume)
	for _, name := range settings.Enabled {
		if _, ok := installed[name]; !ok {
			return fmt.Errorf("%w: enabled plugin %s is missing or invalid", ErrInvalid, name)
		}
	}
	if len(installed) == 0 {
		return m.store.WriteJSON(releaseSettingsPath(version), settings)
	}
	parent := filepath.Join(m.baseDir(settings.Volume), "releases")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".prepare-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for name, old := range installed {
		oldPath := filepath.Join(m.dir(settings.Volume), name+manifestSuffix)
		manifest := old
		if asset, available := assets[name]; available && m.official(oldPath) {
			data, err := fetch(ctx, base+asset, 64<<10)
			if err != nil {
				return fmt.Errorf("plugin %s: %w", name, err)
			}
			sig, err := fetch(ctx, base+asset+".minisig", 4096)
			if err != nil {
				return err
			}
			path := filepath.Join(stage, name+manifestSuffix)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				return err
			}
			if err := os.WriteFile(path+".minisig", sig, 0o600); err != nil {
				return err
			}
			manifest, err = readManifest(path)
			if err != nil || !m.official(path) {
				return fmt.Errorf("%w: plugin %s signature or metadata", ErrInvalid, name)
			}
			if manifest.Name != name || (manifest.Version != version && !strings.HasSuffix(manifest.Version, "-titanos-"+version)) {
				return fmt.Errorf("%w: plugin %s targets another release", ErrInvalid, name)
			}
			if manifest.Kernel != "" && manifest.Kernel != kernel {
				return fmt.Errorf("%w: plugin %s targets another kernel", ErrInvalid, name)
			}
			if err := download(ctx, base+name+".raw", filepath.Join(stage, name+".raw"), manifest.SHA256, manifest.Size); err != nil {
				return err
			}
		} else {
			// Kernel-independent community extensions can be kept unchanged.
			// Never silently drop an enabled driver when no compatible build exists.
			if old.Kernel != "" && old.Kernel != kernel {
				return fmt.Errorf("%w: no trusted build of %s for kernel %s; remove it before updating", ErrInvalid, name, kernel)
			}
			for _, suffix := range []string{".raw", manifestSuffix} {
				if err := copyFile(filepath.Join(m.dir(settings.Volume), name+suffix), filepath.Join(stage, name+suffix), 0o600); err != nil {
					return err
				}
			}
			if m.official(oldPath) {
				if err := copyFile(oldPath+".minisig", filepath.Join(stage, name+manifestSuffix+".minisig"), 0o600); err != nil {
					return err
				}
			}
		}
		if sum, size, err := hashFile(filepath.Join(stage, name+".raw"), maxImageBytes); err != nil || sum != manifest.SHA256 || size != manifest.Size {
			return fmt.Errorf("%w: plugin %s image integrity", ErrInvalid, name)
		}
	}
	// A ready marker is written last. A failed/interrupted preparation can
	// never be selected at boot, even if its directory is visible on disk.
	if err := os.WriteFile(filepath.Join(stage, ".ready"), []byte(version+"\n"), 0o600); err != nil {
		return err
	}
	if err := syncTree(stage); err != nil {
		return err
	}
	dest := m.releaseDir(settings.Volume, version)
	// This is the inactive OS version only. Keep the current and fallback sets.
	if _, err := os.Stat(dest); err == nil {
		entries, err := os.ReadDir(stage)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			a, size, err := hashFile(filepath.Join(stage, entry.Name()), maxImageBytes)
			b, otherSize, otherErr := hashFile(filepath.Join(dest, entry.Name()), maxImageBytes)
			if err != nil || otherErr != nil || a != b || size != otherSize {
				return fmt.Errorf("%w: an existing plugin set for %s differs", ErrExists, version)
			}
		}
		existing, err := os.ReadDir(dest)
		if err != nil || len(existing) != len(entries) {
			return ErrExists
		}
		return m.store.WriteJSON(releaseSettingsPath(version), settings)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(stage, dest); err != nil {
		return err
	}
	if err := syncDirectory(parent); err != nil {
		return err
	}
	return m.store.WriteJSON(releaseSettingsPath(version), settings)
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func syncTree(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := syncDirectory(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return syncDirectory(path)
}

// Healthy checks only enabled plugins. It performs no reads from /config.
func (m *Manager) Healthy() bool {
	m.actions.Lock()
	defer m.actions.Unlock()
	m.mu.Lock()
	loaded := m.loaded
	m.mu.Unlock()
	if !loaded {
		return false
	}
	for _, name := range m.current().Enabled {
		if _, ok := m.active[name]; !ok {
			return false
		}
	}
	return true
}

func releaseSettingsPath(version string) string { return "plugins/releases/" + version + ".json" }
func (m *Manager) settingsPath() string {
	if releaseVersion.MatchString(m.Release) {
		return releaseSettingsPath(m.Release)
	}
	return settingsFile
}

// The first mutation in a new-style installation forks legacy plugin files.
// This keeps the original images usable by an older fallback OS as well.
// Caller holds actions and has checked that the volume is mounted.
func (m *Manager) ensureReleaseSet(volume string) error {
	if !releaseVersion.MatchString(m.Release) || m.dir(volume) != m.baseDir(volume) {
		return nil
	}
	parent := filepath.Join(m.baseDir(volume), "releases")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".initial-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for name := range m.installed(volume) {
		for _, suffix := range []string{".raw", manifestSuffix, manifestSuffix + ".minisig"} {
			source := filepath.Join(m.baseDir(volume), name+suffix)
			if _, err := os.Stat(source); os.IsNotExist(err) && strings.HasSuffix(suffix, ".minisig") {
				continue
			}
			if err := copyFile(source, filepath.Join(stage, name+suffix), 0o600); err != nil {
				return err
			}
		}
	}
	if err := os.WriteFile(filepath.Join(stage, ".ready"), []byte(m.Release+"\n"), 0o600); err != nil {
		return err
	}
	if err := syncTree(stage); err != nil {
		return err
	}
	if err := os.Rename(stage, m.releaseDir(volume, m.Release)); err != nil {
		return err
	}
	return syncDirectory(parent)
}
