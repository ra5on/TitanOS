// Package update brings a new TitanOS onto the stick and switches to it.
//
// The stick holds two systems, a and b. The running one is never touched:
// an update is written to the other slot and gets a boot entry with a
// counter (systemd-boot). The NAS starts from it up to three times; once
// titand is up and says so, the entry is marked good. If it never gets
// there, the boot loader goes back to the old slot by itself.
//
// Updates come from the GitHub releases of the project. What is installed
// is decided by a manifest that carries a signature (minisign) of a key
// that ships in the image; without a valid one nothing is installed.
package update

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ra5on/TitanOS/backend/internal/app"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/minisign"
)

const (
	settingsFile = "system/update.json"
	pendingFile  = "system/update-pending.json"
	// DefaultSource is where the releases are.
	DefaultSource = "https://github.com/ra5on/TitanOS"

	// tries is how often the boot loader starts a new slot before it
	// gives up on it.
	tries = 3
	// Sort keys of the boot entries: the loader starts the first one that
	// has tries left.
	keyPreferred = "titanos-1"
	keyFallback  = "titanos-2"

	maxManifest = 256 << 10
	// FAT32 holds no file of 4 GiB; a system image is far below that.
	maxFile = 3 << 30
)

// Channels of releases.
const (
	ChannelStable = "stable"
	ChannelBeta   = "beta"
)

var (
	// ErrDisabled means titand runs in development mode.
	ErrDisabled = errors.New("update: not available in development mode")
	// ErrNotLoaded means /config could not be read.
	ErrNotLoaded = errors.New("update: settings not loaded from /config")
	// ErrInvalid means a setting is not acceptable.
	ErrInvalid = errors.New("update: invalid setting")
	// ErrNoKey means the image holds no key to check updates with.
	ErrNoKey = errors.New("update: this image holds no key to verify updates with")
	// ErrSignature means the manifest of a release is not signed by a
	// key this image trusts.
	ErrSignature = errors.New("update: the release is not signed by a trusted key")
	// ErrManifest means a manifest is signed but makes no sense.
	ErrManifest = errors.New("update: the manifest of the release is not usable")
	// ErrBusy means an update is being installed or waits for the restart.
	ErrBusy = errors.New("update: an update is in progress")
	// ErrNothing means there is no update to install, or no slot to go back to.
	ErrNothing = errors.New("update: nothing to install")
	// ErrTooOld means this version has to take an update in between first.
	ErrTooOld = errors.New("update: this version cannot go to that release directly")
	// ErrNoSpace means the stick is too small for a second system.
	ErrNoSpace = errors.New("update: not enough room on the stick")
	// ErrUnconfirmed means the running system was itself not confirmed yet.
	ErrUnconfirmed = errors.New("update: the running system is not confirmed as good yet")
	// ErrWorking means titand is at work that a restart would break off:
	// an app update waits for it.
	ErrWorking = errors.New("update: copies, backups or disk tests are running")
)

// Kinds of an update.
const (
	// KindApp replaces titand alone and restarts it, not the NAS.
	KindApp = "app"
	// KindSystem writes a whole system into the other slot.
	KindSystem = "system"
)

// Settings is /config/system/update.json.
type Settings struct {
	Channel string `json:"channel"`
	// AutoInstall installs a found update at InstallTime (HH:MM, local
	// time) and restarts the NAS.
	AutoInstall bool   `json:"autoInstall"`
	InstallTime string `json:"installTime"`
	// Source replaces the address of the project, for a mirror. It is
	// not offered in the web UI; whatever it serves still has to carry a
	// valid signature.
	Source string `json:"source,omitempty"`
	// CheckEvery is how many minutes lie between two automatic checks:
	// 0 (or missing) is once a day, CheckManual turns automatic checks off.
	// Versions before it ignore the field and keep checking once a day.
	CheckEvery int `json:"checkEvery,omitempty"`
	// Kernel is the kernel variant updates install: "" or "stable" for
	// Debian's kernel, "new" for the newer one from trixie-backports
	// (variants.go). Versions before it ignore the field and install the
	// stable variant.
	Kernel string `json:"kernel,omitempty"`
}

