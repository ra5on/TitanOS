// Package lxc runs system containers with LXC: a whole Linux system that
// shares the kernel of the NAS, lighter than a VM and closer to a server
// than a Docker app.
//
// What a container is made of is kept in /config/lxc/<name>.json. Its root
// filesystem lies on a volume, in /mnt/<volume>/lxc/<name>/rootfs; the
// folder /mnt/<volume>/lxc is the lxcpath of LXC. The configuration LXC
// reads (<lxcpath>/<name>/config) is written anew from /config before every
// start. A container runs as an instance of titanos-lxc@.service, so that a
// restart of titand (an app update) leaves it running.
package lxc

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

const (
	configDir = "lxc"
	// bridge is the bridge of the NAS that containers join with "bridge",
	// as VMs do.
	bridge = "br0"
	// sampleInterval is how often the running containers are looked at.
	sampleInterval = 5 * time.Second
	maxMounts      = 16
	// idmapBase and idmapSize are the IDs of the NAS an unprivileged
	// container's 0…65535 become (see /etc/subuid in the image).
	idmapBase = 100000
	idmapSize = 65536
)

var (
	ErrDisabled           = errors.New("lxc: not available in development mode")
	ErrUnavailable        = errors.New("lxc: LXC is not installed on this system")
	ErrInvalidName        = errors.New("lxc: invalid name")
	ErrInvalidDisplayName = errors.New("lxc: invalid display name")
	ErrExists             = errors.New("lxc: a container with this name exists")
	ErrNotFound           = errors.New("lxc: not found")
	ErrInvalid            = errors.New("lxc: invalid settings")
	ErrUnknownVolume      = errors.New("lxc: the volume is not available")
	ErrUnknownImage       = errors.New("lxc: the image is not offered by images.linuxcontainers.org")
	ErrRunning            = errors.New("lxc: the container is running")
	ErrNotRunning         = errors.New("lxc: the container is not running")
	ErrNotConfirmed       = errors.New("lxc: deleting the data was not confirmed")
	ErrCreating           = errors.New("lxc: the container is still being created")
	ErrBusy               = errors.New("lxc: another action on this container is running")
	ErrNoBridge           = errors.New("lxc: the network bridge is not set up")
	ErrBadFolder          = errors.New("lxc: the folder is not a folder on a volume")
	ErrMounted            = errors.New("lxc: something is still mounted in the container's folder")
	ErrNoSnapshots        = errors.New("lxc: snapshots need a container on a Btrfs volume")
	ErrSnapshotNotFound   = errors.New("lxc: snapshot not found")
)

var (
	nameRE     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)
	imageRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	macRE      = regexp.MustCompile(`^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)
	targetRE   = regexp.MustCompile(`^/[A-Za-z0-9._/-]{1,200}$`)
	snapshotRE = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}$`)
)

// Mount is a folder of the NAS that appears inside the container.
type Mount struct {
	// Path is the folder on the NAS, below /mnt/<volume>.
	Path string `json:"path"`
	// Target is where the container sees it, e.g. /mnt/media.
	Target   string `json:"target"`
	ReadOnly bool   `json:"readOnly"`
}

// Container is a system container as stored in /config.
type Container struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	// Volume holds the root filesystem, in /mnt/<volume>/lxc/<name>.
	Volume string `json:"volume"`
	// The image from images.linuxcontainers.org the container was made of.
	Distribution string `json:"distribution"`
	Release      string `json:"release"`
	Arch         string `json:"arch"`
	// CPUs and MemoryMiB limit the container; 0 leaves it unlimited.
	CPUs      int `json:"cpus,omitempty"`
	MemoryMiB int `json:"memoryMiB,omitempty"`
	// Network is "bridge" (br0), "network:<id>" (a confirmed bridge of the
	// network settings) or "none" (loopback only).
	Network string  `json:"network"`
	MAC     string  `json:"mac"`
	Mounts  []Mount `json:"mounts,omitempty"`
	// Autostart starts the container once its volume is there after a boot.
	Autostart bool `json:"autostart"`
	// Privileged runs the container's root as root of the NAS. Off by
	// default; fixed at creation, since the files of the root filesystem
	// belong to other IDs in either case.
	Privileged bool `json:"privileged,omitempty"`
	// Btrfs is set when the root filesystem is a Btrfs subvolume, which
	// makes snapshots possible. Fixed at creation.
	Btrfs   bool      `json:"btrfs,omitempty"`
	Created time.Time `json:"created"`
}

