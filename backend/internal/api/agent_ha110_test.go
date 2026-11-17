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
