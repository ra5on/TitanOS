package api

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

func TestAgentTextSearchAndTemplates(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	db := ts.server.agentStore()
	now := time.Now()
	makeChat := func(user, project string) *agent.Chat {
		c := &agent.Chat{ID: agent.NewID(), Title: "Auftrag", Archived: true, Project: project, Created: now, Updated: now, State: agent.StateIdle}
		for i := 0; i < 42; i++ {
			c.Entries = append(c.Entries, agent.Entry{ID: agent.NewID(), At: now, Role: "user", Content: "Ältere Sicherungsprobe im Archiv"})
		}
		c.Entries = append(c.Entries, agent.Entry{ID: agent.NewID(), Role: "tool", Result: "Kontointerner Rohwert"})
		if err := db.SaveChat(user, c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	chat := makeChat("alex", "")
	search := func(q string) (out struct {
		Hits   []agentTextHit
		After  string
		Offset int
	}) { res, body := ts.request("GET", "/api/v1/agent/text-search?"+q, nil); if res.StatusCode != 200 || json.Unmarshal(body, &out) != nil {
		t.Fatalf("search %d %s", res.StatusCode, body)
	}; return }
	first := search("q=SICHERUNGSPROBE")
	if len(first.Hits) != 40 || first.After == "" || first.Offset != 40 || !first.Hits[0].Archived || first.Hits[0].Chat != chat.ID {
		t.Fatalf("first page %#v", first)
	}
	second := search("q=sicherungsprobe&after=" + url.QueryEscape(first.After) + "&offset=40")
	if len(second.Hits) != 2 || second.Hits[0].Entry != chat.Entries[40].ID {
		t.Fatalf("next page %#v", second)
	}
	if len(search("q=Rohwert").Hits) != 0 {
		t.Fatal("raw tool result returned")
	}
	if _, err := ts.users.Create("maria", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	_, body := ts.request("GET", "/api/v1/agent/settings", nil)
	var prefs map[string]any
	if err := json.Unmarshal(body, &prefs); err != nil {
		t.Fatal(err)
	}
	prefs["users"] = []string{"maria"}
	ts.request("PUT", "/api/v1/agent/settings", prefs)
	_, body = ts.request("POST", "/api/v1/agent/projects", map[string]any{"name": "Familie", "members": []string{"maria"}})
	var project agent.Project
	if err := json.Unmarshal(body, &project); err != nil || project.ID == "" {
		t.Fatal(string(body))
	}
	shared := makeChat("maria", "alex:"+project.ID)
	found := search("q=sicherungsprobe&project=" + project.ID)
	if len(found.Hits) != 40 || found.Hits[0].Chat != shared.ID || found.Hits[0].Author != "maria" {
		t.Fatal("shared search missing")
	}
	if err := db.SaveProjects("alex", nil); err != nil {
		t.Fatal(err)
	}
	if len(search("q=sicherungsprobe&project="+project.ID).Hits) > 0 {
		t.Fatal("revoked project remained visible")
	}
	const path = "/api/v1/agent/prompt-templates"
	prompt := map[string]string{"name": "Probe", "text": "Prüfe die Sicherungen."}
	for range 2 {
		res, body := ts.request("POST", path, prompt)
		if res.StatusCode != 200 || strings.Count(string(body), prompt["text"]) != 1 {
			t.Fatalf("save %d %s", res.StatusCode, body)
		}
	}
	if list, err := db.PromptTemplates("maria"); err != nil || len(list) != 0 {
		t.Fatal("foreign template visible", err)
	}
}
