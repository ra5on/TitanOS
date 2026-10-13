// Package storage manages disks and volumes: classic software RAID (mdadm)
// with a filesystem on top, mounted under /mnt/<name>.
//
// Data safety comes first. Every command that writes to a disk goes through
// the checks in actions.go, and nothing here touches a disk that is part of
// a volume, mounted, or the boot stick.
package storage

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

const (
	stickLabel  = "TITANOS"
	configFile  = "storage/volumes.json"
	smartMaxAge = 10 * time.Minute
)

// Roles of a disk.
const (
	RoleStick  = "stick"  // the boot stick
	RoleMember = "member" // part of a TitanOS volume
	RoleFree   = "free"   // empty: no partitions, no filesystem
	RoleUsed   = "used"   // holds something TitanOS did not put there
)

// States of a disk inside a volume.
const (
	MemberActive     = "active"
	MemberRebuilding = "rebuilding"
	MemberFaulty     = "faulty"
	MemberSpare      = "spare"
)

// States of a volume.
const (
	VolumeOK         = "ok"
	VolumeDegraded   = "degraded"   // a disk is missing, data still there
	VolumeRebuilding = "rebuilding" // a new disk is being filled
	VolumeSyncing    = "syncing"    // first sync after creation
	VolumeChecking   = "checking"
	VolumeFailed     = "failed"  // too many disks missing
	VolumeMissing    = "missing" // none of its disks found
)

// Disk is a physical disk as shown in the UI.
type Disk struct {
	Name      string `json:"name"` // sda
	Path      string `json:"path"` // /dev/sda
	Model     string `json:"model"`
	Serial    string `json:"serial"`
	SizeBytes uint64 `json:"sizeBytes"`
	Kind      string `json:"kind"` // hdd, ssd, nvme, usb
	Role      string `json:"role"`
	// Volume and MemberState are set for members.
	Volume      string `json:"volume,omitempty"`
	MemberState string `json:"memberState,omitempty"`
	// Contents names what was found on a used disk (ext4, ntfs, RAID …).
	Contents []string `json:"contents,omitempty"`
	Mounted  bool     `json:"mounted,omitempty"`
	// Testing is set while a test of the disk runs; nothing else may use it.
	Testing bool   `json:"testing,omitempty"`
	Smart   *Smart `json:"smart"`
}

// Smart is the health a disk reports about itself. Nil if it reports none.
type Smart struct {
	Healthy      bool   `json:"healthy"`
	TemperatureC int    `json:"temperatureC"`
	PowerOnHours int    `json:"powerOnHours"`
	Errors       uint64 `json:"errors"`
	RotationRate int    `json:"rotationRate"`
	// WearPercent is how much of an SSD's life is used; -1 if unknown.
	WearPercent int `json:"wearPercent"`
}

// VolumeConfig is a volume as stored in /config/storage/volumes.json.
type VolumeConfig struct {
	Name           string    `json:"name"`
	Level          string    `json:"level"`      // raid0, raid1, raid5, raid6, raid10
	Filesystem     string    `json:"filesystem"` // ext4, xfs
	ArrayUUID      string    `json:"arrayUUID"`
	FilesystemUUID string    `json:"filesystemUUID"`
	Created        time.Time `json:"created"`
}

// Volume is a configured volume with its live state.
type Volume struct {
	Name       string   `json:"name"`
	Level      string   `json:"level"`
	Filesystem string   `json:"filesystem"`
	MountPoint string   `json:"mountPoint"`
	Mounted    bool     `json:"mounted"`
	State      string   `json:"state"`
	SizeBytes  uint64   `json:"sizeBytes"`
	UsedBytes  uint64   `json:"usedBytes"`
	RaidDisks  int      `json:"raidDisks"`
	Members    []Member `json:"members"`
	Sync       *Sync    `json:"sync"`
	// Mismatches is what the last check found: blocks whose copies differ.
	Mismatches uint64 `json:"mismatches"`
}

// Member is a disk of a volume.
type Member struct {
	Disk  string `json:"disk"` // sda
	State string `json:"state"`
}

// Sync is a running rebuild, first sync or check.
type Sync struct {
	Percent             float64 `json:"percent"`
	SpeedBytesPerSecond uint64  `json:"speedBytesPerSecond"`
	RemainingSeconds    int64   `json:"remainingSeconds"`
}

// Status is everything the storage page shows.
type Status struct {
	Disks   []Disk   `json:"disks"`
	Volumes []Volume `json:"volumes"`
}

// Runner runs a system command. Tests replace it.
type Runner = run.Runner

