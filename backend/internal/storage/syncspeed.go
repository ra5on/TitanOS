package storage

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Rebuild speed of an mdadm volume (decided 31.10.2026, PLAN 13j): how fast
// the kernel rebuilds, first syncs and checks the array. md takes two limits
// per array in KiB/s per disk, sync_speed_min (kept up even while the NAS is
// busy) and sync_speed_max (never more). "system" means the kernel's own
// values (/proc/sys/dev/raid, 1000 and 200000), which titand never changes.
//
// The choice is stored with the volume (VolumeConfig.SyncSpeed, empty is
// normal; older versions ignore the field and keep the kernel's values).
// The kernel forgets the limits when the array is assembled again, so
// applySyncSpeeds sets them again whenever an array appears. In development
// mode (ReadOnly) the choice is kept but sysfs is never touched.
const (
	SyncSpeedGentle = "gentle"
	SyncSpeedNormal = "normal"
	SyncSpeedFast   = "fast"
)

// syncSpeedLimits are sync_speed_min and sync_speed_max of each preset.
var syncSpeedLimits = map[string][2]string{
	// Gentle: the NAS stays responsive, a rebuild takes longer.
	SyncSpeedGentle: {"1000", "30000"},
	SyncSpeedNormal: {"system", "system"},
	// Fast: md keeps at least 50 MB/s per disk even while the NAS is
	// in use, and may go up to what the disks manage.
	SyncSpeedFast: {"50000", "2000000"},
}

// ValidSyncSpeed reports whether preset is one of the presets.
func ValidSyncSpeed(preset string) bool {
	_, ok := syncSpeedLimits[preset]
	return ok
}

// syncSpeedOf is the preset of a configured volume ("normal" when unset or
// unknown, as an older or newer version may have stored).
func syncSpeedOf(v VolumeConfig) string {
	if v.SyncSpeed == "" || !ValidSyncSpeed(v.SyncSpeed) {
		return SyncSpeedNormal
	}
	return v.SyncSpeed
}

// hasSyncSpeed reports whether a volume has an md array that syncs:
// not Btrfs and not RAID 0.
func hasSyncSpeed(v VolumeConfig) bool {
	return v.Filesystem != "btrfs" && v.Level != "raid0"
}

// syncApplied is what titand set on an md device last (RAM only).
type syncApplied struct {
	preset string
	failed bool
}

// SetSyncSpeed stores the rebuild speed of a volume and applies it at once
// if its array runs.
func (m *Manager) SetSyncSpeed(ctx context.Context, name, preset string) error {
	if !ValidSyncSpeed(preset) {
		return fmt.Errorf("%w: unknown rebuild speed", ErrNotPossible)
	}
	m.actions.Lock()
	defer m.actions.Unlock()
	volumes := m.configured()
	for i := range volumes {
		if volumes[i].Name != name {
			continue
		}
		if !hasSyncSpeed(volumes[i]) {
			return fmt.Errorf("%w: this volume has no RAID rebuild", ErrNotPossible)
		}
		stored := preset
		if preset == SyncSpeedNormal {
			stored = ""
		}
		if volumes[i].SyncSpeed == stored {
			return nil
		}
		volumes[i].SyncSpeed = stored
		if err := m.saveVolumes(volumes); err != nil {
			return err
		}
		if m.ReadOnly {
			return nil
		}
		if a, ok := m.arrayOf(ctx, volumes[i]); ok {
			return m.writeSyncSpeed(a.device, preset)
		}
		return nil
	}
	return ErrNotFound
}

// writeSyncSpeed sets the limits of a preset on an md device.
func (m *Manager) writeSyncSpeed(device, preset string) error {
	limits := syncSpeedLimits[preset]
	dir := filepath.Join(m.sysBlock, device, "md")
	// The upper limit first, so that min never stands above max.
	err := os.WriteFile(filepath.Join(dir, "sync_speed_max"), []byte(limits[1]), 0o644)
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "sync_speed_min"), []byte(limits[0]), 0o644)
	}
	m.mu.Lock()
	if m.syncSpeeds == nil {
		m.syncSpeeds = map[string]syncApplied{}
	}
	m.syncSpeeds[device] = syncApplied{preset: preset, failed: err != nil}
	m.mu.Unlock()
	if err != nil {
		return fmt.Errorf("set rebuild speed of %s: %w", device, err)
	}
	return nil
}

// applySyncSpeeds sets the stored rebuild speeds on arrays that appeared
// since the last round, or lost their limits (assembled again). It only
// reads a few sysfs files while nothing changed; mdadm is asked only then.
func (m *Manager) applySyncSpeeds(ctx context.Context) {
	if m.ReadOnly {
		return
	}
	entries, err := os.ReadDir(m.sysBlock)
	if err != nil {
		return
	}
	present := map[string]bool{}
	need := false
	m.mu.Lock()
	for _, entry := range entries {
		device := entry.Name()
		if !strings.HasPrefix(device, "md") {
			continue
		}
		present[device] = true
		applied, ok := m.syncSpeeds[device]
		switch {
		case !ok:
			need = true
		case applied.preset != "" && applied.preset != SyncSpeedNormal && !applied.failed:
			// The kernel marks limits set for the array "(local)".
			if !strings.Contains(readSysfs(filepath.Join(m.sysBlock, device, "md", "sync_speed_max")), "local") {
				need = true
			}
		}
	}
	for device := range m.syncSpeeds {
		if !present[device] {
			delete(m.syncSpeeds, device)
		}
	}
	m.mu.Unlock()
	if !need || !m.actions.TryLock() {
		return
	}
	defer m.actions.Unlock()
	configured := m.configured()
	for _, a := range m.arrays(ctx) {
		preset := ""
		for _, v := range configured {
			if a.uuid != "" && a.uuid == normalizeUUID(v.ArrayUUID) && hasSyncSpeed(v) && v.SyncSpeed != "" {
				preset = syncSpeedOf(v)
			}
		}
		if preset == "" || preset == SyncSpeedNormal {
			// Not ours to set: the kernel's values stay.
			m.mu.Lock()
			if m.syncSpeeds == nil {
				m.syncSpeeds = map[string]syncApplied{}
			}
			m.syncSpeeds[a.device] = syncApplied{}
			m.mu.Unlock()
			continue
		}
		if err := m.writeSyncSpeed(a.device, preset); err != nil {
			slog.Warn("rebuild speed not set", "device", a.device, "err", err)
		} else {
			slog.Info("rebuild speed set", "device", a.device, "speed", preset)
		}
	}
}
