package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/docker"
)

// PLAN 55 point 8: code_run is off by default, asks before each run (also
// with "Alles ohne Nachfrage"), runs in the account's folder only and its
// output is outside content.
func TestAgentCodeRun(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetAll})
	mnt := filepath.Dir(filepath.Dir(ts.withShare()))
	// Saving the settings brings the folders up to date in the background;
	// let that end before the test data goes (cleanups run last first).
	t.Cleanup(func() { time.Sleep(300 * time.Millisecond); ts.server.agentSyncShare() })
	var got []docker.SandboxRequest
	ts.server.agentSandbox = func(_ context.Context, req docker.SandboxRequest) (docker.SandboxResult, error) {
		got = append(got, req)
		return docker.SandboxResult{Output: "218.0\n", Took: 300 * time.Millisecond}, nil
	}
	user := mustUser(t, ts, "alex")
	if box := ts.server.newAgentToolbox(user, ts.server.agentSettings()); box.tools["code_run"] != nil {
		t.Fatal("code_run offered while off")
	}

	_, body := ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	for _, bad := range []map[string]any{{"timeout": 601}, {"memory": 10}} {
		settings["code"] = bad
		if res, _ := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusBadRequest {
			t.Fatalf("code %v accepted: %d", bad, res.StatusCode)
		}
	}
	settings["code"] = map[string]any{"enabled": true, "timeout": 30}
	settings["maxMinutes"] = 20
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	s := ts.server.agentSettings()
	if !s.Code.Enabled || s.Code.Timeout != 30 || s.Code.Memory != 512 || s.MaxMinutes != 20 {
		t.Fatalf("settings %+v", s)
	}
	box := ts.server.newAgentToolbox(user, s)
	if box.tools["code_run"] == nil {
		t.Fatal("code_run not offered")
	}
	chat := &agent.Chat{ID: agent.NewID()}
	args := json.RawMessage(`{"language":"python","code":"print(sum([12.5,40,7.25,100.1,3.15,55]))"}`)
	p, err := box.Prepare(context.Background(), chat, "code_run", args)
	if err != nil {
		t.Fatal(err)
	}
	if p.Mode != agent.ModeAsk || !strings.Contains(p.Summary, "print(sum(") || p.Kind != agent.KindChange || !p.Untrusted {
		t.Fatalf("prepared %+v", p)
	}
	if _, err := box.Prepare(context.Background(), chat, "code_run", json.RawMessage(`{"language":"ruby","code":"x"}`)); err == nil {
		t.Fatal("ruby accepted")
	}
	out, err := box.Execute(context.Background(), chat, p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Text, "Exit code 0") || !strings.Contains(out.Text, "218.0") {
		t.Fatalf("out %q", out.Text)
	}
	req := got[0]
	if req.Work != filepath.Join(mnt, "tank", agentShareName, "alex") || req.UID != user.UID || req.Timeout != 30*time.Second || req.MemoryMB != 512 || req.Language != "python" {
		t.Fatalf("request %+v", req)
	}

	// Allowed by name it runs without asking; denied it is gone.
	settings["policy"] = map[string]any{"preset": "standard", "tools": map[string]string{"code_run": "allow"}}
	ts.request("PUT", "/api/v1/agent/settings", settings)
	box = ts.server.newAgentToolbox(user, ts.server.agentSettings())
	if p, err := box.Prepare(context.Background(), chat, "code_run", args); err != nil || p.Mode != agent.ModeAllow {
		t.Fatalf("allowed by name: %+v %v", p, err)
	}
	settings["policy"] = map[string]any{"preset": "standard", "areas": map[string]string{"code": "deny"}}
	ts.request("PUT", "/api/v1/agent/settings", settings)
	box = ts.server.newAgentToolbox(user, ts.server.agentSettings())
	for _, spec := range box.Specs(chat) {
		if spec.Function.Name == "code_run" {
			t.Fatal("denied code_run offered")
		}
	}
}
