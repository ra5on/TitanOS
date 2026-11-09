package webapps

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// configFile is /config/webapps.json (optional; older titand ignore it).
const configFile = "webapps.json"

// feature is the folder below .titanos on the volume.
const feature = "webapps"

// Error codes of an app's state (field "error"; the UI translates them).
const (
	ErrCodeNoVolume       = "no_volume"
	ErrCodeVolumeMissing  = "volume_unavailable"
	ErrCodeNotInRelease   = "not_in_release"
	ErrCodeDownload       = "download_failed"
	ErrCodeArchive        = "invalid_archive"
	ErrCodeInstall        = "install_failed"
	ErrCodeReleaseVersion = "release_mismatch"
)

// Errors of the manager's actions.
var (
	ErrUnknown = errors.New("webapps: no such app")
	ErrBusy    = errors.New("webapps: the app is being installed")
	ErrNoApp   = errors.New("webapps: the app is not installed")
)

// Release gives the signed manifest and assets of a release (package
// plugins, release_assets.go), as for the image of the remote view.
type Release interface {
	ReleaseManifest(ctx context.Context, version string) ([]byte, error)
	DownloadReleaseAsset(ctx context.Context, version, asset, dest, sha256 string, size int64) error
}

// Installed is one installed app in /config/webapps.json.
type Installed struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	// Volume is where it lies; empty in development mode (temporary folder).
	Volume string `json:"volume"`
	// Icon is the app's icon file below its folder, if it has one.
	Icon string `json:"icon,omitempty"`
}

// fileState is /config/webapps.json.
type fileState struct {
	Installed []Installed `json:"installed"`
	// Pending apps are installed once a volume is mounted (first setup).
	Pending []string `json:"pending,omitempty"`
	// Offered: the notice "install office programs?" was sent or answered.
	Offered bool `json:"offered,omitempty"`
	// Volume: where the owner chose the programs to lie (PLAN 75-21); "" until
	// chosen. It goes before every other rule of [Manager.Volume].
	Volume string `json:"volume,omitempty"`
}

// Status is one app as GET /api/v1/webapps shows it.
type Status struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	Installed bool     `json:"installed"`
	State     string   `json:"state"` // missing | downloading | ready | error
	Progress  float64  `json:"progress"`
	Error     string   `json:"error,omitempty"`
	Bridge    string   `json:"bridge"`
	Types     []string `json:"types"`
	Default   bool     `json:"default"`
	Pending   bool     `json:"pending,omitempty"`
	URL       string   `json:"url,omitempty"`
	Icon      string   `json:"icon,omitempty"`
	Size      int64    `json:"size"`
	License   string   `json:"license"`
	Repo      string   `json:"repo"`
	Trusted   bool     `json:"trusted,omitempty"`
	Successor string   `json:"successor,omitempty"`
	StartNew  bool     `json:"startNew,omitempty"`
}

// job is an install in progress or its failure, in RAM.
type job struct {
	running bool
	err     string
	part    string
	size    int64
	done    bool // downloaded, unpacking
}

// Manager installs, removes and serves the web apps.
type Manager struct {
	Release Release
	// Version is the version of this titand (without "v").
	Version string
	// Dev: development mode; archives from DevDir, files in a temporary folder.
	Dev    bool
	DevDir string
	// MountRoot is /mnt; Mounted lists the mounted volumes, DockerVolume is Docker's.
	MountRoot    string
	Mounted      func() []string
	DockerVolume func() string
	// Ready says whether the NAS is set up (an owner exists): no notice before.
	Ready func() bool
	// Notify sends the notice "install office programs?" (kind
	// webapps_offer, or webapps_offer_docker with the Docker versions found
	// as subject).
	Notify func(kind, subject string)
	// Docker lists the Docker apps that are Craft apps (DetectDocker); nil: none.
	Docker func() []DockerApp
	// OfferDelay is how long after the start the notice waits.
	OfferDelay time.Duration

	store *config.Store

	mu     sync.Mutex
	state  fileState
	jobs   map[string]*job
	devDir string
	base   context.Context
	wake   chan struct{}
}

