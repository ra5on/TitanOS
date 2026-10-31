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
	"slices"
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
	MemberRemoving   = "removing" // Btrfs moves its data to the other disks
)

// States of a volume.
const (
	VolumeOK         = "ok"
	VolumeDegraded   = "degraded"   // a disk is missing, data still there
	VolumeRebuilding = "rebuilding" // a new disk is being filled
	VolumeSyncing    = "syncing"    // first sync after creation
	VolumeChecking   = "checking"
	VolumeReshaping  = "reshaping" // Btrfs: disks are added, removed or the profile changes
	VolumeFailed     = "failed"    // too many disks missing
	VolumeMissing    = "missing"   // none of its disks found
)

// Disk is a physical disk as shown in the UI.
type Disk struct {
	// SmartWarnings are the codes of judgeSmart (smart.go); SmartDefects
	// adds up the counters that only grow on a wearing disk.
	SmartWarnings     []string `json:"smartWarnings,omitempty"`
	SmartDefects      uint64   `json:"smartDefects,omitempty"`
	Identity          string   `json:"identity"`
	UnavailableReason string   `json:"unavailableReason,omitempty"`
	Name              string   `json:"name"` // sda
	Path              string   `json:"path"` // /dev/sda
	Model             string   `json:"model"`
	Serial            string   `json:"serial"`
	SizeBytes         uint64   `json:"sizeBytes"`
	Kind              string   `json:"kind"` // hdd, ssd, nvme, usb
	Role              string   `json:"role"`
	// Volume and MemberState are set for members.
	Volume      string `json:"volume,omitempty"`
	MemberState string `json:"memberState,omitempty"`
	// Contents names what was found on a used disk (ext4, ntfs, RAID …).
	Contents []string `json:"contents,omitempty"`
	Mounted  bool     `json:"mounted,omitempty"`
	// Testing is set while a test of the disk runs; nothing else may use it.
	Testing bool   `json:"testing,omitempty"`
	Smart   *Smart `json:"smart"`
	// wwn and ptUUID only go into Identity.
	wwn, ptUUID string
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
	Filesystem     string    `json:"filesystem"` // ext4, xfs, btrfs
	ArrayUUID      string    `json:"arrayUUID"`
	FilesystemUUID string    `json:"filesystemUUID"`
	Created        time.Time `json:"created"`
	// Set only for a newly created first volume; cleared after its folders
	// are durable. Existing/imported volumes are never populated implicitly.
	InitializeFolders bool `json:"initializeFolders,omitempty"`
	// ShareQuotas lets shares on an ext4 or XFS volume have a size limit.
	ShareQuotas bool `json:"shareQuotas,omitempty"`
	// Profile is the layout of a Btrfs volume: single, raid1 or raid1c3.
	// Missing means raid1, the only layout before 0.29.0.
	Profile string `json:"profile,omitempty"`
	// Devices is how many disks a Btrfs volume has; missing means two.
	Devices int `json:"devices,omitempty"`
	// Cache is the SSD cache of an mdadm volume (cache.go), off when nil.
	Cache *CacheConfig `json:"cache,omitempty"`
	// Encryption is LUKS2 below the file system (crypt.go), off when nil.
	// Older titand ignore it; they cannot mount such a volume and leave it be.
	Encryption *EncryptionConfig `json:"encryption,omitempty"`
}

