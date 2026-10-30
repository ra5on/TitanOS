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
	"net/http"
	"os"
	"path/filepath"
	"reflect"
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
	ErrInvalid       = errors.New("plugins: not a valid plugin")
	ErrNotConfirmed  = errors.New("plugins: the installation was not confirmed")
	ErrBusy          = errors.New("plugins: a service or kernel module is still in use")
	ErrDownloading   = errors.New("plugins: this plugin is already being downloaded")
	ErrDriverPending = errors.New("plugins: cancel the scheduled driver activation or reboot before changing plugins")
	ErrUpdatePending = errors.New("plugins: finish or cancel the system update before changing plugins")
)

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// unitRE checks module and service names; they end up as arguments of
// modprobe and systemctl.
var unitRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,63}$`)

// Manifest describes a plugin. It lies next to the image as <name>.plugin.json.
type Manifest struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	// Description maps a language to a text.
	Description map[string]string `json:"description"`
	Version     string            `json:"version"`
	TitanOS     string            `json:"titanos,omitempty"`
	LocalBuild  bool              `json:"localBuild,omitempty"`
	Revision    uint64            `json:"revision,omitempty"`
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
	// Catalog information, all optional; older TitanOS ignore it. The
	// signature of the manifest covers it like everything else.
	Catalog
}

// Settings is /config/plugins/settings.json.
type Settings struct {
	// Volume holds the installed plugins.
	Volume string `json:"volume"`
	// Enabled are the plugins laid over the system.
	Enabled          []string          `json:"enabled"`
	AutomaticDrivers *bool             `json:"automaticDrivers,omitempty"`
	DriverSet        string            `json:"driverSet,omitempty"`
	PendingDriver    *DriverActivation `json:"pendingDriver,omitempty"`
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
	Local    bool `json:"local"`
	// Download means this signed plugin is available in the matching GitHub release.
	Download   bool   `json:"download"`
	LicenseURL string `json:"licenseUrl,omitempty"`
	// Problem is kernel, activation, integrity, or busy after refused removal.
	Problem string `json:"problem"`
	// Update is another version of this installed plugin found in a share.
	Update *PluginUpdate `json:"update,omitempty"`
	// Source is the ID of the source a community download comes from; URL
	// the link of a single one (external.go).
	Source string `json:"source,omitempty"`
	URL    string `json:"url,omitempty"`
	// Devices is the present hardware the plugin is meant for.
	Devices []Device `json:"devices,omitempty"`
}

// PluginUpdate describes the version an installed plugin can be replaced with.
type PluginUpdate struct {
	Path     string `json:"path"`
	Version  string `json:"version"`
	Official bool   `json:"official"`
	// Download is a newer version in the official catalog or a source; Path
	// is empty. Source names that source.
	Download bool   `json:"download,omitempty"`
	Source   string `json:"source,omitempty"`
}

// Status is everything the plugin page shows.
type Status struct {
	Volume string `json:"volume"`
	// Kernel is the release of the running kernel.
	Kernel       string             `json:"kernel"`
	Plugins      []Plugin           `json:"plugins"`
	Catalog      CatalogStatus      `json:"catalog"`
	DriverUpdate DriverUpdateStatus `json:"driverUpdate"`
	// Recommendations are plugins for present hardware that nothing handles yet.
	Recommendations []Recommendation `json:"recommendations"`
	// Dismissed counts recommendations the owner asked not to see again.
	Dismissed int `json:"dismissed"`
	// Sources of community plugins with what the last refresh found.
	Sources []SourceState `json:"sources"`
}

// Manager installs plugins and keeps the system in line with the settings.
type Manager struct {
	CleanupProtected func() ([]string, error)        // current boot slots/app fallbacks/pending updates
	PrepareRuntime   func(name, volume string) error // optional plugin-specific volatile configuration
	ActivateRuntime  func(name string) error         // reapplies saved configuration even if the service was already running
	ChangeAllowed    func() bool                     // checked under actions, serializes with update staging
	UpdateAllowed    func() bool                     // plugin-specific pending trials, independent of updater phase
	store            *config.Store
	runner           run.Runner
	// Disabled leaves the system alone. Set in development mode.
	LocalBuilds bool // build updated open modules on the NAS, only on request
	buildKey    []byte
	Release     string // running TitanOS version; set before Load
	Disabled    bool
	mountRoot   string
	// extDir is where systemd looks for extensions that are not part of
	// the image: links to the plugins on the volume.
	extDir string
	kernel string
	// keyDir holds the public keys plugins may be signed with.
	keyDir       string
	moduleRoot   string
	moduleSource string
	actions      sync.Mutex
	active       map[string]Manifest
	resident     map[string]Manifest
	problems     map[string]string

	mu       sync.Mutex
	loaded   bool
	settings Settings
	// The release catalog is kept in memory; polling Status never contacts GitHub.
	client        *http.Client
	releaseSource string
	// pluginSource is the plugin repository (repo.go); empty means the
	// plugins of the TitanOS release only.
	pluginSource string
	// The running kernel package and image, which a plugin set has to match.
	kernelPackage, debian, sysextLevel string
	catalogSerial                      uint64
	serialLoaded                       bool
	catalog                            CatalogStatus
	catalogPlugins                     []Plugin
	catalogAttempt                     time.Time
	catalogLoading                     bool
	// Community plugins from links and sources (external.go). Their client
	// checks every address it connects to; sources are kept in RAM after Load.
	externalClient *http.Client
	// publicClient reaches public addresses only: for what a source list
	// names on a host other than its own.
	publicClient  *http.Client
	guardedOnce   sync.Once
	sources       []Source
	sourcesLoaded bool
	sourceEntries []catalogEntry
	sourceStates  map[string]SourceState
	// Official images being downloaded, without holding actions.
	downloading map[string]bool
	// Steps of installations, updates and removals, RAM only; own lock so
	// the page can follow them while actions is held.
	progress progressLog
	// Plugin files found in shares. Status polling reuses the last search;
	// it runs again when the page asks (ScanShares) or the volumes change.
	shareFiles   []shareFile
	sharesFor    string
	sharesLoaded bool
	// Trusted keys, read again only when their directory changes.
	keys     []minisign.PublicKey
	keysFrom time.Time
	// applied is what was last handed to systemd, to notice changes.
	applied        string
	driverRequests chan string
	driverActions  sync.Mutex
	driverStatus   DriverUpdateStatus
	driverAttempt  time.Time
	driverVolume   string
	bootID         string
	// Hardware recommendations (recommend.go): sysfs root, pci.ids and the
	// image's catalog table; empty paths switch detection off (tests).
	sysRoot      string
	pciIDsPath   string
	catalogTable string
	hwMu         sync.Mutex
	hw           hardwareState
	// Notify is told the title of a plugin newly recommended for hardware.
	Notify func(title string)
}

// RuntimeAction keeps configuration changes from racing sysext removal or an
// OS update. A pending access trial is resolved directly by its manager.
func (m *Manager) RuntimeAction(name string, action func() error) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.Disabled {
		return ErrDisabled
	}
	if m.ChangeAllowed != nil && !m.ChangeAllowed() {
		return ErrUpdatePending
	}
	if m.current().PendingDriver != nil {
		return ErrDriverPending
	}
	if _, active := m.active[name]; !active {
		return ErrNotFound
	}
	return action()
}

// NewManager returns a manager working on the real system.
func NewManager(store *config.Store) *Manager {
	release, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	boot, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	version, _ := os.ReadFile("/proc/version")
	osRelease, _ := os.ReadFile("/usr/lib/os-release")
	debian, sysextLevel := osReleaseValues(string(osRelease))
	return &Manager{pluginSource: "https://github.com/ra5on/TitanOS-Plugins", kernelPackage: kernelPackageOf(string(version)), debian: debian, sysextLevel: sysextLevel, driverRequests: make(chan string, 1), bootID: strings.TrimSpace(string(boot)), store: store, runner: run.Exec{}, mountRoot: "/mnt", extDir: "/run/extensions", keyDir: "/usr/share/titanos/plugin-keys", kernel: strings.TrimSpace(string(release)), moduleRoot: "/run/titanos-plugin-modules", moduleSource: "/usr/lib/modules", applied: "-", active: map[string]Manifest{}, resident: map[string]Manifest{}, problems: map[string]string{}, client: catalogClient(), releaseSource: "https://github.com/ra5on/TitanOS",
		sysRoot: "/sys", pciIDsPath: "/usr/share/misc/pci.ids", catalogTable: catalogTableDir}
}

// Load reads the settings from /config.
func (m *Manager) Load() error {
	if err := m.loadBuildKey(); err != nil {
		return err
	}
	var settings Settings
	path := m.settingsPath()
	err := m.store.ReadJSON(path, &settings)
	if errors.Is(err, fs.ErrNotExist) && path != settingsFile {
		if err = m.store.ReadJSON(settingsFile, &settings); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err = m.store.WriteJSON(path, settings); err != nil {
			return err
		}
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := m.loadDriverSelection(&settings); err != nil {
		return err
	}
	// Recommendations are a hint; an unreadable list must not stop plugins.
	if err := m.loadRecommendations(); err != nil {
		slog.Warn("plugin recommendations not readable from /config", "err", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings, m.loaded = settings, true
	return m.loadSources()
}

func (m *Manager) current() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	settings := m.settings
	settings.Enabled = slices.Clone(settings.Enabled)
	return settings
}

// Volume returns the volume the plugins are installed on, or "" while there are none.
func (m *Manager) Volume() string {
	m.mu.Lock()
	downloading := m.driverVolume
	m.mu.Unlock()
	if downloading != "" {
		return downloading
	}
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
	if err := m.store.WriteJSON(m.settingsPath(), settings); err != nil {
		return err
	}
	m.settings = settings
	return nil
}

func (m *Manager) dir(volume string) string {
	if releaseVersion.MatchString(m.Release) {
		dir := m.releaseDir(volume, m.Release)
		if id := m.current().DriverSet; id != "" {
			return filepath.Join(dir, ".sets", id)
		}
		if _, err := os.Stat(filepath.Join(dir, ".ready")); err == nil {
			return dir
		}
	}
	return m.baseDir(volume)
}

// readManifest reads and checks a manifest file.
func readManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	return parseManifest(data, filepath.Base(path))
}

func parseManifest(data []byte, filename string) (Manifest, error) {
	var manifest Manifest
	if len(data) > 64*1024 || json.Unmarshal(data, &manifest) != nil {
		return manifest, fmt.Errorf("%w: the manifest is not readable", ErrInvalid)
	}
	if !nameRE.MatchString(manifest.Name) || filename != manifest.Name+manifestSuffix {
		return manifest, fmt.Errorf("%w: the name of the plugin and of its files do not match", ErrInvalid)
	}
	if len(manifest.SHA256) != 64 || manifest.Size <= 0 || manifest.Size > maxImageBytes {
		return manifest, fmt.Errorf("%w: the manifest does not describe its image", ErrInvalid)
	}
	if _, err := hex.DecodeString(manifest.SHA256); err != nil {
		return manifest, fmt.Errorf("%w: invalid image checksum", ErrInvalid)
	}
	for _, unit := range append(slices.Clone(manifest.Modules), manifest.Services...) {
		if !unitRE.MatchString(unit) {
			return manifest, fmt.Errorf("%w: odd name of a module or service", ErrInvalid)
		}
	}
	if err := manifest.Catalog.validate(); err != nil {
		return manifest, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if manifest.Title == "" {
		manifest.Title = manifest.Name
	}
	return manifest, nil
}

// official reports whether the manifest at path carries a valid signature
// of a trusted key in <path>.minisig.
func (m *Manager) official(path string) bool {
	keys := m.trustedKeys()
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

// trustedKeys returns the keys in keyDir. They are read again only when
// the directory changes, not on every status poll.
func (m *Manager) trustedKeys() []minisign.PublicKey {
	info, err := os.Stat(m.keyDir)
	if err != nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.keys == nil || !info.ModTime().Equal(m.keysFrom) {
		m.keys, m.keysFrom = minisign.LoadKeys(m.keyDir), info.ModTime()
	}
	return m.keys
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
	shares := m.sharePlugins(mounted) // before the lock: a new search may take a while
	m.actions.Lock()
	defer m.actions.Unlock()
	settings := m.current()
	status := Status{Volume: settings.Volume, Kernel: m.kernel, Plugins: []Plugin{}}
	names := map[string]bool{}
	installedAt := map[string]int{}
	if slices.Contains(mounted, settings.Volume) {
		for name, manifest := range m.installed(settings.Volume) {
			plugin := Plugin{Manifest: manifest, Installed: true, Enabled: slices.Contains(settings.Enabled, name)}
			plugin.Official = m.official(filepath.Join(m.dir(settings.Volume), name+manifestSuffix))
			if name == "nvidia" {
				_, err := m.localDriver(m.dir(settings.Volume))
				plugin.Local = err == nil
			}
			_, plugin.Active = m.active[name]
			plugin.Problem = m.problems[name]
			if plugin.Enabled && !m.compatible(manifest) {
				plugin.Problem = "kernel"
			}
			names[name] = true
			installedAt[name] = len(status.Plugins)
			status.Plugins = append(status.Plugins, plugin)
		}
	}
	// Verified release downloads take precedence over unrelated loose files.
	// An identical signed local copy can still be installed without a network.
	catalog, available := m.catalogSnapshot()
	status.Catalog = catalog
	status.DriverUpdate = m.driverSnapshot(settings)
	if status.DriverUpdate.CanRestore {
		bundled, e := readManifest(filepath.Join(m.releaseDir(settings.Volume, m.Release), "nvidia"+manifestSuffix))
		status.DriverUpdate.CanRestore = e == nil && bundled.SHA256 != m.installed(settings.Volume)["nvidia"].SHA256
	}
	downloads := map[string]int{}
	for _, plugin := range available {
		if !names[plugin.Name] {
			names[plugin.Name] = true
			downloads[plugin.Name] = len(status.Plugins)
			status.Plugins = append(status.Plugins, plugin)
		}
	}
	// A newer signed version in the catalog, mostly from the plugin repository.
	for _, plugin := range available {
		if index, ok := installedAt[plugin.Name]; ok {
			installed := &status.Plugins[index]
			if installed.Official && updatable(installed.Manifest, plugin.Manifest) && m.compatible(plugin.Manifest) {
				installed.Update = &PluginUpdate{Version: plugin.Version, Official: true, Download: true}
			}
		}
	}
	// Community plugins from sources: never in place of an official one.
	for _, entry := range m.sourceSnapshot() {
		plugin := entry.plugin
		if index, ok := installedAt[plugin.Name]; ok {
			installed := &status.Plugins[index]
			if installed.Update == nil && !installed.Official && updatable(installed.Manifest, plugin.Manifest) && m.compatible(plugin.Manifest) {
				installed.Update = &PluginUpdate{Version: plugin.Version, Official: plugin.Official, Download: true, Source: plugin.Source}
			}
			continue
		}
		if !names[plugin.Name] {
			names[plugin.Name] = true
			status.Plugins = append(status.Plugins, plugin)
		}
	}
	// Plugins someone put into a share or onto a volume, ready to install.
	for _, file := range shares {
		if index, ok := installedAt[file.manifest.Name]; ok {
			installed := &status.Plugins[index]
			if installed.Update == nil && updatable(installed.Manifest, file.manifest) && file.imageOK && m.compatible(file.manifest) {
				installed.Update = &PluginUpdate{Path: file.path, Version: file.manifest.Version, Official: file.official}
			}
			continue
		}
		if !names[file.manifest.Name] {
			names[file.manifest.Name] = true
			status.Plugins = append(status.Plugins, Plugin{Manifest: file.manifest, Path: file.path, Official: file.official})
		} else if index, ok := downloads[file.manifest.Name]; ok && reflect.DeepEqual(status.Plugins[index].Manifest, file.manifest) && file.official && file.imageOK {
			status.Plugins[index].Download, status.Plugins[index].Path = false, file.path
			delete(downloads, file.manifest.Name)
		}
	}
	sort.Slice(status.Plugins, func(i, j int) bool { return status.Plugins[i].Name < status.Plugins[j].Name })
	m.addHardware(&status)
	status.Sources = m.Sources()
	return status
}

// updatable reports whether found can replace the installed plugin from a
// share. NVIDIA is updated through its driver preparation, with a reboot and a
// way back, never by swapping the image underneath it.
// Only a newer version counts: the old file usually still lies in the share.
func updatable(installed, found Manifest) bool {
	return installed.Name != "nvidia" && !installed.LocalBuild && !found.LocalBuild && found.SHA256 != installed.SHA256 &&
		newerVersion(found.Version, installed.Version)
}

var versionNumbers = regexp.MustCompile(`[0-9]+`)

// newerVersion compares the numbers in two version strings in order
// ("1.10.2" > "1.9"); without numbers, only a different text counts.
func newerVersion(found, installed string) bool {
	a, b := versionNumbers.FindAllString(found, -1), versionNumbers.FindAllString(installed, -1)
	if len(a) == 0 || len(b) == 0 {
		return found != installed
	}
	for i := range max(len(a), len(b)) {
		var x, y string
		if i < len(a) {
			x = strings.TrimLeft(a[i], "0")
		}
		if i < len(b) {
			y = strings.TrimLeft(b[i], "0")
		}
		if len(x) != len(y) {
			return len(x) > len(y)
		}
		if x != y {
			return x > y
		}
	}
	return false
}

// shareFile is a plugin found in a share or on a volume. Installing it checks
// everything again; this is only what the list shows.
type shareFile struct {
	manifest Manifest
	path     string
	official bool
	imageOK  bool // a regular image of the size the manifest names lies next to it
}

// ScanShares looks for plugins on the mounted volumes, outside private and
// app/VM folders, reading at most 50,000 entries. The status poll reuses the
// result instead of reading the disks every few seconds (and waking them);
// the page asks for a new search when it opens and on refresh.
func (m *Manager) ScanShares(mounted []string) {
	var found []shareFile
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
			if manifest, err := readManifest(path); err == nil {
				file := shareFile{manifest: manifest, path: path, official: m.official(path)}
				image, err := os.Stat(filepath.Join(filepath.Dir(path), manifest.Name+".raw"))
				file.imageOK = err == nil && image.Mode().IsRegular() && image.Size() == manifest.Size
				found = append(found, file)
			}
			return nil
		})
	}
	m.mu.Lock()
	m.shareFiles, m.sharesFor, m.sharesLoaded = found, volumesKey(mounted), true
	m.mu.Unlock()
}

// sharePlugins returns the last search, searching again only if there was
// none yet or other volumes are mounted now.
func (m *Manager) sharePlugins(mounted []string) []shareFile {
	m.mu.Lock()
	fresh := m.sharesLoaded && m.sharesFor == volumesKey(mounted)
	m.mu.Unlock()
	if !fresh {
		m.ScanShares(mounted)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.shareFiles
}

func volumesKey(mounted []string) string {
	return strings.Join(slices.Sorted(slices.Values(mounted)), "\x00")
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
	return m.InstallWithActivation(ctx, manifestPath, volume, confirm, mounted, true)
}

// InstallWithActivation can store a verified plugin without touching live
// drivers, services or sysext. Devices already in use remain with their owners.
func (m *Manager) InstallWithActivation(ctx context.Context, manifestPath, volume, confirm string, mounted []string, activate bool) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	tr := m.track("", "install", StepSignature, StepChecksum, StepWrite)
	err := m.install(withTracker(ctx, tr), manifestPath, volume, confirm, mounted, activate)
	tr.finish(err)
	return err
}

// install is shared by local files and verified downloads. Caller holds actions.
func (m *Manager) install(ctx context.Context, manifestPath, volume, confirm string, mounted []string, activate bool) error {
	if m.current().PendingDriver != nil {
		return ErrDriverPending
	}
	if m.ChangeAllowed != nil && !m.ChangeAllowed() {
		return ErrUpdatePending
	}
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
	tr := trackerOf(ctx)
	tr.name(manifest.Name, manifest.Title)
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
	if activate && m.compatible(manifest) {
		tr.expect(activationSteps(manifest)...)
	}
	if signed {
		tr.begin(StepSignature)
	} else {
		tr.skip(StepSignature)
	}
	tr.begin(StepChecksum)
	image := filepath.Join(filepath.Dir(real), manifest.Name+".raw")
	if sum, size, err := hashFile(image, maxImageBytes); err != nil || size != manifest.Size || sum != strings.ToLower(manifest.SHA256) {
		return fmt.Errorf("%w: the image is not the one the manifest describes", ErrInvalid)
	}

	tr.begin(StepWrite)
	if err := m.ensureReleaseSet(settings.Volume); err != nil {
		return err
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
	copiedPath := filepath.Join(dir, manifest.Name+manifestSuffix)
	// Nothing half-copied stays behind: without its manifest an image is
	// not installed, but it would block the next attempt's disk space.
	discard := func() {
		os.Remove(copiedPath)
		os.Remove(copiedPath + ".minisig")
		os.Remove(target)
	}
	if signed {
		if err := copyFile(real+".minisig", copiedPath+".minisig", 0o600); err != nil {
			discard()
			return err
		}
	}
	if err := copyFile(real, copiedPath, 0o600); err != nil {
		discard()
		return err
	}
	// Recheck the copied metadata too: a writable share may have changed
	// after the original signature/image checks.
	copied, err := readManifest(copiedPath)
	if err != nil || !reflect.DeepEqual(copied, manifest) || (signed && !m.official(copiedPath)) {
		discard()
		return fmt.Errorf("%w: plugin metadata changed while being copied", ErrInvalid)
	}
	// The renames above survive a power cut only once the folder is synced.
	if err := syncDirectory(dir); err != nil {
		discard()
		return err
	}
	// A previous interrupted installation may have left an enabled name.
	settings.Enabled = slices.DeleteFunc(settings.Enabled, func(name string) bool { return name == manifest.Name })
	if activate {
		settings.Enabled = append(settings.Enabled, manifest.Name)
	}
	if err := m.save(settings); err != nil {
		return err
	}
	if !activate {
		return nil
	}
	return m.checkActive(ctx, manifest, m.apply(ctx, mounted))
}

// activationSteps are the steps laying a plugin over the running system.
func activationSteps(manifest Manifest) []string {
	steps := []string{StepSysext}
	if len(manifest.Modules) > 0 {
		steps = append(steps, StepModules)
	}
	if len(manifest.Services) > 0 {
		steps = append(steps, StepServices)
	}
	return append(steps, StepHealth)
}

// checkActive is the health step after an activation: the plugin must be part
// of the running system, as the boot healthcheck expects of enabled plugins.
// err is what apply returned; a failure of another plugin is passed on
// without marking a step of this one.
func (m *Manager) checkActive(ctx context.Context, manifest Manifest, err error) error {
	if !m.compatible(manifest) {
		return err // installed for another kernel: nothing to activate
	}
	tr := trackerOf(ctx)
	if _, active := m.active[manifest.Name]; !active {
		if err == nil {
			tr.begin(StepHealth)
			return fmt.Errorf("%s is not active after activation", manifest.Name)
		}
		return err
	}
	tr.begin(StepHealth)
	tr.complete()
	return err
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
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.current().PendingDriver != nil {
		return ErrDriverPending
	}
	if m.ChangeAllowed != nil && !m.ChangeAllowed() {
		return ErrUpdatePending
	}
	return m.setEnabled(ctx, name, enabled, mounted)
}

func (m *Manager) setEnabled(ctx context.Context, name string, enabled bool, mounted []string) error {
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
	if err := m.ensureReleaseSet(settings.Volume); err != nil {
		return err
	}
	if !enabled {
		if err := m.deactivate(ctx, name); err != nil {
			return err
		}
	}
	settings.Enabled = slices.DeleteFunc(settings.Enabled, func(other string) bool { return other == name })
	if enabled {
		settings.Enabled = append(settings.Enabled, name)
	}
	if err := m.save(settings); err != nil {
		m.applied = "-"
		// Disabling stopped the driver before persisting. Restore the previous
		// enabled state if the stick could not store the change.
		return errors.Join(err, m.apply(ctx, mounted))
	}
	m.applied = "-" // an explicit action also retries a failed activation
	return m.apply(ctx, mounted)
}

// Remove switches a plugin off and deletes it from the volume.
func (m *Manager) Remove(ctx context.Context, name string, mounted []string) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.current().PendingDriver != nil {
		return ErrDriverPending
	}
	if m.ChangeAllowed != nil && !m.ChangeAllowed() {
		return ErrUpdatePending
	}
	tr := m.track(name, "remove", StepStop, StepSysext, StepRemove)
	err := m.remove(withTracker(ctx, tr), name, mounted)
	tr.finish(err)
	return err
}

func (m *Manager) remove(ctx context.Context, name string, mounted []string) error {
	tr := trackerOf(ctx)
	if m.Disabled {
		return ErrDisabled
	}
	if !slices.Contains(mounted, m.current().Volume) {
		return ErrUnknownVolume
	}
	manifest, exists := m.installed(m.current().Volume)[name]
	if !exists {
		return ErrNotFound
	}
	tr.name(name, manifest.Title)
	tr.begin(StepStop)
	if err := m.setEnabled(ctx, name, false, mounted); err != nil {
		return err
	}
	tr.begin(StepRemove)
	dir := m.dir(m.current().Volume)
	// The manifest goes first: without it the plugin no longer counts as
	// installed, even if removing the rest fails.
	files := []string{name + manifestSuffix, name + manifestSuffix + ".minisig", name + ".raw"}
	if name == "nvidia" {
		files = append(files, localReceipt)
	}
	var errs []error
	for _, file := range files {
		if err := os.Remove(filepath.Join(dir, file)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(append(errs, syncDirectory(dir))...)
}

// Run lays the enabled plugins over the system once their volume is
// mounted and takes them away before it goes, until ctx is done.
func (m *Manager) Run(ctx context.Context, mounted func() []string) {
	go m.runDriverUpdates(ctx, mounted)
	m.DetectHardware()
	go m.watchDevices(ctx)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		loaded := m.loaded
		m.mu.Unlock()
		if !loaded {
			m.Load()
		} else if !m.Disabled {
			volumes := mounted()
			if err := m.Apply(ctx, volumes); err != nil {
				slog.Error("plugins", "err", err)
			}
			m.notifyRecommendations(volumes)
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
	m.actions.Lock()
	defer m.actions.Unlock()
	return m.apply(ctx, mounted)
}

func (m *Manager) apply(ctx context.Context, mounted []string) (err error) {
	if m.Disabled {
		return nil
	}
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
	key := ":"
	if len(names) > 0 {
		key = settings.Volume + ":"
	}
	for _, name := range names {
		key += name + "=" + wanted[name].SHA256 + ";"
	}
	if key == m.applied {
		return nil
	}
	defer func() {
		if err == nil {
			return
		}
		for name := range wanted {
			if _, active := m.active[name]; !active && m.problems[name] == "" {
				m.problems[name] = "activation"
			}
		}
		m.applied = key
	}()
	tr := trackerOf(ctx)
	target, tracked := wanted[tr.plugin()]
	tr.begin(StepSysext)
	if err := os.MkdirAll(m.extDir, 0o755); err != nil {
		return err
	}
	links, err := os.ReadDir(m.extDir)
	if err != nil {
		return err
	}
	for _, link := range links {
		if !strings.HasSuffix(link.Name(), ".raw") {
			continue
		}
		name := strings.TrimSuffix(link.Name(), ".raw")
		if wanted[name].Name != "" {
			continue
		}
		if err := m.deactivate(ctx, name); err != nil {
			return err
		}
		if err := os.Remove(filepath.Join(m.extDir, link.Name())); err != nil {
			return err
		}
		delete(m.resident, name)
		delete(m.problems, name)
	}
	for _, name := range names {
		manifest := wanted[name]
		if _, exists := m.resident[name]; !exists {
			if manifest.LocalBuild {
				if _, err := m.localDriver(m.dir(settings.Volume)); err != nil {
					m.problems[name] = "integrity"
					return fmt.Errorf("%w: local driver receipt", ErrInvalid)
				}
			}
			image := filepath.Join(m.dir(settings.Volume), name+".raw")
			if sum, size, err := hashFile(image, maxImageBytes); err != nil || sum != strings.ToLower(manifest.SHA256) || size != manifest.Size {
				m.problems[name] = "integrity"
				return fmt.Errorf("%w: installed image %s changed", ErrInvalid, name)
			}
		}
		link := filepath.Join(m.extDir, name+".raw")
		if _, err := os.Lstat(link); errors.Is(err, fs.ErrNotExist) {
			if err := os.Symlink(filepath.Join(m.dir(settings.Volume), name+".raw"), link); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	if _, err := m.runner.Run(ctx, "", "systemd-sysext", "refresh"); err != nil {
		return err
	}
	if _, err := m.runner.Run(ctx, "", "ldconfig", "-X"); err != nil {
		return err
	}
	if _, err := m.runner.Run(ctx, "", "systemctl", "daemon-reload"); err != nil {
		return err
	}
	needsModules := false
	for _, manifest := range wanted {
		needsModules = needsModules || len(manifest.Modules) > 0
	}
	if needsModules {
		if tracked && len(target.Modules) > 0 {
			tr.begin(StepModules)
		}
		if err := m.prepareModules(ctx); err != nil {
			return err
		}
	}
	var activationErrors []error
	for _, name := range names {
		manifest := wanted[name]
		m.resident[name] = manifest
		if _, active := m.active[name]; active {
			continue
		}
		var failure error
		mine := tracked && name == target.Name
		if mine && len(manifest.Modules) > 0 {
			tr.begin(StepModules)
		} else if mine && len(manifest.Services) > 0 {
			tr.begin(StepServices)
		}
		if m.PrepareRuntime != nil {
			failure = m.PrepareRuntime(name, settings.Volume)
		}
		for _, module := range manifest.Modules {
			if failure != nil {
				break
			}
			if _, failure = m.runner.Run(ctx, "", "modprobe", "-d", m.moduleRoot, module); failure != nil {
				break
			}
		}
		if failure == nil {
			if mine && len(manifest.Services) > 0 {
				tr.begin(StepServices)
			}
			for _, service := range manifest.Services {
				if _, failure = m.runner.Run(ctx, "", "systemctl", "start", service); failure != nil {
					break
				}
			}
		}
		if failure == nil && m.ActivateRuntime != nil {
			failure = m.ActivateRuntime(name)
		}
		if failure != nil {
			m.problems[name] = "activation"
			activationErrors = append(activationErrors, fmt.Errorf("%s: %w", name, failure))
		} else {
			m.active[name] = manifest
			delete(m.problems, name)
		}
	}
	// Failed activation remains visible; an explicit Enable retries it. Do not
	// keep probing absent GPUs or restarting failed services every five seconds.
	m.applied = key
	return errors.Join(activationErrors...)
}

func (m *Manager) deactivate(ctx context.Context, name string) (err error) {
	manifest, exists := m.resident[name]
	if !exists {
		return nil
	}
	defer func() {
		if err == nil {
			return
		}
		m.problems[name] = "busy"
		// Keep a running consumer usable if a later module refuses unloading.
		for _, module := range manifest.Modules {
			if _, restoreErr := m.runner.Run(ctx, "", "modprobe", "-d", m.moduleRoot, module); restoreErr != nil {
				delete(m.active, name)
				err = errors.Join(err, restoreErr)
			}
		}
		for _, service := range manifest.Services {
			if _, restoreErr := m.runner.Run(ctx, "", "systemctl", "start", service); restoreErr != nil {
				delete(m.active, name)
				err = errors.Join(err, restoreErr)
			}
		}
	}()
	for i := len(manifest.Services) - 1; i >= 0; i-- {
		if _, failure := m.runner.Run(ctx, "", "systemctl", "stop", manifest.Services[i]); failure != nil {
			return fmt.Errorf("%w: %v", ErrBusy, failure)
		}
	}
	for i := len(manifest.Modules) - 1; i >= 0; i-- {
		if _, failure := m.runner.Run(ctx, "", "modprobe", "-d", m.moduleRoot, "-r", manifest.Modules[i]); failure != nil {
			return fmt.Errorf("%w: %v", ErrBusy, failure)
		}
	}
	delete(m.active, name)
	delete(m.resident, name)
	return nil
}

// sysext makes /usr read-only. Build the module index in RAM with links to
// the merged base/plugin modules, rather than running depmod against /usr.
func (m *Manager) prepareModules(ctx context.Context) error {
	source := filepath.Join(m.moduleSource, m.kernel)
	target := filepath.Join(m.moduleRoot, "lib/modules", m.kernel)
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		if rel == "build" || rel == "source" || (strings.HasPrefix(rel, "modules.") && !strings.HasPrefix(rel, "modules.builtin")) {
			return nil
		}
		return os.Symlink(path, destination)
	}); err != nil {
		return err
	}
	_, err := m.runner.Run(ctx, "", "depmod", "-b", m.moduleRoot, m.kernel)
	return err
}

// Update replaces an installed plugin with the other version of it that lies
// in a share. The running plugin is stopped first; if the new version cannot
// be put in place or activated, the previous files come back and are switched
// on again. An unsigned version is confirmed by name, like a new community
// plugin. Official plugins come with their system release; NVIDIA has its own
// driver preparation and is never swapped here.
//
// Without manifestPath the newer version comes from the official catalog
// (UpdateOfficial).
func (m *Manager) Update(ctx context.Context, name, manifestPath, confirm string, mounted []string) error {
	if manifestPath == "" {
		return m.UpdateDownload(ctx, name, confirm, mounted)
	}
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.current().PendingDriver != nil {
		return ErrDriverPending
	}
	if m.ChangeAllowed != nil && !m.ChangeAllowed() {
		return ErrUpdatePending
	}
	tr := m.track(name, "update", StepSignature, StepChecksum, StepWrite, StepStop)
	err := m.update(withTracker(ctx, tr), name, manifestPath, confirm, mounted)
	tr.finish(err)
	return err
}

func (m *Manager) update(ctx context.Context, name, manifestPath, confirm string, mounted []string) error {
	tr := trackerOf(ctx)
	if m.Disabled {
		return ErrDisabled
	}
	settings := m.current()
	if !slices.Contains(mounted, settings.Volume) {
		return ErrUnknownVolume
	}
	old, exists := m.installed(settings.Volume)[name]
	if !exists {
		return ErrNotFound
	}
	real, err := filepath.EvalSymlinks(manifestPath)
	if err != nil || !strings.HasPrefix(real, m.mountRoot+"/") || strings.HasPrefix(real, m.baseDir(settings.Volume)+"/") {
		return ErrNotFound
	}
	manifest, err := readManifest(real)
	if err != nil {
		return err
	}
	if manifest.Name != name || !updatable(old, manifest) {
		return fmt.Errorf("%w: not another version of %s", ErrInvalid, name)
	}
	if !m.compatible(manifest) {
		return fmt.Errorf("%w: the new version targets another kernel", ErrInvalid)
	}
	signed := m.official(real)
	if !signed && confirm != name {
		return ErrNotConfirmed
	}
	tr.name(name, manifest.Title)
	enabled := slices.Contains(settings.Enabled, name)
	if enabled {
		tr.expect(activationSteps(manifest)...)
	}
	if signed {
		tr.begin(StepSignature)
	} else {
		tr.skip(StepSignature)
	}
	return m.replace(ctx, name, real, manifest, signed, enabled, settings.Volume, mounted)
}

// replace swaps the installed plugin name for the verified manifest at
// real and the image next to it, and brings the previous files back if the
// new version cannot be put in place or started. Caller holds actions.
func (m *Manager) replace(ctx context.Context, name, real string, manifest Manifest, signed, enabled bool, volume string, mounted []string) error {
	tr := trackerOf(ctx)
	tr.begin(StepChecksum)
	image := filepath.Join(filepath.Dir(real), name+".raw")
	if sum, size, err := hashFile(image, maxImageBytes); err != nil || size != manifest.Size || sum != strings.ToLower(manifest.SHA256) {
		return fmt.Errorf("%w: the image is not the one the manifest describes", ErrInvalid)
	}
	tr.begin(StepWrite)
	if err := m.ensureReleaseSet(volume); err != nil {
		return err
	}
	dir := m.dir(volume)

	// The new files are copied and checked next to the installed ones
	// before anything running is touched.
	stage, err := os.MkdirTemp(dir, ".update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	staged := filepath.Join(stage, name+manifestSuffix)
	if err := copyFile(image, filepath.Join(stage, name+".raw"), 0o600); err != nil {
		return err
	}
	if sum, _, err := hashFile(filepath.Join(stage, name+".raw"), maxImageBytes); err != nil || sum != strings.ToLower(manifest.SHA256) {
		return fmt.Errorf("%w: the image changed while it was copied", ErrInvalid)
	}
	if signed {
		if err := copyFile(real+".minisig", staged+".minisig", 0o600); err != nil {
			return err
		}
	}
	if err := copyFile(real, staged, 0o600); err != nil {
		return err
	}
	if copied, err := readManifest(staged); err != nil || !reflect.DeepEqual(copied, manifest) || (signed && !m.official(staged)) {
		return fmt.Errorf("%w: plugin metadata changed while being copied", ErrInvalid)
	}
	previous, err := os.MkdirTemp(dir, ".previous-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(previous)

	tr.begin(StepStop)
	if err := m.deactivate(ctx, name); err != nil {
		return err // still running as before, nothing was replaced
	}
	files := []string{name + ".raw", name + manifestSuffix + ".minisig", name + manifestSuffix}
	move := func(from, to string) error {
		for _, file := range files {
			if err := os.Rename(filepath.Join(from, file), filepath.Join(to, file)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		return syncDirectory(dir)
	}
	putBack := func(cause error) error {
		// The failing step keeps its mark; bringing the old files back is a
		// step of its own, not followed by the steps of the new version.
		if trackerOf(ctx) != nil {
			tr.fail(cause)
			tr.begin(StepRollback)
			ctx = withTracker(ctx, nil)
		}
		// Stop what of the new version may have started, without the
		// restart-on-failure of deactivate: it is not going to stay.
		if manifest, resident := m.resident[name]; resident {
			for i := len(manifest.Services) - 1; i >= 0; i-- {
				m.runner.Run(ctx, "", "systemctl", "stop", manifest.Services[i])
			}
			for i := len(manifest.Modules) - 1; i >= 0; i-- {
				m.runner.Run(ctx, "", "modprobe", "-d", m.moduleRoot, "-r", manifest.Modules[i])
			}
		}
		delete(m.active, name)
		delete(m.resident, name)
		delete(m.problems, name)
		for _, file := range files {
			os.Remove(filepath.Join(dir, file))
		}
		err := move(previous, dir)
		m.applied = "-"
		err = errors.Join(err, m.apply(ctx, mounted))
		if tr != nil {
			if err != nil {
				tr.fail(err)
			} else {
				tr.complete()
			}
		}
		return errors.Join(fmt.Errorf("the new version was not installed, the previous one is back: %w", cause), err)
	}
	// Swapping the files is part of laying the new version over the system.
	if enabled {
		tr.begin(StepSysext)
	} else {
		tr.complete()
	}
	if err := move(dir, previous); err != nil {
		return putBack(err)
	}
	if err := move(stage, dir); err != nil {
		return putBack(err)
	}
	m.applied = "-"
	err = m.apply(ctx, mounted)
	// An error of another plugin is reported, but does not undo this one.
	if _, active := m.active[name]; enabled && !active {
		if err == nil {
			tr.begin(StepHealth)
		}
		return putBack(errors.Join(errors.New("activation failed"), err))
	}
	if enabled {
		tr.begin(StepHealth)
		tr.complete()
	}
	return err
}
