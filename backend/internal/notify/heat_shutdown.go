package notify

import (
	"errors"
	"io/fs"
)

// heatFile holds the switch of Settings › Temperatures › "Bei Überhitzung
// herunterfahren" (PLAN 75-9, 7g). Kept apart from the settings like the
// mutes, so that an older version saving its settings does not drop it.
const heatFile = "notify/heat-shutdown.json"

// heatLooksNeeded is how many looks in a row (one a minute) a sensor must
// stay at its alarm limit before the NAS shuts down: five minutes, so that
// a short peak under load never switches it off.
const heatLooksNeeded = 5

// ShutsDownOnHeat says whether a sensor may switch the NAS off: only chips
// that measure a real component (reliableChips) and report a critical
// value of their own. Inputs of motherboard chips read nonsense too often
// (126 °C on an unused channel of thor, PLAN 27) and never count.
func ShutsDownOnHeat(chip string, critical *float64) (limit float64, ok bool) {
	if !reliableChips[chip] || critical == nil || *critical <= 40 {
		return 0, false
	}
	return *critical - hotMargin, true
}

func (m *Manager) loadHeatLocked() error {
	var file struct {
		Enabled bool `json:"enabled"`
	}
	err := m.store.ReadJSON(heatFile, &file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.heatShutdown = file.Enabled
	return nil
}

// HeatShutdown says whether the NAS shuts down when a reliable sensor stays
// too hot. Off by default.
func (m *Manager) HeatShutdown() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.heatShutdown
}

// SetHeatShutdown switches the shutdown on overheating on or off.
func (m *Manager) SetHeatShutdown(on bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	if err := m.store.WriteJSON(heatFile, map[string]bool{"enabled": on}); err != nil {
		return err
	}
	m.heatShutdown = on
	return nil
}
