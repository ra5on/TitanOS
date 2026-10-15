package hardware

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type testPerfFile struct {
	bytes.Buffer
	closed bool
}

func (f *testPerfFile) Close() error { f.closed = true; return nil }
func (f *testPerfFile) push(value, enabled, running uint64) {
	for _, n := range []uint64{value, enabled, running} {
		binary.Write(&f.Buffer, binary.NativeEndian, n)
	}
}

func testPMU(t *testing.T, root, name string) {
	t.Helper()
	for file, value := range map[string]string{"type": "16", "cpumask": "0", "events/rcs0-busy": "config=0x0", "events/vcs0-busy": "config=0x2000"} {
		path := filepath.Join(root, "bus/event_source/devices", name, file)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestI915GlobalCountersWithoutDRMClients(t *testing.T) {
	sys := t.TempDir()
	testPMU(t, sys, "i915")
	files := map[uint64]*testPerfFile{0: {}, 0x2000: {}}
	opens := 0
	m := &i915PMU{sys: sys, open: func(kind uint32, config uint64, cpu int) (io.ReadCloser, error) {
		opens++
		if kind != 16 || cpu != 0 {
			t.Fatalf("wrong PMU: %d %d", kind, cpu)
		}
		return files[config], nil
	}}
	for _, file := range files {
		file.push(1e9, 5e9, 5e9)
	}
	if got := m.sample("0000:00:02.0"); len(got) != 0 {
		t.Fatal("first sample invented utilization")
	}
	files[0].push(1e9, 10e9, 10e9)
	files[0x2000].push(3.5e9, 10e9, 10e9)
	got := m.sample("0000:00:02.0")
	if opens != 2 || len(got) != 2 || got[0].UsagePercent != 0 || got[1].UsagePercent != 50 {
		t.Fatalf("idle and active engines without any client: %+v, opens=%d", got, opens)
	}
	// A short/dead perf read must clear all baselines and close descriptors.
	if got := m.sample("0000:00:02.0"); len(got) != 0 {
		t.Fatal(got)
	}
	if !files[0].closed || !files[0x2000].closed || len(m.devices) != 0 {
		t.Fatal("dead counters retained")
	}
}

func TestPerfDeltaValidationAndMultiplexing(t *testing.T) {
	before := &perfReading{1e9, 5e9, 4e9}
	for _, tc := range []struct {
		after perfReading
		want  float64
		valid bool
	}{
		{perfReading{2e9, 10e9, 6e9}, 50, true},
		{perfReading{1e9, 10e9, 9e9}, 0, true},
		{perfReading{7e9, 10e9, 9e9}, 100, true},
		{perfReading{0, 10e9, 9e9}, 0, false},
		{perfReading{2e9, 10e9, 4e9}, 0, false},
		{perfReading{2e9, 10e9, 10e9}, 0, false},
		{perfReading{2e9, 30e9, 29e9}, 0, false},
	} {
		got, valid := perfUsage(before, tc.after)
		if got != tc.want || valid != tc.valid {
			t.Fatalf("%+v: %v/%v", tc.after, got, valid)
		}
	}
}

func TestPMUClosesOnPartialFailureAndRemoval(t *testing.T) {
	sys := t.TempDir()
	testPMU(t, sys, "i915_0000_03_00.0")
	file := &testPerfFile{}
	m := &i915PMU{sys: sys, open: func(_ uint32, config uint64, _ int) (io.ReadCloser, error) {
		if config != 0 {
			return nil, errors.New("permission denied")
		}
		return file, nil
	}}
	if got := m.sample("0000:03:00.0"); len(got) != 0 || !file.closed {
		t.Fatal("partial PMU was used or leaked")
	}
	file = &testPerfFile{}
	file.push(0, 1, 1)
	file.push(0, 1, 1)
	m.open = func(uint32, uint64, int) (io.ReadCloser, error) { return file, nil }
	m.sample("0000:03:00.0")
	if err := os.Remove(filepath.Join(sys, "bus/event_source/devices/i915_0000_03_00.0/type")); err != nil {
		t.Fatal(err)
	}
	m.sample("0000:03:00.0")
	if !file.closed || len(m.devices) != 0 {
		t.Fatal("disappeared PMU leaked perf descriptors")
	}
	testPMU(t, sys, "i915_0000_03_00.0")
	file = &testPerfFile{}
	file.push(0, 1, 1)
	file.push(0, 1, 1)
	m.sample("0000:03:00.0")
	m.retain(nil)
	if !file.closed || len(m.devices) != 0 {
		t.Fatal("removed/assigned GPU leaked perf descriptors")
	}
}
