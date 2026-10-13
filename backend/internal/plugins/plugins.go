// Package plugins manages system extensions: drivers and tools that are
// not part of the base image.
//
// A plugin is a systemd-sysext image (a squashfs that is laid over /usr)
// with a manifest next to it. Plugins live on a volume, in
// /mnt/<volume>/.titanos/plugins, and are laid over the system once that
// volume is mounted. Which ones are switched on is kept in /config.
//
// A plugin becomes part of the system and can do whatever the system can.
// One whose manifest carries a signature (minisign) of a key that ships
// with the image counts as official; every other one is unchecked and has
// to be confirmed by name. The manifest names the hash of the image, so
// the signature covers both.
package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/minisign"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

const (
	settingsFile   = "plugins/settings.json"
	manifestSuffix = ".plugin.json"
	// maxImageBytes bounds a plugin; drivers with their libraries are large.
	maxImageBytes = 4 << 30
)

var (
	ErrDisabled      = errors.New("plugins: not available in development mode")
	ErrNotLoaded     = errors.New("plugins: settings could not be read from /config")
	ErrUnknownVolume = errors.New("plugins: the volume is not available")
	ErrNotFound      = errors.New("plugins: not found")
	ErrExists        = errors.New("plugins: this plugin is installed already")
	// ErrInvalid means the manifest or the image is not what it should be.
	ErrInvalid      = errors.New("plugins: not a valid plugin")
	ErrNotConfirmed = errors.New("plugins: the installation was not confirmed")
)

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// Manifest describes a plugin. It lies next to the image as <name>.plugin.json.
type Manifest struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	// Description maps a language to a text.
	Description map[string]string `json:"description"`
	Version     string            `json:"version"`
	// SHA256 and Size are those of the image <name>.raw.
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	// Kernel is the kernel release the plugin's modules were built for.
	// A plugin with modules for another kernel is not loaded.
	Kernel string `json:"kernel"`
	// Contains says in words what the plugin brings, per language.
	Contains map[string][]string `json:"contains"`
	// Modules are loaded and Services started once the plugin is in place.
	Modules  []string `json:"modules"`
	Services []string `json:"services"`
}

// Settings is /config/plugins/settings.json.
type Settings struct {
	// Volume holds the installed plugins.
	Volume string `json:"volume"`
	// Enabled are the plugins laid over the system.
	Enabled []string `json:"enabled"`
}

// Plugin is a plugin as shown in the UI.
type Plugin struct {
	Manifest
	// Installed plugins lie in the plugin folder of the volume; the
	// others were found somewhere on a volume and can be installed.
	Installed bool `json:"installed"`
	Enabled   bool `json:"enabled"`
	// Active means the plugin is part of the running system right now.
	Active bool `json:"active"`
	// Path is where the manifest of a plugin that is not installed lies.
	Path string `json:"path"`
	// Official means the manifest is signed with a key TitanOS trusts.
	Official bool `json:"official"`
	// Problem says why an enabled plugin is not active: kernel.
	Problem string `json:"problem"`
}

// Status is everything the plugin page shows.
type Status struct {
	Volume string `json:"volume"`
	// Kernel is the release of the running kernel.
	Kernel  string   `json:"kernel"`
	Plugins []Plugin `json:"plugins"`
}

// Manager installs plugins and keeps the system in line with the settings.
type Manager struct {
	store  *config.Store
	runner run.Runner
	// Disabled leaves the system alone. Set in development mode.
	Disabled  bool
	mountRoot string
	// extDir is where systemd looks for extensions that are not part of
	// the image: links to the plugins on the volume.
	extDir string
	kernel string
	// keyDir holds the public keys plugins may be signed with.
	keyDir string

	mu       sync.Mutex
	loaded   bool
	settings Settings
	// applied is what was last handed to systemd, to notice changes.
	applied string
}

// NewManager returns a manager working on the real system.
func NewManager(store *config.Store) *Manager {
	release, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	return &Manager{store: store, runner: run.Exec{}, mountRoot: "/mnt", extDir: "/run/extensions", keyDir: "/usr/share/titanos/plugin-keys", kernel: strings.TrimSpace(string(release)), applied: "-"}
}

