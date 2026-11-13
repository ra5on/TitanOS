package nvr

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
)

// Frigate's image. Version and digest belong together and change only
// with a TitanOS release that was tested with them (PLAN 79-7: one fixed,
// checked Frigate version per TitanOS release).
const (
	// FrigateVersion is shown in the diagnosis.
	FrigateVersion = "0.18.2"
	// FrigateRepository is where the image is pulled from.
	FrigateRepository = "ghcr.io/blakeblackshear/frigate"
	// FrigateDigest is the manifest of the amd64 image of FrigateVersion
	// (the standard image: OpenVINO, Edge TPU and Hailo runtimes inside).
	// Pulling by digest means a moved tag never runs unseen.
	FrigateDigest = "sha256:9ebab4801be69ab4ebc7f6ab1d582c859ff7412d457a3fa550056894e8444dad"
)

// frigateImage is the reference the engine knows the pulled image under.
const frigateImage = FrigateRepository + "@" + FrigateDigest

// Engine is the part of Docker the NVR uses (docker/nvr.go).
type Engine interface {
	EngineReady() bool
	NVRInspect(ctx context.Context) (docker.NVRState, error)
	NVRCreate(ctx context.Context, spec docker.NVRSpec) error
	NVRStart(ctx context.Context) error
	NVRRestart(ctx context.Context) error
	NVRRemove(ctx context.Context) error
	NVRLogs(ctx context.Context, tail int, since time.Time) (string, error)
	NVRNetwork(ctx context.Context) error
	NVRImageID(ctx context.Context, ref string) (string, error)
	NVRPull(ctx context.Context, repository, digest string, progress func(done, total int64)) error
	NVRPruneImages(ctx context.Context, repository, keep string) ([]string, error)
	// NVREngineStarted tells when Docker's engine started: a container
	// that ended before was stopped with it, not by a fault of Frigate.
	NVREngineStarted(ctx context.Context) (time.Time, error)
	// NVRExec runs a command (argv) in the NVR's running container and
	// returns what it printed (at most limit bytes; never logged, it may
	// hold a camera's password) and its exit code (preload.go).
	NVRExec(ctx context.Context, cmd []string, limit int) (string, int, error)
}

// Errors the API turns into codes.
var (
	// ErrDockerOff: Docker is switched off; Enable can switch it on when
	// the administrator confirms.
	ErrDockerOff = errors.New("nvr: Docker is switched off")
	// ErrNoVolume: no volume is chosen or it is not mounted.
	ErrNoVolume = errors.New("nvr: no volume for the NVR")
	// ErrVolumeSleeps: the disks of the chosen volume may be put to sleep;
	// recordings keep them awake. The administrator confirms.
	ErrVolumeSleeps = errors.New("nvr: the disks of the volume may sleep")
	// ErrNotFound: no such camera.
	ErrNotFound = errors.New("nvr: no such camera")
	// ErrExists: a camera with this ID exists.
	ErrExists = errors.New("nvr: a camera with this ID exists")
	// ErrNameExists: another camera has this name (letter case aside).
	ErrNameExists = errors.New("nvr: a camera with this name exists")
	// ErrLimit: MaxCameras are configured.
	ErrLimit = errors.New("nvr: too many cameras")
	// ErrNotLoaded: /config could not be read at the start; nothing is
	// written over what may be there.
	ErrNotLoaded = errors.New("nvr: configuration could not be read from /config")
	// ErrDisabled: the NVR is switched off.
	ErrDisabled = errors.New("nvr: the NVR is switched off")
	// ErrRecordingsShrink: the change of a camera lets Frigate delete
	// recordings that exist now (a narrower recording mode, fewer days);
	// the administrator confirms (CameraOptions.ConfirmShrink).
	ErrRecordingsShrink = errors.New("nvr: the change deletes recordings that exist now")
)