// Label is what the web UI shows for a container.
func (c Container) Label() string {
	if c.DisplayName != "" {
		return c.DisplayName
	}
	return c.Name
}

// Job is the creation of a container: downloading and unpacking the image.
type Job struct {
	// State is running or failed.
	State string `json:"state"`
	// Phase is prepare, download or configure.
	Phase   string    `json:"phase"`
	Text    string    `json:"text,omitempty"`
	Started time.Time `json:"started"`
}

// Status is a container with what it is doing right now.
type Status struct {
	Container
	// State is running or stopped.
	State string `json:"state"`
	// Available is false while the volume of the container is not mounted.
	Available bool `json:"available"`
	// CPUPercent is the load in percent of the CPUs the container may use.
	CPUPercent float64 `json:"cpuPercent"`
	// MemoryBytes is the RAM of the NAS the container occupies right now.
	MemoryBytes uint64 `json:"memoryBytes"`
	Job         *Job   `json:"job,omitempty"`
}

type sample struct {
	usage   uint64
	at      time.Time
	percent float64
	memory  uint64
}

// Manager keeps the containers and calls the tools of LXC.
type Manager struct {
	// off is the switch of Settings › Services (service.go).
	off atomic.Bool
	// NetworkBridge returns the device of a confirmed bridge by its ID.
	NetworkBridge func(string) (string, bool)
	// BridgeReady reports whether br0 is set up and confirmed.
	BridgeReady func() bool
	// NetworkLease keeps the network from being rebuilt while a container
	// is attached to it.
	NetworkLease func() (func(), error)
	// Disabled leaves the system alone. Set in development mode.
	Disabled bool

	store  *config.Store
	runner run.Runner
	// usersGID is the group of all accounts; unprivileged containers see it
	// as their group 100 ("users"), so that they can write to shares.
	usersGID  int
	mountRoot string
	// cgroupRoot is /sys/fs/cgroup; state and usage are read there,
	// without waking LXC or systemd.
	cgroupRoot string
	// envDir holds the environment of titanos-lxc@<name>.service (the
	// lxcpath), only while the NAS is up.
	envDir string
	// templateDir holds the configuration snippets of the LXC package.
	templateDir string
	// apparmorParser exists when AppArmor profiles can be generated.
	apparmorParser string
	// lxcStart exists when LXC is installed.
	lxcStart string
	// mountInfo is /proc/self/mountinfo.
	mountInfo string
	// autostartFile keeps autostarted across restarts of titand.
	autostartFile string
	// isBtrfs reports whether a folder lies on Btrfs; tests replace it.
	isBtrfs func(path string) bool
	// fetchIndex loads the image list; tests replace it.
	fetchIndex func(ctx context.Context) ([]byte, error)
	now        func() time.Time

	operations  sync.Map
	mu          sync.Mutex
	loaded      bool
	containers  map[string]Container
	jobs        map[string]*Job
	samples     map[string]*sample
	autostarted map[string]bool
	snapshotOps int

	images imageCache
}

// NewManager returns a manager working on the real system.
func NewManager(store *config.Store, usersGID int) *Manager {
	return &Manager{
		store: store, runner: run.Exec{}, usersGID: usersGID, mountRoot: "/mnt", cgroupRoot: "/sys/fs/cgroup",
		envDir: "/run/titanos-lxc", templateDir: "/usr/share/lxc/config", apparmorParser: "/usr/sbin/apparmor_parser",
		lxcStart: "/usr/bin/lxc-start", mountInfo: "/proc/self/mountinfo", autostartFile: "/run/titanos/lxc-autostarted",
		isBtrfs: onBtrfs, fetchIndex: fetchIndex, now: time.Now,
		containers: map[string]Container{}, jobs: map[string]*Job{}, samples: map[string]*sample{}, autostarted: map[string]bool{},
	}
}

