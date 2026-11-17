package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
)

// haWithAutomations is a Home Assistant with two automations, a script and
// a window contact – the house of the export of 17 Nov 2026.
func haWithAutomations(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ha-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/config":
			w.Write([]byte(`{"version":"2026.11.1","location_name":"Home"}`))
		case "/api/states":
			w.Write([]byte(`[{"entity_id":"automation.licht_aus","state":"on","attributes":{"friendly_name":"Licht aus"}},` +
				`{"entity_id":"automation.morgens","state":"on","attributes":{"friendly_name":"Morgens"}},` +
				`{"entity_id":"script.toggle_room","state":"off","attributes":{}},` +
				`{"entity_id":"binary_sensor.fenster","state":"off","attributes":{}}]`))
		case "/api/template":
			w.Write([]byte(""))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// 1.09 (export of 17 Nov 2026, "wieviele automationen habe ich" → "0"):
// automations could not be released under "Auswahl" (no such kind), and
// ha_entities answered a hidden domain with total 0 like an empty one. Now
// automations are a kind of their own, and the result says whether the
// domain is released at all.
func TestAgentHAEntitiesTellsHiddenFromNone(t *testing.T) {
	ha := haWithAutomations(t)
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetAll})
	if res, body := ts.request("PUT", "/api/v1/homeassistant/settings", map[string]any{"enabled": true, "url": ha.URL, "token": "ha-token"}); res.StatusCode != 200 {
		t.Fatalf("ha settings: %d %s", res.StatusCode, body)
	}
	user := mustUser(t, ts, "alex")
	run := func(release homeassistant.Release, args map[string]any) map[string]any {
		t.Helper()
		settings := ts.server.agentSettings()
		settings.HA.Release = release
		box := ts.server.newAgentToolbox(user, settings)
		data, _ := json.Marshal(args)
		chat := &agent.Chat{ID: agent.NewID()}
		p, err := box.Prepare(context.Background(), chat, "ha_entities", data)
		if err != nil {
			t.Fatalf("prepare: %v", err)
		}
		out, err := box.Execute(context.Background(), chat, p)
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		var result map[string]any
		if err := json.Unmarshal([]byte(out.Text), &result); err != nil {
			t.Fatalf("result %q: %v", out.Text, err)
		}
		return result
	}

	// The owner's release: window contacts and scripts, no automations.
	result := run(homeassistant.Release{Domains: []string{"binary_sensor", "script"}}, map[string]any{"domain": "automation"})
	note, _ := result["note"].(string)
	if result["released"] != false || !strings.Contains(note, "NOT RELEASED") || !strings.Contains(note, "KI-Agent › Home Assistant") {
		t.Fatalf("a hidden domain must not read as none: %v", result)
	}
	// Released: both are there.
	result = run(homeassistant.Release{Domains: []string{"automation", "binary_sensor"}}, map[string]any{"domain": "automation"})
	if result["released"] != true || result["total"] != float64(2) || result["note"] != nil {
		t.Fatalf("released automations: %v", result)
	}
	// "Alles" sees them too.
	if result = run(homeassistant.Release{All: true}, map[string]any{"domain": "automation"}); result["released"] != true || result["total"] != float64(2) {
		t.Fatalf("all: %v", result)
	}
	// Released and none there: said as such.
	result = run(homeassistant.Release{Domains: []string{"automation", "light"}}, map[string]any{"domain": "light"})
	if note, _ := result["note"].(string); result["released"] != true || result["total"] != float64(0) || !strings.Contains(note, "None present") {
		t.Fatalf("none present: %v", result)
	}
	// A single automation released: listed, and the rest is said to be hidden.
	result = run(homeassistant.Release{Entities: []string{"automation.morgens"}}, map[string]any{"domain": "automation"})
	if note, _ := result["note"].(string); result["released"] != false || result["total"] != float64(1) || !strings.Contains(note, "Only the single entities") {
		t.Fatalf("single entity: %v", result)
	}

	// The settings take automations as a kind; on the stick they stay apart
	// from the domains that 1.08 knows, so 1.08 can still save its settings.
	res, body := ts.request("PUT", "/api/v1/agent/settings", map[string]any{"enabled": true, "ollama": ts.server.agentSettings().Ollama, "model": "gpt-oss:20b",
		"homeassistant": map[string]any{"release": map[string]any{"domains": []string{"automation", "script"}}}})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("settings with automations: %d %s", res.StatusCode, body)
	}
	if got := ts.server.agentSettings().HA.Release.Domains; !slices.Contains(got, "automation") || !slices.Contains(got, "script") {
		t.Fatalf("release read back: %v", got)
	}
	var disk struct {
		HA struct {
			Release map[string][]string `json:"release"`
		} `json:"homeassistant"`
	}
	if err := ts.server.agentEnv().store.ReadJSON(agentSettingsFile, &disk); err != nil {
		t.Fatal(err)
	}
	if r := disk.HA.Release; !slices.Equal(r["domains"], []string{"script"}) || !slices.Equal(r["extraDomains"], []string{"automation"}) {
		t.Fatalf("on the stick: %v", r)
	}
	// The page gets the kinds the agent can be given.
	if res, body := ts.request("GET", "/api/v1/homeassistant/settings", nil); res.StatusCode != 200 || !strings.Contains(string(body), `"agentDomains":["alarm_control_panel","automation"`) {
		t.Fatalf("agent domains: %d %s", res.StatusCode, body)
	}
}

// The release of the agent as the previous app reads it: "domains" without
// the extra kinds, which come back from "extraDomains".
func TestHAReleaseKeepsExtraDomainsApart(t *testing.T) {
	data, err := json.Marshal(homeassistant.Release{Domains: []string{"automation", "light"}, Entities: []string{"lock.door"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"domains":["light"],"extraDomains":["automation"],"entities":["lock.door"]}` {
		t.Fatalf("written: %s", data)
	}
	var back homeassistant.Release
	if err := json.Unmarshal(data, &back); err != nil || !slices.Equal(back.Domains, []string{"light", "automation"}) || !slices.Equal(back.Entities, []string{"lock.door"}) {
		t.Fatalf("read: %+v %v", back, err)
	}
	// What 1.08 reads: the struct without the new field.
	var old struct {
		Domains []string `json:"domains"`
	}
	json.Unmarshal(data, &old)
	if !slices.Equal(old.Domains, []string{"light"}) {
		t.Fatalf("1.08 reads %v", old.Domains)
	}
}
