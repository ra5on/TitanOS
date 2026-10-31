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
	// Stale is set when the volume was used without its cache (a read
	// cache whose SSD was missing): the array changed behind the cache's
	// back, so its blocks may be old. The next setup starts the cache
	// empty (metadata zeroed) and clears the flag.
	Stale bool `json:"stale,omitempty"`
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
	if volume.Filesystem == "btrfs" {
		return plain, nil
	}
	name := cacheName(volume.Name)
	device := "/dev/mapper/" + name
	if volume.Cache == nil {
		// A cache switched off in this boot stays as a plain linear
		// mapping of the array until the next start, and holds the array:
		// go through it.
		if table, err := m.runner.Run(ctx, "", "dmsetup", "table", name); err == nil && target(table) == "linear" {
			return device, nil
		}
		return plain, nil
	}
	if _, err := m.runner.Run(ctx, "", "dmsetup", "status", name); err == nil {
		return device, nil // already set up (titand restarted)
	}
	// A write cache may hold blocks the array lacks; so may one that was
	// being written back when the system went down (Removing). Without
	// the SSD such a volume stays unmounted.
	mayBeDirty := volume.Cache.Mode == "writeback" || volume.Cache.Removing
	if !m.cachePresent(volume.Cache) {
		if m.OnCacheMissing != nil {
			m.OnCacheMissing(volume.Name)
		}
		if mayBeDirty {
			return "", fmt.Errorf("%w: the write cache SSD of %s is missing; its unwritten blocks are only there", ErrNotPossible, volume.Name)
		}
		slog.Warn("cache SSD missing, mounting without cache", "volume", volume.Name)
		if err := m.markCacheStale(volume); err != nil {
			return "", err
		}
		return plain, nil
	}
	a, ok := m.arrayOf(ctx, volume)
	if !ok || a.sectors == 0 {
		return "", fmt.Errorf("%w: array of %s not running", ErrNotPossible, volume.Name)
	}
	if volume.Cache.Stale {
		// The array was written without the cache: whatever the SSD holds
		// may be older. Start with an empty cache. (Only a read cache is
		// ever used without its SSD; the mode may have changed since.)
		slog.Warn("cache SSD was missing before, starting the cache empty", "volume", volume.Name)
		if err := m.zeroStart(m.partUUIDPath(volume.Cache.MetaUUID)); err != nil {
			slog.Warn("cache metadata not cleared, mounting without cache", "volume", volume.Name, "err", err)
			return plain, nil // still Stale
		}
	}
	mode, policy := volume.Cache.Mode, cachePolicy
	if volume.Cache.Removing {
		mode, policy = "writeback", "cleaner"
	}
	table := cacheTable(a.sectors, m.partUUIDPath(volume.Cache.MetaUUID), m.partUUIDPath(volume.Cache.DataUUID), "/dev/"+a.device, mode, policy)
	if _, err := m.runner.Run(ctx, "", "dmsetup", "create", name, "--table", table); err != nil {
		if mayBeDirty {
			return "", err
		}
		slog.Warn("cache not set up, mounting without cache", "volume", volume.Name, "err", err)
		if err := m.markCacheStale(volume); err != nil {
			return "", err
		}
		return plain, nil
	}
	if volume.Cache.Stale {
		if err := m.updateVolume(volume.Name, func(v *VolumeConfig) {
			if v.Cache != nil {
				v.Cache.Stale = false
			}
		}); err != nil {
			// Harmless: the next setup clears the (fresh) cache once more.
			slog.Warn("cache marked stale still", "volume", volume.Name, "err", err)
		}
	}
	slog.Info("SSD cache set up", "volume", volume.Name, "mode", mode)
	return device, nil
}

