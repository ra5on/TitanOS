package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// 1.10 (V13): the base load of a run – system prompt plus the tools the
// model is offered before the first question – measured, not estimated.
// A small model has a window of 8–16k tokens; everything here is gone
// before the person has said a word. The caps hold it from growing back.
//
// Measured with the fake Ollama on "Hallo": standard rights, tools by area
// as from the factory, Docker, VMs and LXC on. The test NAS has no Home
// Assistant, no memories, no share "KI-Agent" (no notes tools) and an
// almost empty state report, so a real NAS lies above this: it is the
// part TitanOS itself puts in, not the person's own. Tokens are characters
// / 3 as in agent.tokens.
//
// Before 1.10 (17 Nov 2026): administrator 16 791 characters of prompt
// (12 437 of them the 34 skills in full) and 39 tools with 23 324
// characters, 40 115 characters ≈ 13 400 tokens; a user account 16 819 and
// 33 tools with 19 830, 36 649 ≈ 12 200 tokens.
//
// 1.10 (skills by name only, one tool per task, short HA results): the
// administrator 8 551 + 38 tools with 22 415 = 30 966 characters ≈ 10 300
// tokens; a user 8 683 + 32 tools with 18 607 = 27 290 ≈ 9 100 tokens.
// The caps leave about 1 000 characters of room; a new tool in the core
// areas or a longer rule has to make room elsewhere.
func TestAgentBaseLoad(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetStandard})
	ts.users.Create("gast", testPassword, auth.RoleUser)
	_, body := ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	settings["users"] = []string{"gast"}
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != 200 {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	measure := func(client *http.Client) (prompt, tools, count int) {
		t.Helper()
		fake.mu.Lock()
		before := len(fake.got)
		fake.mu.Unlock()
		res, body := ts.do(client, "POST", "/api/v1/agent/chats", map[string]any{"message": "Hallo"})
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("chat: %d %s", res.StatusCode, body)
		}
		var chat agent.Chat
		json.Unmarshal(body, &chat)
		waitChat(t, ts, client, chat.ID)
		fake.mu.Lock()
		first := fake.got[before]
		fake.mu.Unlock()
		msgs, _ := first["messages"].([]any)
		sys, _ := msgs[0].(map[string]any)
		text, _ := sys["content"].(string)
		data, _ := json.Marshal(first["tools"])
		list, _ := first["tools"].([]any)
		return len(text), len(data), len(list)
	}
	for _, c := range []struct {
		who                         string
		client                      *http.Client
		maxPrompt, maxTools, maxAll int
	}{{"admin", ts.client, 9_500, 23_500, 32_000}, {"user", guest, 9_500, 19_500, 28_500}} {
		prompt, tools, count := measure(c.client)
		total := prompt + tools
		t.Logf("%s: prompt %d chars, %d tools with %d chars, together %d chars ≈ %d tokens", c.who, prompt, count, tools, total, total/3)
		if prompt > c.maxPrompt || tools > c.maxTools || total > c.maxAll {
			t.Errorf("%s: base load over its cap (prompt %d/%d, tools %d/%d, together %d/%d)", c.who, prompt, c.maxPrompt, tools, c.maxTools, total, c.maxAll)
		}
	}
}
