package hardware

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNVIDIAMeasurements(t *testing.T) {
	devices := []Accelerator{{Address: "0000:01:00.0", Driver: "nvidia"}, {Address: "0000:02:00.0", Driver: "vfio-pci"}}
	applyNVIDIA(devices, "00000000:01:00.0, Test GPU, 37, 51, 18.5, 512, 8192, P8, 210, 12, 4\n00000000:02:00.0, Reserved, 90, 60, 50, 1024, 8192, P0, 1800, 10, 20\n")
	d := devices[0]
	if d.Name != "Test GPU" || *d.UsagePercent != 37 || *d.PowerWatts != 18.5 || *d.MemoryUsedBytes != 512*1024*1024 || len(d.Engines) != 2 {
		t.Fatalf("measurements: %+v", d)
	}
	if devices[1].UsagePercent != nil {
		t.Fatal("queried a GPU reserved for a VM")
	}
	applyNVIDIA(devices, "00000000:01:00.0, Test GPU, N/A, [Not Supported], NaN, -1, inf, P8, N/A, N/A, N/A\n")
	if devices[0].UsagePercent != nil || devices[0].PowerWatts != nil || devices[0].MemoryUsedBytes != nil || devices[0].MemoryTotalBytes != nil {
		t.Fatal("unsupported metrics were interpreted as numbers")
	}
}

func TestDRMClientDeduplicationAndEngineCapacity(t *testing.T) {
	proc := t.TempDir()
	fd := "drm-pdev: 0000:00:02.0\ndrm-client-id: 7\ndrm-engine-render: 2000000000 ns\ndrm-engine-capacity-render: 2\n"
	for _, path := range []string{"123/fdinfo/4", "123/fdinfo/5", "234/fdinfo/6"} {
		path = filepath.Join(proc, path)
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(fd), 0o600)
	}
	before, complete := readDRMClients(proc)
	if !complete || len(before) != 1 {
		t.Fatalf("shared descriptors counted more than once: %+v", before)
	}
	id, next := parseDRMClient(strings.Replace(fd, "2000000000", "7000000000", 1))
	current := map[string]drmClient{id: next}
	usage := drmUsage("0000:00:02.0", current, before, 5)
	if len(usage) != 1 || usage[0].UsagePercent != 50 {
		t.Fatalf("usage = %+v", usage)
	}
	// A transient counter regression must not create a later usage spike.
	_, regressed := parseDRMClient(strings.Replace(fd, "2000000000", "1000000000", 1))
	regression := map[string]drmClient{id: regressed}
	if got := drmUsage("0000:00:02.0", regression, current, 5); len(got) != 0 {
		t.Fatalf("counter regression: %+v", got)
	}
	if regression[id].counters["render"] != 7000000000 {
		t.Fatal("lost previous high counter")
	}
	if got := drmUsage("0000:00:02.0", current, nil, 5); len(got) != 0 {
		t.Fatal("first sample invented a load")
	}
}

func TestXeCycleCounters(t *testing.T) {
	fd := "drm-pdev: 0000:00:02.0\ndrm-client-id: 1\ndrm-cycles-rcs: 100\ndrm-total-cycles-rcs: 1000\n"
	id, before := parseDRMClient(fd)
	_, after := parseDRMClient(strings.ReplaceAll(strings.ReplaceAll(fd, ": 100\n", ": 600\n"), ": 1000\n", ": 2000\n"))
	usage := drmUsage(before.address, map[string]drmClient{id: after}, map[string]drmClient{id: before}, 5)
	if len(usage) != 1 || usage[0].UsagePercent != 50 {
		t.Fatalf("Xe cycle utilization: %+v", usage)
	}
}

func TestIncompleteScanCannotBecomeUsageBaseline(t *testing.T) {
	sys := t.TempDir()
	address := "0000:00:02.0"
	device(t, sys, address, "030000", "8086", -1, nil)
	os.Symlink("/drivers/i915", filepath.Join(sys, "bus/pci/devices", address, "driver"))
	m := &AcceleratorMonitor{hardware: &Manager{sys: sys}, proc: filepath.Join(t.TempDir(), "missing"), runner: &acceleratorRunner{}, previous: map[string]drmClient{"old": {address: address}}}
	m.sample(context.Background(), time.Now())
	if len(m.previous) != 0 || m.Snapshot()[0].UsagePercent != nil {
		t.Fatal("incomplete scan retained a baseline")
	}
}

