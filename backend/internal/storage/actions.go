package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/naming"
)

// Errors a request can be answered with. The API turns them into codes.
var (
	ErrNotLoaded       = errors.New("storage: configuration could not be read from /config")
	ErrInvalidName     = errors.New("storage: invalid volume name")
	ErrExists          = errors.New("storage: a volume with this name exists")
	ErrNotFound        = errors.New("storage: no such volume")
	ErrInvalidLevel    = errors.New("storage: unknown RAID level")
	ErrInvalidFS       = errors.New("storage: unknown filesystem")
	ErrTooFewDisks     = errors.New("storage: not enough disks for this RAID level")
	ErrDiskUnavailable = errors.New("storage: disk cannot be used")
	ErrDiskTooSmall    = errors.New("storage: disk is too small")
	ErrNotConfirmed    = errors.New("storage: erasing was not confirmed")
	ErrBusy            = errors.New("storage: volume is busy")
	ErrNotPossible     = errors.New("storage: not possible in the current state")
	ErrReadOnly        = errors.New("storage: disks are not changed in development mode")
)

// minDisks is the smallest number of disks per RAID level.
var minDisks = map[string]int{"raid0": 1, "raid1": 2, "raid5": 3, "raid6": 4, "raid10": 4}

var filesystems = []string{"ext4", "xfs", "btrfs"}

const (
	mebibyte = 1 << 20
	// The partition leaves this much of the disk unused, so that a
	// replacement disk that is a little smaller still fits.
	reserveBytes = 128 * mebibyte
	minDiskBytes = 1 << 30
	// GPT type "Linux RAID".
	raidPartitionType = "A19D880F-05FC-4D3B-A006-743F0F84911E"
)

// CreateRequest describes a new volume.
type CreateRequest struct {
	Name       string   `json:"name"`
	Level      string   `json:"level"`
	Filesystem string   `json:"filesystem"`
	Disks      []string `json:"disks"` // names like "sda"
	// Erase repeats the disks to be wiped. Creating a volume destroys
	// everything on them, so the caller has to say so explicitly.
	Erase      []string          `json:"erase"`
	Identities map[string]string `json:"identities,omitempty"`
	// Encrypt puts LUKS2 below the file system (crypt.go), with Password
	// as its first password. Never stored, never logged.
	Encrypt  bool   `json:"encrypt,omitempty"`
	Password string `json:"password,omitempty"`
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y) && len(slices.Compact(x)) == len(a)
}

// usable is the check of the same name plus what only the manager knows:
// a disk that a VM uses as a whole is not for a volume.
func (m *Manager) usable(status Status, name string) (Disk, error) {
	if m.Claimed != nil {
		if by := m.Claimed()[name]; by != "" {
			return Disk{}, fmt.Errorf("%w: %s belongs to %s", ErrDiskUnavailable, name, by)
		}
	}
	return usable(status, name)
}

// usable returns the disk with the given name if a volume may take it:
// it exists, is not the stick, not in a volume and nothing on it is mounted.
func usable(status Status, name string) (Disk, error) {
	for _, disk := range status.Disks {
		if disk.Name != name {
			continue
		}
		if disk.Role == RoleStick || disk.Role == RoleMember || disk.Mounted {
			return Disk{}, fmt.Errorf("%w: %s is in use", ErrDiskUnavailable, name)
		}
		if disk.Testing {
			return Disk{}, fmt.Errorf("%w: %s is being tested", ErrDiskUnavailable, name)
		}
		if disk.SizeBytes < minDiskBytes {
			return Disk{}, fmt.Errorf("%w: %s", ErrDiskTooSmall, name)
		}
		return disk, nil
	}
	return Disk{}, fmt.Errorf("%w: %s not found", ErrDiskUnavailable, name)
}

// memberSizeMiB is the size of the RAID partition on a disk of the given size.
func memberSizeMiB(diskBytes uint64) uint64 {
	return (diskBytes - mebibyte - reserveBytes) / mebibyte
}

// memberSectors is the size of the largest member partition of an array,
// in 512-byte sectors, as the kernel reports it.
func (m *Manager) memberSectors(a array) uint64 {
	var sectors uint64
	for _, member := range a.members {
		sectors = max(sectors, readSysfsUint(filepath.Join(filepath.Dir(m.sysBlock), "class", "block", member.device, "size")))
	}
	return sectors
}

