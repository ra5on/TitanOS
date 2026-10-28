package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// SSD cache in front of an mdadm volume, with the kernel's dm-cache: an SSD
// keeps copies of the blocks used most. Off unless switched on per volume.
//
//   - writethrough ("Lesecache"): every write goes to the array at once; the
//     SSD only speeds up reads. Losing the SSD loses nothing.
//   - writeback ("Lese- und Schreibcache"): writes land on the SSD first and
//     reach the array later. Faster, but blocks not yet written back exist
//     only on the SSD: without it the volume is not mounted at all.
//
// The SSD gets a GPT with two partitions, metadata and cache blocks, named
// by their PARTUUIDs in VolumeConfig.Cache. The cache device
// /dev/mapper/titanos-cache-<volume> is set up when the volume is mounted,
// so switching it on takes effect with the next start; the array and its
// file system stay as they are. The mode can change while mounted
// (table reload). Switching a writeback cache off first writes every dirty
// block back (policy "cleaner") and only then forgets the SSD.

// CacheConfig is the SSD cache of a volume in volumes.json.
type CacheConfig struct {
	Mode string `json:"mode"` // writethrough or writeback
	// Disk is the serial number of the SSD, for the page only.
	Disk     string `json:"disk"`
	MetaUUID string `json:"metaUUID"` // PARTUUID of the metadata partition
	DataUUID string `json:"dataUUID"` // PARTUUID of the cache partition
	// Removing is set while dirty blocks are written back before removal.
	Removing bool `json:"removing,omitempty"`
}

// CacheView is what the page shows about a cache.
type CacheView struct {
	Mode     string `json:"mode"`
	Disk     string `json:"disk"`
	Active   bool   `json:"active"`
	Missing  bool   `json:"missing"`
	Removing bool   `json:"removing"`
	// From dmsetup status while active.
	UsedBlocks  uint64  `json:"usedBlocks"`
	TotalBlocks uint64  `json:"totalBlocks"`
	DirtyBlocks uint64  `json:"dirtyBlocks"`
	ReadHitRate float64 `json:"readHitRate"` // 0..1, -1 when nothing was read yet
	BlockBytes  uint64  `json:"blockBytes"`
}

const (
	cacheBlockSectors = 512 // 256 KiB
	cachePolicy       = "smq"
)

var cacheModes = []string{"writethrough", "writeback"}

func cacheName(volume string) string { return "titanos-cache-" + volume }

func (m *Manager) partUUIDPath(uuid string) string {
	dir := m.partUUIDDir
	if dir == "" {
		dir = "/dev/disk/by-partuuid"
	}
	return filepath.Join(dir, strings.ToLower(uuid))
}

func (m *Manager) cachePresent(c *CacheConfig) bool {
	for _, uuid := range []string{c.MetaUUID, c.DataUUID} {
		if _, err := os.Stat(m.partUUIDPath(uuid)); err != nil {
			return false
		}
	}
	return true
}

func cacheTable(sectors uint64, meta, data, origin, mode, policy string) string {
	return fmt.Sprintf("0 %d cache %s %s %s %d 1 %s %s 0", sectors, meta, data, origin, cacheBlockSectors, mode, policy)
}

// mountSource is what mountWith mounts for an mdadm volume: the cache
// device when the volume has a cache, else its file system by UUID. An
// error keeps the volume unmounted (a writeback cache without its SSD). An
// encrypted volume mounts its opened LUKS device; the cache, if any, lies
// below that (crypt.go).
func (m *Manager) mountSource(ctx context.Context, volume VolumeConfig) (string, error) {
	if volume.Encryption != nil && volume.Filesystem != "btrfs" {
		name := cryptName(volume, "")
		if !m.mapperOpen(name) {
			return "", ErrVolumeLocked
		}
		return m.mapperPath(name), nil
	}
	return m.lowerDevice(ctx, volume, "UUID="+volume.FilesystemUUID)
}

// lowerDevice is what lies on top of the array of an mdadm volume: its
// cache device when it has a cache (set up here), else plain.
func (m *Manager) lowerDevice(ctx context.Context, volume VolumeConfig, plain string) (string, error) {
	if volume.Cache == nil || volume.Filesystem == "btrfs" {
		return plain, nil
	}
	name := cacheName(volume.Name)
	device := "/dev/mapper/" + name
	if _, err := m.runner.Run(ctx, "", "dmsetup", "status", name); err == nil {
		return device, nil // already set up (titand restarted)
	}
	if !m.cachePresent(volume.Cache) {
		if volume.Cache.Mode == "writeback" && !volume.Cache.Removing {
			if m.OnCacheMissing != nil {
				m.OnCacheMissing(volume.Name)
			}
			return "", fmt.Errorf("%w: the write cache SSD of %s is missing; its unwritten blocks are only there", ErrNotPossible, volume.Name)
		}
		slog.Warn("cache SSD missing, mounting without cache", "volume", volume.Name)
		if m.OnCacheMissing != nil {
			m.OnCacheMissing(volume.Name)
		}
		return plain, nil
	}
	a, ok := m.arrayOf(ctx, volume)
	if !ok || a.sectors == 0 {
		return "", fmt.Errorf("%w: array of %s not running", ErrNotPossible, volume.Name)
	}
	mode, policy := volume.Cache.Mode, cachePolicy
	if volume.Cache.Removing {
		mode, policy = "writeback", "cleaner"
	}
	table := cacheTable(a.sectors, m.partUUIDPath(volume.Cache.MetaUUID), m.partUUIDPath(volume.Cache.DataUUID), "/dev/"+a.device, mode, policy)
	if _, err := m.runner.Run(ctx, "", "dmsetup", "create", name, "--table", table); err != nil {
		if volume.Cache.Mode == "writeback" {
			return "", err
		}
		slog.Warn("cache not set up, mounting without cache", "volume", volume.Name, "err", err)
		return plain, nil
	}
	slog.Info("SSD cache set up", "volume", volume.Name, "mode", mode)
	return device, nil
}

