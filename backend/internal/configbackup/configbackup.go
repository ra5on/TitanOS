// Package configbackup keeps copies of /config on a volume and puts one
// back onto a new stick.
//
// The stick is the only place where the configuration lives, and sticks
// wear out. A backup is one tar.gz file in /mnt/<volume>/.titanos/backups;
// it holds everything in /config, including password hashes and secrets,
// so the folder is for root only.
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
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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
	if err := checkSchedule(settings); err != nil {
		return err
	}
	if settings.Keep < 1 || settings.Keep > 100 {
		return fmt.Errorf("%w: keep", ErrInvalid)
	}
	if settings.Volume != "" && !slices.Contains(known, settings.Volume) {
		return fmt.Errorf("%w: volume", ErrInvalid)
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
		f.Seek(0, io.SeekStart)
		manifest, err := sealedManifest(f)
		return manifest, true, err
	}
	f.Seek(0, io.SeekStart)
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
		files = append(files, file{name: filepath.ToSlash(rel), path: p, info: info})
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
	zip := gzip.NewWriter(part)
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
		if _, err := part.Seek(0, io.SeekStart); err != nil {
			return "", err
		}
		plain, err := io.ReadAll(io.LimitReader(part, maxArchive+1))
		if err != nil {
			return "", err
		}
		sealed, err := seal(manifest, plain, password)
		if err != nil {
			return "", err
		}
		if err := part.Truncate(0); err != nil {
			return "", err
		}
		if _, err := part.WriteAt(sealed, 0); err != nil {
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
		d.Sync()
		d.Close()
	}
	slog.Info("configuration saved", "volume", volume, "backup", name, "files", len(files))

	// The oldest go once there are more than wanted.
	keep := m.current().Keep
	for i, old := range existing {
		if i+1 >= keep {
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

// check reads a backup through and returns what it is. Nothing is written.
func check(r io.Reader) (Manifest, error) {
	unzip, err := gzip.NewReader(r)
	if err != nil {
		return Manifest{}, ErrNotABackup
	}
	archive := tar.NewReader(unzip)
	manifest, err := manifestOf(archive)
	if err != nil {
		return Manifest{}, err
	}
	var total int64
	files, accounts := 0, 0
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Manifest{}, ErrNotABackup
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg || !validName(header.Name) {
			return Manifest{}, fmt.Errorf("%w: unexpected entry %q", ErrNotABackup, header.Name)
		}
		files++
		total += header.Size
		if files > maxFiles || total > maxContent {
			return Manifest{}, ErrTooLarge
		}
		if matched, _ := path.Match("users/*/account.json", header.Name); matched {
			accounts++
		}
		if _, err := io.Copy(io.Discard, archive); err != nil {
			return Manifest{}, ErrNotABackup
		}
	}
	// Without an account nobody could log in after the restore.
	if accounts == 0 {
		return Manifest{}, fmt.Errorf("%w: it holds no account", ErrNotABackup)
	}
	return manifest, nil
}

// validName accepts the names a backup made by TitanOS can hold.
func validName(name string) bool {
	if !fs.ValidPath(name) || name == "." || name == manifestName || strings.ContainsAny(name, "\\\x00") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".tmp-") {
			return false
		}
	}
	return true
}

// restore writes the content of a backup to /config. The accounts come
// last: cut short, the stick still counts as new and can be restored again.
func (m *Manager) restore(r io.ReadSeeker, password string) (Manifest, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return Manifest{}, err
	}
	head := make([]byte, len(encMagic))
	if n, _ := io.ReadFull(r, head); n == len(head) && encrypted(head) {
		r.Seek(0, io.SeekStart)
		data, err := io.ReadAll(io.LimitReader(r, maxArchive+1))
		if err != nil {
			return Manifest{}, err
		}
		plain, err := unseal(data, password)
		if err != nil {
			return Manifest{}, err
		}
		r = bytes.NewReader(plain)
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return Manifest{}, err
	}
	manifest, err := check(r)
	if err != nil {
		return Manifest{}, err
	}
	for _, accounts := range []bool{false, true} {
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return Manifest{}, err
		}
		unzip, err := gzip.NewReader(r)
		if err != nil {
			return Manifest{}, ErrNotABackup
		}
		archive := tar.NewReader(unzip)
		for {
			header, err := archive.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return Manifest{}, ErrNotABackup
			}
			if header.Typeflag != tar.TypeReg || header.Name == manifestName || slices.Contains(skipped, header.Name) || strings.HasPrefix(header.Name, "users/") != accounts {
				continue
			}
			data, err := io.ReadAll(io.LimitReader(archive, maxContent))
			if err != nil {
				return Manifest{}, ErrNotABackup
			}
			if err := m.store.Write(header.Name, data); err != nil {
				return Manifest{}, err
			}
		}
	}
	slog.Info("configuration restored", "from", manifest.Time, "hostname", manifest.Hostname, "version", manifest.Version)
	return manifest, nil
}

