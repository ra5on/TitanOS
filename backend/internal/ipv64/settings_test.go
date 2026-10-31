package ipv64

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// Published apps and maintenance pages are kept by name, replaced on a
// second add, and survive a restart.
func TestPublishedAndMaintenanceKept(t *testing.T) {
	m, _, _, store := testManager(t)
	if err := m.AddPublished(Published{App: "cloud", Port: 8080, Domain: "a.ipv64.net"}); err != nil {
		t.Fatal(err)
	}
	if err := m.AddPublished(Published{App: "cloud", Port: 9090, Domain: "a.ipv64.net"}); err != nil {
		t.Fatal(err)
	}
	if err := m.AddPublished(Published{App: "wiki", Port: 8081, Domain: "b.ipv64.net"}); err != nil {
		t.Fatal(err)
	}
	if err := m.AddMaintenance(Maintenance{ID: "m1", Domain: "a.ipv64.net"}); err != nil {
		t.Fatal(err)
	}
	if p, ok := m.PublishedApp("cloud"); !ok || p.Port != 9090 || p.Records == nil {
		t.Fatalf("cloud: %+v %t", p, ok)
	}
	if _, ok := m.PublishedApp("none"); ok {
		t.Fatal("unknown app found")
	}

	again := NewManager(store)
	again.Disabled = true
	if err := again.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := again.Settings()
	if len(s.Published) != 2 || len(s.Maintenance) != 1 {
		t.Fatalf("after restart: %+v", s)
	}

	if err := m.RemovePublished("cloud"); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveMaintenance("m1"); err != nil {
		t.Fatal(err)
	}
	s = m.Settings()
	if len(s.Published) != 1 || s.Published[0].App != "wiki" || len(s.Maintenance) != 0 {
		t.Fatalf("after removing: %+v", s)
	}
}

// Settings returns a copy: changing it does not change the manager.
func TestSettingsIsACopy(t *testing.T) {
	m, _, _, _ := testManager(t)
	if err := m.AddPublished(Published{App: "cloud"}); err != nil {
		t.Fatal(err)
	}
	s := m.Settings()
	s.Published[0].App = "changed"
	if _, ok := m.PublishedApp("cloud"); !ok {
		t.Fatal("copy shared the list")
	}
}

// Keys lists every distinct key by where it is used.
func TestKeysBySource(t *testing.T) {
	m, _, _, _ := testManager(t)
	m.Fallback = func() (string, string) { return "ddns-key", "le-key" }
	if err := m.SetKey("own-key", false); err != nil {
		t.Fatal(err)
	}
	keys := m.Keys()
	if keys["own"] != "own-key" || keys["ddns"] != "ddns-key" || keys["letsencrypt"] != "le-key" || len(keys) != 3 {
		t.Fatalf("keys %v", keys)
	}
	if err := m.SetKey("", true); err != nil {
		t.Fatal(err)
	}
	m.Fallback = nil
	if len(m.Keys()) != 0 {
		t.Fatalf("keys without any: %v", m.Keys())
	}
	if _, err := m.Client(); !errors.Is(err, ErrNoKey) {
		t.Fatalf("client without key: %v", err)
	}
}

// Without readable settings nothing is changed or written.
func TestNoChangeBeforeLoad(t *testing.T) {
	m := NewManager(config.Open(t.TempDir()))
	if err := m.AddPublished(Published{App: "cloud"}); !errors.Is(err, ErrNotLoaded) {
		t.Fatalf("change before load: %v", err)
	}
	if err := m.SetKey("k", false); !errors.Is(err, ErrNotLoaded) {
		t.Fatalf("key before load: %v", err)
	}
}

// An unreadable settings file is reported, not taken as empty.
func TestLoadReportsBrokenSettings(t *testing.T) {
	store := config.Open(t.TempDir())
	if err := store.Write(settingsFile, []byte("{broken")); err != nil {
		t.Fatal(err)
	}
	m := NewManager(store)
	m.Disabled = true
	if err := m.Load(context.Background()); err == nil {
		t.Fatal("broken settings loaded")
	}
	if err := m.SetKey("k", false); !errors.Is(err, ErrNotLoaded) {
		t.Fatalf("change after a failed load: %v", err)
	}
}

// Run ends with its context and does nothing while switched off.
func TestRunEndsWithContext(t *testing.T) {
	m, _, runner, _ := testManager(t)
	m.Disabled = true
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()
	m.wake()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not end with its context")
	}
	if runner.all() != "" {
		t.Fatalf("switched off, but ran %q", runner.all())
	}
}
