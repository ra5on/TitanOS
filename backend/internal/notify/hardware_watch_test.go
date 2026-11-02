package notify

import (
	"os"
	"path/filepath"
	"strings"
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

func TestHardwareWatcherTemperatureTrust(t *testing.T) {
	hot, crit := 126.0, 110.0
	cases := []struct {
		name  string
		temp  TempReading
		looks int
		want  int
	}{
		// An unlabelled Super-I/O input without a critical value reads nonsense.
		{"unused input of a motherboard chip", TempReading{ID: "a", Label: "temp8", Chip: "nct6798", Celsius: &hot}, 3, 0},
		{"same input with its own critical value", TempReading{ID: "b", Label: "temp8", Chip: "nct6798", Celsius: &hot, Critical: &crit}, 2, 1},
		{"cpu above the fixed limit", TempReading{ID: "c", Label: "Package id 0", Chip: "coretemp", Celsius: &hot}, 2, 1},
		{"one jump is no alarm", TempReading{ID: "d", Label: "Package id 0", Chip: "coretemp", Celsius: &hot}, 1, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := newManager(t)
			w := NewHardwareWatcher(m)
			w.edac = t.TempDir()
			w.Temperatures = func() []TempReading { return []TempReading{c.temp} }
			for range c.looks {
				w.look()
			}
			messages := m.Messages()
			if len(messages) != c.want {
				t.Fatalf("messages: %+v", messages)
			}
			if c.want == 1 && !strings.Contains(messages[0].Subject, c.temp.Chip+" "+c.temp.Label) {
				t.Fatalf("subject without chip: %q", messages[0].Subject)
			}
		})
	}
}

func TestHardwareWatcherTemperatureJumpResets(t *testing.T) {
	m, _ := newManager(t)
	w := NewHardwareWatcher(m)
	w.edac = t.TempDir()
	temp := 126.0
	w.Temperatures = func() []TempReading {
		return []TempReading{{ID: "d", Label: "Package id 0", Chip: "coretemp", Celsius: &temp}}
	}
	w.look()
	temp = 40
	w.look()
	temp = 126
	w.look()
	if n := len(m.Messages()); n != 0 {
		t.Fatalf("a jump now and then raised %d messages", n)
	}
}