// New makes a manager with the defaults of a NAS.
func New(store *config.Store) *Manager {
	return &Manager{store: store, MountRoot: "/mnt", jobs: map[string]*job{},
		base: context.Background(), wake: make(chan struct{}, 1), OfferDelay: 2 * time.Minute}
}

// Load reads /config/webapps.json; a missing file is an empty state.
func (m *Manager) Load() error {
	var s fileState
	err := m.store.ReadJSON(configFile, &s)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err != nil {
		s = fileState{}
	}
	s = m.normalize(s)
	m.mu.Lock()
	m.state = s
	m.mu.Unlock()
	return nil
}

// configured reports whether /config/webapps.json exists (read at Load).
func (m *Manager) normalize(s fileState) fileState {
	installed := []Installed{}
	for _, in := range s.Installed {
		app, ok := Lookup(in.ID)
		if !ok || !ValidVersion(in.Version) || slices.ContainsFunc(installed, func(x Installed) bool { return x.ID == in.ID }) {
			continue
		}
		// A temporary folder of development mode is gone after a restart.
		if in.Volume == "" {
			continue
		}
		if in.Icon != "" && !validIcon(in.Icon) {
			in.Icon = ""
		}
		_ = app
		installed = append(installed, in)
	}
	var pending []string
	for _, id := range s.Pending {
		if _, ok := Lookup(id); ok && !slices.Contains(pending, id) {
			pending = append(pending, id)
		}
	}
	return fileState{Installed: installed, Pending: pending, Offered: s.Offered, Volume: s.Volume}
}

func (m *Manager) mounted() []string {
	if m.Mounted == nil {
		return nil
	}
	return m.Mounted()
}

func (m *Manager) saveLocked() error {
	return m.store.WriteJSON(configFile, m.state)
}

func (m *Manager) installedLocked(id string) (Installed, bool) {
	for _, in := range m.state.Installed {
		if in.ID == id {
			return in, true
		}
	}
	return Installed{}, false
}

// Volume is where new apps go: the volume of installed apps if mounted,
// else Docker's, else the first mounted one; "" if none.
func (m *Manager) Volume() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.volumeLocked()
}

func (m *Manager) volumeLocked() string {
	if m.Dev {
		return ""
	}
	mounted := m.mounted()
	if m.state.Volume != "" && slices.Contains(mounted, m.state.Volume) {
		return m.state.Volume
	}
	for _, in := range m.state.Installed {
		if slices.Contains(mounted, in.Volume) {
			return in.Volume
		}
	}
	if m.DockerVolume != nil {
		if v := m.DockerVolume(); v != "" && slices.Contains(mounted, v) {
			return v
		}
	}
	if len(mounted) > 0 {
		return mounted[0]
	}
	return ""
}

// List reports every catalog app. It reads no disk: a mounted volume with
// the app's record counts as ready.
func (m *Manager) List() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	mounted := m.mounted()
	var out []Status
	for _, app := range catalog {
		st := Status{ID: app.ID, Name: app.Name, Version: app.Version, State: "missing", Bridge: app.Bridge,
			Types: slices.Clone(app.Types), Default: app.Default, Size: app.Size, License: app.License, Repo: app.Repo, Trusted: app.Trusted, Successor: app.Successor, StartNew: app.StartNew,
			Pending: slices.Contains(m.state.Pending, app.ID)}
		if in, ok := m.installedLocked(app.ID); ok {
			st.Installed, st.Version = true, in.Version
			if m.Dev || slices.Contains(mounted, in.Volume) {
				st.State, st.Progress = "ready", 1
				st.URL = "/webapps/" + app.ID + "/" + in.Version + "/"
				if in.Icon != "" {
					st.Icon = st.URL + in.Icon
				}
			} else {
				st.State, st.Error = "error", ErrCodeVolumeMissing
			}
		}
		if j := m.jobs[app.ID]; j != nil {
			switch {
			case j.running:
				st.State, st.Error = "downloading", ""
				st.Progress = 0
				if j.done {
					st.Progress = 1
				} else if j.part != "" && j.size > 0 {
					if info, err := os.Stat(j.part); err == nil {
						st.Progress = min(1, float64(info.Size())/float64(j.size))
					}
				}
			case j.err != "" && !st.Installed:
				st.State, st.Error = "error", j.err
			case j.err != "":
				// An update that failed leaves the installed version; keep it usable.
				st.Error = j.err
			}
		}
		out = append(out, st)
	}
	return out
}

