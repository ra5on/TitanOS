package hardware

import (
	"context"
	"encoding/csv"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/run"
)

// Accelerator contains measured values only. A nil measurement means that
// the device/driver does not expose it; it must never be rendered as zero.
type Accelerator struct {
	Address          string     `json:"address"`
	Name             string     `json:"name"`
	Vendor           string     `json:"vendor"`
	Kind             string     `json:"kind"`
	Driver           string     `json:"driver"`
	State            string     `json:"state"`
	UsagePercent     *float64   `json:"usagePercent"`
	TemperatureC     *float64   `json:"temperatureC"`
	PowerWatts       *float64   `json:"powerWatts"`
	MemoryUsedBytes  *float64   `json:"memoryUsedBytes"`
	MemoryTotalBytes *float64   `json:"memoryTotalBytes"`
	FrequencyMHz     *float64   `json:"frequencyMHz"`
	PState           string     `json:"pState"`
	Engines          []Engine   `json:"engines"`
	History          []*float64 `json:"history"`
}

type Engine struct {
	Name         string  `json:"name"`
	UsagePercent float64 `json:"usagePercent"`
}

type AcceleratorMonitor struct {
	hardware *Manager
	proc     string
	runner   run.Runner
	names    map[string]string
	mu       sync.RWMutex
	devices  []Accelerator
	previous map[string]drmClient
	busy     map[string]uint64
	last     time.Time
}

func NewAcceleratorMonitor(ctx context.Context, hardware *Manager) *AcceleratorMonitor {
	m := &AcceleratorMonitor{hardware: hardware, proc: "/proc", runner: run.Exec{}, names: readPCINames("/usr/share/misc/pci.ids")}
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			m.sample(ctx, time.Now())
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return m
}

func (m *AcceleratorMonitor) Snapshot() []Accelerator {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := append([]Accelerator{}, m.devices...)
	for i := range result {
		result[i].History = slices.Clone(result[i].History)
		result[i].Engines = slices.Clone(result[i].Engines)
	}
	return result
}

func readPCINames(path string) map[string]string {
	data, _ := os.ReadFile(path)
	names := map[string]string{}
	vendor := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "\t\t") || strings.HasPrefix(line, "#") || len(line) < 6 {
			continue
		}
		if line[0] != '\t' {
			if _, err := strconv.ParseUint(line[:4], 16, 16); err == nil {
				vendor = line[:4]
			} else {
				vendor = ""
			}
		} else if vendor != "" && len(line) > 7 {
			if _, err := strconv.ParseUint(line[1:5], 16, 16); err == nil {
				names[vendor+":"+line[1:5]] = strings.TrimSpace(line[5:])
			}
		}
	}
	return names
}

func number(value string) *float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return nil
	}
	return &n
}

func metric(path string, scale float64) *float64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	n := number(string(data))
	if n != nil {
		*n *= scale
	}
	return n
}

