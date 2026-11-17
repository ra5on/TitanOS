package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// PLAN 54 skeleton: every function is off by default; the NAS-wide part is
// for owners and administrators, an account's own part for every account
// of the agent; saving the other settings keeps the functions.
func TestAgentFeaturesSkeleton(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	_, body := ts.request("GET", "/api/v1/agent/features/me", nil)
	var me struct {
		NAS map[string]bool `json:"nas"`
	}
	json.Unmarshal(body, &me)
	if len(me.NAS) != 10 {
		t.Fatalf("public: %s", body)
	}
	for k, on := range me.NAS {
		if on {
			t.Fatalf("%s on by default", k)
		}
	}
	if res, body := ts.request("PUT", "/api/v1/agent/features", map[string]any{"docs": map[string]any{"enabled": true, "deadlines": true}}); res.StatusCode != http.StatusOK {
		t.Fatalf("set: %d %s", res.StatusCode, body)
	}
	_, body = ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	if res, _ := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusOK {
		t.Fatal("settings")
	}
	if f := ts.server.agentSettings().Features; !f.Docs.Enabled || !f.Docs.Deadlines {
		t.Fatalf("settings save dropped the functions: %+v", f.Docs)
	}
	_, body = ts.request("GET", "/api/v1/agent/features/me", nil)
	json.Unmarshal(body, &me)
	if !me.NAS["deadlines"] || me.NAS["paper"] {
		t.Fatalf("public after set: %s", body)
	}
	if res, _ := ts.request("PUT", "/api/v1/agent/features/me", map[string]any{}); res.StatusCode != http.StatusOK {
		t.Fatal("mine")
	}
}

// A-30 (1.03): two tabs or two administrators save the whole block of the
// functions. With the version read ("rev"), a save of an older state is
// refused with 409 agent_features_changed instead of setting the other
// change back; without a version a save works as before.
func TestAgentFeaturesStaleSaveIsRefused(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	read := func() map[string]any {
		_, body := ts.request("GET", "/api/v1/agent/features", nil)
		var f map[string]any
		if err := json.Unmarshal(body, &f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	tab1, tab2 := read(), read()
	rev, _ := tab1["rev"].(string)
	if rev == "" || tab2["rev"] != rev {
		t.Fatalf("no version: %v", tab1["rev"])
	}
	// Tab 1 switches the documents on; the answer carries the new version.
	tab1["docs"] = map[string]any{"enabled": true}
	res, body := ts.request("PUT", "/api/v1/agent/features", tab1)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("first save: %d %s", res.StatusCode, body)
	}
	var saved map[string]any
	json.Unmarshal(body, &saved)
	if saved["rev"] == rev || saved["rev"] == "" {
		t.Fatalf("version after the save: %v", saved["rev"])
	}
	if read()["rev"] != saved["rev"] {
		t.Fatal("GET and PUT disagree on the version")
	}
	// Tab 2 still has the old state: switching the handbook on would set
	// the documents back off.
	tab2["handbook"] = map[string]any{"enabled": true}
	res, body = ts.request("PUT", "/api/v1/agent/features", tab2)
	if res.StatusCode != http.StatusConflict || !strings.Contains(string(body), "agent_features_changed") {
		t.Fatalf("stale save: %d %s", res.StatusCode, body)
	}
	if f := ts.server.agentSettings().Features; !f.Docs.Enabled || f.Handbook.Enabled {
		t.Fatalf("stale save changed something: %+v %+v", f.Docs, f.Handbook)
	}
	// A second save with the version of the first answer goes through.
	saved["handbook"] = map[string]any{"enabled": true}
	if res, body := ts.request("PUT", "/api/v1/agent/features", saved); res.StatusCode != http.StatusOK {
		t.Fatalf("save with the new version: %d %s", res.StatusCode, body)
	}
	// Without a version: the old behaviour.
	delete(tab2, "rev")
	if res, body := ts.request("PUT", "/api/v1/agent/features", tab2); res.StatusCode != http.StatusOK {
		t.Fatalf("save without version: %d %s", res.StatusCode, body)
	}
	if f := ts.server.agentSettings().Features; f.Docs.Enabled || !f.Handbook.Enabled {
		t.Fatalf("save without version: %+v %+v", f.Docs, f.Handbook)
	}
}
