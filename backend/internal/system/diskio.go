package system

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// DiskIO is the throughput of one block device over the last sample
// (task manager, PLAN 61). It comes from the kernel's counters in
// /sys/block/<name>/stat: reading them sends nothing to the disk, so a
// sleeping disk stays asleep.
type DiskIO struct {
	Name                string  `json:"name"`
	ReadBytesPerSecond  float64 `json:"readBytesPerSecond"`
	WriteBytesPerSecond float64 `json:"writeBytesPerSecond"`
	// BusyPercent is the share of time with I/O in flight (io_ticks).
	BusyPercent float64 `json:"busyPercent"`
}

// diskCounters are sectors read, sectors written and milliseconds busy.
type diskCounters [3]uint64

// readDiskCounters reads every block device but loop, RAM and zram devices.
func readDiskCounters(sysBlock string) map[string]diskCounters {
	entries, err := os.ReadDir(sysBlock)
	if err != nil {
		return nil
	}
	out := map[string]diskCounters{}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "zram") {
			continue
		}
		if c, ok := parseDiskStat(readString(filepath.Join(sysBlock, name, "stat"))); ok {
			out[name] = c
		}
	}
	return out
}

// parseDiskStat reads the fields of Documentation/block/stat.rst: 3
// sectors read, 7 sectors written, 10 io_ticks (1-based).
func parseDiskStat(data string) (diskCounters, bool) {
	f := strings.Fields(data)
	if len(f) < 10 {
		return diskCounters{}, false
	}
	var c diskCounters
	for i, field := range []int{2, 6, 9} {
		v, err := strconv.ParseUint(f[field], 10, 64)
		if err != nil {
			return diskCounters{}, false
		}
		c[i] = v
	}
	return c, true
}

// diskRates turns two reads into rates; counters that went back (a
// device replaced under the same name) give no value.
func diskRates(before, after map[string]diskCounters, seconds float64) []DiskIO {
	out := []DiskIO{}
	if seconds <= 0 {
		return out
	}
	for name, c := range after {
		p, ok := before[name]
		if !ok || c[0] < p[0] || c[1] < p[1] || c[2] < p[2] {
			continue
		}
		busy := float64(c[2]-p[2]) / (seconds * 1000) * 100
		out = append(out, DiskIO{
			Name:                name,
			ReadBytesPerSecond:  float64(c[0]-p[0]) * 512 / seconds,
			WriteBytesPerSecond: float64(c[1]-p[1]) * 512 / seconds,
			BusyPercent:         min(100, busy),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
