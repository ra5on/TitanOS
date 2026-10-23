package homeassistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// fakeHA answers like the REST API of Home Assistant.
type fakeHA struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeHA) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer good-token" {
		http.Error(w, "401: Unauthorized", http.StatusUnauthorized)
		return
	}
	body, _ := io.ReadAll(r.Body)
	switch {
	case r.URL.Path == "/api/config":
		w.Write([]byte(`{"version":"2026.10.1","location_name":"Zuhause"}`))
	case r.URL.Path == "/api/states":
		w.Write([]byte(`[
			{"entity_id":"light.kitchen","state":"off","attributes":{"friendly_name":"Küche"}},
			{"entity_id":"lock.front_door","state":"locked","attributes":{}},
			{"entity_id":"switch.heater","state":"on","attributes":{}},
			{"entity_id":"camera.yard","state":"idle","attributes":{}}]`))
	case r.URL.Path == "/api/template":
		w.Write([]byte("light.kitchen\tKüche\nlock.front_door\tFlur\nswitch.heater\tNone\n"))
	case r.URL.Path == "/api/services" && r.Method == http.MethodGet:
		w.Write([]byte(`[{"domain":"notify","services":{"mobile_app_pixel":{},"persistent_notification":{}}},{"domain":"light","services":{"turn_on":{}}}]`))
	case strings.HasPrefix(r.URL.Path, "/api/services/"):
		f.mu.Lock()
		f.calls = append(f.calls, r.URL.Path+" "+string(body))
		f.mu.Unlock()
		w.Write([]byte(`[]`))
	case r.URL.Path == "/redirect":
		http.Redirect(w, r, "http://elsewhere.example/", http.StatusFound)
	default:
		http.NotFound(w, r)
	}
}

