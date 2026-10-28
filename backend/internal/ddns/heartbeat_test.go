package ddns

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
)

// fakeHealth stands in for /health and the pause/resume calls of IPv64.
type fakeHealth struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeHealth) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch {
	case r.URL.Path == "/health" && auth == "hc":
		f.calls = append(f.calls, "ping")
	case strings.HasPrefix(r.URL.Path, "/api/v2/healthchecks/hc/") && auth == "key":
		f.calls = append(f.calls, strings.TrimPrefix(r.URL.Path, "/api/v2/healthchecks/hc/"))
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"token": "hc"}})
	case r.URL.Path == "/api/v2/me" && auth == "key":
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"plan": map[string]string{"name": "Free"}, "usage": map[string]any{"dyndns_updates_24h": map[string]any{"used": 1, "limit": 96}}}})
	default:
		w.WriteHeader(http.StatusUnauthorized)
	}
}

func (f *fakeHealth) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHeartbeatOptInPauseResume(t *testing.T) {
	health := &fakeHealth{}
	server := httptest.NewServer(http.HandlerFunc(health.serve))
	defer server.Close()
	store := config.Open(t.TempDir())
	m := New(store)
	m.IPv64Options = dnsprovider.Options{BaseURL: server.URL}
	m.Load()
	s := Settings{Provider: dnsprovider.Config{Type: dnsprovider.IPv64Net, Token: "key"}, Heartbeat: Heartbeat{Enabled: true}}
	if err := m.Set(s, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("switched on without a healthcheck", err)
	}
	s.Heartbeat = Heartbeat{}
	if err := m.Set(s, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.runHeartbeat(ctx)
	time.Sleep(50 * time.Millisecond)
	if len(health.seen()) != 0 {
		t.Fatal("pings while off")
	}
	if err := m.SetHeartbeat("hc", "titanos-nas", 10, true); !errors.Is(err, ErrInvalid) {
		t.Fatal("interval under 20 minutes", err)
	}
	if err := m.SetHeartbeat("hc", "titanos-nas", 30, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return slices.Contains(health.seen(), "ping") })
	st := m.Status()
	if st.Heartbeat.LastPing == nil || st.Settings.Heartbeat.Token != "" || !st.Secrets["heartbeat"] || st.Settings.Heartbeat.Check != "titanos-nas" {
		t.Fatalf("%+v", st)
	}
	// Other settings keep the token.
	s.Hostnames = []string{"nas.ipv64.de"}
	s.Heartbeat = Heartbeat{Enabled: true, Interval: 60, AutoPause: true}
	if err := m.Set(s, nil); err != nil || m.HeartbeatToken() != "hc" {
		t.Fatal("token lost", err)
	}

	m.PauseForShutdown(context.Background())
	if !slices.Contains(health.seen(), "pause") {
		t.Fatal(health.seen())
	}
	if _, err := store.Read(pausedFile); err != nil {
		t.Fatal("pause not remembered")
	}
	cancel()

	// After the start the check is resumed, then pinged.
	again := New(store)
	again.IPv64Options = m.IPv64Options
	again.Load()
	if !again.Status().Heartbeat.Paused {
		t.Fatal("pause not read")
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go again.runHeartbeat(ctx2)
	waitFor(t, func() bool { return slices.Contains(health.seen(), "resume") })
	waitFor(t, func() bool { _, err := store.Read(pausedFile); return err != nil })

	if err := again.StopHeartbeat(true); err != nil || again.HeartbeatToken() != "" || again.Status().Settings.Heartbeat.Enabled {
		t.Fatal("stop", err)
	}

	// The daily limit of the plan is learned from /me.
	again.learnLimit(context.Background(), again.settings)
}

type combinedUpdater struct{ fakeUpdater }

func (c *combinedUpdater) CombinedUpdate() bool { return true }

func TestCombinedProvidersGetBothAddresses(t *testing.T) {
	w := newWorld(t)
	up := &combinedUpdater{}
	w.m.newUpdater = func(dnsprovider.Config) (dnsprovider.Updater, error) { return up, nil }
	if err := w.m.Set(settings(), nil); err != nil {
		t.Fatal(err)
	}
	w.m.check(context.Background(), false)
	up.take()
	w.v4 = netip.MustParseAddr("198.51.100.77")
	w.m.check(context.Background(), false)
	if got := up.take(); !slices.Equal(got, []string{"nas.example.org 198.51.100.77 2001:db8:1::10"}) {
		t.Fatal(got)
	}
}

func TestRetryAfterIsObeyed(t *testing.T) {
	w := newWorld(t)
	w.up.fail = &dnsprovider.APIError{Provider: "IPv64", Status: 429, Message: "too many", RetryAfter: time.Minute}
	if err := w.m.Set(settings(), nil); err != nil {
		t.Fatal(err)
	}
	w.m.check(context.Background(), false)
	if w.m.retryAfter < 10*time.Minute {
		t.Fatal(w.m.retryAfter)
	}
}
