package sysdata

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestSettingsAreStoredOnlyWhenTheyChange(t *testing.T) {
	store := config.Open(t.TempDir())
	m := New(store)
	if err := m.Load(); err != nil || m.Volume() != "" {
		t.Fatalf("without a file: %v %q", err, m.Volume())
	}
	before := store.Changes()
	if err := m.Update(func(s *Settings) { s.Overrides[Logs] = "" }); err != nil || store.Changes() != before {
		t.Fatalf("nothing changed, yet written: %v", err)
	}
	err := m.Update(func(s *Settings) {
		s.Volume = "fast"
		s.Overrides[Logs] = "other"
		AddPending(s, Logs, "Backup")
		AddPending(s, Logs, "Backup")
		AddPending(s, Config, "Backup") // copies of /config are never moved
	})
	if err != nil {
		t.Fatal(err)
	}
	again := New(store)
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if again.Volume() != "fast" || again.Override(Logs) != "other" || !slices.Equal(again.Pending(Logs), []string{"Backup"}) || again.Pending(Config) != nil {
		t.Fatalf("read back: %+v", again.Settings())
	}
	if err := again.Settle(Logs, "Backup"); err != nil || again.Pending(Logs) != nil {
		t.Fatalf("settle: %v %v", err, again.Pending(Logs))
	}
	for _, bad := range []func(*Settings){
		func(s *Settings) { s.Volume = "../x" },
		func(s *Settings) { s.Overrides[Activity] = "fast" }, // kept in its own settings
		func(s *Settings) { s.Overrides[Logs] = "a/b" },
	} {
		if err := again.Update(bad); err == nil {
			t.Fatal("invalid settings stored")
		}
	}
	if again.Volume() != "fast" {
		t.Fatal("a refused change altered the settings")
	}
}

func TestSizeIsMeasuredInTheBackgroundAndNeverOnASleepingVolume(t *testing.T) {
	base := t.TempDir()
	for _, volume := range []string{"fast", "Backup"} {
		dir := filepath.Join(base, volume, ".titanos", "logs")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "a"), []byte("12345"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := New(config.Open(t.TempDir()))
	m.MountRoot = base
	m.Mounted = func() []string { return []string{"fast", "Backup"} }
	m.Asleep = func(volume string) bool { return volume == "Backup" }
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	if _, at := m.Size("fast", Logs); !at.IsZero() {
		t.Fatal("measured in the caller")
	}
	m.Wait()
	if bytes, at := m.Size("fast", Logs); bytes != 5 || !at.Equal(now) {
		t.Fatalf("size: %d %v", bytes, at)
	}
	m.Size("Backup", Logs)
	m.Wait()
	if _, at := m.Size("Backup", Logs); !at.IsZero() {
		t.Fatal("a sleeping volume was read")
	}
	if _, at := m.Size("gone", Logs); !at.IsZero() {
		t.Fatal("an unmounted volume was read")
	}
	// The value is kept and measured again only after a while.
	if err := os.WriteFile(filepath.Join(base, "fast", ".titanos", "logs", "b"), []byte("123"), 0600); err != nil {
		t.Fatal(err)
	}
	m.Size("fast", Logs)
	m.Wait()
	if bytes, _ := m.Size("fast", Logs); bytes != 5 {
		t.Fatalf("measured again too early: %d", bytes)
	}
	now = now.Add(sizeMaxAge + time.Second)
	m.Size("fast", Logs)
	m.Wait()
	if bytes, _ := m.Size("fast", Logs); bytes != 8 {
		t.Fatalf("not measured again: %d", bytes)
	}
}
