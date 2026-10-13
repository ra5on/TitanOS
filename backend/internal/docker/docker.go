// Package docker runs the Docker engine on a volume and manages
// containers and Compose apps.
//
// Everything Docker stores (images, containers, volumes) lives on a volume
// the administrator picks, never in the RAM overlay. The engine therefore
// only runs while that volume is mounted.
package docker

import (
	"context"
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
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

const (
	settingsFile = "docker/settings.json"
	appsDir      = "apps"
	socketPath   = "/run/docker.sock"
	daemonConfig = "/etc/docker/daemon.json"
	// historyLength is the number of CPU samples kept per container for
	// the sparkline (one every sampleInterval).
	historyLength  = 30
	sampleInterval = 5 * time.Second
	// updateInterval is how often the registries are asked for newer images.
	// Registries limit anonymous requests, so not too often.
	updateInterval = 12 * time.Hour
)

var (
	ErrNotLoaded     = errors.New("docker: configuration could not be read from /config")
	ErrUnknownVolume = errors.New("docker: no such volume")
	ErrNotRunning    = errors.New("docker: the engine is not running")
	ErrInvalidName   = errors.New("docker: invalid app name")
	ErrExists        = errors.New("docker: an app with this name exists")
	ErrNotFound      = errors.New("docker: not found")
	ErrBusy          = errors.New("docker: the app is being set up")
	ErrInvalid       = errors.New("docker: invalid compose file")
	ErrInvalidEnv    = errors.New("docker: invalid variable")
	ErrDisabled      = errors.New("docker: not available in development mode")
)

// Settings is /config/docker/settings.json.
type Settings struct {
	Enabled bool `json:"enabled"`
	// Volume holds Docker's data and the apps' data folders.
	Volume string `json:"volume"`
}

// Port is a published port of a container.
type Port struct {
	Host      int    `json:"host"`
	Container int    `json:"container"`
	Protocol  string `json:"protocol"`
}

// Container is a container as shown in the UI.
type Container struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Image string `json:"image"`
	// State is Docker's: running, exited, created, restarting, paused, dead.
	State string `json:"state"`
	// Since is when the container started, or stopped if it is not running.
	Since time.Time `json:"since"`
	Ports []Port    `json:"ports"`
	// App is the Compose project the container belongs to, if any.
	App     string `json:"app"`
	Service string `json:"service"`
	// Managed is true for apps whose compose file TitanOS keeps in /config.
	Managed     bool      `json:"managed"`
	CPUPercent  float64   `json:"cpuPercent"`
	MemoryBytes uint64    `json:"memoryBytes"`
	CPUHistory  []float64 `json:"cpuHistory"`
	// WebUI is the address of the app's own web interface, from the label
	// "titanos.webui": a host port ("8096"), a port with a path
	// ("8096/admin") or a full URL. Empty if the label is not set.
	WebUI string `json:"webui"`
	// UpdateAvailable is true when the registry has a newer image for the
	// container's tag than the one it runs.
	UpdateAvailable bool `json:"updateAvailable"`
}

// WebUILabel tells TitanOS where the web interface of a container is.
const WebUILabel = "titanos.webui"

// Job is the setup of an app: pulling images and creating containers.
type Job struct {
	State  string `json:"state"` // running, done, failed
	Output string `json:"output"`
}

// Status is everything the Docker page shows.
type Status struct {
	Settings Settings `json:"settings"`
	// Running is whether the engine answers.
	Running    bool           `json:"running"`
	Version    string         `json:"version"`
	Containers []Container    `json:"containers"`
	Apps       []string       `json:"apps"`
	Jobs       map[string]Job `json:"jobs"`
	// UpdatesChecked is when the registries were last asked; zero if never.
	UpdatesChecked time.Time `json:"updatesChecked"`
}

type sample struct {
	cpu, system uint64
	history     []float64
	memory      uint64
	since       time.Time
}

