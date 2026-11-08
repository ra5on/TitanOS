package notify

import (
	"github.com/ra5on/TitanOS/backend/internal/config"
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

// A board sensor with a meaningless critical value (0 °C) is as
// untrustworthy as one without; and a missing reading breaks the run of
// hot looks.
func TestHardwareWatcherMeaninglessCriticalAndGaps(t *testing.T) {
	m, _ := newManager(t)
	w := NewHardwareWatcher(m)
	w.edac = t.TempDir()
	zero, hot := 0.0, 126.0
	var readings []TempReading
	w.Temperatures = func() []TempReading { return readings }
	readings = []TempReading{{ID: "board", Chip: "nct6798", Label: "temp8", Celsius: &hot, Critical: &zero}}
	w.look()
	w.look()
	if n := len(m.Messages()); n != 0 {
		t.Fatalf("board sensor with crit 0 raised %d messages", n)
	}
	cpu := TempReading{ID: "cpu", Chip: "coretemp", Label: "Package id 0", Celsius: &hot}
	gap := TempReading{ID: "cpu", Chip: "coretemp", Label: "Package id 0"}
	for _, r := range []TempReading{cpu, gap, cpu} {
		readings = []TempReading{r}
		w.look()
	}
	if n := len(m.Messages()); n != 0 {
		t.Fatalf("hot, gap, hot raised %d messages; the looks were not in a row", n)
	}
	w.look()
	if n := len(m.Messages()); n != 1 {
		t.Fatalf("two hot looks in a row: %d messages", n)
	}
}

// The message names the sensor so that it can be found: chip, label or
// channel, "no label" when the channel has none, and the sysfs path.
func TestHardwareWatcherSubjectFindsTheSensor(t *testing.T) {
	hot, crit := 126.0, 110.0
	cases := []struct {
		language string
		reading  TempReading
		want     string
	}{
		{"de", TempReading{ID: "a", Label: "temp8", Channel: "temp8", Device: "devices/platform/nct6775.656", Chip: "nct6798", Celsius: &hot, Critical: &crit},
			"nct6798 temp8 (126 °C, ohne Bezeichnung) – /sys/devices/platform/nct6775.656"},
		{"en", TempReading{ID: "b", Label: "temp8", Channel: "temp8", Device: "devices/platform/nct6775.656", Chip: "nct6798", Celsius: &hot, Critical: &crit},
			"nct6798 temp8 (126 °C, no label) – /sys/devices/platform/nct6775.656"},
		{"de", TempReading{ID: "c", Label: "Package id 0", Channel: "temp1", Device: "devices/platform/coretemp.0", Chip: "coretemp", Celsius: &hot},
			"coretemp Package id 0 (126 °C) – /sys/devices/platform/coretemp.0"},
	}
	for _, c := range cases {
		m, _ := newManager(t)
		settings, _ := m.Settings()
		settings.Language = c.language
		if err := m.SetSettings(settings); err != nil {
			t.Fatal(err)
		}
		w := NewHardwareWatcher(m)
		w.edac = t.TempDir()
		w.Temperatures = func() []TempReading { return []TempReading{c.reading} }
		w.look()
		w.look()
		messages := m.Messages()
		if len(messages) != 1 || messages[0].Subject != c.want {
			t.Fatalf("%s: %+v, want %q", c.language, messages, c.want)
		}
	}
}

// Disks without a critical value of their own alarm at their own limit,
// not at the 95 °C of a CPU.
func TestHardwareWatcherDiskLimit(t *testing.T) {
	m, _ := newManager(t)
	w := NewHardwareWatcher(m)
	w.edac = t.TempDir()
	warm := 62.0
	w.Temperatures = func() []TempReading {
		return []TempReading{
			{ID: "disk", Label: "temp1", Channel: "temp1", Chip: "drivetemp", Celsius: &warm},
			{ID: "cpu", Label: "Tctl", Channel: "temp1", Chip: "k10temp", Celsius: &warm},
		}
	}
	w.look()
	w.look()
	messages := m.Messages()
	if len(messages) != 1 || !strings.HasPrefix(messages[0].Subject, "drivetemp ") {
		t.Fatalf("messages: %+v", messages)
	}
}

// PLAN 75-9: shut down on heat only when switched on, only for reliable
// chips with a critical value of their own, after five looks in a row.
func TestHardwareWatcherHeatShutdown(t *testing.T) {
	hot, cool, crit := 98.0, 60.0, 100.0
	cases := []struct {
		name   string
		temp   TempReading
		on     bool
		mute   bool
		looks  int
		gapAt  int
		wantOn bool
	}{
		{"cpu at its limit for five minutes", TempReading{ID: "a", Chip: "coretemp", Celsius: &hot, Critical: &crit}, true, false, 5, -1, true},
		{"switched off by default", TempReading{ID: "b", Chip: "coretemp", Celsius: &hot, Critical: &crit}, false, false, 8, -1, false},
		{"four minutes are not enough", TempReading{ID: "c", Chip: "coretemp", Celsius: &hot, Critical: &crit}, true, false, 4, -1, false},
		{"a cool look starts again", TempReading{ID: "d", Chip: "coretemp", Celsius: &hot, Critical: &crit}, true, false, 6, 2, false},
		{"motherboard chip never", TempReading{ID: "e", Chip: "nct6798", Label: "temp8", Celsius: &hot, Critical: &crit}, true, false, 8, -1, false},
		{"cpu without its own critical value never", TempReading{ID: "f", Chip: "coretemp", Celsius: &hot}, true, false, 8, -1, false},
		{"muted sensor never", TempReading{ID: "0123456789abcdef0123456789abcdef", Chip: "coretemp", Celsius: &hot, Critical: &crit}, true, true, 8, -1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := newManager(t)
			if err := m.SetHeatShutdown(c.on); err != nil {
				t.Fatal(err)
			}
			if c.mute {
				if err := m.SetMutedTemperatures([]string{c.temp.ID}); err != nil {
					t.Fatal(err)
				}
			}
			w := NewHardwareWatcher(m)
			w.edac = t.TempDir()
			offs := 0
			w.Poweroff = func() error { offs++; return nil }
			for i := range c.looks {
				reading := c.temp
				if i == c.gapAt {
					reading.Celsius = &cool
				}
				w.Temperatures = func() []TempReading { return []TempReading{reading} }
				w.look()
			}
			if got := offs > 0; got != c.wantOn {
				t.Fatalf("shut down: %v, want %v", got, c.wantOn)
			}
			if offs > 1 {
				t.Fatalf("asked to shut down %d times", offs)
			}
			said := false
			for _, msg := range m.Messages() {
				if msg.Kind == "heat_shutdown" {
					said = true
				}
			}
			if said != c.wantOn {
				t.Fatalf("message heat_shutdown: %v, want %v", said, c.wantOn)
			}
		})
	}
}

func TestHeatShutdownSettingSurvivesReload(t *testing.T) {
	m, dir := newManager(t)
	if m.HeatShutdown() {
		t.Fatal("on by default")
	}
	if err := m.SetHeatShutdown(true); err != nil {
		t.Fatal(err)
	}
	again := NewManager(config.Open(dir))
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if !again.HeatShutdown() {
		t.Fatal("setting lost")
	}
}
