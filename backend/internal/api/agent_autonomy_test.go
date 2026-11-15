package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// The way a chat works (owner 15.11.2026): "ask" makes every change wait
// although the rights allow it, "plan" and "direct" leave the rights as
// they are and tell the model how to begin; a request sets it, anything
// unknown keeps what the chat has.
func TestAgentChatAutonomy(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetAll})
	user := mustUser(t, ts, "alex")
	box := ts.server.newAgentToolbox(user, ts.server.agentSettings())
	args := json.RawMessage(`{"text":"Der Drucker steht im Flur."}`)
	chat := &agent.Chat{ID: agent.NewID()}
	p, err := box.Prepare(context.Background(), chat, "memory_save", args)
	if err != nil {
		t.Fatal(err)
	}
	direct := p.Mode
	chat.Autonomy = agent.AutonomyAsk
	if p, err = box.Prepare(context.Background(), chat, "memory_save", args); err != nil {
		t.Fatal(err)
	}
	if p.Kind != agent.KindRead && p.Kind != agent.KindAgent && (direct != agent.ModeAllow || p.Mode != agent.ModeAsk) {
		t.Fatalf("direct %q, ask %q (kind %q)", direct, p.Mode, p.Kind)
	}
	chat.Autonomy = agent.AutonomyPlan
	if p, err = box.Prepare(context.Background(), chat, "memory_save", args); err != nil || p.Mode != direct {
		t.Fatalf("plan changed the rights: %q %v", p.Mode, err)
	}

	for asked, want := range map[string]string{"direct": "", "plan": "plan", "ask": "ask", "": "plan", "alles": "plan"} {
		if got := autonomyOf(asked, "plan"); got != want {
			t.Fatalf("autonomyOf(%q) = %q, want %q", asked, got, want)
		}
	}

	in := agent.PromptInput{Account: "alex", Now: time.Now()}
	if text := agent.BuildPrompt(in); !strings.Contains(text, "start right away") {
		t.Fatal("direct is not in the prompt")
	}
	in.Autonomy = agent.AutonomyPlan
	if text := agent.BuildPrompt(in); !strings.Contains(text, "see your plan first") || strings.Contains(text, "start right away") {
		t.Fatal("plan is not in the prompt")
	}
	// A run nobody watches never waits for a yes to its plan.
	in.Origin = "schedule"
	if text := agent.BuildPrompt(in); strings.Contains(text, "see your plan first") {
		t.Fatal("a scheduled run asks for its plan")
	}

	// A draft chat takes the way to work, and PATCH changes it.
	res, body := ts.request("POST", "/api/v1/agent/chats", map[string]any{"draft": true, "autonomy": "ask"})
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		t.Fatalf("draft: %d %s", res.StatusCode, body)
	}
	var made agent.Chat
	json.Unmarshal(body, &made)
	if made.Autonomy != agent.AutonomyAsk {
		t.Fatalf("draft autonomy %q", made.Autonomy)
	}
	if res, body := ts.request("PATCH", "/api/v1/agent/chats/"+made.ID, map[string]any{"autonomy": "direct"}); res.StatusCode != http.StatusOK {
		t.Fatalf("patch: %d %s", res.StatusCode, body)
	}
	stored, err := ts.server.agentStore().Chat("alex", made.ID)
	if err != nil || stored.Autonomy != "" {
		t.Fatalf("stored %q %v", stored.Autonomy, err)
	}
}
