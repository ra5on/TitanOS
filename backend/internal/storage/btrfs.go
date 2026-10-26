package storage

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Native Btrfs mirrors use RAID1 for both data and metadata. No md device is
// involved. The filesystem UUID, not a Linux device name, identifies the pool.
func (m *Manager) createBtrfs(ctx context.Context, req CreateRequest, devices []blockDevice, sizeMiB uint64) error {
	var parts []string
	for _, name := range req.Disks {
		device, _ := findDevice(devices, name)
		part, err := m.prepareDisk(ctx, device, "btrfs:"+req.Name, sizeMiB)
		if err != nil {
			return err
		}
		parts = append(parts, part)
	}
	args := append([]string{"-f", "-L", req.Name, "-d", "raid1", "-m", "raid1"}, parts...)
	if _, err := m.runner.Run(ctx, "", "mkfs.btrfs", args...); err != nil {
		return err
	}
	uuid, err := m.runner.Run(ctx, "", "blkid", "--match-tag", "UUID", "--output", "value", parts[0])
	if err != nil {
		return err
	}
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return fmt.Errorf("%w: new Btrfs filesystem has no UUID", ErrNotPossible)
	}
	volume := VolumeConfig{Name: req.Name, Level: "raid1", Filesystem: "btrfs", FilesystemUUID: uuid, Created: time.Now().UTC().Truncate(time.Second)}
	volume.InitializeFolders = len(m.configured()) == 0
	if err := m.saveVolumes(append(m.configured(), volume)); err != nil {
		return err
	}
	return m.mount(ctx, volume)
}

const (
	// btrfsMirrorDisks is the number of members of a TitanOS Btrfs mirror.
	btrfsMirrorDisks = 2
	// btrfsGrace is how long mountAll waits for every member of a mirror
	// to appear before it mounts with one missing.
	btrfsGrace = 60 * time.Second
)

// btrfsWaiting reports whether a mirror is still within its grace.
func (m *Manager) btrfsWaiting() bool {
	now := m.now()
	for _, first := range m.btrfsSeen {
		if now.Sub(first) < btrfsGrace {
			return true
		}
	}
	return false
}

// mountBtrfs mounts a mirror once all members are visible. With a member
// missing it waits btrfsGrace from the first sighting of the mirror, then
// mounts degraded. It never blocks: mountAll calls again next round.
func (m *Manager) mountBtrfs(ctx context.Context, volume VolumeConfig, devices []blockDevice) {
	parts := btrfsParts(devices, volume.FilesystemUUID)
	if len(parts) == 0 {
		delete(m.btrfsSeen, volume.Name)
		return
	}
	now := m.now()
	if m.btrfsSeen == nil {
		m.btrfsSeen = map[string]time.Time{}
	}
	first, seen := m.btrfsSeen[volume.Name]
	if !seen {
		first = now
		m.btrfsSeen[volume.Name] = now
	}
	waited := now.Sub(first) >= btrfsGrace
	if len(parts) >= btrfsMirrorDisks {
		err := m.mountWith(ctx, volume, false)
		if err == nil {
			delete(m.btrfsSeen, volume.Name)
			slog.Info("volume mounted", "name", volume.Name)
			return
		}
		if !waited {
			slog.Warn("mount Btrfs mirror", "name", volume.Name, "err", err)
			return
		}
	} else if !waited {
		return // wait for the other member
	}
	slog.Warn("mounting Btrfs mirror degraded", "name", volume.Name, "members", len(parts))
	if err := m.mountWith(ctx, volume, true); err != nil {
		slog.Warn("mount Btrfs mirror", "name", volume.Name, "err", err)
		return
	}
	delete(m.btrfsSeen, volume.Name)
}

// btrfsMissing counts the devices a mounted Btrfs filesystem misses, from
// `btrfs filesystem show --raw <mountpoint>`.
func btrfsMissing(output string) int {
	matches := btrfsDeviceLine.FindAllStringSubmatch(output, -1)
	missing, present := 0, 0
	for _, match := range matches {
		if strings.Contains(strings.ToLower(match[3]), "missing") {
			missing++
		} else {
			present++
		}
	}
	// Some btrfs-progs versions leave a disconnected device out entirely.
	if total := btrfsTotalDevices.FindStringSubmatch(output); total != nil {
		if n, _ := strconv.Atoi(total[1]); n > present+missing {
			missing += n - present - missing
		}
	}
	if missing == 0 && strings.Contains(output, "Some devices missing") {
		missing = 1
	}
	return missing
}

var btrfsTotalDevices = regexp.MustCompile(`Total devices\s+(\d+)`)

func btrfsParts(devices []blockDevice, uuid string) []blockDevice {
	var parts []blockDevice
	for _, device := range devices {
		if !isDisk(device) {
			continue
		}
		device.walk(func(part blockDevice) {
			if part.FSType == "btrfs" && part.UUID == uuid && uuid != "" {
				parts = append(parts, part)
			}
		})
	}
	return parts
}

