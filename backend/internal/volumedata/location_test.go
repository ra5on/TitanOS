package volumedata

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestLocationWaitsForOriginalVolumeAcrossRestart(t *testing.T) {
	const feature, file = "agent-v2", "system/agent-volume.json"
	base := homeFixture(t, "first", "next")
	store := config.Open(t.TempDir())
	mounted := []string{"first", "next"}
	choice := "first"
	start := func() *Home {
		return &Home{MountRoot: base, Mounted: func() []string { return mounted }, Choose: func() string { return choice },
			Locations: map[string]*Location{feature: NewLocation(store, file)}}
	}
	h := start()
	v, release := h.Use(feature)
	if v != "first" {
		t.Fatal(v)
	}
	dir, err := Open(base, v, feature, mounted)
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(dir, "account.json", []byte("chat and one completed job")); err != nil {
		t.Fatal(err)
	}
	dir.Close()
	release()
	writes := store.Writes()
	if h.Volume(feature) != v || store.Writes() != writes || store.Changes() != 0 {
		t.Fatal("ordinary reads wrote to the boot disk")
	}

	mounted = []string{"next"}
	choice = "next"
	for _, boot := range []*Home{h, start()} {
		if v, done := boot.Use(feature); v != "" {
			done()
			t.Fatalf("opened a shadow store on %q", v)
		} else {
			done()
		}
	}
	if _, err := os.Stat(filepath.Join(base, "next", ".titanos")); !os.IsNotExist(err) {
		t.Fatalf("destination used while source missing: %v", err)
	}
	h = start()
	mounted = []string{"first", "next"}
	put(t, filepath.Join(base, "next", ".titanos", feature, ".adopt-account.json.part"), "incomplete")
	if v := h.Volume(feature); v != "" {
		t.Fatalf("writing during move on %q", v)
	}
	h.Wait()
	if start().Volume(feature) != "next" {
		t.Fatal("destination lost on restart")
	}
	p := filepath.Join(base, "next", ".titanos", feature)
	b, err := os.ReadFile(filepath.Join(p, "account.json"))
	if err != nil || string(b) != "chat and one completed job" {
		t.Fatalf("restored %q: %v", b, err)
	}
	if entries, err := os.ReadDir(p); err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	if _, err := os.Stat(filepath.Join(base, "first", ".titanos", feature, "account.json")); !os.IsNotExist(err) {
		t.Fatalf("old copy: %v", err)
	}
}

func TestLocationCompletesInterruptedMoveBeforeNextChoice(t *testing.T) {
	const feature, file = "agent-v2", "system/agent-volume.json"
	base := homeFixture(t, "a", "b", "c")
	store := config.Open(t.TempDir())
	if err := store.WriteJSON(file, locationRecord{Current: "a", To: "b"}); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(base, "a", ".titanos", feature, "one"), "one")
	put(t, filepath.Join(base, "b", ".titanos", feature, "two"), "two")
	mounted := []string{"a", "c"}
	h := &Home{MountRoot: base, Choose: func() string { return "c" }, Mounted: func() []string { return mounted }, Locations: map[string]*Location{feature: NewLocation(store, file)}}
	if v := h.Volume(feature); v != "" {
		t.Fatal("partial data offered", v)
	}
	mounted = []string{"a", "b", "c"}
	for range 2 {
		if v := h.Volume(feature); v != "" {
			t.Fatal("partial data offered", v)
		}
		h.Wait()
	}
	if v := h.Volume(feature); v != "c" {
		t.Fatal(v)
	}
	for _, name := range []string{"one", "two"} {
		b, err := os.ReadFile(filepath.Join(base, "c", ".titanos", feature, name))
		if err != nil || string(b) != name {
			t.Fatal(name, string(b), err)
		}
	}
}
func TestLocationRejectsConflictingHistoryAndUnreadableRecord(t *testing.T) {
	const feature, file = "agent-v2", "system/agent-volume.json"
	base := homeFixture(t, "a", "b")
	store := config.Open(t.TempDir())
	if err := store.WriteJSON(file, locationRecord{Current: "a"}); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"a", "b"} {
		put(t, filepath.Join(base, v, ".titanos", feature, "account.json"), v)
	}
	start := func() *Home {
		return &Home{MountRoot: base, Choose: func() string { return "b" }, Mounted: func() []string { return []string{"a", "b"} }, Locations: map[string]*Location{feature: NewLocation(store, file)}}
	}
	for range 2 {
		h := start()
		if v := h.Volume(feature); v != "" {
			t.Fatal("conflicting history exposed", v)
		}
		h.Wait()
		if v := h.Volume(feature); v != "" {
			t.Fatal("conflicting history exposed", v)
		}
	}
	for _, v := range []string{"a", "b"} {
		b, err := os.ReadFile(filepath.Join(base, v, ".titanos", feature, "account.json"))
		if err != nil || string(b) != v {
			t.Fatal(v, string(b), err)
		}
	}
	if err := store.WriteJSON(file, locationRecord{Current: "../unexpected"}); err != nil {
		t.Fatal(err)
	}
	if v := start().Volume(feature); v != "" {
		t.Fatal("invalid anchor accepted", v)
	}
}
