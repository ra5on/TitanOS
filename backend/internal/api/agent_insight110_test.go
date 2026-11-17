package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// agentOn switches the agent on for a test server built elsewhere (the
// NVR server), as agentServer does.
func agentOn(t *testing.T, ts *testServer, fake *agentOllama, policy agent.Policy) {
	t.Helper()
	dir := t.TempDir()
	ts.server.agentOpen = func() (*os.Root, error) { return os.OpenRoot(dir) }
	t.Cleanup(ts.server.agentRT.work.Wait)
	settings := map[string]any{"enabled": true, "ollama": fake.start(t), "model": "gpt-oss:20b", "policy": policy}
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	ts.server.shareSyncs.Wait()
}

// 1.10 (V2, V1): agent_access tells the agent its rights and what it
// cannot see – the folder rules only as a count, a switched-off service
// with where to switch it on – and tools_load says why an area is missing.
func TestAgentAccessSaysWhatIsHidden(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetStandard, Folders: []agent.FolderRule{
		{Root: "share/Docs", Path: "Finanzen/Privat", Mode: agent.ModeDeny}, {Root: "share/Docs", Path: "Steuer", Mode: agent.ModeAsk}}})
	if res, body := ts.request("PUT", "/api/v1/services/docker", map[string]any{"enabled": false}); res.StatusCode != 200 {
		t.Fatalf("docker off: %d %s", res.StatusCode, body)
	}
	result, text, err := runAgentTool(t, ts, ts.server.agentSettings(), "agent_access", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "Finanzen") || strings.Contains(text, "Steuer") {
		t.Fatalf("paths of folder rules shown: %s", text)
	}
	rules, _ := result["folderRules"].(map[string]any)
	if rules["count"] != float64(2) || rules["forbidden"] != float64(1) || rules["ask"] != float64(1) {
		t.Fatalf("folder rules: %v", rules)
	}
	if !strings.Contains(text, `{"areas":["docker"],"why":"Docker is switched off on this NAS (Einstellungen › Dienste)"}`) {
		t.Fatalf("docker off not said: %v", result["notAvailable"])
	}
	if mail, _ := result["mail"].(string); !strings.Contains(mail, "closed to the agent") {
		t.Fatalf("mail: %v", result["mail"])
	}
	if result["role"] != "owner" {
		t.Fatalf("role: %v", result["role"])
	}

	// tools_load of the missing area gives the reason to the model.
	fake.replies = []string{toolCall("tools_load", map[string]any{"area": "docker"}), `{"role":"assistant","content":"Docker ist aus."}`}
	res, body := ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": "Welche Container laufen?"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("chat: %d %s", res.StatusCode, body)
	}
	var started agent.Chat
	json.Unmarshal(body, &started)
	chat := waitChat(t, ts, ts.client, started.ID)
	found := false
	for _, e := range chat.Entries {
		if e.Role == "tool" && e.Tool == "tools_load" && strings.Contains(e.Result, "Docker is switched off") {
			found = true
		}
	}
	if !found {
		t.Fatalf("tools_load did not say why: %+v", chat.Entries)
	}
	// The prompt names the missing areas.
	fake.mu.Lock()
	msgs, _ := fake.got[0]["messages"].([]any)
	fake.mu.Unlock()
	system, _ := msgs[0].(map[string]any)["content"].(string)
	if !strings.Contains(system, "Not available to you here: ") || !strings.Contains(system, "docker") {
		t.Fatalf("prompt without the missing areas")
	}

	// A user: no Home Assistant, said as such.
	ts.users.Create("gast", testPassword, auth.RoleUser)
	box := ts.server.newAgentToolbox(mustUser(t, ts, "gast"), ts.server.agentSettings())
	if why := box.Missing()["home"]; !strings.Contains(why, "only for owners and administrators") {
		t.Fatalf("user, home: %q", why)
	}
	out, _ := box.tools["agent_access"].own(context.Background(), nil, nil)
	if !strings.Contains(out.Text, "only for owners and administrators") || strings.Contains(out.Text, `"nvr":{`) || strings.Count(out.Text, "only for owners and administrators; this account is a user") != 1 {
		t.Fatalf("user access: %s", out.Text)
	}
}