func (m *Manager) btrfsStatus(volume *Volume, status *Status, devices []blockDevice, cfg VolumeConfig) {
	volume.RaidDisks = 2
	for _, disk := range status.Disks {
		if disk.Volume == cfg.Name {
			volume.Members = append(volume.Members, Member{Disk: disk.Name, State: MemberActive})
		}
	}
	// Kernel scrub state is in RAM. Reading it does not wake idle disks.
	m.mu.RLock()
	state := m.btrfsProgress[cfg.Name]
	m.mu.RUnlock()
	switch {
	case len(volume.Members) == 0:
		volume.State = VolumeMissing
	case len(volume.Members) < btrfsMirrorDisks || state.Missing > 0:
		// The mounted filesystem is authoritative: a mirror mounted degraded
		// stays degraded even after the other disk is visible again.
		volume.State = VolumeDegraded
	default:
		volume.State = VolumeOK
	}
	if state.State != "" {
		volume.State, volume.Sync = state.State, state.Sync
	}
	volume.Mismatches = state.Errors
	parts := btrfsParts(devices, cfg.FilesystemUUID)
	for _, part := range parts {
		if volume.SizeBytes == 0 || part.Size < volume.SizeBytes {
			volume.SizeBytes = part.Size
		}
	}
}

func (m *Manager) deleteBtrfs(ctx context.Context, volume VolumeConfig) error {
	devices, err := m.blockDevices(ctx)
	if err != nil {
		return err
	}
	parts := btrfsParts(devices, volume.FilesystemUUID)
	// Do not forget a missing mirror: a returning disk would retain an orphaned
	// filesystem. The administrator must reconnect/replace it before deletion.
	if len(parts) != 2 {
		return fmt.Errorf("%w: reconnect both Btrfs members before deleting the mirror", ErrNotPossible)
	}
	point := m.MountPoint(volume.Name)
	if m.readMounts()[point] {
		if _, err := m.runner.Run(ctx, "", "umount", point); err != nil {
			return fmt.Errorf("%w: %v", ErrBusy, err)
		}
	}
	for _, part := range parts {
		if _, err := m.runner.Run(ctx, "", "wipefs", "--all", "--force", part.Name); err != nil {
			return err
		}
	}
	if err := m.saveVolumes(slices.DeleteFunc(m.configured(), func(v VolumeConfig) bool { return v.Name == volume.Name })); err != nil {
		return err
	}
	os.Remove(point)
	return nil
}

var btrfsDeviceLine = regexp.MustCompile(`(?m)^\s*devid\s+(\d+)\s+size\s+(\d+)\s+used\s+\d+\s+path\s+(.+)$`)

// AddDisk replaces the one missing member, rather than changing the RAID profile
// or reducing the number of copies. A healthy mirror does not accept a spare.
func (m *Manager) replaceMissingBtrfs(ctx context.Context, volume VolumeConfig, diskName, identity string) error {
	point := m.MountPoint(volume.Name)
	if !m.readMounts()[point] {
		return fmt.Errorf("%w: mirror is not mounted", ErrNotPossible)
	}
	devices, err := m.blockDevices(ctx)
	if err != nil {
		return err
	}
	if len(btrfsParts(devices, volume.FilesystemUUID)) != 1 {
		return fmt.Errorf("%w: exactly one Btrfs member must be missing", ErrNotPossible)
	}
	disk, err := m.usable(m.status(devices, m.arrays(ctx)), diskName)
	if err != nil {
		return err
	}
	if err := checkIdentity(devices, diskName, identity); err != nil {
		return err
	}
	output, err := m.runner.Run(ctx, "", "btrfs", "filesystem", "show", "--raw", point)
	if err != nil {
		return err
	}
	missing, size := "", uint64(0)
	for _, match := range btrfsDeviceLine.FindAllStringSubmatch(output, -1) {
		if strings.EqualFold(strings.TrimSpace(match[3]), "missing") {
			if missing != "" {
				return ErrNotPossible
			}
			missing = match[1]
			size, _ = strconv.ParseUint(match[2], 10, 64)
		}
	}
	if missing == "" {
		// A disconnected member is omitted by some btrfs-progs versions.
		// New TitanOS mirrors have IDs 1 and 2; replace preserves the ID.
		matches := btrfsDeviceLine.FindAllStringSubmatch(output, -1)
		if len(matches) == 1 && (matches[0][1] == "1" || matches[0][1] == "2") {
			missing = "1"
			if matches[0][1] == "1" {
				missing = "2"
			}
			size = btrfsParts(devices, volume.FilesystemUUID)[0].Size
		}
	}
	// btrfs-progs reports disconnected devices with size 0 and path MISSING.
	// TitanOS creates equal-sized partitions; the surviving member is the
	// authoritative minimum size even after Linux renumbers the disks.
	if missing != "" && size == 0 {
		size = btrfsParts(devices, volume.FilesystemUUID)[0].Size
	}
	if missing == "" || size == 0 {
		return fmt.Errorf("%w: missing Btrfs device could not be identified", ErrNotPossible)
	}
	sizeMiB := (size + mebibyte - 1) / mebibyte
	if memberSizeMiB(disk.SizeBytes) < sizeMiB {
		return ErrDiskTooSmall
	}
	devices, err = m.releaseOldArrays(ctx, devices, []string{diskName})
	if err != nil {
		return err
	}
	if err := checkIdentity(devices, diskName, identity); err != nil {
		return err
	}
	device, _ := findDevice(devices, diskName)
	part, err := m.prepareDisk(ctx, device, "btrfs:"+volume.Name, sizeMiB)
	if err != nil {
		return err
	}
	_, err = m.runner.Run(ctx, "", "btrfs", "replace", "start", missing, part, point)
	if err == nil {
		m.setBtrfsProgress(volume.Name, VolumeRebuilding)
	}
	return err
}

