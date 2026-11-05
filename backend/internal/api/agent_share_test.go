package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/shares"
)

// PLAN 49: the share KI-Agent with a folder per account. Chats and projects
// are copies; a changed chat file comes back as content, never as a
// command; no account sees another one's folder.
func TestAgentShareFolders(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetStandard})
	mnt := filepath.Dir(filepath.Dir(ts.withShare()))
	ts.users.Create("gast", testPassword, auth.RoleUser)
	_, body := ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	settings["users"] = []string{"gast"}
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}

	// A project and a chat in it.
	ts.request("POST", "/api/v1/agent/projects", map[string]any{"name": "Steuer 2026", "instructions": "Belege nach Monat ordnen."})
	res, body := ts.request("GET", "/api/v1/agent/projects", nil)
	var projects struct{ Projects []agent.Project }
	json.Unmarshal(body, &projects)
	if len(projects.Projects) != 1 {
		t.Fatalf("project: %d %s", res.StatusCode, body)
	}
	fake.replies = []string{`{"role":"assistant","content":"Die Rechnung ist vom Oktober."}`}
	res, body = ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": "Von wann ist die Rechnung?", "project": projects.Projects[0].ID})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("chat: %d %s", res.StatusCode, body)
	}
	var chat agent.Chat
	json.Unmarshal(body, &chat)
	waitChat(t, ts, ts.client, chat.ID)

	ts.server.agentSyncShare()
	var share shares.Share
	for _, sh := range ts.shares.List() {
		if sh.Name == agentShareName {
			share = sh
		}
	}
	if !share.PerUser || share.Access["alex"] != shares.ReadWrite || share.Access["gast"] != shares.ReadWrite || share.Volume != "tank" {
		t.Fatalf("share: %+v", share)
	}
	home := filepath.Join(mnt, "tank", agentShareName, "alex")
	for _, dir := range agentFolders {
		if st, err := os.Stat(filepath.Join(home, dir)); err != nil || !st.IsDir() {
			t.Fatalf("folder %s: %v", dir, err)
		}
	}
	for _, f := range []string{"LIESMICH.md", "Wissen/TitanOS/docker.md", "Skills/TitanOS/Health check.md", "Projekte/Steuer 2026/Anweisungen.md"} {
		if _, err := os.Stat(filepath.Join(home, f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	matches, _ := filepath.Glob(filepath.Join(home, "Projekte", "Steuer 2026", "*.md"))
	var copyFile string
	for _, m := range matches {
		if !strings.HasSuffix(m, "Anweisungen.md") {
			copyFile = m
		}
	}
	data, _ := os.ReadFile(copyFile)
	if !strings.Contains(string(data), "## Du") || !strings.Contains(string(data), "vom Oktober") {
		t.Fatalf("chat copy %s: %s", copyFile, data)
	}

	// Each account sees only its own folder, in the file manager too.
	roots := func(name string) string {
		u, _ := ts.users.Get(name)
		for _, r := range ts.server.fileRoots(u) {
			if r.ID == "share/"+agentShareName {
				return r.Dir
			}
		}
		return ""
	}
	if roots("alex") != home || roots("gast") != filepath.Join(mnt, "tank", agentShareName, "gast") {
		t.Fatalf("roots: %q %q", roots("alex"), roots("gast"))
	}

	// A link the person puts there leads nowhere outside.
	outside := t.TempDir()
	os.Remove(filepath.Join(home, "Notizen"))
	os.RemoveAll(filepath.Join(home, "Notizen"))
	if err := os.Symlink(outside, filepath.Join(home, "Notizen")); err != nil {
		t.Fatal(err)
	}
	ts.server.agentMirror.mu.Lock()
	ts.server.agentMirror.state["alex"].Version = "old"
	ts.server.agentMirror.mu.Unlock()
	ts.server.agentSyncShare()
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("wrote through a link: %v", entries)
	}

	// The person changes the copy; the next task reads it as content.
	os.WriteFile(copyFile, []byte("# Rechnung\n\nEs ist die Telekom-Rechnung, nicht Vodafone."), 0o660)
	fake.replies = []string{`{"role":"assistant","content":"Verstanden."}`}
	if res, body := ts.request("POST", "/api/v1/agent/chats/"+chat.ID+"/messages", map[string]any{"message": "Und?"}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("message: %d %s", res.StatusCode, body)
	}
	done := waitChat(t, ts, ts.client, chat.ID)
	edited := false
	for _, e := range done.Entries {
		if e.Role == "note" && e.Note == "edited" && strings.Contains(e.Content, "Telekom-Rechnung") {
			edited = true
		}
	}
	if !edited || !done.Tainted {
		t.Fatalf("edit not taken as content: tainted=%v %+v", done.Tainted, done.Entries)
	}
	// The same change is taken once, also when the next task comes before
	// the copy was written anew; then the copy has the note in it.
	fake.replies = []string{`{"role":"assistant","content":"Noch einmal."}`}
	ts.request("POST", "/api/v1/agent/chats/"+chat.ID+"/messages", map[string]any{"message": "Weiter"})
	done = waitChat(t, ts, ts.client, chat.ID)
	ts.server.agentSyncShare()
	fake.replies = []string{`{"role":"assistant","content":"Und noch einmal."}`}
	ts.request("POST", "/api/v1/agent/chats/"+chat.ID+"/messages", map[string]any{"message": "Weiter"})
	done = waitChat(t, ts, ts.client, chat.ID)
	count := 0
	for _, e := range done.Entries {
		if e.Note == "edited" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("edited notes: %d", count)
	}
	// A very long copy is not read back.
	ts.server.agentSyncShare()
	ts.server.agentMirror.mu.Lock()
	file := ts.server.agentMirror.state["alex"].Chats[chat.ID].File
	ts.server.agentMirror.mu.Unlock()
	os.WriteFile(filepath.Join(home, file), []byte(strings.Repeat("Langer Text. ", 30000)), 0o660)
	fake.replies = []string{`{"role":"assistant","content":"Gut."}`}
	ts.request("POST", "/api/v1/agent/chats/"+chat.ID+"/messages", map[string]any{"message": "Weiter"})
	done = waitChat(t, ts, ts.client, chat.ID)
	count = 0
	for _, e := range done.Entries {
		if e.Note == "edited" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("a long file was read back: %d edited notes", count)
	}

	// Folders per account are not for shares made over the API.
	if res, body := ts.request("POST", "/api/v1/shares", map[string]any{"name": "Privat", "volume": "tank", "smb": true, "perUser": true}); res.StatusCode != http.StatusCreated {
		t.Fatalf("share: %d %s", res.StatusCode, body)
	}
	for _, sh := range ts.shares.List() {
		if sh.Name == "Privat" && sh.PerUser {
			t.Fatal("perUser from the client")
		}
	}
}

// PLAN 51: a project shared with another account. The member sees it and
// the owner's chats in it (read-only) and starts its own chats there; the
// owner sees those; nobody else sees anything.
func TestAgentSharedProject(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetStandard})
	ts.users.Create("maria", testPassword, auth.RoleUser)
	ts.users.Create("ben", testPassword, auth.RoleUser)
	_, body := ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	settings["users"] = []string{"maria", "ben"}
	ts.request("PUT", "/api/v1/agent/settings", settings)
	if res, body := ts.request("POST", "/api/v1/agent/projects", map[string]any{"name": "Urlaub 2027", "instructions": "Wir fahren nach Norwegen.", "members": []string{"maria", "fremd"}}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown member: %d %s", res.StatusCode, body)
	}
	res, body := ts.request("POST", "/api/v1/agent/projects", map[string]any{"name": "Urlaub 2027", "instructions": "Wir fahren nach Norwegen.", "members": []string{"maria", "alex"}})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("project: %d %s", res.StatusCode, body)
	}
	var p agent.Project
	json.Unmarshal(body, &p)
	if len(p.Members) != 1 || p.Members[0] != "maria" {
		t.Fatalf("members: %+v", p.Members)
	}
	fake.replies = []string{
		`{"role":"assistant","content":"","thinking":"Erst im Dateisystem nachsehen.","tool_calls":[{"function":{"name":"files_list","arguments":{"root":"share/Docs","path":""}}}]}`,
		`{"role":"assistant","content":"Fähre ab Kiel."}`,
	}
	_, body = ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": "Wie kommen wir hin?", "project": p.ID})
	var ownerChat agent.Chat
	json.Unmarshal(body, &ownerChat)
	waitChat(t, ts, ts.client, ownerChat.ID)

	maria := ts.newClient()
	ts.do(maria, "POST", "/api/v1/login", credentials{Name: "maria", Password: testPassword})
	_, body = ts.do(maria, "GET", "/api/v1/agent/projects", nil)
	var mp struct{ Projects []agent.Project }
	json.Unmarshal(body, &mp)
	shared := "alex:" + p.ID
	if len(mp.Projects) != 1 || mp.Projects[0].ID != shared || mp.Projects[0].Owner != "alex" {
		t.Fatalf("maria projects: %s", body)
	}
	_, body = ts.do(maria, "GET", "/api/v1/agent/chats", nil)
	if !strings.Contains(string(body), `"author":"alex"`) || !strings.Contains(string(body), ownerChat.ID) {
		t.Fatalf("maria chats: %s", body)
	}
	res, body = ts.do(maria, "GET", "/api/v1/agent/chats/"+ownerChat.ID+"?author=alex", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), "Fähre ab Kiel") || !strings.Contains(string(body), `"readOnly":true`) {
		t.Fatalf("read owner's chat: %d %s", res.StatusCode, body)
	}
	// Only questions and answers: no results, arguments or thoughts of the
	// owner's steps (they came from the owner's rights).
	for _, leak := range []string{`"result"`, `"args"`, `"thinking"`, `"files"`, `"pending"`, "Geheime Tabelle"} {
		if strings.Contains(string(body), leak) {
			t.Fatalf("foreign chat carries %s: %s", leak, body)
		}
	}
	if res, _ := ts.do(maria, "POST", "/api/v1/agent/chats/"+ownerChat.ID+"/messages", map[string]any{"message": "x"}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("maria wrote into alex's chat: %d", res.StatusCode)
	}
	// Maria's own chat in the shared project, with its instructions.
	fake.replies = []string{`{"role":"assistant","content":"Hütte in Bergen."}`}
	res, body = ts.do(maria, "POST", "/api/v1/agent/chats", map[string]any{"message": "Wo schlafen wir?", "project": shared})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("maria chat: %d %s", res.StatusCode, body)
	}
	var mariaChat agent.Chat
	json.Unmarshal(body, &mariaChat)
	waitChat(t, ts, maria, mariaChat.ID)
	fake.mu.Lock()
	sys := fmt.Sprint(fake.got[len(fake.got)-1]["messages"])
	fake.mu.Unlock()
	if !strings.Contains(sys, "Norwegen") {
		t.Fatal("instructions of the shared project missing")
	}
	_, body = ts.request("GET", "/api/v1/agent/chats", nil)
	if !strings.Contains(string(body), `"author":"maria"`) || !strings.Contains(string(body), mariaChat.ID) {
		t.Fatalf("owner does not see maria's chat: %s", body)
	}
	// Ben is no member: nothing.
	ben := ts.newClient()
	ts.do(ben, "POST", "/api/v1/login", credentials{Name: "ben", Password: testPassword})
	_, body = ts.do(ben, "GET", "/api/v1/agent/chats", nil)
	if strings.Contains(string(body), ownerChat.ID) || strings.Contains(string(body), mariaChat.ID) {
		t.Fatalf("ben sees shared chats: %s", body)
	}
	if res, _ := ts.do(ben, "GET", "/api/v1/agent/chats/"+ownerChat.ID+"?author=alex", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("ben reads alex's chat: %d", res.StatusCode)
	}
	if res, _ := ts.do(ben, "POST", "/api/v1/agent/chats", map[string]any{"message": "x", "project": shared}); res.StatusCode == http.StatusCreated {
		t.Fatal("ben started a chat in a project not shared with him")
	}
}