// markCacheStale records that a read cache's volume is about to be used
// without the cache. If that cannot be saved the volume is not used: the
// next start with the SSD would otherwise serve old blocks.
func (m *Manager) markCacheStale(volume VolumeConfig) error {
	if volume.Cache.Stale {
		return nil
	}
	err := m.updateVolume(volume.Name, func(v *VolumeConfig) {
		if v.Cache != nil {
			v.Cache.Stale = true
		}
	})
	if err != nil {
		return fmt.Errorf("%w: cannot record that %s runs without its cache: %v", ErrNotPossible, volume.Name, err)
	}
	return nil
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
	disk, err := m.usable(m.status(ctx, devices, m.arrays(ctx)), diskName)
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
		if err != nil || !validUUID(uuid) {
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
	if volume.Cache.Mode == "writeback" && mode == "writethrough" && m.cacheActive(ctx, volume) {
		// A read cache is mounted without its SSD, so the switch is only
		// saved once no block lives on the SSD alone.
		if err := m.flushCache(ctx, volume, cacheFlushWait); err != nil {
			m.reloadCache(context.WithoutCancel(ctx), volume, "writeback", cachePolicy)
			if errors.Is(err, errCacheDirty) {
				return fmt.Errorf("%w: the write cache did not finish writing back; try again later", ErrBusy)
			}
			return err
		}
	}
	if err := m.reloadCache(ctx, volume, mode, cachePolicy); err != nil {
		return err
	}
	return m.updateVolume(name, func(v *VolumeConfig) { v.Cache.Mode = mode })
}

// cacheActive tells whether the cache device of a volume is set up.
func (m *Manager) cacheActive(ctx context.Context, volume VolumeConfig) bool {
	_, err := m.runner.Run(ctx, "", "dmsetup", "status", cacheName(volume.Name))
	return err == nil
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
	return m.swapTable(ctx, name, table)
}

// swapTable loads a new table into a device-mapper device and makes it live.
func (m *Manager) swapTable(ctx context.Context, name, table string) error {
	if _, err := m.runner.Run(ctx, "", "dmsetup", "reload", name, "--table", table); err != nil {
		return err
	}
	// Suspend waits for I/O in flight and, with writeback, flushes it.
	if _, err := m.runner.Run(ctx, "", "dmsetup", "suspend", name); err != nil {
		// Drop the loaded table, or a later resume would still swap it in.
		m.runner.Run(context.WithoutCancel(ctx), "", "dmsetup", "clear", name)
		return err
	}
	_, err := m.runner.Run(context.WithoutCancel(ctx), "", "dmsetup", "resume", name)
	return err
}

// cacheFlushWait is how long flushCache waits for dirty blocks to reach
// the array (replaced in tests).
var cacheFlushWait = 5 * time.Minute

// cacheFlushPoll is how often flushCache looks again (replaced in tests).
var cacheFlushPoll = 2 * time.Second

// flushCache writes every dirty block of an active cache back and leaves
// it in writethrough mode (policy cleaner), where no write makes a block
// dirty again. errCacheDirty when the blocks did not get written back
// within wait; the cache then goes on writing back. Called with actions
// held.
func (m *Manager) flushCache(ctx context.Context, volume VolumeConfig, wait time.Duration) error {
	name := cacheName(volume.Name)
	// A write cache writes back in its own mode first (that switch is
	// known to work with dirty blocks); a read cache never leaves
	// writethrough, so no write can become dirty on the way.
	mode := "writeback"
	if volume.Cache.Mode == "writethrough" && !volume.Cache.Removing {
		mode = "writethrough"
	}
	if err := m.reloadCache(ctx, volume, mode, "cleaner"); err != nil {
		return err
	}
	deadline := time.Now().Add(wait)
	for {
		out, err := m.runner.Run(ctx, "", "dmsetup", "status", name)
		if err != nil {
			return err
		}
		if view, ok := parseCacheStatusOK(out); ok && view.DirtyBlocks == 0 {
			break
		}
		if !time.Now().Before(deadline) {
			return errCacheDirty
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(min(cacheFlushPoll, max(time.Until(deadline), 0))):
		}
	}
	if mode != "writethrough" {
		if err := m.reloadCache(ctx, volume, "writethrough", "cleaner"); err != nil {
			return err
		}
	}
	out, err := m.runner.Run(ctx, "", "dmsetup", "status", name)
	if err != nil {
		return err
	}
	if view, ok := parseCacheStatusOK(out); !ok || view.DirtyBlocks > 0 {
		return errCacheDirty
	}
	return nil
}

// clearCacheSSD makes the SSD of a forgotten or deleted cache forget it:
// the metadata start is zeroed (the cache would start empty), so is the
// start of the cache partition, and the cache partition is discarded where
// the SSD supports it. Best effort, only once no device uses the SSD; it
// is a free disk again afterwards and is wiped before any further use.
//
// The start of the cache partition matters: its first cache block may hold
// a copy of the array's first blocks, file system signature and UUID
// included. udev and blkid then see the volume's file system a second time
// on the SSD, so the free SSD was not offered under "Format drive" (seen
// in QEMU with scripts/smoke-cache.py), and two devices carried one UUID.
func (m *Manager) clearCacheSSD(ctx context.Context, c *CacheConfig) {
	if !m.cachePresent(c) {
		return
	}
	for _, uuid := range []string{c.MetaUUID, c.DataUUID} {
		if err := m.zeroStart(m.partUUIDPath(uuid)); err != nil {
			slog.Warn("clear cache partition", "partition", uuid, "err", err)
		}
	}
	if _, err := m.runner.Run(ctx, "", "blkdiscard", "--force", m.partUUIDPath(c.DataUUID)); err != nil {
		slog.Info("cache partition not discarded", "err", err)
	}
}

// forgetCache removes the cache from the configuration of a volume whose
// cache device no longer uses the SSD (detached, linear or never set up)
// and clears the SSD.
func (m *Manager) forgetCache(ctx context.Context, name string, c *CacheConfig) error {
	if err := m.updateVolume(name, func(v *VolumeConfig) { v.Cache = nil }); err != nil {
		return err
	}
	m.clearCacheSSD(ctx, c)
	return nil
}

// errCacheDirty says that a cache still holds blocks the array lacks.
var errCacheDirty = errors.New("storage: the cache still holds unwritten blocks")

// detachCache takes an active cache out from under a running volume
// without unmounting it: the cache device becomes a plain linear mapping of
// the array, so nothing lands on the SSD any more and nothing is lost when
// the device goes away at shutdown. It refuses while blocks are dirty.
// Called with actions held.
func (m *Manager) detachCache(ctx context.Context, volume VolumeConfig) error {
	name := cacheName(volume.Name)
	table, err := m.runner.Run(ctx, "", "dmsetup", "table", name)
	if err != nil {
		return err
	}
	if target(table) == "linear" {
		return nil // detached before (titand stopped before saving)
	}
	a, ok := m.arrayOf(ctx, volume)
	if !ok || a.sectors == 0 {
		return fmt.Errorf("%w: array of %s not running", ErrNotPossible, volume.Name)
	}
	// First writethrough: from now on no write can make a block dirty, so
	// "no dirty blocks" stays true until the switch below.
	if err := m.flushCache(ctx, volume, 0); err != nil {
		return err
	}
	if err := m.swapTable(ctx, name, fmt.Sprintf("0 %d linear /dev/%s 0", a.sectors, a.device)); err != nil {
		return err
	}
	table, err = m.runner.Run(ctx, "", "dmsetup", "table", name)
	if err != nil {
		return err
	}
	if target(table) != "linear" {
		return fmt.Errorf("%w: %s did not switch away from the cache", ErrNotPossible, name)
	}
	return nil
}

// removeMapperTries and removeMapperPause bound how long removeMapper
// waits for a busy device (replaced in tests).
var removeMapperTries, removeMapperPause = 10, time.Second

// removeMapper removes a device-mapper device. Right after a wipe udev
// still probes the device (a change event for the closed, written device),
// and for that moment the kernel refuses with "busy": wait for udev and
// try again. Seen in QEMU with scripts/smoke-cache.py.
func (m *Manager) removeMapper(ctx context.Context, name string) error {
	var err error
	for try := 0; try < removeMapperTries; try++ {
		m.runner.Run(ctx, "", "udevadm", "settle")
		if _, err = m.runner.Run(ctx, "", "dmsetup", "remove", name); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(removeMapperPause):
		}
	}
	return err
}

