package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
)

// haHouse is a Home Assistant with windows, a door, a lock, motion and two
// automations, each with long attributes – the house of "Ist alles zu?".
func haHouse(t *testing.T, extra http.HandlerFunc) *httptest.Server {
	long := strings.Repeat("x", 400)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ha-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/config":
			w.Write([]byte(`{"version":"2026.11.1","location_name":"Home"}`))
		case "/api/states":
			w.Write([]byte(`[` +
				`{"entity_id":"binary_sensor.fenster_kueche","state":"on","attributes":{"friendly_name":"Fenster Küche","device_class":"window","note":"` + long + `"}},` +
				`{"entity_id":"binary_sensor.fenster_bad","state":"off","attributes":{"friendly_name":"Fenster Bad","device_class":"window","note":"` + long + `"}},` +
				`{"entity_id":"binary_sensor.haustuer","state":"off","attributes":{"friendly_name":"Haustür","device_class":"door","note":"` + long + `"}},` +
				`{"entity_id":"binary_sensor.flur_bewegung","state":"off","attributes":{"friendly_name":"Bewegung Flur","device_class":"motion","note":"` + long + `"}},` +
				`{"entity_id":"lock.haustuer","state":"unlocked","attributes":{"friendly_name":"Schloss","note":"` + long + `"}},` +
				`{"entity_id":"automation.licht_aus","state":"on","attributes":{"friendly_name":"Licht aus","last_triggered":"2026-11-17T22:00:00+00:00","id":"1","note":"` + long + `"}},` +
				`{"entity_id":"automation.morgens","state":"off","attributes":{"friendly_name":"Morgens","last_triggered":null,"id":"2"}}` +
				`]`))
		case "/api/template":
			w.Write([]byte(""))
		default:
			if extra != nil {
				extra(w, r)
				return
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func haAgentServer(t *testing.T, ha *httptest.Server) *testServer {
	t.Helper()
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	if res, body := ts.request("PUT", "/api/v1/homeassistant/settings", map[string]any{"enabled": true, "url": ha.URL, "token": "ha-token"}); res.StatusCode != 200 {
		t.Fatalf("ha settings: %d %s", res.StatusCode, body)
	}
	return ts
}

// runAgentTool prepares and runs one tool of the agent as alex.
func runAgentTool(t *testing.T, ts *testServer, settings agentSettings, name string, args map[string]any) (map[string]any, string, error) {
	t.Helper()
	box := ts.server.newAgentToolbox(mustUser(t, ts, "alex"), settings)
	data, _ := json.Marshal(args)
	chat := &agent.Chat{ID: agent.NewID()}
	p, err := box.Prepare(context.Background(), chat, name, data)
	if err != nil {
		return nil, "", err
	}
	if p.Mode == agent.ModeAsk {
		return nil, "", errAsks
	}
	out, err := box.Execute(context.Background(), chat, p)
	if err != nil {
		return nil, "", err
	}
	var result map[string]any
	json.Unmarshal([]byte(out.Text), &result)
	return result, out.Text, nil
}

var errAsks = &asksError{}

type asksError struct{}

func (*asksError) Error() string { return "asks the person first" }

// 1.10 (V5): ha_entities gives the agent a short form – no attributes
// unless asked, the device class, an automation's last run – filters by
// device class and counts per domain. MCP keeps what it had.
func TestAgentHAEntitiesShortForm(t *testing.T) {
	ts := haAgentServer(t, haHouse(t, nil))
	settings := ts.server.agentSettings()
	settings.HA.Release = homeassistant.Release{All: true}

	result, text, err := runAgentTool(t, ts, settings, "ha_entities", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, `"attributes"`) || strings.Contains(text, strings.Repeat("x", 50)) {
		t.Fatalf("attributes without asking: %.300s", text)
	}
	by, _ := result["byDomain"].(map[string]any)
	if by["binary_sensor"] != float64(4) || by["lock"] != float64(1) || by["automation"] != float64(2) {
		t.Fatalf("byDomain: %v", by)
	}
	if !strings.Contains(text, `"lastTriggered":"2026-11-17T22:00:00+00:00"`) || !strings.Contains(text, `"deviceClass":"window"`) {
		t.Fatalf("last run or device class missing: %.600s", text)
	}
	// "Ist alles zu?": windows and doors in one call.
	result, _, err = runAgentTool(t, ts, settings, "ha_entities", map[string]any{"device_class": "window, door"})
	if err != nil || result["total"] != float64(3) {
		t.Fatalf("device_class: %v %v", result, err)
	}
	// Attributes on request.
	_, text, _ = runAgentTool(t, ts, settings, "ha_entities", map[string]any{"domain": "lock", "attributes": true})
	if !strings.Contains(text, `"attributes"`) {
		t.Fatalf("attributes on request missing: %s", text)
	}
	// A hidden kind still says so, with a device class filter as well.
	settings.HA.Release = homeassistant.Release{Domains: []string{"automation"}}
	result, _, _ = runAgentTool(t, ts, settings, "ha_entities", map[string]any{"domain": "binary_sensor", "device_class": "window"})
	if result["hidden"] != true || result["released"] != false {
		t.Fatalf("hidden kind: %v", result)
	}

	// MCP (no agent call): entries as before, with attributes and policy.
	var tool func(context.Context, json.RawMessage) (any, error)
	for _, c := range ts.server.haArea().haTools(mustUser(t, ts, "alex")) {
		if c.Name == "ha_entities" {
			tool = func(ctx context.Context, args json.RawMessage) (any, error) {
				res, err := c.Read(ctx, args)
				return res.Text, err
			}
		}
	}
	got, err := tool(homeassistant.WithRelease(context.Background(), homeassistant.Release{All: true}), json.RawMessage(`{"domain":"lock"}`))
	if err != nil {
		t.Fatal(err)
	}
	mcp, _ := got.(string)
	if !strings.Contains(mcp, `"attributes":{`) || !strings.Contains(mcp, `"policy":{`) || strings.Contains(mcp, "byDomain") {
		t.Fatalf("MCP form changed: %s", mcp)
	}
}

// 1.10 (V7): the logbook of Home Assistant – only released entities, at
// most 24 hours, and a hint (no count) when lines were left out.
func TestAgentHALogbook(t *testing.T) {
	var asked string
	ha := haHouse(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/logbook/") {
			asked = r.URL.Path + "?" + r.URL.RawQuery
			w.Write([]byte(`[{"when":"2026-11-17T02:00:00+00:00","entity_id":"automation.licht_aus","name":"Licht aus","message":"triggered"},` +
				`{"when":"2026-11-17T02:05:00+00:00","entity_id":"lock.haustuer","name":"Schloss","state":"unlocked"},` +
				`{"when":"2026-11-17T02:06:00+00:00","name":"Home Assistant","message":"started"}]`))
			return
		}
		http.NotFound(w, r)
	})
	ts := haAgentServer(t, ha)
	settings := ts.server.agentSettings()
	settings.HA.Release = homeassistant.Release{Domains: []string{"automation"}}
	result, text, err := runAgentTool(t, ts, settings, "ha_logbook", map[string]any{"since": "2026-11-17T00:00", "until": "2026-11-17T07:00"})
	if err != nil {
		t.Fatal(err)
	}
	if result["shown"] != float64(1) || result["hidden"] != true || strings.Contains(text, "Schloss") || strings.Contains(text, "started") {
		t.Fatalf("logbook: %s", text)
	}
	if !strings.HasPrefix(asked, "/api/logbook/") || !strings.Contains(asked, "end_time=") {
		t.Fatalf("asked %q", asked)
	}
	// More than 24 hours are cut to the last 24.
	result, _, err = runAgentTool(t, ts, settings, "ha_logbook", map[string]any{"since": "3d"})
	if err != nil || result["since"] == nil {
		t.Fatalf("long window: %v %v", result, err)
	}
	// An entity that is not released is refused.
	if _, _, err := runAgentTool(t, ts, settings, "ha_logbook", map[string]any{"entity": "lock.haustuer"}); err == nil {
		t.Fatal("logbook of a hidden entity")
	}
}

