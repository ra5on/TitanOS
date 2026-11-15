package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// The key of an outside provider stays on the NAS: never in an answer,
// kept when the request leaves it out, and dropped when the address or
// the provider changes – a stored key must not follow a new address.
func TestAgentProviderSettings(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	get := func() map[string]any {
		t.Helper()
		_, body := ts.request("GET", "/api/v1/agent/settings", nil)
		var settings map[string]any
		json.Unmarshal(body, &settings)
		if strings.Contains(string(body), "sk-") {
			t.Fatalf("a key went to the page: %s", body)
		}
		return settings
	}
	put := func(provider map[string]any) (int, string) {
		t.Helper()
		settings := get()
		settings["provider"] = provider
		res, body := ts.request("PUT", "/api/v1/agent/settings", settings)
		if strings.Contains(string(body), "sk-") {
			t.Fatalf("a key came back: %s", body)
		}
		return res.StatusCode, string(body)
	}
	stored := func() agentProvider { return ts.server.agentSettings().Provider }

	if p := stored(); p.Kind != "" {
		t.Fatalf("from the factory: %+v", p)
	}
	for _, bad := range []map[string]any{
		{"kind": "gemini"},
		{"kind": "openai"},
		{"kind": "openai", "url": "http://api.openai.com/v1", "key": "sk-a"},
		{"kind": "claude"},
	} {
		if code, body := put(bad); code != 400 {
			t.Fatalf("%v: %d %s", bad, code, body)
		}
	}
	if code, body := put(map[string]any{"kind": "openai", "url": "https://openrouter.ai/api/v1/", "key": "sk-one"}); code != 200 || !strings.Contains(body, `"keySet":true`) {
		t.Fatalf("set: %d %s", code, body)
	}
	if p := stored(); p.Key != "sk-one" || p.URL != "https://openrouter.ai/api/v1" {
		t.Fatalf("stored %+v", p)
	}
	// Saved again as the page shows it: the key stays.
	if code, _ := put(map[string]any{"kind": "openai", "url": "https://openrouter.ai/api/v1", "keySet": true}); code != 200 || stored().Key != "sk-one" {
		t.Fatalf("kept: %d %+v", code, stored())
	}
	// Another address without a key: the old key does not go there.
	if code, _ := put(map[string]any{"kind": "openai", "url": "https://example.org/v1", "keySet": true}); code != 200 || stored().Key != "" {
		t.Fatalf("new address: %d %+v", code, stored())
	}
	// Another provider needs its own key.
	if code, _ := put(map[string]any{"kind": "claude", "keySet": true}); code != 400 {
		t.Fatalf("claude without a key: %d", code)
	}
	if code, _ := put(map[string]any{"kind": "claude", "key": "sk-ant-two"}); code != 200 || stored().Key != "sk-ant-two" || stored().URL != "" {
		t.Fatalf("claude: %d %+v", code, stored())
	}
	// Back to Ollama: nothing of the provider is left.
	if code, _ := put(map[string]any{"kind": ""}); code != 200 || stored() != (agentProvider{}) {
		t.Fatalf("back: %d %+v", code, stored())
	}
}
