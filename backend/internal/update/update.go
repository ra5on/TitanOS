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
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

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
	Version  string    `json:"version"`
	Tag      string    `json:"tag"`
	Channel  string    `json:"channel"`
	Kernel   string    `json:"kernel"`
	Released time.Time `json:"released"`
	// MinVersion is the oldest version that may go to this one directly.
	MinVersion string `json:"minVersion,omitempty"`
	Changelog  string `json:"changelog"`
	// Cmdline is the kernel command line of the boot entry; @SLOT@ stands
	// for the slot.
	Cmdline string `json:"cmdline"`
	Files   []File `json:"files"`
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
}

// Progress is how far an installation is.
type Progress struct {
	// Phase is "", download, switch, done or failed.
	Phase               string  `json:"phase"`
	File                string  `json:"file"`
	Percent             float64 `json:"percent"`
	SpeedBytesPerSecond uint64  `json:"speedBytesPerSecond"`
	Error               string  `json:"error"`
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
	Available  *Manifest  `json:"available"`
	LastCheck  *time.Time `json:"lastCheck"`
	CheckError string     `json:"checkError"`
	Progress   Progress   `json:"progress"`
	Pending    *Pending   `json:"pending"`
	// CanRollback says whether the other slot holds a system to go back to.
	CanRollback bool `json:"canRollback"`
	// Unconfirmed is set while the running system still has to be marked good.
	Unconfirmed bool `json:"unconfirmed"`
}

// Manager checks for, installs and takes back updates.
type Manager struct {
	store  *config.Store
	client *http.Client
	// bootDir is where the stick is mounted, keyDir where the keys are
	// that updates must be signed with.
	bootDir, keyDir string
	// version and slot are what is running.
	version, slot string
	bootID        string
	now           func() time.Time
	// stall is how long a download may stand still, retryPause the wait
	// before it is taken up again.
	stall, retryPause time.Duration

	// Disabled leaves the stick alone: in development mode titand runs
	// on the developer's own machine.
	Disabled bool
	// OnEvent is told what users should hear of: update_available,
	// update_installed, update_failed, update_rolled_back.
	OnEvent func(kind, subject string)
	// Reboot restarts the NAS after a scheduled installation.
	Reboot func() error
	// Mounted returns the volumes that are mounted right now.
	Mounted func() []string

	// work serializes everything that writes to the stick.
	work sync.Mutex

	mu         sync.Mutex
	loaded     bool
	settings   Settings
	available  *Manifest
	announced  string
	lastCheck  time.Time
	checkError string
	progress   Progress
	pending    *Pending
	slotCache  []Slot
	slotTime   time.Time
}

// NewManager returns a manager for the system that is running: its
// version and the slot it was started from.
func NewManager(store *config.Store, version, slot string) *Manager {
	bootID, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return &Manager{
		store: store, client: &http.Client{}, bootDir: "/efi", keyDir: "/usr/share/titanos/update-keys",
		version: version, slot: slot, bootID: strings.TrimSpace(string(bootID)), now: time.Now, stall: 90 * time.Second, retryPause: 3 * time.Second,
		settings: Settings{Channel: ChannelStable, InstallTime: "03:30"},
	}
}

// Load reads the settings and what was left pending before the restart.
func (m *Manager) Load() error {
	settings := Settings{Channel: ChannelStable, InstallTime: "03:30"}
	err := m.store.ReadJSON(settingsFile, &settings)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var pending *Pending
	var stored Pending
	if err := m.store.ReadJSON(pendingFile, &stored); err == nil {
		pending = &stored
	}
	m.mu.Lock()
	m.settings, m.pending, m.loaded = settings, pending, true
	m.mu.Unlock()
	return nil
}

var clockTime = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// SetSettings stores channel and schedule. The source is kept as it is.
func (m *Manager) SetSettings(settings Settings) error {
	if settings.Channel != ChannelStable && settings.Channel != ChannelBeta || !clockTime.MatchString(settings.InstallTime) {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	settings.Source = m.settings.Source
	if err := m.store.WriteJSON(settingsFile, settings); err != nil {
		return err
	}
	if settings.Channel != m.settings.Channel {
		m.available = nil // found for the other channel
	}
	m.settings = settings
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
		return err
	}
	unix.Sync()
	m.forget()
	slog.Info("this system is marked good", "slot", m.slot)
	return nil
}

