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
	"crypto/rand"
	"errors"
	"fmt"
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
	"time"
	"unicode"

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
			m.Load()
		} else if !m.Disabled && m.Installed() {
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

func validDisplayName(name string) bool {
	if name == "" {
		return true
	}
	if strings.TrimSpace(name) != name || len([]rune(name)) > 80 {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || (r >= 0x200b && r <= 0x200f) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) || r == 0xfeff {
			return false
		}
	}
	return true
}

func newMAC() string {
	b := make([]byte, 3)
	rand.Read(b)
	// 00:16:3e is the prefix LXC itself uses for containers.
	return fmt.Sprintf("00:16:3e:%02x:%02x:%02x", b[0], b[1], b[2])
}

// bridgeOf returns the bridge a container joins.
func (m *Manager) bridgeOf(c Container) (string, bool) {
	if c.Network == "bridge" {
		return bridge, true
	}
	if id, ok := strings.CutPrefix(c.Network, "network:"); ok && m.NetworkBridge != nil {
		return m.NetworkBridge(id)
	}
	return "", false
}

// check validates the settings that can change after creation and
// normalizes them. The folders are resolved to their real paths.
func (m *Manager) check(c *Container) error {
	if !validDisplayName(c.DisplayName) {
		return ErrInvalidDisplayName
	}
	if len(c.Description) > 2000 {
		return fmt.Errorf("%w: description too long", ErrInvalid)
	}
	if c.CPUs < 0 || c.CPUs > runtime.NumCPU() {
		return fmt.Errorf("%w: CPUs", ErrInvalid)
	}
	if c.MemoryMiB != 0 && (c.MemoryMiB < 64 || c.MemoryMiB > 1<<22) {
		return fmt.Errorf("%w: memory", ErrInvalid)
	}
	switch {
	case c.Network == "" || c.Network == "none":
		c.Network = "none"
	case c.Network == "bridge":
	case strings.HasPrefix(c.Network, "network:") && len(c.Network) > len("network:"):
	default:
		return fmt.Errorf("%w: network", ErrInvalid)
	}
	c.MAC = strings.ToLower(strings.TrimSpace(c.MAC))
	if c.MAC == "" {
		c.MAC = newMAC()
	}
	// A unicast address: the lowest bit of the first byte is clear.
	if !macRE.MatchString(c.MAC) || c.MAC == "00:00:00:00:00:00" {
		return fmt.Errorf("%w: MAC address", ErrInvalid)
	}
	if first, _ := strconv.ParseUint(c.MAC[:2], 16, 8); first&1 != 0 {
		return fmt.Errorf("%w: MAC address", ErrInvalid)
	}
	return m.checkMounts(c)
}

// systemTargets are places in the container no folder of the NAS may cover.
var systemTargets = []string{"/bin", "/boot", "/dev", "/etc", "/lib", "/lib64", "/proc", "/run", "/sbin", "/sys", "/usr", "/var"}

func (m *Manager) checkMounts(c *Container) error {
	if len(c.Mounts) > maxMounts {
		return fmt.Errorf("%w: too many folders", ErrInvalid)
	}
	targets := map[string]bool{}
	for i := range c.Mounts {
		mount := &c.Mounts[i]
		real, err := m.onVolume(mount.Path)
		if err != nil {
			return err
		}
		mount.Path = real
		target := filepath.Clean(mount.Target)
		if !targetRE.MatchString(mount.Target) || target != mount.Target || target == "/" || strings.Contains(target, "/../") || strings.HasSuffix(target, "/..") {
			return fmt.Errorf("%w: the place in the container must be an absolute path without spaces", ErrInvalid)
		}
		for _, system := range systemTargets {
			if target == system || strings.HasPrefix(target, system+"/") && (system == "/dev" || system == "/proc" || system == "/sys" || system == "/run") {
				return fmt.Errorf("%w: %s is part of the container's system", ErrInvalid, target)
			}
		}
		if targets[target] {
			return fmt.Errorf("%w: two folders at %s", ErrInvalid, target)
		}
		targets[target] = true
	}
	if len(c.Mounts) == 0 {
		c.Mounts = nil
	}
	return nil
}

