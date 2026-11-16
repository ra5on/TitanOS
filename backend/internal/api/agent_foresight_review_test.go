package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/advisor"
	"github.com/ra5on/TitanOS/backend/internal/agent"
)

type failingRunner struct{}

func (failingRunner) Run(context.Context, string, string, ...string) (string, error) {
	return "", errors.New("lsblk hangs")
}

// Review 1.02: a failed read of the disks (lsblk timed out, the page was
// left during "Jetzt prüfen") gave a report without disks, which replaced
// the one that warned of a failing disk and could go out as the monthly
// "nothing found".
func TestAdvisorKeepsTheReportWhenDisksAreNotReadable(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	dir := t.TempDir()
	ts.server.agentOpen = func() (*os.Root, error) { return os.OpenRoot(dir) }
	fake := &agentOllama{}
	ts.request("PUT", "/api/v1/agent/settings", map[string]any{"enabled": true, "ollama": fake.start(t), "model": "m", "policy": agent.Policy{Preset: agent.PresetStandard}})
	setFeatures(t, ts, map[string]any{"advisor": map[string]any{"enabled": true}})
	ts.disks.UseRunner(failingRunner{})
	st := ts.server.foresight()
	st.mu.Lock()
	st.loaded = true
	st.saved = advisorSaved{LastRun: time.Now().Add(-24 * time.Hour), Report: advisor.Report{Findings: []advisor.Finding{{Area: "disk", Subject: "sda", Level: advisor.LevelFail}}}}
	st.mu.Unlock()
	if res, _ := ts.request("POST", "/api/v1/agent/advisor/run", nil); res.StatusCode == http.StatusOK {
		t.Fatal("a report without disks was made")
	}
	saved, _ := ts.server.advisorReport()
	if len(saved.Report.Findings) != 1 || saved.Report.Findings[0].Level != advisor.LevelFail {
		t.Fatalf("warning gone: %+v", saved.Report)
	}
}