func (m *AcceleratorMonitor) sample(ctx context.Context, now time.Time) {
	devices := []Accelerator{}
	nvidiaDevices := []string{}
	previous := m.Snapshot()
	seconds := now.Sub(m.last).Seconds()
	pciDevices := m.hardware.Status().PCI
	clients, complete := map[string]drmClient{}, true
	if slices.ContainsFunc(pciDevices, func(p PCI) bool { return p.Driver == "i915" || p.Driver == "xe" || p.Driver == "amdgpu" }) {
		clients, complete = readDRMClients(m.proc)
	}
	nextBusy := map[string]uint64{}
	for _, pci := range pciDevices {
		if !slices.Contains([]string{"8086", "1002", "1022", "10de"}, pci.Vendor) {
			continue
		}
		d := Accelerator{Address: pci.Address, Name: m.names[pci.Vendor+":"+pci.Device], Vendor: pci.VendorName, Kind: pci.Kind, Driver: pci.Driver, State: "ready", Engines: []Engine{}, History: []*float64{}}
		if d.Name == "" {
			d.Name = pci.VendorName + " " + strings.ToUpper(pci.Device)
		}
		root := filepath.Join(m.hardware.sys, "bus/pci/devices", pci.Address)
		switch {
		case pci.Driver == "vfio-pci":
			d.State = "vm"
		case pci.Driver == "":
			d.State = "driver_missing"
		case pci.Vendor == "10de" && pci.Driver != "nvidia":
			d.State = "driver_missing"
		default:
			if m.hardware.read(root, "power/runtime_status") == "suspended" {
				d.State = "suspended"
			}
			if pci.Vendor == "10de" && d.State != "suspended" {
				nvidiaDevices = append(nvidiaDevices, pci.Address)
			}
			if d.State == "suspended" {
				break
			}
			d.UsagePercent = metric(filepath.Join(root, "gpu_busy_percent"), 1)
			d.MemoryUsedBytes = metric(filepath.Join(root, "mem_info_vram_used"), 1)
			d.MemoryTotalBytes = metric(filepath.Join(root, "mem_info_vram_total"), 1)
			if pci.Kind == "npu" {
				data, err := os.ReadFile(filepath.Join(root, "npu_busy_time_us"))
				if busy, parseErr := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil && parseErr == nil {
					nextBusy[pci.Address] = busy
					if old, ok := m.busy[pci.Address]; ok && busy >= old && seconds > 0 && seconds <= 15 {
						usage := min(100, float64(busy-old)/seconds/10000)
						d.UsagePercent = &usage
					}
				}
				d.MemoryUsedBytes = metric(filepath.Join(root, "npu_memory_utilization"), 1)
				for _, path := range []string{"freq/current_freq", "npu_current_frequency_mhz"} {
					if d.FrequencyMHz == nil {
						d.FrequencyMHz = metric(filepath.Join(root, path), 1)
					}
				}
			}
			if complete && seconds > 0 && seconds <= 15 {
				d.Engines = drmUsage(pci.Address, clients, m.previous, seconds)
				if d.UsagePercent == nil && len(d.Engines) > 0 {
					usage := 0.0
					for _, e := range d.Engines {
						usage = max(usage, e.UsagePercent)
					}
					d.UsagePercent = &usage
				}
			}
			// hwmon files belong to this PCI device, never a CPU/package sensor.
			hwmons, _ := filepath.Glob(filepath.Join(root, "hwmon/hwmon*"))
			for _, hwmon := range hwmons {
				if d.TemperatureC == nil {
					d.TemperatureC = metric(filepath.Join(hwmon, "temp1_input"), 0.001)
				}
				if d.PowerWatts == nil {
					d.PowerWatts = metric(filepath.Join(hwmon, "power1_average"), 0.000001)
				}
				if d.PowerWatts == nil {
					d.PowerWatts = metric(filepath.Join(hwmon, "power1_input"), 0.000001)
				}
			}
			if d.UsagePercent != nil {
				*d.UsagePercent = min(100, *d.UsagePercent)
			}
		}
		devices = append(devices, d)
	}
	if len(nvidiaDevices) > 0 {
		queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		for _, address := range nvidiaDevices {
			if queryCtx.Err() != nil {
				break
			}
			// An unrestricted query would also inspect suspended GPUs when
			// another card is active. Select each eligible device explicitly.
			output, err := m.runner.Run(queryCtx, "", "nvidia-smi", "--id="+address, "--query-gpu=pci.bus_id,name,utilization.gpu,temperature.gpu,power.draw,memory.used,memory.total,pstate,clocks.current.graphics,utilization.encoder,utilization.decoder", "--format=csv,noheader,nounits")
			if err == nil {
				applyNVIDIA(devices, output)
			}
		}
		cancel()
	}
	for i := range devices {
		for _, old := range previous {
			if old.Address == devices[i].Address && old.Driver == devices[i].Driver {
				devices[i].History = slices.Clone(old.History)
				break
			}
		}
		devices[i].History = append(devices[i].History, devices[i].UsagePercent)
		if len(devices[i].History) > 36 {
			devices[i].History = devices[i].History[len(devices[i].History)-36:]
		}
	}
	if !complete {
		clients = nil
	} // an incomplete scan cannot be the next baseline
	m.previous, m.busy, m.last = clients, nextBusy, now
	m.mu.Lock()
	m.devices = devices
	m.mu.Unlock()
}

func applyNVIDIA(devices []Accelerator, output string) {
	reader := csv.NewReader(strings.NewReader(output))
	reader.TrimLeadingSpace = true
	for {
		row, err := reader.Read()
		if err == io.EOF {
			return
		}
		if err != nil {
			return
		}
		if len(row) != 11 {
			continue
		}
		address := strings.ToLower(strings.TrimSpace(row[0]))
		parts := strings.Split(address, ":")
		if len(parts) == 3 && len(parts[0]) == 8 && strings.HasPrefix(parts[0], "0000") {
			address = address[4:]
		}
		for i := range devices {
			d := &devices[i]
			if d.Address != address || d.Driver != "nvidia" || d.State != "ready" {
				continue
			}
			d.Name = strings.TrimSpace(row[1])
			d.UsagePercent = number(row[2])
			d.TemperatureC = number(row[3])
			d.PowerWatts = number(row[4])
			d.MemoryUsedBytes = number(row[5])
			if d.MemoryUsedBytes != nil {
				*d.MemoryUsedBytes *= 1024 * 1024
			}
			d.MemoryTotalBytes = number(row[6])
			if d.MemoryTotalBytes != nil {
				*d.MemoryTotalBytes *= 1024 * 1024
			}
			d.PState = strings.TrimSpace(row[7])
			d.FrequencyMHz = number(row[8])
			if d.UsagePercent != nil {
				*d.UsagePercent = min(100, *d.UsagePercent)
			}
			for n, name := range []string{"encode", "decode"} {
				if value := number(row[9+n]); value != nil {
					d.Engines = append(d.Engines, Engine{Name: name, UsagePercent: min(100, *value)})
				}
			}
		}
	}
}