// Manager runs the NVR.
type Manager struct {
	Engine Engine
	// Access holds which account may see which camera (never nil; the
	// wiring gives it the accounts and loads it).
	Access *Access
	// Dev: development mode. Nothing is pulled and no volume is touched:
	// Engine is a DevEngine, data go to a temporary folder (dev.go).
	Dev       bool
	MountRoot string
	// Mounted lists the mounted volumes.
	Mounted func() []string
	// Asleep tells whether the disks of a volume sleep right now (RAM of
	// the storage manager): the backup of the faces does not wake them.
	Asleep func(volume string) bool
	// Sleeps names the volumes whose disks may be put to sleep; known is
	// false while that cannot be told.
	Sleeps func(ctx context.Context) (sleeping map[string]bool, known bool)
	// Devices are the GPUs etc. for containers; Leased those with a VM.
	Devices func(ctx context.Context) []hardware.ContainerDevice
	Leased  func() map[string]bool
	// DockerEnabled tells whether Docker is switched on; EnableDocker
	// switches it on with a volume for its data; DockerVolume is the
	// volume Docker is set to ("" if none).
	DockerEnabled func() bool
	EnableDocker  func(volume string) error
	DockerVolume  func() string
	// Notify raises a message of TitanOS (notify.Manager.Notify).
	Notify func(severity, kind, subject string)
	// Route sends a message about a camera the ways its owner chose
	// (routing.go); the wiring gives it notify.Manager.NotifyVia.
	Route func(Notice)
	// Addresses are the addresses of the NAS in the home network, for
	// WebRTC; lanAddresses if nil.
	Addresses func() []string
	// KernelLog returns the kernel's messages of this start (diagnosis).
	KernelLog func(ctx context.Context) string
	// FrigateBase is the address of Frigate's API for the container's
	// address; tests and development mode put their own server there.
	FrigateBase func(address string) string
	// Timezone names the time zone of the NAS ("Europe/Berlin"), for the
	// days of the recordings.
	Timezone func() string
	// Now is the clock; time.Now if nil.
	Now func() time.Time
	// Resolve looks a host name up (net.DefaultResolver if nil).
	Resolve func(ctx context.Context, host string) ([]netip.Addr, error)

	// The watch (watch.go); tests shorten them.
	Tick          time.Duration // between two looks at the container
	HealthyWithin time.Duration // a start must answer within this
	CrashWindow   time.Duration // CrashLimit ends within it are a loop
	CrashLimit    int
	OfflineAfter  time.Duration // a camera without pictures is reported after this

	store   *config.Store
	frigate *frigate
	feed    *feed
	facts   func() hostFacts
	bootID  func() string

	mu sync.Mutex
	// camerasMu orders the owner's changes of the cameras and of the
	// deleted cameras (cameras.json, deleted-cameras.json); settingsMu
	// those of settings.json. Both are taken before mu, never while mu is
	// held, and never by the loop while it waits for Docker.
	camerasMu, settingsMu sync.Mutex
	// exportsMu orders the changes of exports.json (exports.go).
	exportsMu sync.Mutex
	loaded    bool
	settings  Settings
	cameras   []Camera
	// deleted are the deleted cameras whose recordings are kept, as read
	// from the stick (deleted.go).
	deleted []DeletedCamera
	state   State
	rt      runtime
	kick    chan struct{}
	devDir  string
	// restored are faces a restore of /config left on the stick, until
	// they are on the volume; backupFaces is the library as last read for
	// the backup (backup.go).
	restored, backupFaces []BackupFile
	// devFrigate is the stand-in of development mode (dev.go).
	devFrigate *DevFrigate
	// snapshots are the stills of the last second (status.go).
	snapshots map[snapshotKey]cachedSnapshot
	// probed is what the checks of cameras found, by stream address
	// (probe.go).
	probed map[string]probedStream
}

