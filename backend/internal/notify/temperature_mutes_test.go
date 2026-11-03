package notify

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const (
	cpuSensor   = "00112233445566778899aabbccddeeff"
	boardSensor = "ffeeddccbbaa99887766554433221100"
)

func TestAlarmLimit(t *testing.T) {
	low, crit := 30.0, 100.0
	for _, c := range []struct {
		chip     string
		critical *float64
		limit    float64
		alarms   bool
	}{
		{"coretemp", nil, 95, true},
		{"coretemp", &crit, 95, true},
		{"nct6798", nil, 0, false},
		{"nct6798", &low, 0, false},
		{"nct6798", &crit, 95, true},
		{"", nil, 95, true},
		// Kinds of chips without a value of their own get their own default.
		{"drivetemp", nil, 60, true},
		{"nvme", nil, 85, true},
		{"k10temp", nil, 95, true},
		// The chip's own value comes first, also for disks.
		{"nvme", &crit, 95, true},
	} {
		limit, alarms := AlarmLimit(c.chip, c.critical)
		if limit != c.limit || alarms != c.alarms {
			t.Errorf("%s %v: %v %v", c.chip, c.critical, limit, alarms)
		}
	}
}

func TestMutedTemperaturesAreKept(t *testing.T) {
	m, dir := newManager(t)
	if got := m.MutedTemperatures(); len(got) != 0 {
		t.Fatalf("muted from the start: %v", got)
	}
	if err := m.SetMutedTemperatures([]string{boardSensor, cpuSensor, cpuSensor}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]string{{"temp8"}, {strings.ToUpper(cpuSensor)}} {
		if err := m.SetMutedTemperatures(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%v: %v", bad, err)
		}
	}
	// Settings of a version without mutes do not touch them.
	settings, _ := m.Settings()
	if err := m.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
	again := NewManager(config.Open(dir))
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if got := again.MutedTemperatures(); !slices.Equal(got, []string{cpuSensor, boardSensor}) {
		t.Errorf("after loading: %v", got)
	}
	if !again.TemperatureMuted(cpuSensor) || again.TemperatureMuted("other") {
		t.Error("TemperatureMuted")
	}
	// An unreadable file keeps the settings from loading, like the settings.
	os.WriteFile(filepath.Join(dir, mutesFile), []byte("{"), 0o600)
	if err := NewManager(config.Open(dir)).Load(); err == nil {
		t.Error("broken mutes file loaded")
	}
}

func TestHardwareWatcherSkipsMutedSensors(t *testing.T) {
	m, _ := newManager(t)
	w := NewHardwareWatcher(m)
	w.edac = t.TempDir()
	hot := 99.0
	w.Temperatures = func() []TempReading {
		return []TempReading{{ID: cpuSensor, Label: "Package id 0", Chip: "coretemp", Celsius: &hot}}
	}
	w.look()
	if err := m.SetMutedTemperatures([]string{cpuSensor}); err != nil {
		t.Fatal(err)
	}
	w.look()
	w.look()
	if messages := m.Messages(); len(messages) != 0 {
		t.Fatalf("muted sensor notified: %+v", messages)
	}
	// Switched on again it needs two hot looks in a row once more.
	m.SetMutedTemperatures(nil)
	w.look()
	if len(m.Messages()) != 0 {
		t.Fatal("one look after unmuting notified")
	}
	w.look()
	if len(m.Messages()) != 1 {
		t.Fatalf("messages: %+v", m.Messages())
	}
}