type acceleratorRunner struct {
	calls  int
	output string
	err    error
}

func (r *acceleratorRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	r.calls++
	return r.output, r.err
}

func TestAcceleratorSamplingAndHistory(t *testing.T) {
	sys := t.TempDir()
	m := &AcceleratorMonitor{hardware: &Manager{sys: sys}, proc: t.TempDir(), runner: &acceleratorRunner{err: errors.New("no GPU")}}
	write := func(address, file, value string) {
		t.Helper()
		p := filepath.Join(sys, "bus/pci/devices", address, file)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	address := "0000:00:0b.0"
	device(t, sys, address, "120000", "8086", -1, nil)
	os.Symlink("/sys/bus/pci/drivers/intel_vpu", filepath.Join(sys, "bus/pci/devices", address, "driver"))
	write(address, "npu_busy_time_us", "1000000")
	write(address, "npu_memory_utilization", "1024")
	write(address, "freq/current_freq", "400")
	now := time.Now()
	m.sample(context.Background(), now)
	if d := m.Snapshot()[0]; d.UsagePercent != nil || *d.FrequencyMHz != 400 || *d.MemoryUsedBytes != 1024 {
		t.Fatalf("first sample: %+v", d)
	}
	write(address, "npu_busy_time_us", "3500000")
	m.sample(context.Background(), now.Add(5*time.Second))
	if d := m.Snapshot()[0]; d.UsagePercent == nil || *d.UsagePercent != 50 {
		t.Fatalf("NPU delta: %+v", d)
	}
	write(address, "npu_busy_time_us", "10")
	m.sample(context.Background(), now.Add(10*time.Second))
	if m.Snapshot()[0].UsagePercent != nil {
		t.Fatal("reset counter generated a measurement")
	}
	for i := 3; i < 45; i++ {
		m.sample(context.Background(), now.Add(time.Duration(i)*5*time.Second))
	}
	if len(m.Snapshot()[0].History) != 36 {
		t.Fatal("history is not bounded")
	}
	copy := m.Snapshot()
	copy[0].History[0] = nil
	copy[0].Name = "changed"
	if m.Snapshot()[0].Name == "changed" {
		t.Fatal("snapshot aliases live state")
	}
	// Driver loss clears readings and the preceding history.
	os.Remove(filepath.Join(sys, "bus/pci/devices", address, "driver"))
	m.sample(context.Background(), now.Add(225*time.Second))
	if d := m.Snapshot()[0]; d.State != "driver_missing" || d.UsagePercent != nil || d.MemoryUsedBytes != nil || len(d.History) != 1 {
		t.Fatalf("driver disappeared: %+v", d)
	}
	if m.runner.(*acceleratorRunner).calls != 0 {
		t.Fatal("called nvidia-smi without an NVIDIA device")
	}
}

func TestSuspendedAndReservedGPUNotQueried(t *testing.T) {
	for _, driver := range []string{"nvidia", "vfio-pci"} {
		t.Run(driver, func(t *testing.T) {
			sys := t.TempDir()
			address := "0000:01:00.0"
			device(t, sys, address, "030000", "10de", -1, nil)
			root := filepath.Join(sys, "bus/pci/devices", address)
			os.Symlink("/drivers/"+driver, filepath.Join(root, "driver"))
			os.MkdirAll(filepath.Join(root, "power"), 0o755)
			os.WriteFile(filepath.Join(root, "power/runtime_status"), []byte("suspended"), 0o600)
			runner := &acceleratorRunner{}
			m := &AcceleratorMonitor{hardware: &Manager{sys: sys}, runner: runner, proc: t.TempDir()}
			m.sample(context.Background(), time.Now())
			if runner.calls != 0 || m.Snapshot()[0].UsagePercent != nil {
				t.Fatal("woke or measured a suspended/reserved GPU")
			}
		})
	}
}