// CommandError is a failed system command, with what it printed.
type CommandError = run.CommandError

// Manager knows the configured volumes and carries out all storage actions.
type Manager struct {
	store  *config.Store
	runner Runner
	// Where volumes are mounted and where the kernel shows block devices.
	mountRoot string
	sysBlock  string

	// ReadOnly refuses everything that would change a disk. It is set in
	// development mode, where titand runs on the developer's own machine.
	ReadOnly bool

	// actions serializes everything that changes disks or the configuration.
	actions sync.Mutex

	// Claimed returns the disks that something outside this package
	// uses as a whole (a VM), with who it is. Such a disk is never tested.
	Claimed func() map[string]string
	// OnTestDone is told about every disk test that ends.
	OnTestDone func(DiskTest)
	// openDisk and smartPoll are replaced in tests.
	openDisk  func(path string, write bool) (blockFile, error)
	smartPoll time.Duration

	testMu sync.Mutex
	tests  map[string]*DiskTest

	mu        sync.RWMutex
	volumes   []VolumeConfig
	loaded    bool
	smart     map[string]*Smart // by disk path
	smartTime time.Time
}

// NewManager returns a manager working on the real system.
func NewManager(store *config.Store) *Manager {
	return &Manager{store: store, runner: run.Exec{}, mountRoot: "/mnt", sysBlock: "/sys/block", smart: map[string]*Smart{}}
}

// Load reads the configured volumes from /config.
func (m *Manager) Load() error {
	var volumes []VolumeConfig
	err := m.store.ReadJSON(configFile, &volumes)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.volumes = volumes
	m.loaded = true
	return nil
}

func (m *Manager) configured() []VolumeConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]VolumeConfig(nil), m.volumes...)
}

func (m *Manager) find(name string) (VolumeConfig, bool) {
	for _, volume := range m.configured() {
		if volume.Name == name {
			return volume, true
		}
	}
	return VolumeConfig{}, false
}

// saveVolumes writes the list to /config and then takes it over.
func (m *Manager) saveVolumes(volumes []VolumeConfig) error {
	if volumes == nil {
		volumes = []VolumeConfig{}
	}
	if err := m.store.WriteJSON(configFile, volumes); err != nil {
		return err
	}
	m.mu.Lock()
	m.volumes = volumes
	m.mu.Unlock()
	return nil
}

// arrays reads the live md arrays.
func (m *Manager) arrays(ctx context.Context) []array {
	return readArrays(m.sysBlock, func(device string) string {
		output, err := m.runner.Run(ctx, "", "mdadm", "--detail", "--export", "/dev/"+device)
		if err != nil {
			return ""
		}
		return parseMdadmExport(output)["MD_UUID"]
	})
}

func (m *Manager) blockDevices(ctx context.Context) ([]blockDevice, error) {
	output, err := m.runner.Run(ctx, "", "lsblk", lsblkArgs...)
	if err != nil {
		return nil, err
	}
	return parseLsblk(output)
}

// Status returns disks and volumes with their current state. It is cheap
// and does not wake the stick or sleeping disks.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	devices, err := m.blockDevices(ctx)
	if err != nil {
		return Status{}, err
	}
	return m.status(devices, m.arrays(ctx)), nil
}

func (m *Manager) status(devices []blockDevice, arrays []array) Status {
	configured := m.configured()
	volumeByArray := map[string]string{}
	for _, volume := range configured {
		volumeByArray[normalizeUUID(volume.ArrayUUID)] = volume.Name
	}

	status := Status{Disks: []Disk{}, Volumes: []Volume{}}
	testing := m.testing()
	diskOfPart := map[string]string{} // sda1 → sda
	for _, device := range devices {
		if !isDisk(device) {
			continue
		}
		disk := classify(device, volumeByArray)
		m.mu.RLock()
		disk.Smart = m.smart[disk.Path]
		m.mu.RUnlock()
		disk.Testing = testing[disk.Name]
		status.Disks = append(status.Disks, disk)
		device.walk(func(node blockDevice) {
			diskOfPart[strings.TrimPrefix(node.KName, "/dev/")] = disk.Name
		})
	}

	mounts := readMounts()
	for _, cfg := range configured {
		volume := Volume{
			Name: cfg.Name, Level: cfg.Level, Filesystem: cfg.Filesystem,
			MountPoint: m.mountRoot + "/" + cfg.Name, State: VolumeMissing, Members: []Member{},
		}
		for _, a := range arrays {
			if a.uuid != normalizeUUID(cfg.ArrayUUID) {
				continue
			}
			volume.RaidDisks = a.disks
			volume.SizeBytes = a.sectors * 512
			volume.Mismatches = a.mismatchCount
			volume.State, volume.Sync = arrayState(a)
			for _, member := range a.members {
				state := memberState(member)
				diskName := diskOfPart[member.device]
				volume.Members = append(volume.Members, Member{Disk: diskName, State: state})
				for i := range status.Disks {
					if status.Disks[i].Name == diskName {
						status.Disks[i].Role = RoleMember
						status.Disks[i].Volume = cfg.Name
						status.Disks[i].MemberState = state
					}
				}
			}
			sort.Slice(volume.Members, func(i, j int) bool { return volume.Members[i].Disk < volume.Members[j].Disk })
		}
		if mounts[volume.MountPoint] {
			volume.Mounted = true
			var stat syscall.Statfs_t
			if syscall.Statfs(volume.MountPoint, &stat) == nil {
				volume.SizeBytes = stat.Blocks * uint64(stat.Bsize)
				volume.UsedBytes = (stat.Blocks - stat.Bfree) * uint64(stat.Bsize)
			}
		}
		status.Volumes = append(status.Volumes, volume)
	}
	return status
}