// Volume is a configured volume with its live state.
type Volume struct {
	Name       string `json:"name"`
	Level      string `json:"level"`
	Filesystem string `json:"filesystem"`
	MountPoint string `json:"mountPoint"`
	Mounted    bool   `json:"mounted"`
	State      string `json:"state"`
	SizeBytes  uint64 `json:"sizeBytes"`
	UsedBytes  uint64 `json:"usedBytes"`
	RaidDisks  int    `json:"raidDisks"`
	// Cache is the SSD cache, if the volume has one.
	Cache *CacheView `json:"cache,omitempty"`
	// Encryption is set for an encrypted volume.
	Encryption *EncryptionView `json:"encryption,omitempty"`
	Members    []Member        `json:"members"`
	Sync       *Sync           `json:"sync"`
	// Mismatches is what the last check found: blocks whose copies differ.
	Mismatches uint64 `json:"mismatches"`
	// ShareQuotas is one of the Quotas* states.
	ShareQuotas      string `json:"shareQuotas"`
	ShareQuotasError string `json:"shareQuotasError,omitempty"`
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
	Disks     []Disk          `json:"disks"`
	Volumes   []Volume        `json:"volumes"`
	DiskSleep DiskSleepStatus `json:"diskSleep"`
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
	mounts    func() map[string]bool
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

	// OnTestInterrupted is told about every test that a restart stopped
	// and that waits to be continued.
	OnTestInterrupted func(DiskTest)
	// checkpointEvery and testStateVolumes are replaced in tests.
	checkpointEvery  time.Duration
	testStateVolumes func() []string

	testMu      sync.Mutex
	tests       map[string]*DiskTest
	testNotices []TestNotice
	// testSaveMu serializes the checkpoint file (disktest_resume.go):
	// the volume it lies on, which volumes have one and which were read.
	testSaveMu       sync.Mutex
	testStateVolume  string
	testStateFiles   map[string]bool
	testStateScanned map[string]bool

	// OnSnapshotRestoreDone is told about every folder restore that ends.
	OnSnapshotRestoreDone func(SnapshotFolderJob)
	restoreMu             sync.Mutex
	restores              []*SnapshotFolderJob
	// restoreStep is called before each file of a folder restore (tests).
	restoreStep func(name string)

	mu      sync.RWMutex
	volumes []VolumeConfig
	// quotaProblems are why share quotas could not start, by volume.
	quotaProblems map[string]string
	// quotaProbe replaces the kernel check in tests.
	quotaProbe   func(mountPoint string) (bool, error)
	growth       *GrowthPointer
	growthActive bool
	loaded       bool
	// settled is set once the volumes were read from /config and a first
	// attempt to mount them was made.
	settled    bool
	smart      map[string]*Smart // by disk path
	smartDisks map[string]*smartEntry
	smartTime  time.Time
	// smartRefresh serializes the smartctl rounds.
	smartRefresh    sync.Mutex
	diskSleep       DiskSleepSettings
	diskSleepLoaded bool
	diskSleepErrors []DiskSleepError
	// diskSleepAsleep are the disks sent to standby without activity
	// since, by device name (RAM only).
	diskSleepAsleep map[string]bool
	// sleepSamples is protected by actions, never persisted on the stick.
	sleepSamples  map[string]sleepSample
	btrfsProgress map[string]btrfsProgress
	// btrfsSeen is when mountAll first saw a member of an unmounted Btrfs
	// mirror (protected by actions). clock replaces time.Now in tests.
	btrfsSeen map[string]time.Time
	// btrfsRemoving names the disk being taken out of a Btrfs volume.
	btrfsRemoving map[string]string
	// OnReshaped is told when taking a disk out of a Btrfs volume ended.
	OnReshaped func(volume string, err error)
	// OnCacheMissing is told when a volume's cache SSD is not there at
	// mount; OnCacheRemoved when a write cache was written back and removed.
	OnCacheMissing, OnCacheRemoved func(volume string)
	partUUIDDir                    string
	zero                           func(part string) error
	clock                          func() time.Time

	// Encryption (crypt.go). OnLocked is told once per start that an
	// encrypted volume waits for its password, OnUnlocked when it was
	// unlocked. BeforeLock stops what runs on a volume before it is locked
	// and returns how to start it again if locking fails.
	OnLocked, OnUnlocked func(volume string)
	BeforeLock           func(ctx context.Context, volume string) (undo func(), err error)
	// Protected by mu.
	cryptAttempts map[string]cryptAttempt
	cryptSlots    map[string]int
	lockNotified  map[string]bool
	locking       map[string]bool
	// mapperDir, cryptTemp and lockWait are replaced in tests.
	mapperDir, cryptTemp string
	lockWait             time.Duration
}

// NewManager returns a manager working on the real system.
func NewManager(store *config.Store) *Manager {
	return &Manager{store: store, runner: timeoutRunner{}, mountRoot: "/mnt", sysBlock: "/sys/block", smart: map[string]*Smart{}}
}

// Load reads the configured volumes from /config.
func (m *Manager) Load() error {
	var volumes []VolumeConfig
	err := m.store.ReadJSON(configFile, &volumes)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := m.loadGrowth(); err != nil {
		return err
	}
	m.mu.Lock()
	m.volumes = volumes
	m.loaded = true
	m.mu.Unlock()
	return m.loadDiskSleep()
}

func (m *Manager) configured() []VolumeConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]VolumeConfig(nil), m.volumes...)
}

// FilesystemUUIDs returns the filesystem UUIDs of the volumes of the NAS.
func (m *Manager) FilesystemUUIDs() []string {
	var uuids []string
	for _, volume := range m.configured() {
		if volume.FilesystemUUID != "" {
			uuids = append(uuids, volume.FilesystemUUID)
		}
	}
	return uuids
}