// prepareDisk wipes a disk and creates the one RAID partition on it.
func (m *Manager) prepareDisk(ctx context.Context, device blockDevice, volume string, sizeMiB uint64) (string, error) {
	// Remove every signature, innermost first, so that nothing on the disk
	// is recognized as an old filesystem or array afterwards.
	// Only the disk and its own partitions are wiped. Nothing below an array
	// or a device-mapper/LVM/crypt holder belongs to this disk alone: it may be
	// a filesystem spread over other disks. Callers refuse such holders first.
	var nodes []blockDevice
	var collect func(node blockDevice)
	collect = func(node blockDevice) {
		if !ownedByDisk(node) {
			return
		}
		nodes = append(nodes, node)
		for _, child := range node.Children {
			collect(child)
		}
	}
	collect(device)
	slices.Reverse(nodes)
	for _, node := range nodes {
		if node.FSType == "linux_raid_member" {
			if _, err := m.runner.Run(ctx, "", "mdadm", "--zero-superblock", "--force", node.Name); err != nil {
				return "", err
			}
		}
		if _, err := m.runner.Run(ctx, "", "wipefs", "--all", "--force", node.Name); err != nil {
			return "", err
		}
	}

	partitionType := raidPartitionType
	if strings.HasPrefix(volume, "btrfs:") {
		partitionType = "0FC63DAF-8483-4772-8E79-3D69D8477DE4"
		volume = strings.TrimPrefix(volume, "btrfs:")
	}
	table := fmt.Sprintf("label: gpt\nstart=1MiB, size=%dMiB, type=%s, name=titanos-%s\n", sizeMiB, partitionType, volume)
	if _, err := m.runner.Run(ctx, table, "sfdisk", "--quiet", "--wipe", "always", "--wipe-partitions", "always", device.Name); err != nil {
		return "", err
	}
	if _, err := m.runner.Run(ctx, "", "udevadm", "settle"); err != nil {
		return "", err
	}
	return partitionPath(device.Name), nil
}

func findDevice(devices []blockDevice, name string) (blockDevice, bool) {
	for _, device := range devices {
		if device.Name == "/dev/"+name {
			return device, true
		}
	}
	return blockDevice{}, false
}

