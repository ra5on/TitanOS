package system

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Temperature is an actual hwmon channel. Labels belong to the driver;
// an unlabelled motherboard input must not be guessed to be a CPU sensor.
type Temperature struct {
	ID        string   `json:"id"`
	Chip      string   `json:"chip"`
	Label     string   `json:"label"`
	Device    string   `json:"device"`
	Celsius   *float64 `json:"celsius"`
	CriticalC *float64 `json:"criticalC"`
}

func temperatureValue(path string) *float64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	milli, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || milli < -273150 || milli > 1000000 {
		return nil
	}
	value := float64(milli) / 1000
	return &value
}

func readTemperatures(sys string) []Temperature {
	result := []Temperature{}
	paths, _ := filepath.Glob(filepath.Join(sys, "class/hwmon/hwmon*"))
	for _, path := range paths {
		chip := readTrimmed(filepath.Join(path, "name"))
		// Disk temperatures already come from the storage monitor. Reading
		// their hwmon channels can issue I/O and defeat disk standby/APST.
		if chip == "" || chip == "nvme" || chip == "drivetemp" {
			continue
		}
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			continue
		}
		device := filepath.Dir(real)
		if filepath.Base(device) == "hwmon" {
			device = filepath.Dir(device)
		}
		// Some drivers expose the device via a link next to their channels.
		if linked, err := filepath.EvalSymlinks(filepath.Join(path, "device")); err == nil {
			device = linked
		}
		relative, err := filepath.Rel(sys, device)
		if err != nil || relative == ".." || strings.HasPrefix(relative, "../") {
			continue
		}
		readable := true
		for parent := device; parent != sys && strings.HasPrefix(parent, sys+"/"); parent = filepath.Dir(parent) {
			if readTrimmed(filepath.Join(parent, "power/runtime_status")) == "suspended" {
				readable = false
				break
			}
			if driver, _ := os.Readlink(filepath.Join(parent, "driver")); filepath.Base(driver) == "vfio-pci" {
				readable = false
				break
			}
		}
		inputs, _ := filepath.Glob(filepath.Join(path, "temp*_input"))
		for _, input := range inputs {
			channel := strings.TrimSuffix(filepath.Base(input), "_input")
			if _, err := strconv.ParseUint(strings.TrimPrefix(channel, "temp"), 10, 32); err != nil {
				continue
			}
			// hwmon indices change at boot; the physical device and channel do not.
			digest := sha256.Sum256([]byte(relative + "\x00" + chip + "\x00" + channel))
			label := readTrimmed(filepath.Join(path, channel+"_label"))
			if label == "" {
				label = channel
			}
			sensor := Temperature{ID: hex.EncodeToString(digest[:16]), Chip: chip, Label: label, Device: relative}
			if readable {
				sensor.CriticalC = temperatureValue(filepath.Join(path, channel+"_crit"))
				if readTrimmed(filepath.Join(path, channel+"_fault")) != "1" && readTrimmed(filepath.Join(path, channel+"_enable")) != "0" {
					sensor.Celsius = temperatureValue(input)
				}
			}
			result = append(result, sensor)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Chip != result[j].Chip {
			return result[i].Chip < result[j].Chip
		}
		if result[i].Device != result[j].Device {
			return result[i].Device < result[j].Device
		}
		return result[i].Label < result[j].Label
	})
	return result
}
