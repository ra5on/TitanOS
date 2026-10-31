package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/activity"
)

// The actions of the catalog over MCP: system:write opens them, the MCP
// page switches them, dangerous calls need "all" there and confirm=true,
// and the call runs as the account of the token, logged as MCP.
func TestMCPCatalogActions(t *testing.T) {
	ts := newTestServer(t)
	log := activity.New(ts.store)
	ts.server.opts.Activity = log
	ts.setup()
	ts.withShare()
	ts.mcpOn()

	reader := ts.mcpToken("alex", "mcp", "system:read")
	if names := mcpToolNames(t, ts.mcp(reader, "2025-06-18", rpc(1, "tools/list", nil))); slices.Contains(names, "share_delete") || !slices.Contains(names, "accounts_list") {
		t.Fatalf("system:read tools: %v", names)
	}
	admin := ts.mcpToken("alex", "mcp", "system:read", "system:write")
	names := mcpToolNames(t, ts.mcp(admin, "2025-06-18", rpc(1, "tools/list", nil)))
	for _, want := range []string{"share_delete", "volume_action", "power_action", "account_action", "firewall_rule_add", "plugin_install", "stick_backup"} {
		if !slices.Contains(names, want) {
			t.Errorf("%s missing with system:write", want)
		}
	}
	for _, skipped := range []string{"folder_create", "file_rename", "file_delete", "container_action", "web_read", "knowledge_save", "start_task"} {
		if slices.Contains(names, skipped) {
			t.Errorf("%s offered", skipped)
		}
	}

	// Dangerous: refused until allowed, then only with confirm=true.
	if text, isErr, _ := ts.mcpCall(admin, "share_delete", map[string]any{"share": "Docs", "confirm": true}); !isErr || !strings.Contains(text, "MCP") {
		t.Fatalf("deleted while not allowed: %s", text)
	}
	ts.mcpAllow("share_delete")
	if text, isErr, _ := ts.mcpCall(admin, "share_delete", map[string]any{"share": "Docs"}); !isErr || !strings.Contains(text, "confirm=true") {
		t.Fatalf("deleted without confirm: %s", text)
	}
	if text, isErr, _ := ts.mcpCall(admin, "share_delete", map[string]any{"share": "Docs", "confirm": true, "reason": "tidy up"}); isErr {
		t.Fatalf("delete: %s", text)
	}
	if res, body := ts.request("GET", "/api/v1/shares", nil); res.StatusCode != 200 || strings.Contains(string(body), `"Docs"`) {
		t.Fatalf("share still there: %s", body)
	}
	entry, ok := findActivity(activityEntries(t, log), "DELETE /api/v1/shares/{name}")
	if !ok || entry.Via != activity.ViaMCP || entry.User != "alex" || !entry.OK {
		t.Fatalf("activity: %+v %v", entry, ok)
	}

	// Switched off on the MCP page: gone from the list and refused.
	if res, body := ts.request("PUT", "/api/v1/system/mcp", map[string]any{"enabled": true, "tools": map[string]string{"accounts_list": "off"}}); res.StatusCode != 200 {
		t.Fatalf("switch off: %d %s", res.StatusCode, body)
	}
	if names := mcpToolNames(t, ts.mcp(admin, "2025-06-18", rpc(1, "tools/list", nil))); slices.Contains(names, "accounts_list") {
		t.Fatal("switched-off tool listed")
	}
	if reply := ts.mcp(admin, "2025-06-18", rpc(9, "tools/call", map[string]any{"name": "accounts_list", "arguments": map[string]any{}})); reply.msg["error"] == nil {
		t.Fatalf("switched-off tool ran: %v", reply.msg)
	}
}

func TestMCPSettingsListToolsAndCheckModes(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if res, _ := ts.request("PUT", "/api/v1/system/mcp", map[string]any{"enabled": true, "tools": map[string]string{"vm_action": "maybe"}}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid mode: %d", res.StatusCode)
	}
	if res, body := ts.request("PUT", "/api/v1/system/mcp", map[string]any{"enabled": true, "tools": map[string]string{"vm_action": "all", "docker_list": "on"}}); res.StatusCode != 200 || strings.Contains(string(body), "docker_list") {
		t.Fatalf("save: %d %s", res.StatusCode, body)
	}
	// Switching MCP off and on keeps the tools.
	ts.request("PUT", "/api/v1/system/mcp", map[string]any{"enabled": false})
	ts.request("PUT", "/api/v1/system/mcp", map[string]any{"enabled": true})
	res, body := ts.request("GET", "/api/v1/system/mcp", nil)
	var view struct {
		Tools []mcpToolView `json:"tools"`
	}
	if res.StatusCode != 200 || json.Unmarshal(body, &view) != nil {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	found := map[string]mcpToolView{}
	for _, tool := range view.Tools {
		found[tool.Name] = tool
	}
	if v := found["vm_action"]; v.Mode != "all" || !v.Dangerous || v.Kind != "change" || v.Area != "vms" {
		t.Fatalf("vm_action: %+v", v)
	}
	if v := found["system_status"]; v.Mode != "on" || v.Kind != "read" || v.Scope != "system:read" {
		t.Fatalf("system_status: %+v", v)
	}
	if v := found["power_action"]; v.Scope != "system:write" || !v.Dangerous {
		t.Fatalf("power_action: %+v", v)
	}
}
