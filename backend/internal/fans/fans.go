// Package fans controls the fans the hwmon drivers offer (Settings ›
// Temperatures › Fans, docs/FANS.md). Off unless chosen per fan: TitanOS
// leaves every fan to the BIOS until a fan gets a fixed speed or a curve.
//
// Safety first, a fan carries heat away: a curve whose sensor cannot be
// read runs the fan at full speed, no fan goes below its minimum, and when
// titand stops the fans go back to the mode they had (kept in /run, so a
// restart of titand still knows the BIOS mode). Development mode only
// reads.
package fans

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const (
	settingsFile = "system/fans.json"
	// DiskSensor is the warmest disk as SMART reports it (no disk is woken).
	DiskSensor = "disks"
	interval   = 5 * time.Second
	// hysteresis keeps a fan from going up and down on every half degree.
	hysteresis = 2.0
	maxPoints  = 8
)

var ErrInvalid = errors.New("fans: invalid settings")

// Point of a curve: at Celsius the fan runs at Percent.
type Point struct {
	Celsius float64 `json:"celsius"`
	Percent int     `json:"percent"`
}

// Control is what one fan does; Mode "bios" (default), "fixed" or "curve".
type Control struct {
	ID      string  `json:"id"`
	Mode    string  `json:"mode"`
	Percent int     `json:"percent,omitempty"`
	Sensor  string  `json:"sensor,omitempty"`
	Points  []Point `json:"points,omitempty"`
	// Min is the least a controlled fan gets, in percent (default 30).
	Min int `json:"min"`
}

// Settings are kept in /config/system/fans.json.
type Settings struct {
	Fans []Control `json:"fans"`
}

