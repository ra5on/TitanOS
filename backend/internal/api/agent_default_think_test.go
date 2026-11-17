package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// Owner, 17 Nov 2026: a new default thinking level under Einstellungen ›
// KI-Agent only counted after a restart. A chat without a level of its own
// runs with the level of the settings at the time of the run; a level
// chosen in the chat stays. (The page no longer sends its copy of the
// default with every message; test-ui-agent90.mjs checks that side.)
func TestAgentDefaultThinkFollowsSettings(t *testing.T) {
	fake := &agentOllama{replies: []string{
		`{"role":"assistant","content":"Eins."}`,
		`{"role":"assistant","content":"Zwei."}`,
		`{"role":"assistant","content":"Drei."}`,
	}}
	ts := agentServer(t, fake, agent.Policy{})
	lastThink := func() any {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return fake.got[len(fake.got)-1]["think"]
	}
	res, body := ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": "Hallo"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %s", res.StatusCode, body)
	}
	var started agent.Chat
	json.Unmarshal(body, &started)
	waitChat(t, ts, ts.client, started.ID)
	if lastThink() != "high" {
		t.Fatalf("think: %v", lastThink())
	}

	// The default changes; no restart.
	_, body = ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	settings["think"] = "low"
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	ts.server.shareSyncs.Wait()
	res, body = ts.request("POST", "/api/v1/agent/chats/"+started.ID+"/messages", map[string]any{"message": "Weiter"})
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("message: %d %s", res.StatusCode, body)
	}
	if chat := waitChat(t, ts, ts.client, started.ID); chat.Think != "" {
		t.Fatalf("the chat took a level of its own: %q", chat.Think)
	}
	if lastThink() != "low" {
		t.Fatalf("the open chat kept the old default: %v", lastThink())
	}

	// A level chosen in the chat stays its own.
	res, body = ts.request("POST", "/api/v1/agent/chats/"+started.ID+"/messages", map[string]any{"message": "Gründlich", "think": "medium"})
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("message: %d %s", res.StatusCode, body)
	}
	if chat := waitChat(t, ts, ts.client, started.ID); chat.Think != "medium" || lastThink() != "medium" {
		t.Fatalf("chosen level: chat %q, sent %v", chat.Think, lastThink())
	}
}
