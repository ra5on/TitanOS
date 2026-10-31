package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
)

func TestServicesSwitchVMsAndLXC(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if res, body := ts.request("PUT", "/api/v1/services/vms", map[string]any{"enabled": false}); res.StatusCode != 200 {
		t.Fatalf("switch off: %d %s", res.StatusCode, body)
	}
	_, body := ts.request("GET", "/api/v1/services/state", nil)
	var state map[string]bool
	json.Unmarshal(body, &state)
	if state["vms"] || !state["lxc"] || !state["docker"] {
		t.Fatalf("state: %s", body)
	}
	var stored serviceSettings
	if err := ts.store.ReadJSON(servicesFile, &stored); err != nil || !stored.VMsOff {
		t.Fatalf("stored: %+v %v", stored, err)
	}
	if res, _ := ts.request("POST", "/api/v1/vms/nope/start", nil); res.StatusCode != http.StatusConflict && res.StatusCode != http.StatusNotFound {
		t.Fatalf("start while off: %d", res.StatusCode)
	}
	ts.mcpOn()
	token := ts.mcpToken("alex", "mcp", "vms:read", "vms:write", "system:read")
	names := mcpToolNames(t, ts.mcp(token, "2025-06-18", rpc(1, "tools/list", nil)))
	if slices.Contains(names, "vm_list") || slices.Contains(names, "vm_start") {
		t.Fatalf("VM tools while off: %v", names)
	}
	ts.request("PUT", "/api/v1/services/vms", map[string]any{"enabled": true})
	names = mcpToolNames(t, ts.mcp(token, "2025-06-18", rpc(2, "tools/list", nil)))
	if !slices.Contains(names, "vm_list") {
		t.Fatalf("VM tools after switching on: %v", names)
	}
	if res, _ := ts.request("PUT", "/api/v1/services/printer", map[string]any{"enabled": false}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown service: %d", res.StatusCode)
	}
}

// A new NAS has Docker not set up yet; its tab stays, because that is where
// it is set up. Only switching it off here hides it.
func TestDockerHiddenOnlyWhenSwitchedOff(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if !ts.server.servicesOn()["docker"] {
		t.Fatal("docker hidden on a new NAS")
	}
	if res, body := ts.request("PUT", "/api/v1/services/docker", map[string]any{"enabled": false}); res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if ts.server.servicesOn()["docker"] {
		t.Fatal("docker still shown after switching off")
	}
}