func (m *Manager) checkBtrfs(ctx context.Context, volume VolumeConfig, start bool) error {
	if !m.readMounts()[m.MountPoint(volume.Name)] {
		return ErrNotPossible
	}
	action := "cancel"
	if start {
		action = "start"
	}
	_, err := m.runner.Run(ctx, "", "btrfs", "scrub", action, m.MountPoint(volume.Name))
	if err == nil && start {
		m.setBtrfsProgress(volume.Name, VolumeChecking)
	}
	return err
}

type Snapshot struct {
	Name string `json:"name"`
}

var snapshotName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// Snapshot directories are root-owned and private; none may be a symlink.
func privateDir(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, owned := info.Sys().(*syscall.Stat_t)
	if !owned || stat.Uid != uint32(os.Geteuid()) || !info.IsDir() || info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%w: unsafe snapshot directory", ErrNotPossible)
	}
	return nil
}

func (m *Manager) snapshotDir(name string) (string, error) {
	volume, ok := m.find(name)
	if !ok {
		return "", ErrNotFound
	}
	if volume.Filesystem != "btrfs" || !slices.Contains(m.Mounted(), name) {
		return "", ErrNotPossible
	}
	base := filepath.Join(m.MountPoint(name), ".titanos")
	if err := privateDir(base); err != nil {
		return "", err
	}
	dir := filepath.Join(base, "snapshots")
	return dir, privateDir(dir)
}

func (m *Manager) Snapshots(name string) ([]Snapshot, error) {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return nil, ErrReadOnly
	}
	dir, err := m.snapshotDir(name)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	result := []Snapshot{}
	for _, entry := range entries {
		if entry.IsDir() && snapshotName.MatchString(entry.Name()) {
			result = append(result, Snapshot{Name: entry.Name()})
		}
	}
	return result, nil
}

// SnapshotAction never rolls an active volume back in place. Restore creates
// a new writable snapshot so the administrator can copy out individual files.
func (m *Manager) SnapshotAction(ctx context.Context, volume, name, action, confirm string) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	return m.snapshotAction(ctx, volume, name, action, confirm)
}

// Caller holds actions so snapshot identity checks and removal are one operation.
func (m *Manager) snapshotAction(ctx context.Context, volume, name, action, confirm string) error {
	if m.ReadOnly {
		return ErrReadOnly
	}
	if m.GrowthPending() {
		return ErrBusy
	}
	if !snapshotName.MatchString(name) {
		return ErrInvalidName
	}
	dir, err := m.snapshotDir(volume)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, name)
	info, statErr := os.Lstat(path)
	if action == "create" {
		if !os.IsNotExist(statErr) {
			return ErrExists
		}
		_, err = m.runner.Run(ctx, "", "btrfs", "subvolume", "snapshot", "-r", m.MountPoint(volume), path)
		return err
	}
	if statErr != nil || !info.IsDir() {
		return ErrNotFound
	}
	switch action {
	case "delete":
		if confirm != name {
			return ErrNotConfirmed
		}
		_, err = m.runner.Run(ctx, "", "btrfs", "subvolume", "delete", "--commit-after", path)
	case "restore":
		restored := filepath.Join(m.MountPoint(volume), "restored")
		if err := privateDir(restored); err != nil {
			return err
		}
		target := filepath.Join(restored, name)
		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			return ErrExists
		}
		_, err = m.runner.Run(ctx, "", "btrfs", "subvolume", "snapshot", path, target)
	default:
		return ErrNotPossible
	}
	return err
}

func (m *Manager) setBtrfsProgress(name, state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.btrfsProgress == nil {
		m.btrfsProgress = map[string]btrfsProgress{}
	}
	m.btrfsProgress[name] = btrfsProgress{State: state, Sync: &Sync{RemainingSeconds: -1}}
}
