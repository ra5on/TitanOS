package storage

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// btrfsLayout is a profile TitanOS offers for Btrfs volumes: how many disks
// it needs at least, the mkfs/balance profiles for data and metadata, and
// how many copies of each block there are (how many disks may fail is one
// less).
type btrfsLayout struct {
	minDisks   int
	data, meta string
	copies     int
}

var btrfsLayouts = map[string]btrfsLayout{
	// One disk or several added up, without redundancy. Metadata is kept
	// twice on each disk, so a bad sector does not take the tree with it.
	"single":  {minDisks: 1, data: "single", meta: "dup", copies: 1},
	"raid1":   {minDisks: 2, data: "raid1", meta: "raid1", copies: 2},
	"raid1c3": {minDisks: 3, data: "raid1c3", meta: "raid1c3", copies: 3},
}

// btrfsProfile is the profile of a configured Btrfs volume.
func btrfsProfile(v VolumeConfig) string {
	if _, ok := btrfsLayouts[v.Profile]; ok {
		return v.Profile
	}
	return "raid1"
}

// btrfsDevices is the number of disks a configured Btrfs volume should have.
func btrfsDiskCount(v VolumeConfig) int {
	if v.Devices > 0 {
		return v.Devices
	}
	if btrfsProfile(v) == "single" {
		return 1
	}
	return 2
}

// btrfsUsable estimates the space a profile gives on partitions of these
// sizes: Btrfs places each copy on a different disk.
func btrfsUsable(profile string, sizes []uint64) uint64 {
	if len(sizes) == 0 {
		return 0
	}
	sorted := slices.Sorted(slices.Values(sizes))
	var sum uint64
	for _, size := range sorted {
		sum += size
	}
	copies := uint64(btrfsLayouts[profile].copies)
	if copies <= 1 {
		return sum
	}
	// Every copy of a block lies on a different disk: the largest disks
	// can only be filled as far as the others hold the other copies.
	usable := sum / copies
	rest := sum
	for i := len(sorted) - 1; i >= 0 && uint64(len(sorted)-1-i) < copies-1; i-- {
		rest -= sorted[i]
	}
	return min(usable, rest)
}

var (
	btrfsBalanceRunning = regexp.MustCompile(`(?i)balance on '.*' is (running|paused)`)
	btrfsBalanceLeft    = regexp.MustCompile(`(\d+)% left`)
)

// parseBtrfsBalance reads `btrfs balance status`: whether a balance is
// running or paused, and how far it got.
func parseBtrfsBalance(output string) (running bool, percent float64) {
	match := btrfsBalanceRunning.FindStringSubmatch(output)
	if match == nil {
		return false, 0
	}
	if left := btrfsBalanceLeft.FindStringSubmatch(output); left != nil {
		n, _ := strconv.ParseFloat(left[1], 64)
		percent = max(0, 100-n)
	}
	return true, percent
}

// Reshaping reports whether a Btrfs volume is balanced or loses a disk. Like
// a growing RAID it keeps app updates and other storage changes waiting.
func (m *Manager) Reshaping() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.btrfsRemoving) > 0 {
		return true
	}
	for _, state := range m.btrfsProgress {
		if state.State == VolumeReshaping {
			return true
		}
	}
	return false
}

// balanceArgs converts the whole volume to a profile. "soft" skips chunks
// that already have it, so a resumed or repeated conversion is cheap.
func balanceArgs(profile, point string) []string {
	layout := btrfsLayouts[profile]
	return []string{"balance", "start", "--bg", "-dconvert=" + layout.data + ",soft", "-mconvert=" + layout.meta + ",soft", point}
}

// AddBtrfsDisk adds a disk to a healthy Btrfs volume and converts it to the
// given profile (the current one when empty). The balance runs in the
// kernel and survives a restart: Btrfs resumes it when the volume is
// mounted again. The configuration names the new layout as soon as the
// disk is part of the file system.
func (m *Manager) AddBtrfsDisk(ctx context.Context, name, diskName, profile, identity string, erase bool) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return ErrReadOnly
	}
	if m.GrowthPending() {
		return ErrBusy
	}
	volume, ok := m.find(name)
	if !ok || volume.Filesystem != "btrfs" {
		return ErrNotFound
	}
	if !erase {
		return ErrNotConfirmed
	}
	return m.addBtrfsDisk(ctx, volume, diskName, profile, identity)
}