// Create builds a new volume: partitions the disks, creates the array and
// the filesystem, mounts it and records it in /config.
// Everything on the disks is destroyed.
func (m *Manager) Create(ctx context.Context, req CreateRequest) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return ErrReadOnly
	}
	if m.GrowthPending() {
		return ErrBusy
	}

	m.mu.RLock()
	loaded := m.loaded
	m.mu.RUnlock()
	if !loaded {
		return ErrNotLoaded
	}
	if !naming.ValidVolume(req.Name) {
		return ErrInvalidName
	}
	if slices.ContainsFunc(m.configured(), func(v VolumeConfig) bool { return strings.EqualFold(v.Name, req.Name) }) {
		return ErrExists
	}
	if !slices.Contains(filesystems, req.Filesystem) {
		return ErrInvalidFS
	}
	need, ok := minDisks[req.Level]
	if req.Filesystem == "btrfs" {
		// Btrfs has its own profiles: single, raid1, raid1c3.
		layout, known := btrfsLayouts[req.Level]
		need, ok = layout.minDisks, known
	}
	if !ok {
		return ErrInvalidLevel
	}
	if len(req.Disks) < need || (req.Level == "raid10" && len(req.Disks)%2 != 0) {
		return ErrTooFewDisks
	}
	if !sameSet(req.Disks, req.Erase) {
		return ErrNotConfirmed
	}
	if _, err := os.Stat(filepath.Join(m.mountRoot, req.Name)); err == nil && m.readMounts()[filepath.Join(m.mountRoot, req.Name)] {
		return ErrExists
	}
	var password []byte
	if req.Encrypt {
		password = []byte(req.Password)
		defer wipe(password)
		if err := validPassword(password); err != nil {
			return err
		}
	}

	devices, err := m.blockDevices(ctx)
	if err != nil {
		return err
	}
	status := m.status(devices, m.arrays(ctx))
	if err := validateDiskIdentities(req, status); err != nil {
		return err
	}
	smallest := ^uint64(0)
	for _, name := range req.Disks {
		disk, err := m.usable(status, name)
		if err != nil {
			return err
		}
		smallest = min(smallest, disk.SizeBytes)
	}
	devices, err = m.releaseOldArrays(ctx, devices, req.Disks)
	if err != nil {
		return err
	}
	sizeMiB := memberSizeMiB(smallest)

	slog.Info("creating volume", "name", req.Name, "level", req.Level, "filesystem", req.Filesystem, "disks", req.Disks)
	if req.Filesystem == "btrfs" {
		return m.createBtrfs(ctx, req, devices, sizeMiB, password)
	}
	var parts []string
	for _, name := range req.Disks {
		device, _ := findDevice(devices, name)
		part, err := m.prepareDisk(ctx, device, req.Name, sizeMiB)
		if err != nil {
			return err
		}
		parts = append(parts, part)
	}

	arrayPath := "/dev/md/" + req.Name
	args := []string{"--create", arrayPath, "--run", "--quiet", "--metadata=1.2", "--homehost=any",
		"--name=" + req.Name, "--level=" + strings.TrimPrefix(req.Level, "raid")}
	if req.Level == "raid0" && len(parts) == 1 {
		// mdadm requires --force before --raid-devices=1 for a single-disk array.
		args = append(args, "--force")
	}
	args = append(args, fmt.Sprintf("--raid-devices=%d", len(parts)))
	if req.Level != "raid0" {
		// The bitmap lets a disk that dropped out briefly catch up quickly
		// instead of being rebuilt from scratch.
		args = append(args, "--bitmap=internal")
	}
	if _, err := m.runner.Run(ctx, "", "mdadm", append(args, parts...)...); err != nil {
		m.undoCreate(ctx, req, "", parts)
		return err
	}
	arrayUUID, _, encryption, fsUUID, err := m.formatArray(ctx, req, arrayPath, password)
	if err != nil {
		// The disks were given up for this volume: leave nothing behind
		// that looks like an array, a LUKS container or a file system.
		m.undoCreate(ctx, req, arrayPath, parts)
		return err
	}

	volume := VolumeConfig{
		Name: req.Name, Level: req.Level, Filesystem: req.Filesystem,
		ArrayUUID: arrayUUID, FilesystemUUID: fsUUID,
		Created:           time.Now().UTC().Truncate(time.Second),
		InitializeFolders: len(m.configured()) == 0,
		Encryption:        encryption,
	}
	// Record the volume before mounting: if the stick cannot be written,
	// the array exists but is not lost – it shows up as a foreign RAID.
	if err := m.saveVolumes(append(m.configured(), volume)); err != nil {
		return fmt.Errorf("volume created but not saved to /config: %w", err)
	}
	return m.mount(ctx, volume)
}

// formatArray turns a freshly created array into the volume's file system,
// inside LUKS when asked to, and returns the array UUID, the device the
// file system is on, the encryption and the file system UUID.
func (m *Manager) formatArray(ctx context.Context, req CreateRequest, arrayPath string, password []byte) (string, string, *EncryptionConfig, string, error) {
	if _, err := m.runner.Run(ctx, "", "udevadm", "settle"); err != nil {
		return "", "", nil, "", err
	}
	detail, err := m.runner.Run(ctx, "", "mdadm", "--detail", "--export", arrayPath)
	if err != nil {
		return "", "", nil, "", err
	}
	arrayUUID := parseMdadmExport(detail)["MD_UUID"]
	if !validUUID(mdUUIDDashed(arrayUUID)) {
		return "", "", nil, "", errors.New("storage: new array has no UUID")
	}
	// An encrypted volume has its file system inside LUKS on the array.
	target := arrayPath
	var encryption *EncryptionConfig
	if req.Encrypt {
		target, encryption, err = m.encryptArray(ctx, req.Name, arrayPath, password)
		if err != nil {
			return "", "", nil, "", err
		}
	}
	mkfs := []string{"-F", "-q", "-L", req.Name, "-m", "0", target}
	if req.Filesystem == "xfs" {
		mkfs = []string{"-f", "-q", "-L", req.Name, target}
	}
	if _, err := m.runner.Run(ctx, "", "mkfs."+req.Filesystem, mkfs...); err != nil {
		return "", "", nil, "", err
	}
	out, err := m.runner.Run(ctx, "", "blkid", "--match-tag", "UUID", "--output", "value", target)
	if err != nil {
		return "", "", nil, "", err
	}
	fsUUID := strings.TrimSpace(out)
	if !validUUID(fsUUID) {
		return "", "", nil, "", fmt.Errorf("storage: new file system on %s has no valid UUID (%q)", target, fsUUID)
	}
	return arrayUUID, target, encryption, fsUUID, nil
}

