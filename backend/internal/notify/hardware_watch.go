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
	// edac is where the kernel counts memory errors.
	edac string

	spun    map[string]bool
	stopped map[string]int
	hot     map[string]bool
	ce, ue  map[string]int64
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
	ID, Label         string
	Celsius, Critical *float64
}

// hotMargin is how close to the critical value counts as too hot, and
// coolMargin how far below it the warning is over.
const hotMargin, coolMargin = 5.0, 15.0

// NewHardwareWatcher returns a watcher that reports through m.
func NewHardwareWatcher(m *Manager) *HardwareWatcher {
	return &HardwareWatcher{notify: m, edac: "/sys/devices/system/edac/mc", spun: map[string]bool{}, stopped: map[string]int{}, hot: map[string]bool{}, ce: map[string]int64{}, ue: map[string]int64{}}
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
				continue
			}
			limit := 95.0
			if t.Critical != nil && *t.Critical > 40 {
				limit = *t.Critical - hotMargin
			}
			name := t.Label
			if name == "" {
				name = t.ID
			}
			if *t.Celsius >= limit && !w.hot[t.ID] {
				w.hot[t.ID] = true
				w.notify.Notify(Error, "temperature_critical", name+" ("+strconv.Itoa(int(*t.Celsius))+" °C)")
			} else if *t.Celsius < limit-coolMargin {
				delete(w.hot, t.ID)
			}
		}
	}
	w.lookMemory()
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
