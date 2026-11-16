package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
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

// Review 1.02: another member of a shared project read the arguments of
// an undone step (a path in a private folder), errors, and the titles of
// the author's private notes from the chat's notes.
func TestForeignChatKeepsTheAuthorsSteps(t *testing.T) {
	c := &agent.Chat{ID: "x", Entries: []agent.Entry{
		{Role: "user", Content: "Räum auf"},
		{Role: "tool", Tool: "files_delete", Args: []byte(`{"path":"/Privat/Steuer.pdf"}`), Result: "ok", Summary: "files_delete path=/Privat/Steuer.pdf"},
		{Role: "note", Note: "undone", Tool: "files_delete", Content: "files_delete path=/Privat/Steuer.pdf"},
		{Role: "note", Note: "badcall", Content: "open /Privat/Steuer.pdf: no such file"},
		{Role: "note", Note: "note", Content: "Arzttermine"},
		{Role: "note", Note: "homeassistant", Content: "switch switch.schlafzimmer"},
		{Role: "note", Note: "stopped"},
		{Role: "assistant", Content: "Erledigt."},
	}}
	out := foreignChat(c)
	data, _ := json.Marshal(out)
	for _, secret := range []string{"Privat", "Arzttermine", "schlafzimmer"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("%q reached another member: %s", secret, data)
		}
	}
	if !strings.Contains(string(data), `"stopped"`) || !strings.Contains(string(data), "Erledigt.") {
		t.Fatalf("too much gone: %s", data)
	}
}

// Review 1.02: offers found while prices were on stayed in the kept
// report; with prices off the tool gave them to the model without marking
// the chat as having read foreign content.
func TestHardwareAdviceWithoutPricesHasNoOffers(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	setFeatures(t, ts, map[string]any{"advisor": map[string]any{"enabled": true}})
	st := ts.server.foresight()
	st.mu.Lock()
	st.loaded = true
	st.saved = advisorSaved{LastRun: time.Now(), Offers: map[string][]advisorOffer{"sda": {{Title: "WD Red", URL: "https://shop.example/wd80"}}}}
	st.mu.Unlock()
	owner, _ := ts.users.Get(ownerName(t, ts))
	tool := toolNames(ts, owner)["hardware_advice"]
	if tool == nil || tool.untrusted {
		t.Fatal("tool")
	}
	out, err := tool.own(t.Context(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.Text, "shop.example") {
		t.Fatalf("offer without prices: %s", out.Text)
	}
}