// EnableCache prepares an SSD as cache of an mdadm volume. It takes effect
// the next time the volume is mounted (normally the next start).
func (m *Manager) EnableCache(ctx context.Context, name, diskName, mode, identity string, erase bool) error {
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
		return fmt.Errorf("%w: an SSD cache is available for mdadm volumes only", ErrNotPossible)
	}
	if volume.Cache != nil {
		return fmt.Errorf("%w: the volume has a cache already", ErrExists)
	}
	if !slices.Contains(cacheModes, mode) {
		return ErrInvalidLevel
	}
	if !erase {
		return ErrNotConfirmed
	}
	devices, err := m.blockDevices(ctx)
	if err != nil {
		return err
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
	device, _ := findDevice(devices, diskName)
	// Metadata: 16 bytes per cache block, doubled, at least 64 MiB.
	blocks := disk.SizeBytes / (cacheBlockSectors * 512)
	metaMiB := max(64, (blocks*32)>>20+16)
	if disk.SizeBytes < (metaMiB+256)*mebibyte {
		return ErrDiskTooSmall
	}
	slog.Info("preparing cache SSD", "volume", name, "disk", diskName, "mode", mode)
	if _, err := m.runner.Run(ctx, "", "wipefs", "--all", "--force", device.Name); err != nil {
		return err
	}
	script := fmt.Sprintf("label: gpt\nsize=%dMiB, name=\"titanos-cache-meta-%s\"\nname=\"titanos-cache-%s\"\n", metaMiB, name, name)
	if _, err := m.runner.Run(ctx, script, "sfdisk", "--quiet", "--wipe", "always", device.Name); err != nil {
		return err
	}
	m.runner.Run(ctx, "", "udevadm", "settle")
	var uuids []string
	for i := 1; i <= 2; i++ {
		part := partitionName(device.Name, i)
		out, err := m.runner.Run(ctx, "", "blkid", "-p", "-s", "PART_ENTRY_UUID", "-o", "value", part)
		uuid := strings.TrimSpace(out)
		if err != nil || uuid == "" {
			return fmt.Errorf("%w: partition %s has no PARTUUID", ErrNotPossible, part)
		}
		// dm-cache reads an all-zero metadata start as "new cache".
		if i == 1 {
			if err := m.zeroStart(part); err != nil {
				return err
			}
		}
		uuids = append(uuids, uuid)
	}
	cache := &CacheConfig{Mode: mode, Disk: disk.Serial, MetaUUID: uuids[0], DataUUID: uuids[1]}
	return m.updateVolume(name, func(v *VolumeConfig) { v.Cache = cache })
}

func partitionName(disk string, n int) string {
	if last := disk[len(disk)-1]; last >= '0' && last <= '9' {
		return fmt.Sprintf("%sp%d", disk, n)
	}
	return fmt.Sprintf("%s%d", disk, n)
}

// zeroStart clears the first MiB of a partition.
func (m *Manager) zeroStart(part string) error {
	if m.zero != nil {
		return m.zero(part)
	}
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_EXCL, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(make([]byte, mebibyte)); err != nil {
		return err
	}
	return f.Sync()
}

// SetCacheMode switches between read cache and read/write cache. An active
// cache changes at once (table reload); otherwise with the next mount.
func (m *Manager) SetCacheMode(ctx context.Context, name, mode string) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return ErrReadOnly
	}
	volume, ok := m.find(name)
	if !ok || volume.Cache == nil {
		return ErrNotFound
	}
	if !slices.Contains(cacheModes, mode) || volume.Cache.Removing {
		return ErrInvalidLevel
	}
	if err := m.reloadCache(ctx, volume, mode, cachePolicy); err != nil {
		return err
	}
	return m.updateVolume(name, func(v *VolumeConfig) { v.Cache.Mode = mode })
}

