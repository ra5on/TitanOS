// Package vm runs virtual machines with QEMU/KVM behind libvirt.
//
// What a VM is made of is kept in /config/vms/<name>.json. libvirt only
// gets to know a VM while it runs: its own configuration lives in RAM and
// would be gone after a restart of the NAS. Disks and the firmware's
// settings lie on a volume, in /mnt/<volume>/vms/<name>.
package vm

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

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
	// runGid owns runDir: the group QEMU runs in, not the one of all accounts.
	// runUid is the user QEMU runs as.
	runGid int
	runUid int

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
		kvm: "/dev/kvm", gid: gid, runGid: qemuGroup(gid), runUid: qemuUser(), vms: map[string]VM{}, samples: map[string]*sample{}, autostarted: map[string]bool{}, jobs: map[string]*Job{}, borrowed: map[string][]string{}, starting: map[string]bool{},
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

// Run starts the VMs marked for autostart once their volumes are there and
// samples the running ones, until ctx is done.
func (m *Manager) Run(ctx context.Context, mounted func() []string) {
	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()
	// After a restart of titand (an app update) exclusive VMs may still
	// run: what is reserved for them is taken up again, so that it is
	// given back when they stop.
	if !m.Disabled {
		m.reserve(ctx)
	}
	for {
		m.mu.Lock()
		loaded := m.loaded
		m.mu.Unlock()
		if !loaded {
			m.Load()
		} else if !m.Disabled && !m.off.Load() {
			m.runSnapshotSchedules(ctx, mounted(), time.Now())
			m.runPowerSchedules(ctx, mounted(), time.Now())
			m.refreshVolumeCopies(mounted())
			m.autostart(ctx, mounted())
			m.sample()
			m.checkIOErrors(ctx)
			if m.AfterPoll != nil {
				if err := m.AfterPoll(ctx, m.RunningNames()); err != nil {
					slog.Warn("return VM hardware", "err", err)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) autostart(ctx context.Context, mounted []string) {
	if m.AutoReady != nil && !m.AutoReady() {
		return
	}
	m.mu.Lock()
	var due []string
	for name, vm := range m.vms {
		if vm.Autostart && !m.autostarted[name] && m.available(vm, mounted) {
			// One attempt per boot: a VM someone shut down stays down.
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
			slog.Error("autostart of a VM failed", "vm", name, "err", err)
		}
	}
}

// sample reads CPU time and memory of the running VMs from /proc.
func (m *Manager) sample() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	defer m.release()
	for name, vm := range m.vms {
		pid := m.pid(name)
		if pid == 0 {
			delete(m.samples, name)
			continue
		}
		ticks, started := readStat(filepath.Join(m.procDir, strconv.Itoa(pid), "stat"))
		s := m.samples[name]
		if s == nil {
			s = &sample{since: bootTime(m.procDir).Add(time.Duration(started) * time.Second / clockTicks)}
			m.samples[name] = s
		} else if elapsed := now.Sub(s.at).Seconds(); elapsed > 0 && ticks >= s.ticks {
			// Seconds of CPU per second, as a share of what the vCPUs can do.
			s.percent = min(100, 100*float64(ticks-s.ticks)/clockTicks/elapsed/float64(max(vm.VCPUs, 1)))
		}
		s.ticks, s.at = ticks, now
		s.memory = readRSS(filepath.Join(m.procDir, strconv.Itoa(pid), "status"))
	}
}

// clockTicks is the unit of the times in /proc/<pid>/stat on Linux.
const clockTicks = 100

// readStat returns the CPU time a process used and when it started, both
// in clock ticks.
func readStat(path string) (cpu, started uint64) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0
	}
	// The name of the program is in brackets and may hold spaces.
	_, rest, found := strings.Cut(string(data), ") ")
	fields := strings.Fields(rest)
	if !found || len(fields) < 20 {
		return 0, 0
	}
	// After the name: state is field 3 of the line, utime 14, stime 15, starttime 22.
	utime, _ := strconv.ParseUint(fields[11], 10, 64)
	stime, _ := strconv.ParseUint(fields[12], 10, 64)
	started, _ = strconv.ParseUint(fields[19], 10, 64)
	return utime + stime, started
}

func readRSS(path string) uint64 {
	data, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "VmRSS:"); ok {
			kb, _ := strconv.ParseUint(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "kB")), 10, 64)
			return kb * 1024
		}
	}
	return 0
}

