package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/scripts"
)

func scriptServer(t *testing.T) *testServer {
	t.Helper()
	mnt := t.TempDir()
	if err := os.MkdirAll(filepath.Join(mnt, "Data"), 0o755); err != nil {
		t.Fatal(err)
	}
	ts := newTestServerWith(t, func(o *Options) {
		m := scripts.New(o.Store)
		m.MountRoot, m.Mounted, m.Shell, m.StartMarker = mnt, func() []string { return []string{"Data"} }, "/bin/sh", ""
		o.Scripts = m
	})
	ts.setup()
	return ts
}

func TestScriptRoutes(t *testing.T) {
	ts := scriptServer(t)
	res, body := ts.request("POST", "/api/v1/scripts", map[string]any{"name": "Hallo", "trigger": "manual", "content": "echo hallo welt\n" + strings.Repeat("#", 100<<10)})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	var created struct {
		ID string `json:"id"`
	}
	json.Unmarshal(body, &created)
	if res, body := ts.request("POST", "/api/v1/scripts/"+created.ID+"/run", nil); res.StatusCode != http.StatusAccepted {
		t.Fatalf("run: %d %s", res.StatusCode, body)
	}
	var out scripts.Output
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		_, body := ts.request("GET", "/api/v1/scripts/"+created.ID+"/output?from=0", nil)
		json.Unmarshal(body, &out)
		if out.Run != nil && !out.Running {
			break
		}
	}
	if out.Run == nil || out.Run.Outcome != "ok" || out.Text != "hallo welt\n" {
		t.Fatalf("output %+v", out)
	}
	res, body = ts.request("GET", "/api/v1/scripts", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"outcome":"ok"`) {
		t.Fatalf("list: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/scripts/volume", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("not an id: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/scripts/"+created.ID+"/cancel", nil); errorCode(t, body) != "script_not_running" {
		t.Fatalf("cancel: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/scripts/"+created.ID, map[string]any{"name": "x", "trigger": "schedule", "schedule": map[string]any{"every": "custom", "cron": "99 * * * *"}}); errorCode(t, body) != "script_invalid" {
		t.Fatalf("bad cron: %d %s", res.StatusCode, body)
	}

	// An API token, even of the owner with every scope, reaches no script
	// route; nor does MCP list a tool for them.
	token := ts.mcpToken("alex", "mcp", "system:read", "system:write", "files:read", "files:write", "docker:read", "docker:write", "vms:read", "vms:write")
	for _, call := range [][2]string{{"GET", "/api/v1/scripts"}, {"POST", "/api/v1/scripts/" + created.ID + "/run"}, {"GET", "/api/v1/scripts/" + created.ID + "/output"}} {
		req, _ := http.NewRequest(call[0], ts.url+call[1], nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode < 400 {
			t.Errorf("token reached %s %s: %d", call[0], call[1], res.StatusCode)
		}
	}
	ts.mcpOn()
	for _, name := range mcpToolNames(t, ts.mcp(token, "2025-06-18", rpc(1, "tools/list", nil))) {
		if strings.Contains(name, "script") {
			t.Errorf("MCP offers %s", name)
		}
	}
	for _, r := range publicRoutes {
		if strings.HasPrefix(r.Path, "/scripts") {
			t.Errorf("public route %s %s", r.Method, r.Path)
		}
	}

	if res, body := ts.request("DELETE", "/api/v1/scripts/"+created.ID, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.StatusCode, body)
	}
}
