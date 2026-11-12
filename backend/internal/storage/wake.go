package storage

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"slices"
	"sort"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/diskwake"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// What woke a sleeping disk (PLAN 79-6 step 4, 79-16 a). TitanOS traces no
// I/O: every part of it that touches a volume notes that in RAM
// (diskwake.Note) right before, and when the sleep timer sees the first
// I/O after a confirmed standby it takes what was noted for the disk's
// volume around that moment. Nothing noted means it was not TitanOS itself
// (a share, an app, a VM).

const (
	// wakeLead is how long before the last idle sample a noted access still
	// counts: a write reaches the disk only when the kernel flushes it
	// (dirty pages after up to 30 s, the Btrfs commit every 30 s).
	wakeLead = 45 * time.Second
	// wakeHistory is how many wakes are kept per disk, wakeDays for how
	// many days they are counted, wakeDisks for how many disks.
	wakeHistory = 20
	wakeDays    = 14
	wakeDisks   = 64
	wakeCauses  = 6

	wakeFeature = "disksleep"
	wakeFile    = "wakes.json"
	wakeLimit   = 1 << 20
	// wakeRetry is how long a failed read or write of the file waits.
	wakeRetry = 5 * time.Minute
)

// DiskWake is one time a sleeping disk woke up. Causes is never empty:
// without anything TitanOS knows of it is the single code "unknown".
type DiskWake struct {
	At     time.Time        `json:"at"`
	Causes []diskwake.Cause `json:"causes"`
}

// DiskWakeDay counts the wakes of a disk on one day (local time).
type DiskWakeDay struct {
	Day   string `json:"day"` // 2026-11-12
	Count int    `json:"count"`
}

// wakeLog is the history of one disk: newest wake first.
type wakeLog struct {
	Wakes []DiskWake     `json:"wakes"`
	Days  map[string]int `json:"days,omitempty"`
}

// wakeState is the history file: by sleepDiskID.
type wakeState struct {
	Version int                `json:"version"`
	Disks   map[string]wakeLog `json:"disks"`
}

func (m *Manager) wakeNotes() *diskwake.Recorder {
	if m.wakeRecorder != nil {
		return m.wakeRecorder
	}
	return diskwake.Default
}

// wakeKey is what the history of a disk is kept under: its stable identity,
// else its device name (RAM only, never stored).
func wakeKey(id, name string) string {
	if id != "" {
		return id
	}
	return "name:" + name
}

// wakeCausesFor says what woke a disk of volume whose first I/O came
// between idle (the last sample without activity) and now. asleepSince
// bounds the window: what was noted before the disk went to sleep did not
// wake it. busy is why the disk is exempt from sleeping now (a test, a VM,
// RAID or Btrfs maintenance), if it is.
func (m *Manager) wakeCausesFor(ctx context.Context, volume string, asleepSince, idle, now time.Time, busy *diskwake.Cause) []diskwake.Cause {
	from := idle.Add(-wakeLead)
	if from.Before(asleepSince) {
		from = asleepSince
	}
	var causes []diskwake.Cause
	if volume != "" {
		causes = m.wakeNotes().Recent(volume, from, now)
	}
	if busy != nil && !slices.ContainsFunc(causes, func(c diskwake.Cause) bool { return c.Code == busy.Code }) {
		causes = append([]diskwake.Cause{*busy}, causes...)
	}
	if len(causes) == 0 && volume != "" && m.WakeProbe != nil {
		// Only when TitanOS itself was not it: who else is on the volume.
		causes = m.WakeProbe(ctx, volume)
	}
	if len(causes) == 0 {
		return []diskwake.Cause{{Code: diskwake.Unknown}}
	}
	if len(causes) > wakeCauses {
		causes = causes[:wakeCauses]
	}
	return causes
}

// recordWake adds a wake to the history of a disk (caller holds actions).
func (m *Manager) recordWake(key string, wake DiskWake) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.diskWakes == nil {
		m.diskWakes = map[string]wakeLog{}
	}
	log, known := m.diskWakes[key]
	if !known && len(m.diskWakes) >= wakeDisks {
		return
	}
	log.Wakes = append([]DiskWake{wake}, log.Wakes...)
	if len(log.Wakes) > wakeHistory {
		log.Wakes = log.Wakes[:wakeHistory]
	}
	days := map[string]int{}
	for day, count := range log.Days {
		days[day] = count
	}
	days[wake.At.Local().Format(time.DateOnly)]++
	log.Days = trimWakeDays(days)
	m.diskWakes[key] = log
	m.diskWakesDirty = true
}