// New makes a manager with the defaults of a NAS.
func New(store *config.Store) *Manager {
	m := &Manager{
		store: store, MountRoot: "/mnt", settings: Defaults(), cameras: []Camera{}, feed: newFeed(), Access: NewAccess(store),
		FrigateBase:   func(address string) string { return fmt.Sprintf("http://%s:%d", address, frigatePort) },
		Tick:          10 * time.Second,
		HealthyWithin: 5 * time.Minute,
		CrashWindow:   5 * time.Minute,
		CrashLimit:    3,
		OfflineAfter:  5 * time.Minute,
		facts:         readHostFacts,
		bootID:        readBootID,
		kick:          make(chan struct{}, 1),
		rt:            runtime{phase: PhaseOff, cameras: map[string]*cameraRuntime{}},
	}
	m.frigate = newFrigate(m.proxySecret)
	return m
}

func readBootID() string {
	data, _ := os.ReadFile("/proc/sys/kernel/random/boot_id") // unreadable: every start counts as the same boot
	return strings.TrimSpace(string(data))
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Load reads settings and cameras from /config; missing files keep the
// defaults. In development mode without a camera file there are four
// demo cameras (in RAM only).
func (m *Manager) Load() error {
	s := Defaults()
	if err := m.store.ReadJSON(settingsFile, &s); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var cameras []Camera
	err := m.store.ReadJSON(camerasFile, &cameras)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if errors.Is(err, fs.ErrNotExist) && m.Dev {
		cameras = demoCameras()
	}
	if cameras == nil {
		cameras = []Camera{}
	}
	sortCameras(cameras)
	// Without the deleted cameras config.yml would have Frigate delete
	// their recordings, and a new camera could take one's ID: a list that
	// cannot be read leaves the NVR unloaded, like the cameras.
	var deleted []DeletedCamera
	if err := m.store.ReadJSON(deletedFile, &deleted); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	deleted = slices.DeleteFunc(deleted, func(d DeletedCamera) bool { return !validDeleted(d) })
	restored := m.loadRestored()
	m.mu.Lock()
	m.restored = restored
	m.settings, m.cameras, m.deleted, m.loaded = normalize(s), cameras, deleted, true
	m.changedLocked()
	m.mu.Unlock()
	return nil
}

// changedLocked notes a change the loop applies (m.mu held): config and
// container are compared with what they should be at the next look, and
// a look that planned from what was before plans again (watch.go).
func (m *Manager) changedLocked() {
	m.rt.dirty = true
	m.rt.gen++
}

// Settings returns the settings (with the restream password: only for
// administrators).
func (m *Manager) Settings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.settings
	// A copy: whoever decodes a request into it must not write into the
	// manager's own list.
	s.Plates = make([]KnownPlate, 0, len(m.settings.Plates))
	for _, entry := range m.settings.Plates {
		s.Plates = append(s.Plates, KnownPlate{Name: entry.Name, Plates: slices.Clone(entry.Plates)})
	}
	if len(s.Plates) == 0 {
		s.Plates = nil
	}
	return s
}

func (m *Manager) mounted() []string {
	if m.Dev {
		return []string{devVolume}
	}
	if m.Mounted == nil {
		return nil
	}
	return m.Mounted()
}

// SetOptions go with a change of the settings.
type SetOptions struct {
	// EnableDocker: the administrator agreed to switch Docker on.
	EnableDocker bool
	// ConfirmSleepingVolume: the administrator was told that recordings
	// keep the disks of this volume awake.
	ConfirmSleepingVolume bool
}

