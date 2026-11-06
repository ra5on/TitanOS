package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// covBlockingOllama answers everything at once except real chat requests,
// which hang until the request ends (stop) or the test ends.
func covBlockingOllama(t *testing.T) string {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.Write([]byte(`{"version":"0.40.14"}`))
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"gpt-oss:20b","size":1,"details":{"context_length":131072},"capabilities":["completion","tools","thinking"]}]}`))
		case "/api/show":
			w.Write([]byte(`{"capabilities":["completion","tools","thinking"],"thinking":{"values":["low","medium","high"],"default":"medium"}}`))
		case "/api/ps":
			w.Write([]byte(`{"models":[{"name":"gpt-oss:20b","context_length":32768,"size_vram":1}]}`))
		case "/api/chat":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if msgs, ok := body["messages"].([]any); ok && len(msgs) == 0 {
				w.Write([]byte(`{"model":"gpt-oss:20b","message":{"role":"assistant","content":""},"done":true,"done_reason":"load"}`))
				return
			}
			select {
			case <-r.Context().Done():
			case <-release:
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	return srv.URL
}

// covGuest makes the account gast, allowed to use the agent, and logs it in.
func covGuest(t *testing.T, ts *testServer) *http.Client {
	t.Helper()
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	if res, _ := ts.do(guest, "GET", "/api/v1/agent/memory", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("guest without the agent: %d", res.StatusCode)
	}
	_, body := ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	settings["users"] = []string{"gast"}
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusOK {
		t.Fatalf("allow guest: %d %s", res.StatusCode, body)
	}
	return guest
}

func TestCovAgentMemoryRoundTrip(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{})
	list := func(client *http.Client) []agent.Memory {
		res, body := ts.do(client, "GET", "/api/v1/agent/memory", nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("list: %d %s", res.StatusCode, body)
		}
		var out struct{ Memory []agent.Memory }
		json.Unmarshal(body, &out)
		return out.Memory
	}
	for _, bad := range []string{"", "   ", string(make([]byte, 600))} {
		if res, _ := ts.request("PUT", "/api/v1/agent/memory/new", map[string]any{"text": bad}); res.StatusCode != http.StatusBadRequest {
			t.Errorf("accepted %q: %d", bad, res.StatusCode)
		}
	}
	if res, body := ts.request("PUT", "/api/v1/agent/memory/new", map[string]any{"text": "  Der   Drucker steht im Flur. "}); res.StatusCode != http.StatusOK {
		t.Fatalf("new: %d %s", res.StatusCode, body)
	}
	ts.request("PUT", "/api/v1/agent/memory/new", map[string]any{"text": "HA ist die VM."})
	got := list(ts.client)
	if len(got) != 2 || got[0].Text != "Der Drucker steht im Flur." || got[0].ID == "" {
		t.Fatalf("list: %+v", got)
	}
	// A memory from outside content loses the mark once the person edits it.
	store := ts.server.agentStore()
	mem, _ := store.Memories("alex")
	mem[1].Untrusted = true
	store.SaveMemories("alex", mem)
	if res, _ := ts.request("PUT", "/api/v1/agent/memory/"+mem[1].ID, map[string]any{"text": "HA ist die VM HAOS."}); res.StatusCode != http.StatusOK {
		t.Fatal("edit")
	}
	got = list(ts.client)
	if got[1].Text != "HA ist die VM HAOS." || got[1].Untrusted {
		t.Fatalf("edited: %+v", got[1])
	}
	if res, _ := ts.request("PUT", "/api/v1/agent/memory/"+agent.NewID(), map[string]any{"text": "x"}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("edit unknown: %d", res.StatusCode)
	}

	// Another account sees none of it and cannot touch it.
	guest := covGuest(t, ts)
	if g := list(guest); len(g) != 0 {
		t.Fatalf("guest sees %+v", g)
	}
	if res, _ := ts.do(guest, "DELETE", "/api/v1/agent/memory/"+got[0].ID, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("guest deletes the owner's memory: %d", res.StatusCode)
	}
	if res, _ := ts.do(guest, "PUT", "/api/v1/agent/memory/"+got[0].ID, map[string]any{"text": "gehackt"}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("guest edits the owner's memory: %d", res.StatusCode)
	}
	if res, _ := ts.do(guest, "DELETE", "/api/v1/agent/memory/all", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("guest clears own: %d", res.StatusCode)
	}
	if len(list(ts.client)) != 2 {
		t.Fatal("guest's clear removed the owner's memory")
	}

	if res, _ := ts.request("DELETE", "/api/v1/agent/memory/"+got[0].ID, nil); res.StatusCode != http.StatusNoContent {
		t.Fatal("delete")
	}
	if res, _ := ts.request("DELETE", "/api/v1/agent/memory/"+got[0].ID, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("delete twice: %d", res.StatusCode)
	}
	if res, _ := ts.request("DELETE", "/api/v1/agent/memory/all", nil); res.StatusCode != http.StatusNoContent || len(list(ts.client)) != 0 {
		t.Fatal("delete all")
	}
}

func TestCovAgentSkillsRoundTrip(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{})
	list := func(client *http.Client) []agent.Skill {
		res, body := ts.do(client, "GET", "/api/v1/agent/skills", nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("list: %d %s", res.StatusCode, body)
		}
		var out struct{ Skills []agent.Skill }
		json.Unmarshal(body, &out)
		return out.Skills
	}
	builtIn := len(agent.BuiltInSkills)
	if builtIn == 0 || len(list(ts.client)) != builtIn {
		t.Fatalf("built-in skills: %d", len(list(ts.client)))
	}
	first := agent.BuiltInSkills[0].ID
	if res, _ := ts.request("PUT", "/api/v1/agent/skills/"+first, map[string]any{"disabled": true}); res.StatusCode != http.StatusOK {
		t.Fatal("switch off built-in")
	}
	find := func(l []agent.Skill, id string) *agent.Skill {
		i := slices.IndexFunc(l, func(s agent.Skill) bool { return s.ID == id })
		if i < 0 {
			return nil
		}
		return &l[i]
	}
	if sk := find(list(ts.client), first); sk == nil || !sk.Disabled {
		t.Fatalf("built-in not switched off: %+v", sk)
	}
	if len(list(ts.client)) != builtIn {
		t.Fatal("switching a built-in added a skill")
	}
	ts.request("PUT", "/api/v1/agent/skills/"+first, map[string]any{"disabled": false})
	if sk := find(list(ts.client), first); sk == nil || sk.Disabled {
		t.Fatal("built-in not switched on again")
	}
	if res, _ := ts.request("PUT", "/api/v1/agent/skills/builtin-nope", map[string]any{"disabled": true}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown built-in: %d", res.StatusCode)
	}
	if res, _ := ts.request("DELETE", "/api/v1/agent/skills/"+first, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("built-in deleted: %d", res.StatusCode)
	}

	if res, _ := ts.request("PUT", "/api/v1/agent/skills/new", map[string]any{"name": "Ohne Schritte"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("skill without steps: %d", res.StatusCode)
	}
	if res, _ := ts.request("PUT", "/api/v1/agent/skills/new", map[string]any{"name": "Rechnungen", "when": "Rechnungen ablegen", "steps": "1. files_search"}); res.StatusCode != http.StatusOK {
		t.Fatal("new skill")
	}
	own := list(ts.client)[builtIn:]
	if len(own) != 1 || own[0].Name != "Rechnungen" || own[0].BuiltIn {
		t.Fatalf("own skills: %+v", own)
	}
	if res, _ := ts.request("PUT", "/api/v1/agent/skills/"+own[0].ID, map[string]any{"name": "Rechnungen", "when": "x", "steps": "1. files_list", "disabled": true}); res.StatusCode != http.StatusOK {
		t.Fatal("edit")
	}
	if sk := find(list(ts.client), own[0].ID); sk == nil || sk.Steps != "1. files_list" || !sk.Disabled {
		t.Fatalf("edited: %+v", sk)
	}

	guest := covGuest(t, ts)
	if g := list(guest); len(g) != builtIn {
		t.Fatalf("guest sees %d skills", len(g))
	}
	if res, _ := ts.do(guest, "DELETE", "/api/v1/agent/skills/"+own[0].ID, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("guest deletes the owner's skill: %d", res.StatusCode)
	}
	if res, _ := ts.do(guest, "PUT", "/api/v1/agent/skills/"+own[0].ID, map[string]any{"name": "x", "steps": "y"}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("guest edits the owner's skill: %d", res.StatusCode)
	}

	if res, _ := ts.request("DELETE", "/api/v1/agent/skills/"+own[0].ID, nil); res.StatusCode != http.StatusNoContent {
		t.Fatal("delete")
	}
	if len(list(ts.client)) != builtIn {
		t.Fatal("skill still listed")
	}
}

func TestCovAgentSchedulesRoundTrip(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{})
	type view struct {
		agent.Schedule
		Next string
		When string
	}
	list := func(client *http.Client) []view {
		res, body := ts.do(client, "GET", "/api/v1/agent/schedules", nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("list: %d %s", res.StatusCode, body)
		}
		var out struct{ Schedules []view }
		json.Unmarshal(body, &out)
		return out.Schedules
	}
	if res, _ := ts.request("PUT", "/api/v1/agent/schedules/new", map[string]any{"name": "x", "task": "y", "every": "daily", "at": "25:00", "enabled": true}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad time accepted: %d", res.StatusCode)
	}
	if res, body := ts.request("PUT", "/api/v1/agent/schedules/new", map[string]any{"name": "Check", "task": "Gesundheitscheck", "every": "daily", "at": "07:00", "enabled": true}); res.StatusCode != http.StatusOK {
		t.Fatalf("new: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("PUT", "/api/v1/agent/schedules/new", map[string]any{"name": "Morgen", "task": "x", "every": "daily", "at": "06:00", "kind": "morning", "enabled": true}); res.StatusCode != http.StatusOK {
		t.Fatal("morning")
	}
	if res, _ := ts.request("PUT", "/api/v1/agent/schedules/new", map[string]any{"name": "Morgen 2", "task": "x", "every": "daily", "at": "06:30", "kind": "morning", "enabled": true}); res.StatusCode != http.StatusConflict {
		t.Fatalf("second morning report: %d", res.StatusCode)
	}
	got := list(ts.client)
	if len(got) != 2 || got[0].Name != "Check" || got[0].When != "täglich um 07:00" || got[0].Next == "" || got[0].Created.IsZero() {
		t.Fatalf("list: %+v", got)
	}
	// Changing keeps what the server knows (created, last run).
	created := got[0].Created
	if res, _ := ts.request("PUT", "/api/v1/agent/schedules/"+got[0].ID, map[string]any{"name": "Check", "task": "Gesundheitscheck", "every": "daily", "at": "08:00", "enabled": false, "created": time.Time{}}); res.StatusCode != http.StatusOK {
		t.Fatal("edit")
	}
	got = list(ts.client)
	if got[0].At != "08:00" || got[0].Enabled || !got[0].Created.Equal(created) || got[0].Next != "" {
		t.Fatalf("edited: %+v", got[0])
	}

	guest := covGuest(t, ts)
	if g := list(guest); len(g) != 0 {
		t.Fatalf("guest sees %+v", g)
	}
	if res, _ := ts.do(guest, "DELETE", "/api/v1/agent/schedules/"+got[0].ID, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("guest deletes the owner's schedule: %d", res.StatusCode)
	}
	if res, _ := ts.do(guest, "PUT", "/api/v1/agent/schedules/"+got[0].ID, map[string]any{"name": "x", "task": "y", "every": "daily", "at": "01:00", "enabled": true}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("guest edits the owner's schedule: %d", res.StatusCode)
	}

	if res, _ := ts.request("DELETE", "/api/v1/agent/schedules/"+got[0].ID, nil); res.StatusCode != http.StatusNoContent {
		t.Fatal("delete")
	}
	if res, _ := ts.request("DELETE", "/api/v1/agent/schedules/"+got[0].ID, nil); res.StatusCode != http.StatusNotFound {
		t.Fatal("delete twice")
	}
	if len(list(ts.client)) != 1 {
		t.Fatal("schedule still listed")
	}
}

func TestCovAgentProjectsAndChatPatch(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{})
	if res, _ := ts.request("POST", "/api/v1/agent/projects", map[string]any{"name": "  "}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("project without name: %d", res.StatusCode)
	}
	res, body := ts.request("POST", "/api/v1/agent/projects", map[string]any{"name": "Home Assistant", "instructions": "HA ist die VM.", "icon": "🏠"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("project: %d %s", res.StatusCode, body)
	}
	var project agent.Project
	json.Unmarshal(body, &project)
	if project.ID == "" || project.Created.IsZero() {
		t.Fatalf("project: %+v", project)
	}
	res, body = ts.request("PUT", "/api/v1/agent/projects/"+project.ID, map[string]any{"name": "HA", "instructions": "neu"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("set: %d %s", res.StatusCode, body)
	}
	_, body = ts.request("GET", "/api/v1/agent/projects", nil)
	var projects struct{ Projects []agent.Project }
	json.Unmarshal(body, &projects)
	if len(projects.Projects) != 1 || projects.Projects[0].Name != "HA" || projects.Projects[0].ID != project.ID || !projects.Projects[0].Created.Equal(project.Created) {
		t.Fatalf("projects: %s", body)
	}

	// A chat, then pin, rename, move into the project.
	res, body = ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": "Hallo"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("chat: %d %s", res.StatusCode, body)
	}
	var chat agent.Chat
	json.Unmarshal(body, &chat)
	waitChat(t, ts, ts.client, chat.ID)
	patch := func(client *http.Client, body map[string]any) (int, agent.ChatInfo) {
		res, data := ts.do(client, "PATCH", "/api/v1/agent/chats/"+chat.ID, body)
		var info agent.ChatInfo
		json.Unmarshal(data, &info)
		return res.StatusCode, info
	}
	if code, info := patch(ts.client, map[string]any{"pinned": true, "title": "  Mein Chat  ", "project": project.ID}); code != http.StatusOK || !info.Pinned || info.Title != "Mein Chat" || info.Project != project.ID {
		t.Fatalf("patch: %d %+v", code, info)
	}
	for _, bad := range []map[string]any{{"title": "   "}, {"title": string(make([]byte, 201))}} {
		if code, _ := patch(ts.client, bad); code != http.StatusBadRequest {
			t.Errorf("title %v: %d", bad, code)
		}
	}
	if code, _ := patch(ts.client, map[string]any{"project": agent.NewID()}); code != http.StatusNotFound {
		t.Fatalf("unknown project: %d", code)
	}
	stored, _ := ts.server.agentStore().Chat("alex", chat.ID)
	if stored.Title != "Mein Chat" || !stored.Pinned || stored.Project != project.ID {
		t.Fatalf("stored: %+v", stored.Info())
	}
	// Only the changed fields change.
	if code, info := patch(ts.client, map[string]any{"pinned": false}); code != http.StatusOK || info.Pinned || info.Title != "Mein Chat" || info.Project != project.ID {
		t.Fatalf("unpin: %d %+v", code, info)
	}

	// Another account: none of it.
	guest := covGuest(t, ts)
	if code, _ := patch(guest, map[string]any{"title": "gehackt"}); code != http.StatusNotFound {
		t.Fatalf("guest patches the owner's chat: %d", code)
	}
	if res, _ := ts.do(guest, "PUT", "/api/v1/agent/projects/"+project.ID, map[string]any{"name": "gehackt"}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("guest changes the owner's project: %d", res.StatusCode)
	}
	if res, _ := ts.do(guest, "DELETE", "/api/v1/agent/projects/"+project.ID, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("guest deletes the owner's project: %d", res.StatusCode)
	}
	_, body = ts.do(guest, "GET", "/api/v1/agent/projects", nil)
	var guestProjects struct{ Projects []agent.Project }
	json.Unmarshal(body, &guestProjects)
	if len(guestProjects.Projects) != 0 {
		t.Fatalf("guest sees %s", body)
	}
	// The guest cannot move its own chat into the owner's project.
	res, body = ts.do(guest, "POST", "/api/v1/agent/chats", map[string]any{"message": "Hallo"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("guest chat: %d %s", res.StatusCode, body)
	}
	var guestChat agent.Chat
	json.Unmarshal(body, &guestChat)
	waitChat(t, ts, guest, guestChat.ID)
	if res, _ := ts.do(guest, "PATCH", "/api/v1/agent/chats/"+guestChat.ID, map[string]any{"project": project.ID}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("guest moves into the owner's project: %d", res.StatusCode)
	}

	// Deleting the project keeps the chat, without the project.
	if res, _ := ts.request("DELETE", "/api/v1/agent/projects/"+project.ID, nil); res.StatusCode != http.StatusNoContent {
		t.Fatal("delete project")
	}
	if res, _ := ts.request("DELETE", "/api/v1/agent/projects/"+project.ID, nil); res.StatusCode != http.StatusNotFound {
		t.Fatal("delete project twice")
	}
	stored, err := ts.server.agentStore().Chat("alex", chat.ID)
	if err != nil || stored.Project != "" || stored.Title != "Mein Chat" {
		t.Fatalf("chat after the project: %v %+v", err, stored)
	}
}

// Stop ends a run; while it runs the chat cannot be changed.
func TestCovAgentStopAndBusyPatch(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	dir := t.TempDir()
	ts.server.agentOpen = func() (*os.Root, error) { return os.OpenRoot(dir) }
	settings := map[string]any{"enabled": true, "ollama": covBlockingOllama(t), "model": "gpt-oss:20b", "think": "high", "policy": agent.Policy{}}
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	res, body := ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": "Denk lange nach"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("chat: %d %s", res.StatusCode, body)
	}
	var chat agent.Chat
	json.Unmarshal(body, &chat)
	deadline := time.Now().Add(5 * time.Second)
	for !ts.server.agentRunning("alex", chat.ID) {
		if time.Now().After(deadline) {
			t.Fatal("never ran")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if res, _ := ts.request("PATCH", "/api/v1/agent/chats/"+chat.ID, map[string]any{"title": "neu"}); res.StatusCode != http.StatusConflict {
		t.Fatalf("patch while running: %d", res.StatusCode)
	}
	if res, _ := ts.request("DELETE", "/api/v1/agent/chats/"+chat.ID, nil); res.StatusCode != http.StatusConflict {
		t.Fatalf("delete while running: %d", res.StatusCode)
	}
	// Another account cannot stop it.
	guest := covGuest(t, ts)
	res, body = ts.do(guest, "POST", "/api/v1/agent/chats/"+chat.ID+"/stop", nil)
	if res.StatusCode != http.StatusOK || string(body) == "" || !ts.server.agentRunning("alex", chat.ID) {
		t.Fatalf("guest stop: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("POST", "/api/v1/agent/chats/"+chat.ID+"/stop", nil)
	var out struct{ Running bool }
	json.Unmarshal(body, &out)
	if res.StatusCode != http.StatusAccepted || !out.Running {
		t.Fatalf("stop: %d %s", res.StatusCode, body)
	}
	done := waitChat(t, ts, ts.client, chat.ID)
	stopped := false
	for _, e := range done.Entries {
		stopped = stopped || (e.Role == "note" && e.Note == "stopped")
	}
	if done.State == agent.StateRunning || !stopped {
		t.Fatalf("after stop: %s %+v", done.State, done.Entries)
	}
	res, body = ts.request("POST", "/api/v1/agent/chats/"+chat.ID+"/stop", nil)
	json.Unmarshal(body, &out)
	if res.StatusCode != http.StatusOK || out.Running {
		t.Fatalf("stop when idle: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("PATCH", "/api/v1/agent/chats/"+chat.ID, map[string]any{"title": "neu"}); res.StatusCode != http.StatusOK {
		t.Fatalf("patch after stop: %d", res.StatusCode)
	}
}
