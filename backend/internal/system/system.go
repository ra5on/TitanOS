// Package system reads the state of the machine (CPU, memory, network) and
// carries out system actions such as a reboot.
package system

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Info is a snapshot of the machine for the dashboard.
type Info struct {
	Hostname      string        `json:"hostname"`
	Kernel        string        `json:"kernel"`
	UptimeSeconds int64         `json:"uptimeSeconds"`
	CPU           CPU           `json:"cpu"`
	Memory        Memory        `json:"memory"`
	Image         *Image        `json:"image"`
	Stick         Stick         `json:"stick"`
	Network       *Network      `json:"network"`
	Networks      []Network     `json:"networks"`
	Temperatures  []Temperature `json:"temperatures"`
	Inventory     Inventory     `json:"inventory"`
}

type CPU struct {
	Model        string        `json:"model"`
	Threads      int           `json:"threads"`
	UsagePercent float64       `json:"usagePercent"`
	ThreadUsage  []ThreadUsage `json:"threadUsage"`
}
type ThreadUsage struct {
	ID           string   `json:"id"`
	UsagePercent *float64 `json:"usagePercent"`
}

type Memory struct {
	TotalBytes     uint64          `json:"totalBytes"`
	UsedBytes      uint64          `json:"usedBytes"`
	FreeBytes      uint64          `json:"freeBytes"`
	AvailableBytes uint64          `json:"availableBytes"`
	CacheBytes     uint64          `json:"cacheBytes"`
	DirtyBytes     uint64          `json:"dirtyBytes"`
	WritebackBytes uint64          `json:"writebackBytes"`
	ReserveBytes   uint64          `json:"reserveBytes"`
	LowAvailable   bool            `json:"lowAvailable,omitempty"`
	Pressure       *MemoryPressure `json:"pressure,omitempty"`
}

// Image describes the system image in RAM. It is nil when TitanOS is not
// running from a stick (development).
type Image struct {
	Slot              string `json:"slot"`
	SizeBytes         uint64 `json:"sizeBytes"`
	OverlayUsedBytes  uint64 `json:"overlayUsedBytes"`
	OverlayTotalBytes uint64 `json:"overlayTotalBytes"`
}

type Stick struct {
	Label   string `json:"label"`
	Present bool   `json:"present"`
}

// Network describes the interface that holds the default route.
type Network struct {
	Interface        string   `json:"interface"`
	IPv4             string   `json:"ipv4"`
	IPv4Addresses    []string `json:"ipv4Addresses"`
	IPv6Addresses    []string `json:"ipv6Addresses"`
	Gateway6         string   `json:"gateway6"`
	MAC              string   `json:"mac"`
	MTU              int      `json:"mtu"`
	State            string   `json:"state"`
	Gateway          string   `json:"gateway"`
	SpeedMbps        int      `json:"speedMbps"`
	RxBytesPerSecond float64  `json:"rxBytesPerSecond"`
	TxBytesPerSecond float64  `json:"txBytesPerSecond"`
}

const (
	stickLabel     = "TITANOS"
	stickDevice    = "/dev/disk/by-label/" + stickLabel
	imageFile      = "/run/live/medium/filesystem.squashfs"
	overlayDir     = "/run/live/overlay"
	sampleInterval = 2 * time.Second
)

// Monitor samples the values that are rates (CPU usage, network throughput)
// in the background, so that a request gets them without waiting.
type Monitor struct {
	mu           sync.RWMutex
	cpu          float64
	rx, tx       float64
	network      string
	rates        map[string][2]float64
	temperatures []Temperature
	threadUsage  []ThreadUsage
	memory       Memory
}

// NewMonitor starts sampling until ctx is done.
func NewMonitor(ctx context.Context) *Monitor {
	m := &Monitor{memory: ReadMemory()}
	go m.run(ctx)
	return m
}