// SetSettings validates and stores the settings and applies them. The
// restream password is kept (made when the restream is first switched on);
// what the request says about it is ignored. One change at a time: two at
// once could leave the stick and RAM with different settings.
func (m *Manager) SetSettings(ctx context.Context, s Settings, opts SetOptions) error {
	m.settingsMu.Lock()
	defer m.settingsMu.Unlock()
	s = normalize(s)
	if m.Dev && s.Volume == "" {
		s.Volume = devVolume
	}
	mounted := m.mounted()
	if err := ValidateSettings(s, mounted); err != nil {
		if s.Enabled && (s.Volume == "" || !slices.Contains(mounted, s.Volume)) {
			return ErrNoVolume
		}
		return err
	}
	m.mu.Lock()
	old, loaded := m.settings, m.loaded
	m.mu.Unlock()
	if !loaded {
		return ErrNotLoaded
	}
	s.Plates, _ = cleanPlates(s.Plates) // validated above
	if len(s.Plates) == 0 {
		s.Plates = nil
	}
	s.RTSPPassword, s.ProxySecret = old.RTSPPassword, old.ProxySecret
	if s.RTSPOut && s.RTSPPassword == "" {
		s.RTSPPassword = newSecret()
	}
	if s.Enabled && s.ProxySecret == "" {
		s.ProxySecret = newProxySecret()
	}
	if s.Volume != "" && s.Volume != old.Volume && !opts.ConfirmSleepingVolume && m.Sleeps != nil && !m.Dev {
		if sleeping, known := m.Sleeps(ctx); known && sleeping[s.Volume] {
			return ErrVolumeSleeps
		}
	}
	if s.Enabled && !m.Dev && m.DockerEnabled != nil && !m.DockerEnabled() {
		if !opts.EnableDocker || m.EnableDocker == nil {
			return ErrDockerOff
		}
		// Docker keeps its own volume if it has one; else it shares the
		// NVR's.
		volume := s.Volume
		if m.DockerVolume != nil && slices.Contains(mounted, m.DockerVolume()) {
			volume = m.DockerVolume()
		}
		if err := m.EnableDocker(volume); err != nil {
			return fmt.Errorf("nvr: switch Docker on: %w", err)
		}
		slog.Info("Docker switched on for the NVR", "volume", volume)
		// The container Docker finds ended is no crash of Frigate.
		m.mu.Lock()
		m.dockerWentLocked()
		m.mu.Unlock()
	}
	if err := m.store.WriteJSON(settingsFile, s); err != nil {
		return err
	}
	m.mu.Lock()
	m.settings = s
	// Another device, another volume or switching on again: the choice is
	// tried afresh.
	if s.Detector.Kind != old.Detector.Kind || s.Volume != old.Volume || (s.Enabled && !old.Enabled) {
		m.resetStateLocked()
	}
	if s.Decoder != old.Decoder {
		// Another decoder: a GPU Docker refused before is tried again.
		m.rt.brokenDecoders = nil
	}
	if s.Volume != old.Volume {
		// Another volume: its state, exports and config.yml are its own.
		m.rt.stateLoaded, m.rt.exports, m.rt.exportSizes = false, nil, nil
		m.rt.configHash, m.rt.configDir, m.rt.configCameras = "", "", nil
	}
	m.changedLocked()
	m.rt.retryAt = time.Time{}
	m.mu.Unlock()
	m.wake()
	return nil
}

// newSecret is a password of letters and digits (it goes into a YAML
// field Frigate substitutes braces in, and into addresses people type).
func newSecret() string {
	raw := make([]byte, 15)
	if _, err := rand.Read(raw); err != nil {
		panic(err) // crypto/rand does not fail on Linux
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
}

// newProxySecret is the secret between titand and Frigate: 256 bits,
// letters and digits (Frigate substitutes braces in the field).
func newProxySecret() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err) // crypto/rand does not fail on Linux
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
}

// proxySecret is the secret Frigate expects in X-Proxy-Secret.
func (m *Manager) proxySecret() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings.ProxySecret
}

// FrigateHeader are the headers every connection to Frigate carries (the
// API's relays of the live stream and of talking back use them).
func (m *Manager) FrigateHeader() http.Header {
	return http.Header{proxySecretHeader: {m.proxySecret()}}
}

// ensureSecret makes the secret of an NVR that is on without one (its
// settings came from a build before the secret): once per start of
// titand. If the stick cannot be written the secret stays in RAM; the
// next start makes another one. While a change of the settings is being
// made the loop does not wait for it: that change makes the secret.
func (m *Manager) ensureSecret() {
	if !m.settingsMu.TryLock() {
		return
	}
	defer m.settingsMu.Unlock()
	m.mu.Lock()
	if m.settings.ProxySecret != "" || !m.settings.Enabled || !m.loaded {
		m.mu.Unlock()
		return
	}
	m.settings.ProxySecret = newProxySecret()
	s := m.settings
	m.changedLocked()
	m.mu.Unlock()
	if err := m.store.WriteJSON(settingsFile, s); err != nil {
		slog.Warn("NVR: the secret for Frigate is not on the stick; another one is made at the next start", "err", err)
	}
}