// undoCreate takes apart what a failed Create built after the disks were
// partitioned: the LUKS container is closed, signatures inside the array
// (LUKS, file system) and the array itself are removed and the member
// partitions and disks wiped. arrayPath is empty when mdadm --create
// itself failed. Best effort, errors are logged: the volume was never
// recorded, and the disks had been given up for it.
func (m *Manager) undoCreate(ctx context.Context, req CreateRequest, arrayPath string, parts []string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	slog.Warn("creating volume failed, cleaning up the disks", "name", req.Name, "disks", req.Disks)
	try := func(name string, args ...string) {
		if _, err := m.runner.Run(ctx, "", name, args...); err != nil {
			slog.Warn("clean up after failed create", "command", name+" "+strings.Join(args, " "), "err", err)
		}
	}
	if arrayPath != "" {
		if req.Encrypt {
			if err := m.luksClose(ctx, cryptName(VolumeConfig{Name: req.Name}, "")); err != nil {
				slog.Warn("clean up after failed create: close LUKS", "err", err)
			}
		}
		try("wipefs", "--all", "--force", arrayPath)
		try("mdadm", "--stop", arrayPath)
	}
	for _, part := range parts {
		try("mdadm", "--zero-superblock", "--force", part)
		try("wipefs", "--all", "--force", part)
	}
	for _, disk := range req.Disks {
		try("wipefs", "--all", "--force", "/dev/"+disk)
	}
	try("udevadm", "settle")
}

// arrayOf returns the live array of a volume.
func (m *Manager) arrayOf(ctx context.Context, volume VolumeConfig) (array, bool) {
	// A Btrfs mirror has no array; an array whose UUID mdadm could not tell
	// is nobody's. An empty UUID never matches.
	uuid := normalizeUUID(volume.ArrayUUID)
	if uuid == "" || volume.Filesystem == "btrfs" {
		return array{}, false
	}
	for _, a := range m.arrays(ctx) {
		if a.uuid == uuid {
			return a, true
		}
	}
	return array{}, false
}

func (m *Manager) mount(ctx context.Context, volume VolumeConfig) error {
	return m.mountWith(ctx, volume, false)
}

// mountWith mounts a volume. degraded lets a Btrfs mirror start with a
// member missing; mountAll asks for it only after waiting for all members.
func (m *Manager) mountWith(ctx context.Context, volume VolumeConfig, degraded bool) error {
	mountPoint := filepath.Join(m.mountRoot, volume.Name)
	if err := os.MkdirAll(mountPoint, 0o755); err != nil {
		return err
	}
	// The source first: quotas are prepared on exactly the device that
	// gets mounted.
	source, err := m.mountSource(ctx, volume)
	if err != nil {
		return err
	}
	quotas := m.quotaMountOptions(ctx, volume, source)
	options := "noatime" + quotas
	if volume.Filesystem == "btrfs" {
		options += ",subvolid=5"
		if degraded {
			options += ",degraded"
		}
	}
	_, err = m.runner.Run(ctx, "", "mount", "-t", volume.Filesystem, "-o", options, source, mountPoint)
	if err != nil && quotas != "" {
		// Quotas never keep a volume from mounting.
		slog.Warn("mount with project quotas failed, mounting without", "volume", volume.Name, "err", err)
		m.setQuotaProblem(volume.Name, err.Error())
		_, err = m.runner.Run(ctx, "", "mount", "-t", volume.Filesystem, "-o", strings.TrimSuffix(options, quotas), source, mountPoint)
	}
	if err != nil {
		return err
	}
	return m.initializeFolders(volume)
}

