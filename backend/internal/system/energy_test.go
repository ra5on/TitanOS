package system

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func energyFixture(t *testing.T) (*EnergyManager, string) {
	t.Helper()
	root := t.TempDir()
	m := NewEnergyManager(config.Open(filepath.Join(root, "config")))
	m.sys = filepath.Join(root, "sys")
	for _, policy := range []string{"policy0", "policy4"} {
		dir := filepath.Join(m.sys, "devices/system/cpu/cpufreq", policy)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		for name, value := range map[string]string{
			"scaling_available_governors": "performance powersave userspace", "scaling_governor": "performance", "scaling_driver": "intel_pstate",
			"energy_performance_available_preferences": "performance balance_power power", "energy_performance_preference": "performance",
		} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(value+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m, root
}

func TestEnergyDiscoverApplyAndRestoreAtBoot(t *testing.T) {
	m, _ := energyFixture(t)
	status := m.Status()
	if len(status.Controls) != 2 || !slices.Equal(status.Controls[0].Choices, []string{"performance", "powersave"}) {
		t.Fatalf("unsupported choices: %+v", status)
	}
	wanted := EnergySettings{Governor: "powersave", Preference: "balance_power"}
	if err := m.Set(wanted); err != nil {
		t.Fatal(err)
	}
	for _, c := range m.Status().Controls {
		if c.Mixed || c.Current == "performance" {
			t.Fatalf("not applied to all policies: %+v", c)
		}
		for _, path := range c.paths {
			if err := m.write(path, "performance"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if got := m.Status(); got.Settings != wanted || got.Controls[0].Current != "powersave" || got.Controls[1].Current != "balance_power" {
		t.Fatalf("not restored: %+v", got)
	}
}

func TestEnergyRejectUnsupportedAndRollbackPartialFailure(t *testing.T) {
	m, root := energyFixture(t)
	for _, choice := range []EnergySettings{{Governor: "../performance"}, {Platform: "low-power"}, {Preference: "custom"}} {
		if err := m.Set(choice); !errors.Is(err, ErrEnergyChoice) {
			t.Fatalf("accepted %+v: %v", choice, err)
		}
	}
	write := m.write
	m.write = func(path, value string) error {
		if filepath.Base(path) == "energy_performance_preference" && value == "power" {
			return errors.New("driver rejected preference")
		}
		return write(path, value)
	}
	if err := m.Set(EnergySettings{Governor: "powersave", Preference: "power"}); err == nil {
		t.Fatal("driver failure ignored")
	}
	for _, c := range m.Status().Controls {
		for _, path := range c.paths {
			if energyRead(path) != "performance" {
				t.Fatal("partial change not restored")
			}
		}
	}
	m.write = write
	if err := os.MkdirAll(filepath.Join(root, "config", energyFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(EnergySettings{Governor: "powersave"}); err == nil {
		t.Fatal("configuration failure ignored")
	}
	if m.Status().Controls[0].Current != "performance" {
		t.Fatal("failed save left a changed governor")
	}
}

func TestEnergyCommonChoicesAndDevelopmentMode(t *testing.T) {
	m, _ := energyFixture(t)
	path := filepath.Join(m.sys, "devices/system/cpu/cpufreq/policy4/scaling_available_governors")
	if err := os.WriteFile(path, []byte("performance\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(m.Status().Controls[0].Choices, []string{"performance"}) {
		t.Fatal("choices not supported on every policy offered")
	}
	m.Disabled = true
	m.write = func(string, string) error { t.Fatal("development mode wrote hardware"); return nil }
	if err := m.Set(EnergySettings{Governor: "performance"}); err == nil {
		t.Fatal("development mode accepted mutation")
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	m.sys = t.TempDir()
	if len(m.Status().Controls) != 0 {
		t.Fatal("made up profiles without hardware")
	}
}
