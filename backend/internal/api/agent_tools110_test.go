package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// 1.10 (V10): one tool for each task. The route tools that do what a
// catalog tool of the agent does are gone for the agent – MCP keeps them –
// and a rule the person set for an old name still holds.
func TestAgentHasOneToolPerTask(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	alex := mustUser(t, ts, "alex")
	box := ts.server.newAgentToolbox(alex, ts.server.agentSettings())
	for name := range agentRetired {
		if _, ok := box.tools[name]; ok {
			t.Errorf("agent still has %s", name)
		}
	}
	for _, name := range []string{"file_read", "file_write", "file_copy", "vm_action", "files_mkdir", "files_rename", "files_delete", "docker_container_start"} {
		if _, ok := box.tools[name]; !ok {
			t.Errorf("agent lost %s", name)
		}
	}
	for _, s := range box.Specs(nil) {
		if strings.HasPrefix(s.Function.Description, "Propose to") {
			t.Errorf("%s: %q", s.Function.Name, s.Function.Description)
		}
	}
	// MCP is unchanged.
	mcp := map[string]bool{}
	for _, tool := range ts.server.mcpAvailable(alex, []string{"system:read", "system:write", "files:read", "files:write", "docker:read", "docker:write", "vms:read", "vms:write", "mcp"}) {
		mcp[tool.Name] = true
	}
	for name := range agentRetired {
		if !mcp[name] {
			t.Errorf("MCP lost %s", name)
		}
	}

	// Rules for old names.
	settings := ts.server.agentSettings()
	settings.Policy.Tools = map[string]string{"vm_reboot": agent.ModeDeny, "vm_start": agent.ModeAllow, "files_read_text": agent.ModeDeny}
	box = ts.server.newAgentToolbox(alex, settings)
	mode := func(tool string, args string) string {
		tl := box.tools[tool]
		return box.callMode(tl, tl.area, tl.kind, json.RawMessage(args))
	}
	if m := mode("vm_action", `{"vm":"haos","action":"reboot"}`); m != agent.ModeDeny {
		t.Errorf("reboot: %s", m)
	}
	if m := mode("vm_action", `{"vm":"haos","action":"start"}`); m != agent.ModeAllow {
		t.Errorf("start: %s", m)
	}
	if m := mode("vm_action", `{"vm":"haos","action":"poweroff"}`); m != agent.ModeAsk {
		t.Errorf("poweroff follows the area: %s", m)
	}
	if _, err := box.Prepare(context.Background(), &agent.Chat{}, "vm_action", json.RawMessage(`{"vm":"haos","action":"reboot"}`)); err == nil {
		t.Error("reboot ran although vm_reboot is forbidden")
	}
	if _, err := box.Prepare(context.Background(), &agent.Chat{}, "file_read", json.RawMessage(`{"root":"share/Docs","path":"a.txt"}`)); err == nil {
		t.Error("file_read ran although files_read_text is forbidden")
	}
	// A rule of the new tool itself and of the old one: the stricter.
	settings.Policy.Tools = map[string]string{"vm_start": agent.ModeAllow, "vm_action": agent.ModeAsk}
	box = ts.server.newAgentToolbox(alex, settings)
	if m := mode("vm_action", `{"vm":"haos","action":"start"}`); m != agent.ModeAsk {
		t.Errorf("both rules: %s", m)
	}
}

// 1.10 (V12, owner 17 Nov 2026): looking for updates runs without asking,
// also under "Nur lesen"; installing still asks or is forbidden.
func TestAgentUpdateChecksAreReading(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	alex := mustUser(t, ts, "alex")
	box := ts.server.newAgentToolbox(alex, ts.server.agentSettings())
	p, err := box.Prepare(context.Background(), &agent.Chat{}, "docker_check_updates", json.RawMessage(`{}`))
	if err != nil || p.Mode != agent.ModeAllow || p.Kind != agent.KindRead {
		t.Fatalf("docker check: %+v %v", p, err)
	}
	p, err = box.Prepare(context.Background(), &agent.Chat{}, "update_action", json.RawMessage(`{"action":"check"}`))
	if err != nil || p.Mode != agent.ModeAllow {
		t.Fatalf("update check: %+v %v", p, err)
	}
	install := json.RawMessage(`{"action":"install"}`)
	ua := box.tools["update_action"]
	if m := box.callMode(ua, ua.area, ua.kindOf(install), install); m != agent.ModeAsk {
		t.Fatalf("install must ask: %s", m)
	}
	settings := ts.server.agentSettings()
	settings.Policy = agent.Policy{Preset: agent.PresetRead}
	box = ts.server.newAgentToolbox(alex, settings)
	offered := false
	for _, s := range box.Specs(nil) {
		offered = offered || s.Function.Name == "update_action"
	}
	if !offered {
		t.Fatal("update_action not offered under \"Nur lesen\"")
	}
	if p, err := box.Prepare(context.Background(), &agent.Chat{}, "update_action", json.RawMessage(`{"action":"check"}`)); err != nil || p.Mode != agent.ModeAllow {
		t.Fatalf("check under read: %+v %v", p, err)
	}
	if _, err := box.Prepare(context.Background(), &agent.Chat{}, "update_action", json.RawMessage(`{"action":"install"}`)); err == nil {
		t.Fatal("install under \"Nur lesen\"")
	}
}

// 1.10 (V15): files_search finds the biggest files without paging through
// all of them.
func TestFilesSearchBySize(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	dir := ts.withShare()
	os.WriteFile(filepath.Join(dir, "klein.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "gross.iso"), make([]byte, 3_000_000), 0o644)
	os.WriteFile(filepath.Join(dir, "mittel.mkv"), make([]byte, 1_500_000), 0o644)
	result, text, err := runAgentTool(t, ts, ts.server.agentSettings(), "files_search", map[string]any{"root": "share/Docs", "minSize": 1, "sort": "size"})
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := result["entries"].([]any)
	if len(entries) != 2 || !strings.Contains(text, "gross.iso") || strings.Index(text, "gross.iso") > strings.Index(text, "mittel.mkv") || strings.Contains(text, "klein.txt") {
		t.Fatalf("by size: %s", text)
	}
	if _, _, err := runAgentTool(t, ts, ts.server.agentSettings(), "files_search", map[string]any{"root": "share/Docs", "sort": "colour"}); err == nil {
		t.Fatal("unknown sort accepted")
	}
}