func (m *Monitor) run(ctx context.Context) {
	m.mu.Lock()
	m.temperatures = readTemperatures("/sys")
	m.mu.Unlock()
	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()

	prevCPU, _ := readCPUTimes()
	previousThreads := parseCPUThreads(readString("/proc/stat"))
	prevIface := defaultRoute().iface
	prevRx, prevTx := readNetBytes(prevIface)
	previous := networkCounters()
	prevTime := time.Now()
	var memoryWarning memoryWarning
	memoryWarning.observe(m.Memory(), prevTime)

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			seconds := now.Sub(prevTime).Seconds()
			prevTime = now

			cpu, err := readCPUTimes()
			currentThreads := parseCPUThreads(readString("/proc/stat"))
			threadUsage := sampleCPUThreads(previousThreads, currentThreads)
			previousThreads = currentThreads
			usage := 0.0
			if err == nil {
				usage = cpuUsage(prevCPU, cpu)
				prevCPU = cpu
			}

			iface := defaultRoute().iface
			rx, tx := readNetBytes(iface)
			var rxRate, txRate float64
			if iface == prevIface && seconds > 0 && rx >= prevRx && tx >= prevTx {
				rxRate = float64(rx-prevRx) / seconds
				txRate = float64(tx-prevTx) / seconds
			}
			prevIface, prevRx, prevTx = iface, rx, tx
			temperatures := readTemperatures("/sys")
			current := networkCounters()
			rates := make(map[string][2]float64)
			for name, counters := range current {
				if before, ok := previous[name]; ok && seconds > 0 && counters[0] >= before[0] && counters[1] >= before[1] {
					rates[name] = [2]float64{float64(counters[0]-before[0]) / seconds, float64(counters[1]-before[1]) / seconds}
				}
			}
			previous = current
			memory := ReadMemory()
			memory.LowAvailable = memoryWarning.observe(memory, now)

			m.mu.Lock()
			m.memory = memory
			m.rates = rates
			m.threadUsage = threadUsage
			m.cpu, m.rx, m.tx, m.network = usage, rxRate, txRate, iface
			m.temperatures = temperatures
			m.mu.Unlock()
		}
	}
}

// Info returns the current state of the machine.
func (m *Monitor) Info() Info {
	m.mu.RLock()
	usage, rx, tx := m.cpu, m.rx, m.tx
	temperatures := slices.Clone(m.temperatures)
	threads := slices.Clone(m.threadUsage)
	rates := m.rates
	sampledInterface := m.network
	m.mu.RUnlock()

	hostname, _ := os.Hostname()
	info := Info{
		Hostname:      hostname,
		Kernel:        readTrimmed("/proc/sys/kernel/osrelease"),
		UptimeSeconds: readUptime(),
		CPU:           CPU{Model: cpuModel(readString("/proc/cpuinfo")), Threads: runtime.NumCPU(), UsagePercent: usage, ThreadUsage: threads},
		Memory:        m.Memory(),
		Image:         readImage(),
		Stick:         Stick{Label: stickLabel, Present: exists(stickDevice)},
		Temperatures:  append([]Temperature{}, temperatures...),
		Inventory:     systemInventory(),
	}

	if route := defaultRoute(); route.iface != "" {
		if sampledInterface != route.iface {
			rx, tx = 0, 0
		}
		info.Network = &Network{
			Interface: route.iface,

			Gateway:          route.gateway,
			SpeedMbps:        readSpeed(route.iface),
			RxBytesPerSecond: rx,
			TxBytesPerSecond: tx,
		}
	}
	info.Networks = networkDetails(rates)
	for i := range info.Networks {
		if info.Network != nil && info.Networks[i].Interface == info.Network.Interface {
			info.Network = &info.Networks[i]
			break
		}
	}
	return info
}

func readString(path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}