// Fan is a controllable fan as the page shows it.
type Fan struct {
	ID    string `json:"id"`
	Chip  string `json:"chip"`
	Label string `json:"label"`
	// RPM is nil when the driver reports no speed for it.
	RPM *int `json:"rpm"`
	// Percent is the duty the driver reports now.
	Percent int     `json:"percent"`
	Control Control `json:"control"`
	// SensorC is the temperature the curve follows now, if any.
	SensorC *float64 `json:"sensorC,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// channel is a pwm output of a hwmon chip.
type channel struct {
	id, chip, label string
	pwm, enable     string // files
	fanInput        string
}

// Manager discovers fans and drives them.
type Manager struct {
	store *config.Store
	sys   string
	// runtime keeps the original modes over titand restarts (RAM).
	runtime string
	// Disabled only reads (development mode).
	Disabled bool
	// Temperatures are the hwmon sensors by ID; DiskMax the warmest disk.
	Temperatures func() map[string]float64
	DiskMax      func() (float64, bool)

	mu       sync.Mutex
	loaded   bool
	settings Settings
	original map[string]string
	applied  map[string]int
	problems map[string]string
	sensors  map[string]float64
}

// New returns a manager for the configuration in store.
func New(store *config.Store) *Manager {
	return &Manager{store: store, sys: "/sys", runtime: "/run/titanos/fans-original.json",
		Temperatures: func() map[string]float64 { return nil }, DiskMax: func() (float64, bool) { return 0, false },
		original: map[string]string{}, applied: map[string]int{}, problems: map[string]string{}, sensors: map[string]float64{}}
}

// discover finds the pwm outputs that can be switched to manual.
func (m *Manager) discover() []channel {
	var out []channel
	paths, _ := filepath.Glob(filepath.Join(m.sys, "class/hwmon/hwmon*"))
	for _, path := range paths {
		chip := read(filepath.Join(path, "name"))
		if chip == "" || chip == "nvme" || chip == "drivetemp" {
			continue
		}
		device := path
		if real, err := filepath.EvalSymlinks(path); err == nil {
			device = filepath.Dir(real)
			if filepath.Base(device) == "hwmon" {
				device = filepath.Dir(device)
			}
		}
		if linked, err := filepath.EvalSymlinks(filepath.Join(path, "device")); err == nil {
			device = linked
		}
		relative, err := filepath.Rel(m.sys, device)
		if err != nil || strings.HasPrefix(relative, "..") {
			continue
		}
		pwms, _ := filepath.Glob(filepath.Join(path, "pwm[0-9]*"))
		for _, pwm := range pwms {
			name := filepath.Base(pwm)
			index := strings.TrimPrefix(name, "pwm")
			if _, err := strconv.Atoi(index); err != nil {
				continue
			}
			enable := pwm + "_enable"
			if _, err := os.Stat(enable); err != nil {
				continue
			}
			digest := sha256.Sum256([]byte(relative + "\x00" + chip + "\x00" + name))
			label := read(filepath.Join(path, "fan"+index+"_label"))
			if label == "" {
				label = "fan" + index
			}
			out = append(out, channel{id: hex.EncodeToString(digest[:16]), chip: chip, label: label, pwm: pwm, enable: enable, fanInput: filepath.Join(path, "fan"+index+"_input")})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].chip+out[i].label < out[j].chip+out[j].label })
	return out
}

func read(path string) string {
	data, _ := os.ReadFile(path)
	return strings.TrimSpace(string(data))
}

// Load reads the settings from /config and the original modes from /run.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var s Settings
	if err := m.store.ReadJSON(settingsFile, &s); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if data, err := os.ReadFile(m.runtime); err == nil {
		_ = json.Unmarshal(data, &m.original) // a value that cannot be read leaves the zero value, which the code handles
	}
	m.settings, m.loaded = s, true
	return nil
}

func check(c Control) (Control, error) {
	if c.Min == 0 {
		c.Min = 30
	}
	if c.Min < 20 || c.Min > 100 {
		return c, fmt.Errorf("%w: minimum between 20 and 100 %%", ErrInvalid)
	}
	switch c.Mode {
	case "bios":
		c.Percent, c.Sensor, c.Points = 0, "", nil
	case "fixed":
		if c.Percent < c.Min || c.Percent > 100 {
			return c, fmt.Errorf("%w: fixed speed between the minimum and 100 %%", ErrInvalid)
		}
		c.Sensor, c.Points = "", nil
	case "curve":
		if c.Sensor == "" || len(c.Points) < 2 || len(c.Points) > maxPoints {
			return c, fmt.Errorf("%w: a curve needs a sensor and 2 to %d points", ErrInvalid, maxPoints)
		}
		sort.Slice(c.Points, func(i, j int) bool { return c.Points[i].Celsius < c.Points[j].Celsius })
		for i, p := range c.Points {
			if p.Celsius < 0 || p.Celsius > 110 || p.Percent < 0 || p.Percent > 100 {
				return c, fmt.Errorf("%w: points between 0 and 110 °C, 0 and 100 %%", ErrInvalid)
			}
			if i > 0 && (p.Celsius == c.Points[i-1].Celsius || p.Percent < c.Points[i-1].Percent) {
				return c, fmt.Errorf("%w: a curve rises with the temperature", ErrInvalid)
			}
		}
		c.Percent = 0
	default:
		return c, fmt.Errorf("%w: mode", ErrInvalid)
	}
	return c, nil
}

// Set stores the controls; fans not named go back to the BIOS.
func (m *Manager) Set(s Settings) error {
	known := map[string]bool{}
	for _, ch := range m.discover() {
		known[ch.id] = true
	}
	out := Settings{Fans: []Control{}}
	for _, c := range s.Fans {
		if !known[c.ID] {
			return fmt.Errorf("%w: unknown fan", ErrInvalid)
		}
		c, err := check(c)
		if err != nil {
			return err
		}
		if c.Mode != "bios" {
			out.Fans = append(out.Fans, c)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return errors.New("fans: not loaded")
	}
	if err := m.store.WriteJSON(settingsFile, out); err != nil {
		return err
	}
	m.settings = out
	return nil
}

// curve maps a temperature to a duty, linear between the points.
func curve(points []Point, celsius float64) int {
	if celsius <= points[0].Celsius {
		return points[0].Percent
	}
	for i := 1; i < len(points); i++ {
		if celsius <= points[i].Celsius {
			a, b := points[i-1], points[i]
			f := (celsius - a.Celsius) / (b.Celsius - a.Celsius)
			return int(math.Round(float64(a.Percent) + f*float64(b.Percent-a.Percent)))
		}
	}
	return points[len(points)-1].Percent
}

// target is the duty for a control now; ok false means the sensor failed.
func (m *Manager) target(c Control, temps map[string]float64) (int, *float64, bool) {
	switch c.Mode {
	case "fixed":
		return max(c.Percent, c.Min), nil, true
	case "curve":
		var celsius float64
		var ok bool
		if c.Sensor == DiskSensor {
			celsius, ok = m.DiskMax()
		} else {
			celsius, ok = temps[c.Sensor]
		}
		if !ok {
			return 100, nil, false
		}
		// Going down waits for the temperature to drop a little further.
		m.mu.Lock()
		last, seen := m.sensors[c.ID]
		if seen && celsius < last && last-celsius < hysteresis {
			celsius = last
		} else {
			m.sensors[c.ID] = celsius
		}
		m.mu.Unlock()
		return max(curve(c.Points, celsius), c.Min), &celsius, true
	}
	return 0, nil, true
}

// apply drives every fan once.
func (m *Manager) apply() {
	m.mu.Lock()
	settings := m.settings
	m.mu.Unlock()
	temps := m.Temperatures()
	channels := m.discover()
	controlled := map[string]bool{}
	for _, c := range settings.Fans {
		i := slices.IndexFunc(channels, func(ch channel) bool { return ch.id == c.ID })
		if i < 0 {
			continue
		}
		ch := channels[i]
		controlled[c.ID] = true
		percent, _, ok := m.target(c, temps)
		problem := ""
		if !ok {
			problem = "sensor not readable, fan at full speed"
		}
		if err := m.drive(ch, percent); err != nil {
			problem = err.Error()
		}
		m.mu.Lock()
		if problem != "" {
			m.problems[c.ID] = problem
		} else {
			delete(m.problems, c.ID)
		}
		m.mu.Unlock()
	}
	// A fan no longer controlled goes back to what it had.
	m.mu.Lock()
	var release []channel
	for id := range m.original {
		if !controlled[id] {
			if i := slices.IndexFunc(channels, func(ch channel) bool { return ch.id == id }); i >= 0 {
				release = append(release, channels[i])
			}
		}
	}
	m.mu.Unlock()
	for _, ch := range release {
		m.restore(ch)
	}
}

// drive takes a fan over (remembering its mode once) and sets its duty.
func (m *Manager) drive(ch channel, percent int) error {
	if m.Disabled {
		return nil
	}
	m.mu.Lock()
	_, taken := m.original[ch.id]
	if !taken {
		m.original[ch.id] = read(ch.enable)
		m.saveOriginal()
	}
	last, wrote := m.applied[ch.id]
	m.mu.Unlock()
	if read(ch.enable) != "1" {
		if err := os.WriteFile(ch.enable, []byte("1"), 0o644); err != nil {
			return fmt.Errorf("manual mode not accepted: %w", err)
		}
		wrote = false
	}
	value := int(math.Round(float64(percent) * 255 / 100))
	if wrote && last == value {
		return nil
	}
	if err := os.WriteFile(ch.pwm, []byte(strconv.Itoa(value)), 0o644); err != nil {
		return fmt.Errorf("speed not accepted: %w", err)
	}
	m.mu.Lock()
	m.applied[ch.id] = value
	m.mu.Unlock()
	return nil
}

// restore gives a fan back to the mode it had before TitanOS took it.
func (m *Manager) restore(ch channel) {
	m.mu.Lock()
	mode, ok := m.original[ch.id]
	delete(m.original, ch.id)
	delete(m.applied, ch.id)
	delete(m.problems, ch.id)
	m.saveOriginal()
	m.mu.Unlock()
	if !ok || m.Disabled {
		return
	}
	if mode == "" || mode == "1" {
		// It was manual before: leave it at full speed, the safe side.
		_ = os.WriteFile(ch.pwm, []byte("255"), 0o644) // full speed is the safe side; nothing else is left to try
		return
	}
	if err := os.WriteFile(ch.enable, []byte(mode), 0o644); err != nil {
		slog.Warn("fan not given back to the BIOS", "fan", ch.label, "err", err)
		_ = os.WriteFile(ch.pwm, []byte("255"), 0o644) // full speed is the safe side; nothing else is left to try
	}
}

// saveOriginal keeps the original modes in /run; called with mu held.
func (m *Manager) saveOriginal() {
	if data, err := json.Marshal(m.original); err == nil {
		_ = os.MkdirAll(filepath.Dir(m.runtime), 0o755) // a runtime or state file only; without it the default applies
		_ = os.WriteFile(m.runtime, data, 0o600)        // a runtime or state file only; without it the default applies
	}
}

// Run drives the fans every five seconds until ctx ends, then gives them
// back to the BIOS.
func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		m.apply()
		select {
		case <-ctx.Done():
			for _, ch := range m.discover() {
				m.mu.Lock()
				_, taken := m.original[ch.id]
				m.mu.Unlock()
				if taken {
					m.restore(ch)
				}
			}
			return
		case <-ticker.C:
		}
	}
}

// Fans lists the controllable fans with their state.
func (m *Manager) Fans() []Fan {
	m.mu.Lock()
	settings := m.settings
	problems := make(map[string]string, len(m.problems))
	for k, v := range m.problems {
		problems[k] = v
	}
	m.mu.Unlock()
	temps := m.Temperatures()
	out := []Fan{}
	for _, ch := range m.discover() {
		f := Fan{ID: ch.id, Chip: ch.chip, Label: ch.label, Control: Control{ID: ch.id, Mode: "bios", Min: 30}, Error: problems[ch.id]}
		if rpm, err := strconv.Atoi(read(ch.fanInput)); err == nil {
			f.RPM = &rpm
		}
		if duty, err := strconv.Atoi(read(ch.pwm)); err == nil {
			f.Percent = int(math.Round(float64(duty) * 100 / 255))
		}
		if i := slices.IndexFunc(settings.Fans, func(c Control) bool { return c.ID == ch.id }); i >= 0 {
			f.Control = settings.Fans[i]
			if f.Control.Mode == "curve" {
				if f.Control.Sensor == DiskSensor {
					if c, ok := m.DiskMax(); ok {
						f.SensorC = &c
					}
				} else if c, ok := temps[f.Control.Sensor]; ok {
					f.SensorC = &c
				}
			}
		}
		out = append(out, f)
	}
	return out
}
