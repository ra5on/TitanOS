package homeassistant

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestHubConnections(t *testing.T) {
	store := config.Open(t.TempDir())
	primary := NewManager(store)
	if err := primary.Load(); err != nil {
		t.Fatal(err)
	}
	if err := primary.SetSettings(Settings{URL: "http://ha.local:8123", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	hub := NewHub(store, primary)
	if err := hub.Load(); err != nil || len(hub.All()) != 1 || hub.Get("") != primary || hub.Get(PrimaryID) != primary {
		t.Fatalf("only the first: %v %v", hub.All(), err)
	}
	second, err := hub.Add("Ferienwohnung")
	if err != nil {
		t.Fatal(err)
	}
	if second.Name() != "Ferienwohnung" || hub.Get(second.ID()) != second || !connID.MatchString(second.ID()) {
		t.Fatalf("second %q %q", second.ID(), second.Name())
	}
	if err := second.SetSettings(Settings{Name: "Ferienwohnung", URL: "http://fewo.local:8123", Token: "u", MQTT: MQTT{Host: "broker", Interval: 60}}); err != nil {
		t.Fatal(err)
	}

	// After a restart of titand: both are back, the first unchanged.
	again := NewManager(store)
	again.Load()
	hub2 := NewHub(store, again)
	if err := hub2.Load(); err != nil || len(hub2.All()) != 2 {
		t.Fatalf("loaded %v %v", hub2.All(), err)
	}
	if s, _ := again.Settings(); s.URL != "http://ha.local:8123" || s.Name != "" {
		t.Fatalf("first %+v", s)
	}
	if s, _ := hub2.Get(second.ID()).Settings(); s.URL != "http://fewo.local:8123" || s.MQTT.Interval != 60 {
		t.Fatalf("second %+v", s)
	}

	if err := hub.Remove(PrimaryID); err != ErrPrimary {
		t.Fatalf("first removed: %v", err)
	}
	if err := hub.Remove("deadbeef"); err != ErrNoConnection {
		t.Fatalf("unknown: %v", err)
	}
	for len(hub.All()) < MaxConnections {
		if _, err := hub.Add(""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := hub.Add("one too many"); err != ErrTooMany {
		t.Fatalf("too many: %v", err)
	}
	if err := second.SetSettings(Settings{Name: "line\nbreak"}); err == nil {
		t.Fatal("name with a line break")
	}
}

func TestHubRunFollowsConnections(t *testing.T) {
	store := config.Open(t.TempDir())
	primary := NewManager(store)
	primary.Load()
	hub := NewHub(store, primary)
	hub.Load()
	var mu sync.Mutex
	running := map[string]bool{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		hub.Run(ctx, func(ctx context.Context, m *Manager) {
			mu.Lock()
			running[m.ID()] = true
			mu.Unlock()
			<-ctx.Done()
			mu.Lock()
			delete(running, m.ID())
			mu.Unlock()
		})
	}()
	wait := func(want int) {
		t.Helper()
		for i := 0; i < 200; i++ {
			mu.Lock()
			n := len(running)
			mu.Unlock()
			if n == want {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("running %v, want %d", running, want)
	}
	wait(1)
	second, _ := hub.Add("zwei")
	wait(2)
	hub.Remove(second.ID())
	wait(1)
	cancel()
	<-done
	wait(0)
	// Buttons of all connections share one runner and its limits.
	if primary.commandRunnerFor(nil) != second.commandRunnerFor(nil) {
		t.Fatal("two runners")
	}
}

// A list or a connection that cannot be read is never written over: the
// other connections stay listed when one is added or removed.
func TestHubKeepsWhatItCouldNotRead(t *testing.T) {
	dir := t.TempDir()
	store := config.Open(dir)
	primary := NewManager(store)
	primary.Load()
	hub := NewHub(store, primary)
	hub.Load()
	a, err := hub.Add("A")
	if err != nil {
		t.Fatal(err)
	}
	b, err := hub.Add("B")
	if err != nil {
		t.Fatal(err)
	}
	// After a restart, the file of B does not read.
	if err := os.WriteFile(filepath.Join(dir, "homeassistant", "connections", b.ID()+".json"), []byte("{kaputt"), 0o600); err != nil {
		t.Fatal(err)
	}
	hub = NewHub(store, NewManager(store))
	hub.Load()
	c, err := hub.Add("C")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.Remove(c.ID()); err != nil {
		t.Fatal(err)
	}
	var list connectionList
	if err := store.ReadJSON(connectionsFile, &list); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(list.Connections, []string{a.ID(), b.ID()}) {
		t.Fatalf("list: %v, want %s and %s", list.Connections, a.ID(), b.ID())
	}
	// The list itself does not read: nothing is added or removed over it.
	os.WriteFile(filepath.Join(dir, "homeassistant", "connections.json"), []byte("{kaputt"), 0o600)
	hub = NewHub(store, NewManager(store))
	if err := hub.Load(); err == nil {
		t.Fatal("an unreadable list was taken for none")
	}
	if _, err := hub.Add("D"); err == nil {
		t.Fatal("added over an unreadable list")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "homeassistant", "connections.json")); string(data) != "{kaputt" {
		t.Fatalf("list written over: %s", data)
	}
}