// mountAll mounts every configured volume whose array is running and that
// is not mounted yet. Arrays can appear late (slow disks, degraded start).
func (m *Manager) mountAll(ctx context.Context) {
	if m.ReadOnly {
		return
	}
	if !m.actions.TryLock() {
		return // an action is rearranging things; next round
	}
	defer m.actions.Unlock()

	configured := m.configured()
	if len(configured) == 0 {
		return
	}
	mounts := m.readMounts()
	var arrays []array
	for _, volume := range configured {
		if mounts[filepath.Join(m.mountRoot, volume.Name)] {
			if err := m.initializeFolders(volume); err != nil {
				slog.Warn("initialize volume folders", "name", volume.Name, "err", err)
			}
			// Unlocked before titand started: learn its key slots once.
			if volume.Encryption != nil && !m.knowsPasswords(volume.Name) {
				m.readPasswords(ctx, volume)
			}
			continue
		}
		// An encrypted volume waits for its password (Unlock).
		if m.isLocked(volume) {
			m.noticeLocked(ctx, volume)
			continue
		}
		if volume.Filesystem == "btrfs" {
			if devices, err := m.blockDevices(ctx); err == nil {
				m.mountBtrfs(ctx, volume, devices)
			}
			continue
		}
		if arrays == nil {
			arrays = m.arrays(ctx)
		}
		for _, a := range arrays {
			if a.uuid == "" || a.uuid != normalizeUUID(volume.ArrayUUID) || a.state == "inactive" || a.state == "clear" {
				continue
			}
			if err := m.mount(ctx, volume); err != nil {
				slog.Warn("mount volume", "name", volume.Name, "err", err)
			} else {
				slog.Info("volume mounted", "name", volume.Name)
			}
		}
	}
}

// Delete removes a volume and wipes its disks. All data on it is destroyed.
// confirm must repeat the name.
func (m *Manager) Delete(ctx context.Context, name, confirm string) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return ErrReadOnly
	}
	if m.GrowthPending() {
		return ErrBusy
	}

	volume, ok := m.find(name)
	if !ok {
		return ErrNotFound
	}
	if confirm != name {
		return ErrNotConfirmed
	}
	if m.restoreUses(name, "") {
		return fmt.Errorf("%w: a snapshot folder restore is running", ErrBusy)
	}

	if volume.Filesystem == "btrfs" {
		return m.deleteBtrfs(ctx, volume)
	}
	slog.Info("deleting volume", "name", name)
	mountPoint := filepath.Join(m.mountRoot, name)
	if m.readMounts()[mountPoint] {
		if _, err := m.runner.Run(ctx, "", "umount", mountPoint); err != nil {
			return fmt.Errorf("%w: %v", ErrBusy, err)
		}
	}

	if a, running := m.arrayOf(ctx, volume); running {
		if volume.Encryption != nil {
			// Close LUKS and destroy its key slots: nothing on the array
			// can be read again.
			if err := m.luksClose(ctx, cryptName(volume, "")); err != nil {
				return fmt.Errorf("%w: %v", ErrBusy, err)
			}
			md := "/dev/" + a.device
			device, err := m.lowerDevice(ctx, volume, md)
			if err != nil {
				device = md // a write cache whose SSD is gone: erase the array itself
			}
			if device != md {
				// Through a cache: write back what is dirty and switch to
				// writethrough first, so that the erased header reaches the
				// disks (and replaces any copy on the SSD) instead of
				// waiting on the SSD as a dirty block that removing the
				// cache device would drop.
				if err := m.flushCache(ctx, volume, cacheFlushWait); err != nil {
					return fmt.Errorf("%w: the SSD cache could not be written back yet, try again: %v", ErrBusy, err)
				}
			}
			if err := m.eraseLUKS(ctx, device); err != nil {
				return err
			}
			// erase leaves the LUKS header with empty key slots inside
			// the array; wipefs on the members below cannot see it (it
			// starts at the data offset). Remove its signatures here, so
			// that a reassembled array is not offered as LUKS again.
			if _, err := m.runner.Run(ctx, "", "wipefs", "--all", "--force", device); err != nil {
				return fmt.Errorf("wipe LUKS header: %w", err)
			}
		}
		// The cache device holds the array open (also one left detached,
		// as a linear mapping, after the cache was switched off).
		if _, err := m.runner.Run(ctx, "", "dmsetup", "status", cacheName(name)); err == nil {
			if err := m.removeMapper(ctx, cacheName(name)); err != nil {
				return fmt.Errorf("%w: %v", ErrBusy, err)
			}
		}
		if volume.Cache != nil {
			m.clearCacheSSD(ctx, volume.Cache)
		}
		if _, err := m.runner.Run(ctx, "", "mdadm", "--stop", "/dev/"+a.device); err != nil {
			return err
		}
		for _, member := range a.members {
			part := "/dev/" + member.device
			// A disk that has already failed may not answer any more.
			if _, err := m.runner.Run(ctx, "", "mdadm", "--zero-superblock", "--force", part); err != nil {
				slog.Warn("clear array member", "device", part, "err", err)
			}
			if _, err := m.runner.Run(ctx, "", "wipefs", "--all", "--force", part); err != nil {
				slog.Warn("wipe array member", "device", part, "err", err)
			}
		}
		m.wipePartitionTables(ctx, a)
	}

	remaining := slices.DeleteFunc(m.configured(), func(v VolumeConfig) bool { return v.Name == name })
	if err := m.saveVolumes(remaining); err != nil {
		return err
	}
	os.Remove(mountPoint)
	return nil
}