func bootTime(procDir string) time.Time {
	data, _ := os.ReadFile(filepath.Join(procDir, "stat"))
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "btime "); ok {
			seconds, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			return time.Unix(seconds, 0)
		}
	}
	return time.Time{}
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

// checkISO makes sure an installation medium is a file on a volume and
// returns its real path.
func (m *Manager) checkISO(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", ErrBadISO
	}
	// Links must not lead out of the volumes.
	real, err := filepath.EvalSymlinks(path)
	if err != nil || !strings.HasPrefix(real, m.mountRoot+"/") {
		return "", ErrBadISO
	}
	if info, err := os.Stat(real); err != nil || !info.Mode().IsRegular() {
		return "", ErrBadISO
	}
	return real, nil
}

// prepare checks the settings of a VM and fills in what was left open.
func (m *Manager) prepare(ctx context.Context, vm *VM, mounted []string) error {
	if !nameRE.MatchString(vm.Name) {
		return ErrInvalidName
	}
	if vm.DisplayName != "" {
		vm.DisplayName = strings.TrimSpace(vm.DisplayName)
		if !validDisplayName(vm.DisplayName) {
			return ErrInvalidDisplayName
		}
	}
	m.mu.Lock()
	duplicateLabel := false
	for _, other := range m.vms {
		if other.Name != vm.Name && strings.EqualFold(other.Label(), vm.Label()) {
			duplicateLabel = true
			break
		}
	}
	m.mu.Unlock()
	if duplicateLabel {
		return ErrExists
	}
	if !validVMDirectory(vm.folder()) {
		return fmt.Errorf("%w: invalid VM folder", ErrBadFolder)
	}
	if !slices.Contains([]string{"linux", "windows", "other"}, vm.Template) {
		return fmt.Errorf("%w: unknown template", ErrInvalid)
	}
	if err := checkPowerSchedule(&vm.PowerSchedule); err != nil {
		return err
	}
	if err := validateFirmware(*vm); err != nil {
		return err
	}
	if len(vm.CPUPin) > 0 {
		// One vCPU per pinned CPU.
		vm.VCPUs = len(vm.CPUPin)
	}
	if vm.MemoryMiB < 256 || vm.MemoryMiB > 4<<20 || vm.VCPUs < 1 || vm.VCPUs > runtime.NumCPU() {
		return fmt.Errorf("%w: memory or number of vCPUs out of range", ErrInvalid)
	}
	if len(vm.Description) > 200 || strings.ContainsAny(vm.Description, "\n\r") {
		return fmt.Errorf("%w: description too long", ErrInvalid)
	}
	if len(vm.Disks) > maxDisks || (len(vm.Disks) == 0 && vm.ISO == "") {
		return fmt.Errorf("%w: a VM needs a disk or an installation medium", ErrInvalid)
	}
	iso, err := m.checkISO(vm.ISO)
	if err != nil {
		return err
	}
	vm.ISO = iso
	drivers, err := m.checkISO(vm.VirtIOISO)
	if err != nil {
		return err
	}
	vm.VirtIOISO = drivers
	if len(vm.CPUPin) > 0 {
		seen := map[int]bool{}
		for _, cpu := range vm.CPUPin {
			if cpu < 0 || cpu >= runtime.NumCPU() || seen[cpu] {
				return fmt.Errorf("%w: unknown or repeated CPU", ErrInvalid)
			}
			seen[cpu] = true
		}
	} else {
		vm.Exclusive = false
	}
	vm.TopologyAutostart = nil
	if strings.HasPrefix(vm.Network, "network:") {
		if _, ok := m.namedBridge(vm.Network); !ok {
			return ErrNoBridge
		}
	} else if vm.Network != "bridge" {
		vm.Network = "user"
	}
	if err := m.checkFolders(vm.Folders); err != nil {
		return err
	}
	for _, address := range vm.PCI {
		if !pciRE.MatchString(address) {
			return fmt.Errorf("%w: not a PCI address", ErrInvalid)
		}
	}
	for _, id := range vm.USB {
		if !usbRE.MatchString(id) {
			return fmt.Errorf("%w: not a USB device", ErrInvalid)
		}
	}
	file := 0
	for i := range vm.Disks {
		disk := &vm.Disks[i]
		if disk.Path != "" {
			if err := m.checkExisting(ctx, disk, mounted); err != nil {
				return err
			}
			if other := m.ownerOf(*disk, vm.Name); other != "" {
				return fmt.Errorf("%w: %s is used by the VM %s", ErrBadImage, disk.Path, other)
			}
			continue
		}
		if disk.Format != "qcow2" && disk.Format != "raw" {
			return fmt.Errorf("%w: unknown disk format", ErrInvalid)
		}
		if disk.Source != "" {
			if err := m.checkSource(ctx, disk); err != nil {
				return err
			}
		}
		// The web UI takes sizes in decimal GB (decision of 31 Oct 2026),
		// so the smallest disk is 1 GB, not 1 GiB.
		if disk.SizeBytes < 1_000_000_000 || disk.SizeBytes > 16<<40 {
			return fmt.Errorf("%w: disk size out of range", ErrInvalid)
		}
		if !slices.Contains(mounted, disk.Volume) {
			return ErrUnknownVolume
		}
		file++
		if disk.File == "" {
			extension := map[string]string{"qcow2": "qcow2", "raw": "img"}[disk.Format]
			disk.File = fmt.Sprintf("vdisk%d.%s", file, extension)
		}
	}
	for _, disk := range vm.Disks {
		if disk.Path == "" {
			vm.Volume = disk.Volume
			break
		}
	}
	if vm.Volume == "" && len(mounted) > 0 {
		// A VM without a disk of its own still needs a place for its firmware's settings.
		vm.Volume = mounted[0]
	}
	if !slices.Contains(mounted, vm.Volume) {
		return ErrUnknownVolume
	}
	return nil
}