// --- the stick -------------------------------------------------------------

func other(slot string) string {
	if slot == "a" {
		return "b"
	}
	return "a"
}

var slotFiles = []string{"vmlinuz", "initrd.img", "filesystem.squashfs"}

// entry is the boot entry of a slot as it lies on the stick.
type entry struct {
	path    string
	content string
	// left is the number of tries the loader still gives it; nil for an
	// entry that is marked good.
	left *int
}

var entryName = regexp.MustCompile(`^titanos-([ab])(?:\+([0-9]+)(?:-[0-9]+)?)?\.conf$`)

// readEntries returns the boot entries by slot.
func (m *Manager) readEntries() map[string]entry {
	entries := map[string]entry{}
	dir := filepath.Join(m.bootDir, "loader/entries")
	files, _ := os.ReadDir(dir)
	for _, file := range files {
		match := entryName.FindStringSubmatch(file.Name())
		if match == nil {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, file.Name()))
		if err != nil {
			continue
		}
		e := entry{path: filepath.Join(dir, file.Name()), content: string(content)}
		if match[2] != "" {
			var left int
			fmt.Sscan(match[2], &left)
			e.left = &left
		}
		entries[match[1]] = e
	}
	return entries
}

var sortKeyLine = regexp.MustCompile(`(?m)^sort-key[ \t]+(\S+)[ \t]*$`)

func sortKey(content string) string {
	if match := sortKeyLine.FindStringSubmatch(content); match != nil {
		return match[1]
	}
	return ""
}

func withSortKey(content, key string) string {
	if sortKeyLine.MatchString(content) {
		return sortKeyLine.ReplaceAllString(content, "sort-key "+key)
	}
	return strings.TrimRight(content, "\n") + "\nsort-key " + key + "\n"
}

// nextSlot tells which slot the boot loader starts next: the first entry
// in its order that has tries left.
func nextSlot(entries map[string]entry) string {
	best := ""
	for slot, e := range entries {
		if e.left != nil && *e.left == 0 {
			continue // given up on
		}
		if best == "" || sortKey(e.content) < sortKey(entries[best].content) || sortKey(e.content) == sortKey(entries[best].content) && slot < best {
			best = slot
		}
	}
	return best
}

// writeFile replaces a file on the stick: written next to it, flushed,
// then renamed over it.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".new"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// putEntry writes the boot entry of a slot, with a counter if it has to
// prove itself, and removes the other forms of its name.
func (m *Manager) putEntry(slot, content string, counted bool) error {
	dir := filepath.Join(m.bootDir, "loader/entries")
	name := "titanos-" + slot + ".conf"
	if counted {
		name = fmt.Sprintf("titanos-%s+%d.conf", slot, tries)
	}
	if err := writeFile(filepath.Join(dir, name), []byte(content)); err != nil {
		return err
	}
	files, _ := os.ReadDir(dir)
	for _, file := range files {
		if match := entryName.FindStringSubmatch(file.Name()); match != nil && match[1] == slot && file.Name() != name {
			os.Remove(filepath.Join(dir, file.Name()))
		}
	}
	return syncDir(dir)
}

func (m *Manager) removeEntry(slot string) {
	dir := filepath.Join(m.bootDir, "loader/entries")
	files, _ := os.ReadDir(dir)
	for _, file := range files {
		if match := entryName.FindStringSubmatch(file.Name()); match != nil && match[1] == slot {
			os.Remove(filepath.Join(dir, file.Name()))
		}
	}
	syncDir(dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	// FAT has no directory entries of its own to flush; an error here is
	// no reason to fail.
	d.Sync()
	return nil
}

// unpinLoader removes a fixed default from loader.conf: with one, the
// loader would start that entry even after giving up on it.
func (m *Manager) unpinLoader() error {
	path := filepath.Join(m.bootDir, "loader/loader.conf")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil // no file, no default
	}
	var kept []string
	changed := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "default") {
			changed = true
			continue
		}
		kept = append(kept, line)
	}
	if !changed {
		return nil
	}
	return writeFile(path, []byte(strings.Join(kept, "\n")))
}