// 1.10 (V1): a listing says how many entries a locked folder hid, not which.
func TestAgentFolderRulesCountWhatTheyHide(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard, Folders: []agent.FolderRule{{Root: "share/Docs", Path: "Privat", Mode: agent.ModeDeny}}})
	dir := ts.withShare()
	os.MkdirAll(filepath.Join(dir, "Privat"), 0o755)
	os.WriteFile(filepath.Join(dir, "liste.md"), []byte("x"), 0o644)
	result, text, err := runAgentTool(t, ts, ts.server.agentSettings(), "files_list", map[string]any{"root": "share/Docs"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "Privat") || result["hidden"] != float64(1) {
		t.Fatalf("hidden: %s", text)
	}
	if why, _ := result["why"].(string); !strings.Contains(why, "Ordner-Regeln") {
		t.Fatalf("why: %q", why)
	}
	// Nothing locked here: no hidden field.
	_, text, _ = runAgentTool(t, ts, ts.server.agentSettings(), "files_list", map[string]any{"root": "share/Docs", "path": "Privat/.."})
	_ = text
}

// 1.10 (V7): the new reading tools are only for owners and administrators,
// under the rights of the agent; the new areas stand under extraAreas on
// the stick so that 1.09 can still save its settings.
func TestAgentInsightToolsRights(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetRead})
	log := activity.New(ts.store)
	ts.server.opts.Activity = log
	log.Record(activity.Entry{At: time.Now().Add(-2 * time.Hour), User: "alex", Via: "agent", IP: "192.0.2.7", Action: "files.delete", Target: "share/Docs/alt.txt", OK: true})
	log.Record(activity.Entry{At: time.Now().Add(-50 * time.Hour), User: "alex", Via: "web", Action: "share.create", Target: "Alt", OK: true})
	ts.users.Create("gast", testPassword, auth.RoleUser)
	user := ts.server.newAgentToolbox(mustUser(t, ts, "gast"), ts.server.agentSettings())
	for _, name := range []string{"activity_log", "monitoring_history", "capacity_forecast", "nvr_events", "ha_logbook", "release_notes"} {
		if _, ok := user.tools[name]; ok {
			t.Errorf("a user gets %s", name)
		}
	}
	// "Nur lesen" reads the activity log; addresses stay out, the window holds.
	result, text, err := runAgentTool(t, ts, ts.server.agentSettings(), "activity_log", map[string]any{"since": "24h"})
	if err != nil {
		t.Fatal(err)
	}
	if result["total"] != float64(1) || strings.Contains(text, "192.0.2.7") || !strings.Contains(text, "files.delete") || strings.Contains(text, "share.create") {
		t.Fatalf("activity: %s", text)
	}
	result, _, _ = runAgentTool(t, ts, ts.server.agentSettings(), "activity_log", map[string]any{"since": "3d", "via": "web"})
	if result["total"] != float64(1) {
		t.Fatalf("activity via web: %v", result)
	}
	// The area "Verlauf" forbidden: not offered and refused.
	settings := ts.server.agentSettings()
	settings.Policy.Areas = map[string]string{agent.AreaHistory: agent.ModeDeny}
	box := ts.server.newAgentToolbox(mustUser(t, ts, "alex"), settings)
	for _, s := range box.Specs(nil) {
		if s.Function.Name == "activity_log" {
			t.Fatal("forbidden area offered")
		}
	}
	if _, _, err := runAgentTool(t, ts, settings, "activity_log", map[string]any{}); err == nil {
		t.Fatal("forbidden area ran")
	}
	// The monitoring history switched off is no "all quiet".
	if ts.server.opts.Monitoring != nil {
		result, _, err = runAgentTool(t, ts, ts.server.agentSettings(), "monitoring_history", map[string]any{})
		if err != nil || result["hidden"] != true || !strings.Contains(result["why"].(string), "switched off") {
			t.Fatalf("monitoring off: %v %v", result, err)
		}
	}

	// The page of the rights shows the area NVR also before a camera exists.
	_, body := ts.request("GET", "/api/v1/agent/tools", nil)
	if !strings.Contains(string(body), `{"name":"nvr_events","area":"nvr"`) || !strings.Contains(string(body), `"activity_log"`) {
		t.Fatalf("tools of the page: %.300s", body)
	}

	// New areas go to extraAreas on the stick; read back as one.
	_, body = ts.request("GET", "/api/v1/agent/settings", nil)
	var put map[string]any
	json.Unmarshal(body, &put)
	put["policy"] = map[string]any{"preset": "standard", "areas": map[string]any{"history": "deny", "nvr": "allow", "files": "ask"}}
	put["nvrNames"] = true
	if res, body := ts.request("PUT", "/api/v1/agent/settings", put); res.StatusCode != 200 {
		t.Fatalf("save: %d %s", res.StatusCode, body)
	}
	data, err := ts.store.Read(agentSettingsFile)
	if err != nil {
		t.Fatal(err)
	}
	var stick struct {
		Policy struct {
			Areas      map[string]string `json:"areas"`
			ExtraAreas map[string]string `json:"extraAreas"`
		} `json:"policy"`
		NVRNames bool `json:"nvrNames"`
	}
	json.Unmarshal(data, &stick)
	if stick.Policy.Areas["history"] != "" || stick.Policy.Areas["files"] != "ask" || stick.Policy.ExtraAreas["history"] != "deny" || stick.Policy.ExtraAreas["nvr"] != "allow" || !stick.NVRNames {
		t.Fatalf("on the stick: %s", data)
	}
	got := ts.server.agentSettings().Policy
	if got.Areas["history"] != "deny" || got.Areas["nvr"] != "allow" || got.Areas["files"] != "ask" {
		t.Fatalf("read back: %v", got.Areas)
	}
	// What 1.09 sends back (areas without the new ones, extraAreas it does
	// not know of) keeps the rest.
	var old agent.Policy
	if err := json.Unmarshal([]byte(`{"preset":"standard","areas":{"files":"ask"}}`), &old); err != nil || old.Areas["files"] != "ask" {
		t.Fatalf("old policy: %v %v", old, err)
	}
}

