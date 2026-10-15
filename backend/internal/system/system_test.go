package system

import (
	"math"
	"testing"
)

func TestParseMeminfo(t *testing.T) {
	got := parseMeminfo("MemTotal:        4009804 kB\nMemFree:         3626296 kB\nMemAvailable:    3556764 kB\nBuffers:               0 kB\n")
	if got.TotalBytes != 4009804*1024 || got.UsedBytes != (4009804-3626296)*1024 || got.FreeBytes != 3626296*1024 || got.AvailableBytes != 3556764*1024 {
		t.Errorf("parseMeminfo = %+v", got)
	}
	if empty := parseMeminfo(""); empty.TotalBytes != 0 || empty.UsedBytes != 0 {
		t.Errorf("empty meminfo = %+v", empty)
	}
}

func TestMemoryIncludesCacheAndRAMImageExactlyOnce(t *testing.T) {
	got := parseMeminfo("MemTotal: 16000 kB\nMemFree: 2000 kB\nMemAvailable: 8000 kB\nCached: 7000 kB\nShmem: 1000 kB\nBuffers: 500 kB\n")
	if got.UsedBytes != 14000*1024 || got.AvailableBytes != 8000*1024 || got.UsedBytes+got.FreeBytes != got.TotalBytes {
		t.Fatalf("occupied RAM must include caches and shmem once: %+v", got)
	}
	for _, input := range []string{"MemFree: 100 kB", "MemTotal: 10 kB\nMemFree: 20 kB\nMemAvailable: 30 kB", "MemTotal: -1 kB", "MemTotal: 18446744073709551615 kB"} {
		got := parseMeminfo(input)
		if got.UsedBytes > got.TotalBytes || got.AvailableBytes > got.TotalBytes || got.FreeBytes > got.TotalBytes {
			t.Fatalf("invalid memory values: %+v", got)
		}
	}
}

func TestCPUUsage(t *testing.T) {
	prev := parseCPULine("cpu  100 0 100 700 100 0 0 0 0 0")
	cur := parseCPULine("cpu  150 0 150 750 150 0 0 0 0 0")
	// 200 ticks passed, 100 of them idle or waiting for I/O.
	if got := cpuUsage(prev, cur); math.Abs(got-50) > 0.001 {
		t.Errorf("cpuUsage = %v, want 50", got)
	}
	if got := cpuUsage(cur, cur); got != 0 {
		t.Errorf("cpuUsage without progress = %v", got)
	}
	if got := cpuUsage(cur, prev); got != 0 {
		t.Errorf("cpuUsage with counters going backwards = %v", got)
	}
}

func TestParseDefaultRoute(t *testing.T) {
	table := "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
		"wlan0\t00000000\t0102A8C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n" +
		"enp0s4\t00000000\t0202000A\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
		"enp0s4\t0002000A\t00000000\t0001\t0\t0\t100\t00FFFFFF\t0\t0\t0\n"
	got := parseDefaultRoute(table)
	if got.iface != "enp0s4" || got.gateway != "10.0.2.2" {
		t.Errorf("parseDefaultRoute = %+v", got)
	}
	if none := parseDefaultRoute("Iface\tDestination\n"); none.iface != "" {
		t.Errorf("route without default = %+v", none)
	}
}

func TestParseSlot(t *testing.T) {
	cmdline := `initrd=\titanos\b\initrd.img boot=live live-media=/dev/disk/by-label/TITANOS live-media-path=/titanos/b toram=filesystem.squashfs`
	if got := parseSlot(cmdline); got != "b" {
		t.Errorf("parseSlot = %q, want b", got)
	}
	if got := parseSlot("BOOT_IMAGE=/vmlinuz root=/dev/sda1"); got != "" {
		t.Errorf("parseSlot on a normal system = %q", got)
	}
}

func TestCPUModel(t *testing.T) {
	if got := cpuModel("processor\t: 0\nmodel name\t: Example CPU 3000\nflags\t: fpu\n"); got != "Example CPU 3000" {
		t.Errorf("cpuModel = %q", got)
	}
}
