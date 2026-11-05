package hwcheck

import (
	"strings"
	"testing"
)

// A port without a cable is information, not a problem; empty sections
// say so instead of ending without a line.
func TestReportTextQuietParts(t *testing.T) {
	text := Report{Network: []NIC{{Name: "enp2s0", Link: true, SpeedMbs: 1000}, {Name: "enp3s0"}}}.Text("0.49.0")
	for _, want := range []string{"enp2s0", "link yes  1000 Mbit/s", "enp3s0", "link no (not connected)", "Devices without a driver\n  none", "Missing firmware\n  none"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	if s := summarize(Report{CPU: CPU{Level: 3}, Virtualization: Virtualization{CPUFlag: true, KVM: true, IOMMU: true}, Network: []NIC{{Name: "enp3s0"}}}); len(s) != 0 {
		t.Errorf("a port without link gives a finding: %+v", s)
	}
}
