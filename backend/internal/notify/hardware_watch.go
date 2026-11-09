package notify

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// HardwareWatcher reports what the board says about itself: a fan that
// stopped, a temperature near its critical value, and memory errors that
// ECC corrected or could not correct. Like Watcher it tells when something
// changes, not what is.
type HardwareWatcher struct {
	notify *Manager
	// Fans and Temperatures read the sensors; nil leaves them out.
	Fans         func() []FanReading
	Temperatures func() []TempReading
	// Poweroff shuts the NAS down in an orderly way when a reliable sensor
	// stays too hot and the owner switched that on (heat_shutdown.go); nil
	// never shuts down.
	Poweroff func() error
	// edac is where the kernel counts memory errors.
	edac string

	spun    map[string]bool
	stopped map[string]int
	hot     map[string]bool
	// hotLooks counts looks in a row at or above the limit.
	hotLooks map[string]int
	// heatLooks counts looks in a row at the limit of ShutsDownOnHeat;
	// poweredOff is set once the shutdown was asked for.
	heatLooks  map[string]int
	poweredOff bool
	ce, ue     map[string]int64
}

// FanReading is one fan with a speed sensor.
type FanReading struct {
	ID, Label string
	RPM       *int
	// Percent is the duty it is driven with; 0 means it may stand on purpose.
	Percent int
}

// TempReading is one temperature sensor.
type TempReading struct {
	ID, Label string
	// Channel is the hwmon channel (temp8); a Label equal to it means the
	// channel has none. Device is the sysfs path of the chip below /sys.
	Channel, Device string
	// Chip is the hwmon driver name (coretemp, nvme, nct6798 ...).
	Chip              string
	Celsius, Critical *float64
}

// unlabelled says in the language of the notifications that a channel
// has no label, so that "temp8" does not read like a name.
var unlabelled = map[string]string{"de": "ohne Bezeichnung", "en": "no label"}

// subject names a hot sensor so that it can be found: chip, label or
// channel, the reading, and the sysfs path of the chip.
func (t TempReading) subject(language string) string {
	name := t.Label
	if name == "" {
		name = t.ID
	}
	if t.Chip != "" {
		name = t.Chip + " " + name
	}
	inside := strconv.Itoa(int(*t.Celsius)) + " °C"
	if t.Channel != "" && (t.Label == "" || t.Label == t.Channel) {
		word, ok := unlabelled[language]
		if !ok {
			word = unlabelled["en"]
		}
		inside += ", " + word
	}
	name += " (" + inside + ")"
	if t.Device != "" {
		name += " – /sys/" + t.Device
	}
	return name
}

// reliableChips are drivers whose sensors measure a real component, so a
// reading above the limit of their kind (chipLimits) is believed even
// without a critical value of their own. Inputs of other chips (Super-I/O, ACPI) are often unused
// and read nonsense, so they only raise an alarm with a critical value
// the chip itself reports.
var reliableChips = map[string]bool{"coretemp": true, "k10temp": true, "zenpower": true, "nvme": true, "drivetemp": true, "amdgpu": true, "radeon": true, "nouveau": true}

// chipLimits are the alarm limits of reliable chips that report no
// critical value of their own: hard disks are specified up to about
// 60 °C, NVMe drives throttle at around 80–85 °C. Other reliable chips
// use defaultLimit. Disk channels reach the watcher only if someone feeds
// them in; system.ReadTemperatures leaves disks to the storage monitor
// (disk_hot), so that reading them does not wake sleeping disks.
var chipLimits = map[string]float64{"drivetemp": 60, "nvme": 85}

// defaultLimit is the limit of reliable chips without a value of their own.
const defaultLimit = 95.0

// hotLooksNeeded is how many looks in a row must be too hot, so that a
// reading that jumps once is no alarm.
const hotLooksNeeded = 2

// hotMargin is how close to the critical value counts as too hot, and
// coolMargin how far below it the warning is over.
const hotMargin, coolMargin = 5.0, 15.0

// NewHardwareWatcher returns a watcher that reports through m.
func NewHardwareWatcher(m *Manager) *HardwareWatcher {
	return &HardwareWatcher{notify: m, edac: "/sys/devices/system/edac/mc", spun: map[string]bool{}, stopped: map[string]int{}, hot: map[string]bool{}, hotLooks: map[string]int{}, heatLooks: map[string]int{}, ce: map[string]int64{}, ue: map[string]int64{}}
}