// 1.10 (V7): camera events ask first even under "Alles ohne Nachfrage",
// give no pictures and no names unless switched on.
func TestAgentNVREventsAskAndHideNames(t *testing.T) {
	ts, _ := nvrServer(t)
	ts.setup()
	nvrRunning(t, ts)
	agentOn(t, ts, &agentOllama{}, agent.Policy{Preset: agent.PresetAll})
	settings := ts.server.agentSettings()
	if _, _, err := runAgentTool(t, ts, settings, "nvr_events", map[string]any{}); err != errAsks {
		t.Fatalf("nvr_events without asking under \"Alles\": %v", err)
	}
	settings.Policy.Areas = map[string]string{agent.AreaNVR: agent.ModeAllow}
	result, text, err := runAgentTool(t, ts, settings, "nvr_events", map[string]any{"since": "2d"})
	if err != nil {
		t.Fatal(err)
	}
	events, _ := result["events"].([]any)
	if len(events) == 0 {
		t.Fatalf("no events: %s", text)
	}
	for _, bad := range []string{"Anna", "snapshot", "clip", "thumbnail", ".jpg"} {
		if strings.Contains(text, bad) {
			t.Fatalf("%s in the events: %.500s", bad, text)
		}
	}
	settings.NVRNames = true
	_, text, _ = runAgentTool(t, ts, settings, "nvr_events", map[string]any{"since": "2d", "limit": 100})
	if !strings.Contains(text, `"who":"Anna"`) {
		t.Fatalf("names switched on but missing: %.800s", text)
	}
	if _, _, err := runAgentTool(t, ts, settings, "nvr_events", map[string]any{"camera": "gibt-es-nicht"}); err == nil || !strings.Contains(err.Error(), "cameras:") {
		t.Fatalf("unknown camera: %v", err)
	}
	// A user never gets it.
	ts.users.Create("gast", testPassword, auth.RoleUser)
	if _, ok := ts.server.newAgentToolbox(mustUser(t, ts, "gast"), settings).tools["nvr_events"]; ok {
		t.Fatal("user has nvr_events")
	}
}