// addBtrfsDisk is AddBtrfsDisk with the storage actions held.
func (m *Manager) addBtrfsDisk(ctx context.Context, volume VolumeConfig, diskName, profile, identity string) error {
	name := volume.Name
	if m.Reshaping() {
		return ErrBusy
	}
	if profile == "" {
		profile = btrfsProfile(volume)
	}
	layout, ok := btrfsLayouts[profile]
	if !ok {
		return ErrInvalidLevel
	}
	devices, err := m.blockDevices(ctx)
	if err != nil {
		return err
	}
	point := m.MountPoint(volume.Name)
	parts := btrfsParts(devices, volume.FilesystemUUID)
	if !m.readMounts()[point] || len(parts) < btrfsDiskCount(volume) || m.btrfsState(volume.Name).Missing > 0 {
		return fmt.Errorf("%w: the volume must be complete and mounted; a missing disk is replaced instead", ErrNotPossible)
	}
	if len(parts)+1 < layout.minDisks {
		return ErrTooFewDisks
	}
	disk, err := m.usable(m.status(devices, m.arrays(ctx)), diskName)
	if err != nil {
		return err
	}
	if err := checkIdentity(devices, diskName, identity); err != nil {
		return err
	}
	devices, err = m.releaseOldArrays(ctx, devices, []string{diskName})
	if err != nil {
		return err
	}
	if err := checkIdentity(devices, diskName, identity); err != nil {
		return err
	}
	slog.Info("adding disk to Btrfs volume", "name", name, "disk", diskName, "profile", profile)
	device, _ := findDevice(devices, diskName)
	part, err := m.prepareDisk(ctx, device, "btrfs:"+volume.Name, memberSizeMiB(disk.SizeBytes))
	if err != nil {
		return err
	}
	if _, err := m.runner.Run(ctx, "", "btrfs", "device", "add", "-f", part, point); err != nil {
		return err
	}
	// From here on the disk belongs to the file system: say so before the
	// long part, so that a restart knows how many disks to wait for.
	if err := m.saveLayout(volume.Name, profile, len(parts)+1); err != nil {
		return err
	}
	return m.startBalance(ctx, volume.Name, profile)
}

// ConvertBtrfs changes the profile of a Btrfs volume without new disks, for
// example from raid1c3 to raid1 or from raid1 to single.
func (m *Manager) ConvertBtrfs(ctx context.Context, name, profile string) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return ErrReadOnly
	}
	if m.GrowthPending() || m.Reshaping() {
		return ErrBusy
	}
	volume, ok := m.find(name)
	if !ok || volume.Filesystem != "btrfs" {
		return ErrNotFound
	}
	layout, ok := btrfsLayouts[profile]
	if !ok {
		return ErrInvalidLevel
	}
	if profile == btrfsProfile(volume) {
		return nil
	}
	if !m.readMounts()[m.MountPoint(volume.Name)] || m.btrfsState(volume.Name).Missing > 0 {
		return fmt.Errorf("%w: the volume must be complete and mounted", ErrNotPossible)
	}
	if btrfsDiskCount(volume) < layout.minDisks {
		return ErrTooFewDisks
	}
	slog.Info("converting Btrfs volume", "name", name, "from", btrfsProfile(volume), "to", profile)
	if err := m.saveLayout(volume.Name, profile, btrfsDiskCount(volume)); err != nil {
		return err
	}
	return m.startBalance(ctx, volume.Name, profile)
}