// target is the target type of a one-line dmsetup table or status.
func target(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return ""
	}
	return fields[2]
}

// DisableCache takes the SSD out. A read cache without dirty blocks is
// detached at once (detachCache). A write cache first writes every dirty
// block back; RunCacheCleaner detaches and forgets it when done.
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
	out, err := m.runner.Run(ctx, "", "dmsetup", "status", cacheName(name))
	if err != nil {
		if (volume.Cache.Mode == "writeback" || volume.Cache.Removing) && m.cachePresent(volume.Cache) {
			// Not set up although the SSD is there (volume locked or not
			// mounted yet): its dirty blocks can be written back once the
			// volume runs again, so do not throw them away now. Without
			// the SSD they are gone anyway; forgetting the cache is then
			// the only way to use the array again.
			return fmt.Errorf("%w: the write cache of %s is not running; mount the volume first so that its blocks can be written back", ErrNotPossible, name)
		}
		return m.forgetCache(ctx, name, volume.Cache)
	}
	if target(out) == "linear" {
		return m.forgetCache(ctx, name, volume.Cache)
	}
	if view, ok := parseCacheStatusOK(out); ok && view.DirtyBlocks == 0 && volume.Cache.Mode == "writethrough" {
		// A read cache: nothing on the SSD the array lacks.
		if err := m.detachCache(ctx, volume); err == nil {
			return m.forgetCache(ctx, name, volume.Cache)
		} else if !errors.Is(err, errCacheDirty) {
			return err
		}
	}
	if err := m.reloadCache(ctx, volume, "writeback", "cleaner"); err != nil {
		return err
	}
	return m.updateVolume(name, func(v *VolumeConfig) { v.Cache.Removing = true })
}