// Owns reports a filesystem, RAID array or LUKS UUID of a configured
// volume, in any of the spellings of lsblk and mdadm, or the PARTUUID of a
// partition of its SSD cache (those carry no filesystem to know them by).
func (m *Manager) Owns(uuid string) bool {
	uuid = normalizeUUID(uuid)
	if uuid == "" {
		return false
	}
	for _, volume := range m.configured() {
		if normalizeUUID(volume.FilesystemUUID) == uuid || normalizeUUID(volume.ArrayUUID) == uuid {
			return true
		}
		if volume.Encryption != nil && slices.ContainsFunc(volume.Encryption.UUIDs, func(u string) bool { return normalizeUUID(u) == uuid }) {
			return true
		}
		if c := volume.Cache; c != nil && (normalizeUUID(c.MetaUUID) == uuid || normalizeUUID(c.DataUUID) == uuid) {
			return true
		}
	}
	return false
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
	status := m.status(ctx, devices, m.arrays(ctx))
	status.DiskSleep = m.diskSleepStatus(devices)
	return status, nil
}

// volumesByUUID maps the UUIDs found on member partitions (md array UUID,
// Btrfs UUID, LUKS UUIDs of encrypted Btrfs members) to the volume name.
func volumesByUUID(configured []VolumeConfig) map[string]string {
	volumeByArray := map[string]string{}
	for _, volume := range configured {
		uuid := volume.ArrayUUID
		if volume.Filesystem == "btrfs" {
			uuid = volume.FilesystemUUID
		}
		if uuid = normalizeUUID(uuid); uuid != "" {
			volumeByArray[uuid] = volume.Name
		}
		// A locked Btrfs member shows only its LUKS container.
		if volume.Encryption != nil && volume.Filesystem == "btrfs" {
			for _, uuid := range volume.Encryption.UUIDs {
				volumeByArray[normalizeUUID(uuid)] = volume.Name
			}
		}
	}
	return volumeByArray
}