// freeBytes is a variable so that tests do not depend on the free space of
// the machine they run on.
var freeBytes = statfsFree

func statfsFree(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * stat.Bsize, nil
}

// share makes a file or folder of a VM reachable for QEMU through the group.
func (m *Manager) share(path string, mode os.FileMode) error {
	if err := os.Chown(path, -1, m.gid); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func (m *Manager) makeDir(volume, name string) (string, error) {
	// The folder of all VMs is closed to everyone but root and QEMU.
	parent := filepath.Join(m.mountRoot, volume, "vms")
	dir := filepath.Join(parent, name)
	for _, path := range []string{parent, dir} {
		if err := os.Mkdir(path, 0o770); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		if info, err := os.Lstat(path); err != nil || !info.IsDir() {
			return "", ErrBadFolder
		}
		var fsInfo unix.Statfs_t
		if err := unix.Statfs(path, &fsInfo); err != nil {
			return "", err
		}
		if fsInfo.Type == unix.BTRFS_SUPER_MAGIC {
			// New disk files inherit NOCOW; existing files are not rewritten.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := m.runner.Run(ctx, "", "chattr", "+C", path)
			cancel()
			if err != nil {
				return "", err
			}
		}
		if err := m.share(path, 0o770); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// newMAC returns a network address from the range of QEMU/KVM.
func newMAC() string {
	b := make([]byte, 3)
	rand.Read(b)
	return fmt.Sprintf("52:54:00:%02x:%02x:%02x", b[0], b[1], b[2])
}

// Create makes a new VM with its disks. It does not start it.
func (m *Manager) Create(ctx context.Context, vm VM, mounted []string) error {
	if m.NetworkLease != nil {
		release, err := m.NetworkLease()
		if err != nil {
			return err
		}
		defer release()
	}
	if !m.createMu.TryLock() {
		return ErrSnapshotBusy
	}
	defer m.createMu.Unlock()
	if m.Disabled {
		return ErrDisabled
	}
	if vm.Network == "bridge" && m.BridgeReady != nil && !m.BridgeReady() {
		return ErrNoBridge
	}
	if err := m.prepare(ctx, &vm, mounted); err != nil {
		return err
	}
	m.mu.Lock()
	_, exists := m.vms[vm.Name]
	loaded := m.loaded
	m.mu.Unlock()
	if !loaded {
		return fmt.Errorf("%w: /config is not readable", ErrInvalid)
	}
	if exists {
		return ErrExists
	}
	if err := m.checkDirectory(vm); err != nil {
		return err
	}
	vm.UUID, vm.MAC, vm.Created = newUUID(), newMAC(), time.Now().UTC().Truncate(time.Second)

	// Whatever is made here is taken back if a later step fails.
	var created []string
	fail := func(err error) error {
		for _, path := range created {
			os.Remove(path)
		}
		for _, volume := range vm.volumes() {
			os.Remove(m.dir(volume, vm.folder())) // only if empty
		}
		return err
	}
	for _, volume := range vm.volumes() {
		if _, err := m.makeDir(volume, vm.folder()); err != nil {
			return fail(err)
		}
	}
	copies := false
	for _, disk := range vm.Disks {
		if disk.Path != "" {
			continue
		}
		path := m.diskPath(vm, disk)
		if _, err := os.Lstat(path); err == nil {
			// Never write over a disk that is there, e.g. of a VM removed earlier.
			return fail(fmt.Errorf("%w: %s exists already", ErrExists, path))
		}
		free, err := freeBytes(filepath.Dir(path))
		if err != nil {
			return fail(err)
		}
		// A raw disk takes its full size at once; a qcow2 one starts small.
		if needed := map[bool]int64{true: disk.SizeBytes, false: gib}[disk.Format == "raw"]; free < needed {
			return fail(ErrNoSpace)
		}
		if disk.Source != "" {
			// Copied in the background once the VM is stored.
			copies = true
			continue
		}
		args := []string{"create", "-q", "-f", disk.Format}
		if disk.Format == "raw" {
			args = append(args, "-o", "preallocation=falloc")
		}
		created = append(created, path)
		if _, err := m.runner.Run(ctx, "", "qemu-img", append(args, path, strconv.FormatInt(disk.SizeBytes, 10))...); err != nil {
			return fail(err)
		}
		if err := m.share(path, 0o660); err != nil {
			return fail(err)
		}
	}
	if err := m.persistVM(vm); err != nil {
		return fail(err)
	}
	m.mu.Lock()
	m.vms[vm.Name] = vm
	if copies {
		m.jobs[vm.Name] = &Job{State: "running"}
	}
	m.mu.Unlock()
	if vm.PowerSchedule.Enabled {
		m.resetPower(vm.Name, time.Now())
	}
	if copies {
		go m.copyDisks(context.WithoutCancel(ctx), vm)
	}
	return nil
}

// Change is what can be changed on a VM that exists.
type Change struct {
	DisplayName *string  `json:"displayName"`
	Description string   `json:"description"`
	MemoryMiB   int      `json:"memoryMiB"`
	VCPUs       int      `json:"vcpus"`
	ISO         string   `json:"iso"`
	VirtIOISO   string   `json:"virtioISO"`
	Autostart   bool     `json:"autostart"`
	CPUPin      []int    `json:"cpuPin"`
	Exclusive   bool     `json:"exclusive"`
	Folders     []Folder `json:"folders"`
	PCI         []string `json:"pci"`
	AutoGPU     bool     `json:"autoGPU"`
	USB         []string `json:"usb"`
	Network     string   `json:"network"`
	// PowerSchedule replaces the schedule; without it the schedule stays.
	PowerSchedule *PowerSchedule `json:"powerSchedule"`
}

// Update changes a VM. Everything but the display name, description and autostart
// belongs to the virtual hardware and only changes while the VM is off.
func (m *Manager) Update(ctx context.Context, name string, change Change, mounted []string) error {
	if m.NetworkLease != nil {
		release, err := m.NetworkLease()
		if err != nil {
			return err
		}
		defer release()
	}
	lock, lockErr := m.operation(name)
	if lockErr != nil {
		return lockErr
	}
	defer lock.Unlock()
	if v, e := m.get(name); e == nil && m.pendingSnapshot(v) {
		return ErrSnapshotRecovery
	}
	if m.Disabled {
		return ErrDisabled
	}
	vm, err := m.get(name)
	if err != nil {
		return err
	}
	changed := vm
	if change.DisplayName != nil {
		changed.DisplayName = strings.TrimSpace(*change.DisplayName)
		if !validDisplayName(changed.DisplayName) {
			return ErrInvalidDisplayName
		}
	}
	changed.Description, changed.Autostart = change.Description, change.Autostart
	changed.MemoryMiB, changed.VCPUs, changed.ISO = change.MemoryMiB, change.VCPUs, change.ISO
	changed.VirtIOISO = change.VirtIOISO
	changed.CPUPin, changed.Exclusive, changed.Folders, changed.PCI, changed.USB = change.CPUPin, change.Exclusive, change.Folders, change.PCI, change.USB
	changed.Network = change.Network
	changed.AutoGPU = change.AutoGPU
	if change.PowerSchedule != nil {
		changed.PowerSchedule = *change.PowerSchedule
	}
	if err := m.prepare(ctx, &changed, mounted); err != nil {
		return err
	}
	before, after := vm, changed
	before.DisplayName, after.DisplayName = "", ""
	before.Description, before.Autostart, after.Description, after.Autostart = "", false, "", false
	before.PowerSchedule, after.PowerSchedule = PowerSchedule{}, PowerSchedule{}
	if !reflect.DeepEqual(normal(before), normal(after)) && m.pid(name) != 0 {
		return ErrRunning
	}
	if err := m.persistVM(changed); err != nil {
		return err
	}
	m.mu.Lock()
	m.vms[name] = changed
	m.mu.Unlock()
	if !reflect.DeepEqual(vm.PowerSchedule, changed.PowerSchedule) {
		m.resetPower(name, time.Now())
	}
	return nil
}

// normal makes VMs comparable: no entries and an empty list are the same.
func normal(vm VM) VM {
	if len(vm.CPUPin) == 0 {
		vm.CPUPin = nil
	}
	if len(vm.Folders) == 0 {
		vm.Folders = nil
	}
	if len(vm.PCI) == 0 {
		vm.PCI = nil
	}
	if len(vm.USB) == 0 {
		vm.USB = nil
	}
	return vm
}

// XML returns what libvirt is told about a VM.
func (m *Manager) XML(name string) (string, error) {
	vm, err := m.get(name)
	if err != nil {
		return "", err
	}
	return m.domainXML(vm)
}

// Start boots a VM.
func (m *Manager) Start(ctx context.Context, name string, mounted []string) error {
	if m.off.Load() {
		return ErrServiceOff
	}
	if m.NetworkLease != nil {
		release, err := m.NetworkLease()
		if err != nil {
			return err
		}
		defer release()
	}
	lock, lockErr := m.operation(name)
	if lockErr != nil {
		return lockErr
	}
	defer lock.Unlock()
	if v, e := m.get(name); e == nil && m.pendingSnapshot(v) {
		return ErrSnapshotRecovery
	}
	if m.Disabled {
		return ErrDisabled
	}
	vm, err := m.get(name)
	if err != nil {
		return err
	}
	if !m.HasKVM() {
		return ErrNoKVM
	}
	if m.pid(name) != 0 {
		return ErrRunning
	}
	if !m.available(vm, mounted) {
		return ErrUnknownVolume
	}
	m.mu.Lock()
	job := m.jobs[name]
	m.mu.Unlock()
	if job != nil {
		return ErrPreparing
	}
	if vm.Network == "bridge" {
		if m.BridgeReady != nil && !m.BridgeReady() {
			return ErrNoBridge
		}
		if _, err := os.Stat(filepath.Join(m.sysDir, "class/net", bridge)); err != nil {
			return ErrNoBridge
		}
	}
	if strings.HasPrefix(vm.Network, "network:") {
		dev, ok := m.namedBridge(vm.Network)
		if !ok {
			return ErrNoBridge
		}
		if _, err := os.Stat(filepath.Join(m.sysDir, "class/net", dev)); err != nil {
			return ErrNoBridge
		}
	}
	if vm.ISO != "" {
		if _, err := m.checkISO(vm.ISO); err != nil {
			return err
		}
	}
	if vm.VirtIOISO != "" {
		if _, err := m.checkISO(vm.VirtIOISO); err != nil {
			return err
		}
	}
	// Everything the VM was given may have changed since: a folder
	// removed, a disk put into a volume.
	if err := m.checkFolders(vm.Folders); err != nil {
		return err
	}
	for i := range vm.Disks {
		if vm.Disks[i].Path != "" {
			disk := vm.Disks[i]
			if err := m.checkExisting(ctx, &disk, mounted); err != nil {
				return err
			}
		}
	}

	// The firmware keeps its settings (boot order, …) in a file of its
	// own. QEMU must be able to write it, so it is made here, not by libvirt.
	dir, err := m.makeDir(vm.Volume, vm.folder())
	if err != nil {
		return err
	}
	if err := m.prepareNVRAM(vm); err != nil {
		return err
	}
	if err := os.MkdirAll(m.runDir, 0o770); err != nil {
		return err
	}
	if err := os.Chown(m.runDir, -1, m.runGid); err != nil {
		return err
	}
	if err := os.Chmod(m.runDir, 0o770); err != nil {
		return err
	}
	os.Remove(m.VNCSocket(name))
	os.Remove(m.SerialSocket(name))
	m.mu.Lock()
	m.starting[name] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.starting, name)
		m.mu.Unlock()
	}()
	started := false
	if m.BeforeStart != nil {
		finish, err := m.BeforeStart(ctx, vm.Name, vm.PCI, vm.USB, vm.AutoGPU)
		if err != nil {
			return err
		}
		defer func() { finish(started) }()
	}
	if err := m.lend(vm); err != nil {
		return err
	}
	if vm.TPM {
		if err := m.prepareTPM(dir); err != nil {
			return err
		}
	}

	description, err := m.domainXML(vm)
	if err != nil {
		return err
	}
	file := filepath.Join(m.runDir, name+".xml")
	if err := os.WriteFile(file, []byte(description), 0o600); err != nil {
		return err
	}
	if _, err := m.runner.Run(ctx, "", "virsh", "--quiet", "create", "--file", file); err != nil {
		m.mu.Lock()
		delete(m.starting, name)
		m.release()
		m.mu.Unlock()
		return err
	}
	started = true
	slog.Info("VM started", "vm", name)
	m.reserve(ctx)
	return nil
}

// lookupGroup returns the number of a group, or fallback if the system has none of that name.
func lookupGroup(name string, fallback int) int {
	if group, err := user.LookupGroup(name); err == nil {
		if gid, err := strconv.Atoi(group.Gid); err == nil {
			return gid
		}
	}
	return fallback
}

// qemuUser returns the user QEMU runs as, or root where there is none.
func qemuUser() int {
	if account, err := user.Lookup("libvirt-qemu"); err == nil {
		if uid, err := strconv.Atoi(account.Uid); err == nil {
			return uid
		}
	}
	return 0
}

// qemuGroup returns the group QEMU runs in.
func qemuGroup(fallback int) int { return lookupGroup("kvm", fallback) }

func copyFile(from, to string) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o660)
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		target.Close()
		os.Remove(to)
		return err
	}
	return target.Close()
}