func (m *Manager) slotVersion(slot string) string {
	data, err := os.ReadFile(filepath.Join(m.bootDir, "titanos", slot, "version"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (m *Manager) slotComplete(slot string) bool {
	for _, name := range slotFiles {
		if info, err := os.Stat(filepath.Join(m.bootDir, "titanos", slot, name)); err != nil || info.Size() == 0 {
			return false
		}
	}
	return true
}

// slots returns what lies in the two slots. The stick sleeps when nobody
// uses it, and a page that asks every few seconds must not keep it awake:
// what was read is kept for a while, unless something is going on.
func (m *Manager) slots(fresh bool) []Slot {
	m.mu.Lock()
	cached, age := m.slotCache, m.now().Sub(m.slotTime)
	m.mu.Unlock()
	if cached != nil && !fresh && age < 10*time.Minute {
		return cached
	}
	entries := m.readEntries()
	next := nextSlot(entries)
	var list []Slot
	for _, name := range []string{"a", "b"} {
		e, hasEntry := entries[name]
		slot := Slot{Name: name, Version: m.slotVersion(name), Running: name == m.slot, Complete: hasEntry && m.slotComplete(name), Next: name == next, TriesLeft: e.left}
		if slot.Running && slot.Version == "" {
			slot.Version = m.version
		}
		list = append(list, slot)
	}
	m.mu.Lock()
	m.slotCache, m.slotTime = list, m.now()
	m.mu.Unlock()
	return list
}

// forget drops what is remembered about the slots, after they changed.
func (m *Manager) forget() {
	m.mu.Lock()
	m.slotCache = nil
	m.mu.Unlock()
}

// Status returns everything the update page shows.
func (m *Manager) Status() Status {
	m.mu.Lock()
	status := Status{Version: m.version, Settings: m.settings, Available: m.available, CheckError: m.checkError, Progress: m.progress, Pending: m.pending}
	if !m.lastCheck.IsZero() {
		at := m.lastCheck
		status.LastCheck = &at
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

// --- looking for a release --------------------------------------------------

func (m *Manager) fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "TitanOS/"+m.version)
	res, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s: larger than expected", url)
	}
	return data, nil
}

var (
	assetName  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,120}$`)
	hexSHA256  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	feedTag    = regexp.MustCompile(`/releases/tag/([A-Za-z0-9][A-Za-z0-9._+-]{0,120})"`)
	cmdlineSet = regexp.MustCompile(`^[\x21-\x7e]+( [\x21-\x7e]+)*$`)
)

// parseManifest reads a manifest and refuses whatever TitanOS would not
// know how to install.
func parseManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, fmt.Errorf("%w: %v", ErrManifest, err)
	}
	bad := func(what string) (Manifest, error) { return Manifest{}, fmt.Errorf("%w: %s", ErrManifest, what) }
	if _, ok := parseVersion(manifest.Version); !ok {
		return bad("version")
	}
	if manifest.MinVersion != "" {
		if _, ok := parseVersion(manifest.MinVersion); !ok {
			return bad("minimum version")
		}
	}
	if !assetName.MatchString(manifest.Tag) {
		return bad("tag")
	}
	if manifest.Channel != ChannelStable && manifest.Channel != ChannelBeta {
		return bad("channel")
	}
	if !cmdlineSet.MatchString(manifest.Cmdline) || !strings.Contains(manifest.Cmdline, "live-media-path=/titanos/@SLOT@") || len(manifest.Cmdline) > 1000 {
		return bad("kernel command line")
	}
	if len(manifest.Changelog) > 100_000 {
		return bad("changelog")
	}
	var names []string
	for _, file := range manifest.Files {
		if !slices.Contains(slotFiles, file.Name) || slices.Contains(names, file.Name) || !assetName.MatchString(file.Asset) || !hexSHA256.MatchString(file.SHA256) || file.Size <= 0 || file.Size > maxFile {
			return bad("file " + file.Name)
		}
		names = append(names, file.Name)
	}
	if len(names) != len(slotFiles) {
		return bad("files")
	}
	return manifest, nil
}

