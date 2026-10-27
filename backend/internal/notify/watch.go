package notify

import (
	"context"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/storage"
)

// Thresholds for the warnings about volumes and disks. A warning is given
// once when the upper value is passed and again only after the lower one
// was reached in between.
const (
	fullPercent, roomyPercent = 90, 85
	hotCelsius, coolCelsius   = 55, 50
	// startGrace is how long after the start a volume that is not (yet)
	// there is not reported as failed or missing: mdadm starts an array
	// with a missing disk only after its last-resort timer, Btrfs waits a
	// minute for its members, and slow controllers bring disks late.
	startGrace = 3 * time.Minute
)

// Watcher turns the state of volumes and disks into messages: it tells
// when something changes, not what is.
type Watcher struct {
	notify *Manager
	// reported is the trouble each volume was last reported with ("" for
	// none); seen is what the last look found. Trouble is reported when
	// two looks in a row find it: while the NAS starts, the arrays take a
	// moment to come up.
	reported, seen map[string]string
	known          map[string]bool
	checking       map[string]bool
	// full, hot and unhealthy are the warnings that are standing.
	full, hot, unhealthy map[string]bool
	// settled is when startGrace ends; zero for no grace.
	settled time.Time
	now     func() time.Time
}

// NewWatcher returns a watcher that reports through m.
func NewWatcher(m *Manager) *Watcher {
	return &Watcher{
		notify: m, reported: map[string]string{}, seen: map[string]string{}, known: map[string]bool{},
		checking: map[string]bool{}, full: map[string]bool{}, hot: map[string]bool{}, unhealthy: map[string]bool{},
		now: time.Now,
	}
}

// Run looks at the storage every 30 seconds until ctx is done.
func (w *Watcher) Run(ctx context.Context, status func(context.Context) (storage.Status, error)) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	w.settled = w.now().Add(startGrace)
	for {
		// The stick may not have been there when titand started.
		if !w.notify.Loaded() {
			w.notify.Load()
		}
		if current, err := status(ctx); err == nil {
			w.Observe(current)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// trouble maps the state of a volume to what there is to report about it.
// The second value is false for states that say nothing about its health:
// during a check or the first sync the last report stands.
func trouble(state string) (string, bool) {
	switch state {
	case storage.VolumeDegraded, storage.VolumeFailed, storage.VolumeMissing, storage.VolumeRebuilding:
		return state, true
	case storage.VolumeOK:
		return "", true
	}
	return "", false
}

// Observe compares the storage with what was seen last and reports changes.
func (w *Watcher) Observe(status storage.Status) {
	for _, volume := range status.Volumes {
		name := volume.Name
		now, clear := trouble(volume.State)
		if (now == storage.VolumeFailed || now == storage.VolumeMissing) && w.now().Before(w.settled) {
			clear = false // still starting: neither reported nor counted
		}
		if clear {
			before := w.reported[name]
			twice := w.seen[name] == now && w.known[name]
			w.seen[name], w.known[name] = now, true
			if now != before && twice {
				w.reported[name] = now
				switch now {
				case storage.VolumeDegraded:
					w.notify.Notify(Error, "volume_degraded", name)
				case storage.VolumeFailed:
					w.notify.Notify(Error, "volume_failed", name)
				case storage.VolumeMissing:
					w.notify.Notify(Error, "volume_missing", name)
				case storage.VolumeRebuilding:
					w.notify.Notify(Warning, "volume_rebuilding", name)
				default:
					w.notify.Notify(Info, "volume_ok", name)
				}
			}
		}

		// A check that ends with copies that differ is worth a look.
		checking := volume.State == storage.VolumeChecking
		if w.checking[name] && !checking && volume.Mismatches > 0 {
			w.notify.Notify(Warning, "volume_mismatch", name)
		}
		w.checking[name] = checking

		if volume.Mounted && volume.SizeBytes > 0 {
			percent := int(volume.UsedBytes * 100 / volume.SizeBytes)
			if percent >= fullPercent && !w.full[name] {
				w.full[name] = true
				w.notify.Notify(Warning, "volume_full", name)
			} else if percent < roomyPercent {
				delete(w.full, name)
			}
		}
	}
	for _, disk := range status.Disks {
		if disk.Smart == nil {
			continue
		}
		// Device names change between starts, the serial number does not.
		key, label := disk.Name, disk.Name
		if disk.Serial != "" {
			key, label = disk.Serial, disk.Name+" ("+disk.Model+", "+disk.Serial+")"
		}
		if !disk.Smart.Healthy && !w.unhealthy[key] {
			w.unhealthy[key] = true
			w.notify.Notify(Error, "disk_unhealthy", label)
		} else if disk.Smart.Healthy {
			delete(w.unhealthy, key)
		}
		if disk.Smart.TemperatureC >= hotCelsius && !w.hot[key] {
			w.hot[key] = true
			w.notify.Notify(Warning, "disk_hot", label)
		} else if disk.Smart.TemperatureC < coolCelsius {
			delete(w.hot, key)
		}
	}
}