// Run looks every minute until ctx is done.
func (w *HardwareWatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		w.look()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *HardwareWatcher) look() {
	if w.Fans != nil {
		for _, fan := range w.Fans() {
			if fan.RPM == nil {
				continue
			}
			name := fan.Label
			if name == "" {
				name = fan.ID
			}
			switch {
			case *fan.RPM > 0:
				w.spun[fan.ID] = true
				w.stopped[fan.ID] = 0
			case w.spun[fan.ID] && fan.Percent > 0:
				// It turned before and is driven now: two looks in a row
				// at zero, so that a slow start is no alarm.
				w.stopped[fan.ID]++
				if w.stopped[fan.ID] == 2 {
					w.notify.Notify(Error, "fan_stopped", name)
				}
			}
		}
	}
	if w.Temperatures != nil {
		for _, t := range w.Temperatures() {
			if t.Celsius == nil {
				// No reading breaks a run: the two hot looks must be in a row.
				delete(w.hotLooks, t.ID)
				delete(w.heatLooks, t.ID)
				continue
			}
			w.lookHeat(t)
			limit, alarms := AlarmLimit(t.Chip, t.Critical)
			if !alarms || w.notify.TemperatureMuted(t.ID) {
				delete(w.hotLooks, t.ID)
				delete(w.hot, t.ID)
				continue
			}
			switch {
			case *t.Celsius >= limit:
				w.hotLooks[t.ID]++
				if w.hotLooks[t.ID] >= hotLooksNeeded && !w.hot[t.ID] {
					w.hot[t.ID] = true
					w.notify.Notify(Error, "temperature_critical", t.subject(w.notify.language()))
				}
			default:
				w.hotLooks[t.ID] = 0
				if *t.Celsius < limit-coolMargin {
					delete(w.hot, t.ID)
				}
			}
		}
	}
	w.lookMemory()
}

// lookHeat shuts the NAS down when a reliable sensor (ShutsDownOnHeat)
// that is not muted stays at its alarm limit for heatLooksNeeded looks in a
// row and the owner switched it on. It says so first, then shuts down once
// in an orderly way (VMs and apps stop, as with the power schedule).
func (w *HardwareWatcher) lookHeat(t TempReading) {
	limit, ok := ShutsDownOnHeat(t.Chip, t.Critical)
	if !ok || *t.Celsius < limit || w.notify.TemperatureMuted(t.ID) {
		delete(w.heatLooks, t.ID)
		return
	}
	w.heatLooks[t.ID]++
	if w.heatLooks[t.ID] < heatLooksNeeded || w.poweredOff || w.Poweroff == nil || !w.notify.HeatShutdown() {
		return
	}
	w.poweredOff = true
	w.notify.Notify(Error, "heat_shutdown", t.subject(w.notify.language()))
	if err := w.Poweroff(); err != nil {
		// It may try again on the next looks.
		w.poweredOff = false
		w.notify.Notify(Error, "heat_shutdown_failed", err.Error())
	}
}

// lookMemory compares the ECC counters of each memory controller with the
// last look; the first look only learns them.
func (w *HardwareWatcher) lookMemory() {
	controllers, _ := filepath.Glob(filepath.Join(w.edac, "mc[0-9]*"))
	for _, mc := range controllers {
		id := filepath.Base(mc)
		ce, okCE := counter(filepath.Join(mc, "ce_count"))
		ue, okUE := counter(filepath.Join(mc, "ue_count"))
		if okCE {
			if last, seen := w.ce[id]; seen && ce > last {
				w.notify.Notify(Warning, "memory_corrected", id+": "+strconv.FormatInt(ce-last, 10))
			}
			w.ce[id] = ce
		}
		if okUE {
			if last, seen := w.ue[id]; seen && ue > last {
				w.notify.Notify(Error, "memory_uncorrected", id+": "+strconv.FormatInt(ue-last, 10))
			}
			w.ue[id] = ue
		}
	}
}

func counter(path string) (int64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	return n, err == nil
}
