package notify

import (
	"errors"
	"io/fs"
	"regexp"
	"slices"
)

// mutesFile holds the temperature sensors that raise no notification. It
// is kept apart from the settings, so that a version without it, saving
// the settings, does not drop it.
const mutesFile = "notify/temperature-mutes.json"

// maxMutes bounds the list; a NAS has far fewer sensors.
const maxMutes = 256

// sensorID is the stable ID of a temperature sensor (system.Temperature).
var sensorID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// AlarmLimit is the temperature from which a sensor raises an alarm, and
// false if it never does. A critical value of the chip itself above 40 °C
// comes first: 5 °C below it. Without one, reliable chips use the limit
// of their kind (chipLimits, else 95 °C); other chips never alarm, since
// unused inputs of Super-I/O chips read nonsense.
func AlarmLimit(chip string, critical *float64) (float64, bool) {
	limit, _, ok := AlarmLimitSource(chip, critical)
	return limit, ok
}

// AlarmLimitSource is AlarmLimit and whether the limit comes from the
// chip's own critical value (true) or from the default of its kind.
func AlarmLimitSource(chip string, critical *float64) (limit float64, own, alarms bool) {
	switch {
	case critical != nil && *critical > 40:
		return *critical - hotMargin, true, true
	case chip == "" || reliableChips[chip]:
		if limit, ok := chipLimits[chip]; ok {
			return limit, false, true
		}
		return defaultLimit, false, true
	}
	return 0, false, false
}

func (m *Manager) loadMutesLocked() error {
	var file struct {
		Muted []string `json:"muted"`
	}
	err := m.store.ReadJSON(mutesFile, &file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.mutes = map[string]bool{}
	for _, id := range file.Muted {
		if sensorID.MatchString(id) {
			m.mutes[id] = true
		}
	}
	return nil
}

// MutedTemperatures returns the sensors that raise no notification, sorted.
func (m *Manager) MutedTemperatures() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := []string{}
	for id := range m.mutes {
		list = append(list, id)
	}
	slices.Sort(list)
	return list
}

// TemperatureMuted says whether a sensor raises no notification.
func (m *Manager) TemperatureMuted(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mutes[id]
}

// SetMutedTemperatures stores the sensors that raise no notification. IDs
// of sensors that are gone now are kept: they may come back.
func (m *Manager) SetMutedTemperatures(ids []string) error {
	mutes := map[string]bool{}
	for _, id := range ids {
		if !sensorID.MatchString(id) {
			return invalid("sensor %q", id)
		}
		mutes[id] = true
	}
	if len(mutes) > maxMutes {
		return invalid("too many sensors")
	}
	list := make([]string, 0, len(mutes))
	for id := range mutes {
		list = append(list, id)
	}
	slices.Sort(list)
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	if err := m.store.WriteJSON(mutesFile, map[string][]string{"muted": list}); err != nil {
		return err
	}
	m.mutes = mutes
	return nil
}