// trimWakeDays keeps the newest wakeDays days.
func trimWakeDays(days map[string]int) map[string]int {
	if len(days) <= wakeDays {
		return days
	}
	keys := make([]string, 0, len(days))
	for day := range days {
		keys = append(keys, day)
	}
	sort.Strings(keys)
	for _, day := range keys[:len(keys)-wakeDays] {
		delete(days, day)
	}
	return days
}

// wakesOf returns the history of a disk for the status: the wakes, how
// many there were today and on the last days (newest first).
func (m *Manager) wakesOf(key string, now time.Time) (wakes []DiskWake, today int, days []DiskWakeDay) {
	m.mu.RLock()
	log := m.diskWakes[key]
	m.mu.RUnlock()
	for day, count := range log.Days {
		days = append(days, DiskWakeDay{Day: day, Count: count})
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Day > days[j].Day })
	if len(days) > 7 {
		days = days[:7]
	}
	return slices.Clone(log.Wakes), log.Days[now.Local().Format(time.DateOnly)], days
}

// wakeVolume is where the history file goes: the system data volume, and
// only one whose disks never sleep. "" means RAM only for now.
func (m *Manager) wakeVolume(sleeping map[string]bool) string {
	if m.WakeDataVolume == nil {
		return ""
	}
	volume := m.WakeDataVolume()
	if volume == "" || sleeping[volume] || !slices.Contains(m.Mounted(), volume) {
		return ""
	}
	return volume
}

// syncWakes reads the stored history once and writes it after a wake
// (caller holds actions). It touches only a volume that never sleeps;
// without one the history stays in RAM. It runs only when a wake was
// recorded or nothing was read yet: no cyclic write.
func (m *Manager) syncWakes(sleeping map[string]bool, now time.Time) {
	m.mu.RLock()
	dirty, loaded := m.diskWakesDirty, m.diskWakesLoaded
	m.mu.RUnlock()
	if !dirty && loaded {
		return
	}
	volume := m.wakeVolume(sleeping)
	if volume == "" || now.Sub(m.diskWakesTried) < wakeRetry && !now.Before(m.diskWakesTried) {
		return
	}
	root, err := volumedata.Open(m.mountRoot, volume, wakeFeature, m.Mounted())
	if err != nil {
		m.diskWakesTried = now // no cyclic attempts on a folder that fails
		return
	}
	defer root.Close()
	if !loaded {
		var stored wakeState
		err := volumedata.ReadJSON(root, wakeFile, &stored, wakeLimit)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("history of disk wakes not readable, starting afresh", "volume", volume, "err", err)
		}
		m.mu.Lock()
		m.diskWakes = mergeWakes(m.diskWakes, stored.Disks)
		m.diskWakesLoaded = true
		m.mu.Unlock()
	}
	if !dirty {
		return
	}
	m.mu.RLock()
	state := wakeState{Version: 1, Disks: map[string]wakeLog{}}
	for key, log := range m.diskWakes {
		if validSleepDiskID(key) { // device names change between starts
			state.Disks[key] = log
		}
	}
	m.mu.RUnlock()
	if err := volumedata.WriteJSON(root, wakeFile, state); err != nil {
		slog.Warn("history of disk wakes not written", "volume", volume, "err", err)
		m.diskWakesTried = now
		return
	}
	m.mu.Lock()
	m.diskWakesDirty = false
	m.mu.Unlock()
}

// mergeWakes puts the stored history behind what happened since the start.
func mergeWakes(current, stored map[string]wakeLog) map[string]wakeLog {
	if current == nil {
		current = map[string]wakeLog{}
	}
	for key, old := range stored {
		if !validSleepDiskID(key) || len(current) >= wakeDisks && current[key].Wakes == nil {
			continue
		}
		log := current[key]
		for _, wake := range old.Wakes {
			if len(log.Wakes) >= wakeHistory {
				break
			}
			if wake.At.IsZero() || len(wake.Causes) == 0 {
				continue
			}
			if len(wake.Causes) > wakeCauses {
				wake.Causes = wake.Causes[:wakeCauses]
			}
			log.Wakes = append(log.Wakes, wake)
		}
		sort.SliceStable(log.Wakes, func(i, j int) bool { return log.Wakes[i].At.After(log.Wakes[j].At) })
		days := map[string]int{}
		for day, count := range old.Days {
			if _, err := time.Parse(time.DateOnly, day); err == nil && count > 0 {
				days[day] = count
			}
		}
		for day, count := range log.Days {
			days[day] += count
		}
		log.Days = trimWakeDays(days)
		current[key] = log
	}
	return current
}
