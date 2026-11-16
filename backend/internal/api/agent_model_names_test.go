package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// Owner 16 Nov 2026: a model can carry a name of the owner's own that the
// chat shows; the model is still asked for by its real name.
func TestAgentModelNames(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	_, body := ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	if err := json.Unmarshal(body, &settings); err != nil {
		t.Fatal(err)
	}
	// Spaces around go, an empty name and one like the model's are dropped.
	settings["modelNames"] = map[string]string{"gpt-oss:20b": "  Jarvis ", "leer:1b": "  ", "gleich:1b": "gleich:1b"}
	res, body := ts.request("PUT", "/api/v1/agent/settings", settings)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	ts.server.shareSyncs.Wait()
	if got := ts.server.agentSettings().ModelNames; len(got) != 1 || got["gpt-oss:20b"] != "Jarvis" {
		t.Fatalf("stored names: %v", got)
	}
	if ts.server.agentSettings().Model != "gpt-oss:20b" {
		t.Fatalf("the model itself changed: %q", ts.server.agentSettings().Model)
	}
	_, body = ts.request("GET", "/api/v1/agent/status", nil)
	if !strings.Contains(string(body), `"modelLabel":"Jarvis"`) || !strings.Contains(string(body), `"label":"Jarvis"`) {
		t.Fatalf("status without the shown name: %s", body)
	}
	// Too long, or with a line break: refused.
	for _, bad := range []string{strings.Repeat("x", 41), "Zeile\nzwei"} {
		settings["modelNames"] = map[string]string{"gpt-oss:20b": bad}
		if res, _ := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusBadRequest {
			t.Errorf("name %q: %d, want 400", bad, res.StatusCode)
		}
	}
}
