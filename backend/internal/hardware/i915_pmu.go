package hardware

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// i915 exposes per-engine busy time through perf, including intervals without
// an open userspace DRM client. Discover event IDs from sysfs instead of
// hard-coding engine numbers. Busy events do not force the GPU out of RC6.
// ABI: Linux drivers/gpu/drm/i915/i915_pmu.c and perf_event_open(2).
type i915PMU struct {
	sys     string
	open    func(uint32, uint64, int) (io.ReadCloser, error)
	devices map[string]*pmuDevice
}

type pmuDevice struct {
	identity string
	engines  []pmuEngine
}

type pmuEngine struct {
	name     string
	file     io.ReadCloser
	previous *perfReading
}

type perfReading struct{ value, enabled, running uint64 }

func openPerfCounter(kind uint32, config uint64, cpu int) (io.ReadCloser, error) {
	attr := unix.PerfEventAttr{
		Type: kind, Size: unix.PERF_ATTR_SIZE_VER0, Config: config,
		Read_format: unix.PERF_FORMAT_TOTAL_TIME_ENABLED | unix.PERF_FORMAT_TOTAL_TIME_RUNNING,
	}
	fd, err := unix.PerfEventOpen(&attr, -1, cpu, -1, unix.PERF_FLAG_FD_CLOEXEC)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), "i915-busy"), nil
}

func (m *i915PMU) retain(addresses map[string]bool) {
	if m == nil {
		return
	}
	for address := range m.devices {
		if !addresses[address] {
			m.drop(address)
		}
	}
}

func (m *i915PMU) drop(address string) {
	if device := m.devices[address]; device != nil {
		for _, engine := range device.engines {
			engine.file.Close()
		}
		delete(m.devices, address)
	}
}

func (m *i915PMU) sample(address string) []Engine {
	if m == nil {
		return nil
	}
	name := "i915_" + strings.ReplaceAll(address, ":", "_")
	if address == "0000:00:02.0" {
		name = "i915"
	}
	root := filepath.Join(m.sys, "bus/event_source/devices", name)
	read := func(file string) string {
		data, _ := os.ReadFile(filepath.Join(root, file))
		return strings.TrimSpace(string(data))
	}
	kind, err := strconv.ParseUint(read("type"), 10, 32)
	if err != nil {
		m.drop(address)
		return nil
	}
	// cpumask is a CPU list, e.g. "0" or "0-3,8".
	first := strings.FieldsFunc(read("cpumask"), func(r rune) bool { return r == ',' || r == '-' })
	if len(first) == 0 {
		m.drop(address)
		return nil
	}
	cpu, err := strconv.Atoi(first[0])
	if err != nil || cpu < 0 {
		m.drop(address)
		return nil
	}
	identity := fmt.Sprintf("%d:%d", kind, cpu)
	device := m.devices[address]
	if device != nil && device.identity != identity {
		m.drop(address)
		device = nil
	}
	if device == nil {
		device = &pmuDevice{identity: identity}
		paths, _ := filepath.Glob(filepath.Join(root, "events/*-busy"))
		if len(paths) == 0 || len(paths) > 64 {
			return nil
		}
		for _, path := range paths {
			data, err := os.ReadFile(path)
			configText, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "config=")
			config, parseErr := strconv.ParseUint(configText, 0, 64)
			if err != nil || !ok || parseErr != nil {
				for _, opened := range device.engines {
					opened.file.Close()
				}
				return nil
			}
			file, err := m.open(uint32(kind), config, cpu)
			if err != nil {
				// An incomplete set must not appear as a fully measured GPU.
				for _, opened := range device.engines {
					opened.file.Close()
				}
				return nil
			}
			device.engines = append(device.engines, pmuEngine{name: strings.TrimSuffix(filepath.Base(path), "-busy"), file: file})
		}
		if m.devices == nil {
			m.devices = map[string]*pmuDevice{}
		}
		m.devices[address] = device
	}
	result := []Engine{}
	for i := range device.engines {
		engine := &device.engines[i]
		var data [24]byte
		if n, err := engine.file.Read(data[:]); err != nil || n != len(data) {
			m.drop(address)
			return nil
		}
		current := perfReading{binary.NativeEndian.Uint64(data[:8]), binary.NativeEndian.Uint64(data[8:16]), binary.NativeEndian.Uint64(data[16:])}
		if percent, ok := perfUsage(engine.previous, current); ok {
			result = append(result, Engine{Name: engine.name, UsagePercent: percent})
		}
		engine.previous = &current
	}
	return result
}

func perfUsage(before *perfReading, after perfReading) (float64, bool) {
	if before == nil || after.value < before.value || after.enabled <= before.enabled || after.running <= before.running {
		return 0, false
	}
	enabled, running := after.enabled-before.enabled, after.running-before.running
	if running > enabled || enabled > 15e9 {
		return 0, false
	}
	// Scale for multiplexing: busy * enabled/running / enabled.
	return min(100, float64(after.value-before.value)/float64(running)*100), true
}