// RestoreUpload restores from a backup that is being uploaded. The file is
// small and is kept in RAM while it is checked.
func (m *Manager) RestoreUpload(r io.Reader, password string) (Manifest, error) {
	if m.Disabled {
		return Manifest{}, ErrDisabled
	}
	m.work.Lock()
	defer m.work.Unlock()
	if err := os.MkdirAll(m.scanRoot, 0o700); err != nil {
		return Manifest{}, err
	}
	spool, err := os.CreateTemp(m.scanRoot, "upload-*.tar.gz")
	if err != nil {
		return Manifest{}, err
	}
	defer os.Remove(spool.Name())
	defer spool.Close()
	n, err := io.Copy(spool, io.LimitReader(r, maxArchive+1))
	if err != nil {
		return Manifest{}, err
	}
	if n > maxArchive {
		return Manifest{}, ErrTooLarge
	}
	return m.restore(spool, password)
}

// arrays lists the md arrays that are running, by device name.
func (m *Manager) arrays() []string {
	entries, _ := os.ReadDir(m.sysBlock)
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "md") && deviceName.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	return names
}

// look mounts the file system of an array read-only, without replaying
// its journal, and hands its backup folder to fn. On a new stick TitanOS
// does not know which arrays are its volumes; nothing on them is changed.
func (m *Manager) look(ctx context.Context, device string, fn func(dir, label string) error) error {
	output, err := m.runner.Run(ctx, "", "blkid", "-o", "export", "/dev/"+device)
	if err != nil {
		return err
	}
	var kind, label string
	for line := range strings.SplitSeq(output, "\n") {
		if value, ok := strings.CutPrefix(line, "TYPE="); ok {
			kind = value
		}
		if value, ok := strings.CutPrefix(line, "LABEL="); ok {
			label = value
		}
	}
	options := "ro,nodev,nosuid,noexec"
	switch kind {
	case "ext4":
		options += ",noload"
	case "xfs":
		options += ",norecovery"
	case "btrfs":
		options += ",nologreplay,subvolid=5,degraded"
	default:
		return fmt.Errorf("configbackup: %s holds no file system of a volume", device)
	}
	dir := filepath.Join(m.scanRoot, device)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if _, err := m.runner.Run(ctx, "", "mount", "-t", kind, "-o", options, "/dev/"+device, dir); err != nil {
		return err
	}
	defer func() {
		if _, err := m.runner.Run(context.WithoutCancel(ctx), "", "umount", dir); err != nil {
			slog.Warn("array not unmounted after looking for backups", "device", device, "err", err)
		}
	}()
	return fn(filepath.Join(dir, folder), label)
}

// Find looks for backups on the arrays of this machine that are not
// mounted as volumes: what a new stick needs to offer a restore.
func (m *Manager) Find(ctx context.Context) []Backup {
	if m.Disabled {
		return []Backup{}
	}
	m.work.Lock()
	defer m.work.Unlock()
	list := []Backup{}
	for _, device := range m.candidates(ctx) {
		err := m.look(ctx, device, func(dir, label string) error {
			for _, backup := range readFolder(dir) {
				backup.Device, backup.Label = device, label
				backup.Newer = newerRelease(backup.Version, m.version)
				list = append(list, backup)
			}
			return nil
		})
		if err != nil {
			slog.Info("no backups read from array", "device", device, "err", err)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Time.After(list[j].Time) })
	return list
}

// RestoreFound restores from a backup that Find reported.
func (m *Manager) RestoreFound(ctx context.Context, device, name, password string) (Manifest, error) {
	if m.Disabled {
		return Manifest{}, ErrDisabled
	}
	if !deviceName.MatchString(device) || !backupName.MatchString(name) || !slices.Contains(m.candidates(ctx), device) {
		return Manifest{}, ErrNotFound
	}
	m.work.Lock()
	defer m.work.Unlock()
	var manifest Manifest
	err := m.look(ctx, device, func(dir, label string) error {
		f, err := os.Open(filepath.Join(dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		defer f.Close()
		if info, err := f.Stat(); err != nil || info.Size() > maxArchive {
			return ErrTooLarge
		}
		manifest, err = m.restore(f, password)
		return err
	})
	return manifest, err
}

// Discover native Btrfs mirrors by filesystem UUID, scanning each mirror once.
func (m *Manager) candidates(ctx context.Context) []string {
	names := m.arrays()
	output, err := m.runner.Run(ctx, "", "blkid", "-t", "TYPE=btrfs", "-o", "export")
	if err != nil {
		return names
	}
	seen := map[string]bool{}
	for _, block := range strings.Split(output, "\n\n") {
		values := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			key, value, _ := strings.Cut(line, "=")
			values[key] = value
		}
		device := strings.TrimPrefix(values["DEVNAME"], "/dev/")
		if uuid := values["UUID"]; uuid != "" && !seen[uuid] && values["TYPE"] == "btrfs" && deviceName.MatchString(device) {
			names = append(names, device)
			seen[uuid] = true
		}
	}
	return names
}

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