func onBtrfs(path string) bool {
	var info unix.Statfs_t
	return unix.Statfs(path, &info) == nil && info.Type == unix.BTRFS_SUPER_MAGIC
}

// Installed reports whether the tools of LXC are on the system.
func (m *Manager) Installed() bool {
	_, err := os.Stat(m.lxcStart)
	return err == nil
}

// Load reads the containers from /config.
func (m *Manager) Load() error {
	names, err := m.store.List(configDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	containers := map[string]Container{}
	for _, file := range names {
		name, isJSON := strings.CutSuffix(file, ".json")
		if !isJSON || !nameRE.MatchString(name) {
			continue
		}
		var c Container
		if err := m.store.ReadJSON(configDir+"/"+file, &c); err != nil || c.Name != name {
			slog.Warn("container not readable from /config", "file", file, "err", err)
			continue
		}
		containers[name] = c
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.containers, m.loaded = containers, true
	if data, err := os.ReadFile(m.autostartFile); err == nil {
		for _, name := range strings.Fields(string(data)) {
			m.autostarted[name] = true
		}
	}
	return nil
}

// noteAutostarted writes down which containers were autostarted. The lock is held.
func (m *Manager) noteAutostarted() {
	names := make([]string, 0, len(m.autostarted))
	for name := range m.autostarted {
		names = append(names, name)
	}
	sort.Strings(names)
	if err := os.MkdirAll(filepath.Dir(m.autostartFile), 0o700); err == nil {
		if err := os.WriteFile(m.autostartFile, []byte(strings.Join(names, "\n")+"\n"), 0o600); err != nil {
			slog.Warn("autostarted containers not noted", "err", err)
		}
	}
}

// lxcPath is the lxcpath of LXC on a volume.
func (m *Manager) lxcPath(volume string) string {
	return filepath.Join(m.mountRoot, volume, "lxc")
}

func (m *Manager) dir(c Container) string {
	return filepath.Join(m.lxcPath(c.Volume), c.Name)
}

func (m *Manager) rootfs(c Container) string {
	return filepath.Join(m.dir(c), "rootfs")
}

func (m *Manager) unit(name string) string {
	return "titanos-lxc@" + name + ".service"
}

// operation keeps two actions on one container apart.
func (m *Manager) operation(name string) (*sync.Mutex, error) {
	value, _ := m.operations.LoadOrStore(name, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	if !lock.TryLock() {
		return nil, ErrBusy
	}
	return lock, nil
}

func (m *Manager) get(name string) (Container, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.containers[name]
	if !ok {
		return Container{}, ErrNotFound
	}
	return c, nil
}

func (m *Manager) creating(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.jobs[name]
	return job != nil && job.State == "running"
}

// cgroups returns where the container's processes may be: the cgroup LXC
// makes for the payload (at the root as root does by default, or below
// the unit with Delegate=yes) and the cgroup of the unit itself.
func (m *Manager) cgroups(name string) []string {
	unit := filepath.Join(m.cgroupRoot, "system.slice", `system-titanos\x2dlxc.slice`, m.unit(name))
	return []string{
		filepath.Join(m.cgroupRoot, "lxc.payload."+name),
		filepath.Join(unit, "lxc.payload."+name),
		unit,
	}
}

// populated reports whether a cgroup has processes, from cgroup.events.
func populated(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "cgroup.events"))
	return err == nil && strings.Contains(string(data), "populated 1")
}

// running returns the cgroup of a running container, or "".
func (m *Manager) running(name string) string {
	if m.Disabled {
		return ""
	}
	for _, dir := range m.cgroups(name) {
		if populated(dir) {
			return dir
		}
	}
	return ""
}

func available(c Container, mounted []string) bool {
	return slices.Contains(mounted, c.Volume)
}

// Run starts the containers marked for autostart and samples the running
// ones until ctx ends.
func (m *Manager) Run(ctx context.Context, mounted func() []string) {
	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		loaded := m.loaded
		m.mu.Unlock()
		if !loaded {
			_ = m.Load() // Load keeps its own state; a failure shows up as not loaded
		} else if !m.Disabled && !m.off.Load() && m.Installed() {
			m.autostart(ctx, mounted())
			m.sample()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// networkReady reports whether the network a container joins is there.
func (m *Manager) networkReady(c Container) bool {
	switch {
	case c.Network == "none":
		return true
	case c.Network == "bridge":
		return m.BridgeReady == nil || m.BridgeReady()
	default:
		_, ok := m.bridgeOf(c)
		return ok
	}
}

func (m *Manager) autostart(ctx context.Context, mounted []string) {
	if m.Disabled {
		return
	}
	m.mu.Lock()
	var due []string
	for name, c := range m.containers {
		job := m.jobs[name]
		if c.Autostart && !m.autostarted[name] && job == nil && available(c, mounted) && m.networkReady(c) {
			// One attempt per boot: a container someone stopped stays down.
			m.autostarted[name] = true
			due = append(due, name)
		}
	}
	if len(due) > 0 {
		m.noteAutostarted()
	}
	m.mu.Unlock()
	sort.Strings(due)
	for _, name := range due {
		if err := m.Start(ctx, name, mounted); err != nil && !errors.Is(err, ErrRunning) {
			slog.Error("autostart of a container failed", "container", name, "err", err)
		}
	}
}

// sample reads CPU time and memory of the running containers from their cgroups.
func (m *Manager) sample() {
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, c := range m.containers {
		dir := m.running(name)
		if dir == "" {
			delete(m.samples, name)
			continue
		}
		usage := cpuUsage(dir)
		s := m.samples[name]
		if s == nil {
			s = &sample{}
			m.samples[name] = s
		} else if elapsed := now.Sub(s.at).Seconds(); elapsed > 0 && usage >= s.usage {
			cpus := c.CPUs
			if cpus <= 0 {
				cpus = runtime.NumCPU()
			}
			// Microseconds of CPU per second, as a share of what it may use.
			s.percent = min(100, float64(usage-s.usage)/1e6/elapsed/float64(cpus)*100)
		}
		s.usage, s.at = usage, now
		s.memory = readUint(filepath.Join(dir, "memory.current"))
	}
}

func cpuUsage(dir string) uint64 {
	data, _ := os.ReadFile(filepath.Join(dir, "cpu.stat"))
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "usage_usec "); ok {
			n, _ := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
			return n
		}
	}
	return 0
}

func readUint(path string) uint64 {
	data, _ := os.ReadFile(path)
	n, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	return n
}

// List returns all containers with their state.
func (m *Manager) List(mounted []string) []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := make([]Status, 0, len(m.containers))
	for name, c := range m.containers {
		status := Status{Container: c, State: "stopped", Available: available(c, mounted)}
		if job := m.jobs[name]; job != nil {
			copied := *job
			status.Job = &copied
		}
		if m.running(name) != "" {
			status.State = "running"
			if s := m.samples[name]; s != nil {
				status.CPUPercent, status.MemoryBytes = s.percent, s.memory
			}
		}
		list = append(list, status)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// Names returns the names and labels of all containers.
func (m *Manager) Names() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := map[string]string{}
	for name, c := range m.containers {
		names[name] = c.Label()
	}
	return names
}

// RunningNames returns the containers that run right now.
func (m *Manager) RunningNames() map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := map[string]bool{}
	for name := range m.containers {
		if m.running(name) != "" {
			result[name] = true
		}
	}
	return result
}

// Busy reports whether a container is being created or a snapshot is
// being taken or restored. titand does that work itself.
func (m *Manager) Busy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.snapshotOps > 0 {
		return true
	}
	for _, job := range m.jobs {
		if job.State == "running" {
			return true
		}
	}
	return false
}

// NetworkReferences returns the networks containers are attached to, in
// the same form as the VMs report theirs.
func (m *Manager) NetworkReferences() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	refs := []string{}
	for _, c := range m.containers {
		if c.Network == "bridge" || strings.HasPrefix(c.Network, "network:") {
			refs = append(refs, c.Network)
		}
	}
	return refs
}