// Action asks a running VM to shut down or restart, or switches it off.
func (m *Manager) Action(ctx context.Context, name, action string) error {
	lock, lockErr := m.operation(name)
	if lockErr != nil {
		return lockErr
	}
	defer lock.Unlock()
	if v, e := m.get(name); e == nil && m.pendingSnapshot(v) {
		return ErrSnapshotRecovery
	}
	if m.Disabled {
		return ErrDisabled
	}
	if _, err := m.get(name); err != nil {
		return err
	}
	// shutdown and reboot ask the guest; destroy is pulling the plug.
	command, known := map[string]string{"shutdown": "shutdown", "reboot": "reboot", "poweroff": "destroy", "resume": "resume"}[action]
	if !known {
		return ErrNotFound
	}
	if m.pid(name) == 0 {
		return ErrNotRunning
	}
	_, err := m.runner.Run(ctx, "", "virsh", "--quiet", command, name)
	return err
}

// Delete forgets a VM. Its disks stay unless deleteDisks is set, which
// needs the name of the VM once more as confirmation.
func (m *Manager) Delete(name string, deleteDisks bool, confirm string) error {
	lock, lockErr := m.operation(name)
	if lockErr != nil {
		return lockErr
	}
	defer lock.Unlock()
	if v, e := m.get(name); e == nil && m.pendingSnapshot(v) {
		return ErrSnapshotRecovery
	}
	if m.Disabled {
		return ErrDisabled
	}
	vm, err := m.get(name)
	if err != nil {
		return err
	}
	entries, readErr := os.ReadDir(m.snapshotRoot(vm))
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		return readErr
	}
	for _, entry := range entries {
		if entry.IsDir() && snapshotID.MatchString(entry.Name()) {
			return ErrSnapshotExists
		}
	}
	if m.pid(name) != 0 {
		return ErrRunning
	}
	m.mu.Lock()
	job := m.jobs[name]
	m.mu.Unlock()
	if job != nil && job.State == "running" {
		return ErrPreparing
	}
	if deleteDisks && confirm != name && confirm != vm.Label() {
		return ErrNotConfirmed
	}
	if err := m.store.Remove(configDir + "/" + name + ".json"); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.vms, name)
	delete(m.autostarted, name)
	delete(m.jobs, name)
	m.mu.Unlock()
	m.resetPower(name, time.Now())
	os.Remove(filepath.Join(m.runDir, name+".xml"))

	if deleteDisks {
		// Only what TitanOS made itself; anything else in the folder stays,
		// and with it the folder.
		for _, disk := range vm.Disks {
			if disk.Path != "" {
				continue // was there before the VM and stays
			}
			if err := os.Remove(m.diskPath(vm, disk)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				slog.Warn("disk of a removed VM not deleted", "vm", name, "err", err)
			}
		}
		os.Remove(filepath.Join(m.dir(vm.Volume, vm.folder()), "nvram.fd"))
		os.RemoveAll(filepath.Join(m.dir(vm.Volume, vm.folder()), "tpm"))
		// The copy of the settings goes with the disks it describes.
		m.removeVolumeCopy(vm)
		for _, volume := range vm.volumes() {
			os.Remove(m.dir(volume, vm.folder()))
		}
	} else {
		// The disks stay: the copy says the VM was removed, and it can be
		// taken over again ("VM übernehmen").
		m.markVolumeCopyRemoved(vm, time.Now().UTC().Truncate(time.Second))
	}
	return nil
}

// Medium is an installation medium found on a volume.
type Medium struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
}

// Media looks for ISO files on the mounted volumes, a few folders deep.
func (m *Manager) Media(mounted []string) []Medium {
	media := []Medium{}
	visited := 0
	for _, volume := range mounted {
		root := filepath.Join(m.mountRoot, volume)
		filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			visited++
			if err != nil || visited > 50000 || len(media) >= 200 {
				return fs.SkipDir
			}
			if entry.IsDir() {
				depth := strings.Count(strings.TrimPrefix(path, root), "/")
				// Docker's data, app data and recycle bins hold no media.
				if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "appdata" || depth > 4) {
					return fs.SkipDir
				}
				return nil
			}
			if strings.EqualFold(filepath.Ext(entry.Name()), ".iso") && entry.Type().IsRegular() {
				if info, err := entry.Info(); err == nil {
					media = append(media, Medium{Path: path, SizeBytes: info.Size()})
				}
			}
			return nil
		})
	}
	sort.Slice(media, func(i, j int) bool { return media[i].Path < media[j].Path })
	return media
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
