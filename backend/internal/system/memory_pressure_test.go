package system

import (
	"testing"
	"time"
)

func TestMemoryPressure(t *testing.T) {
	got := parseMemoryPressure("some avg10=12.34 avg60=3.21 total=123\nfull avg10=1.25 avg60=0.1 total=4\n")
	if got == nil || got.SomeAvg10 != 12.34 || got.FullAvg10 != 1.25 {
		t.Fatal(got)
	}
	for _, data := range []string{"", "some avg10=0", "some avg10=NaN\nfull avg10=0", "some avg10=Inf\nfull avg10=0", "some avg10=-1\nfull avg10=0", "some avg10=101\nfull avg10=0", "some avg10=1\nfull avg10=2", "some avg10=1 avg10=2\nfull avg10=0"} {
		if parseMemoryPressure(data) != nil {
			t.Fatalf("accepted invalid PSI: %q", data)
		}
	}
}

func TestMemoryWarningTracksAvailabilityNotCache(t *testing.T) {
	var w memoryWarning
	now := time.Now()
	mem := Memory{TotalBytes: 16 << 30, UsedBytes: 15 << 30, CacheBytes: 12 << 30, AvailableBytes: 12 << 30}
	for i := 0; i <= 40; i++ {
		if w.observe(mem, now) {
			t.Fatal("healthy cache mistaken for memory shortage")
		}
		now = now.Add(sampleInterval)
	}
	mem.AvailableBytes = 1 << 30
	for i := 0; i <= 15; i++ {
		if active := w.observe(mem, now); active != (i == 15) {
			t.Fatalf("shortage sample %d: %v", i, active)
		}
		now = now.Add(sampleInterval)
	}
	mem.AvailableBytes = 8 << 30
	for i := 0; i <= 30; i++ {
		if active := w.observe(mem, now); active != (i < 30) {
			t.Fatalf("recovery sample %d: %v", i, active)
		}
		now = now.Add(sampleInterval)
	}
	mem.AvailableBytes = 0
	w.observe(mem, now)
	if w.observe(mem, now.Add(time.Minute)) {
		t.Fatal("gap counted as continuous pressure")
	}
}
