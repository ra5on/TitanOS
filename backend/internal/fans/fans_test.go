package fans

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// fakeSys builds /sys/class/hwmon/hwmon0 (it87, two pwm outputs, the second
// without manual mode) and hwmon1 (nvme, ignored).
func fakeSys(t *testing.T) string {
	sys := t.TempDir()
	write := func(rel, value string) {
		path := filepath.Join(sys, rel)
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(value+"\n"), 0o644)
	}
	write("devices/platform/it87.656/hwmon/hwmon0/name", "it8686")
	write("devices/platform/it87.656/hwmon/hwmon0/pwm1", "128")
	write("devices/platform/it87.656/hwmon/hwmon0/pwm1_enable", "2")
	write("devices/platform/it87.656/hwmon/hwmon0/fan1_input", "900")
	write("devices/platform/it87.656/hwmon/hwmon0/fan1_label", "CPU_FAN")
	write("devices/platform/it87.656/hwmon/hwmon0/pwm2", "100")
	write("devices/pci0000:00/nvme/hwmon/hwmon1/name", "nvme")
	write("devices/pci0000:00/nvme/hwmon/hwmon1/pwm1", "0")
	write("devices/pci0000:00/nvme/hwmon/hwmon1/pwm1_enable", "2")
	os.MkdirAll(filepath.Join(sys, "class/hwmon"), 0o755)
	os.Symlink("../../devices/platform/it87.656/hwmon/hwmon0", filepath.Join(sys, "class/hwmon/hwmon0"))
	os.Symlink("../../devices/pci0000:00/nvme/hwmon/hwmon1", filepath.Join(sys, "class/hwmon/hwmon1"))
	return sys
}

func fixture(t *testing.T) (*Manager, string) {
	sys := fakeSys(t)
	m := New(config.Open(t.TempDir()))
	m.sys, m.runtime = sys, filepath.Join(t.TempDir(), "original.json")
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m, filepath.Join(sys, "devices/platform/it87.656/hwmon/hwmon0")
}

func TestDiscoversOnlyManualCapableFans(t *testing.T) {
	m, _ := fixture(t)
	fans := m.Fans()
	if len(fans) != 1 || fans[0].Label != "CPU_FAN" || fans[0].Chip != "it8686" || *fans[0].RPM != 900 || fans[0].Percent != 50 || fans[0].Control.Mode != "bios" {
		t.Fatalf("fans %+v", fans)
	}
}

func TestCurveFixedAndBackToBIOS(t *testing.T) {
	m, dir := fixture(t)
	id := m.Fans()[0].ID
	temp := 40.0
	sensorOK := true
	m.Temperatures = func() map[string]float64 {
		if !sensorOK {
			return nil
		}
		return map[string]float64{"cpu": temp}
	}
	// Nothing is touched while all fans are left to the BIOS.
	m.apply()
	if read(filepath.Join(dir, "pwm1_enable")) != "2" {
		t.Fatal("BIOS fan taken over")
	}
	curveCtl := Control{ID: id, Mode: "curve", Sensor: "cpu", Points: []Point{{30, 20}, {60, 60}, {80, 100}}, Min: 30}
	if err := m.Set(Settings{Fans: []Control{curveCtl}}); err != nil {
		t.Fatal(err)
	}
	m.apply()
	if read(filepath.Join(dir, "pwm1_enable")) != "1" || read(filepath.Join(dir, "pwm1")) != "84" { // 33 % of 255
		t.Fatalf("curve at 40 °C: enable %s pwm %s", read(filepath.Join(dir, "pwm1_enable")), read(filepath.Join(dir, "pwm1")))
	}
	temp = 39 // within the hysteresis: stays
	m.apply()
	if got := read(filepath.Join(dir, "pwm1")); got != "84" {
		t.Fatalf("hysteresis: %s", got)
	}
	temp = 25 // below the curve: the minimum holds
	m.apply()
	if got := read(filepath.Join(dir, "pwm1")); got != "77" { // minimum 30 %
		t.Fatalf("minimum: %s", got)
	}
	temp = 90
	m.apply()
	if read(filepath.Join(dir, "pwm1")) != "255" {
		t.Fatal("full speed at the top of the curve")
	}
	// A sensor that cannot be read means full speed and a problem shown.
	temp, sensorOK = 40, false
	m.sensors = map[string]float64{}
	os.WriteFile(filepath.Join(dir, "pwm1"), []byte("50"), 0o644)
	m.applied = map[string]int{}
	m.apply()
	if read(filepath.Join(dir, "pwm1")) != "255" || m.Fans()[0].Error == "" {
		t.Fatal("failed sensor did not mean full speed")
	}
	// The original mode survives a restart of titand.
	again := New(m.store)
	again.sys, again.runtime = m.sys, m.runtime
	again.Load()
	if again.original[id] != "2" {
		t.Fatalf("original mode lost: %v", again.original)
	}
	// Back to the BIOS: the fan gets its old mode.
	if err := m.Set(Settings{Fans: nil}); err != nil {
		t.Fatal(err)
	}
	m.apply()
	if read(filepath.Join(dir, "pwm1_enable")) != "2" {
		t.Fatal("not given back to the BIOS")
	}
	// Fixed, then titand stops: back to the BIOS as well.
	m.Set(Settings{Fans: []Control{{ID: id, Mode: "fixed", Percent: 60, Min: 30}}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	// Run applies right away; a slow machine may need longer than a moment.
	for deadline := time.Now().Add(5 * time.Second); read(filepath.Join(dir, "pwm1")) != "153" && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if read(filepath.Join(dir, "pwm1")) != "153" {
		t.Fatalf("fixed: %s", read(filepath.Join(dir, "pwm1")))
	}
	cancel()
	<-done
	if read(filepath.Join(dir, "pwm1_enable")) != "2" {
		t.Fatal("stopping titand left the fan in manual mode")
	}
}

func TestValidation(t *testing.T) {
	m, _ := fixture(t)
	id := m.Fans()[0].ID
	for _, c := range []Control{
		{ID: "unknown", Mode: "fixed", Percent: 50},
		{ID: id, Mode: "fixed", Percent: 10},
		{ID: id, Mode: "fixed", Percent: 50, Min: 10},
		{ID: id, Mode: "curve", Sensor: "cpu", Points: []Point{{30, 50}}},
		{ID: id, Mode: "curve", Sensor: "cpu", Points: []Point{{30, 80}, {60, 40}}},
		{ID: id, Mode: "curve", Points: []Point{{30, 40}, {60, 80}}},
		{ID: id, Mode: "turbo"},
	} {
		if err := m.Set(Settings{Fans: []Control{c}}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %+v", c)
		}
	}
}

func TestDevelopmentModeOnlyReads(t *testing.T) {
	m, dir := fixture(t)
	m.Disabled = true
	m.Set(Settings{Fans: []Control{{ID: m.Fans()[0].ID, Mode: "fixed", Percent: 100}}})
	m.apply()
	if read(filepath.Join(dir, "pwm1_enable")) != "2" || read(filepath.Join(dir, "pwm1")) != "128" {
		t.Fatal("development mode wrote to the fan")
	}
}
