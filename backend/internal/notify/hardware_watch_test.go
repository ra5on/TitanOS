package notify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHardwareWatcherReportsChanges(t *testing.T) {
	m, _ := newManager(t)
	w := NewHardwareWatcher(m)
	w.edac = t.TempDir()
	mc := filepath.Join(w.edac, "mc0")
	os.MkdirAll(mc, 0o755)
	os.WriteFile(filepath.Join(mc, "ce_count"), []byte("3\n"), 0o644)
	os.WriteFile(filepath.Join(mc, "ue_count"), []byte("0\n"), 0o644)
	rpm, idle := 900, 0
	temp, crit := 70.0, 100.0
	fans := []FanReading{{ID: "f1", Label: "CPU fan", RPM: &rpm, Percent: 40}, {ID: "f2", Label: "never", RPM: &idle, Percent: 40}}
	w.Fans = func() []FanReading { return fans }
	w.Temperatures = func() []TempReading {
		return []TempReading{{ID: "t1", Label: "Package id 0", Celsius: &temp, Critical: &crit}}
	}
	w.look()
	if len(m.Messages()) != 0 {
		t.Fatalf("first look: %+v", m.Messages())
	}
	rpm, temp = 0, 96
	os.WriteFile(filepath.Join(mc, "ce_count"), []byte("5\n"), 0o644)
	w.look()
	w.look()
	kinds := map[string]int{}
	for _, msg := range m.Messages() {
		kinds[msg.Kind]++
	}
	if kinds["fan_stopped"] != 1 || kinds["temperature_critical"] != 1 || kinds["memory_corrected"] != 1 || len(kinds) != 3 {
		t.Fatalf("kinds: %v", kinds)
	}
	// A fan driven at 0 % may stand.
	fans[0].Percent = 0
	w.stopped["f1"] = 0
	w.look()
	w.look()
	if n := len(m.Messages()); n != 3 {
		t.Fatalf("messages: %d", n)
	}
}
