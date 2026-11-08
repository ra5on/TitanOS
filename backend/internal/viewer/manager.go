package viewer

import (
	"bufio"
	"bytes"
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
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/viewer/proto"
)

// Engine is the part of Docker the viewer uses (docker/viewer.go).
type Engine interface {
	Viewers(ctx context.Context) ([]docker.ViewerContainer, error)
	ViewerCreate(ctx context.Context, spec docker.ViewerSpec) (string, error)
	ViewerStart(ctx context.Context, id string) error
	ViewerRemove(ctx context.Context, id string) error
	ViewerState(ctx context.Context, id, network string) (string, bool, error)
	ViewerMemory(ctx context.Context, id string) (uint64, error)
	ViewerImageID(ctx context.Context, ref string) (string, error)
	ViewerLoadImage(ctx context.Context, archive io.Reader) error
	ViewerNetwork(ctx context.Context, name, bridge, user, app string) error
	ViewerNetworks(ctx context.Context) ([]string, error)
	ViewerRemoveNetwork(ctx context.Context, name string) error
	AppEndpoints(ctx context.Context, app string) ([]docker.AppEndpoint, error)
	ViewerAttach(ctx context.Context, network, id, alias string) (string, bool, error)
	ViewerDetach(ctx context.Context, network, id, alias string) error
	ViewerAttached(ctx context.Context, network, alias string) ([]string, error)
	ViewerImages(ctx context.Context) ([]docker.ViewerImage, error)
	ViewerRemoveImage(ctx context.Context, id string) error
}

// Release gives the signed manifest and assets of a release (package
// plugins, release_assets.go).
type Release interface {
	ReleaseManifest(ctx context.Context, version string) ([]byte, error)
	DownloadReleaseAsset(ctx context.Context, version, asset, dest, sha256 string, size int64) error
}

// Errors of Open; the API turns each into a code for the window.
var (
	// ErrImageNotReady: the image is being fetched (or failed); see ImageStatus.
	ErrImageNotReady = errors.New("viewer: the image is not ready")
	// ErrMemory: a new view would leave less than Reserve of RAM.
	ErrMemory = errors.New("viewer: not enough free memory")
	// ErrHostNetwork: the app uses the network of the NAS (or of another
	// container) and cannot join the internal network.
	ErrHostNetwork = errors.New("viewer: the app uses the host network")
	// ErrNoWebUI: no container of the app publishes the port of its WebUI.
	ErrNoWebUI = errors.New("viewer: the app has no web interface")
	// ErrNotRunning: the app's web container does not run.
	ErrNotRunning = errors.New("viewer: the app does not run")
	// ErrNoVolume: no mounted volume for the profiles.
	ErrNoVolume = errors.New("viewer: no volume for the remote view")
	// ErrName: an account or app name that cannot be a folder or label.
	ErrName = errors.New("viewer: invalid name")
	// ErrLimit: the account has PerAccount views open already.
	ErrLimit = errors.New("viewer: too many remote views for this account")
	// ErrStale: the container of the view is gone or has another address;
	// the view was stopped and is to be opened again.
	ErrStale = errors.New("viewer: the view's container changed")
)

const (
	// Reserve is the RAM that must stay free after a new view.
	Reserve = 700 << 20
	// PerView is the memory limit of one viewer container. It is reserved
	// for every open or waiting view (measured 230–400 MB in use).
	PerView         = 1 << 30
	containerMemory = PerView
	// PerAccount is how many views one account may have at a time.
	PerAccount = 4
	// systemShare is the RAM kept out of the overall cap of views.
	systemShare = 2 << 30
	// agentPort is where the agent in the container listens.
	agentPort = proto.Port
	// attachAlias marks the endpoint of an app TitanOS connected to the
	// viewer network; only endpoints with it are ever disconnected.
	attachAlias = "titanos-viewer-attached"
)

// profileUID is the user (and group) of the browser in the container;
// tests set their own.
var profileUID = 1000