// latest fetches and verifies the manifest of the newest release of the channel.
func (m *Manager) latest(ctx context.Context, channel string) (Manifest, error) {
	keys := minisign.LoadKeys(m.keyDir)
	if len(keys) == 0 {
		return Manifest{}, ErrNoKey
	}
	source := m.source()
	base := source + "/releases/latest/download/"
	if channel == ChannelBeta {
		// "latest" leaves out pre-releases; the feed lists every release,
		// the newest first.
		feed, err := m.fetch(ctx, source+"/releases.atom", 2<<20)
		if err != nil {
			return Manifest{}, err
		}
		match := feedTag.FindSubmatch(feed)
		if match == nil {
			return Manifest{}, fmt.Errorf("%w: no release found", ErrNothing)
		}
		base = source + "/releases/download/" + string(match[1]) + "/"
	}
	data, err := m.fetch(ctx, base+"manifest.json", maxManifest)
	if err != nil {
		return Manifest{}, err
	}
	signature, err := m.fetch(ctx, base+"manifest.json.minisig", 4096)
	if err != nil {
		return Manifest{}, err
	}
	if minisign.Verify(keys, data, string(signature)) != nil {
		return Manifest{}, ErrSignature
	}
	return parseManifest(data)
}

// Check looks for a release that is newer than what is running.
func (m *Manager) Check(ctx context.Context) error {
	m.mu.Lock()
	channel := m.settings.Channel
	m.mu.Unlock()
	manifest, err := m.latest(ctx, channel)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastCheck, m.checkError, m.available = m.now(), "", nil
	if err != nil {
		m.checkError = err.Error()
		return err
	}
	found, _ := parseVersion(manifest.Version)
	// A build that is not a release is older than any release.
	if running, ok := parseVersion(m.version); ok && found.compare(running) <= 0 {
		return nil
	}
	m.available = &manifest
	if m.announced != manifest.Version && m.OnEvent != nil {
		m.announced = manifest.Version
		go m.OnEvent("update_available", manifest.Version)
	}
	return nil
}

// --- installing -------------------------------------------------------------

func (m *Manager) setProgress(change func(*Progress)) {
	m.mu.Lock()
	change(&m.progress)
	m.mu.Unlock()
}

// Install starts installing the release that Check found. It returns at
// once; Status tells how far it is.
func (m *Manager) Install(ctx context.Context) error {
	if m.Disabled {
		return ErrDisabled
	}
	m.mu.Lock()
	manifest, pending, phase := m.available, m.pending, m.progress.Phase
	m.mu.Unlock()
	switch {
	case phase == "download" || phase == "switch" || pending != nil:
		return ErrBusy
	case manifest == nil:
		return ErrNothing
	}
	if manifest.MinVersion != "" {
		minimum, _ := parseVersion(manifest.MinVersion)
		if running, ok := parseVersion(m.version); ok && running.compare(minimum) < 0 {
			return fmt.Errorf("%w: update to %s first", ErrTooOld, manifest.MinVersion)
		}
	}
	entries := m.readEntries()
	if e, ok := entries[m.slot]; ok && e.left != nil {
		return ErrUnconfirmed
	}
	// The stick must hold the new system next to the running one.
	target := other(m.slot)
	var room unix.Statfs_t
	if err := unix.Statfs(m.bootDir, &room); err != nil {
		return err
	}
	free := int64(room.Bavail) * int64(room.Bsize)
	for _, name := range slotFiles {
		if info, err := os.Stat(filepath.Join(m.bootDir, "titanos", target, name)); err == nil {
			free += info.Size() // goes away first
		}
	}
	if free < manifest.Size()+(8<<20) {
		return fmt.Errorf("%w: %d MiB needed, %d MiB free", ErrNoSpace, manifest.Size()>>20, free>>20)
	}
	if !m.work.TryLock() {
		return ErrBusy
	}
	m.setProgress(func(p *Progress) { *p = Progress{Phase: "download"} })
	go func() {
		defer m.work.Unlock()
		err := m.install(context.WithoutCancel(ctx), *manifest, target)
		m.forget()
		m.setProgress(func(p *Progress) {
			if err != nil {
				*p = Progress{Phase: "failed", Error: err.Error()}
			} else {
				*p = Progress{Phase: "done", Percent: 100}
			}
		})
		if err != nil {
			slog.Error("update not installed", "version", manifest.Version, "err", err)
		}
	}()
	return nil
}

