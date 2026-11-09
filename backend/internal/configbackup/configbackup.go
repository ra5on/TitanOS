// Package configbackup keeps copies of /config on a volume and puts one
// back onto a new stick.
//
// The stick is the only place where the configuration lives, and sticks
// wear out. A backup is one tar.gz file in /mnt/<volume>/.titanos/backups;
// it holds everything in /config, including password hashes and secrets,
// so the folder is for root only. The keys of the TitanOS certificate
// authority and of app windows are left out (see leftOut).
package configbackup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/retention"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

const (
	settingsFile = "system/backup.json"
	// folder is where the backups lie on a volume.
	folder       = ".titanos/backups"
	manifestName = "titanos-backup.json"
	format       = 1

	defaultKeep = 14
	// settle is how long the configuration must have been left alone
	// before a change is saved: changes tend to come in groups.
	settle = 5 * time.Minute
	// noVolumeAfter is how long saving may wait for its volume before
	// someone is told: the stick may die meanwhile.
	noVolumeAfter = 24 * time.Hour
)

// Limits for what is taken in when restoring. /config is small: a backup far
// beyond this is not one. A backup is not made beyond them either.
var (
	maxArchive int64 = 64 << 20
	maxContent int64 = 256 << 20
	maxFiles         = 20000
)

