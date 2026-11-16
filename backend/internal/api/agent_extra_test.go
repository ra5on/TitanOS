package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// A-41 (1.03): cluster_action is judged by what it acts on – a container
// under Docker, a VM or system container under VMs – not always under VMs.
// With Docker forbidden and VMs without asking, the agent must not start a
// container on another node.
func TestClusterActionFollowsTheAreaOfItsKind(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	owner, _ := ts.users.Get("alex")
	box := func(areas map[string]string) *agentToolbox {
		settings := ts.server.agentSettings()
		settings.Policy = agent.Policy{Preset: agent.PresetStandard, Areas: areas}
		b := ts.server.newAgentToolbox(owner, settings)
		tool := b.clusterActionTool()
		b.tools[tool.name] = tool
		b.ordered = append(b.ordered, tool)
		return b
	}
	call := func(kind string) json.RawMessage {
		return json.RawMessage(`{"node":"n1","kind":"` + kind + `","name":"web","action":"start"}`)
	}
	b := box(map[string]string{"docker": agent.ModeDeny, "vms": agent.ModeAllow})
	if _, err := b.Prepare(context.Background(), nil, "cluster_action", call("container")); err == nil {
		t.Fatal("a container started with Docker forbidden")
	}
	for _, kind := range []string{"vm", "lxc"} {
		p, err := b.Prepare(context.Background(), nil, "cluster_action", call(kind))
		if err != nil || p.Mode != agent.ModeAllow || p.Area != "vms" {
			t.Fatalf("%s: %+v %v", kind, p, err)
		}
	}
	b = box(map[string]string{"docker": agent.ModeAsk, "vms": agent.ModeAllow})
	if p, err := b.Prepare(context.Background(), nil, "cluster_action", call("container")); err != nil || p.Mode != agent.ModeAsk || p.Area != "docker" {
		t.Fatalf("container with Docker asking: %+v %v", p, err)
	}
	// The model still gets the tool while one of its areas allows it, and
	// not when both forbid it.
	offered := func(b *agentToolbox) bool {
		for _, s := range b.Specs(nil) {
			if s.Function.Name == "cluster_action" {
				return true
			}
		}
		return false
	}
	if !offered(box(map[string]string{"docker": agent.ModeDeny, "vms": agent.ModeAllow})) || !offered(box(map[string]string{"docker": agent.ModeAllow, "vms": agent.ModeDeny})) {
		t.Fatal("tool not offered although an area allows it")
	}
	if offered(box(map[string]string{"docker": agent.ModeDeny, "vms": agent.ModeDeny})) {
		t.Fatal("tool offered although every area forbids it")
	}
	// The page of the rights lists it under Docker and under VMs.
	var areas []string
	for _, v := range box(map[string]string{"docker": agent.ModeAsk}).views() {
		if v.Name == "cluster_action" {
			areas = append(areas, v.Area+"="+v.Mode)
		}
	}
	if len(areas) != 2 || areas[0] != "vms="+agent.ModeAsk && areas[0] != "vms="+agent.ModeAllow || areas[1] != "docker="+agent.ModeAsk {
		t.Fatalf("rows: %v", areas)
	}
}