// Manager runs the remote views.
type Manager struct {
	Engine  Engine
	Release Release
	// Version is the version of this titand (without "v").
	Version string
	// Dev: development mode, local image, profiles in a temporary folder.
	Dev       bool
	MountRoot string
	// Mounted lists the mounted volumes; DockerVolume is Docker's.
	Mounted      func() []string
	DockerVolume func() string
	// Devices are the GPUs etc. for containers; Leased those with a VM.
	Devices func(ctx context.Context) []hardware.ContainerDevice
	Leased  func() map[string]bool
	// Route is the app's HTTPS name target (host port and scheme), if any.
	Route func(app string) (port int, scheme string, ok bool)
	// MemAvailable is MemAvailable of /proc/meminfo in bytes.
	MemAvailable func() (uint64, error)
	// MemTotal is MemTotal of /proc/meminfo: the overall cap of views.
	MemTotal func() (uint64, error)
	// CPUs is the number of CPUs (a view gets at most two).
	CPUs int
	// Prefix starts the names of containers and networks (docker.ViewerPrefix).
	Prefix string
	// Policy renders Chromium's managed policy for the app's address
	// (proto.ChromiumPolicy); PolicyRoot holds one folder per view on the
	// host, owned by root, mounted read-only into the container.
	Policy     func(appURL string) ([]byte, error)
	PolicyRoot string
	// HostGuard makes sure the host drops new connections from viewer
	// networks (nft, see guard.go); nil in development mode.
	HostGuard func(ctx context.Context) error
	// Linger keeps a container after the last window closed.
	Linger time.Duration

	store *config.Store

	mu       sync.Mutex
	settings Settings
	image    imageState
	sessions map[sessionKey]*session
	gpus     []GPU
	gpusAt   time.Time
	cleaned  bool
	cleanMu  sync.Mutex
	startMu  sync.Mutex
	devDir   string
	base     context.Context
}

// New makes a manager with the defaults of a NAS.
func New(store *config.Store) *Manager {
	return &Manager{
		store: store, MountRoot: "/mnt", Prefix: docker.ViewerPrefix, PolicyRoot: "/run/titanos/viewer",
		Linger: 2 * time.Minute, MemAvailable: memAvailable, MemTotal: memTotal, CPUs: runtime.NumCPU(), settings: Defaults(),
		sessions: map[sessionKey]*session{}, base: context.Background(),
	}
}

func (m *Manager) ctx() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.base
}

// Load reads the settings; a missing file keeps the defaults.
func (m *Manager) Load() error {
	s := Defaults()
	if err := m.store.ReadJSON(settingsFile, &s); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	m.mu.Lock()
	m.settings = normalize(s)
	m.mu.Unlock()
	return nil
}

// Settings returns the current settings.
func (m *Manager) Settings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings
}

// SetSettings validates and stores the settings. Open views keep theirs.
func (m *Manager) SetSettings(ctx context.Context, s Settings) error {
	if err := Validate(s, m.GPUs(ctx), m.mounted()); err != nil {
		return err
	}
	if err := m.store.WriteJSON(settingsFile, s); err != nil {
		return err
	}
	m.mu.Lock()
	m.settings = s
	m.mu.Unlock()
	return nil
}

func (m *Manager) mounted() []string {
	if m.Mounted == nil {
		return nil
	}
	return m.Mounted()
}

// GPUs lists the devices for the remote view (cached for a minute: NVIDIA
// asks nvidia-smi).
func (m *Manager) GPUs(ctx context.Context) []GPU {
	m.mu.Lock()
	if m.gpus != nil && time.Since(m.gpusAt) < time.Minute {
		out := slices.Clone(m.gpus)
		m.mu.Unlock()
		return out
	}
	m.mu.Unlock()
	var devices []hardware.ContainerDevice
	if m.Devices != nil {
		devices = m.Devices(ctx)
	}
	var leased map[string]bool
	if m.Leased != nil {
		leased = m.Leased()
	}
	gpus := gpusFrom(devices, leased)
	m.mu.Lock()
	m.gpus, m.gpusAt = gpus, time.Now()
	m.mu.Unlock()
	return slices.Clone(gpus)
}

// NewViewGPU is the encoder a view started now would use: vaapi, nvenc or cpu.
func (m *Manager) NewViewGPU(ctx context.Context) string {
	if g := chooseGPU(m.Settings().GPU, m.GPUs(ctx)); g != nil {
		return g.Kind
	}
	return "cpu"
}

// dataVolume is the volume of the image and profiles: the setting, else
// Docker's volume; it must be mounted.
func (m *Manager) dataVolume() (string, error) {
	volume := m.Settings().Volume
	if volume == "" && m.DockerVolume != nil {
		volume = m.DockerVolume()
	}
	if volume == "" || !volumeName.MatchString(volume) || !slices.Contains(m.mounted(), volume) {
		return "", ErrNoVolume
	}
	return volume, nil
}

