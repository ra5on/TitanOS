package devices

import (
	"errors"
	"io/fs"
	"log/slog"
	"time"
)

// "Last synced" (TECHNIK 3.5): per device in RAM (time, kind, address),
// on the volume in .titanos/calendar/<account>/devices-seen.json at most
// hourly per device and only when the volume is open for the request
// anyway – never on the stick.

// seenEvery: a device's time is written to the volume at most this often.
const seenEvery = time.Hour

type seen struct {
	At   time.Time
	Kind string
	Addr string
	// saved: when this device's time last went to the volume; before:
	// read from the volume after a restart.
	saved  time.Time
	before bool
}

// seenFile is devices-seen.json (no addresses on the volume).
type seenFile struct {
	Devices map[string]seenEntry `json:"devices"`
}

type seenEntry struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
}

// Synced notes a successful answer to a device (kind CalDAV or CardDAV,
// addr its address). volumeOpen says the request had the volume open
// anyway (a read or write of entries, not an answer from RAM): only then
// the time may go to the volume, at most hourly per device.
func (m *Manager) Synced(account, device, kind, addr string, volumeOpen bool) {
	if volumeOpen && m.seen != nil {
		// What the volume holds from before a restart is not overwritten.
		m.loadSeen(account)
	}
	now := m.now()
	m.mu.Lock()
	byDevice := m.synced[account]
	if byDevice == nil {
		byDevice = map[string]*seen{}
		m.synced[account] = byDevice
	}
	s := byDevice[device]
	if s == nil {
		s = &seen{}
		byDevice[device] = s
	}
	s.At, s.Kind, s.Addr, s.before = now.UTC(), kind, addr, false
	due := volumeOpen && m.seen != nil && m.seenLoaded[account] && now.Sub(s.saved) >= seenEvery
	var snapshot seenFile
	if due {
		s.saved = now
		snapshot.Devices = map[string]seenEntry{}
		for id, d := range byDevice {
			snapshot.Devices[id] = seenEntry{At: d.At, Kind: d.Kind}
		}
	}
	m.mu.Unlock()
	if due {
		if err := m.seen.SaveSeen(account, snapshot); err != nil {
			slog.Debug("Calendar: last sync not saved", "user", account, "err", err)
		}
	}
}

// loadSeen reads devices-seen.json once per account after a restart; a
// volume that is not there now is asked again next time.
func (m *Manager) loadSeen(account string) {
	if m.seen == nil {
		return
	}
	m.mu.Lock()
	done := m.seenLoaded[account]
	m.mu.Unlock()
	if done {
		return
	}
	var f seenFile
	if err := m.seen.LoadSeen(account, &f); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seenLoaded[account] {
		return
	}
	m.seenLoaded[account] = true
	byDevice := m.synced[account]
	if byDevice == nil {
		byDevice = map[string]*seen{}
		m.synced[account] = byDevice
	}
	for id, e := range f.Devices {
		if cur := byDevice[id]; cur == nil || cur.At.Before(e.At) {
			byDevice[id] = &seen{At: e.At, Kind: e.Kind, before: true}
		}
	}
}