// Manager keeps the engine in line with the settings and answers the UI.
type Manager struct {
	store  *config.Store
	runner run.Runner
	api    *client
	// Disabled leaves the system alone. Set in development mode.
	Disabled bool
	// configDir is where /config lives, for the paths given to docker compose.
	configDir    string
	mountRoot    string
	daemonConfig string

	mu       sync.Mutex
	settings Settings
	loaded   bool
	applied  string // the data root the engine was last started with
	samples  map[string]*sample
	jobs     map[string]*Job
	appList  []string // apps in /config; nil until read

	// What the registries said the last time they were asked.
	remote         map[string]string   // image reference → digest
	local          map[string][]string // image ID → digests it is known under
	updatesChecked time.Time
}

// NewManager returns a manager working on the real system.
func NewManager(store *config.Store, configDir string) *Manager {
	return &Manager{
		store: store, runner: run.Exec{}, api: newClient(socketPath), configDir: configDir, mountRoot: "/mnt", daemonConfig: daemonConfig,
		samples: map[string]*sample{}, jobs: map[string]*Job{}, remote: map[string]string{}, local: map[string][]string{},
	}
}

// Load reads the settings from /config.
func (m *Manager) Load() error {
	var settings Settings
	err := m.store.ReadJSON(settingsFile, &settings)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings = settings
	m.loaded = true
	return nil
}

