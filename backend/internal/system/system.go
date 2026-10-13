// Package system reads the state of the machine (CPU, memory, network) and
// carries out system actions such as a reboot.
package system

import (
	"context"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Info is a snapshot of the machine for the dashboard.
type Info struct {
	Hostname      string   `json:"hostname"`
	Kernel        string   `json:"kernel"`
	UptimeSeconds int64    `json:"uptimeSeconds"`
	CPU           CPU      `json:"cpu"`
	Memory        Memory   `json:"memory"`
	Image         *Image   `json:"image"`
	Stick         Stick    `json:"stick"`
	Network       *Network `json:"network"`
}

type CPU struct {
	Model        string  `json:"model"`
	Threads      int     `json:"threads"`
	UsagePercent float64 `json:"usagePercent"`
}

type Memory struct {
	TotalBytes uint64 `json:"totalBytes"`
	UsedBytes  uint64 `json:"usedBytes"`
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
	Interface        string  `json:"interface"`
	IPv4             string  `json:"ipv4"`
	Gateway          string  `json:"gateway"`
	SpeedMbps        int     `json:"speedMbps"`
	RxBytesPerSecond float64 `json:"rxBytesPerSecond"`
	TxBytesPerSecond float64 `json:"txBytesPerSecond"`
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
	mu      sync.RWMutex
	cpu     float64
	rx, tx  float64
	network string
}

// NewMonitor starts sampling until ctx is done.
func NewMonitor(ctx context.Context) *Monitor {
	m := &Monitor{}
	go m.run(ctx)
	return m
}

func (m *Monitor) run(ctx context.Context) {
	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()

	prevCPU, _ := readCPUTimes()
	prevIface := defaultRoute().iface
	prevRx, prevTx := readNetBytes(prevIface)
	prevTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			seconds := now.Sub(prevTime).Seconds()
			prevTime = now

			cpu, err := readCPUTimes()
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

			m.mu.Lock()
			m.cpu, m.rx, m.tx, m.network = usage, rxRate, txRate, iface
			m.mu.Unlock()
		}
	}
}

// Info returns the current state of the machine.
func (m *Monitor) Info() Info {
	m.mu.RLock()
	usage, rx, tx := m.cpu, m.rx, m.tx
	m.mu.RUnlock()

	hostname, _ := os.Hostname()
	info := Info{
		Hostname:      hostname,
		Kernel:        readTrimmed("/proc/sys/kernel/osrelease"),
		UptimeSeconds: readUptime(),
		CPU:           CPU{Model: cpuModel(readString("/proc/cpuinfo")), Threads: runtime.NumCPU(), UsagePercent: usage},
		Memory:        parseMeminfo(readString("/proc/meminfo")),
		Image:         readImage(),
		Stick:         Stick{Label: stickLabel, Present: exists(stickDevice)},
	}

	if route := defaultRoute(); route.iface != "" {
		info.Network = &Network{
			Interface:        route.iface,
			IPv4:             interfaceIPv4(route.iface),
			Gateway:          route.gateway,
			SpeedMbps:        readSpeed(route.iface),
			RxBytesPerSecond: rx,
			TxBytesPerSecond: tx,
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

// parseMeminfo reads total and used memory from /proc/meminfo. "Used" is
// what is not available to new programs; caches that the kernel can drop do
// not count.
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
		if err != nil {
			continue
		}
		values[name] = kib * 1024
	}
	total, available := values["MemTotal"], values["MemAvailable"]
	if available > total {
		available = total
	}
	return Memory{TotalBytes: total, UsedBytes: total - available}
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
	return parseDefaultRoute(readString("/proc/net/route"))
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

func interfaceIPv4(name string) string {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
			return ipnet.String()
		}
	}
	return ""
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
	speed, err := strconv.Atoi(readTrimmed("/sys/class/net/" + iface + "/speed"))
	if err != nil || speed < 0 {
		return 0
	}
	return speed
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