// wipePartitionTables turns the former members of an array back into empty disks.
func (m *Manager) wipePartitionTables(ctx context.Context, a array) {
	m.runner.Run(ctx, "", "udevadm", "settle")
	devices, err := m.blockDevices(ctx)
	if err != nil {
		return
	}
	for _, device := range devices {
		for _, part := range device.partitions() {
			if !slices.ContainsFunc(a.members, func(member arrayMember) bool { return "/dev/"+member.device == part.KName }) {
				continue
			}
			if _, err := m.runner.Run(ctx, "", "wipefs", "--all", "--force", device.Name); err != nil {
				slog.Warn("wipe disk", "device", device.Name, "err", err)
			}
		}
	}
	m.runner.Run(ctx, "", "udevadm", "settle")
}

// AddDisk adds a disk to a volume. If a disk is missing the new one takes
// its place and is rebuilt; otherwise it waits as a spare that steps in
// automatically when a disk fails. Everything on the disk is destroyed.
// identity, if not empty, is the disk identity the caller picked (as the disk
// list shows it); the disk is refused when it no longer matches.
func (m *Manager) AddDisk(ctx context.Context, name, diskName string, erase bool, identity string) error {
	return m.AddDiskWithPassword(ctx, name, diskName, erase, identity, nil)
}

// AddDiskWithPassword is AddDisk for encrypted volumes as well. A new
// member of an encrypted Btrfs volume gets its own LUKS container with
// password, which must open the volume; an mdadm volume needs none (the
// disk joins the array below the encryption).
func (m *Manager) AddDiskWithPassword(ctx context.Context, name, diskName string, erase bool, identity string, password []byte) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return ErrReadOnly
	}
	if m.GrowthPending() {
		return ErrBusy
	}

	volume, ok := m.find(name)
	if !ok {
		return ErrNotFound
	}
	if !erase {
		return ErrNotConfirmed
	}
	if volume.Filesystem == "btrfs" {
		// A missing member is replaced; a complete volume grows by a disk
		// and keeps its profile.
		devices, err := m.blockDevices(ctx)
		if err != nil {
			return err
		}
		if len(btrfsParts(devices, volume.FilesystemUUID)) < btrfsDiskCount(volume) || m.btrfsState(volume.Name).Missing > 0 {
			return m.replaceMissingBtrfs(ctx, volume, diskName, identity, password)
		}
		return m.addBtrfsDisk(ctx, volume, diskName, "", identity, password)
	}
	if volume.Level == "raid0" {
		return fmt.Errorf("%w: RAID 0 has no redundancy to rebuild from", ErrNotPossible)
	}
	a, running := m.arrayOf(ctx, volume)
	if !running {
		return fmt.Errorf("%w: the volume is not running", ErrNotPossible)
	}

	slog.Info("adding disk to volume", "name", name, "disk", diskName)
	part, err := m.prepareMember(ctx, name, a, diskName, identity)
	if err != nil {
		return err
	}
	_, err = m.runner.Run(ctx, "", "mdadm", "--manage", "/dev/"+a.device, "--add", part)
	return err
}