func (m *Manager) status(ctx context.Context, devices []blockDevice, arrays []array) Status {
	configured := m.configured()
	volumeByArray := volumesByUUID(configured)
	status := Status{Disks: []Disk{}, Volumes: []Volume{}, DiskSleep: m.DiskSleep()}
	testing := m.testing()
	diskOfPart := map[string]string{} // sda1 → sda
	for _, device := range devices {
		if !isDisk(device) {
			continue
		}
		disk := classify(device, volumeByArray)
		m.mu.RLock()
		disk.Smart = m.smart[disk.Path]
		if entry := m.smartDisks[disk.Path]; entry != nil && entry.values != nil {
			values := *entry.values
			_, warnings := judgeSmart(&values, disk.Kind)
			disk.SmartWarnings, disk.SmartDefects = warnings, smartDefects(&values)
		}
		m.mu.RUnlock()
		disk.Testing = testing[disk.Name]
		status.Disks = append(status.Disks, disk)
		device.walk(func(node blockDevice) {
			diskOfPart[strings.TrimPrefix(node.KName, "/dev/")] = disk.Name
		})
	}

	mounts := m.readMounts()
	for _, cfg := range configured {
		volume := Volume{
			Name: cfg.Name, Level: cfg.Level, Filesystem: cfg.Filesystem,
			MountPoint: m.mountRoot + "/" + cfg.Name, State: VolumeMissing, Members: []Member{},
		}
		for _, a := range arrays {
			if a.uuid == "" || cfg.Filesystem == "btrfs" || a.uuid != normalizeUUID(cfg.ArrayUUID) {
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
		if cfg.Filesystem == "btrfs" {
			m.btrfsStatus(&volume, &status, devices, cfg)
		}
		if cfg.Cache != nil {
			volume.Cache = m.cacheView(ctx, cfg)
			// The cache SSD belongs to the volume: never offered elsewhere.
			for _, device := range devices {
				device.walk(func(node blockDevice) {
					if node.PartUUID == "" || !strings.EqualFold(node.PartUUID, cfg.Cache.MetaUUID) && !strings.EqualFold(node.PartUUID, cfg.Cache.DataUUID) {
						return
					}
					for i := range status.Disks {
						if status.Disks[i].Name == diskOfPart[strings.TrimPrefix(node.KName, "/dev/")] {
							status.Disks[i].Role, status.Disks[i].Volume = RoleMember, cfg.Name
						}
					}
				})
			}
		}
		volume.Encryption = m.encryptionView(cfg)
		if mounts[volume.MountPoint] && !m.isLocking(cfg.Name) {
			volume.Mounted = true
			var stat syscall.Statfs_t
			if syscall.Statfs(volume.MountPoint, &stat) == nil {
				volume.SizeBytes = stat.Blocks * uint64(stat.Bsize)
				volume.UsedBytes = (stat.Blocks - stat.Bfree) * uint64(stat.Bsize)
			}
		}
		m.quotaState(cfg, &volume)
		status.Volumes = append(status.Volumes, volume)
	}
	claimed := map[string]string{}
	if m.Claimed != nil {
		claimed = m.Claimed()
	}
	for i := range status.Disks {
		disk := &status.Disks[i]
		disk.Identity = diskIdentity(*disk)
		disk.UnavailableReason = diskUnavailable(*disk)
		if claimed[disk.Name] != "" {
			disk.UnavailableReason = "vm"
		}
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
// A disk in standby is left asleep and keeps the values read before.
func (m *Manager) RefreshSmart(ctx context.Context) {
	m.smartRefresh.Lock()
	defer m.smartRefresh.Unlock()
	m.refreshSmart(ctx)
}

// refreshSmart does the work of RefreshSmart; smartRefresh is held.
func (m *Manager) refreshSmart(ctx context.Context) {
	devices, err := m.blockDevices(ctx)
	if err != nil {
		slog.Warn("list disks", "err", err)
		return
	}
	m.mu.RLock()
	before, beforeSmart := m.smartDisks, m.smart
	m.mu.RUnlock()
	now := m.now()
	smart := map[string]*Smart{}
	entries := map[string]*smartEntry{}
	for _, device := range devices {
		if !isDisk(device) {
			continue
		}
		// smartctl reports problems of the disk in its exit status; the
		// JSON is complete either way.
		output, _ := m.runner.Run(ctx, "", "smartctl", append(smartctlArgs, device.Name)...)
		entry := &smartEntry{checked: now}
		if smartSleeping(output) {
			entry.sleeping = true
			if old := before[device.Name]; old != nil {
				entry.read, entry.values, entry.raw = old.read, old.values, old.raw
			}
			smart[device.Name] = beforeSmart[device.Name]
		} else {
			entry.values = parseSmartValues(output)
			if entry.values != nil {
				entry.read, entry.raw = now, output
			}
			smart[device.Name] = parseSmart(output)
		}
		entries[device.Name] = entry
	}
	m.mu.Lock()
	m.smart = smart
	m.smartDisks = entries
	m.smartTime = now
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
		m.refreshBtrfs(ctx)
		// A mirror still waiting for its second member is not settled: the
		// boot check must not count it as missing before the grace is over.
		// After that it is settled even if mounting fails, so that a broken
		// mirror does not undo a system update.
		if loaded && !m.btrfsWaiting() {
			m.mu.Lock()
			m.settled = true
			m.mu.Unlock()
		}
		if m.Settled() {
			// Disk tests that a restart stopped, from volumes mounted since.
			m.ResumeSavedTests(ctx)
		}
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

// Settled reports whether the volumes are known and were looked after once.
func (m *Manager) Settled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settled
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
	mounts := m.readMounts()
	var names []string
	for _, volume := range m.configured() {
		// A volume being locked is let go of by everything that asks.
		if mounts[m.mountRoot+"/"+volume.Name] && !m.isLocking(volume.Name) {
			names = append(names, volume.Name)
		}
	}
	return names
}

// MountPoint returns where a volume is mounted.
func (m *Manager) MountPoint(volume string) string {
	return m.mountRoot + "/" + volume
}

// ActionsRunning includes synchronous storage work, such as restoring snapshot
// files, which is not represented by a disk-test or background reshape job.
func (m *Manager) ActionsRunning() bool {
	if !m.actions.TryLock() {
		return true
	}
	m.actions.Unlock()
	return false
}

func (m *Manager) now() time.Time {
	if m.clock != nil {
		return m.clock()
	}
	return time.Now()
}

func (m *Manager) readMounts() map[string]bool {
	if m.mounts != nil {
		return m.mounts()
	}
	return readMounts()
}

// UseRunner makes the manager run commands through runner. For tests of
// other packages.
func (m *Manager) UseRunner(runner Runner) {
	m.runner = runner
}

// UseMounts makes the manager see volumes as mounted below root as mounted
// says, instead of reading /proc. For tests of other packages.
func (m *Manager) UseMounts(root string, mounted func() map[string]bool) {
	m.mountRoot, m.mounts = root, mounted
}
