// Package vm runs virtual machines with QEMU/KVM behind libvirt.
//
// What a VM is made of is kept in /config/vms/<name>.json. libvirt only
// gets to know a VM while it runs: its own configuration lives in RAM and
// would be gone after a restart of the NAS. Disks and the firmware's
// settings lie on a volume, in /mnt/<volume>/vms/<name>.
package vm

import (
	"context"
	"errors"
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
	"sync/atomic"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

const (
	// bridge is the bridge of the NAS that VMs join for an address of their own.
	bridge    = "br0"
	configDir = "vms"
	maxDisks  = 8
	gib       = 1 << 30
	// sampleInterval is how often the running VMs are looked at.
	sampleInterval = 5 * time.Second
)

var (
	ErrDisabled           = errors.New("vm: not available in development mode")
	ErrNoKVM              = errors.New("vm: this machine has no hardware virtualization (KVM)")
	ErrInvalidName        = errors.New("vm: invalid name")
	ErrInvalidDisplayName = errors.New("vm: invalid display name")
	ErrExists             = errors.New("vm: a VM with this name exists")
	ErrNotFound           = errors.New("vm: not found")
	ErrInvalid            = errors.New("vm: invalid settings")
	// ErrUnknownVolume means a volume is not known or not mounted.
	ErrUnknownVolume = errors.New("vm: the volume is not available")
	ErrNoSpace       = errors.New("vm: not enough free space on the volume")
	ErrBadISO        = errors.New("vm: the installation medium is not a file on a volume")
	ErrRunning       = errors.New("vm: the VM is running")
	ErrNotRunning    = errors.New("vm: the VM is not running")
	ErrNotConfirmed  = errors.New("vm: deleting the disks was not confirmed")
	ErrPreparing     = errors.New("vm: the disks of the VM are still being prepared")
	ErrNoBridge      = errors.New("vm: the network bridge is not set up")
	ErrBadFolder     = errors.New("vm: the folder is not a folder on a volume")
	ErrBadImage      = errors.New("vm: not a disk image on a volume or a free disk")
)

var (
	nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)
	tagRE  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,36}$`)
	pciRE  = regexp.MustCompile(`^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`)
	usbRE  = regexp.MustCompile(`^[0-9a-f]{4}:[0-9a-f]{4}$`)
)

// Disk is a disk of a VM. Usually it is a file TitanOS made in the VM's
// folder; it can also be an image that was there before, or a whole disk
// of the NAS.
type Disk struct {
	// File is the name of the file in the VM's folder, e.g. vdisk1.qcow2.
	File string `json:"file"`
	// Format is qcow2 (grows as it fills, default) or raw (full size at once).
	Format    string `json:"format"`
	SizeBytes int64  `json:"sizeBytes"`
	// Volume is where the file lies; disks of one VM may be on different volumes.
	Volume string `json:"volume"`
	// Path is set for a disk that is not a file of the VM's folder: an
	// existing image used as it is, or a whole disk (/dev/disk/by-id/…).
	// Removing the VM never deletes it.
	Path string `json:"path,omitempty"`
	// Source is an existing image this disk is a copy of. It is only set
	// until the copy is done.
	Source string `json:"source,omitempty"`
}

// isBlock reports whether a disk is a whole disk of the NAS.
func (m *Manager) isBlock(d Disk) bool {
	return d.Path != "" && (strings.HasPrefix(d.Path, "/dev/") || filepath.Dir(d.Path) == m.byID)
}

// Folder is a folder of the NAS a VM sees through virtiofs.
type Folder struct {
	Path string `json:"path"`
	// Tag is the name the guest mounts it by.
	Tag      string `json:"tag"`
	ReadOnly bool   `json:"readOnly"`
}

// VM is a virtual machine as stored in /config.
type VM struct {
	SnapshotSchedule SnapshotSchedule `json:"snapshotSchedule"`
	Name             string           `json:"name"`
	DisplayName      string           `json:"displayName,omitempty"`
	Description      string           `json:"description"`
	// Firmware is fixed at creation. Empty preserves UEFI for existing configurations.
	Firmware string `json:"firmware,omitempty"`
	// Template decides which virtual hardware the guest gets: linux, windows or other.
	Template  string `json:"template"`
	UUID      string `json:"uuid"`
	MAC       string `json:"mac"`
	MemoryMiB int    `json:"memoryMiB"`
	VCPUs     int    `json:"vcpus"`
	// Volume holds the VM's folder with the firmware's settings.
	Volume    string `json:"volume"`
	Directory string `json:"directory,omitempty"`
	Disks     []Disk `json:"disks"`
	// ISO is the path of the installation medium, or empty.
	ISO       string `json:"iso"`
	VirtIOISO string `json:"virtioISO,omitempty"`
	// Autostart starts the VM once its volumes are there after a boot.
	Autostart bool      `json:"autostart"`
	Created   time.Time `json:"created"`

	// CPUPin ties each vCPU to a logical CPU of the NAS; empty lets Linux
	// spread them freely. Its length is the number of vCPUs.
	CPUPin []int `json:"cpuPin"`
	// Exclusive keeps everything else on the NAS off the pinned CPUs
	// while the VM runs.
	Exclusive bool `json:"exclusive"`
	// Folders of the NAS the guest can mount.
	Folders []Folder `json:"folders"`
	// TPM gives the guest a TPM 2.0, SecureBoot a firmware that only
	// starts signed systems. Windows 11 asks for both. Fixed at creation:
	// the firmware's settings depend on it.
	TPM        bool `json:"tpm"`
	SecureBoot bool `json:"secureBoot"`
	// Network is "bridge" for a VM with an address of its own in the
	// network of the NAS, which needs the bridge br0. Anything else is
	// "user": outgoing connections through the NAS only.
	Network string `json:"network"`
	// Older applications cannot attach named networks correctly: no automatic
	// guest start on a fallback version. Current applications restore this flag.
	TopologyAutostart *bool `json:"topologyAutostart,omitempty"`
	// PCI devices (0000:01:00.0) and USB devices (046d:c52b) handed to the guest.
	PCI     []string `json:"pci"`
	AutoGPU bool     `json:"autoGPU"`
	USB     []string `json:"usb"`
	// PowerSchedule starts and shuts the VM down at set times; left out of
	// the file while unused. Older versions of titand ignore it (and drop
	// it when they save the VM).
	PowerSchedule PowerSchedule `json:"powerSchedule,omitzero"`
}

// Job is the preparation of a VM's disks, which can take a while.
type Job struct {
	Operation string `json:"operation,omitempty"`
	State     string `json:"state"` // running, failed
	Text      string `json:"text"`
}

// Status is a VM with what it is doing right now.
type Status struct {
	VM
	// State is running or stopped.
	State string `json:"state"`
	// Available is false while a volume of the VM is not mounted.
	Available bool `json:"available"`
	// CPUPercent is the load of the VM in percent of its own vCPUs.
	CPUPercent float64 `json:"cpuPercent"`
	// MemoryBytes is the RAM of the NAS the VM occupies right now.
	MemoryBytes uint64    `json:"memoryBytes"`
	Since       time.Time `json:"since"`
	// Job is set while disks are copied, or if that failed.
	Job *Job `json:"job"`
	// IOError says QEMU paused the VM after a read or write error of one
	// of its disks on the NAS; "resume" lets it go on once that is fixed.
	IOError bool `json:"ioError,omitempty"`
	// Schedule says when the power schedule starts and stops the VM next.
	Schedule *PowerView `json:"schedule,omitempty"`
}

type sample struct {
	ticks   uint64
	at      time.Time
	percent float64
	memory  uint64
	since   time.Time
}

// Manager keeps the VMs and talks to libvirt.
type Manager struct {
	// off is the switch of Settings › Services (service.go).
	off               atomic.Bool
	ScheduleAdmission func() (func(), error)
	AutoReady         func() bool
	// BridgeReady rejects creation/start until the network trial is confirmed.
	BridgeReady   func() bool
	NetworkBridge func(string) (string, bool)
	NetworkLease  func() (func(), error)
	BeforeStart   func(context.Context, string, []string, []string, bool) (func(bool), error)
	AfterPoll     func(context.Context, map[string]bool) error
	Timezone      func() string
	// Notify reports scheduled starts and stops that failed or were left out.
	Notify func(severity, kind, subject string)
	// Activity records a scheduled start or stop in the activity log.
	Activity func(action, target string, ok bool)
	// PowerOffSoon says the NAS shuts down by its own schedule shortly or
	// waits to; a scheduled start is then left out.
	PowerOffSoon func() bool
	// Mounted returns the volumes mounted right now. The copies of the
	// settings in the folders of the VMs are only written to those.
	Mounted func() []string

	store  *config.Store
	runner run.Runner
	// Disabled leaves the system alone. Set in development mode.
	Disabled  bool
	mountRoot string
	// runDir holds what only lives while the NAS is up: the descriptions
	// handed to libvirt and the sockets of the screens. QEMU creates the
	// sockets, so it must get in there; nobody else may.
	runDir string
	// pidDir is where libvirt notes the process of every running VM.
	// Looking there says which VMs run without waking libvirt up.
	pidDir  string
	procDir string
	kvm     string
	// gid owns the files of the VMs. QEMU does not run as root; it gets at
	// disks and installation media through this group.
	gid int
	// runGID owns runDir: the group QEMU runs in, not the one of all accounts.
	// runUID is the user QEMU runs as.
	runGID int
	runUID int

	// FreeDisks returns the names (sda, nvme0n1) of the disks that are in
	// no volume. Only those may be handed to a VM as a whole.
	FreeDisks func() []string
	// byID is where the stable names of the disks are.
	byID string
	// sysDir and devDir are /sys and /dev.
	sysDir, devDir string

	operations      sync.Map
	snapshotJobs    map[string]*Job
	snapshotSpaceMu sync.Mutex
	snapshotSpace   map[uint64]uint64
	createMu        sync.Mutex
	mu              sync.Mutex
	loaded          bool
	vms             map[string]VM
	samples         map[string]*sample
	autostarted     map[string]bool
	// ioPaused are the running VMs paused after a disk error; ioChecked
	// is when that was last looked at.
	ioPaused  map[string]bool
	ioChecked time.Time
	jobs      map[string]*Job
	// reserved is the set of CPUs kept free for exclusive VMs right now.
	reserved string
	// borrowed are whole disks whose device node was opened to QEMU.
	borrowed map[string][]string
	// starting are the VMs libvirt is bringing up right now. They do not
	// count as stopped although their process is not there yet.
	starting map[string]bool
	// addresses are the last answers about the addresses of running VMs.
	addresses map[string]cachedAddresses
	// autostartFile keeps autostarted in RAM across restarts of titand
	// (an app update): a VM someone shut down stays down until the NAS
	// itself starts again.
	autostartFile string

	// The power schedules: what was carried out (also in powerFile, so that
	// a restart of titand does nothing twice) and what is due.
	powerMu      sync.Mutex
	powerZone    *time.Location
	powerHandled map[string]time.Time
	powerPending map[string]*powerPending
	powerBoot    bool
	powerFile    string
	powerWork    sync.WaitGroup
	// powerUnit is a second of the stop timeout, powerPoll how often a
	// shutting down VM is looked at. Tests make both short.
	powerUnit, powerPoll time.Duration
	// now is the clock the next scheduled times are shown from; tests set it.
	now func() time.Time

	// copyMu keeps writes of the copies on the volumes apart; copied are the
	// VMs whose copy was written or looked at since titand started.
	copyMu sync.Mutex
	copied map[string]bool
}

// NewManager returns a manager working on the real system. gid is the
// group all accounts and QEMU are in.
func NewManager(store *config.Store, gid int) *Manager {
	return &Manager{
		store: store, runner: run.Exec{}, mountRoot: "/mnt", runDir: "/run/titanos-vms", pidDir: "/run/libvirt/qemu", procDir: "/proc",
		kvm: "/dev/kvm", gid: gid, runGID: qemuGroup(gid), runUID: qemuUser(), vms: map[string]VM{}, samples: map[string]*sample{}, autostarted: map[string]bool{}, jobs: map[string]*Job{}, borrowed: map[string][]string{}, starting: map[string]bool{},
		snapshotJobs: map[string]*Job{}, copied: map[string]bool{}, autostartFile: "/run/titanos/vms-autostarted",
		powerFile: "/run/titanos/vm-schedule.json", powerUnit: time.Second, powerPoll: 2 * time.Second,
		FreeDisks: func() []string { return nil }, byID: "/dev/disk/by-id", sysDir: "/sys", devDir: "/dev",
	}
}

// Load reads the VMs from /config.
func (m *Manager) Load() error {
	names, err := m.store.List(configDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	vms := map[string]VM{}
	for _, file := range names {
		name, isJSON := strings.CutSuffix(file, ".json")
		if !isJSON || !nameRE.MatchString(name) {
			continue
		}
		var vm VM
		if err := m.store.ReadJSON(configDir+"/"+file, &vm); err != nil || vm.Name != name {
			slog.Warn("VM not readable from /config", "file", file, "err", err)
			continue
		}
		vms[name] = vm
		if strings.HasPrefix(vm.Network, "network:") && vm.TopologyAutostart != nil {
			vm.Autostart = *vm.TopologyAutostart
			vm.TopologyAutostart = nil
			vms[name] = vm
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vms, m.loaded = vms, true
	if data, err := os.ReadFile(m.autostartFile); err == nil {
		for _, name := range strings.Fields(string(data)) {
			m.autostarted[name] = true
		}
	}
	return nil
}

// noteAutostarted writes down which VMs were autostarted. The lock is held.
func (m *Manager) noteAutostarted() {
	names := make([]string, 0, len(m.autostarted))
	for name := range m.autostarted {
		names = append(names, name)
	}
	sort.Strings(names)
	if err := os.MkdirAll(filepath.Dir(m.autostartFile), 0o700); err == nil {
		err = os.WriteFile(m.autostartFile, []byte(strings.Join(names, "\n")+"\n"), 0o600)
		if err != nil {
			slog.Warn("autostarted VMs not noted", "err", err)
		}
	}
}

// HasKVM reports whether the machine can run VMs at a useful speed.
func (m *Manager) HasKVM() bool {
	_, err := os.Stat(m.kvm)
	return err == nil
}

func (m *Manager) dir(volume, name string) string {
	return filepath.Join(m.mountRoot, volume, "vms", name)
}

func (m *Manager) diskPath(vm VM, disk Disk) string {
	if disk.Path != "" {
		return disk.Path
	}
	return filepath.Join(m.dir(disk.Volume, vm.folder()), disk.File)
}

// SerialSocket is where the serial port of a running VM is reachable.
func (m *Manager) SerialSocket(name string) string {
	return filepath.Join(m.runDir, name+".serial")
}

// VNCSocket is where the screen of a running VM is reachable.
func (m *Manager) VNCSocket(name string) string {
	return filepath.Join(m.runDir, name+".sock")
}

// volumes returns the volumes a VM needs.
func (vm VM) volumes() []string {
	needed := []string{vm.Volume}
	for _, disk := range vm.Disks {
		if disk.Volume != "" && !slices.Contains(needed, disk.Volume) {
			needed = append(needed, disk.Volume)
		}
	}
	return needed
}

func (m *Manager) available(vm VM, mounted []string) bool {
	for _, volume := range vm.volumes() {
		if !slices.Contains(mounted, volume) {
			return false
		}
	}
	for _, path := range []string{vm.ISO, vm.VirtIOISO} {
		if path == "" {
			continue
		}
		relative, err := filepath.Rel(m.mountRoot, path)
		if err != nil || strings.HasPrefix(relative, "..") {
			return false
		}
		volume := strings.Split(filepath.ToSlash(relative), "/")[0]
		if !slices.Contains(mounted, volume) {
			return false
		}
	}
	return true
}

// pid returns the process of a running VM, or 0.
func (m *Manager) pid(name string) int {
	data, err := os.ReadFile(filepath.Join(m.pidDir, name+".pid"))
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	if pid <= 0 {
		return 0
	}
	// A file left behind by a process that is gone does not count.
	if _, err := os.Stat(filepath.Join(m.procDir, strconv.Itoa(pid))); err != nil {
		return 0
	}
	return pid
}

// List returns all VMs with their state.
func (m *Manager) List(mounted []string) []Status {
	loc, now := m.zone(), m.clock()
	m.mu.Lock()
	defer m.mu.Unlock()
	list := make([]Status, 0, len(m.vms))
	for name, vm := range m.vms {
		status := Status{VM: vm, State: "stopped", Available: m.available(vm, mounted), Schedule: m.powerView(vm, loc, now)}
		if job := m.jobs[name]; job != nil {
			copied := *job
			status.Job = &copied
		}
		if job := m.snapshotJobs[name]; job != nil {
			copied := *job
			status.Job = &copied
		}
		if !m.Disabled && m.pid(name) != 0 {
			status.State = "running"
			status.IOError = m.ioPaused[name]
			if s := m.samples[name]; s != nil {
				status.CPUPercent, status.MemoryBytes, status.Since = s.percent, s.memory, s.since
			}
		}
		list = append(list, status)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

func (m *Manager) get(name string) (VM, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	vm, ok := m.vms[name]
	if !ok {
		return VM{}, ErrNotFound
	}
	return vm, nil
}

func (m *Manager) RunningNames() map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := map[string]bool{}
	for name := range m.vms {
		if m.starting[name] || m.pid(name) != 0 {
			result[name] = true
		}
	}
	return result
}

// Busy reports whether disks of a VM are being copied or a snapshot is
// being taken or restored. titand does that work itself.
func (m *Manager) Busy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, job := range m.jobs {
		if job.State == "running" {
			return true
		}
	}
	for _, job := range m.snapshotJobs {
		if job.State == "running" {
			return true
		}
	}
	return false
}

// checkIOErrors asks libvirt every 30 seconds whether a running VM was
// paused after a disk error (error_policy "stop") and reports each such
// pause once. libvirt runs anyway while a VM does.
func (m *Manager) checkIOErrors(ctx context.Context) {
	now := m.clock()
	if now.Sub(m.ioChecked) < 30*time.Second {
		return
	}
	m.ioChecked = now
	running := m.RunningNames()
	paused := map[string]bool{}
	for name := range running {
		out, err := m.runner.Run(ctx, "", "virsh", "--quiet", "domstate", name, "--reason")
		if err == nil && strings.Contains(out, "paused") && strings.Contains(strings.ToLower(out), "i/o error") {
			paused[name] = true
		}
	}
	m.mu.Lock()
	var fresh []string
	for name := range paused {
		if !m.ioPaused[name] {
			fresh = append(fresh, name)
		}
	}
	m.ioPaused = paused
	m.mu.Unlock()
	for _, name := range fresh {
		slog.Error("VM paused after a disk error", "vm", name)
		if m.Notify != nil {
			m.Notify("error", "vm_io_error", name)
		}
	}
}
