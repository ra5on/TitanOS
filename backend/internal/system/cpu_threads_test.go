package system

import "testing"

func TestCPUThreadSamples(t *testing.T) {
	before := parseCPUThreads("cpu 100 0 0 100\ncpu10 10 0 0 10\ncpu2 10 0 0 10\n")
	after := parseCPUThreads("cpu10 20 0 0 20\ncpu2 20 0 0 10\ncpu3 5 0 0 5\nintr 2 3 4 5\n")
	got := sampleCPUThreads(before, after)
	if len(got) != 3 || got[0].ID != "cpu2" || got[1].ID != "cpu3" || got[2].ID != "cpu10" {
		t.Fatal(got)
	}
	if got[0].UsagePercent == nil || *got[0].UsagePercent != 100 || got[1].UsagePercent != nil || *got[2].UsagePercent != 50 {
		t.Fatal(got)
	}
	reset := sampleCPUThreads(after, before)
	if reset[0].UsagePercent != nil {
		t.Fatal(reset)
	}
}
