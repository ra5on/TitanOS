package homeassistant

import (
	"context"
	"testing"
)

func TestAgentReleaseIsItsOwn(t *testing.T) {
	m := &Manager{loaded: true, settings: Settings{Domains: []string{"light"}, Entities: []string{"switch.pumpe"}}}
	agent := WithRelease(context.Background(), Release{Domains: []string{"sensor"}, Entities: []string{"lock.haustuer"}})
	// The agent's list decides, not the release for MCP.
	if m.AllowedCtx(agent, "light.flur") || !m.AllowedCtx(agent, "sensor.temp") || !m.AllowedCtx(agent, "lock.haustuer") || m.AllowedCtx(agent, "switch.pumpe") {
		t.Fatal("agent release")
	}
	if !m.AllowedCtx(context.Background(), "light.flur") || m.AllowedCtx(context.Background(), "sensor.temp") {
		t.Fatal("MCP release changed")
	}
	if m.CanCallCtx(agent, "light.flur", "turn_on") || !m.CanCallCtx(agent, "lock.haustuer", "unlock") {
		t.Fatal("agent call")
	}
	if WithRelease(context.Background(), Release{}).Value(releaseKey{}) == nil || m.AllowedCtx(WithRelease(context.Background(), Release{}), "light.flur") {
		t.Fatal("an empty agent list releases nothing")
	}
	if _, err := m.Call(agent, "light", "turn_on", []string{"light.flur"}, nil); err == nil {
		t.Fatal("agent called an entity it has not released")
	}
}
