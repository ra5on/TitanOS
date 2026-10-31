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
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
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
	ErrInvalid       = errors.New("plugins: not a valid plugin")
	ErrNotConfirmed  = errors.New("plugins: the installation was not confirmed")
	ErrBusy          = errors.New("plugins: a service or kernel module is still in use")
	ErrDownloading   = errors.New("plugins: this plugin is already being downloaded")
	ErrDriverPending = errors.New("plugins: cancel the scheduled driver activation or reboot before changing plugins")
	ErrUpdatePending = errors.New("plugins: finish or cancel the system update before changing plugins")
	// ErrConflict means an enabled plugin excludes the one to be enabled.
	ErrConflict = errors.New("plugins: another enabled plugin excludes this one")
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
	// Conflicts names plugins that must not be enabled together with this
	// one (conflicts.go). Optional; older TitanOS ignore it.
	Conflicts []string `json:"conflicts,omitempty"`
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
	// Variant is the kernel variant of the running image ("" or "stable"
	// for the default kernel, "new" for the newer one, docs/KERNEL.md):
	// its plugins are kept apart from those of the same version with the
	// other kernel. Set before Load.
	Variant   string
	Disabled  bool
	mountRoot string
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
		dir := m.releaseDir(volume, m.key())
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