// reloadCache changes mode and policy of an active cache; it does nothing
// for a cache that is not set up.
func (m *Manager) reloadCache(ctx context.Context, volume VolumeConfig, mode, policy string) error {
	name := cacheName(volume.Name)
	if _, err := m.runner.Run(ctx, "", "dmsetup", "status", name); err != nil {
		return nil
	}
	a, ok := m.arrayOf(ctx, volume)
	if !ok {
		return fmt.Errorf("%w: array not running", ErrNotPossible)
	}
	table := cacheTable(a.sectors, m.partUUIDPath(volume.Cache.MetaUUID), m.partUUIDPath(volume.Cache.DataUUID), "/dev/"+a.device, mode, policy)
	if _, err := m.runner.Run(ctx, "", "dmsetup", "reload", name, "--table", table); err != nil {
		return err
	}
	// Suspend waits for I/O in flight and, with writeback, flushes it.
	if _, err := m.runner.Run(ctx, "", "dmsetup", "suspend", name); err != nil {
		return err
	}
	_, err := m.runner.Run(ctx, "", "dmsetup", "resume", name)
	return err
}

// DisableCache takes the SSD out. A read cache is forgotten at once (it
// stays in use until the next start, harmlessly). A write cache first
// writes every dirty block back; RunCacheCleaner forgets it when done.
func (m *Manager) DisableCache(ctx context.Context, name string) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return ErrReadOnly
	}
	volume, ok := m.find(name)
	if !ok || volume.Cache == nil {
		return ErrNotFound
	}
	if _, err := m.runner.Run(ctx, "", "dmsetup", "status", cacheName(name)); err != nil || volume.Cache.Mode == "writethrough" {
		// Not set up, or nothing on the SSD that the array lacks.
		return m.updateVolume(name, func(v *VolumeConfig) { v.Cache = nil })
	}
	if err := m.reloadCache(ctx, volume, "writeback", "cleaner"); err != nil {
		return err
	}
	return m.updateVolume(name, func(v *VolumeConfig) { v.Cache.Removing = true })
}

// RunCacheCleaner forgets caches that were being written back once they
// hold no dirty block, every 10 seconds until ctx is done.
func (m *Manager) RunCacheCleaner(ctx context.Context) {
	for {
		for _, volume := range m.configured() {
			if volume.Cache == nil || !volume.Cache.Removing {
				continue
			}
			out, err := m.runner.Run(ctx, "", "dmsetup", "status", cacheName(volume.Name))
			if err != nil {
				continue
			}
			if view := parseCacheStatus(out); view.DirtyBlocks == 0 {
				m.actions.Lock()
				err := m.updateVolume(volume.Name, func(v *VolumeConfig) { v.Cache = nil })
				m.actions.Unlock()
				if err == nil {
					slog.Info("write cache written back and removed", "volume", volume.Name)
					if m.OnCacheRemoved != nil {
						m.OnCacheRemoved(volume.Name)
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

// parseCacheStatus reads `dmsetup status` of a cache target:
// 0 <len> cache <mbs> <used>/<total meta> <cbs> <used>/<total> <rh> <rm> <wh> <wm> <demotions> <promotions> <dirty> …
func parseCacheStatus(output string) CacheView {
	view := CacheView{ReadHitRate: -1}
	fields := strings.Fields(output)
	if len(fields) < 14 || fields[2] != "cache" {
		return view
	}
	blockSectors, _ := strconv.ParseUint(fields[5], 10, 64)
	view.BlockBytes = blockSectors * 512
	if used, total, ok := strings.Cut(fields[6], "/"); ok {
		view.UsedBlocks, _ = strconv.ParseUint(used, 10, 64)
		view.TotalBlocks, _ = strconv.ParseUint(total, 10, 64)
	}
	hits, _ := strconv.ParseFloat(fields[7], 64)
	misses, _ := strconv.ParseFloat(fields[8], 64)
	if hits+misses > 0 {
		view.ReadHitRate = hits / (hits + misses)
	}
	view.DirtyBlocks, _ = strconv.ParseUint(fields[13], 10, 64)
	return view
}

// cacheView is the state of a volume's cache for the page.
func (m *Manager) cacheView(cfg VolumeConfig) *CacheView {
	if cfg.Cache == nil {
		return nil
	}
	view := CacheView{ReadHitRate: -1}
	if out, err := m.runner.Run(context.Background(), "", "dmsetup", "status", cacheName(cfg.Name)); err == nil {
		view = parseCacheStatus(out)
		view.Active = true
	}
	view.Mode, view.Disk, view.Removing = cfg.Cache.Mode, cfg.Cache.Disk, cfg.Cache.Removing
	view.Missing = !m.cachePresent(cfg.Cache)
	return &view
}

// updateVolume changes one configured volume and saves.
func (m *Manager) updateVolume(name string, change func(*VolumeConfig)) error {
	volumes := m.configured()
	i := slices.IndexFunc(volumes, func(v VolumeConfig) bool { return v.Name == name })
	if i < 0 {
		return ErrNotFound
	}
	change(&volumes[i])
	return m.saveVolumes(volumes)
}

// ErrCacheConfigured keeps grow and import away from cached volumes.
var ErrCacheConfigured = errors.New("storage: switch the SSD cache off first")
