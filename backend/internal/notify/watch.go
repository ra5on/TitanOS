package notify

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/storage"
)

// Thresholds for the warnings about volumes and disks. A warning is given
// once when the upper value is passed and again only after the lower one
// was reached in between.
const (
	fullPercent, roomyPercent = 90, 85
	hotCelsius, coolCelsius   = 55, 50
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
	// worn is what was last reported about the wear of a disk (by serial);
	// it is kept in wornFile so that a restart of titand does not repeat it.
	worn     map[string]wear
	wornFile string
}

// NewWatcher returns a watcher that reports through m.
func NewWatcher(m *Manager) *Watcher {
	return &Watcher{
		notify: m, reported: map[string]string{}, seen: map[string]string{}, known: map[string]bool{},
		checking: map[string]bool{}, full: map[string]bool{}, hot: map[string]bool{}, unhealthy: map[string]bool{},
		worn: map[string]wear{}, wornFile: "/run/titanos/disk-wear.json",
	}
}

// Run looks at the storage every 30 seconds until ctx is done.
func (w *Watcher) Run(ctx context.Context, status func(context.Context) (storage.Status, error)) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
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
		if now, clear := trouble(volume.State); clear {
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
		w.observeWear(key, label, disk.SmartWarnings, disk.SmartDefects)
		if disk.Smart.TemperatureC >= hotCelsius && !w.hot[key] {
			w.hot[key] = true
			w.notify.Notify(Warning, "disk_hot", label)
		} else if disk.Smart.TemperatureC < coolCelsius {
			delete(w.hot, key)
		}
	}
}

// wear is what was reported about a disk: its warnings and defect count.
type wear struct {
	Warnings []string `json:"warnings"`
	Defects  uint64   `json:"defects"`
}

// observeWear reports a disk that shows a new sign of wear (a SMART warning
// it did not have) or whose defect counters grew since the last report.
// The SMART self-assessment "failed" has its own message (disk_unhealthy).
func (w *Watcher) observeWear(key, label string, warnings []string, defects uint64) {
	var now []string
	for _, code := range warnings {
		if slices.Contains(storage.WearWarnings, code) {
			now = append(now, code)
		}
	}
	if len(w.worn) == 0 && w.wornFile != "" {
		if data, err := os.ReadFile(w.wornFile); err == nil {
			json.Unmarshal(data, &w.worn)
		}
	}
	before, known := w.worn[key]
	if len(now) == 0 {
		if known {
			delete(w.worn, key)
			w.saveWorn()
		}
		return
	}
	fresh := slices.ContainsFunc(now, func(code string) bool { return !slices.Contains(before.Warnings, code) })
	if !fresh && defects <= before.Defects {
		return
	}
	w.worn[key] = wear{Warnings: now, Defects: defects}
	w.saveWorn()
	w.notify.Notify(Warning, "disk_wear", label)
}

func (w *Watcher) saveWorn() {
	if w.wornFile == "" {
		return
	}
	data, _ := json.Marshal(w.worn)
	os.MkdirAll(filepath.Dir(w.wornFile), 0o755)
	if err := os.WriteFile(w.wornFile+".tmp", data, 0o600); err == nil {
		os.Rename(w.wornFile+".tmp", w.wornFile)
	}
}