// SetSettings turns Docker on or off and picks its volume.
func (m *Manager) SetSettings(settings Settings, volumes []string) error {
	if m.Disabled {
		return ErrDisabled
	}
	if settings.Enabled && !slices.Contains(volumes, settings.Volume) {
		return ErrUnknownVolume
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

// Current returns the settings in use.
func (m *Manager) Current() Settings { return m.current() }

func (m *Manager) current() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings
}

// dataRoot is where Docker keeps its data on the volume.
func (m *Manager) dataRoot(volume string) string {
	return filepath.Join(m.mountRoot, volume, ".titanos", "docker")
}

// appData is the folder relative paths in an app's compose file refer to.
func (m *Manager) appData(volume, app string) string {
	return filepath.Join(m.mountRoot, volume, "appdata", app)
}

// Run starts and stops the engine as settings and mounted volumes change,
// and samples the containers, until ctx is done.
func (m *Manager) Run(ctx context.Context, mounted func() []string) {
	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		loaded := m.loaded
		m.mu.Unlock()
		if !loaded {
			m.Load()
		} else if !m.Disabled {
			if err := m.reconcile(ctx, mounted()); err != nil {
				slog.Error("docker engine", "err", err)
			}
			m.sample(ctx)
			m.mu.Lock()
			due := m.applied != "" && time.Since(m.updatesChecked) > updateInterval
			m.mu.Unlock()
			if due {
				m.CheckUpdates(ctx)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// reconcile makes the engine run exactly when Docker is enabled and its
// volume is mounted.
func (m *Manager) reconcile(ctx context.Context, mounted []string) error {
	settings := m.current()
	want := ""
	if settings.Enabled && slices.Contains(mounted, settings.Volume) {
		want = m.dataRoot(settings.Volume)
	}
	m.mu.Lock()
	applied := m.applied
	m.mu.Unlock()
	if want == applied {
		return nil
	}

	// Stop first: the engine must never run with a data root that is gone,
	// and a changed data root needs a restart anyway.
	if _, err := m.runner.Run(ctx, "", "systemctl", "stop", "docker.socket", "docker.service"); err != nil {
		return err
	}
	if want != "" {
		if err := os.MkdirAll(want, 0o710); err != nil {
			return err
		}
		daemon, _ := json.MarshalIndent(map[string]any{
			"data-root":  want,
			"log-driver": "json-file",
			// Logs live on the volume too; keep them from growing without end.
			"log-opts": map[string]string{"max-size": "10m", "max-file": "3"},
		}, "", "  ")
		if err := os.MkdirAll(filepath.Dir(m.daemonConfig), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(m.daemonConfig, append(daemon, '\n'), 0o644); err != nil {
			return err
		}
		if _, err := m.runner.Run(ctx, "", "systemctl", "start", "docker.service"); err != nil {
			return err
		}
		slog.Info("docker engine started", "data", want)
	} else if applied != "" {
		slog.Info("docker engine stopped")
	}
	m.mu.Lock()
	m.applied = want
	m.mu.Unlock()
	return nil
}

// sample reads CPU and memory of the running containers.
func (m *Manager) sample(ctx context.Context) {
	m.mu.Lock()
	running := m.applied != ""
	m.mu.Unlock()
	if !running {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, sampleInterval)
	defer cancel()
	containers, err := m.api.containers(ctx)
	if err != nil {
		return
	}
	seen := map[string]bool{}
	for _, c := range containers {
		seen[c.ID] = true
		var inspect apiInspect
		m.api.getJSON(ctx, "/containers/"+c.ID+"/json", nil, &inspect)
		since := inspect.State.FinishedAt
		var stats apiStats
		if c.State == "running" {
			since = inspect.State.StartedAt
			stats, _ = m.api.stats(ctx, c.ID)
		}

		m.mu.Lock()
		s := m.samples[c.ID]
		if s == nil {
			s = &sample{}
			m.samples[c.ID] = s
		}
		percent := 0.0
		if c.State == "running" && s.system > 0 && stats.CPUStats.SystemUsage > s.system && stats.CPUStats.CPUUsage.TotalUsage >= s.cpu {
			cpus := max(stats.CPUStats.OnlineCPUs, 1)
			percent = 100 * float64(cpus) * float64(stats.CPUStats.CPUUsage.TotalUsage-s.cpu) / float64(stats.CPUStats.SystemUsage-s.system)
		}
		s.cpu, s.system = stats.CPUStats.CPUUsage.TotalUsage, stats.CPUStats.SystemUsage
		s.history = append(s.history, percent)
		if len(s.history) > historyLength {
			s.history = s.history[len(s.history)-historyLength:]
		}
		// What `docker stats` shows: usage without the page cache.
		s.memory = stats.MemoryStats.Usage
		if cache := stats.MemoryStats.Stats["inactive_file"]; cache < s.memory {
			s.memory -= cache
		}
		s.since = since
		m.mu.Unlock()
	}
	m.mu.Lock()
	for id := range m.samples {
		if !seen[id] {
			delete(m.samples, id)
		}
	}
	m.mu.Unlock()
}

// Status returns settings, engine state, containers and apps.
func (m *Manager) Status(ctx context.Context) Status {
	status := Status{Settings: m.current(), Containers: []Container{}, Apps: []string{}, Jobs: map[string]Job{}}
	m.mu.Lock()
	for name, job := range m.jobs {
		status.Jobs[name] = *job
	}
	apps := m.knownApps()
	m.mu.Unlock()
	status.Apps = apps
	if m.Disabled {
		return status
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	version, err := m.api.version(ctx)
	if err != nil {
		return status
	}
	status.Running, status.Version = true, version
	containers, err := m.api.containers(ctx)
	if err != nil {
		return status
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	status.UpdatesChecked = m.updatesChecked
	for _, c := range containers {
		container := Container{
			ID: c.ID, Image: c.Image, State: c.State, Ports: []Port{}, CPUHistory: []float64{},
			App: c.Labels["com.docker.compose.project"], Service: c.Labels["com.docker.compose.service"],
		}
		if len(c.Names) > 0 {
			container.Name = strings.TrimPrefix(c.Names[0], "/")
		}
		container.Managed = slices.Contains(apps, container.App)
		container.WebUI = c.Labels[WebUILabel]
		if remote := m.remote[c.Image]; remote != "" {
			if digests, known := m.local[c.ImageID]; known && !slices.Contains(digests, remote) {
				container.UpdateAvailable = true
			}
		}
		for _, p := range c.Ports {
			port := Port{Host: p.PublicPort, Container: p.PrivatePort, Protocol: p.Type}
			// Docker lists a port once per address family.
			if p.PublicPort != 0 && !slices.Contains(container.Ports, port) {
				container.Ports = append(container.Ports, port)
			}
		}
		sort.Slice(container.Ports, func(i, j int) bool { return container.Ports[i].Host < container.Ports[j].Host })
		if s := m.samples[c.ID]; s != nil {
			container.Since = s.since
			container.MemoryBytes = s.memory
			container.CPUHistory = slices.Clone(s.history)
			if n := len(s.history); n > 0 {
				container.CPUPercent = s.history[n-1]
			}
		}
		status.Containers = append(status.Containers, container)
	}
	sort.Slice(status.Containers, func(i, j int) bool { return status.Containers[i].Name < status.Containers[j].Name })
	return status
}

var containerIDRE = regexp.MustCompile(`^[a-f0-9]{12,64}$`)

// Action starts, stops or restarts a container.
func (m *Manager) Action(ctx context.Context, id, action string) error {
	if m.Disabled {
		return ErrDisabled
	}
	if !containerIDRE.MatchString(id) || !slices.Contains([]string{"start", "stop", "restart"}, action) {
		return ErrNotFound
	}
	return mapAPIError(m.api.post(ctx, "/containers/"+id+"/"+action, nil))
}

// Remove stops and deletes a container. Its data in mounted folders stays.
func (m *Manager) Remove(ctx context.Context, id string) error {
	if m.Disabled {
		return ErrDisabled
	}
	if !containerIDRE.MatchString(id) {
		return ErrNotFound
	}
	res, err := m.api.do(ctx, "DELETE", "/containers/"+id, map[string][]string{"force": {"true"}})
	if err == nil {
		res.Body.Close()
	}
	return mapAPIError(err)
}

// Logs returns the last lines of a container's output.
func (m *Manager) Logs(ctx context.Context, id string, tail int) (string, error) {
	if m.Disabled {
		return "", ErrDisabled
	}
	if !containerIDRE.MatchString(id) {
		return "", ErrNotFound
	}
	text, err := m.api.logs(ctx, id, min(max(tail, 1), 2000))
	return text, mapAPIError(err)
}

func mapAPIError(err error) error {
	var api *apiError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &api) && api.Status == 404:
		return ErrNotFound
	case errors.As(err, &api):
		return err
	default:
		return fmt.Errorf("%w: %v", ErrNotRunning, err)
	}
}

// CheckUpdates asks the registries whether the images of the containers
// have newer versions. It needs the network and can take a while.
func (m *Manager) CheckUpdates(ctx context.Context) error {
	if m.Disabled {
		return ErrDisabled
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	containers, err := m.api.containers(ctx)
	if err != nil {
		return mapAPIError(err)
	}
	remote := map[string]string{}
	local := map[string][]string{}
	for _, c := range containers {
		// An image named by its ID or digest has no tag that could move.
		if strings.HasPrefix(c.Image, "sha256:") || strings.Contains(c.Image, "@") {
			continue
		}
		if _, done := local[c.ImageID]; !done {
			local[c.ImageID], _ = m.api.imageDigests(ctx, c.ImageID)
		}
		if _, done := remote[c.Image]; !done {
			digest, err := m.api.remoteDigest(ctx, c.Image)
			if err != nil {
				slog.Debug("update check", "image", c.Image, "err", err)
			}
			remote[c.Image] = digest
		}
	}
	m.mu.Lock()
	m.remote, m.local, m.updatesChecked = remote, local, time.Now()
	m.mu.Unlock()
	return nil
}

// Console opens a shell inside a running container. The returned function
// tells the shell the size of the terminal.
func (m *Manager) Console(ctx context.Context, id string) (io.ReadWriteCloser, func(cols, rows int), error) {
	if m.Disabled {
		return nil, nil, ErrDisabled
	}
	if !containerIDRE.MatchString(id) {
		return nil, nil, ErrNotFound
	}
	// bash where the image has it, sh everywhere else.
	exec, err := m.api.execCreate(ctx, id, []string{"/bin/sh", "-c", "command -v bash >/dev/null 2>&1 && exec bash || exec sh"})
	if err != nil {
		return nil, nil, mapAPIError(err)
	}
	stream, err := m.api.execStart(ctx, exec)
	if err != nil {
		return nil, nil, mapAPIError(err)
	}
	resize := func(cols, rows int) {
		if cols > 0 && rows > 0 && cols <= 1000 && rows <= 1000 {
			m.api.execResize(context.WithoutCancel(ctx), exec, cols, rows)
		}
	}
	return stream, resize, nil
}

// AppsWithUpdates returns the managed apps that have a container with a
// newer image available.
func (m *Manager) AppsWithUpdates(ctx context.Context) []string {
	var apps []string
	for _, c := range m.Status(ctx).Containers {
		if c.Managed && c.UpdateAvailable && !slices.Contains(apps, c.App) {
			apps = append(apps, c.App)
		}
	}
	return apps
}
