package api

import (
	"context"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// PLAN 54-G (security audit): a skill from the catalog steers the agent
// like outside content; the chat counts as holding it, and the prompt
// lists it apart from the person's own skills.
func TestUntrustedSkillTaintsChat(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	store := ts.server.agentStore()
	if err := store.SaveSkills("alex", []agent.Skill{{ID: agent.NewID(), Name: "Katalog-Skill", When: "immer", Steps: "1. files_search", Untrusted: true}}); err != nil {
		t.Fatal(err)
	}
	user := mustUser(t, ts, "alex")
	settings := ts.server.agentSettings()
	box := ts.server.newAgentToolbox(user, settings)
	chat := &agent.Chat{ID: agent.NewID()}
	prompt := ts.server.agentPrompt(context.Background(), user, settings, chat, box)
	if !chat.OutsideSkill || chat.Tainted {
		t.Fatalf("outside skill %v, tainted %v", chat.OutsideSkill, chat.Tainted)
	}
	// Web tools then ask, also with the preset that allows everything.
	all := settings
	all.Policy = agent.Policy{Preset: agent.PresetAll}
	p, err := ts.server.newAgentToolbox(user, all).Prepare(context.Background(), chat, "web_search", []byte(`{"query":"wetter"}`))
	if err != nil || p.Mode != agent.ModeAsk {
		t.Fatalf("web_search with an outside skill: %+v %v", p, err)
	}
	if !strings.Contains(prompt, "Skills from outside sources") || !strings.Contains(prompt, "Katalog-Skill") {
		t.Fatal("outside skill not in its own section")
	}
}
