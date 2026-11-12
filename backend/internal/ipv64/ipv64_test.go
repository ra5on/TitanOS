package ipv64

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

type fakeRunner struct {
	calls []string
	err   error
}

func (r *fakeRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return "", r.err
}

func testManager(t *testing.T, store *config.Store) (*Manager, *fakeRunner) {
	t.Helper()
	runner := &fakeRunner{}
	m := NewManager(store)
	m.runner = runner
	m.exists = func(string) bool { return false }
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return m, runner
}

// Published apps are kept by name, replaced on a second add, and survive
// a restart.
func TestPublishedKept(t *testing.T) {
	store := config.Open(t.TempDir())
	m, _ := testManager(t, store)
	for _, p := range []Published{{App: "cloud", Port: 8080, Domain: "a.ipv64.net"}, {App: "cloud", Port: 9090, Domain: "a.ipv64.net"}, {App: "wiki", Port: 8081, Domain: "b.ipv64.net"}} {
		if err := m.AddPublished(p); err != nil {
			t.Fatal(err)
		}
	}
	if p, ok := m.PublishedApp("cloud"); !ok || p.Port != 9090 || p.Records == nil {
		t.Fatalf("cloud: %+v %t", p, ok)
	}
	if _, ok := m.PublishedApp("none"); ok {
		t.Fatal("unknown app found")
	}
	again, _ := testManager(t, store)
	if _, ok := again.PublishedApp("wiki"); !ok {
		t.Fatal("wiki lost by a restart")
	}
	if err := m.RemovePublished("cloud"); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.PublishedApp("cloud"); ok {
		t.Fatal("cloud still there")
	}
}

func TestNotLoadedRefusesChanges(t *testing.T) {
	m := NewManager(config.Open(t.TempDir()))
	if err := m.AddPublished(Published{App: "cloud"}); !errors.Is(err, ErrNotLoaded) {
		t.Fatalf("add before load: %v", err)
	}
}

// oldSettings is /config/ipv64/settings.json as 0.76 wrote it with the
// whole account area in use.
const oldSettings = `{
  "apiKey": "own-key",
  "notify": {"enabled": true, "failover": true, "cdnTraffic": true, "healthchecks": true},
  "blocklist": {"enabled": true},
  "wireguard": {"enabled": true, "router": "r1", "gateway": "g1", "peer": "p1", "publicKey": "pub", "addresses": ["10.9.0.2/32"]},
  "published": [{"app": "immich", "port": 2283, "domain": "nas.ipv64.de", "cdn": "c1", "records": ["rec1"], "at": "2026-11-01T10:00:00Z"}],
  "maintenance": [{"id": "m1", "domain": "nas.ipv64.de", "check": "c", "checkName": "titanos-nas", "offlinePage": "o1", "policy": "p1", "records": ["r"], "at": "2026-11-01T10:00:00Z"}]
}`

// Settings of the removed account area do not stop the start, are not
// used, and are still in the file after this version wrote it: an older
// version of the app finds them after a way back.
func TestOldSettingsAreIgnoredAndKept(t *testing.T) {
	store := config.Open(t.TempDir())
	if err := store.Write(settingsFile, []byte(oldSettings)); err != nil {
		t.Fatal(err)
	}
	before := store.Changes()
	m, runner := testManager(t, store)
	if store.Changes() != before {
		t.Fatal("loading wrote to /config")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("commands without a leftover tunnel: %v", runner.calls)
	}
	if p, ok := m.PublishedApp("immich"); !ok || p.CDN != "c1" {
		t.Fatalf("published app not read: %+v", p)
	}
	// The own key of the removed area is only the last source.
	if key, source := m.Key(); key != "own-key" || source != "legacy" {
		t.Fatalf("key %q from %q", key, source)
	}
	m.Fallback = func() (string, string) { return "", "le-key" }
	if key, source := m.Key(); key != "le-key" || source != "letsencrypt" {
		t.Fatalf("key %q from %q", key, source)
	}
	if err := m.AddPublished(Published{App: "wiki", Port: 8081, Domain: "nas.ipv64.de"}); err != nil {
		t.Fatal(err)
	}
	var file map[string]json.RawMessage
	if err := store.ReadJSON(settingsFile, &file); err != nil {
		t.Fatal(err)
	}
	var old map[string]json.RawMessage
	if err := json.Unmarshal([]byte(oldSettings), &old); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"apiKey", "notify", "blocklist", "wireguard", "maintenance"} {
		var a, b any
		if json.Unmarshal(file[field], &a) != nil || json.Unmarshal(old[field], &b) != nil {
			t.Fatalf("%s not readable", field)
		}
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		if string(ja) != string(jb) {
			t.Fatalf("%s changed: %s", field, ja)
		}
	}
	var published []Published
	if err := json.Unmarshal(file["published"], &published); err != nil || len(published) != 2 {
		t.Fatalf("published: %v %+v", err, published)
	}
}

// A file that is no JSON is reported and nothing is written over it.
func TestBrokenSettings(t *testing.T) {
	store := config.Open(t.TempDir())
	if err := store.Write(settingsFile, []byte("{broken")); err != nil {
		t.Fatal(err)
	}
	m := NewManager(store)
	if err := m.Load(context.Background()); err == nil {
		t.Fatal("broken file loaded")
	}
	if err := m.AddPublished(Published{App: "cloud"}); !errors.Is(err, ErrNotLoaded) {
		t.Fatalf("add over a broken file: %v", err)
	}
}

// The tunnel an older version left running is removed once at the start,
// never in development mode.
func TestLeftoverTunnelRemoved(t *testing.T) {
	m := NewManager(config.Open(t.TempDir()))
	runner := &fakeRunner{}
	m.runner = runner
	m.exists = func(iface string) bool { return iface == "ipv64wg" }
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 || runner.calls[0] != "ip link del dev ipv64wg" {
		t.Fatalf("calls: %v", runner.calls)
	}
	dev := NewManager(config.Open(t.TempDir()))
	devRunner := &fakeRunner{}
	dev.runner, dev.Disabled = devRunner, true
	dev.exists = func(string) bool { return true }
	if err := dev.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(devRunner.calls) != 0 {
		t.Fatalf("development mode ran: %v", devRunner.calls)
	}
}