// Enable switches the NVR on (volume "" keeps the chosen one).
func (m *Manager) Enable(ctx context.Context, volume string, opts SetOptions) error {
	s := m.Settings()
	s.Enabled = true
	if volume != "" {
		s.Volume = volume
	}
	return m.SetSettings(ctx, s, opts)
}

// Disable stops and removes the container; settings, cameras, Frigate's
// database and the recordings stay.
func (m *Manager) Disable(ctx context.Context) error {
	s := m.Settings()
	s.Enabled = false
	return m.SetSettings(ctx, s, SetOptions{})
}

// RetryDetector goes back to the chosen device after a failover or after
// the NVR stopped because nothing worked.
func (m *Manager) RetryDetector() error {
	m.mu.Lock()
	if !m.settings.Enabled {
		m.mu.Unlock()
		return ErrDisabled
	}
	m.resetStateLocked()
	// "Wieder versuchen" also tries a decoding GPU Docker refused.
	m.rt.brokenDecoders = nil
	m.changedLocked()
	m.rt.retryAt = time.Time{}
	m.mu.Unlock()
	m.wake()
	return nil
}

// resetStateLocked forgets failover and stop; the loop stores it.
func (m *Manager) resetStateLocked() {
	m.state = State{Boot: m.state.Boot, CapNoticeDay: m.state.CapNoticeDay}
	m.rt.stateDirty = true
	m.rt.crashes, m.rt.hangs = nil, nil
	m.rt.lastError = nil
	m.rt.active = ""
}

// dockerWentLocked notes that Docker's engine was down or switched on by
// titand (m.mu held): a container found ended afterwards was stopped by
// Docker, not by a crash of Frigate, and is started without counting.
func (m *Manager) dockerWentLocked() {
	m.rt.startedAt, m.rt.healthy = time.Time{}, false
	m.rt.crashes = nil
}

// wake makes the loop look now.
func (m *Manager) wake() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// Cameras returns the cameras, sorted, with their passwords: callers that
// answer a request use CameraView.
func (m *Manager) Cameras() []Camera {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneCameras(m.cameras)
}

func cloneCameras(list []Camera) []Camera {
	out := make([]Camera, len(list))
	for i, c := range list {
		c.Detect.Objects = slices.Clone(c.Detect.Objects)
		c.Notify.Kinds = slices.Clone(c.Notify.Kinds)
		// A request is decoded into a copy of the stored camera: shared
		// slices would let a refused change reach the cameras in RAM.
		c.Notify.Recipients = slices.Clone(c.Notify.Recipients)
		c.Notify.Window.Days = slices.Clone(c.Notify.Window.Days)
		out[i] = c
	}
	return out
}

// Camera returns one camera.
func (m *Manager) Camera(id string) (Camera, bool) {
	for _, c := range m.Cameras() {
		if c.ID == id {
			return c, true
		}
	}
	return Camera{}, false
}

// CameraIDs lists the IDs of the cameras.
func (m *Manager) CameraIDs() []string {
	ids := []string{}
	for _, c := range m.Cameras() {
		ids = append(ids, c.ID)
	}
	return ids
}

// CameraOptions go with a change of a camera.
type CameraOptions struct {
	// ClearPassword drops the stored password (UpdateCamera).
	ClearPassword bool
	// ConfirmShrink: the administrator was told that the change lets
	// Frigate delete recordings that exist now (ErrRecordingsShrink).
	ConfirmShrink bool
}

