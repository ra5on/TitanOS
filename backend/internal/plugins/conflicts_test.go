package plugins

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

func TestConflictingPluginsAreNeverEnabledTogether(t *testing.T) {
	m, _ := newTestManager(t)
	ctx, mounted := context.Background(), []string{"data"}
	media := filepath.Join(m.mountRoot, "data/media")
	open := plugin(t, media, "nvidia", func(p *Manifest) { p.Modules = []string{"nvidia"} })
	// The legacy driver names no conflict itself: the known pair still holds.
	legacy := plugin(t, media, "nvidia-legacy", func(p *Manifest) { p.Modules = []string{"nvidia"} })
	if err := m.Install(ctx, open, "data", "nvidia", mounted); err != nil {
		t.Fatal(err)
	}
	if err := m.Install(ctx, legacy, "data", "nvidia-legacy", mounted); !errors.Is(err, ErrConflict) {
		t.Fatalf("second NVIDIA driver installed active: %v", err)
	}
	// Stored without activation is fine; switching it on is not.
	if err := m.InstallWithActivation(ctx, legacy, "data", "nvidia-legacy", mounted, false); err != nil {
		t.Fatal(err)
	}
	if err := m.SetEnabled(ctx, "nvidia-legacy", true, mounted); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting plugin enabled: %v", err)
	}
	if slices.Contains(m.current().Enabled, "nvidia-legacy") {
		t.Fatal("conflicting plugin saved as enabled")
	}
	// After switching the other one off it can be enabled.
	if err := m.SetEnabled(ctx, "nvidia", false, mounted); err != nil {
		t.Fatal(err)
	}
	if err := m.SetEnabled(ctx, "nvidia-legacy", true, mounted); err != nil {
		t.Fatal(err)
	}
	if err := m.SetEnabled(ctx, "nvidia", true, mounted); !errors.Is(err, ErrConflict) {
		t.Fatalf("open driver enabled next to the legacy one: %v", err)
	}
}

func TestSettingsWithBothConflictingPluginsLoadOnlyTheFirst(t *testing.T) {
	m, _ := newTestManager(t)
	ctx, mounted := context.Background(), []string{"data"}
	media := filepath.Join(m.mountRoot, "data/media")
	for _, name := range []string{"first", "second"} {
		path := plugin(t, media, name, func(p *Manifest) {
			if name == "second" {
				p.Conflicts = []string{"first"}
			}
		})
		if err := m.InstallWithActivation(ctx, path, "data", name, mounted, false); err != nil {
			t.Fatal(err)
		}
	}
	// As written by a version without the check, or by hand.
	settings := m.current()
	settings.Enabled = []string{"first", "second"}
	if err := m.save(settings); err != nil {
		t.Fatal(err)
	}
	m.applied = "-"
	if err := m.Apply(ctx, mounted); err != nil {
		t.Fatal(err)
	}
	for _, p := range m.Status(mounted).Plugins {
		switch p.Name {
		case "first":
			if !p.Active {
				t.Fatalf("first plugin not loaded: %+v", p)
			}
		case "second":
			if p.Active || p.Problem != "conflict" {
				t.Fatalf("conflicting plugin loaded: %+v", p)
			}
		}
	}
}

func TestConflictingIsSymmetric(t *testing.T) {
	a, b := Manifest{Name: "a", Conflicts: []string{"b"}}, Manifest{Name: "b"}
	if !conflicting(a, b) || !conflicting(b, a) || conflicting(a, a) || conflicting(b, Manifest{Name: "c"}) {
		t.Fatal("conflict rules wrong")
	}
	if !conflicting(Manifest{Name: "nvidia-legacy"}, Manifest{Name: "nvidia"}) {
		t.Fatal("NVIDIA pair not known")
	}
}

// The catalog entries of the image: the Realtek vendor drivers are shown for
// a chip r8169 handles but never recommended for it; an unbound Pascal card
// gets the legacy NVIDIA plugin and not the open one.
func TestCatalogRulesForRealtekAndLegacyNVIDIA(t *testing.T) {
	table := readCatalogTable(filepath.Join("..", "..", "..", "plugins", "catalog"))
	if len(table) == 0 {
		t.Fatal("catalog of the repository not found")
	}
	devices := []Device{
		{Key: "pci:10ec:8125", Bus: "pci", Address: "0000:03:00.0", Driver: "r8169", modalias: "pci:v000010ECd00008125sv000010ECsd00000123bc02sc00i00"},
		{Key: "pci:10ec:8126", Bus: "pci", Address: "0000:04:00.0", Driver: "r8169", modalias: "pci:v000010ECd00008126sv000010ECsd00000123bc02sc00i00"},
		{Key: "pci:10de:1b80", Bus: "pci", Address: "0000:05:00.0", modalias: "pci:v000010DEd00001B80sv00001043sd00008591bc03sc00i00"},
	}
	var names []string
	for _, r := range recommend(table, map[string]bool{}, devices, nil) {
		names = append(names, r.Plugin)
	}
	if !slices.Equal(names, []string{"nvidia-legacy"}) {
		t.Fatalf("recommended: %v", names)
	}
	for _, m := range table {
		want := map[string]string{"r8125": "0000:03:00.0", "r8126": "0000:04:00.0", "nvidia-legacy": "0000:05:00.0"}[m.Name]
		if want == "" {
			continue
		}
		if found := matching(m.Hardware, devices, false); len(found) != 1 || found[0].Address != want {
			t.Errorf("%s shows %v", m.Name, found)
		}
	}
}