// RemoveBtrfsDisk takes a disk out of a healthy Btrfs volume. Btrfs first
// moves its data to the other disks, which can take hours; this returns
// as soon as that has started. The remaining disks must hold the data and
// the profile.
func (m *Manager) RemoveBtrfsDisk(ctx context.Context, name, diskName string) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return ErrReadOnly
	}
	if m.GrowthPending() || m.Reshaping() {
		return ErrBusy
	}
	volume, ok := m.find(name)
	if !ok || volume.Filesystem != "btrfs" {
		return ErrNotFound
	}
	point := m.MountPoint(volume.Name)
	if !m.readMounts()[point] || m.btrfsState(volume.Name).Missing > 0 {
		return fmt.Errorf("%w: the volume must be complete and mounted", ErrNotPossible)
	}
	devices, err := m.blockDevices(ctx)
	if err != nil {
		return err
	}
	parts := btrfsParts(devices, volume.FilesystemUUID)
	if len(parts)-1 < btrfsLayouts[btrfsProfile(volume)].minDisks {
		return fmt.Errorf("%w: change the profile first, %s needs more disks", ErrTooFewDisks, btrfsProfile(volume))
	}
	device, found := findDevice(devices, diskName)
	if !found {
		return fmt.Errorf("%w: %s not found", ErrDiskUnavailable, diskName)
	}
	var part blockDevice
	var others []uint64
	for _, p := range parts {
		if slices.ContainsFunc(device.partitions(), func(d blockDevice) bool { return d.KName == p.KName }) {
			part = p
		} else {
			others = append(others, p.Size)
		}
	}
	if part.KName == "" {
		return fmt.Errorf("%w: %s is not a disk of %s", ErrDiskUnavailable, diskName, name)
	}
	// What is stored must fit on what remains.
	usage, err := m.runner.Run(ctx, "", "btrfs", "filesystem", "usage", "-b", point)
	if err != nil {
		return err
	}
	if used := btrfsDataUsed(usage); used == 0 || used > btrfsUsable(btrfsProfile(volume), others)*95/100 {
		return fmt.Errorf("%w: the other disks cannot hold the data", ErrDiskTooSmall)
	}
	slog.Info("removing disk from Btrfs volume", "name", name, "disk", diskName)
	m.mu.Lock()
	if m.btrfsRemoving == nil {
		m.btrfsRemoving = map[string]string{}
	}
	m.btrfsRemoving[volume.Name] = diskName
	m.mu.Unlock()
	go func() {
		ctx := context.WithoutCancel(ctx)
		_, err := m.runner.Run(ctx, "", "btrfs", "device", "remove", part.KName, point)
		m.actions.Lock()
		defer m.actions.Unlock()
		if err != nil {
			slog.Error("removing disk from Btrfs volume failed", "name", name, "disk", diskName, "err", err)
		} else {
			if err := m.saveLayout(volume.Name, btrfsProfile(volume), len(parts)-1); err != nil {
				slog.Error("Btrfs layout not saved after removing a disk", "name", name, "err", err)
			}
			m.runner.Run(ctx, "", "wipefs", "--all", "--force", part.KName)
			m.runner.Run(ctx, "", "wipefs", "--all", "--force", device.Name)
			m.runner.Run(ctx, "", "udevadm", "settle")
			slog.Info("disk removed from Btrfs volume", "name", name, "disk", diskName)
		}
		m.mu.Lock()
		delete(m.btrfsRemoving, volume.Name)
		m.mu.Unlock()
		if m.OnReshaped != nil {
			m.OnReshaped(volume.Name, err)
		}
	}()
	return nil
}

var btrfsUsedLine = regexp.MustCompile(`(?m)^\s*Used:\s+(\d+)\s*$`)

// btrfsDataUsed reads the bytes in use from `btrfs filesystem usage -b`,
// counted once (not per copy): the first "Used:" of the overall section is
// raw, so the data line is taken instead.
func btrfsDataUsed(output string) uint64 {
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Data,") {
			continue
		}
		if _, after, ok := strings.Cut(line, "Used:"); ok {
			fields := strings.FieldsFunc(after, func(r rune) bool { return r == ' ' || r == '(' || r == ',' })
			if len(fields) > 0 {
				n, _ := strconv.ParseUint(fields[0], 10, 64)
				return n
			}
		}
	}
	if match := btrfsUsedLine.FindStringSubmatch(output); match != nil {
		n, _ := strconv.ParseUint(match[1], 10, 64)
		return n
	}
	return 0
}

func (m *Manager) startBalance(ctx context.Context, name, profile string) error {
	if _, err := m.runner.Run(ctx, "", "btrfs", balanceArgs(profile, m.MountPoint(name))...); err != nil {
		return err
	}
	m.setBtrfsProgress(name, VolumeReshaping)
	return nil
}

// saveLayout writes profile and number of disks of a Btrfs volume.
func (m *Manager) saveLayout(name, profile string, devices int) error {
	volumes := m.configured()
	for i := range volumes {
		if volumes[i].Name == name {
			volumes[i].Profile, volumes[i].Devices, volumes[i].Level = profile, devices, profile
		}
	}
	return m.saveVolumes(volumes)
}

func (m *Manager) btrfsState(name string) btrfsProgress {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.btrfsProgress[name]
}

// FilesystemOf names the file system of a configured volume, "" if unknown.
func (m *Manager) FilesystemOf(name string) string {
	volume, _ := m.find(name)
	return volume.Filesystem
}