var (
	// ErrInvalid means a setting is not acceptable.
	ErrInvalid = errors.New("configbackup: invalid setting")
	// ErrNotLoaded means /config could not be read.
	ErrNotLoaded = errors.New("configbackup: settings not loaded from /config")
	// ErrDisabled means titand runs in development mode.
	ErrDisabled = errors.New("configbackup: not available in development mode")
	// ErrNoVolume means there is no mounted volume to save to.
	ErrNoVolume = errors.New("configbackup: no mounted volume to save to")
	// ErrNotFound means there is no such backup.
	ErrNotFound = errors.New("configbackup: no such backup")
	// ErrNotABackup means a file is not a backup of /config, or damaged.
	ErrNotABackup = errors.New("configbackup: not a backup of the configuration")
	// ErrTooLarge means a file is larger than any backup would be.
	ErrTooLarge = errors.New("configbackup: too large for a backup of the configuration")

	backupName = regexp.MustCompile(`^config-(\d{8}T\d{6}Z)-([0-9a-f]{12})\.tar\.gz$`)
	// Btrfs may also sit on a whole disk, without a partition.
	deviceName = regexp.MustCompile(`^(?:md[0-9]+|sd[a-z]+[0-9]*|vd[a-z]+[0-9]*|nvme[0-9]+n[0-9]+(?:p[0-9]+)?)$`)
	release    = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)`)

	// skipped are files of a backup that describe the moment it was made,
	// not the configuration: an update waiting for its restart would be
	// reported as failed or installed on the new stick.
	skipped = []string{"system/update-pending.json", ".accounts.lock"}
)

// leftOut tells whether a file of /config stays out of every backup and
// is never restored: the TitanOS certificate authority and the key of app
// windows (config.NotBackedUp). After a restore a new authority is made on
// first need (devices install it again) and app windows ask for a new pass.
func leftOut(name string) bool { return config.LeftOutOfBackups(name) }

const timeLayout = "20060102T150405Z"

// Settings is /config/system/backup.json.
type Settings struct {
	Enabled bool `json:"enabled"`
	// Volume to save to; empty for the first one that is mounted.
	Volume string `json:"volume"`
	// Keep is how many backups stay; older ones are removed.
	Keep     int    `json:"keep"`
	Schedule string `json:"schedule"`
	Time     string `json:"time"`
	Weekday  int    `json:"weekday"`
	// Encrypt seals new backups with the password in passwordFile
	// (crypt.go). Older versions ignore it and save without.
	Encrypt bool `json:"encrypt,omitempty"`
	// Versions picks which backups stay by day, week and month (0.41.0,
	// see internal/retention); Keep stays the count older versions use.
	Versions *retention.Keep `json:"versions,omitempty"`
}

// Manifest is the first file in a backup and says what it is.
type Manifest struct {
	Format   int       `json:"format"`
	Time     time.Time `json:"time"`
	Hostname string    `json:"hostname"`
	Version  string    `json:"version"`
}

// Backup is one saved state of /config.
type Backup struct {
	Name string `json:"name"`
	// Volume it lies on. For a backup found on a new stick, where no
	// volume is known yet, Device is the array instead (md127) and Label
	// the name of its file system.
	Volume    string    `json:"volume,omitempty"`
	Device    string    `json:"device,omitempty"`
	Label     string    `json:"label,omitempty"`
	Time      time.Time `json:"time"`
	SizeBytes int64     `json:"sizeBytes"`
	// Encrypted backups need their password to be restored.
	Encrypted bool   `json:"encrypted,omitempty"`
	Hostname  string `json:"hostname"`
	Version   string `json:"version"`
	// Newer says the backup was made by a newer TitanOS than the one
	// running: the stick should be updated to that version first, or
	// settings that only the newer version knows may be lost.
	Newer bool `json:"newer,omitempty"`
}

// State is what the settings page shows.
type State struct {
	Settings Settings `json:"settings"`
	// Target is the volume the next backup goes to; empty if none is mounted.
	Target string `json:"target"`
	// LastCheck is when the configuration was last compared with the
	// newest backup; LastError what went wrong then, if anything.
	LastCheck *time.Time `json:"lastCheck"`
	LastError string     `json:"lastError"`
	Backups   []Backup   `json:"backups"`
	Next      *time.Time `json:"next"`
}

// Manager makes, lists and restores the backups.
type Manager struct {
	store   *config.Store
	runner  run.Runner
	version string
	// mountRoot is where volumes are mounted, scanRoot where arrays are
	// looked into on a new stick, sysBlock where the kernel shows them.
	mountRoot, scanRoot, sysBlock string
	now                           func() time.Time

	// Disabled refuses to write backups and to restore: in development
	// mode titand runs on the developer's own machine.
	Disabled bool
	// OnResult is told how each attempt to save went (nil for success).
	OnResult func(volume string, err error)
	// OnNoVolume is told once when nothing could be saved for noVolumeAfter
	// because the volume to save to is not mounted; volume is the chosen
	// one, empty for "the first mounted".
	OnNoVolume func(volume string)

	// work serializes everything that touches backups.
	work sync.Mutex

	mu        sync.Mutex
	loaded    bool
	settings  Settings
	lastCheck time.Time
	lastError string
}

// NewManager returns a manager working on the real system.
func NewManager(store *config.Store, version string) *Manager {
	return &Manager{
		store: store, runner: run.Exec{}, version: version,
		mountRoot: "/mnt", scanRoot: "/run/titanos-restore", sysBlock: "/sys/block", now: time.Now,
		settings: Settings{Enabled: true, Keep: defaultKeep},
	}
}

// Load reads the settings from /config. Without a file backups are on.
func (m *Manager) Load() error {
	settings := Settings{Enabled: true, Keep: defaultKeep}
	err := m.store.ReadJSON(settingsFile, &settings)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if settings.Keep < 1 {
		settings.Keep = defaultKeep
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings, m.loaded = settings, true
	return nil
}

// SetSettings stores new settings. known are the volumes that exist.
func (m *Manager) SetSettings(settings Settings, known []string) error {
	if err := CheckSettings(settings, known); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	if err := m.store.WriteJSON(settingsFile, settings); err != nil {
		return err
	}
	m.settings = settings
	return nil
}

// CheckSettings reports what SetSettings would refuse, without storing
// anything: callers that change more than the settings (the password)
// check first, so that a refused request changes nothing.
func CheckSettings(settings Settings, known []string) error {
	if err := checkSchedule(settings); err != nil {
		return err
	}
	if settings.Keep < 1 || settings.Keep > 100 {
		return fmt.Errorf("%w: keep", ErrInvalid)
	}
	if v := settings.Versions; v != nil && (v.Mode != "" || v.WhenFull != "" || retention.Check(*v) != nil) {
		return fmt.Errorf("%w: versions", ErrInvalid)
	}
	if settings.Volume != "" && !slices.Contains(known, settings.Volume) {
		return fmt.Errorf("%w: volume", ErrInvalid)
	}
	return nil
}

func (m *Manager) current() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings
}

// target picks the volume to save to among the mounted ones.
func target(settings Settings, mounted []string) string {
	if settings.Volume != "" {
		if slices.Contains(mounted, settings.Volume) {
			return settings.Volume
		}
		return ""
	}
	if len(mounted) == 0 {
		return ""
	}
	sorted := slices.Clone(mounted)
	sort.Strings(sorted)
	return sorted[0]
}

// State returns settings, the last result and the backups on the mounted volumes.
func (m *Manager) State(mounted []string) State {
	m.mu.Lock()
	state := State{Settings: m.settings, LastError: m.lastError}
	if !m.lastCheck.IsZero() {
		at := m.lastCheck
		state.LastCheck = &at
	}
	m.mu.Unlock()
	state.Next = nextScheduled(state.Settings, m.now())
	state.Target = target(state.Settings, mounted)
	state.Backups = m.List(mounted)
	return state
}

// List returns the backups on the mounted volumes, the newest first.
func (m *Manager) List(mounted []string) []Backup {
	list := []Backup{}
	for _, volume := range mounted {
		for _, backup := range readFolder(filepath.Join(m.mountRoot, volume, folder)) {
			backup.Volume = volume
			list = append(list, backup)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Time.After(list[j].Time) })
	return list
}

// readFolder lists the backups in a directory. Files that only look like
// backups are left out.
func readFolder(dir string) []Backup {
	entries, _ := os.ReadDir(dir)
	var list []Backup
	for _, entry := range entries {
		match := backupName.FindStringSubmatch(entry.Name())
		info, err := entry.Info()
		if match == nil || err != nil || !info.Mode().IsRegular() {
			continue
		}
		manifest, sealed, err := readManifestOf(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		list = append(list, Backup{Name: entry.Name(), Time: manifest.Time, SizeBytes: info.Size(), Encrypted: sealed, Hostname: manifest.Hostname, Version: manifest.Version})
	}
	return list
}

// readManifestOf reads the manifest of a backup and says whether it is
// encrypted.
func readManifestOf(file string) (Manifest, bool, error) {
	f, err := os.Open(file)
	if err != nil {
		return Manifest{}, false, err
	}
	defer f.Close()
	head := make([]byte, len(encMagic))
	if n, _ := io.ReadFull(f, head); n == len(head) && encrypted(head) {
		_, _ = f.Seek(0, io.SeekStart) // the offset was just read from the same file; the copy checks what it reads
		manifest, err := sealedManifest(f)
		return manifest, true, err
	}
	_, _ = f.Seek(0, io.SeekStart) // the offset was just read from the same file; the copy checks what it reads
	manifest, err := plainManifest(f)
	return manifest, false, err
}

func plainManifest(f io.Reader) (Manifest, error) {
	unzip, err := gzip.NewReader(f)
	if err != nil {
		return Manifest{}, ErrNotABackup
	}
	return manifestOf(tar.NewReader(unzip))
}

func manifestOf(archive *tar.Reader) (Manifest, error) {
	header, err := archive.Next()
	if err != nil || header.Name != manifestName || header.Typeflag != tar.TypeReg || header.Size > 4096 {
		return Manifest{}, ErrNotABackup
	}
	var manifest Manifest
	if err := json.NewDecoder(archive).Decode(&manifest); err != nil || manifest.Format != format {
		return Manifest{}, ErrNotABackup
	}
	return manifest, nil
}

// file is one file of /config that goes into a backup.
type file struct {
	name string // slash separated, relative to /config
	path string
	info fs.FileInfo
}

// configFiles lists what is in /config, sorted by name.
func (m *Manager) configFiles() ([]file, error) {
	// /config is a link to the stick; walking does not follow links.
	root, err := filepath.EvalSymlinks(m.store.Dir())
	if err != nil {
		return nil, err
	}
	var files []file
	err = filepath.WalkDir(root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Leftovers of a write that was cut short.
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if leftOut(name) {
			return nil
		}
		files = append(files, file{name: name, path: p, info: info})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, err
}

// Create saves /config to the target volume unless the newest backup
// there holds the same content already. It returns the name of the backup
// that now holds the current state.
func (m *Manager) Create(mounted []string) (string, error) {
	if m.Disabled {
		return "", ErrDisabled
	}
	m.work.Lock()
	defer m.work.Unlock()
	volume := target(m.current(), mounted)
	name, err := m.create(volume)
	m.mu.Lock()
	m.lastCheck, m.lastError = m.now(), ""
	if err != nil {
		m.lastError = err.Error()
	}
	m.mu.Unlock()
	if m.OnResult != nil && !errors.Is(err, ErrNoVolume) {
		m.OnResult(volume, err)
	}
	return name, err
}

func (m *Manager) create(volume string) (string, error) {
	if volume == "" {
		return "", ErrNoVolume
	}
	files, err := m.configFiles()
	if err != nil {
		return "", err
	}
	// A backup that restoring would refuse is no backup: say so now, not
	// when the stick has died.
	var content int64
	for _, f := range files {
		content += f.info.Size()
	}
	if len(files) > maxFiles || content > maxContent {
		return "", fmt.Errorf("%w: %d files, %d MiB", ErrTooLarge, len(files), content>>20)
	}
	dir := filepath.Join(m.mountRoot, volume, folder)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}

	// What a backup that was cut short left behind.
	if stale, _ := filepath.Glob(filepath.Join(dir, ".config-*.part")); len(stale) > 0 {
		for _, name := range stale {
			os.Remove(name)
		}
	}

	part, err := os.CreateTemp(dir, ".config-*.part")
	if err != nil {
		return "", err
	}
	defer os.Remove(part.Name()) // no-op once renamed
	defer part.Close()

	now := m.now().UTC().Truncate(time.Second)
	hostname, _ := os.Hostname()
	manifest, _ := json.MarshalIndent(Manifest{Format: format, Time: now, Hostname: hostname, Version: m.version}, "", "  ")

	// The digest covers names and content, not times: a backup is new
	// only if something in it is.
	digest := sha256.New()
	encrypt := m.current().Encrypt
	var password string
	if encrypt {
		secret, err := m.store.Read(passwordFile)
		if err != nil || len(secret) == 0 {
			return "", ErrPasswordRequired
		}
		password = string(secret)
		digest.Write([]byte(encMagic))
	}
	// An encrypted backup is put together in RAM (it is bounded by
	// maxArchive and was read whole for sealing anyway): written to the
	// volume first, the unencrypted archive would stay in freed blocks and
	// in snapshots of the volume, for whoever holds a disk later.
	var plain bytes.Buffer
	var sink io.Writer = part
	if encrypt {
		sink = &plain
	}
	zip := gzip.NewWriter(sink)
	archive := tar.NewWriter(zip)
	if err := archive.WriteHeader(&tar.Header{Name: manifestName, Mode: 0o600, Size: int64(len(manifest)), ModTime: now, Typeflag: tar.TypeReg}); err != nil {
		return "", err
	}
	if _, err := archive.Write(manifest); err != nil {
		return "", err
	}
	for _, f := range files {
		data, err := os.ReadFile(f.path)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(digest, "%s\x00%d\x00", f.name, len(data))
		digest.Write(data)
		if err := archive.WriteHeader(&tar.Header{Name: f.name, Mode: 0o600, Size: int64(len(data)), ModTime: f.info.ModTime(), Typeflag: tar.TypeReg}); err != nil {
			return "", err
		}
		if _, err := archive.Write(data); err != nil {
			return "", err
		}
	}
	if err := archive.Close(); err != nil {
		return "", err
	}
	if err := zip.Close(); err != nil {
		return "", err
	}
	if encrypt {
		if int64(plain.Len()) > maxArchive {
			return "", fmt.Errorf("%w: archive of %d MiB", ErrTooLarge, plain.Len()>>20)
		}
		sealed, err := seal(manifest, plain.Bytes(), password)
		if err != nil {
			return "", err
		}
		if _, err := part.Write(sealed); err != nil {
			return "", err
		}
	}
	if info, err := part.Stat(); err != nil {
		return "", err
	} else if info.Size() > maxArchive {
		return "", fmt.Errorf("%w: archive of %d MiB", ErrTooLarge, info.Size()>>20)
	}
	short := hex.EncodeToString(digest.Sum(nil))[:12]

	existing := readFolder(dir)
	sort.Slice(existing, func(i, j int) bool { return existing[i].Time.After(existing[j].Time) })
	if len(existing) > 0 && backupName.FindStringSubmatch(existing[0].Name)[2] == short {
		return existing[0].Name, nil
	}

	if err := part.Sync(); err != nil {
		return "", err
	}
	if err := part.Close(); err != nil {
		return "", err
	}
	name := "config-" + now.Format(timeLayout) + "-" + short + ".tar.gz"
	if err := os.Rename(part.Name(), filepath.Join(dir, name)); err != nil {
		return "", err
	}
	// The new name has to survive a power cut too: it is what a new
	// stick looks for.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync() // the data are written; the sync only hurries them to disk
		d.Close()
	}
	slog.Info("configuration saved", "volume", volume, "backup", name, "files", len(files))

	// The oldest go once there are more than wanted.
	settings := m.current()
	if settings.Versions != nil {
		// The new one and the older ones, by the rules.
		versions := []retention.Version{{ID: name, Time: m.now().In(time.Local)}}
		for _, old := range existing {
			versions = append(versions, retention.Version{ID: old.Name, Time: old.Time.In(time.Local)})
		}
		stays := retention.Select(*settings.Versions, versions, m.now().In(time.Local))
		for _, old := range existing {
			if !stays[old.Name] {
				os.Remove(filepath.Join(dir, old.Name))
			}
		}
		return name, nil
	}
	for i, old := range existing {
		if i+1 >= settings.Keep {
			os.Remove(filepath.Join(dir, old.Name))
		}
	}
	return name, nil
}

// Open opens a backup on a mounted volume for download.
func (m *Manager) Open(mounted []string, volume, name string) (*os.File, error) {
	if !slices.Contains(mounted, volume) || !backupName.MatchString(name) {
		return nil, ErrNotFound
	}
	f, err := os.Open(filepath.Join(m.mountRoot, volume, folder, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return f, err
}

// Run saves the configuration once after the start and then whenever it
// changed, until ctx is done. It asks the store whether something changed;
// the stick is only read when a backup is made.
func (m *Manager) Run(ctx context.Context, mounted func() []string) {
	if m.Disabled {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	var (
		saved        uint64 // the store's count of changes at the last backup
		done         bool
		changedSince time.Time
		retryAfter   time.Time
		planned      *time.Time
		previous     Settings
		noVolume     outage
	)
	for tick := 0; ; tick++ {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		settings := m.current()
		if settings != previous {
			planned = nextScheduled(settings, m.now())
			previous = settings
		}
		// The first minutes belong to the start of the NAS.
		if !settings.Enabled || tick < 2 || m.now().Before(retryAfter) {
			continue
		}
		changes := m.store.Changes()
		scheduled := planned != nil && !m.now().Before(*planned)
		due := !done || scheduled
		if done && changes != saved {
			if changedSince.IsZero() {
				changedSince = m.now()
			}
			due = due || m.now().Sub(changedSince) >= settle
		}
		if !due {
			continue
		}
		_, err := m.Create(mounted())
		switch {
		case errors.Is(err, ErrNoVolume):
			// Nothing to save to yet; the next minute may bring a volume.
			// One that does not come is worth a word: backups stopped.
			if noVolume.long(m.now()) && m.OnNoVolume != nil {
				slog.Warn("configuration not saved for a day: no volume", "volume", settings.Volume)
				m.OnNoVolume(settings.Volume)
			}
		case err != nil:
			noVolume = outage{}
			slog.Warn("configuration not saved", "err", err)
			retryAfter = m.now().Add(time.Hour)
		default:
			noVolume = outage{}
			saved, done, changedSince = changes, true, time.Time{}
			if scheduled {
				planned = nextScheduled(settings, m.now())
			}
		}
	}
}

// outage remembers since when saving found no volume.
type outage struct {
	since time.Time
	told  bool
}

// long notes another attempt without a volume and reports, once, that it has
// lasted noVolumeAfter.
func (o *outage) long(now time.Time) bool {
	if o.since.IsZero() {
		o.since = now
	}
	if o.told || now.Sub(o.since) < noVolumeAfter {
		return false
	}
	o.told = true
	return true
}

// --- restoring -------------------------------------------------------------

// newerRelease says whether version a is a newer release than b. Versions
// that are not releases (development builds) are never newer.
func newerRelease(a, b string) bool {
	x, y := release.FindStringSubmatch(a), release.FindStringSubmatch(b)
	if x == nil || y == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		p, _ := strconv.Atoi(x[i])
		q, _ := strconv.Atoi(y[i])
		if p != q {
			return p > q
		}
	}
	return false
}

// nextScheduled uses NAS local time and calendar days (including DST changes).
// A missed schedule after a reboot is covered by the existing startup backup.
func nextScheduled(s Settings, now time.Time) *time.Time {
	if !s.Enabled || s.Schedule == "" || checkSchedule(s) != nil {
		return nil
	}
	clock, _ := time.Parse("15:04", s.Time)
	for day := 0; day < 9; day++ {
		date := now.AddDate(0, 0, day)
		candidate := time.Date(date.Year(), date.Month(), date.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
		if candidate.After(now) && (s.Schedule == "daily" || int(candidate.Weekday()) == s.Weekday) {
			return &candidate
		}
	}
	return nil
}
func checkSchedule(s Settings) error {
	if s.Schedule == "" {
		return nil
	}
	if s.Schedule != "daily" && s.Schedule != "weekly" {
		return fmt.Errorf("%w: schedule", ErrInvalid)
	}
	if _, err := time.Parse("15:04", s.Time); err != nil || len(s.Time) != 5 || s.Weekday < 0 || s.Weekday > 6 {
		return fmt.Errorf("%w: schedule time or weekday", ErrInvalid)
	}
	return nil
}