// 1.10 (V14): "Was ist neu?" reads the notes of the running version, and
// says so instead of inventing when the build has none.
func TestAgentReleaseNotes(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	ts.server.manual = toolcatalog.NewManual(fstest.MapFS{
		"releases/v1.10.0.md": {Data: []byte("# TitanOS 1.10\n\nNeu: agent_access.\n")},
		"releases/v1.9.0.md":  {Data: []byte("# TitanOS 1.09\n\nNeu: Automationen.\n")},
	})
	box := ts.server.newAgentToolbox(mustUser(t, ts, "alex"), ts.server.agentSettings())
	run := func(args string) map[string]any {
		t.Helper()
		out, err := box.tools["release_notes"].own(context.Background(), nil, json.RawMessage(args))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		json.Unmarshal([]byte(out.Text), &m)
		return m
	}
	box.version = "1.10.0"
	if m := run(`{}`); !strings.Contains(m["text"].(string), "agent_access") || m["running"] != "1.10" {
		t.Fatalf("running: %v", m)
	}
	if m := run(`{"version":"1.09"}`); !strings.Contains(m["text"].(string), "Automationen") {
		t.Fatalf("1.09: %v", m)
	}
	box.version = "1.2.3"
	if m := run(`{}`); m["hidden"] != true || !strings.Contains(m["why"].(string), "Do not make up") {
		t.Fatalf("missing notes: %v", m)
	}
}

// 1.10 (V15): a planned task can be taken back by the agent.
func TestAgentScheduleDelete(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	store := ts.server.agentStore()
	if err := store.SaveSchedules("alex", []agent.Schedule{{ID: "s1", Name: "Müll", Task: "Müll raus", Kind: "reminder", Every: "once", Date: "2026-11-18", At: "09:00"}}); err != nil {
		t.Fatal(err)
	}
	if _, text, err := runAgentTool(t, ts, ts.server.agentSettings(), "schedule_delete", map[string]any{"id": "s1"}); err != nil || !strings.Contains(text, "Müll") {
		t.Fatalf("delete: %q %v", text, err)
	}
	if list, _ := store.Schedules("alex"); len(list) != 0 {
		t.Fatalf("still there: %v", list)
	}
	if _, _, err := runAgentTool(t, ts, ts.server.agentSettings(), "schedule_delete", map[string]any{"id": "s1"}); err == nil {
		t.Fatal("deleted twice")
	}
}

func TestParseMoment(t *testing.T) {
	now := time.Date(2026, 11, 17, 8, 0, 0, 0, time.Local)
	for in, want := range map[string]time.Time{
		"":                          {},
		"8h":                        now.Add(-8 * time.Hour),
		"2d":                        now.AddDate(0, 0, -2),
		"2026-11-17T03:00":          time.Date(2026, 11, 17, 3, 0, 0, 0, time.Local),
		"2026-11-17 03:00":          time.Date(2026, 11, 17, 3, 0, 0, 0, time.Local),
		"2026-11-16":                time.Date(2026, 11, 16, 0, 0, 0, 0, time.Local),
		"2026-11-17T03:00:00+01:00": time.Date(2026, 11, 17, 3, 0, 0, 0, time.FixedZone("", 3600)),
	} {
		got, err := parseMoment(in, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("%q: %v %v, want %v", in, got, err, want)
		}
	}
	if _, err := parseMoment("gestern", now); err == nil {
		t.Error("gestern read as a moment")
	}
}
