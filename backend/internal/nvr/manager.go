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
	// ErrLimit: MaxCameras are configured.
	ErrLimit = errors.New("nvr: too many cameras")
	// ErrNotLoaded: /config could not be read at the start; nothing is
	// written over what may be there.
	ErrNotLoaded = errors.New("nvr: configuration could not be read from /config")
	// ErrDisabled: the NVR is switched off.
	ErrDisabled = errors.New("nvr: the NVR is switched off")
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

	mu       sync.Mutex
	loaded   bool
	settings Settings
	cameras  []Camera
	state    State
	rt       runtime
	kick     chan struct{}
	devDir   string
	// restored are faces a restore of /config left on the stick, until
	// they are on the volume; backupFaces is the library as last read for
	// the backup (backup.go).
	restored, backupFaces []BackupFile
	// devFrigate is the stand-in of development mode (dev.go).
	devFrigate *DevFrigate
	// snapshots are the stills of the last second (status.go).
	snapshots map[snapshotKey]cachedSnapshot
}

// New makes a manager with the defaults of a NAS.
func New(store *config.Store) *Manager {
	return &Manager{
		store: store, MountRoot: "/mnt", settings: Defaults(), cameras: []Camera{}, frigate: newFrigate(), feed: newFeed(), Access: NewAccess(store),
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
	restored := m.loadRestored()
	m.mu.Lock()
	m.restored = restored
	m.settings, m.cameras, m.loaded = normalize(s), cameras, true
	m.rt.dirty = true
	m.mu.Unlock()
	return nil
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
// what the request says about it is ignored.
func (m *Manager) SetSettings(ctx context.Context, s Settings, opts SetOptions) error {
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
	s.RTSPPassword = old.RTSPPassword
	if s.RTSPOut && s.RTSPPassword == "" {
		s.RTSPPassword = newSecret()
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
	if s.Volume != old.Volume {
		m.rt.stateLoaded, m.rt.exports, m.rt.exportSizes = false, nil, nil
	}
	m.rt.dirty = true
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
	m.rt.dirty = true
	m.rt.retryAt = time.Time{}
	m.mu.Unlock()
	m.wake()
	return nil
}

// resetStateLocked forgets failover and stop; the loop stores it.
func (m *Manager) resetStateLocked() {
	m.state = State{Boot: m.state.Boot, CapNoticeDay: m.state.CapNoticeDay}
	m.rt.stateDirty = true
	m.rt.crashes = nil
	m.rt.lastError = nil
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

// AddCamera stores a new camera. An empty ID is made from the name.
func (m *Manager) AddCamera(c Camera) (Camera, error) {
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
	c, err := ValidateCamera(c)
	if err != nil {
		return c, err
	}
	return c, m.storeCamerasLocked(append(cloneCameras(m.cameras), c))
}

// UpdateCamera replaces a camera. An empty password keeps the stored one
// as long as the camera is still reached at the same host with the same
// user: a password is never sent to another address by an edit.
// clearPassword drops it.
func (m *Manager) UpdateCamera(id string, c Camera, clearPassword bool) (Camera, error) {
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
	c, err := ValidateCamera(c)
	if err != nil {
		return c, err
	}
	if c.Source.Password == "" && !clearPassword && was.Source.Password != "" {
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
	list := cloneCameras(m.cameras)
	list[at] = c
	return c, m.storeCamerasLocked(list)
}

func streamHost(raw string) string {
	host, _ := streamURL(raw) // "" for an address that does not parse: never equal to a stored host
	return host
}

// RemoveCamera deletes a camera and its rights. Its recordings stay in
// Frigate until their days are over.
func (m *Manager) RemoveCamera(id string) error {
	m.mu.Lock()
	if !m.loaded {
		m.mu.Unlock()
		return ErrNotLoaded
	}
	list := slices.DeleteFunc(cloneCameras(m.cameras), func(each Camera) bool { return each.ID == id })
	if len(list) == len(m.cameras) {
		m.mu.Unlock()
		return ErrNotFound
	}
	err := m.storeCamerasLocked(list)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	if m.Access != nil {
		if err := m.Access.ForgetCamera(id); err != nil {
			slog.Warn("rights of the deleted camera not removed", "camera", id, "err", err)
		}
	}
	return nil
}

// storeCamerasLocked writes the cameras and has them applied.
func (m *Manager) storeCamerasLocked(list []Camera) error {
	sortCameras(list)
	if err := m.store.WriteJSON(camerasFile, list); err != nil {
		return err
	}
	m.cameras = list
	m.rt.dirty = true
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
