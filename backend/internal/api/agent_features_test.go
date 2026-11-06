package api

import (
	"encoding/json"
	"net/http"
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
	if len(me.NAS) != 11 {
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