// 1.10 (V15): the settings of a released automation can be read before
// changing it; one that is not released cannot.
func TestAgentHAConfigRead(t *testing.T) {
	ha := haHouse(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/states/automation.licht_aus" {
			w.Write([]byte(`{"entity_id":"automation.licht_aus","state":"on","attributes":{"friendly_name":"Licht aus","id":"1"}}`))
			return
		}
		if r.URL.Path == "/api/config/automation/config/1" {
			w.Write([]byte(`{"id":"1","alias":"Licht aus","triggers":[{"trigger":"time","at":"22:00"}],"actions":[{"action":"lock.lock","target":{"entity_id":"lock.haustuer"}}]}`))
			return
		}
		http.NotFound(w, r)
	})
	ts := haAgentServer(t, ha)
	settings := ts.server.agentSettings()
	settings.HA.Release = homeassistant.Release{Domains: []string{"automation"}}
	_, text, err := runAgentTool(t, ts, settings, "ha_config_read", map[string]any{"entity": "automation.licht_aus"})
	if err != nil || !strings.Contains(text, `"at":"22:00"`) || strings.Contains(text, "lock.haustuer") || !strings.Contains(text, `"action":"lock.lock"`) || !strings.Contains(text, `"hidden":true`) {
		t.Fatalf("config: %s %v", text, err)
	}
	settings.HA.Release = homeassistant.Release{Domains: []string{"script"}}
	if _, _, err := runAgentTool(t, ts, settings, "ha_config_read", map[string]any{"entity": "automation.licht_aus"}); err == nil {
		t.Fatal("read an automation that is not released")
	}
	if _, _, err := runAgentTool(t, ts, settings, "ha_config_read", map[string]any{"entity": "light.flur"}); err == nil {
		t.Fatal("read a light as a config")
	}
}