// keepRank orders the recording modes by what Frigate keeps of the
// recordings that exist: continuous keeps every segment, motion those
// with motion, events those with an active object. Off records nothing
// and keeps everything that was recorded, like continuous.
func keepRank(mode string) int {
	switch mode {
	case RecordContinuous, RecordOff:
		return 3
	case RecordMotion:
		return 2
	}
	return 1
}

// shrinks tells whether going from was to next lets Frigate delete
// recordings that exist now: a narrower mode (continuous → motion →
// events; off counts as continuous, and any mode → off keeps), or fewer
// days. Frigate applies the new retention to what is there within a
// minute of its restart (frigate/record/cleanup.py), and a retention that
// protected the old segments would keep every new one
// (frigate/record/maintainer.py: continuous.days > 0 keeps all).
func shrinks(was, next Record) bool {
	return next.RetainDays < was.RetainDays || keepRank(next.Mode) < keepRank(was.Mode)
}

// AddCamera stores a new camera. An empty ID is made from the name.
func (m *Manager) AddCamera(ctx context.Context, c Camera, opts CameraOptions) (Camera, error) {
	if err := m.checkNames(ctx, c); err != nil {
		return c, err
	}
	m.camerasMu.Lock()
	defer m.camerasMu.Unlock()
	// A new ID is never one of a deleted camera whose recordings are
	// kept, whatever volume is mounted; that ID given on purpose takes
	// them back.
	deleted := m.deletedList()
	added, err := m.addCamera(c, deleted, opts)
	if err != nil {
		return added, err
	}
	if slices.ContainsFunc(deleted, func(d DeletedCamera) bool { return d.ID == added.ID }) {
		if err := m.storeDeletedLocked(func(list []DeletedCamera) []DeletedCamera {
			return slices.DeleteFunc(list, func(d DeletedCamera) bool { return d.ID == added.ID })
		}); err != nil {
			// The camera wins over its stub in config.yml anyway.
			slog.Warn("NVR: deleted camera not taken off its list", "camera", added.ID, "err", err)
		}
	}
	return added, nil
}

func (m *Manager) addCamera(c Camera, deleted []DeletedCamera, opts CameraOptions) (Camera, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return c, ErrNotLoaded
	}
	if len(m.cameras) >= MaxCameras {
		return c, ErrLimit
	}
	taken := []string{}
	for _, each := range m.cameras {
		taken = append(taken, each.ID)
	}
	if c.ID == "" {
		for _, d := range deleted {
			taken = append(taken, d.ID)
		}
		c.ID = SlugID(c.Name, taken)
	} else if slices.Contains(taken, c.ID) {
		return c, ErrExists
	}
	if c.Order == 0 {
		for _, each := range m.cameras {
			c.Order = max(c.Order, each.Order)
		}
		c.Order++
	}
	// Codecs and sizes are titand's (streamFactsLocked), never the request's.
	clearStreamFacts(&c)
	c, err := ValidateCamera(c)
	if err != nil {
		return c, err
	}
	if err := m.uniqueNameLocked(c); err != nil {
		return c, err
	}
	// Only now is the ID known that the rights of the recipients are on.
	if c, err = m.checkRecipients(c, nil); err != nil {
		return c, err
	}
	// The ID of a deleted camera takes its recordings back: they are kept
	// from now on as the new camera keeps its own.
	if at := slices.IndexFunc(deleted, func(d DeletedCamera) bool { return d.ID == c.ID }); at >= 0 && !opts.ConfirmShrink &&
		shrinks(Record{Mode: RecordOff, RetainDays: deleted[at].RetainDays}, c.Record) {
		return c, ErrRecordingsShrink
	}
	m.streamFactsLocked(&c, nil)
	return c, m.storeCamerasLocked(append(cloneCameras(m.cameras), c))
}

// uniqueNameLocked refuses a name another camera has, letter case aside:
// people tell cameras apart by name.
func (m *Manager) uniqueNameLocked(c Camera) error {
	for _, other := range m.cameras {
		if other.ID != c.ID && strings.EqualFold(other.Name, c.Name) {
			return ErrNameExists
		}
	}
	return nil
}