// prepareMember checks a new disk for array a and gives it the one RAID
// partition, at least as large as the members' (called with actions held).
func (m *Manager) prepareMember(ctx context.Context, name string, a array, diskName, identity string) (string, error) {
	devices, err := m.blockDevices(ctx)
	if err != nil {
		return "", err
	}
	disk, err := m.usable(m.status(devices, []array{a}), diskName)
	if err != nil {
		return "", err
	}
	if err := checkIdentity(devices, diskName, identity); err != nil {
		return "", err
	}
	// The new partition must be at least as large as the existing ones.
	sizeMiB := (m.memberSectors(a)*512 + mebibyte - 1) / mebibyte
	if sizeMiB == 0 || disk.SizeBytes < (sizeMiB+1)*mebibyte+mebibyte {
		return "", fmt.Errorf("%w: %s", ErrDiskTooSmall, diskName)
	}

	// Same guard as Create: refuse holders, mounts and foreign arrays that
	// also live on other disks; stop an old array that lives only here.
	devices, err = m.releaseOldArrays(ctx, devices, []string{diskName})
	if err != nil {
		return "", err
	}
	if err := checkIdentity(devices, diskName, identity); err != nil {
		return "", err
	}
	device, _ := findDevice(devices, diskName)
	return m.prepareDisk(ctx, device, name, sizeMiB)
}

// RemoveDisk takes a failed or spare disk out of a volume and wipes it.
// A disk that is still doing its job cannot be removed this way.
func (m *Manager) RemoveDisk(ctx context.Context, name, diskName string) error {
	if volume, ok := m.find(name); ok && volume.Filesystem == "btrfs" {
		return m.RemoveBtrfsDisk(ctx, name, diskName)
	}
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return ErrReadOnly
	}
	if m.GrowthPending() {
		return ErrBusy
	}

	volume, ok := m.find(name)
	if !ok {
		return ErrNotFound
	}
	a, running := m.arrayOf(ctx, volume)
	if !running {
		return fmt.Errorf("%w: the volume is not running", ErrNotPossible)
	}
	devices, err := m.blockDevices(ctx)
	if err != nil {
		return err
	}
	device, found := findDevice(devices, diskName)
	if !found {
		return fmt.Errorf("%w: %s not found", ErrDiskUnavailable, diskName)
	}
	for _, member := range a.members {
		part := "/dev/" + member.device
		if !slices.ContainsFunc(device.partitions(), func(p blockDevice) bool { return p.KName == part }) {
			continue
		}
		if state := memberState(member); state != MemberFaulty && state != MemberSpare {
			return fmt.Errorf("%w: %s is an active part of the volume", ErrNotPossible, diskName)
		}
		slog.Info("removing disk from volume", "name", name, "disk", diskName)
		if _, err := m.runner.Run(ctx, "", "mdadm", "--manage", "/dev/"+a.device, "--remove", part); err != nil {
			return err
		}
		m.runner.Run(ctx, "", "mdadm", "--zero-superblock", "--force", part)
		m.runner.Run(ctx, "", "wipefs", "--all", "--force", part)
		m.runner.Run(ctx, "", "wipefs", "--all", "--force", device.Name)
		m.runner.Run(ctx, "", "udevadm", "settle")
		return nil
	}
	return fmt.Errorf("%w: %s is not part of %s", ErrDiskUnavailable, diskName, name)
}

// Check starts or stops a consistency check: all copies and parity are read
// and compared. Not possible while the volume is degraded or rebuilding.
func (m *Manager) Check(ctx context.Context, name string, start bool) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return ErrReadOnly
	}
	if m.GrowthPending() {
		return ErrBusy
	}

	volume, ok := m.find(name)
	if !ok {
		return ErrNotFound
	}
	if volume.Filesystem == "btrfs" {
		return m.checkBtrfs(ctx, volume, start)
	}
	a, running := m.arrayOf(ctx, volume)
	if !running || volume.Level == "raid0" {
		return fmt.Errorf("%w: nothing to check", ErrNotPossible)
	}
	action := "idle"
	if start {
		if a.degraded > 0 {
			return fmt.Errorf("%w: a disk is missing", ErrNotPossible)
		}
		if a.syncAction != "idle" {
			return fmt.Errorf("%w: the volume is busy (%s)", ErrNotPossible, a.syncAction)
		}
		action = "check"
	} else if a.syncAction != "check" {
		return fmt.Errorf("%w: no check is running", ErrNotPossible)
	}
	return os.WriteFile(filepath.Join(m.sysBlock, a.device, "md", "sync_action"), []byte(action), 0o644)
}
