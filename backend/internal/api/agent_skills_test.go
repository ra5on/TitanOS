package api

import (
	"regexp"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// PLAN 53: the built-in skills only name tools the agent really has, so the
// model is never told to call one that does not exist. Tools of Home
// Assistant come only with a connection; they are listed here.
func TestBuiltInSkillsNameRealTools(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{})
	u, _ := ts.users.Get("alex")
	box := ts.server.newAgentToolbox(u, ts.server.agentSettings())
	known := map[string]bool{"ha_state": true, "ha_call": true, "ha_diagnostics": true, "ha_statistics": true, "ha_history": true, "ha_entities": true}
	for _, x := range box.ordered {
		known[x.name] = true
	}
	word := regexp.MustCompile(`\b[a-z]+_[a-z_]+\b`)
	if len(agent.BuiltInSkills) < 28 {
		t.Fatalf("only %d built-in skills", len(agent.BuiltInSkills))
	}
	seen := map[string]bool{}
	for _, s := range agent.BuiltInSkills {
		if seen[s.ID] {
			t.Errorf("skill %s twice", s.ID)
		}
		seen[s.ID] = true
		for _, name := range word.FindAllString(s.Steps, -1) {
			if !known[name] {
				t.Errorf("skill %q names unknown tool %q", s.Name, name)
			}
		}
	}
}