// Run removes what an earlier titand left (containers with the viewer
// label, app endpoints with the marker alias) once Docker answers.
func (m *Manager) Run(ctx context.Context) {
	m.mu.Lock()
	m.base = ctx
	m.mu.Unlock()
	for {
		if m.cleanup(ctx) == nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

// cleanup runs once; it removes only viewer containers and networks
// (label and name prefix) and only marked endpoints. Views opened later are
// never touched by it.
func (m *Manager) cleanup(ctx context.Context) error {
	m.cleanMu.Lock()
	defer m.cleanMu.Unlock()
	if m.cleaned {
		return nil
	}
	// Each left-over viewer gets its stop time (docker.ViewerStopSeconds).
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	list, err := m.Engine.Viewers(ctx)
	if err != nil {
		return err
	}
	for _, c := range list {
		if err := m.Engine.ViewerRemove(ctx, c.ID); err != nil {
			slog.Warn("left-over viewer not removed", "container", c.Name, "err", err)
		} else {
			slog.Info("left-over viewer removed", "container", c.Name)
		}
	}
	networks, err := m.Engine.ViewerNetworks(ctx)
	if err != nil {
		return err
	}
	for _, network := range networks {
		m.dropNetwork(ctx, network)
	}
	if !m.Dev && m.PolicyRoot != "" {
		os.RemoveAll(m.PolicyRoot)
	}
	m.cleaned = true
	return nil
}

// dropNetwork takes the apps TitanOS attached off a view's network and
// removes it.
func (m *Manager) dropNetwork(ctx context.Context, network string) {
	attached, err := m.Engine.ViewerAttached(ctx, network, attachAlias)
	if err == nil {
		for _, id := range attached {
			if err := m.Engine.ViewerDetach(ctx, network, id, attachAlias); err != nil {
				slog.Warn("app not taken off the viewer network", "network", network, "err", err)
			}
		}
	}
	if err := m.Engine.ViewerRemoveNetwork(ctx, network); err != nil {
		slog.Warn("viewer network not removed", "network", network, "err", err)
	}
}

type sessionKey struct{ user, app string }

type session struct {
	key       sessionKey
	ready     chan struct{}
	err       error
	container string
	name      string
	secret    string
	address   string
	appID     string
	network   string
	policy    string
	gpu       string
	started   time.Time
	viewers   int
	gen       int
	replace   chan struct{}
	linger    *time.Timer
}

// Lease is one window's hold on a running view.
type Lease struct {
	m   *Manager
	s   *session
	gen int
	// Ended closes when the same account opened the app in another window
	// (that window takes the view over, see Replaced) or the view was
	// stopped (Stop).
	Ended <-chan struct{}
	once  sync.Once
}

var (
	appNameRE  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	userNameRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,63}$`)
)

// ContainerName is the name of the viewer of an account and app. A hash
// keeps "a-b"+"c" and "a"+"b-c" apart; the labels name both.
func (m *Manager) ContainerName(user, app string) string {
	return m.Prefix + viewHash(user, app)
}

func viewHash(user, app string) string {
	sum := sha256.Sum256([]byte(user + "\x00" + app))
	return hex.EncodeToString(sum[:8])
}

// BridgeName is the host interface of a view's network: "tvw" and 12 hex
// digits (15 characters, the limit of Linux), matched by the host rules.
func BridgeName(user, app string) string {
	return BridgePrefix + viewHash(user, app)[:12]
}

// BridgePrefix starts the host interfaces of viewer networks.
const BridgePrefix = "tvw"

// Open starts or reuses the view of user on app and returns a lease on
// it. A second window of the same account takes the view over.
func (m *Manager) Open(ctx context.Context, user, app string) (*Lease, error) {
	if !appNameRE.MatchString(app) || !userNameRE.MatchString(user) {
		return nil, ErrName
	}
	if err := m.cleanup(ctx); err != nil {
		return nil, fmt.Errorf("viewer: docker: %w", err)
	}
	key := sessionKey{user, app}
	for {
		m.mu.Lock()
		s := m.sessions[key]
		if s == nil {
			s = &session{key: key, ready: make(chan struct{})}
			m.sessions[key] = s
			m.mu.Unlock()
			err := m.start(ctx, s)
			m.mu.Lock()
			s.err = err
			if err != nil {
				delete(m.sessions, key)
			}
			close(s.ready)
			m.mu.Unlock()
			if err != nil {
				return nil, err
			}
			continue
		}
		select {
		case <-s.ready:
		default:
			m.mu.Unlock()
			select {
			case <-s.ready:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			continue
		}
		if s.err != nil || m.sessions[key] != s {
			m.mu.Unlock()
			continue
		}
		if s.linger != nil {
			s.linger.Stop()
			s.linger = nil
		}
		if s.replace != nil {
			close(s.replace)
		}
		s.gen++
		s.replace = make(chan struct{})
		s.viewers++
		lease := &Lease{m: m, s: s, gen: s.gen, Ended: s.replace}
		m.mu.Unlock()
		return lease, nil
	}
}

// start makes the container of a new view, one at a time. On failure
// nothing is left.
func (m *Manager) start(ctx context.Context, s *session) (err error) {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	image := m.readyImage()
	if image == "" {
		return ErrImageNotReady
	}
	// Docker may have lost it (another data root, removed by hand): fetch
	// it again instead of failing every view until titand restarts.
	if id, err := m.Engine.ViewerImageID(ctx, image); err == nil && id == "" {
		m.mu.Lock()
		if m.image.id == image {
			m.image = imageState{}
		}
		m.mu.Unlock()
		m.readyImage()
		return ErrImageNotReady
	}
	if err := m.admit(ctx, s); err != nil {
		return err
	}
	endpoints, err := m.Engine.AppEndpoints(ctx, s.key.app)
	if err != nil {
		return err
	}
	routePort, scheme := 0, "http"
	if m.Route != nil {
		if port, sch, ok := m.Route(s.key.app); ok {
			routePort = port
			if sch != "" {
				scheme = sch
			}
		}
	}
	target, err := pickTarget(endpoints, routePort)
	if err != nil {
		return err
	}
	profile, err := m.profileDir(s.key.user, s.key.app)
	if err != nil {
		return err
	}
	if m.HostGuard != nil {
		if err := m.HostGuard(ctx); err != nil {
			return fmt.Errorf("viewer: host rules: %w", err)
		}
	}
	name := m.ContainerName(s.key.user, s.key.app)
	// A container of the same view an earlier run left behind.
	if list, err := m.Engine.Viewers(ctx); err == nil {
		for _, c := range list {
			if c.Name == name {
				if err := m.Engine.ViewerRemove(ctx, c.ID); err != nil {
					return err
				}
			}
		}
	}
	// One internal network per view: the viewer and this app only.
	if err := m.Engine.ViewerNetwork(ctx, name, BridgeName(s.key.user, s.key.app), s.key.user, s.key.app); err != nil {
		return err
	}
	s.network = name
	defer func() {
		if err != nil {
			m.dropNetwork(context.WithoutCancel(ctx), name)
		}
	}()
	appAddress, _, err := m.Engine.ViewerAttach(ctx, name, target.id, attachAlias)
	if err != nil {
		return fmt.Errorf("viewer: connect the app: %w", err)
	}
	s.appID = target.id
	if appAddress == "" {
		return errors.New("viewer: the app has no address on the viewer network")
	}
	appURL := fmt.Sprintf("%s://%s%s", scheme, hostPort(appAddress, target.port), target.path)
	policy, err := m.writePolicy(name, appURL)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil && policy != "" {
			os.RemoveAll(filepath.Dir(policy))
		}
	}()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	settings := m.Settings()
	gpu := chooseGPU(settings.GPU, m.GPUs(ctx))
	cpus := int64(min(2, max(1, m.CPUs)))
	spec := docker.ViewerSpec{
		Name: name, Image: image, User: s.key.user, App: s.key.app, Profile: profile, Network: name, Policy: policy,
		MemoryBytes: containerMemory, PidsLimit: 512, ShmBytes: 512 << 20,
		// Apps and VMs win when the CPU is short (Docker's default is 1024).
		NanoCPUs: cpus * 1e9, CPUShares: 512,
	}
	kind := "cpu"
	if gpu != nil {
		kind = gpu.Kind
		switch gpu.Kind {
		case "vaapi":
			spec.Devices = []string{gpu.node}
			spec.GroupAdd = gpu.groups
		case "nvenc":
			spec.CDIDevices = []string{gpu.cdi}
		}
	}
	spec.Env = viewerEnv(appURL, hex.EncodeToString(secret), kind, gpu, settings)
	id, err := m.Engine.ViewerCreate(ctx, spec)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = m.Engine.ViewerRemove(context.WithoutCancel(ctx), id)
		}
	}()
	if err := m.Engine.ViewerStart(ctx, id); err != nil {
		return err
	}
	var address string
	for range 20 {
		var running bool
		if address, running, err = m.Engine.ViewerState(ctx, id, name); err == nil && running && address != "" {
			break
		}
		if err == nil {
			err = errors.New("viewer: the container has no address yet")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if err != nil {
		return err
	}
	m.mu.Lock()
	s.container, s.name, s.secret, s.address, s.gpu, s.policy, s.started = id, name, hex.EncodeToString(secret), address, kind, policy, time.Now()
	m.mu.Unlock()
	slog.Info("remote view started", "user", s.key.user, "app", s.key.app, "gpu", kind)
	return nil
}

// admit checks the limits for a new view: at most PerAccount per account,
// an overall cap from the RAM of the NAS, and memory: the limit of every
// view (open, waiting or starting) is reserved. MemAvailable already
// counts what views use now, so only the rest up to their limit is added.
func (m *Manager) admit(ctx context.Context, s *session) error {
	m.mu.Lock()
	own := 0
	for key, other := range m.sessions {
		if other != s && key.user == s.key.user {
			own++
		}
	}
	m.mu.Unlock()
	if own >= PerAccount {
		return ErrLimit
	}
	reserved, views := m.reserved(ctx, s)
	if views >= m.maxViews() {
		return ErrMemory
	}
	if m.MemAvailable == nil {
		return nil
	}
	avail, err := m.MemAvailable()
	if err != nil {
		return nil
	}
	if avail < Reserve+reserved+PerView {
		return ErrMemory
	}
	return nil
}

// reserved is the memory still promised to the views other than skip (up
// to their limit beyond what they use now) and how many there are.
func (m *Manager) reserved(ctx context.Context, skip *session) (uint64, int) {
	m.mu.Lock()
	views := 0
	var ids []string
	for _, other := range m.sessions {
		if other == skip {
			continue
		}
		views++
		if other.container != "" {
			ids = append(ids, other.container)
		}
	}
	m.mu.Unlock()
	reserved := uint64(views-len(ids)) * PerView // starting: nothing used yet
	for _, id := range ids {
		mctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		used, err := m.Engine.ViewerMemory(mctx, id)
		cancel()
		if err != nil || used > PerView {
			used = 0
		}
		reserved += PerView - used
	}
	return reserved, views
}

// maxViews is the overall cap: what fits into the RAM beside the system.
func (m *Manager) maxViews() int {
	if m.MemTotal == nil {
		return 1 << 20
	}
	total, err := m.MemTotal()
	if err != nil || total == 0 {
		return 1 << 20
	}
	if total <= systemShare+PerView {
		return 1
	}
	return int((total - systemShare) / PerView)
}

// writePolicy writes Chromium's policy for one view into a folder of its
// own below PolicyRoot (root, 0755; the file titanos.json 0444) and returns
// the file, which is mounted read-only.
func (m *Manager) writePolicy(name, appURL string) (string, error) {
	if m.Policy == nil {
		return "", nil
	}
	origin, err := proto.AppOrigin(appURL)
	if err != nil {
		return "", fmt.Errorf("viewer: policy: %w", err)
	}
	data, err := m.Policy(origin)
	if err != nil {
		return "", fmt.Errorf("viewer: policy: %w", err)
	}
	root := m.PolicyRoot
	if m.Dev {
		m.mu.Lock()
		if m.devDir == "" {
			m.devDir, err = os.MkdirTemp("", "titanos-viewer-dev-")
		}
		root = filepath.Join(m.devDir, "policies")
		m.mu.Unlock()
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	dir := filepath.Join(root, name)
	os.RemoveAll(dir)
	if err := os.Mkdir(dir, 0o755); err != nil {
		return "", err
	}
	file := filepath.Join(dir, "titanos.json")
	if err := os.WriteFile(file, data, 0o444); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return file, nil
}

func viewerEnv(target, key, kind string, gpu *GPU, s Settings) []string {
	audio := "0"
	if s.Audio {
		audio = "1"
	}
	env := []string{
		"VIEWER_URL=" + target,
		"VIEWER_KEY=" + key,
		"VIEWER_GPU=" + kind,
		"VIEWER_AUDIO=" + audio,
		"VIEWER_QUALITY=" + s.Quality,
		"VIEWER_FPS=" + strconv.Itoa(s.FPS),
		"VIEWER_TRANSPORT=" + s.Transport,
		"HOME=/profile",
	}
	if gpu != nil && gpu.node != "" {
		env = append(env, "VIEWER_GPU_DEVICE="+gpu.node)
	}
	return env
}

func hostPort(address string, port int) string {
	return address + ":" + strconv.Itoa(port)
}

// target is where the viewer's browser goes: the app container, the port
// inside it and the path of the WebUI label.
type target struct {
	id   string
	port int
	path string
}

// pickTarget finds the container that publishes the app's web port (the
// port of its HTTPS name if it has one, else of its WebUI label) and the
// container port behind it: the viewer reaches the container over the
// internal network, not the port on the NAS.
func pickTarget(endpoints []docker.AppEndpoint, routePort int) (target, error) {
	hostNet := false
	for _, e := range endpoints {
		if e.NetworkMode == "host" || len(e.NetworkMode) > 10 && e.NetworkMode[:10] == "container:" || len(e.NetworkMode) > 8 && e.NetworkMode[:8] == "service:" {
			if webUIPort(e.WebUI) > 0 || routePort > 0 {
				hostNet = true
			}
			continue
		}
		want := routePort
		path := webUIPath(e.WebUI)
		if want == 0 {
			want = webUIPort(e.WebUI)
		}
		if want == 0 {
			continue
		}
		for _, p := range e.Ports {
			if p.Host == want && (p.Protocol == "tcp" || p.Protocol == "") {
				if e.State != "running" {
					return target{}, ErrNotRunning
				}
				return target{id: e.ID, port: p.Container, path: path}, nil
			}
		}
	}
	if hostNet {
		return target{}, ErrHostNetwork
	}
	return target{}, ErrNoWebUI
}

var webUIRE = regexp.MustCompile(`^(?:(\d{1,5})([/?#].*)?|(?i:https?)://\[host\]:(\d{1,5})([/?#].*)?)$`)

// webUIPort reads the port of a WebUI label ("8096", "8096/admin",
// "http://[host]:8096/…"), like appproxy.WebUIPort.
func webUIPort(label string) int {
	m := webUIRE.FindStringSubmatch(label)
	if m == nil {
		return 0
	}
	port, err := strconv.Atoi(m[1] + m[3])
	if err != nil || port < 1 || port > 65535 {
		return 0
	}
	return port
}

// webUIPath is the path of a WebUI label, "/" without one.
func webUIPath(label string) string {
	m := webUIRE.FindStringSubmatch(label)
	if m == nil {
		return "/"
	}
	path := m[2] + m[4]
	if path == "" || path[0] != '/' {
		return "/" + path
	}
	return path
}

// profileDir makes <volume>/.titanos/viewer/profiles/<user>/<app>, the
// folder only the container's user (1000) may use; its parents belong to
// root. In development mode it is a temporary folder.
func (m *Manager) profileDir(user, app string) (string, error) {
	if m.Dev {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.devDir == "" {
			dir, err := os.MkdirTemp("", "titanos-viewer-dev-")
			if err != nil {
				return "", err
			}
			m.devDir = dir
		}
		dir := filepath.Join(m.devDir, user, app)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
		if os.Chown(dir, 1000, 1000) != nil {
			// Not root (a development machine): the container's user must
			// still be able to write there.
			_ = os.Chmod(dir, 0o777) //nolint:gosec // temporary development folder only
		}
		return dir, nil
	}
	parent, err := m.privateDir("profiles", user)
	if err != nil {
		return "", err
	}
	if err := checkSpace(parent, 256<<20); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err := root.Mkdir(app, 0o700); err == nil {
		if err := root.Lchown(app, profileUID, profileUID); err != nil {
			return "", err
		}
	} else if !errors.Is(err, fs.ErrExist) {
		return "", err
	}
	if err := ownedDir(root, app, profileUID, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(parent, app), nil
}

// Check makes sure, right before titand dials the agent, that the view's
// container still runs with the address it had on the view's own network.
// Otherwise the view is stopped and ErrStale returned: open it again.
func (l *Lease) Check(ctx context.Context) error {
	address, running, err := l.m.Engine.ViewerState(ctx, l.s.container, l.s.network)
	if err == nil && running && address == l.s.address {
		return nil
	}
	l.m.mu.Lock()
	current := l.m.sessions[l.s.key] == l.s
	if current {
		delete(l.m.sessions, l.s.key)
		if l.s.linger != nil {
			l.s.linger.Stop()
		}
	}
	l.m.mu.Unlock()
	// This lease is done; the stop below does not count it again.
	l.once.Do(func() {})
	if current {
		l.m.stop(l.s)
	}
	return ErrStale
}

// Close ends the lease. linger keeps the container for Manager.Linger when
// this was the last window (the window was closed or lost its connection);
// otherwise (logout, lock, right taken away, idle) the view stops at once.
func (l *Lease) Close(linger bool) {
	l.once.Do(func() { l.m.release(l.s, linger) })
}

func (m *Manager) release(s *session, linger bool) {
	m.mu.Lock()
	s.viewers--
	if s.viewers > 0 || m.sessions[s.key] != s {
		m.mu.Unlock()
		return
	}
	if linger && m.Linger > 0 {
		s.linger = time.AfterFunc(m.Linger, func() { m.stopIfIdle(s) })
		m.mu.Unlock()
		return
	}
	delete(m.sessions, s.key)
	m.mu.Unlock()
	m.stop(s)
}

func (m *Manager) stopIfIdle(s *session) {
	m.mu.Lock()
	if s.viewers > 0 || m.sessions[s.key] != s {
		m.mu.Unlock()
		return
	}
	delete(m.sessions, s.key)
	m.mu.Unlock()
	m.stop(s)
}

// stop removes the container (the profile stays), the view's network
// (after taking the app off it) and its policy folder.
func (m *Manager) stop(s *session) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := m.Engine.ViewerRemove(ctx, s.container); err != nil {
		slog.Warn("viewer not removed", "user", s.key.user, "app", s.key.app, "err", err)
	}
	if s.network != "" {
		m.dropNetwork(ctx, s.network)
	}
	if s.policy != "" {
		os.RemoveAll(filepath.Dir(s.policy))
	}
	slog.Info("remote view ended", "user", s.key.user, "app", s.key.app)
}

// Stop ends every view of an app or of an account ("" matches all), e.g.
// when an account is removed. Their windows see the connection end.
func (m *Manager) Stop(user, app string) {
	m.StopWhere(func(u, a string) bool { return (user == "" || u == user) && (app == "" || a == app) })
}

// StopWhere ends every view for which match is true (e.g. a right that was
// taken away), including views that wait for their window to come back.
func (m *Manager) StopWhere(match func(user, app string) bool) {
	m.mu.Lock()
	var stopping []*session
	for key, s := range m.sessions {
		select {
		case <-s.ready:
		default:
			continue
		}
		if s.err == nil && match(key.user, key.app) {
			if s.linger != nil {
				s.linger.Stop()
			}
			if s.replace != nil {
				close(s.replace)
				s.replace = nil
			}
			delete(m.sessions, key)
			stopping = append(stopping, s)
		}
	}
	m.mu.Unlock()
	for _, s := range stopping {
		m.stop(s)
	}
}

// AgentURL is the address of the agent's stream. The session key goes in
// the header KeyHeader, never in the address.
func (l *Lease) AgentURL() string {
	return "ws://" + hostPort(l.s.address, agentPort) + "/stream"
}

// HealthURL answers 200 once the agent listens (no key needed).
func (l *Lease) HealthURL() string {
	return "http://" + hostPort(l.s.address, agentPort) + "/healthz"
}

// DownloadURL is the address of a file the browser in the view downloaded;
// send Key in KeyHeader with it.
func (l *Lease) DownloadURL(id string) string {
	return "http://" + hostPort(l.s.address, agentPort) + "/downloads/" + id
}

// KeyHeader carries the session key to the agent.
const KeyHeader = "X-Viewer-Key"

// Key is the session key of the view. Never log it.
func (l *Lease) Key() string { return l.s.secret }

// Replaced tells, once Ended closed, whether another window took the view
// over (rather than the view being stopped).
func (l *Lease) Replaced() bool {
	l.m.mu.Lock()
	defer l.m.mu.Unlock()
	return l.m.sessions[l.s.key] == l.s && l.s.gen != l.gen
}

// GPU is the encoder of the view: "vaapi", "nvenc" or "cpu".
func (l *Lease) GPU() string { return l.s.gpu }

// Peek returns a lease-less handle on a running view of user and app, for
// downloads; ok is false without one.
func (m *Manager) Peek(user, app string) (*Lease, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[sessionKey{user, app}]
	if s == nil || s.address == "" {
		return nil, false
	}
	return &Lease{m: m, s: s}, true
}

// SessionInfo describes a view for the status.
type SessionInfo struct {
	User string `json:"user"`
	App  string `json:"app"`
	// State is "starting", "open" (a window is connected) or "idle" (the
	// container waits Linger for the window to come back).
	State       string    `json:"state"`
	Since       time.Time `json:"since"`
	Windows     int       `json:"windows"`
	GPU         string    `json:"gpu"`
	MemoryBytes uint64    `json:"memoryBytes"`
}

// Sessions lists the views; user "" lists all.
func (m *Manager) Sessions(ctx context.Context, user string) []SessionInfo {
	m.mu.Lock()
	out := []SessionInfo{}
	ids := []string{}
	for key, s := range m.sessions {
		if user != "" && key.user != user {
			continue
		}
		info := SessionInfo{User: key.user, App: key.app, State: "open", Since: s.started, Windows: s.viewers, GPU: s.gpu}
		if s.container == "" {
			info.State = "starting"
		} else if s.viewers == 0 {
			info.State = "idle"
		}
		out = append(out, info)
		ids = append(ids, s.container)
	}
	m.mu.Unlock()
	for i := range out {
		if ids[i] != "" {
			mctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			out[i].MemoryBytes, _ = m.Engine.ViewerMemory(mctx, ids[i])
			cancel()
		}
	}
	slices.SortFunc(out, func(a, b SessionInfo) int {
		if a.User != b.User {
			return compare(a.User, b.User)
		}
		return compare(a.App, b.App)
	})
	return out
}

func compare(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// Memory is the RAM estimate of the status.
type Memory struct {
	AvailableBytes uint64 `json:"availableBytes"`
	ReserveBytes   uint64 `json:"reserveBytes"`
	PerViewBytes   uint64 `json:"perViewBytes"`
	// More is how many further views fit (estimate).
	More int `json:"more"`
}

// MemoryEstimate is free RAM and how many more views fit.
func (m *Manager) MemoryEstimate() Memory {
	out := Memory{ReserveBytes: Reserve, PerViewBytes: PerView}
	if m.MemAvailable == nil {
		return out
	}
	avail, err := m.MemAvailable()
	if err != nil {
		return out
	}
	out.AvailableBytes = avail
	reserved, views := m.reserved(context.Background(), nil)
	if avail > Reserve+reserved {
		out.More = int((avail - Reserve - reserved) / PerView)
	}
	out.More = max(0, min(out.More, m.maxViews()-views))
	return out
}

// memAvailable reads MemAvailable of /proc/meminfo.
func memAvailable() (uint64, error) { return meminfo("MemAvailable:") }

// memTotal reads MemTotal of /proc/meminfo.
func memTotal() (uint64, error) { return meminfo("MemTotal:") }

func meminfo(field string) (uint64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	return parseMeminfo(data, field)
}

func parseMemAvailable(data []byte) (uint64, error) { return parseMeminfo(data, "MemAvailable:") }

func parseMeminfo(data []byte, field string) (uint64, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := bytes.Fields(scanner.Bytes())
		if len(fields) >= 2 && string(fields[0]) == field {
			kb, err := strconv.ParseUint(string(fields[1]), 10, 64)
			if err != nil {
				return 0, err
			}
			return kb << 10, nil
		}
	}
	return 0, errors.New("viewer: " + field + " missing in /proc/meminfo")
}

// DeleteProfiles removes the browser profiles of an account (account
// deleted). Its views must be stopped first (Stop). No link is followed.
func (m *Manager) DeleteProfiles(user string) error {
	if m.Dev || !userNameRE.MatchString(user) {
		return nil
	}
	volume, err := m.dataVolume()
	if err != nil {
		return nil
	}
	root, err := os.OpenRoot(filepath.Join(m.MountRoot, volume))
	if err != nil {
		return err
	}
	defer root.Close()
	path := ""
	for _, part := range []string{".titanos", "viewer", "profiles", user} {
		path = filepath.Join(path, part)
		st, err := root.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !st.IsDir() || st.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", errUnsafeDir, path)
		}
	}
	return root.RemoveAll(path)
}
