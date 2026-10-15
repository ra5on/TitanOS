package system

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const energyFile = "system/energy.json"

var ErrEnergyChoice = errors.New("system: energy setting is not supported")

type EnergySettings struct {
	Governor   string `json:"governor"`
	Preference string `json:"preference"`
	Platform   string `json:"platform"`
}

type EnergyControl struct {
	Key     string   `json:"key"`
	Choices []string `json:"choices"`
	Current string   `json:"current"`
	Mixed   bool     `json:"mixed"`
	paths   []string
}

type EnergyStatus struct {
	Settings EnergySettings  `json:"settings"`
	Controls []EnergyControl `json:"controls"`
	Drivers  []string        `json:"drivers"`
	ReadOnly bool            `json:"readOnly"`
	Error    string          `json:"error,omitempty"`
}

// EnergyManager changes only profiles advertised by the kernel. It never
// writes sysfs in development mode or reads /config during status polling.
type EnergyManager struct {
	store    *config.Store
	sys      string
	Disabled bool
	mu       sync.Mutex
	settings EnergySettings
	loaded   bool
	problem  string
	write    func(string, string) error
}

func NewEnergyManager(store *config.Store) *EnergyManager {
	return &EnergyManager{store: store, sys: "/sys", write: func(path, value string) error {
		// Do not create files if a CPU policy disappears (e.g. hot unplug).
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
		if err != nil {
			return err
		}
		_, err = file.WriteString(value + "\n")
		return errors.Join(err, file.Close())
	}}
}

func energyRead(path string) string {
	data, _ := os.ReadFile(path)
	return strings.TrimSpace(string(data))
}

var energyValue = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,40}$`)

func (m *EnergyManager) discover() EnergyStatus {
	s := EnergyStatus{Settings: m.settings, Controls: []EnergyControl{}, Drivers: []string{}, ReadOnly: m.Disabled, Error: m.problem}
	policies, _ := filepath.Glob(filepath.Join(m.sys, "devices/system/cpu/cpufreq/policy[0-9]*"))
	for _, policy := range policies {
		if driver := energyRead(filepath.Join(policy, "scaling_driver")); driver != "" && !slices.Contains(s.Drivers, driver) {
			s.Drivers = append(s.Drivers, driver)
		}
	}
	add := func(key string, roots []string, options, current string) {
		control := EnergyControl{Key: key}
		for i, root := range roots {
			choices := strings.Fields(energyRead(filepath.Join(root, options)))
			choices = slices.DeleteFunc(choices, func(v string) bool { return !energyValue.MatchString(v) || v == "userspace" })
			value := energyRead(filepath.Join(root, current))
			if len(choices) == 0 || value == "" {
				return
			}
			if i == 0 {
				control.Choices, control.Current = choices, value
			} else {
				control.Choices = slices.DeleteFunc(control.Choices, func(v string) bool { return !slices.Contains(choices, v) })
				if control.Current != value {
					control.Mixed = true
				}
			}
			control.paths = append(control.paths, filepath.Join(root, current))
		}
		if len(control.Choices) > 0 {
			s.Controls = append(s.Controls, control)
		}
	}
	add("governor", policies, "scaling_available_governors", "scaling_governor")
	add("preference", policies, "energy_performance_available_preferences", "energy_performance_preference")
	add("platform", []string{filepath.Join(m.sys, "firmware/acpi")}, "platform_profile_choices", "platform_profile")
	return s
}

func (m *EnergyManager) Status() EnergyStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.discover()
}

func (m *EnergyManager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var settings EnergySettings
	if err := m.store.ReadJSON(energyFile, &settings); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.loaded, m.settings = true, settings
	_, err := m.apply(settings)
	if err != nil {
		m.problem = err.Error()
	}
	return err
}

func (m *EnergyManager) apply(settings EnergySettings) (func() error, error) {
	controls := m.discover().Controls
	wanted := map[string]string{"governor": settings.Governor, "preference": settings.Preference, "platform": settings.Platform}
	for key, value := range wanted {
		if value == "" {
			continue
		}
		index := slices.IndexFunc(controls, func(c EnergyControl) bool { return c.Key == key })
		if index < 0 || !slices.Contains(controls[index].Choices, value) {
			return nil, fmt.Errorf("%w: %s=%s", ErrEnergyChoice, key, value)
		}
	}
	if m.Disabled {
		return func() error { return nil }, nil
	}
	// Snapshot every advertised control: changing the governor can also change
	// its energy preference. Restore governors before preferences on failure.
	old := map[string]string{}
	for _, c := range controls {
		for _, path := range c.paths {
			old[path] = energyRead(path)
		}
	}
	restore := func() error {
		var failures []error
		for _, c := range controls {
			for _, path := range c.paths {
				if energyRead(path) != old[path] {
					failures = append(failures, m.write(path, old[path]))
				}
			}
		}
		return errors.Join(failures...)
	}
	for _, c := range controls {
		value := wanted[c.Key]
		if value == "" {
			continue
		}
		for _, path := range c.paths {
			if energyRead(path) == value {
				continue
			}
			if err := m.write(path, value); err != nil {
				return nil, errors.Join(err, restore())
			}
			if energyRead(path) != value {
				return nil, errors.Join(fmt.Errorf("energy setting was not accepted: %s", c.Key), restore())
			}
		}
	}
	return restore, nil
}

func (m *EnergyManager) Set(settings EnergySettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return errors.New("energy settings were not loaded")
	}
	if m.Disabled {
		return errors.New("energy settings are read-only in development mode")
	}
	restore, err := m.apply(settings)
	if err != nil {
		return err
	}
	if err := m.store.WriteJSON(energyFile, settings); err != nil {
		return errors.Join(err, restore())
	}
	m.settings, m.problem = settings, ""
	return nil
}