func (m *Manager) install(ctx context.Context, manifest Manifest, target string) error {
	slog.Info("installing update", "version", manifest.Version, "slot", target)
	dir := filepath.Join(m.bootDir, "titanos", target)
	// What is in the slot can no longer be started while it is replaced.
	m.removeEntry(target)
	for _, name := range append(slices.Clone(slotFiles), "version") {
		os.Remove(filepath.Join(dir, name))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	base := m.source() + "/releases/download/" + manifest.Tag + "/"
	total, done := manifest.Size(), int64(0)
	for _, file := range manifest.Files {
		before := done
		err := m.download(ctx, base+file.Asset, filepath.Join(dir, file.Name), file, func(n int64, speed uint64) {
			m.setProgress(func(p *Progress) {
				p.File, p.Percent, p.SpeedBytesPerSecond = file.Name, 100*float64(before+n)/float64(total), speed
			})
		})
		if err != nil {
			return fmt.Errorf("%s: %w", file.Name, err)
		}
		done += file.Size
	}
	m.setProgress(func(p *Progress) { *p = Progress{Phase: "switch", Percent: 100} })
	if err := writeFile(filepath.Join(dir, "version"), []byte(manifest.Version+"\n")); err != nil {
		return err
	}
	content := fmt.Sprintf("title    TitanOS\nversion  %s\nsort-key %s\nlinux    /titanos/%s/vmlinuz\ninitrd   /titanos/%s/initrd.img\noptions  %s\n",
		manifest.Version, keyPreferred, target, target, strings.ReplaceAll(manifest.Cmdline, "@SLOT@", target))
	return m.switchTo(target, content, Pending{Kind: "update", Version: manifest.Version})
}

// switchTo makes a slot the one that starts next, on trial, with the
// running one as the fallback.
func (m *Manager) switchTo(target, content string, pending Pending) error {
	if err := m.unpinLoader(); err != nil {
		return err
	}
	// The fallback first: should the power fail in between, the stick
	// still starts what is running now.
	if running, ok := m.readEntries()[m.slot]; ok {
		if err := m.putEntry(m.slot, withSortKey(running.content, keyFallback), false); err != nil {
			return err
		}
	}
	pending.Slot, pending.From, pending.Since, pending.BootID = target, m.version, m.now(), m.bootID
	if m.Mounted != nil {
		pending.Volumes = m.Mounted()
	}
	if err := m.store.WriteJSON(pendingFile, pending); err != nil {
		return err
	}
	if err := m.putEntry(target, withSortKey(content, keyPreferred), true); err != nil {
		m.store.Remove(pendingFile)
		return err
	}
	// Everything has to be on the stick before someone restarts.
	unix.Sync()
	m.mu.Lock()
	m.pending, m.available, m.slotCache = &pending, nil, nil
	m.mu.Unlock()
	slog.Info("next start switches the system", "kind", pending.Kind, "version", pending.Version, "slot", target)
	return nil
}

// Rollback makes the other slot the one that starts next. Like an update
// it is on trial: if that system does not come up, this one starts again.
func (m *Manager) Rollback() error {
	if m.Disabled {
		return ErrDisabled
	}
	if !m.work.TryLock() {
		return ErrBusy
	}
	defer m.work.Unlock()
	m.mu.Lock()
	pending := m.pending
	m.mu.Unlock()
	if pending != nil {
		return ErrBusy
	}
	target := other(m.slot)
	entries := m.readEntries()
	e, ok := entries[target]
	if !ok || !m.slotComplete(target) || e.left != nil && *e.left == 0 {
		return ErrNothing
	}
	if running, ok := entries[m.slot]; ok && running.left != nil {
		return ErrUnconfirmed
	}
	return m.switchTo(target, e.content, Pending{Kind: "rollback", Version: m.slotVersion(target)})
}

// Cancel takes back a pending switch: the running system stays the one
// that starts next.
func (m *Manager) Cancel() error {
	if m.Disabled {
		return ErrDisabled
	}
	if !m.work.TryLock() {
		return ErrBusy
	}
	defer m.work.Unlock()
	m.mu.Lock()
	pending := m.pending
	m.mu.Unlock()
	if pending == nil || pending.BootID != m.bootID {
		return ErrNothing
	}
	entries := m.readEntries()
	if running, ok := entries[m.slot]; ok {
		if err := m.putEntry(m.slot, withSortKey(running.content, keyPreferred), false); err != nil {
			return err
		}
	}
	if target, ok := entries[pending.Slot]; ok {
		if err := m.putEntry(pending.Slot, withSortKey(target.content, keyFallback), false); err != nil {
			return err
		}
	}
	if err := m.store.Remove(pendingFile); err != nil {
		return err
	}
	unix.Sync()
	m.mu.Lock()
	m.pending, m.progress, m.slotCache = nil, Progress{}, nil
	m.mu.Unlock()
	return nil
}

// download fetches one file of a release into the slot, checking size and
// checksum. A download that broke off is taken up where it stopped.
func (m *Manager) download(ctx context.Context, url, dest string, file File, progress func(done int64, speed uint64)) error {
	part := dest + ".part"
	var last error
	wrong := 0
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * m.retryPause):
			}
		}
		done, err := m.downloadOnce(ctx, url, part, file, progress)
		if err == nil {
			return os.Rename(part, dest)
		}
		last = err
		slog.Warn("download interrupted", "file", file.Name, "have", done, "err", err)
		if errors.Is(err, errChecksum) {
			// What is there is wrong. Once more from the start, in case
			// it was damaged on the way; twice wrong is the release.
			os.Remove(part)
			if wrong++; wrong == 2 {
				break
			}
		}
	}
	os.Remove(part)
	return last
}

