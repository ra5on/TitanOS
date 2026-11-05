package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// PLAN 53: initiative per account, suggestions, nightly thinking.
func TestAgentInitiative(t *testing.T) {
	content, _ := json.Marshal(`Gern: {"facts":["Die Backup-SSD hängt am USB-Hub im Flur","Das Passwort ist geheim123","{\"tool\":1}","Antwortet lieber kurz"]}`)
	fake := &agentOllama{replies: []string{`{"role":"assistant","content":` + string(content) + `}`}}
	ts := agentServer(t, fake, agent.Policy{})
	_, body := ts.request("GET", "/api/v1/agent/initiative", nil)
	var in agentInitiative
	json.Unmarshal(body, &in)
	if in.Level != "suggest" || !in.Reflect || !in.Confirm || in.Wake != "wake" {
		t.Fatalf("defaults: %s", body)
	}
	in.Length, in.Name, in.Search = "short", "Titan", []agentFolderRef{{Root: "share/Docs"}}
	if res, body := ts.request("PUT", "/api/v1/agent/initiative", in); res.StatusCode != 200 {
		t.Fatalf("save: %d %s", res.StatusCode, body)
	}
	bad := in
	bad.ReflectAt = "25:99"
	if res, _ := ts.request("PUT", "/api/v1/agent/initiative", bad); res.StatusCode != 400 {
		t.Fatal("bad time accepted")
	}
	// The weekly review became a planned task.
	list, _ := ts.server.agentStore().Schedules("alex")
	if len(list) != 1 || list[0].Name != "Wochenrückblick" {
		t.Fatalf("weekly: %+v", list)
	}
	// Personality reaches the prompt.
	ini, _ := ts.server.agentInitiativeOf("alex")
	style := agentStyle(ini)
	if !strings.Contains(style, "Titan") || !strings.Contains(style, "short") {
		t.Fatalf("style: %s", style)
	}

	// Nightly thinking: facts go to Vorschläge, secrets and noise do not.
	store := ts.server.agentStore()
	now := time.Now()
	chat := &agent.Chat{ID: agent.NewID(), Title: "Backup", Created: now, Updated: now, State: agent.StateIdle, Origin: "web",
		Entries: []agent.Entry{{ID: agent.NewID(), At: now, Role: "user", Content: "Die SSD fürs Backup hängt am Hub im Flur."}, {ID: agent.NewID(), At: now, Role: "assistant", Content: "Gemerkt."}}}
	if err := store.SaveChat("alex", chat); err != nil {
		t.Fatal(err)
	}
	u, _ := ts.users.Get("alex")
	ini, _ = ts.server.agentInitiativeOf("alex")
	ts.server.agentReflect(u, ini, now)
	items := ts.server.agentInbox("alex")
	texts := []string{}
	for _, it := range items {
		texts = append(texts, it.Title)
	}
	if len(items) != 2 || items[0].Title != "Die Backup-SSD hängt am USB-Hub im Flur" {
		t.Fatalf("inbox: %v", texts)
	}
	if ini, _ := ts.server.agentInitiativeOf("alex"); ini.LastReflect.IsZero() {
		t.Fatal("last run not kept")
	}
	if !strings.Contains(nightNote("alex"), "Backup-SSD") || nightNote("alex") != "" {
		t.Fatal("morning note not once")
	}
	// Accepting makes a memory; a second, different sentence about the same
	// thing asks whether the old one still holds.
	if res, body := ts.request("POST", "/api/v1/agent/inbox/"+items[0].ID, map[string]any{"accept": true}); res.StatusCode != 200 {
		t.Fatalf("accept: %d %s", res.StatusCode, body)
	}
	mems, _ := store.Memories("alex")
	if len(mems) != 1 {
		t.Fatalf("memories: %+v", mems)
	}
	if conflict := memoryConflict(mems, "Die Backup-SSD hängt jetzt am USB-Hub im Keller"); conflict == nil {
		t.Fatal("no conflict found")
	}
	if memoryConflict(mems, "Plex läuft als Docker-App") != nil {
		t.Fatal("unrelated memory taken as conflict")
	}
	// Search places: Backup is not one, unless the person names it.
	box := ts.server.newAgentToolbox(u, ts.server.agentSettings())
	mode := agent.ModeAllow
	ask := &agent.Chat{Entries: []agent.Entry{{Role: "user", Content: "Such die Rechnung"}}}
	if err := box.initiativeCheck(t.Context(), ask, "files_search", json.RawMessage(`{"root":"share/Backup"}`), &mode); err == nil {
		t.Fatal("searched outside the search places")
	}
	named := &agent.Chat{Entries: []agent.Entry{{Role: "user", Content: "Such auch im Backup"}}}
	if err := box.initiativeCheck(t.Context(), named, "files_search", json.RawMessage(`{"root":"share/Backup"}`), &mode); err != nil {
		t.Fatal(err)
	}
	// Another account has its own settings and suggestions.
	ts.users.Create("gast", testPassword, auth.RoleUser)
	if g, _ := ts.server.agentInitiativeOf("gast"); g.Name != "" || len(ts.server.agentInbox("gast")) != 0 {
		t.Fatal("settings leak between accounts")
	}
}

func TestAgentInitiativeHelpers(t *testing.T) {
	loc := time.Local
	in := defaultInitiative()
	at := time.Date(2026, 11, 6, 3, 31, 0, 0, loc)
	if !reflectDue(in, at) || reflectDue(in, at.Add(-2*time.Minute)) {
		t.Fatal("due at 03:30")
	}
	in.LastReflect = at
	if reflectDue(in, at.Add(time.Hour)) {
		t.Fatal("twice a night")
	}
	samples := []volumeSample{}
	for i := 0; i < 10; i++ {
		samples = append(samples, volumeSample{Used: uint64(100 + i*10), Size: 1000})
	}
	if d := daysUntilFull(samples); d < 80 || d > 82 {
		t.Fatalf("days: %d", d)
	}
	if daysUntilFull(samples[:3]) != 0 {
		t.Fatal("forecast from three days")
	}
	for _, noise := range []string{"You are a helpful assistant", "call memory_save", "{x}", "kurz"} {
		if !reflectNoise(noise) {
			t.Errorf("not noise: %q", noise)
		}
	}
	if got := parseReflect(`x {"facts":["a  b"]} y`); len(got) != 1 || got[0] != "a b" {
		t.Fatalf("parse: %v", got)
	}
}