type drmClient struct {
	address                                 string
	counters, capacity, cycles, totalCycles map[string]uint64
}

func parseDRMClient(data string) (string, drmClient) {
	client := drmClient{counters: map[string]uint64{}, capacity: map[string]uint64{}, cycles: map[string]uint64{}, totalCycles: map[string]uint64{}}
	id := ""
	for _, line := range strings.Split(data, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if key == "drm-pdev" {
			client.address = value
			continue
		}
		if key == "drm-client-id" {
			id = value
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		if name, ok := strings.CutPrefix(key, "drm-engine-capacity-"); ok && n > 0 {
			client.capacity[name] = n
		} else if name, ok := strings.CutPrefix(key, "drm-engine-"); ok && len(fields) == 2 && fields[1] == "ns" {
			client.counters[name] = n
		} else if name, ok := strings.CutPrefix(key, "drm-cycles-"); ok {
			client.cycles[name] = n
		} else if name, ok := strings.CutPrefix(key, "drm-total-cycles-"); ok {
			client.totalCycles[name] = n
		}
	}
	if id == "" || client.address == "" {
		return "", client
	}
	return client.address + ":" + id, client
}

func readDRMClients(proc string) (map[string]drmClient, bool) {
	clients := map[string]drmClient{}
	pids, err := os.ReadDir(proc)
	if err != nil {
		return clients, false
	}
	count := 0
	for _, pid := range pids {
		if _, err := strconv.ParseUint(pid.Name(), 10, 32); err != nil {
			continue
		}
		folder := filepath.Join(proc, pid.Name(), "fdinfo")
		files, err := os.ReadDir(folder)
		if errors.Is(err, os.ErrPermission) {
			return clients, false
		}
		for _, file := range files {
			count++
			if count > 20000 {
				return clients, false
			}
			data, err := os.ReadFile(filepath.Join(folder, file.Name()))
			if errors.Is(err, os.ErrPermission) {
				return clients, false
			}
			if err != nil || len(data) > 65536 {
				continue
			}
			id, client := parseDRMClient(string(data))
			if id != "" {
				clients[id] = client
			}
		}
	}
	return clients, true
}

func drmUsage(address string, current, previous map[string]drmClient, seconds float64) []Engine {
	usage := map[string]float64{}
	if seconds <= 0 {
		return []Engine{}
	}
	for id, client := range current {
		if client.address != address {
			continue
		}
		old, ok := previous[id]
		if !ok {
			continue
		}
		for name, busy := range client.counters {
			before, ok := old.counters[name]
			if !ok {
				continue
			}
			if busy < before {
				client.counters[name] = before
				continue
			}
			capacity := max(uint64(1), client.capacity[name])
			usage[name] += float64(busy-before) / seconds / 1e9 / float64(capacity) * 100
		}
		for name, busy := range client.cycles {
			if _, ns := client.counters[name]; ns {
				continue
			}
			before, ok := old.cycles[name]
			total, totalOK := client.totalCycles[name]
			beforeTotal, oldTotalOK := old.totalCycles[name]
			if !ok || !totalOK || !oldTotalOK {
				continue
			}
			if busy < before || total < beforeTotal {
				client.cycles[name] = before
				client.totalCycles[name] = beforeTotal
				continue
			}
			if total == beforeTotal {
				continue
			}
			capacity := max(uint64(1), client.capacity[name])
			usage[name] += float64(busy-before) / float64(total-beforeTotal) / float64(capacity) * 100
		}
	}
	engines := []Engine{}
	for name, percent := range usage {
		engines = append(engines, Engine{Name: name, UsagePercent: min(100, percent)})
	}
	sort.Slice(engines, func(i, j int) bool { return engines[i].Name < engines[j].Name })
	return engines
}
