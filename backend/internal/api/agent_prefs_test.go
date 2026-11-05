package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// PLAN 53: the agent app's settings are kept per account on the NAS.
func TestAgentPrefsPerAccount(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{})
	if res, body := ts.request("GET", "/api/v1/agent/prefs", nil); res.StatusCode != 200 || strings.TrimSpace(string(body)) != "{}" {
		t.Fatalf("empty prefs: %d %s", res.StatusCode, body)
	}
	want := map[string]any{"gestures": true, "swipeLeft": "menu", "glass": 60.0, "notify": map[string]any{"done": true}}
	if res, body := ts.request("PUT", "/api/v1/agent/prefs", want); res.StatusCode != 200 {
		t.Fatalf("save: %d %s", res.StatusCode, body)
	}
	_, body := ts.request("GET", "/api/v1/agent/prefs", nil)
	var got map[string]any
	json.Unmarshal(body, &got)
	if got["swipeLeft"] != "menu" || got["glass"] != 60.0 || got["notify"].(map[string]any)["done"] != true {
		t.Fatalf("prefs: %s", body)
	}
	for _, bad := range []any{
		map[string]any{"bad key": true},
		map[string]any{"a": map[string]any{"b": map[string]any{"c": 1}}},
		map[string]any{"a": []int{1}},
		map[string]any{"a": strings.Repeat("x", 300)},
	} {
		if res, _ := ts.request("PUT", "/api/v1/agent/prefs", bad); res.StatusCode != 400 {
			t.Fatalf("accepted %v", bad)
		}
	}
	// Another account has its own.
	ts.users.Create("gast", testPassword, auth.RoleUser)
	_, sbody := ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(sbody, &settings)
	settings["users"] = []string{"gast"}
	ts.request("PUT", "/api/v1/agent/settings", settings)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	if _, body := ts.do(guest, "GET", "/api/v1/agent/prefs", nil); strings.TrimSpace(string(body)) != "{}" {
		t.Fatalf("guest sees prefs of the owner: %s", body)
	}
	if _, body := ts.do(guest, "GET", "/api/v1/agent/suggestions", nil); !strings.Contains(string(body), `"suggestions":[]`) {
		t.Fatalf("guest suggestions: %s", body)
	}
}