// Install starts installing id (in the background).
func (m *Manager) Install(id string) error {
	if _, ok := Lookup(id); !ok {
		return ErrUnknown
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startLocked(id)
}

func (m *Manager) startLocked(id string) error {
	if j := m.jobs[id]; j != nil && j.running {
		return nil
	}
	m.jobs[id] = &job{running: true}
	ctx := m.base
	go m.run(ctx, id)
	return nil
}

// InstallDefaults installs the given apps (nil: the basic set). Without a
// mounted volume they wait in "pending" until one is. An empty list only
// records that the offer was answered.
func (m *Manager) InstallDefaults(ids []string) error {
	if ids == nil {
		ids = Defaults()
	}
	for _, id := range ids {
		if _, ok := Lookup(id); !ok {
			return ErrUnknown
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	changed := !m.state.Offered
	m.state.Offered = true
	for _, id := range ids {
		if _, ok := m.installedLocked(id); ok {
			continue
		}
		if m.volumeLocked() == "" && !m.Dev {
			if !slices.Contains(m.state.Pending, id) {
				m.state.Pending = append(m.state.Pending, id)
				changed = true
			}
			continue
		}
		if err := m.startLocked(id); err != nil {
			return err
		}
	}
	if changed {
		if err := m.saveLocked(); err != nil {
			return err
		}
	}
	return nil
}

// Uninstall removes an app and its files.
func (m *Manager) Uninstall(id string) error {
	if _, ok := Lookup(id); !ok {
		return ErrUnknown
	}
	m.mu.Lock()
	if j := m.jobs[id]; j != nil && j.running {
		m.mu.Unlock()
		return ErrBusy
	}
	in, ok := m.installedLocked(id)
	pending := slices.Contains(m.state.Pending, id)
	if !ok && !pending {
		m.mu.Unlock()
		return ErrNoApp
	}
	m.state.Installed = slices.DeleteFunc(m.state.Installed, func(x Installed) bool { return x.ID == id })
	m.state.Pending = slices.DeleteFunc(m.state.Pending, func(x string) bool { return x == id })
	delete(m.jobs, id)
	err := m.saveLocked()
	m.mu.Unlock()
	if err != nil {
		return err
	}
	if ok {
		if root, err := m.openPrivate(in.Volume); err == nil {
			if err := root.RemoveAll(id); err != nil {
				slog.Warn("web app files not removed", "app", id, "err", err)
			}
			root.Close()
		}
	}
	slog.Info("web app removed", "app", id)
	return nil
}

// openPrivate pins <volume>/.titanos/webapps (development mode: the
// temporary folder).
func (m *Manager) openPrivate(volume string) (*os.Root, error) {
	if m.Dev {
		m.mu.Lock()
		dir := m.devDir
		m.mu.Unlock()
		if dir == "" {
			d, err := os.MkdirTemp("", "titand-webapps-")
			if err != nil {
				return nil, err
			}
			m.mu.Lock()
			if m.devDir == "" {
				m.devDir = d
			} else {
				os.Remove(d)
			}
			dir = m.devDir
			m.mu.Unlock()
		}
		return os.OpenRoot(dir)
	}
	if volume == "" {
		return nil, volumedata.ErrUnavailable
	}
	return volumedata.Open(m.MountRoot, volume, feature, m.mounted())
}

// run installs id and records the result.
func (m *Manager) run(ctx context.Context, id string) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	in, code, err := m.install(ctx, id)
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.jobs[id]
	if j == nil {
		j = &job{}
		m.jobs[id] = j
	}
	j.running, j.part, j.done = false, "", false
	if err != nil {
		slog.Warn("web app not installed", "app", id, "err", err)
		j.err = code
		return
	}
	j.err = ""
	prev, had := m.installedLocked(id)
	m.state.Installed = slices.DeleteFunc(m.state.Installed, func(x Installed) bool { return x.ID == id })
	m.state.Installed = append(m.state.Installed, in)
	m.state.Pending = slices.DeleteFunc(m.state.Pending, func(x string) bool { return x == id })
	if err := m.saveLocked(); err != nil {
		slog.Warn("web apps not saved to /config", "err", err)
	}
	slog.Info("web app installed", "app", id, "version", in.Version, "volume", in.Volume)
	// Moved to another volume: the old copy goes once the new one serves.
	if had && prev.Volume != in.Volume && prev.Volume != "" {
		go m.removeCopy(prev.Volume, id)
	}
}

// removeCopy deletes the files of id on volume (after a move).
func (m *Manager) removeCopy(volume, id string) {
	root, err := m.openPrivate(volume)
	if err != nil {
		slog.Warn("old copy of a web app not removed", "app", id, "volume", volume, "err", err)
		return
	}
	defer root.Close()
	if err := root.RemoveAll(id); err != nil {
		slog.Warn("old copy of a web app not removed", "app", id, "volume", volume, "err", err)
		return
	}
	slog.Info("old copy of a web app removed", "app", id, "volume", volume)
}

// Volumes are the mounted volumes the programs can lie on.
func (m *Manager) Volumes() []string {
	if m.Dev {
		return nil
	}
	return slices.Clone(m.mounted())
}

// NeedsVolume: there is a choice to make before anything is installed –
// several volumes, no program yet, none chosen (PLAN 75-21). Installs that
// were not asked for (pending, the automatic replacement) wait for it.
func (m *Manager) NeedsVolume() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.needsVolumeLocked()
}

func (m *Manager) needsVolumeLocked() bool {
	if m.Dev || m.state.Volume != "" {
		return false
	}
	mounted := m.mounted()
	if len(mounted) < 2 {
		return false
	}
	return !slices.ContainsFunc(m.state.Installed, func(in Installed) bool { return slices.Contains(mounted, in.Volume) })
}

// ErrVolume: the volume is not mounted.
var ErrVolume = errors.New("webapps: volume not mounted")

// SetVolume records where the programs lie and moves those that lie
// elsewhere there: each is installed again on volume and its old copy
// removed once the new one is ready, so it stays usable meanwhile.
func (m *Manager) SetVolume(volume string) error {
	if m.Dev {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !slices.Contains(m.mounted(), volume) {
		return ErrVolume
	}
	for _, j := range m.jobs {
		if j.running {
			return ErrBusy
		}
	}
	m.state.Volume = volume
	if err := m.saveLocked(); err != nil {
		return err
	}
	for _, in := range m.state.Installed {
		if in.Volume != volume {
			_ = m.startLocked(in.ID)
		}
	}
	m.poke()
	return nil
}

// poke wakes Run (pending installs).
func (m *Manager) poke() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) setJob(id string, f func(*job)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j := m.jobs[id]; j != nil {
		f(j)
	}
}

// install gets, checks and unpacks the archive of id; it returns the
// error code for the UI with the error.
func (m *Manager) install(ctx context.Context, id string) (Installed, string, error) {
	app, _ := Lookup(id)
	m.mu.Lock()
	volume := m.volumeLocked()
	m.mu.Unlock()
	if volume == "" && !m.Dev {
		return Installed{}, ErrCodeNoVolume, errors.New("webapps: no mounted volume")
	}
	root, err := m.openPrivate(volume)
	if err != nil {
		return Installed{}, ErrCodeNoVolume, err
	}
	defer root.Close()
	if !m.Dev {
		if err := checkSpace(root.Name(), 3*MaxArchive); err != nil {
			return Installed{}, ErrCodeNoVolume, err
		}
	}
	archive, code, err := m.fetch(ctx, root, app)
	if err != nil {
		return Installed{}, code, err
	}
	defer func() {
		if !m.Dev {
			root.Remove(archive)
		}
	}()
	m.setJob(id, func(j *job) { j.done = true })
	icon, err := m.unpack(root, app, archive)
	if err != nil {
		if errors.Is(err, ErrArchive) {
			return Installed{}, ErrCodeArchive, err
		}
		return Installed{}, ErrCodeInstall, err
	}
	return Installed{ID: id, Version: app.Version, Volume: volume, Icon: icon}, "", nil
}

// fetch returns the name (below root, or an absolute path in development
// mode) of the verified archive of app.
func (m *Manager) fetch(ctx context.Context, root *os.Root, app App) (string, string, error) {
	if m.Dev {
		return m.devArchive(app)
	}
	if m.Release == nil {
		return "", ErrCodeNotInRelease, ErrNotInRelease
	}
	data, err := m.Release.ReleaseManifest(ctx, m.Version)
	if err != nil {
		return "", ErrCodeDownload, fmt.Errorf("webapps: release manifest: %w", err)
	}
	entries, err := ParseRelease(data, m.Version)
	if err != nil {
		return "", ErrCodeDownload, err
	}
	i := slices.IndexFunc(entries, func(e ReleaseEntry) bool { return e.ID == app.ID })
	if i < 0 {
		return "", ErrCodeNotInRelease, ErrNotInRelease
	}
	entry := entries[i]
	if entry.Version != app.Version {
		return "", ErrCodeReleaseVersion, fmt.Errorf("webapps: the release brings %s %s, the catalog %s", app.ID, entry.Version, app.Version)
	}
	if err := root.MkdirAll("downloads", 0o700); err != nil {
		return "", ErrCodeInstall, err
	}
	// Archives of other versions are no longer needed.
	if entries, err := fs.ReadDir(root.FS(), "downloads"); err == nil {
		for _, e := range entries {
			if name := e.Name(); strings.HasPrefix(name, "titanos-webapp-"+app.ID+"-") && name != entry.Asset+".part" {
				root.Remove("downloads/" + name)
			}
		}
	}
	name := "downloads/" + entry.Asset
	part := filepath.Join(root.Name(), name+".part")
	m.setJob(app.ID, func(j *job) { j.part, j.size = part, entry.Size })
	if err := m.Release.DownloadReleaseAsset(ctx, m.Version, entry.Asset, part, entry.SHA256, entry.Size); err != nil {
		return "", ErrCodeDownload, fmt.Errorf("webapps: download: %w", err)
	}
	if err := root.Rename(name+".part", name); err != nil {
		return "", ErrCodeInstall, err
	}
	return name, "", nil
}

// devArchive finds the archive of app in DevDir (as titanos-webapp-… or
// under its upstream name) and checks it against the catalog.
func (m *Manager) devArchive(app App) (string, string, error) {
	if m.DevDir == "" {
		return "", ErrCodeNotInRelease, errors.New("webapps: development mode needs -webapps-dir")
	}
	for _, name := range []string{app.Asset(), app.UpstreamAsset} {
		p := filepath.Join(m.DevDir, name)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := verifyFile(p, app.SHA256, app.Size); err != nil {
			return "", ErrCodeDownload, err
		}
		return p, "", nil
	}
	return "", ErrCodeNotInRelease, fmt.Errorf("webapps: %s not in %s", app.Asset(), m.DevDir)
}

func verifyFile(p, sum string, size int64) error {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, size+1))
	if err != nil {
		return err
	}
	if n != size || hex.EncodeToString(h.Sum(nil)) != sum {
		return fmt.Errorf("webapps: %s does not match the catalog (size or SHA-256)", filepath.Base(p))
	}
	return nil
}

