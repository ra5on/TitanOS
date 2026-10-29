package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestAssistantCloudSettingsKeepTheKeys(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	const key = "sk-ant-api-geheim-123"
	res, body := ts.request("PUT", "/api/v1/assistant", map[string]any{"provider": "anthropic", "anthropicKey": key})
	if res.StatusCode != 200 || strings.Contains(string(body), key) || !strings.Contains(string(body), `"anthropicKeySet":true`) || !strings.Contains(string(body), `"model":"claude-sonnet-5-5"`) {
		t.Fatalf("save: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/assistant", nil); res.StatusCode != 200 || strings.Contains(string(body), key) || !strings.Contains(string(body), `"anthropicKeySet":true`) {
		t.Fatalf("read: %d %s", res.StatusCode, body)
	}
	// The stick shows neither the key file nor its folder.
	if res, body := ts.request("GET", "/api/v1/files/list?root=stick", nil); res.StatusCode != 200 || strings.Contains(string(body), `"assistant"`) {
		t.Fatalf("stick listing: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/files/download?root=stick&path=assistant/keys.json", nil); res.StatusCode == 200 || strings.Contains(string(body), key) {
		t.Fatalf("key file downloaded: %d %s", res.StatusCode, body)
	}
	// Switched on without a key: refused with a code the page explains.
	res, body = ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "provider": "anthropic", "clearAnthropicKey": true})
	if res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "assistant_key_missing" {
		t.Fatalf("missing key: %d %s", res.StatusCode, body)
	}
	// A stored OpenAI-compatible key belongs to its server.
	if res, body := ts.request("PUT", "/api/v1/assistant", map[string]any{"provider": "openai", "openaiBase": "https://openrouter.ai/api/v1", "openaiKey": "sk-or-1"}); res.StatusCode != 200 || !strings.Contains(string(body), `"openaiKeySet":true`) {
		t.Fatalf("openai: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("PUT", "/api/v1/assistant", map[string]any{"provider": "openai", "openaiBase": "https://example.com/v1"})
	if res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "assistant_key_server" {
		t.Fatalf("other server: %d %s", res.StatusCode, body)
	}
	// This host's loopback is never a provider, also not for owners.
	res, body = ts.request("POST", "/api/v1/assistant/models", map[string]any{"provider": "openai", "openaiBase": "http://127.0.0.1:9/v1", "openaiKey": "sk"})
	if res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "assistant_address_blocked" {
		t.Fatalf("loopback: %d %s", res.StatusCode, body)
	}
}

func TestAssistantOllamaSetupProposal(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	res, body := ts.request("GET", "/api/v1/assistant/ollama-setup", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"model":"qwen3:`) || !strings.Contains(string(body), `"basis":"ram"`) || !strings.Contains(string(body), `"enabled":false`) || !strings.Contains(string(body), `"name":"qwen3:32b"`) || !strings.Contains(string(body), `"fits":`) {
		t.Fatalf("setup: %d %s", res.StatusCode, body)
	}
}
