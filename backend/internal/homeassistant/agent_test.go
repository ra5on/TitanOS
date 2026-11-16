package homeassistant

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestAgentReleaseIsItsOwn(t *testing.T) {
	m := &Manager{loaded: true, settings: Settings{Domains: []string{"light"}, Entities: []string{"switch.pumpe"}}}
	agent := WithRelease(context.Background(), Release{Domains: []string{"sensor"}, Entities: []string{"lock.haustuer"}})
	// The agent's list decides, not the release for MCP.
	if m.AllowedCtx(agent, "light.flur") || !m.AllowedCtx(agent, "sensor.temp") || !m.AllowedCtx(agent, "lock.haustuer") || m.AllowedCtx(agent, "switch.pumpe") {
		t.Fatal("agent release")
	}
	if !m.AllowedCtx(context.Background(), "light.flur") || m.AllowedCtx(context.Background(), "sensor.temp") {
		t.Fatal("MCP release changed")
	}
	if m.CanCallCtx(agent, "light.flur", "turn_on") || !m.CanCallCtx(agent, "lock.haustuer", "unlock") {
		t.Fatal("agent call")
	}
	if WithRelease(context.Background(), Release{}).Value(releaseKey{}) == nil || m.AllowedCtx(WithRelease(context.Background(), Release{}), "light.flur") {
		t.Fatal("an empty agent list releases nothing")
	}
	if _, err := m.Call(agent, "light", "turn_on", []string{"light.flur"}, nil); err == nil {
		t.Fatal("agent called an entity it has not released")
	}
}

// "Alles" releases every entity; a single entity can be released whatever
// its domain. Calling stays with the domains that have services here.
func TestAgentReleaseAllAndAnyDomain(t *testing.T) {
	m := &Manager{loaded: true}
	all := WithRelease(context.Background(), Release{All: true})
	if !m.AllowedCtx(all, "light.flur") || !m.AllowedCtx(all, "number.ladestrom") || m.AllowedCtx(all, "not an entity") {
		t.Fatal("all")
	}
	if m.ReleasesNothing(all) || !m.ReleasesNothing(WithRelease(context.Background(), Release{})) || m.ReleasesNothing(context.Background()) {
		t.Fatal("ReleasesNothing")
	}
	one := WithRelease(context.Background(), Release{Entities: []string{"number.ladestrom"}})
	if !m.AllowedCtx(one, "number.ladestrom") || m.AllowedCtx(one, "number.other") {
		t.Fatal("a single entity of a domain without services can be read")
	}
	if _, err := m.Call(all, "number", "set_value", []string{"number.ladestrom"}, nil); err == nil {
		t.Fatal("a domain without services here was called")
	}
}

// A connection switched off is not asked for statistics either (they go
// over the WebSocket API, the rest over REST, which checks it already).
func TestStatisticsOfAConnectionSwitchedOff(t *testing.T) {
	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		http.Error(w, "", http.StatusNotFound)
	}))
	defer srv.Close()
	m := NewManager(config.Open(t.TempDir()))
	m.Load()
	if err := m.SetSettings(Settings{URL: srv.URL, Token: "t"}); err != nil {
		t.Fatal(err)
	}
	_, err := m.Statistics(context.Background(), []string{"sensor.power"}, time.Now().Add(-time.Hour), time.Now(), "hour")
	if !errors.Is(err, ErrOff) || asked != 0 {
		t.Fatalf("switched off: %v, asked %d", err, asked)
	}
}

// The agent's own release decides what it may call and see; the policies
// of MCP do not apply to it (ha_capabilities used to list no service).
func TestAgentCapabilitiesFollowItsRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/states/light.flur":
			w.Write([]byte(`{"entity_id":"light.flur","state":"off","attributes":{}}`))
		case "/api/services":
			w.Write([]byte(`[{"domain":"light","services":{"turn_on":{},"turn_off":{}}}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	m := NewManager(config.Open(t.TempDir()))
	m.Load()
	// MCP has nothing released, but a policy for the light.
	if err := m.SetSettings(Settings{Enabled: true, URL: srv.URL, Token: "t", Policies: map[string]EntityPolicy{"light.flur": {ReadOnly: true, Aliases: []string{"MCP"}}}}); err != nil {
		t.Fatal(err)
	}
	ctx := WithRelease(context.Background(), Release{All: true})
	got, err := m.Capabilities(ctx, "light.flur")
	if err != nil {
		t.Fatal(err)
	}
	out := got.(map[string]any)
	if services := out["services"].([]map[string]any); len(services) != 2 {
		t.Fatalf("services: %v", services)
	}
	if p := out["policy"].(EntityPolicy); p.ReadOnly || len(p.Aliases) != 0 {
		t.Fatalf("policy of MCP shown to the agent: %+v", p)
	}
}