// onVolume makes sure a folder is a folder of a volume, not one of the
// private folders of TitanOS or of LXC, and returns its real path.
func (m *Manager) onVolume(path string) (string, error) {
	// A control character could end the line of the LXC configuration
	// the folder is written to and smuggle in further keys.
	if strings.ContainsFunc(path, unicode.IsControl) {
		return "", ErrBadFolder
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", ErrBadFolder
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil || strings.ContainsFunc(real, unicode.IsControl) {
		return "", ErrBadFolder
	}
	relative, found := strings.CutPrefix(real, m.mountRoot+"/")
	parts := strings.Split(relative, "/")
	if !found || len(parts) < 2 {
		// The whole volume is not handed to a container.
		return "", ErrBadFolder
	}
	for _, part := range parts[1:] {
		if strings.HasPrefix(part, ".") {
			return "", ErrBadFolder
		}
	}
	if parts[1] == "lxc" {
		return "", ErrBadFolder
	}
	if info, err := os.Stat(real); err != nil || !info.IsDir() {
		return "", ErrBadFolder
	}
	return real, nil
}

// Create adds a container and makes its root filesystem from an image of
// images.linuxcontainers.org in the background.
func (m *Manager) Create(ctx context.Context, c Container, mounted []string) error {
	if m.Disabled {
		return ErrDisabled
	}
	if !m.Installed() {
		return ErrUnavailable
	}
	if !nameRE.MatchString(c.Name) {
		return ErrInvalidName
	}
	if !slices.Contains(mounted, c.Volume) {
		return ErrUnknownVolume
	}
	if c.Arch == "" {
		c.Arch = hostArch()
	}
	if !imageRE.MatchString(c.Distribution) || !imageRE.MatchString(c.Release) || !imageRE.MatchString(c.Arch) {
		return ErrUnknownImage
	}
	images, err := m.Images(ctx)
	if err != nil && len(images) == 0 {
		return err
	}
	if !slices.ContainsFunc(images, func(image Image) bool {
		return image.Distribution == c.Distribution && image.Release == c.Release && image.Arch == c.Arch
	}) {
		return ErrUnknownImage
	}
	if err := m.check(&c); err != nil {
		return err
	}
	if c.Network != "none" && !m.networkReady(c) {
		return ErrNoBridge
	}
	c.Created = m.now().UTC().Truncate(time.Second)
	c.Btrfs = m.isBtrfs(filepath.Join(m.mountRoot, c.Volume))

	lock, err := m.operation(c.Name)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if _, exists := m.containers[c.Name]; exists {
		m.mu.Unlock()
		lock.Unlock()
		return ErrExists
	}
	if _, err := os.Lstat(m.dir(c)); err == nil {
		// A folder left behind, e.g. of a container removed with its data
		// kept: never write over it.
		m.mu.Unlock()
		lock.Unlock()
		return fmt.Errorf("%w: %s already exists", ErrExists, m.dir(c))
	}
	if err := m.store.WriteJSON(configDir+"/"+c.Name+".json", c); err != nil {
		m.mu.Unlock()
		lock.Unlock()
		return err
	}
	m.containers[c.Name] = c
	job := &Job{State: "running", Phase: "prepare", Started: m.now()}
	m.jobs[c.Name] = job
	m.mu.Unlock()

	go func() {
		defer lock.Unlock()
		// The request that asked for it ends long before the download does.
		err := m.build(context.WithoutCancel(ctx), c, job)
		m.mu.Lock()
		defer m.mu.Unlock()
		if err != nil {
			slog.Error("container not created", "container", c.Name, "err", err)
			job.State, job.Text = "failed", errorText(err)
			return
		}
		slog.Info("container created", "container", c.Name, "image", c.Distribution+"/"+c.Release)
		delete(m.jobs, c.Name)
	}()
	return nil
}

func errorText(err error) string {
	var command *run.CommandError
	if errors.As(err, &command) && command.Output != "" {
		output := command.Output
		if len(output) > 4000 {
			output = output[len(output)-4000:]
		}
		return output
	}
	return err.Error()
}

func (m *Manager) setPhase(job *Job, phase string) {
	m.mu.Lock()
	job.Phase = phase
	m.mu.Unlock()
}

// build runs lxc-create with the download template. Its cache and
// temporary files lie on the volume, never in the RAM of the NAS.
func (m *Manager) build(ctx context.Context, c Container, job *Job) error {
	path := m.lxcPath(c.Volume)
	cache, tmp := filepath.Join(path, ".cache"), filepath.Join(path, ".tmp")
	for _, dir := range []string{path, cache, tmp} {
		// Unprivileged containers' root must pass through the lxcpath.
		if err := os.Mkdir(dir, 0o711); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
			return ErrBadFolder
		}
	}
	os.Chmod(tmp, 0o700)
	os.Chmod(cache, 0o700)
	initial := filepath.Join(tmp, c.Name+".conf")
	if err := os.WriteFile(initial, []byte(m.Config(c, true)), 0o600); err != nil {
		return err
	}
	defer os.Remove(initial)
	m.setPhase(job, "download")
	backing := "dir"
	if c.Btrfs {
		backing = "btrfs"
	}
	_, err := m.runner.Run(ctx, "", "env", "LXC_CACHE_PATH="+cache, "TMPDIR="+tmp,
		"lxc-create", "-P", path, "-n", c.Name, "-B", backing, "-f", initial, "-t", "download", "--",
		"--dist", c.Distribution, "--release", c.Release, "--arch", c.Arch)
	if err != nil {
		return err
	}
	m.setPhase(job, "configure")
	// The template wrote a configuration of its own; from now on the one
	// made from /config is used.
	return m.writeConfig(c)
}

func hostArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "amd64"
}

// writeConfig writes the configuration LXC reads for a container.
func (m *Manager) writeConfig(c Container) error {
	dir := m.dir(c)
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("%w: %s is missing", ErrBadFolder, dir)
	}
	path := filepath.Join(dir, "config")
	tmp := path + ".titanos-new"
	if err := os.WriteFile(tmp, []byte(m.Config(c, false)), 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Change is what can be changed in a container after it was made.
type Change struct {
	DisplayName *string `json:"displayName"`
	Description string  `json:"description"`
	CPUs        int     `json:"cpus"`
	MemoryMiB   int     `json:"memoryMiB"`
	Network     string  `json:"network"`
	MAC         string  `json:"mac"`
	Mounts      []Mount `json:"mounts"`
	Autostart   bool    `json:"autostart"`
}

// Update changes the settings of a container. A running container gets
// them with its next start.
func (m *Manager) Update(name string, change Change) error {
	lock, err := m.operation(name)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	c, err := m.get(name)
	if err != nil {
		return err
	}
	if change.DisplayName != nil {
		c.DisplayName = *change.DisplayName
	}
	c.Description, c.CPUs, c.MemoryMiB, c.Network, c.MAC, c.Mounts, c.Autostart = change.Description, change.CPUs, change.MemoryMiB, change.Network, change.MAC, change.Mounts, change.Autostart
	if err := m.check(&c); err != nil {
		return err
	}
	if err := m.store.WriteJSON(configDir+"/"+name+".json", c); err != nil {
		return err
	}
	m.mu.Lock()
	m.containers[name] = c
	m.mu.Unlock()
	return nil
}

// Start writes the configuration of a container and starts it as an
// instance of titanos-lxc@.service.
func (m *Manager) Start(ctx context.Context, name string, mounted []string) error {
	if m.Disabled {
		return ErrDisabled
	}
	lock, err := m.operation(name)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	c, err := m.get(name)
	if err != nil {
		return err
	}
	if m.creating(name) {
		return ErrCreating
	}
	if !available(c, mounted) {
		return ErrUnknownVolume
	}
	if m.running(name) != "" {
		return ErrRunning
	}
	if c.Network != "none" {
		if !m.networkReady(c) {
			return ErrNoBridge
		}
		if m.NetworkLease != nil {
			release, err := m.NetworkLease()
			if err != nil {
				return ErrNoBridge
			}
			defer release()
		}
	}
	// The folders may have gone since they were chosen.
	if err := m.checkMounts(&c); err != nil {
		return err
	}
	if err := m.writeConfig(c); err != nil {
		return err
	}
	if err := os.MkdirAll(m.envDir, 0o700); err != nil {
		return err
	}
	env := "LXC_PATH=" + m.lxcPath(c.Volume) + "\n"
	if err := os.WriteFile(filepath.Join(m.envDir, name+".env"), []byte(env), 0o600); err != nil {
		return err
	}
	_, err = m.runner.Run(ctx, "", "systemctl", "start", m.unit(name))
	return err
}

// Action stops ("stop"), forcibly stops ("kill") or restarts ("restart") a container.
func (m *Manager) Action(ctx context.Context, name, action string, mounted []string) error {
	if m.Disabled {
		return ErrDisabled
	}
	if action == "start" {
		return m.Start(ctx, name, mounted)
	}
	if action != "stop" && action != "kill" && action != "restart" {
		return ErrNotFound
	}
	lock, err := m.operation(name)
	if err != nil {
		return err
	}
	c, err := m.get(name)
	if err != nil {
		lock.Unlock()
		return err
	}
	if m.running(name) == "" {
		lock.Unlock()
		return ErrNotRunning
	}
	if action == "kill" {
		// Pulling the plug, then the unit notices its end.
		if _, err := m.runner.Run(ctx, "", "lxc-stop", "-P", m.lxcPath(c.Volume), "-n", name, "-k"); err != nil {
			lock.Unlock()
			return err
		}
	}
	// The unit's ExecStop asks the container to shut down, for up to a minute.
	_, err = m.runner.Run(ctx, "", "systemctl", "stop", m.unit(name))
	lock.Unlock()
	if err != nil || action != "restart" {
		return err
	}
	return m.Start(ctx, name, mounted)
}

// mountedBelow reports whether anything is mounted in or below a folder.
func (m *Manager) mountedBelow(dir string) bool {
	data, err := os.ReadFile(m.mountInfo)
	if err != nil {
		// Unknown: assume the worst.
		return true
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		point := strings.ReplaceAll(fields[4], `\040`, " ")
		if point == dir || strings.HasPrefix(point, dir+"/") {
			return true
		}
	}
	return false
}

// Delete forgets a container. Its folder stays unless deleteData is set,
// which needs the name of the container once more as confirmation.
func (m *Manager) Delete(ctx context.Context, name string, deleteData bool, confirm string) error {
	if m.Disabled {
		return ErrDisabled
	}
	lock, err := m.operation(name)
	if err != nil {
		if m.creating(name) {
			return ErrCreating
		}
		return err
	}
	defer lock.Unlock()
	c, err := m.get(name)
	if err != nil {
		return err
	}
	if m.running(name) != "" {
		return ErrRunning
	}
	if deleteData && confirm != name && confirm != c.Label() {
		return ErrNotConfirmed
	}
	dir := m.dir(c)
	if deleteData {
		// A stopped container has nothing mounted; anything that still is
		// could be a folder of the NAS, which must not be deleted with it.
		if m.mountedBelow(dir) {
			return ErrMounted
		}
	}
	if err := m.store.Remove(configDir + "/" + name + ".json"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.mu.Lock()
	delete(m.containers, name)
	delete(m.jobs, name)
	delete(m.samples, name)
	delete(m.autostarted, name)
	m.mu.Unlock()
	os.Remove(filepath.Join(m.envDir, name+".env"))
	if !deleteData {
		return nil
	}
	if c.Btrfs {
		// Snapshots and the root filesystem are subvolumes.
		snapshots, _ := m.snapshotNames(c)
		for _, id := range snapshots {
			m.runner.Run(ctx, "", "btrfs", "subvolume", "delete", filepath.Join(m.snapshotDir(c), id))
		}
		if _, err := os.Lstat(m.rootfs(c)); err == nil {
			if _, err := m.runner.Run(ctx, "", "btrfs", "subvolume", "delete", m.rootfs(c)); err != nil {
				slog.Warn("root filesystem of a removed container is no subvolume", "container", name, "err", err)
			}
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		slog.Warn("folder of a removed container not deleted", "container", name, "err", err)
		return err
	}
	return nil
}

// ConsoleCommand returns the command for a console of a running
// container: a login shell inside it ("shell"), or its own text console
// ("console", leave with Ctrl+a q).
func (m *Manager) ConsoleCommand(name, mode string) ([]string, error) {
	if m.Disabled {
		return nil, ErrDisabled
	}
	c, err := m.get(name)
	if err != nil {
		return nil, err
	}
	if m.running(name) == "" {
		return nil, ErrNotRunning
	}
	path := m.lxcPath(c.Volume)
	if mode == "console" {
		return []string{"lxc-console", "-P", path, "-n", name, "-t", "0"}, nil
	}
	return []string{"lxc-attach", "-P", path, "-n", name, "--clear-env",
		"--set-var", "TERM=xterm-256color", "--set-var", "LANG=C.UTF-8",
		"--set-var", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"--", "/bin/sh", "-c", "cd /root 2>/dev/null; if command -v bash >/dev/null; then exec bash -l; fi; exec sh -l"}, nil
}

// GeneratedConfig returns the configuration LXC gets with the next start.
func (m *Manager) GeneratedConfig(name string) (string, error) {
	c, err := m.get(name)
	if err != nil {
		return "", err
	}
	return m.Config(c, false), nil
}
