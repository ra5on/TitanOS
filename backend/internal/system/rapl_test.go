package system

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRaplWatts(t *testing.T) {
	sys := t.TempDir()
	write := func(rel, value string) {
		p := filepath.Join(sys, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(value+"\n"), 0o644)
	}
	write("class/powercap/intel-rapl:0/name", "package-0")
	write("class/powercap/intel-rapl:0/energy_uj", "1000000")
	write("class/powercap/intel-rapl:0/max_energy_range_uj", "262143328850")
	write("class/powercap/intel-rapl:0:0/name", "core")
	write("class/powercap/intel-rapl:0:0/energy_uj", "5")
	write("class/powercap/intel-rapl:1/name", "psys")
	zones := raplZones(sys)
	if len(zones) != 1 {
		t.Fatalf("zones %v", zones)
	}
	now := time.Unix(1000, 0)
	before, ok := readRapl(zones, now)
	if !ok {
		t.Fatal("not read")
	}
	write("class/powercap/intel-rapl:0/energy_uj", "51000000") // 50 J in 5 s
	after, _ := readRapl(zones, now.Add(5*time.Second))
	if w := raplWatts(zones, before, after); w == nil || *w != 10 {
		t.Fatalf("watts %v", w)
	}
	// The counter wraps.
	before = raplSample{at: now, energy: []uint64{262143328850 - 1000000}}
	after = raplSample{at: now.Add(time.Second), energy: []uint64{4000000}}
	if w := raplWatts(zones, before, after); w == nil || *w != 5 {
		t.Fatalf("wrapped %v", w)
	}
	if raplZones(t.TempDir()) != nil {
		t.Fatal("zones without powercap")
	}
}