// arrayState derives the state of a volume from its array.
func arrayState(a array) (string, *Sync) {
	if a.state == "inactive" || a.state == "clear" || a.state == "" {
		return VolumeFailed, nil
	}
	var progress *Sync
	if a.syncTotal > 0 && a.syncAction != "idle" && a.syncAction != "frozen" {
		progress = &Sync{
			Percent:             100 * float64(a.syncDone) / float64(a.syncTotal),
			SpeedBytesPerSecond: a.syncSpeedKiB * 1024,
			RemainingSeconds:    -1,
		}
		if a.syncSpeedKiB > 0 && a.syncTotal >= a.syncDone {
			progress.RemainingSeconds = int64((a.syncTotal - a.syncDone) / 2 / a.syncSpeedKiB)
		}
	}
	switch a.syncAction {
	case "recover":
		return VolumeRebuilding, progress
	case "resync":
		return VolumeSyncing, progress
	case "check", "repair":
		return VolumeChecking, progress
	}
	if a.degraded > 0 {
		return VolumeDegraded, nil
	}
	return VolumeOK, nil
}

// readMounts returns the mount points that are in use.
func readMounts() map[string]bool {
	data, _ := os.ReadFile("/proc/self/mounts")
	mounts := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 {
			mounts[fields[1]] = true
		}
	}
	return mounts
}

// RefreshSmart reads the health of all disks. It can take seconds per disk,
// so it runs in the background and on request, never while a page loads.
func (m *Manager) RefreshSmart(ctx context.Context) {
	devices, err := m.blockDevices(ctx)
	if err != nil {
		slog.Warn("list disks", "err", err)
		return
	}
	smart := map[string]*Smart{}
	for _, device := range devices {
		if !isDisk(device) {
			continue
		}
		// smartctl reports problems of the disk in its exit status; the
		// JSON is complete either way.
		output, _ := m.runner.Run(ctx, "", "smartctl", append(smartctlArgs, device.Name)...)
		smart[device.Name] = parseSmart(output)
	}
	m.mu.Lock()
	m.smart = smart
	m.smartTime = time.Now()
	m.mu.Unlock()
}

// Run mounts the configured volumes as their arrays appear and keeps the
// SMART values fresh, until ctx is done.
func (m *Manager) Run(ctx context.Context) {
	m.RefreshSmart(ctx)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		// If the stick was not readable at start, keep trying: without the
		// configuration no volume gets mounted.
		m.mu.RLock()
		loaded := m.loaded
		m.mu.RUnlock()
		if !loaded {
			if err := m.Load(); err == nil {
				slog.Info("volumes read from /config")
			}
		}
		m.mountAll(ctx)
		m.mu.RLock()
		stale := time.Since(m.smartTime) > smartMaxAge
		m.mu.RUnlock()
		if stale {
			m.RefreshSmart(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Names returns the names of the configured volumes.
func (m *Manager) Names() []string {
	var names []string
	for _, volume := range m.configured() {
		names = append(names, volume.Name)
	}
	return names
}

// Mounted returns the configured volumes that are mounted right now.
func (m *Manager) Mounted() []string {
	mounts := readMounts()
	var names []string
	for _, volume := range m.configured() {
		if mounts[m.mountRoot+"/"+volume.Name] {
			names = append(names, volume.Name)
		}
	}
	return names
}

// MountPoint returns where a volume is mounted.
func (m *Manager) MountPoint(volume string) string {
	return m.mountRoot + "/" + volume
}