func newManager(t *testing.T, s Settings) (*Manager, *fakeHA, *httptest.Server) {
	t.Helper()
	fake := &fakeHA{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	m := NewManager(config.Open(t.TempDir()))
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if s.URL == "" {
		s.URL = srv.URL
	}
	if err := m.SetSettings(s); err != nil {
		t.Fatal(err)
	}
	return m, fake, srv
}

func TestCheckAndTokenStaysOnTheNAS(t *testing.T) {
	m, _, _ := newManager(t, Settings{Enabled: true, Token: "good-token"})
	status, err := m.Check(context.Background())
	if err != nil || status.Version != "2026.10.1" || status.Location != "Zuhause" {
		t.Fatalf("%+v %v", status, err)
	}
	s, set := m.Settings()
	if s.Token != "" || !set["token"] {
		t.Fatal("token returned")
	}
	// Saving without a token keeps it; a wrong one is reported.
	if err := m.SetSettings(s); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Check(context.Background()); err != nil {
		t.Fatalf("stored token lost: %v", err)
	}
	s.Token = "bad"
	m.SetSettings(s)
	if _, err := m.Check(context.Background()); !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "401") {
		t.Fatalf("wrong token: %v", err)
	}
	if err := m.SetSettings(Settings{URL: s.URL}, "token"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Check(context.Background()); !errors.Is(err, ErrOff) {
		t.Fatalf("switched off: %v", err)
	}
}

func TestOnlyReleasedEntities(t *testing.T) {
	m, fake, _ := newManager(t, Settings{Enabled: true, Token: "good-token", Domains: []string{"light"}, Entities: []string{"lock.front_door"}})
	states, err := m.States(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range states {
		ids = append(ids, s.EntityID+"@"+s.Area)
	}
	if strings.Join(ids, ",") != "light.kitchen@Küche,lock.front_door@Flur" {
		t.Fatalf("states %v", ids)
	}
	if states[0].Name() != "Küche" || states[1].Name() != "lock.front_door" {
		t.Fatal("names")
	}

	ctx := context.Background()
	if _, err := m.Call(ctx, "light", "turn_on", []string{"light.kitchen"}, map[string]any{"brightness_pct": 40}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		domain, service string
		entities        []string
		data            map[string]any
	}{
		{"switch", "turn_off", []string{"switch.heater"}, nil},                                        // domain not released
		{"light", "turn_on", []string{"lock.front_door"}, nil},                                        // entity of another domain
		{"camera", "snapshot", []string{"camera.yard"}, nil},                                          // never offered
		{"homeassistant", "restart", []string{"light.kitchen"}, nil},                                  // never offered
		{"light", "turn_on", []string{"light.kitchen"}, map[string]any{"entity_id": "switch.heater"}}, // smuggled target
		{"light", "turn_on/../../../config", []string{"light.kitchen"}, nil},
	} {
		if _, err := m.Call(ctx, c.domain, c.service, c.entities, c.data); err == nil {
			t.Errorf("%+v was called", c)
		}
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.calls) != 1 || !strings.Contains(fake.calls[0], `"entity_id":["light.kitchen"]`) || !strings.Contains(fake.calls[0], `"brightness_pct":40`) {
		t.Fatalf("calls %v", fake.calls)
	}
}

func TestNotifyServicesAndMessage(t *testing.T) {
	m, fake, _ := newManager(t, Settings{Enabled: true, Token: "good-token"})
	services, err := m.NotifyServices(context.Background())
	if err != nil || strings.Join(services, ",") != "notify.mobile_app_pixel,notify.persistent_notification" {
		t.Fatalf("%v %v", services, err)
	}
	if err := m.Notify(context.Background(), "notify.mobile_app_pixel", "Hi", "Text", map[string]any{"url": "/assistant"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Notify(context.Background(), "light.turn_on", "x", "y", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-notify service: %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	var body map[string]any
	_, raw, _ := strings.Cut(fake.calls[0], " ")
	json.Unmarshal([]byte(raw), &body)
	if !strings.HasPrefix(fake.calls[0], "/api/services/notify/mobile_app_pixel") || body["title"] != "Hi" || body["data"].(map[string]any)["url"] != "/assistant" {
		t.Fatalf("calls %v", fake.calls)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	m, _, _ := newManager(t, Settings{Enabled: true, Token: "good-token"})
	if err := m.do(context.Background(), http.MethodGet, "/redirect", nil, nil); !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("redirect: %v", err)
	}
}

func TestFingerprintPinsTheCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(&fakeHA{})
	defer srv.Close()
	sum := sha256.Sum256(srv.Certificate().Raw)
	good := hex.EncodeToString(sum[:])
	m := NewManager(config.Open(t.TempDir()))
	m.Load()
	// Without a fingerprint the self-signed certificate is refused.
	if err := m.SetSettings(Settings{Enabled: true, URL: srv.URL, Token: "good-token"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Check(context.Background()); err == nil {
		t.Fatal("self-signed accepted without fingerprint")
	}
	// The right one in colon notation works, another does not.
	colons := strings.ToUpper(good[:2]) + ":" + good[2:]
	if err := m.SetSettings(Settings{Enabled: true, URL: srv.URL, Token: "good-token", Fingerprint: colons}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Check(context.Background()); err != nil {
		t.Fatalf("pinned: %v", err)
	}
	other := strings.Repeat("ab", 32)
	m.SetSettings(Settings{Enabled: true, URL: srv.URL, Token: "good-token", Fingerprint: other})
	if _, err := m.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("wrong fingerprint: %v", err)
	}
}

func TestSettingsAreChecked(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(config.Open(dir))
	m.Load()
	for _, s := range []Settings{
		{Enabled: true, URL: "http://ha:8123"},                       // no token
		{URL: "ftp://ha"},                                            // scheme
		{URL: "http://user:pw@ha:8123"},                              // credentials in the address
		{URL: "http://ha:8123/some/path"},                            // path
		{URL: "http://ha:8123", Domains: []string{"homeassistant"}},  // domain that cannot be released
		{URL: "http://ha:8123", Entities: []string{"light.Kitchen"}}, // not an entity ID
		{URL: "http://ha:8123", Entities: []string{"camera.yard"}},   // domain that cannot be released
		{URL: "http://ha:8123", Fingerprint: "1234"},                 // fingerprint
		{URL: "http://ha:8123", Token: "two words"},                  // token
	} {
		if err := m.SetSettings(s); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v accepted: %v", s, err)
		}
	}
	if err := m.SetSettings(Settings{URL: "http://ha:8123/", Domains: []string{"light", "light"}}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, settingsFile))
	var stored Settings
	json.Unmarshal(data, &stored)
	if stored.URL != "http://ha:8123" || len(stored.Domains) != 1 || stored.Entities == nil {
		t.Fatalf("stored %s", data)
	}
}

func TestChangedIsSignalled(t *testing.T) {
	m, _, srv := newManager(t, Settings{Enabled: true, Token: "good-token"})
	changed := m.Changed()
	m.SetSettings(Settings{Enabled: true, URL: srv.URL, Token: "good-token", Domains: []string{"light"}})
	select {
	case <-changed:
	default:
		t.Fatal("no signal")
	}
}