func readTrimmed(path string) string {
	return strings.TrimSpace(readString(path))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readUptime() int64 {
	fields := strings.Fields(readString("/proc/uptime"))
	if len(fields) == 0 {
		return 0
	}
	seconds, _ := strconv.ParseFloat(fields[0], 64)
	return int64(seconds)
}

func cpuModel(cpuinfo string) string {
	for line := range strings.SplitSeq(cpuinfo, "\n") {
		if name, value, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(name) == "model name" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// Used includes all occupied RAM, including caches, kernel memory and tmpfs.
// Available is the kernel's separate estimate of capacity for new workloads.
// Do not add the RAM image, VM allocations or Shmem: they are already counted.
func ReadMemory() Memory {
	mem := parseMeminfo(readString("/proc/meminfo"))
	mem.ReserveBytes = memoryReserve(mem.TotalBytes)
	mem.Pressure = parseMemoryPressure(readString("/proc/pressure/memory"))
	return mem
}

// Memory returns the shared sample, including the server-owned warning state.
func (m *Monitor) Memory() Memory {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.memory
}

func parseMeminfo(meminfo string) Memory {
	values := map[string]uint64{}
	for line := range strings.SplitSeq(meminfo, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		kib, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil || kib > ^uint64(0)/1024 {
			continue
		}
		values[name] = kib * 1024
	}
	total := values["MemTotal"]
	free := min(values["MemFree"], total)
	available, ok := values["MemAvailable"]
	if !ok {
		available = free
	}
	cache := values["Cached"] - min(values["Cached"], values["Shmem"])
	cache = min(total-free, cache)
	for _, extra := range []uint64{values["Buffers"], values["SReclaimable"]} {
		cache += min(total-free-cache, extra)
	}
	return Memory{TotalBytes: total, UsedBytes: total - free, FreeBytes: free, AvailableBytes: min(available, total), CacheBytes: cache, DirtyBytes: values["Dirty"], WritebackBytes: values["Writeback"]}
}

type cpuTimes struct {
	idle, total uint64
}

func readCPUTimes() (cpuTimes, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuTimes{}, err
	}
	line, _, _ := strings.Cut(string(data), "\n")
	return parseCPULine(line), nil
}

// parseCPULine parses the first line of /proc/stat:
// cpu user nice system idle iowait irq softirq steal guest guest_nice
func parseCPULine(line string) cpuTimes {
	fields := strings.Fields(line)
	var t cpuTimes
	for i, field := range fields {
		if i == 0 || i > 8 { // skip the label; guest time is already part of user
			continue
		}
		value, _ := strconv.ParseUint(field, 10, 64)
		t.total += value
		if i == 4 || i == 5 { // idle, iowait
			t.idle += value
		}
	}
	return t
}

func cpuUsage(prev, cur cpuTimes) float64 {
	total := cur.total - prev.total
	if cur.total <= prev.total || total == 0 {
		return 0
	}
	idle := cur.idle - prev.idle
	if idle > total {
		idle = total
	}
	return 100 * float64(total-idle) / float64(total)
}

type route struct {
	iface, gateway string
}

func defaultRoute() route {
	r := parseDefaultRoute(readString("/proc/net/route"))
	if r.iface == "" {
		r = parseDefaultRoute6(readString("/proc/net/ipv6_route"), "")
	}
	return r
}

// parseDefaultRoute finds the default route with the lowest metric in
// /proc/net/route. Addresses there are hexadecimal in host byte order.
func parseDefaultRoute(table string) route {
	var best route
	bestMetric := -1
	for i, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		if i == 0 || len(fields) < 8 || fields[1] != "00000000" || fields[7] != "00000000" {
			continue
		}
		metric, _ := strconv.Atoi(fields[6])
		if bestMetric != -1 && metric >= bestMetric {
			continue
		}
		gateway := ""
		if raw, err := strconv.ParseUint(fields[2], 16, 32); err == nil && raw != 0 {
			gateway = net.IPv4(byte(raw), byte(raw>>8), byte(raw>>16), byte(raw>>24)).String()
		}
		best, bestMetric = route{iface: fields[0], gateway: gateway}, metric
	}
	return best
}

// IPv6 routes use network-byte-order hex and a hexadecimal metric. Only
// usable, unicast, non-source-specific defaults are candidates.
func parseDefaultRoute6(table, iface string) route {
	var best route
	bestMetric := uint64(^uint32(0)) + 1
	for _, line := range strings.Split(table, "\n") {
		f := strings.Fields(line)
		if len(f) != 10 || f[0] != strings.Repeat("0", 32) || f[1] != "00" || f[3] != "00" || f[9] == "lo" || (iface != "" && f[9] != iface) {
			continue
		}
		metric, e1 := strconv.ParseUint(f[5], 16, 32)
		flags, e2 := strconv.ParseUint(f[8], 16, 32)
		raw, e3 := hex.DecodeString(f[4])
		if e1 != nil || e2 != nil || e3 != nil || len(raw) != 16 || flags&1 == 0 || flags&0x200 != 0 || metric >= bestMetric {
			continue
		}
		gateway := ""
		if ip := net.IP(raw); !ip.IsUnspecified() {
			gateway = ip.String()
			if ip.IsLinkLocalUnicast() {
				gateway += "%" + f[9]
			}
		}
		best, bestMetric = route{f[9], gateway}, metric
	}
	return best
}

func interfaceAddresses(addrs []net.Addr) (v4, v6 []string) {
	v4, v6 = []string{}, []string{}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsUnspecified() {
			if ipnet.IP.To4() != nil {
				v4 = append(v4, ipnet.String())
			} else {
				v6 = append(v6, ipnet.String())
			}
		}
	}
	slices.Sort(v4)
	slices.Sort(v6)
	return slices.Compact(v4), slices.Compact(v6)
}

