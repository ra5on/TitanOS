package storage

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/naming"
)

// systemDataMaxAge is how long the choice of SystemDataVolume is reused
// while the mounted volumes and the disk sleep settings stay the same. A
// disk that joins or leaves a volume is noticed after this time.
const systemDataMaxAge = 5 * time.Minute

// systemDataChoice is the last answer of SystemDataVolume (RAM only).
type systemDataChoice struct {
	key    string
	at     time.Time
	volume string
}

// SystemDataVolume is the volume TitanOS keeps its own data on (kept
// journal, activity log, copies of /config, notifications, SMART history)
// when no volume was chosen for them: the first mounted volume by name
// whose disks never sleep, that is one without a rotational member disk
// with an effective disk sleep time above zero (SSD and NVMe volumes, and
// HDD volumes with disk sleep off). Only if every mounted volume may sleep
// it is the first one by name, as before.
//
// It answers "" while no volume is mounted, and while the volumes or the
// disk sleep settings are not known yet (shortly after the start, or with
// an unreadable stick): callers then wait instead of writing to a volume
// that would turn out to be the wrong one. It lists the disks like Status
// (lsblk) at most every few minutes and reads neither a disk nor /config.
func (m *Manager) SystemDataVolume(ctx context.Context) string {
	mounted := slices.DeleteFunc(m.Mounted(), func(name string) bool { return !naming.ValidVolume(name) })
	if len(mounted) == 0 || !m.Settled() {
		return ""
	}
	slices.Sort(mounted)
	sleep := m.DiskSleep()
	if !sleep.Available && !m.ReadOnly {
		return ""
	}
	settings := sleep.DiskSleepSettings
	if !settings.anyActive() {
		return mounted[0] // no disk sleeps: nothing to list
	}
	key := strings.Join(mounted, "\x00") + "\x01" + settings.fingerprint()
	now := time.Now()
	m.mu.RLock()
	last := m.systemData
	m.mu.RUnlock()
	if last.key == key && now.Sub(last.at) < systemDataMaxAge {
		return last.volume
	}
	devices, err := m.blockDevices(ctx)
	if err != nil {
		// Without the disks nothing says which volume may sleep: stay where
		// the data went so far, or wait.
		if slices.Contains(mounted, last.volume) {
			return last.volume
		}
		return ""
	}
	volume := pickSystemDataVolume(mounted, sleepingVolumes(m.diskSleepStatus(devices)))
	m.mu.Lock()
	m.systemData = systemDataChoice{key: key, at: now, volume: volume}
	m.mu.Unlock()
	return volume
}

// fingerprint tells two settings apart for the cache of SystemDataVolume.
func (s DiskSleepSettings) fingerprint() string {
	var b strings.Builder
	write := func(key string, minutes int) {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(strconv.Itoa(minutes))
		b.WriteByte('\x00')
	}
	write("", s.IdleMinutes)
	for _, overrides := range []map[string]int{s.Volumes, s.Disks} {
		keys := make([]string, 0, len(overrides))
		for key := range overrides {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			write(key, overrides[key])
		}
		b.WriteByte('\x01')
	}
	return b.String()
}

// sleepingVolumes are the volumes with at least one disk TitanOS may put
// to sleep: a member that can sleep and has an effective idle time.
func sleepingVolumes(status DiskSleepStatus) map[string]bool {
	sleeping := map[string]bool{}
	for _, disk := range status.DiskStatus {
		if disk.Volume != "" && disk.Effective > 0 {
			sleeping[disk.Volume] = true
		}
	}
	return sleeping
}

// pickSystemDataVolume takes the first volume by name that never sleeps,
// else the first one. mounted is sorted and not empty.
func pickSystemDataVolume(mounted []string, sleeping map[string]bool) string {
	for _, volume := range mounted {
		if !sleeping[volume] {
			return volume
		}
	}
	return mounted[0]
}
