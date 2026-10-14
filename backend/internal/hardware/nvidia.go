package hardware

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

const nvidiaConfig = "plugins/nvidia/settings.json"

var ErrPowerLimit = errors.New("hardware: unsupported NVIDIA power limit")

type NvidiaSettings struct {
	// 0 restores the board default; 20..100 is a percentage of that default.
	PowerPercent int `json:"powerPercent"`
}

type NvidiaControl struct {
	store    *config.Store
	devices  *Manager
	runner   run.Runner
	Disabled bool
	mu       sync.Mutex
	loaded   bool
	settings map[string]NvidiaSettings
	applied  map[string]bool
	problems map[string]string
}

func NewNvidiaControl(store *config.Store, devices *Manager, disabled bool) *NvidiaControl {
	return &NvidiaControl{store: store, devices: devices, runner: run.Exec{}, Disabled: disabled, settings: map[string]NvidiaSettings{}, applied: map[string]bool{}, problems: map[string]string{}}
}

func (n *NvidiaControl) Load() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	var settings map[string]NvidiaSettings
	if err := n.store.ReadJSON(nvidiaConfig, &settings); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, value := range settings {
		if !validPower(value.PowerPercent) {
			return ErrPowerLimit
		}
	}
	if settings != nil {
		n.settings = settings
	}
	n.loaded = true
	return nil
}

func validPower(percent int) bool { return percent == 0 || percent >= 20 && percent <= 100 }

func powerWatts(output string, percent int) (float64, error) {
	rows, err := csv.NewReader(strings.NewReader(strings.TrimSpace(output))).ReadAll()
	if err != nil || len(rows) != 1 || len(rows[0]) != 3 {
		return 0, ErrPowerLimit
	}
	var limits [3]float64
	for i, value := range rows[0] {
		v, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || !(v > 0 && v < 10000) {
			return 0, ErrPowerLimit
		}
		limits[i] = v
	}
	watts := limits[2]
	if percent != 0 {
		watts *= float64(percent) / 100
	}
	if !validPower(percent) || watts < limits[0] || watts > limits[1] {
		return 0, fmt.Errorf("%w: %.1f W outside %.1f–%.1f W", ErrPowerLimit, watts, limits[0], limits[1])
	}
	return watts, nil
}

func (n *NvidiaControl) apply(ctx context.Context, address string, settings NvidiaSettings) error {
	available := false
	for _, device := range n.devices.Status().PCI {
		if device.Address == address && device.Vendor == "10de" && device.Driver == "nvidia" {
			available = true
		}
	}
	if !available {
		return ErrUnknown
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := n.runner.Run(ctx, "", "nvidia-smi", "--id="+address, "--query-gpu=power.min_limit,power.max_limit,power.default_limit", "--format=csv,noheader,nounits")
	if err != nil {
		return err
	}
	watts, err := powerWatts(output, settings.PowerPercent)
	if err != nil {
		return err
	}
	_, err = n.runner.Run(ctx, "", "nvidia-smi", "--id="+address, "--power-limit="+strconv.FormatFloat(watts, 'f', 2, 64))
	return err
}

func (n *NvidiaControl) Set(ctx context.Context, address string, settings NvidiaSettings) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.loaded {
		return errors.New("hardware: settings could not be loaded")
	}
	if n.Disabled {
		return errors.New("hardware: disabled in development mode")
	}
	if !validPower(settings.PowerPercent) {
		return ErrPowerLimit
	}
	if err := n.apply(ctx, address, settings); err != nil {
		return err
	}
	before := n.settings[address]
	n.settings[address] = settings
	if err := n.store.WriteJSON(nvidiaConfig, n.settings); err != nil {
		n.settings[address] = before
		// Report both failures if the old setting cannot be restored.
		return errors.Join(err, n.apply(context.WithoutCancel(ctx), address, before))
	}
	n.applied[address] = true
	delete(n.problems, address)
	return nil
}

func (n *NvidiaControl) Status() map[string]any {
	n.mu.Lock()
	defer n.mu.Unlock()
	settings, problems := map[string]NvidiaSettings{}, map[string]string{}
	for key, value := range n.settings {
		settings[key] = value
	}
	for key, value := range n.problems {
		problems[key] = value
	}
	return map[string]any{"settings": settings, "problems": problems}
}

// A driver reload (including a VM returning its GPU) requires reapplying the
// limit. Values are cached in RAM; this loop never reads /config.
func (n *NvidiaControl) Run(ctx context.Context) {
	if n.Disabled {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		n.mu.Lock()
		present := map[string]bool{}
		for _, device := range n.devices.Status().PCI {
			present[device.Address] = device.Driver == "nvidia"
		}
		for address, settings := range n.settings {
			if !present[address] {
				delete(n.applied, address)
				continue
			}
			if n.applied[address] {
				continue
			}
			if err := n.apply(ctx, address, settings); err != nil {
				n.problems[address] = err.Error()
			} else {
				delete(n.problems, address)
			}
			n.applied[address] = true // failed attempts stay visible, no wake-up loop
		}
		n.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