func networkCounters() map[string][2]uint64 {
	result := make(map[string][2]uint64)
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		rx, tx := readNetBytes(iface.Name)
		result[iface.Name] = [2]uint64{rx, tx}
	}
	return result
}

func networkDetails(rates map[string][2]float64) []Network {
	result := []Network{}
	interfaces, _ := net.Interfaces()
	ipv4Route := parseDefaultRoute(readString("/proc/net/route"))
	ipv6Routes := readString("/proc/net/ipv6_route")
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		v4, v6 := interfaceAddresses(addrs)
		n := Network{Interface: iface.Name, IPv4Addresses: v4, IPv6Addresses: v6,
			MAC: iface.HardwareAddr.String(), MTU: iface.MTU, SpeedMbps: readSpeed(iface.Name),
			State:            readTrimmed(fmt.Sprintf("/sys/class/net/%s/operstate", iface.Name)),
			RxBytesPerSecond: rates[iface.Name][0], TxBytesPerSecond: rates[iface.Name][1],
			Gateway6: parseDefaultRoute6(ipv6Routes, iface.Name).gateway}
		if len(v4) > 0 {
			n.IPv4 = v4[0]
		}
		if ipv4Route.iface == iface.Name {
			n.Gateway = ipv4Route.gateway
		}
		result = append(result, n)
	}
	slices.SortFunc(result, func(a, b Network) int { return strings.Compare(a.Interface, b.Interface) })
	return result
}

func readNetBytes(iface string) (rx, tx uint64) {
	if iface == "" {
		return 0, 0
	}
	base := "/sys/class/net/" + iface + "/statistics/"
	rx, _ = strconv.ParseUint(readTrimmed(base+"rx_bytes"), 10, 64)
	tx, _ = strconv.ParseUint(readTrimmed(base+"tx_bytes"), 10, 64)
	return rx, tx
}

func readSpeed(iface string) int {
	return readLinkSpeed("/sys/class/net", iface)
}

func readImage() *Image {
	stat, err := os.Stat(imageFile)
	if err != nil {
		return nil
	}
	image := &Image{Slot: parseSlot(readString("/proc/cmdline")), SizeBytes: uint64(stat.Size())}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(overlayDir, &fs); err == nil {
		image.OverlayTotalBytes = fs.Blocks * uint64(fs.Bsize)
		image.OverlayUsedBytes = (fs.Blocks - fs.Bfree) * uint64(fs.Bsize)
	}
	return image
}

// BootSlot returns the slot of the stick this system was started from, or
// "" if it does not run from a stick (development).
func BootSlot() string {
	return parseSlot(readString("/proc/cmdline"))
}

// parseSlot returns the system slot ("a" or "b") the kernel command line
// booted from: live-media-path=/titanos/<slot>.
func parseSlot(cmdline string) string {
	for arg := range strings.FieldsSeq(cmdline) {
		if path, ok := strings.CutPrefix(arg, "live-media-path=/titanos/"); ok {
			return path
		}
	}
	return ""
}