// Load reads the settings from /config.
func (m *Manager) Load() error {
	var settings Settings
	err := m.store.ReadJSON(settingsFile, &settings)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings, m.loaded = settings, true
	return nil
}

func (m *Manager) current() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Settings{Volume: m.settings.Volume, Enabled: slices.Clone(m.settings.Enabled)}
}

// Volume returns the volume the plugins are installed on, or "" while there are none.
func (m *Manager) Volume() string {
	settings := m.current()
	if len(settings.Enabled) == 0 && len(m.installed(settings.Volume)) == 0 {
		return ""
	}
	return settings.Volume
}

func (m *Manager) save(settings Settings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	sort.Strings(settings.Enabled)
	if err := m.store.WriteJSON(settingsFile, settings); err != nil {
		return err
	}
	m.settings = settings
	return nil
}

func (m *Manager) dir(volume string) string {
	return filepath.Join(m.mountRoot, volume, ".titanos", "plugins")
}

// readManifest reads and checks a manifest file.
func readManifest(path string) (Manifest, error) {
	var manifest Manifest
	data, err := os.ReadFile(path)
	if err != nil {
		return manifest, err
	}
	if len(data) > 64*1024 || json.Unmarshal(data, &manifest) != nil {
		return manifest, fmt.Errorf("%w: the manifest is not readable", ErrInvalid)
	}
	if !nameRE.MatchString(manifest.Name) || filepath.Base(path) != manifest.Name+manifestSuffix {
		return manifest, fmt.Errorf("%w: the name of the plugin and of its files do not match", ErrInvalid)
	}
	if len(manifest.SHA256) != 64 || manifest.Size <= 0 || manifest.Size > maxImageBytes {
		return manifest, fmt.Errorf("%w: the manifest does not describe its image", ErrInvalid)
	}
	for _, unit := range append(slices.Clone(manifest.Modules), manifest.Services...) {
		// These end up as arguments of modprobe and systemctl.
		if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,63}$`).MatchString(unit) {
			return manifest, fmt.Errorf("%w: odd name of a module or service", ErrInvalid)
		}
	}
	if manifest.Title == "" {
		manifest.Title = manifest.Name
	}
	return manifest, nil
}

// official reports whether the manifest at path carries a valid signature
// of a trusted key in <path>.minisig.
func (m *Manager) official(path string) bool {
	keys := minisign.LoadKeys(m.keyDir)
	if len(keys) == 0 {
		return false
	}
	manifest, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	signature, err := os.ReadFile(path + ".minisig")
	return err == nil && minisign.Verify(keys, manifest, string(signature)) == nil
}

// installed returns the plugins in the plugin folder of the volume.
func (m *Manager) installed(volume string) map[string]Manifest {
	found := map[string]Manifest{}
	entries, _ := os.ReadDir(m.dir(volume))
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), manifestSuffix) {
			continue
		}
		manifest, err := readManifest(filepath.Join(m.dir(volume), entry.Name()))
		if err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(m.dir(volume), manifest.Name+".raw")); err == nil {
			found[manifest.Name] = manifest
		}
	}
	return found
}

// Status lists the installed plugins and those found on the volumes.
func (m *Manager) Status(mounted []string) Status {
	settings := m.current()
	status := Status{Volume: settings.Volume, Kernel: m.kernel, Plugins: []Plugin{}}
	names := map[string]bool{}
	if slices.Contains(mounted, settings.Volume) {
		for name, manifest := range m.installed(settings.Volume) {
			plugin := Plugin{Manifest: manifest, Installed: true, Enabled: slices.Contains(settings.Enabled, name)}
			plugin.Official = m.official(filepath.Join(m.dir(settings.Volume), name+manifestSuffix))
			if _, err := os.Lstat(filepath.Join(m.extDir, name+".raw")); err == nil {
				plugin.Active = true
			}
			if plugin.Enabled && !m.compatible(manifest) {
				plugin.Problem = "kernel"
			}
			names[name] = true
			status.Plugins = append(status.Plugins, plugin)
		}
	}
	// Plugins someone put into a share or onto a volume, ready to install.
	visited := 0
	for _, volume := range mounted {
		root := filepath.Join(m.mountRoot, volume)
		filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			visited++
			if err != nil || visited > 50000 {
				return fs.SkipDir
			}
			if entry.IsDir() {
				depth := strings.Count(strings.TrimPrefix(path, root), "/")
				if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "appdata" || entry.Name() == "vms" || depth > 4) {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(entry.Name(), manifestSuffix) {
				return nil
			}
			if manifest, err := readManifest(path); err == nil && !names[manifest.Name] {
				names[manifest.Name] = true
				status.Plugins = append(status.Plugins, Plugin{Manifest: manifest, Path: path, Official: m.official(path)})
			}
			return nil
		})
	}
	sort.Slice(status.Plugins, func(i, j int) bool { return status.Plugins[i].Name < status.Plugins[j].Name })
	return status
}

// compatible reports whether a plugin fits the running kernel.
func (m *Manager) compatible(manifest Manifest) bool {
	return manifest.Kernel == "" || manifest.Kernel == m.kernel
}

func hashFile(path string, limit int64) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(file, limit+1))
	return hex.EncodeToString(hash.Sum(nil)), n, err
}

// Install copies a plugin found on a volume into the plugin folder and
// switches it on. confirm has to be the name of the plugin: nothing has
// checked what is in it.
func (m *Manager) Install(ctx context.Context, manifestPath, volume, confirm string, mounted []string) error {
	if m.Disabled {
		return ErrDisabled
	}
	real, err := filepath.EvalSymlinks(manifestPath)
	if err != nil || !strings.HasPrefix(real, m.mountRoot+"/") {
		return ErrNotFound
	}
	manifest, err := readManifest(real)
	if err != nil {
		return err
	}
	// Nothing vouches for an unsigned plugin; whoever installs it says so by name.
	signed := m.official(real)
	if !signed && confirm != manifest.Name {
		return ErrNotConfirmed
	}
	settings := m.current()
	if settings.Volume == "" {
		settings.Volume = volume
	}
	if !slices.Contains(mounted, settings.Volume) {
		return ErrUnknownVolume
	}
	if _, exists := m.installed(settings.Volume)[manifest.Name]; exists {
		return ErrExists
	}
	image := filepath.Join(filepath.Dir(real), manifest.Name+".raw")
	if sum, size, err := hashFile(image, maxImageBytes); err != nil || size != manifest.Size || sum != strings.ToLower(manifest.SHA256) {
		return fmt.Errorf("%w: the image is not the one the manifest describes", ErrInvalid)
	}

	dir := m.dir(settings.Volume)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// The image first, under another name until it is complete; a plugin
	// counts as installed once its manifest is there.
	target := filepath.Join(dir, manifest.Name+".raw")
	if err := copyFile(image, target+".part", 0o600); err != nil {
		os.Remove(target + ".part")
		return err
	}
	// What was copied is what was checked: the source may have changed in between.
	if sum, _, err := hashFile(target+".part", maxImageBytes); err != nil || sum != strings.ToLower(manifest.SHA256) {
		os.Remove(target + ".part")
		return fmt.Errorf("%w: the image changed while it was copied", ErrInvalid)
	}
	if err := os.Rename(target+".part", target); err != nil {
		return err
	}
	if signed {
		if err := copyFile(real+".minisig", filepath.Join(dir, manifest.Name+manifestSuffix+".minisig"), 0o600); err != nil {
			return err
		}
	}
	if err := copyFile(real, filepath.Join(dir, manifest.Name+manifestSuffix), 0o600); err != nil {
		return err
	}
	if !slices.Contains(settings.Enabled, manifest.Name) {
		settings.Enabled = append(settings.Enabled, manifest.Name)
	}
	if err := m.save(settings); err != nil {
		return err
	}
	return m.Apply(ctx, mounted)
}

func copyFile(from, to string, mode os.FileMode) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		target.Close()
		return err
	}
	if err := target.Sync(); err != nil {
		target.Close()
		return err
	}
	return target.Close()
}

// SetEnabled switches an installed plugin on or off.
func (m *Manager) SetEnabled(ctx context.Context, name string, enabled bool, mounted []string) error {
	if m.Disabled {
		return ErrDisabled
	}
	settings := m.current()
	if !slices.Contains(mounted, settings.Volume) {
		return ErrUnknownVolume
	}
	if _, exists := m.installed(settings.Volume)[name]; !exists {
		return ErrNotFound
	}
	settings.Enabled = slices.DeleteFunc(settings.Enabled, func(other string) bool { return other == name })
	if enabled {
		settings.Enabled = append(settings.Enabled, name)
	}
	if err := m.save(settings); err != nil {
		return err
	}
	return m.Apply(ctx, mounted)
}

// Remove switches a plugin off and deletes it from the volume.
func (m *Manager) Remove(ctx context.Context, name string, mounted []string) error {
	if err := m.SetEnabled(ctx, name, false, mounted); err != nil {
		return err
	}
	dir := m.dir(m.current().Volume)
	os.Remove(filepath.Join(dir, name+manifestSuffix))
	os.Remove(filepath.Join(dir, name+manifestSuffix+".minisig"))
	return os.Remove(filepath.Join(dir, name+".raw"))
}

// Run lays the enabled plugins over the system once their volume is
// mounted and takes them away before it goes, until ctx is done.
func (m *Manager) Run(ctx context.Context, mounted func() []string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		loaded := m.loaded
		m.mu.Unlock()
		if !loaded {
			m.Load()
		} else if !m.Disabled {
			if err := m.Apply(ctx, mounted()); err != nil {
				slog.Error("plugins", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Apply makes the system carry exactly the plugins that are switched on,
// installed and fit the kernel.
func (m *Manager) Apply(ctx context.Context, mounted []string) error {
	settings := m.current()
	wanted := map[string]Manifest{}
	if slices.Contains(mounted, settings.Volume) {
		for name, manifest := range m.installed(settings.Volume) {
			if slices.Contains(settings.Enabled, name) && m.compatible(manifest) {
				wanted[name] = manifest
			}
		}
	}
	names := make([]string, 0, len(wanted))
	for name := range wanted {
		names = append(names, name)
	}
	sort.Strings(names)
	key := settings.Volume + ":" + strings.Join(names, ",")
	m.mu.Lock()
	unchanged := key == m.applied
	m.mu.Unlock()
	if unchanged {
		return nil
	}

	if err := os.MkdirAll(m.extDir, 0o755); err != nil {
		return err
	}
	links, _ := os.ReadDir(m.extDir)
	var added []string
	all := map[string]Manifest{}
	if slices.Contains(mounted, settings.Volume) {
		all = m.installed(settings.Volume)
	}
	for _, link := range links {
		name := strings.TrimSuffix(link.Name(), ".raw")
		if wanted[name].Name != "" {
			continue
		}
		// What the plugin started ends before its files go.
		for _, service := range all[name].Services {
			if _, err := m.runner.Run(ctx, "", "systemctl", "stop", service); err != nil {
				slog.Warn("plugin service not stopped", "plugin", name, "service", service, "err", err)
			}
		}
		os.Remove(filepath.Join(m.extDir, link.Name()))
	}
	for _, name := range names {
		link := filepath.Join(m.extDir, name+".raw")
		if _, err := os.Lstat(link); err == nil {
			continue
		}
		if err := os.Symlink(filepath.Join(m.dir(settings.Volume), name+".raw"), link); err != nil {
			return err
		}
		added = append(added, name)
	}
	// One call puts all extensions in place and takes away what is gone.
	if _, err := m.runner.Run(ctx, "", "systemd-sysext", "refresh"); err != nil {
		return err
	}
	m.mu.Lock()
	m.applied = key
	m.mu.Unlock()
	slog.Info("plugins in place", "plugins", names)

	for _, name := range added {
		manifest := wanted[name]
		if len(manifest.Modules) > 0 {
			// The modules are new to the system; tell it where they are.
			if _, err := m.runner.Run(ctx, "", "depmod", "-a"); err != nil {
				slog.Warn("plugin modules not indexed", "plugin", name, "err", err)
			}
		}
		for _, module := range manifest.Modules {
			if _, err := m.runner.Run(ctx, "", "modprobe", module); err != nil {
				slog.Warn("plugin module not loaded", "plugin", name, "module", module, "err", err)
			}
		}
		if len(manifest.Services) > 0 {
			m.runner.Run(ctx, "", "systemctl", "daemon-reload")
		}
		for _, service := range manifest.Services {
			if _, err := m.runner.Run(ctx, "", "systemctl", "start", service); err != nil {
				slog.Warn("plugin service not started", "plugin", name, "service", service, "err", err)
			}
		}
	}
	return nil
}