// checkNames checks the host names in a camera's addresses as its IP
// addresses are checked (ValidateCamera): every address a name stands
// for must be allowed, in the home network unless the administrator
// allowed public addresses, never this NAS. A name that does not resolve
// is refused. titand checks again at every connection it makes itself
// (Prober); Frigate's connections it cannot check.
func (m *Manager) checkNames(ctx context.Context, c Camera) error {
	hosts := []string{}
	if c.Source.Kind == "onvif" {
		hosts = append(hosts, c.Source.Host)
	}
	for _, raw := range []string{c.Source.MainURL, c.Source.SubURL} {
		if host, err := streamURL(raw); err == nil {
			hosts = append(hosts, host)
		}
	}
	resolve := m.Resolve
	if resolve == nil {
		resolve = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}
	prober := &Prober{AllowPublic: c.Source.AllowPublic}
	for _, host := range hosts {
		if _, err := netip.ParseAddr(host); err == nil || host == "" {
			continue
		}
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		addrs, err := resolve(rctx, host)
		cancel()
		if err != nil || len(addrs) == 0 {
			return invalid("source: the name %s cannot be resolved", host)
		}
		for _, addr := range addrs {
			if !prober.allowed(addr) {
				return fmt.Errorf("%w: %s (%s)", ErrTargetNotAllowed, host, addr)
			}
		}
	}
	return nil
}

// UpdateCamera replaces a camera. An empty password keeps the stored one
// as long as the camera is still reached at the same host with the same
// user: a password is never sent to another address by an edit.
// opts.ClearPassword drops it. A change that lets Frigate delete
// recordings that exist now needs opts.ConfirmShrink (ErrRecordingsShrink).
func (m *Manager) UpdateCamera(ctx context.Context, id string, c Camera, opts CameraOptions) (Camera, error) {
	if err := m.checkNames(ctx, c); err != nil {
		return c, err
	}
	m.camerasMu.Lock()
	defer m.camerasMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return c, ErrNotLoaded
	}
	at := slices.IndexFunc(m.cameras, func(each Camera) bool { return each.ID == id })
	if at < 0 {
		return c, ErrNotFound
	}
	was := m.cameras[at]
	c.ID = id
	clearStreamFacts(&c)
	c, err := ValidateCamera(c)
	if err != nil {
		return c, err
	}
	if c.Source.Password == "" && !opts.ClearPassword && was.Source.Password != "" {
		known := []string{was.Source.Host, streamHost(was.Source.MainURL), streamHost(was.Source.SubURL)}
		for _, host := range []string{c.Source.Host, streamHost(c.Source.MainURL), streamHost(c.Source.SubURL)} {
			if host != "" && !slices.Contains(known, host) {
				return c, invalid("source.password: needed again for another address")
			}
		}
		if c.Source.User != was.Source.User {
			return c, invalid("source.password: needed again for another user")
		}
		c.Source.Password = was.Source.Password
	}
	if err := m.uniqueNameLocked(c); err != nil {
		return c, err
	}
	if c, err = m.checkRecipients(c, &was); err != nil {
		return c, err
	}
	// The sounds' codecs and the pictures' sizes: from a check of each
	// stream, else as they were for the same stream, else unknown.
	m.streamFactsLocked(&c, &was)
	// Asked last: a change that is refused anyway needs no confirmation.
	if !opts.ConfirmShrink && shrinks(was.Record, c.Record) {
		return c, ErrRecordingsShrink
	}
	list := cloneCameras(m.cameras)
	list[at] = c
	return c, m.storeCamerasLocked(list)
}

func streamHost(raw string) string {
	host, _ := streamURL(raw) // "" for an address that does not parse: never equal to a stored host
	return host
}