var errChecksum = errors.New("the checksum does not match the manifest")

func (m *Manager) downloadOnce(ctx context.Context, url, part string, file File, progress func(done int64, speed uint64)) (int64, error) {
	f, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	have := info.Size()
	if have > file.Size {
		return have, errChecksum
	}
	// What is there already counts towards the checksum.
	hash := sha256.New()
	if _, err := io.CopyN(hash, f, have); err != nil {
		return have, err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// A connection that stands still is given up on.
	watchdog := time.AfterFunc(m.stall, cancel)
	defer watchdog.Stop()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return have, err
	}
	req.Header.Set("User-Agent", "TitanOS/"+m.version)
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	res, err := m.client.Do(req)
	if err != nil {
		return have, err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusOK && have > 0:
		// The server sends everything again.
		if err := f.Truncate(0); err != nil {
			return have, err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return have, err
		}
		have = 0
		hash.Reset()
	case res.StatusCode == http.StatusOK, res.StatusCode == http.StatusPartialContent && have > 0:
	default:
		return have, fmt.Errorf("%s: %s", url, res.Status)
	}

	buffer := make([]byte, 1<<20)
	mark, markDone := time.Now(), have
	for {
		n, readErr := res.Body.Read(buffer)
		if n > 0 {
			watchdog.Reset(m.stall)
			if have+int64(n) > file.Size {
				return have + int64(n), errChecksum // longer than the manifest says
			}
			if _, err := f.Write(buffer[:n]); err != nil {
				return have, err
			}
			hash.Write(buffer[:n])
			have += int64(n)
			if now := time.Now(); now.Sub(mark) >= time.Second {
				progress(have, uint64(float64(have-markDone)/now.Sub(mark).Seconds()))
				mark, markDone = now, have
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return have, readErr
		}
	}
	if have < file.Size {
		return have, io.ErrUnexpectedEOF
	}
	if hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
		return have, errChecksum
	}
	progress(have, 0)
	return have, f.Sync()
}

// Run looks for updates once a day and installs them at the set time if
// that is wanted, until ctx is done.
func (m *Manager) Run(ctx context.Context) {
	if m.Disabled {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	var checked time.Time
	for minute := 0; ; minute++ {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		m.Confirm()
		// The first look a while after the start, when the network is up.
		if minute >= 5 && m.now().Sub(checked) >= 24*time.Hour {
			checked = m.now()
			if err := m.Check(ctx); err != nil && !errors.Is(err, ErrNoKey) {
				slog.Warn("looking for updates failed", "err", err)
			}
		}
		m.mu.Lock()
		settings, available, phase, pending := m.settings, m.available, m.progress.Phase, m.pending
		m.mu.Unlock()
		if !settings.AutoInstall || m.now().Format("15:04") != settings.InstallTime {
			continue
		}
		switch {
		case available != nil && pending == nil && phase != "download" && phase != "switch":
			if err := m.Install(ctx); err != nil {
				slog.Warn("scheduled update not started", "err", err)
				continue
			}
			// Wait for it, then restart within the same night.
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case <-time.After(5 * time.Second):
					}
					m.mu.Lock()
					phase := m.progress.Phase
					m.mu.Unlock()
					if phase == "done" && m.Reboot != nil {
						slog.Info("restarting for the scheduled update")
						if err := m.Reboot(); err != nil {
							slog.Error("restart for the update failed", "err", err)
						}
						return
					}
					if phase != "download" && phase != "switch" {
						return
					}
				}
			}()
		}
	}
}