const (
	// CheckManual in CheckEvery leaves looking for updates to the button:
	// no scheduled checks, none after a login and no automatic installation.
	CheckManual = -1
	// CheckDaily is the interval in minutes when CheckEvery is not set.
	CheckDaily = 24 * 60
	// MinCheckEvery and MaxCheckEvery bound a chosen interval: an hour to
	// 30 days.
	MinCheckEvery = 60
	MaxCheckEvery = 30 * 24 * 60
	// checkRetry is the longest wait after a failed check. A daily check
	// waits a day as before; a weekly one does not wait a week to learn
	// whether the network is back.
	checkRetry = 24 * time.Hour
	// checkStartDelay lets the network come up before the first check.
	checkStartDelay = 5 * time.Minute
)

// Manual tells whether automatic checks are off.
func (s Settings) Manual() bool {
	return s.CheckEvery == CheckManual
}

// CheckInterval is the time between two automatic checks. A value out of
// range, say from a file edited by hand, counts as once a day.
func (s Settings) CheckInterval() time.Duration {
	if s.CheckEvery < MinCheckEvery || s.CheckEvery > MaxCheckEvery {
		return CheckDaily * time.Minute
	}
	return time.Duration(s.CheckEvery) * time.Minute
}

// File is one file of a system slot in a release.
type File struct {
	// Name in the slot: vmlinuz, initrd.img or filesystem.squashfs.
	Name string `json:"name"`
	// Asset is the name of the file in the release.
	Asset  string `json:"asset"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Manifest describes a release. It is the signed part of an update.
type Manifest struct {
	Version string `json:"version"`
	Tag     string `json:"tag"`
	Channel string `json:"channel"`
	Kernel  string `json:"kernel"`
	// KernelPackage is the Debian version of the kernel package; the
	// plugin repository orders its kernel sets by it. Optional.
	KernelPackage string    `json:"kernelPackage,omitempty"`
	Released      time.Time `json:"released"`
	// MinVersion is the oldest version that may go to this one directly.
	MinVersion string `json:"minVersion,omitempty"`
	Changelog  string `json:"changelog"`
	// Cmdline is the kernel command line of the boot entry; @SLOT@ stands
	// for the slot.
	Cmdline string `json:"cmdline"`
	Files   []File `json:"files"`
	// App is titand of this release alone, for systems with the image it
	// was made for. Older versions of TitanOS do not know the field and
	// install Files as always.
	App     *app.Info `json:"app,omitempty"`
	Plugins []struct {
		Name  string `json:"name"`
		Asset string `json:"asset"`
	} `json:"plugins,omitempty"`
	// Variants holds the same system with another kernel, by name ("new").
	// Optional; older versions ignore it and install Files.
	Variants map[string]Variant `json:"variants,omitempty"`
}

// Size returns how much the release takes on the stick.
func (m Manifest) Size() int64 {
	var total int64
	for _, file := range m.Files {
		total += file.Size
	}
	return total
}

// Slot is one of the two systems on the stick.
type Slot struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// Running is the slot this system was started from.
	Running bool `json:"running"`
	// Complete says whether there is a system in it that could be started.
	Complete bool `json:"complete"`
	// Next is the slot the boot loader starts next time.
	Next bool `json:"next"`
	// TriesLeft is set while the slot still has to prove itself.
	TriesLeft *int `json:"triesLeft"`
	// Kernel is the kernel release in the slot, Variant its kernel
	// variant (stable or new); empty if unknown.
	Kernel  string `json:"kernel,omitempty"`
	Variant string `json:"variant,omitempty"`
}

// Progress is how far an installation is.
type Progress struct {
	// Phase is "", download, switch, done or failed.
	Phase               string  `json:"phase"`
	File                string  `json:"file"`
	Percent             float64 `json:"percent"`
	SpeedBytesPerSecond uint64  `json:"speedBytesPerSecond"`
	Error               string  `json:"error"`
	// FallbackRemoved says that the stick had no room for the new system
	// next to both old ones: the fallback slot was emptied before the
	// download, and a failed download leaves no system to go back to.
	FallbackRemoved bool `json:"fallbackRemoved,omitempty"`
}

// Pending is an update or a step back that takes effect with the next restart.
type Pending struct {
	// Kind is update or rollback.
	Kind    string    `json:"kind"`
	Version string    `json:"version"`
	Slot    string    `json:"slot"`
	From    string    `json:"from"`
	Since   time.Time `json:"since"`
	// BootID is the start of the NAS during which this was set up.
	BootID string `json:"bootID"`
	// Volumes were mounted then. The new system only counts as good once
	// it has them mounted again: a kernel that no longer sees the disks
	// must not stay.
	Volumes []string `json:"volumes"`
}

// Status is what the update page shows.
type Status struct {
	Version string `json:"version"`
	// Release is false for a build that is not a release (development).
	Release  bool     `json:"release"`
	Slots    []Slot   `json:"slots"`
	Settings Settings `json:"settings"`
	// CanVerify is false if the image holds no key: then no update can
	// be installed.
	CanVerify bool `json:"canVerify"`
	// Available is the newer release that was found, if any.
	Available *Manifest  `json:"available"`
	LastCheck *time.Time `json:"lastCheck"`
	// NextCheck is when the next automatic check is due; nil when checks
	// are manual or none are scheduled (development mode).
	NextCheck  *time.Time `json:"nextCheck"`
	CheckError string     `json:"checkError"`
	Progress   Progress   `json:"progress"`
	Pending    *Pending   `json:"pending"`
	// CanRollback says whether the other slot holds a system to go back to.
	CanRollback bool `json:"canRollback"`
	// Unconfirmed is set while the running system still has to be marked good.
	Unconfirmed bool `json:"unconfirmed"`
	// Base is the version of the system image; Version may be newer when
	// an app update runs on it.
	Base string `json:"base"`
	// Kind is how Available would be installed: app or system.
	Kind string `json:"kind"`
	// App is what lies in the app folder of the running slot.
	App AppStatus `json:"app"`
	// Variant is the kernel variant of the running system.
	Variant string `json:"variant"`
	// BootLoader is what started this system: systemd-boot, grub-i386-pc
	// (legacy BIOS) or grub-i386-efi (32-bit UEFI).
	BootLoader string `json:"bootLoader"`
	// KernelSwitch says that Available is the running version with the
	// other kernel variant, as the settings ask for.
	KernelSwitch bool `json:"kernelSwitch,omitempty"`
	// KernelMissing names a newer release that has no system with the
	// chosen kernel variant: it is not offered until the choice changes.
	KernelMissing string `json:"kernelMissing,omitempty"`
}

// AppStatus tells about the app updates of the running slot.
type AppStatus struct {
	Current  string `json:"current"`
	Previous string `json:"previous"`
	// Trial is set while a new app has to prove itself.
	Trial bool `json:"trial"`
	// CanRollback says whether there is an app or the base to go back to.
	CanRollback bool `json:"canRollback"`
}

// Manager checks for, installs and takes back updates.
type Manager struct {
	store  *config.Store
	client *http.Client
	// bootDir is where the stick is mounted, keyDir where the keys are
	// that updates must be signed with.
	bootDir, keyDir string
	// cmdline returns the kernel command line (/proc/cmdline): it tells
	// whether GRUB started this system.
	cmdline func() string
	// version and slot are what is running.
	version, slot string
	bootID        string
	now           func() time.Time
	// stall is how long a download may stand still, retryPause the wait
	// before it is taken up again.
	stall, retryPause time.Duration
	// freeSpace is what the stick has free; tests replace it.
	freeSpace func() (int64, error)

	// Disabled leaves the stick alone: in development mode titand runs
	// on the developer's own machine.
	Disabled bool
	// Variant is the kernel variant of the running image (ReadVariant).
	Variant string
	// OnEvent is told what users should hear of: update_available,
	// update_installed, update_failed, update_rolled_back.
	OnEvent func(kind, subject string)
	// Reboot restarts the NAS after a scheduled installation.
	Reboot func() error
	// Mounted returns the volumes that are mounted right now.
	Mounted func() []string
	// Encrypted names the volumes with LUKS encryption. A system before
	// encryptionSince cannot open them; going back to one must not wait for
	// them, or its boot check fails and the stick returns to this system.
	Encrypted func() []string
	// PreparePlugins gets version, kernel variant, kernel, kernel package,
	// base version and the plugin assets of the target.
	PreparePlugins func(context.Context, string, string, string, string, string, map[string]string, func(context.Context, string, int64) ([]byte, error), func(context.Context, string, string, string, int64) error) error
	PluginsHealthy func() bool

	// Base is the version of the system image; "" for one that knows no
	// app updates. Apps keeps the apps of the running slot; nil turns app
	// updates off.
	Base string
	Apps *app.Stick
	// Restart restarts titand after an app changed, together with the
	// check that the app is ready.
	Restart func() error
	// Working tells whether titand is at work that a restart would break off.
	Working func() bool
	// appFor is where the app folder of a slot lies; tests replace it.
	appFor func(slot string) *app.Stick
	// restartDelay lets the last answers out before titand restarts.
	restartDelay time.Duration

	// work serializes everything that writes to the stick.
	work sync.Mutex

	mu        sync.Mutex
	loaded    bool
	settings  Settings
	available *Manifest
	// availableRaw and availableSig are the signed manifest as it came:
	// an app keeps them next to it on the stick.
	availableRaw, availableSig []byte
	// found is the newest release the last check found, newer or not;
	// evaluateLocked decides from it and the settings what is available.
	found              *Manifest
	foundRaw, foundSig []byte
	kernelMissing      string
	// appTrial is set while the app of this slot is on trial; appCache is
	// what was read from the stick about it.
	appTrial          bool
	appCache          *app.State
	announced         string
	loginCheckRunning bool
	lastCheck         time.Time
	// started is when Run began; scheduled checks count from it until the
	// first check is done.
	started    time.Time
	checkError string
	progress   Progress
	pending    *Pending
	slotCache  []Slot
	slotTime   time.Time
	// kernelParams are the kernel parameters settings of the NAS ask for;
	// every boot entry written carries them. "" means: take it out.
	kernelParams map[string]string
}

// NewManager returns a manager for the system that is running: its
// version and the slot it was started from.
func NewManager(store *config.Store, version, slot string) *Manager {
	bootID, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return &Manager{
		store: store, client: &http.Client{}, bootDir: "/efi", keyDir: "/usr/share/titanos/update-keys",
		version: version, slot: slot, bootID: strings.TrimSpace(string(bootID)), now: time.Now, stall: 90 * time.Second, retryPause: 3 * time.Second, restartDelay: 2 * time.Second,
		settings: Settings{Channel: ChannelStable, InstallTime: "03:30"},
		Variant:  VariantStable,
		cmdline: func() string {
			data, _ := os.ReadFile("/proc/cmdline")
			return string(data)
		},
	}
}

// stickFor returns the app folder of a slot.
func (m *Manager) stickFor(slot string) *app.Stick {
	if m.appFor != nil {
		return m.appFor(slot)
	}
	return app.New(m.bootDir, slot)
}

// Load reads the settings and what was left pending before the restart.
func (m *Manager) Load() error {
	settings := Settings{Channel: ChannelStable, InstallTime: "03:30"}
	err := m.store.ReadJSON(settingsFile, &settings)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// One release channel from 0.5.0 onward. Save the migration once so a
	// rollback to an older beta also continues to follow stable releases.
	if settings.Channel != ChannelStable {
		settings.Channel = ChannelStable
		if err := m.store.WriteJSON(settingsFile, settings); err != nil {
			return err
		}
	}
	var pending *Pending
	var stored Pending
	if err := m.store.ReadJSON(pendingFile, &stored); err == nil {
		pending = &stored
	}
	m.mu.Lock()
	m.settings, m.pending, m.loaded = settings, pending, true
	m.mu.Unlock()
	m.settleApp()
	return nil
}

// settleApp reads the app state of the running slot and tells once what
// came of the last change of the app. While an app is on trial, Run calls
// it every minute until the check after the restart has settled it; the
// stick is not read otherwise.
func (m *Manager) settleApp() {
	if m.Apps == nil {
		return
	}
	state, err := m.Apps.ReadState()
	if err != nil {
		slog.Warn("app state not readable from the stick", "err", err)
		return
	}
	m.mu.Lock()
	m.appTrial, m.appCache = state.Trial, &state
	m.mu.Unlock()
	if state.Trial || state.Result == "" {
		return
	}
	result, version, err := m.Apps.TakeResult()
	if err != nil {
		slog.Warn("app result not cleared", "err", err)
		return
	}
	state.Result, state.ResultVersion = "", ""
	m.mu.Lock()
	m.appCache = &state
	m.mu.Unlock()
	if version == "" {
		version = m.Base
	}
	kind := map[string]string{app.ResultInstalled: "update_installed", app.ResultFailed: "update_failed", app.ResultRolledBack: "update_rolled_back"}[result]
	slog.Info("app update settled", "outcome", result, "version", version, "running", m.version)
	if kind != "" && m.OnEvent != nil {
		m.OnEvent(kind, version)
	}
}

// appKind tells whether a release can be installed as an app here.
func (m *Manager) appKind(manifest *Manifest) bool {
	return manifest != nil && manifest.App != nil && m.Apps != nil && m.Base != "" && manifest.App.Base == m.Base &&
		m.wantedVariant() == normalVariant(m.Variant)
}

var clockTime = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// SetSettings stores channel and schedule. The source is kept as it is.
func (m *Manager) SetSettings(settings Settings) error {
	if settings.Channel != "" && settings.Channel != ChannelStable && settings.Channel != ChannelBeta || !clockTime.MatchString(settings.InstallTime) {
		return ErrInvalid
	}
	if settings.CheckEvery != 0 && settings.CheckEvery != CheckManual && (settings.CheckEvery < MinCheckEvery || settings.CheckEvery > MaxCheckEvery) {
		return ErrInvalid
	}
	// Installing by itself needs something that finds the update.
	if settings.Manual() && settings.AutoInstall {
		return ErrInvalid
	}
	if !validVariant(settings.Kernel) {
		return ErrInvalid
	}
	// The default kernel is the default: leave the field out.
	if settings.Kernel == VariantStable {
		settings.Kernel = ""
	}
	// Once a day is the default: leave the field out so the file stays as
	// it was before the field existed.
	if settings.CheckEvery == CheckDaily {
		settings.CheckEvery = 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	settings.Channel = ChannelStable
	settings.Source = m.settings.Source
	if err := m.store.WriteJSON(settingsFile, settings); err != nil {
		return err
	}
	if settings.Channel != m.settings.Channel {
		m.available, m.found = nil, nil // found for the other channel
	}
	m.settings = settings
	// Another kernel choice can turn the last found release into an
	// update, a kernel switch or nothing.
	m.evaluateLocked(false)
	return nil
}

func (m *Manager) source() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.settings.Source != "" {
		return strings.TrimRight(m.settings.Source, "/")
	}
	return DefaultSource
}

// Confirm looks at what a restart made of a pending update and tells. It
// is called after the start and every minute until it is settled.
func (m *Manager) Confirm() {
	m.mu.Lock()
	pending := m.pending
	m.mu.Unlock()
	// Still the same start of the NAS: the restart is yet to come.
	if pending == nil || pending.BootID == m.bootID {
		return
	}
	// The new system is running but still on trial: it only counts once
	// the boot loader's counter is gone. Should it be restarted before
	// that and fall back, the old system finds this still pending.
	if e, ok := m.readEntries()[m.slot]; pending.Slot == m.slot && ok && e.left != nil {
		return
	}
	kind := "update_failed"
	subject := pending.Version
	switch {
	case pending.Slot == m.slot && pending.Kind == "rollback":
		kind = "update_rolled_back"
	case pending.Slot == m.slot:
		kind = "update_installed"
	}
	slog.Info("pending update settled", "outcome", kind, "version", pending.Version, "slot", pending.Slot, "running", m.slot)
	if err := m.store.Remove(pendingFile); err != nil {
		slog.Warn("pending update not cleared", "err", err)
	}
	m.mu.Lock()
	m.pending, m.slotCache = nil, nil
	m.mu.Unlock()
	if m.OnEvent != nil {
		m.OnEvent(kind, subject)
	}
}

// Healthy reports whether this system may be marked good. It always is,
// except while it is the one on trial and a volume that was mounted before
// the switch is not mounted yet.
func (m *Manager) Healthy() bool {
	m.mu.Lock()
	pending := m.pending
	m.mu.Unlock()
	if pending == nil || pending.Slot != m.slot || pending.BootID == m.bootID || m.Mounted == nil {
		return true
	}
	if m.PluginsHealthy != nil && !m.PluginsHealthy() {
		return false
	}
	mounted := m.Mounted()
	for _, volume := range pending.Volumes {
		if !slices.Contains(mounted, volume) {
			return false
		}
	}
	return true
}

// OnTrial reports whether the boot loader started this system with a
// counter on its entry.
func (m *Manager) OnTrial() bool {
	e, ok := m.readEntries()[m.slot]
	return ok && e.left != nil
}

// Bless takes the counter off the boot entry of the running system: from
// now on the loader starts it without counting. This is what
// systemd-bless-boot does; TitanOS does it itself, once titand is ready,
// and gets by without that service and its EFI variables.
func (m *Manager) Bless() error {
	e, ok := m.readEntries()[m.slot]
	if !ok || e.left == nil {
		return nil
	}
	good := filepath.Join(filepath.Dir(e.path), "titanos-"+m.slot+".conf")
	if err := os.Rename(e.path, good); err != nil {
		// titand may just have written GRUB's count into the name
		// (ReconcileBootLoader): take the entry as it is now.
		e, ok = m.readEntries()[m.slot]
		if !ok || e.left == nil {
			return err
		}
		if err := os.Rename(e.path, good); err != nil {
			return err
		}
	}
	m.syncGrub(false)
	unix.Sync()
	m.forget()
	slog.Info("this system is marked good", "slot", m.slot)
	return nil
}

// CurrentProgress is safe for background polling: unlike Status it never
// reads signing keys or the boot entries on the USB stick.
func (m *Manager) CurrentProgress() Progress {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.progress
}

// Status returns everything the update page shows.
func (m *Manager) Status() Status {
	m.mu.Lock()
	status := Status{Version: m.version, Settings: m.settings, Available: m.available, CheckError: m.checkError, Progress: m.progress, Pending: m.pending}
	if !m.lastCheck.IsZero() {
		at := m.lastCheck
		status.LastCheck = &at
	}
	if next, ok := m.nextCheckLocked(); ok {
		status.NextCheck = &next
	}
	status.Base = m.Base
	status.Variant = normalVariant(m.Variant)
	status.BootLoader = m.BootLoader()
	status.KernelMissing = m.kernelMissing
	if m.available != nil {
		status.Kind = KindSystem
		if m.appKind(m.available) {
			status.Kind = KindApp
		}
		status.KernelSwitch = m.available.Version == m.version
	}
	if m.appCache != nil {
		cache := *m.appCache
		status.App = AppStatus{Current: cache.Current, Previous: cache.Previous, Trial: cache.Trial, CanRollback: !cache.Trial && (cache.Current != "" || cache.Previous != "")}
	}
	m.mu.Unlock()
	status.Settings.Source = ""
	_, status.Release = parseVersion(m.version)
	status.CanVerify = len(minisign.LoadKeys(m.keyDir)) > 0
	status.Slots = []Slot{}
	if m.Disabled {
		return status
	}
	for _, slot := range m.slots(status.Pending != nil || status.Progress.Phase == "download" || status.Progress.Phase == "switch") {
		status.Slots = append(status.Slots, slot)
		if slot.Running && slot.TriesLeft != nil {
			status.Unconfirmed = true
		}
		if !slot.Running && slot.Complete && (slot.TriesLeft == nil || *slot.TriesLeft > 0) {
			status.CanRollback = true
		}
	}
	return status
}

// encryptionSince is the first version that opens encrypted volumes.
var encryptionSince = "0.31.0"

// knowsEncryption tells whether a target version can open encrypted
// volumes; a version that is no release number (a development build) is
// taken to be new.
func knowsEncryption(target string) bool {
	v, ok := parseVersion(target)
	since, _ := parseVersion(encryptionSince)
	return !ok || v.compare(since) >= 0
}