// unpack extracts archive into <id>/.staging-…, precompresses, publishes
// it as <id>/<version> and removes other versions.
func (m *Manager) unpack(root *os.Root, app App, archive string) (string, error) {
	var file *os.File
	var err error
	if filepath.IsAbs(archive) {
		file, err = os.OpenFile(archive, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	} else {
		file, err = root.OpenFile(archive, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	}
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !m.Dev {
		// The archive was checked against the signed manifest; check it again
		// as it is opened (nobody else writes there, but it costs little).
		if info.Size() > MaxArchive {
			return "", archiveError("archive too large")
		}
	}
	zr, err := zip.NewReader(file, info.Size())
	if err != nil {
		return "", archiveError("%v", err)
	}
	if err := root.MkdirAll(app.ID, 0o755); err != nil {
		return "", err
	}
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	staging := app.ID + "/.staging-" + hex.EncodeToString(raw[:])
	if err := root.Mkdir(staging, 0o755); err != nil {
		return "", err
	}
	published := false
	defer func() {
		if !published {
			root.RemoveAll(staging)
		}
	}()
	dst, err := root.OpenRoot(staging)
	if err != nil {
		return "", err
	}
	err = Extract(zr, app.ID, app.Version, dst)
	if err == nil {
		err = Precompress(dst)
	}
	icon := ""
	if err == nil {
		icon = findIcon(dst)
	}
	dst.Close()
	if err != nil {
		return "", err
	}
	final := app.ID + "/" + app.Version
	old := ""
	if _, err := root.Lstat(final); err == nil {
		old = app.ID + "/.old-" + hex.EncodeToString(raw[:])
		if err := root.Rename(final, old); err != nil {
			return "", err
		}
	}
	if err := root.Rename(staging, final); err != nil {
		return "", err
	}
	published = true
	// Other versions and leftovers of earlier attempts go.
	if entries, err := fs.ReadDir(root.FS(), app.ID); err == nil {
		for _, e := range entries {
			if e.Name() != app.Version {
				if err := root.RemoveAll(app.ID + "/" + e.Name()); err != nil {
					slog.Warn("old web app files not removed", "app", app.ID, "name", e.Name(), "err", err)
				}
			}
		}
	}
	return icon, nil
}

// Run installs pending apps once a volume is mounted and sends the notice
// for existing NAS once. It returns when ctx ends.
func (m *Manager) Run(ctx context.Context) {
	m.mu.Lock()
	m.base = ctx
	m.mu.Unlock()
	offer := time.NewTimer(m.OfferDelay)
	defer offer.Stop()
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		m.startPending()
		select {
		case <-ctx.Done():
			m.cleanupDev()
			return
		case <-offer.C:
			m.maybeOffer()
		case <-tick.C:
		case <-m.wake:
		}
	}
}

// startPending installs what waits for a volume (in RAM only; no /config read).
func (m *Manager) startPending() {
	m.mu.Lock()
	defer m.mu.Unlock()
	// After an update of TitanOS the catalog may name newer versions: they
	// are installed (the old one stays usable until the new one is there).
	mounted := m.mounted()
	for _, in := range m.state.Installed {
		app, _ := Lookup(in.ID)
		if j := m.jobs[in.ID]; app.Version == in.Version || (j != nil && (j.running || j.err != "")) || !slices.Contains(mounted, in.Volume) {
			continue
		}
		_ = m.startLocked(in.ID)
	}
	if len(m.state.Pending) == 0 || (m.volumeLocked() == "" && !m.Dev) || m.needsVolumeLocked() {
		return
	}
	for _, id := range m.state.Pending {
		if j := m.jobs[id]; j != nil && (j.running || j.err != "") {
			continue // a failure waits for the button (no retry loop)
		}
		_ = m.startLocked(id)
	}
}

// maybeOffer sends "install office programs?" once to an NAS that has none,
// nothing pending and never answered.
func (m *Manager) maybeOffer() {
	if m.Notify == nil || (m.Ready != nil && !m.Ready()) {
		return
	}
	m.mu.Lock()
	if m.state.Offered || len(m.state.Installed) > 0 || len(m.state.Pending) > 0 {
		m.mu.Unlock()
		return
	}
	m.state.Offered = true
	err := m.saveLocked()
	m.mu.Unlock()
	if err != nil {
		slog.Warn("web apps not saved to /config", "err", err)
		return
	}
	var names []string
	if m.Docker != nil {
		for _, d := range m.Docker() {
			names = append(names, d.App)
		}
	}
	if len(names) > 0 {
		m.Notify("webapps_offer_docker", strings.Join(names, ", "))
		return
	}
	m.Notify("webapps_offer", "")
}

// Await waits until id is installed (nil) or its install failed.
func (m *Manager) Await(ctx context.Context, id string) error {
	for {
		m.mu.Lock()
		j := m.jobs[id]
		_, installed := m.installedLocked(id)
		running := j != nil && j.running
		failed := ""
		if j != nil {
			failed = j.err
		}
		m.mu.Unlock()
		switch {
		case running:
		case failed != "":
			return fmt.Errorf("webapps: %s not installed: %s", id, failed)
		case installed:
			return nil
		default:
			return ErrNoApp
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// Close removes the temporary folder of development mode.
func (m *Manager) Close() { m.cleanupDev() }

func (m *Manager) cleanupDev() {
	m.mu.Lock()
	dir := m.devDir
	m.devDir = ""
	m.mu.Unlock()
	if m.Dev && dir != "" {
		os.RemoveAll(dir)
	}
}

// appRoot pins the folder of an installed app at version for serving.
func (m *Manager) appRoot(id, version string) (*os.Root, error) {
	m.mu.Lock()
	in, ok := m.installedLocked(id)
	m.mu.Unlock()
	if !ok || in.Version != version {
		return nil, ErrNoApp
	}
	root, err := m.openPrivate(in.Volume)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.OpenRoot(id + "/" + version)
}

// checkSpace is spaceFor; tests replace it.
var checkSpace = spaceFor

// spaceFor refuses a folder in RAM or without room for need bytes.
func spaceFor(dir string, need int64) error {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return err
	}
	if st.Type == 0x01021994 || st.Type == 0x794c7630 {
		return errors.New("webapps: the folder is not on a data volume")
	}
	if int64(st.Bavail)*int64(st.Bsize) < need {
		return fmt.Errorf("webapps: not enough space on the volume (%d MiB needed)", need>>20)
	}
	return nil
}

var (
	iconLink = regexp.MustCompile(`(?is)<link\s[^>]*rel=["'](?:shortcut\s+)?icon["'][^>]*>`)
	iconHref = regexp.MustCompile(`(?is)\shref=["']([^"']+)["']`)
)

// validIcon accepts a plain relative file name of a picture.
func validIcon(name string) bool {
	if _, ok := cleanEntry(name); !ok || strings.HasPrefix(name, ".") {
		return false
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".svg", ".ico", ".webp":
		return true
	}
	return false
}

// findIcon is the icon index.html links (relative, existing), else
// favicon.svg/png or icon-256.png at the top.
func findIcon(dst *os.Root) string {
	data, err := readLimited(dst, "index.html", maxIndex)
	if err == nil {
		for _, tag := range iconLink.FindAll(data, -1) {
			if m := iconHref.FindSubmatch(tag); m != nil {
				name := strings.TrimPrefix(string(m[1]), "./")
				if validIcon(name) {
					if st, err := dst.Lstat(name); err == nil && st.Mode().IsRegular() {
						return name
					}
				}
			}
		}
	}
	for _, name := range []string{"favicon.svg", "favicon.png", "icon-256.png"} {
		if st, err := dst.Lstat(name); err == nil && st.Mode().IsRegular() {
			return name
		}
	}
	return ""
}