// finishCacheRemoval detaches a written-back cache from its running
// volume and forgets it. The cache is only forgotten once the volume no
// longer goes through it: a cache device left in writeback mode could
// collect dirty blocks again, lost at shutdown.
func (m *Manager) finishCacheRemoval(ctx context.Context, name string) bool {
	m.actions.Lock()
	defer m.actions.Unlock()
	volume, ok := m.find(name)
	if !ok || volume.Cache == nil || !volume.Cache.Removing {
		return false
	}
	if err := m.detachCache(ctx, volume); err != nil {
		if !errors.Is(err, errCacheDirty) {
			slog.Warn("write cache not detached yet", "volume", name, "err", err)
		}
		return false
	}
	if err := m.forgetCache(ctx, name, volume.Cache); err != nil {
		slog.Warn("written-back cache not forgotten", "volume", name, "err", err)
		return false
	}
	return true
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
			if view, ok := parseCacheStatusOK(out); target(out) != "linear" && (!ok || view.DirtyBlocks > 0) {
				continue
			}
			if m.finishCacheRemoval(ctx, volume.Name) {
				slog.Info("write cache written back and removed", "volume", volume.Name)
				if m.OnCacheRemoved != nil {
					m.OnCacheRemoved(volume.Name)
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
	view, _ := parseCacheStatusOK(output)
	return view
}

// parseCacheStatusOK is parseCacheStatus that also says whether output was
// the status of a cache target at all. Callers that act on "no dirty
// blocks" must not trust a status they could not read.
func parseCacheStatusOK(output string) (CacheView, bool) {
	view := CacheView{ReadHitRate: -1}
	fields := strings.Fields(output)
	if len(fields) < 14 || fields[2] != "cache" {
		return view, false
	}
	dirty, err := strconv.ParseUint(fields[13], 10, 64)
	if err != nil {
		return view, false
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
	view.DirtyBlocks = dirty
	return view, true
}

// cacheView is the state of a volume's cache for the page.
func (m *Manager) cacheView(ctx context.Context, cfg VolumeConfig) *CacheView {
	if cfg.Cache == nil {
		return nil
	}
	view := CacheView{ReadHitRate: -1}
	if out, err := m.runner.Run(ctx, "", "dmsetup", "status", cacheName(cfg.Name)); err == nil {
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