// RemoveCamera deletes a camera and its rights. Its recordings stay until
// their days are over: the camera stays in config.yml as a stub that is
// switched off (deleted.go). That is noted on the stick first, whatever
// volume is mounted or chosen; then its rights go. If the stick cannot be
// written, or the rights could not be read at the start
// (ErrRightsUnreadable: a camera added later with its ID would inherit
// them), the camera is not deleted.
func (m *Manager) RemoveCamera(id string) error {
	m.camerasMu.Lock()
	defer m.camerasMu.Unlock()
	m.mu.Lock()
	loaded := m.loaded
	at := slices.IndexFunc(m.cameras, func(each Camera) bool { return each.ID == id })
	var gone Camera
	if at >= 0 {
		gone = m.cameras[at]
	}
	m.mu.Unlock()
	if !loaded {
		return ErrNotLoaded
	}
	if at < 0 {
		return ErrNotFound
	}
	if m.Access != nil && !m.Access.Readable() {
		return ErrRightsUnreadable
	}
	stub := DeletedCamera{ID: gone.ID, Name: gone.Name, RetainDays: gone.Record.RetainDays, DeletedAt: m.now().UTC().Truncate(time.Second)}
	if err := m.storeDeletedLocked(func(list []DeletedCamera) []DeletedCamera {
		return append(slices.DeleteFunc(list, func(d DeletedCamera) bool { return d.ID == id }), stub)
	}); err != nil {
		return fmt.Errorf("nvr: deleted camera not noted on the stick: %w", err)
	}
	// The rights go before the camera: a camera whose rights stay is not
	// deleted (its stub is harmless while it is a camera).
	if m.Access != nil {
		if err := m.Access.ForgetCamera(id); err != nil {
			return fmt.Errorf("nvr: rights of the camera not removed: %w", err)
		}
	}
	m.mu.Lock()
	list := slices.DeleteFunc(cloneCameras(m.cameras), func(each Camera) bool { return each.ID == id })
	err := m.storeCamerasLocked(list)
	m.mu.Unlock()
	return err
}

// storeCamerasLocked writes the cameras and has them applied.
func (m *Manager) storeCamerasLocked(list []Camera) error {
	sortCameras(list)
	if err := m.store.WriteJSON(camerasFile, list); err != nil {
		return err
	}
	m.cameras = list
	m.changedLocked()
	m.rt.retryAt = time.Time{}
	m.wake()
	return nil
}

// Hardware probes what the NAS has for the NVR.
func (m *Manager) Hardware(ctx context.Context) HardwareInfo {
	if m.Dev {
		return devHardware()
	}
	var devices []hardware.ContainerDevice
	if m.Devices != nil {
		devices = m.Devices(ctx)
	}
	var leased map[string]bool
	if m.Leased != nil {
		leased = m.Leased()
	}
	return probeHardware(devices, leased, m.facts())
}

// hardwareCached is Hardware, at most once a minute: the devices are read
// from /sys, and an NVIDIA card asks nvidia-smi.
func (m *Manager) hardwareCached(ctx context.Context, fresh bool) HardwareInfo {
	m.mu.Lock()
	if !fresh && m.rt.hardwareAt.After(m.now().Add(-time.Minute)) {
		hw := m.rt.hardware
		m.mu.Unlock()
		return hw
	}
	m.mu.Unlock()
	hw := m.Hardware(ctx)
	m.mu.Lock()
	m.rt.hardware, m.rt.hardwareAt = hw, m.now()
	m.mu.Unlock()
	return hw
}

func (m *Manager) addresses() []string {
	if m.Addresses != nil {
		return m.Addresses()
	}
	return lanAddresses()
}

// lanAddresses are the private IPv4 addresses of the NAS on interfaces
// that are no container or VM bridges.
func lanAddresses() []string {
	out := []string{}
	interfaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, i := range interfaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		skip := false
		for _, prefix := range []string{"docker", "br-", "veth", "virbr", "lxc", "tvw", "vnet", "tap"} {
			skip = skip || strings.HasPrefix(i.Name, prefix)
		}
		if skip {
			continue
		}
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			prefix, err := netip.ParsePrefix(a.String())
			if err != nil {
				continue
			}
			if addr := prefix.Addr(); addr.Is4() && HomeAddr(addr) {
				out = append(out, addr.String())
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
