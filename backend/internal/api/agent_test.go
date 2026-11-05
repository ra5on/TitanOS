package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// agentOllama is a fake Ollama: /api/chat answers with the replies in
// order (each a message object), and it keeps what it got.
type agentOllama struct {
	mu      sync.Mutex
	replies []string
	got     []map[string]any
}

func (f *agentOllama) start(t *testing.T) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.Write([]byte(`{"version":"0.40.14"}`))
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"gpt-oss:20b","size":1,"details":{"context_length":131072},"capabilities":["completion","tools","thinking"]}]}`))
		case "/api/show":
			w.Write([]byte(`{"capabilities":["completion","tools","thinking"],"thinking":{"values":["low","medium","high"],"default":"medium"}}`))
		case "/api/ps":
			w.Write([]byte(`{"models":[]}`))
		case "/api/pull":
			w.Write([]byte(`{"status":"pulling manifest"}` + "\n" + `{"status":"downloading","completed":5,"total":10}` + "\n" + `{"status":"success"}` + "\n"))
		case "/api/chat":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.got = append(f.got, body)
			reply := `{"role":"assistant","content":"Fertig."}`
			if len(f.replies) > 0 {
				reply, f.replies = f.replies[0], f.replies[1:]
			}
			f.mu.Unlock()
			w.Write([]byte(`{"message":` + reply + `,"done":false}` + "\n" + `{"message":{"role":"assistant","content":""},"done":true}` + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func toolCall(name string, args any) string {
	data, _ := json.Marshal(args)
	return `{"role":"assistant","content":"","tool_calls":[{"function":{"name":"` + name + `","arguments":` + string(data) + `}}]}`
}

func agentServer(t *testing.T, fake *agentOllama, policy agent.Policy) *testServer {
	t.Helper()
	ts := newTestServer(t)
	ts.setup()
	dir := t.TempDir()
	ts.server.agentOpen = func() (*os.Root, error) { return os.OpenRoot(dir) }
	settings := map[string]any{"enabled": true, "ollama": fake.start(t), "model": "gpt-oss:20b", "think": "high", "policy": policy}
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	return ts
}

// waitChat polls a chat until it no longer runs.
func waitChat(t *testing.T, ts *testServer, client *http.Client, id string) agent.Chat {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		res, body := ts.do(client, "GET", "/api/v1/agent/chats/"+id, nil)
		var chat agent.Chat
		json.Unmarshal(body, &chat)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("chat: %d %s", res.StatusCode, body)
		}
		if chat.State != agent.StateRunning && !ts.server.agentRunning("alex", id) && !ts.server.agentRunning("gast", id) {
			return chat
		}
		if time.Now().After(deadline) {
			t.Fatalf("chat still running: %+v", chat)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAgentSettingsAndAccess(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{})
	for _, bad := range []map[string]any{
		{"enabled": true},
		{"ollama": "ftp://x"},
		{"policy": map[string]any{"preset": "everything"}},
		{"policy": map[string]any{"areas": map[string]string{"files": "sometimes"}}},
		{"policy": map[string]any{"areas": map[string]string{"nowhere": "allow"}}},
		{"users": []string{"nobody"}},
		{"maxSteps": 1000},
		{"homeassistant": map[string]any{"notify": []string{"mail.me"}}},
	} {
		if res, _ := ts.request("PUT", "/api/v1/agent/settings", bad); res.StatusCode != http.StatusBadRequest {
			t.Errorf("accepted %v: %d", bad, res.StatusCode)
		}
	}
	res, body := ts.request("GET", "/api/v1/agent/status", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"reachable":true`) || !strings.Contains(string(body), `"levels":["low","medium","high"]`) {
		t.Fatalf("status: %d %s", res.StatusCode, body)
	}
	// A user without "KI-Agent erlaubt" sees nothing of the agent.
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, route := range []struct{ method, path string }{{"GET", "/agent/chats"}, {"POST", "/agent/chats"}, {"GET", "/agent/memory"}, {"GET", "/agent/projects"}} {
		if res, _ := ts.do(guest, route.method, "/api/v1"+route.path, map[string]any{"message": "x"}); res.StatusCode != http.StatusForbidden {
			t.Errorf("guest %s: %d", route.path, res.StatusCode)
		}
	}
	for _, path := range []string{"/agent/settings", "/agent/tools"} {
		if res, _ := ts.do(guest, "GET", "/api/v1"+path, nil); res.StatusCode != http.StatusForbidden {
			t.Errorf("guest %s: %d", path, res.StatusCode)
		}
	}
	// Allowed: its own chats, never the owner's.
	_, body = ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	settings["users"] = []string{"gast"}
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != 200 {
		t.Fatalf("allow: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": "Hallo"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("owner chat: %d %s", res.StatusCode, body)
	}
	var chat agent.Chat
	json.Unmarshal(body, &chat)
	waitChat(t, ts, ts.client, chat.ID)
	if res, _ := ts.do(guest, "GET", "/api/v1/agent/chats/"+chat.ID, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("guest reads the owner's chat: %d", res.StatusCode)
	}
	if res, _ := ts.do(guest, "GET", "/api/v1/agent/chats", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("guest list: %d", res.StatusCode)
	}
	// The user's toolbox has no administration tools.
	box := ts.server.newAgentToolbox(mustUser(t, ts, "gast"), ts.server.agentSettings())
	for _, name := range []string{"system_status", "docker_list", "vm_action", "power_action", "account_action"} {
		if _, ok := box.tools[name]; ok {
			t.Errorf("user agent has %s", name)
		}
	}
	// Removing the old assistant leaves the agent alone.
	ts.server.removeRetiredAssistant()
	if _, err := ts.store.Read(agentSettingsFile); err != nil {
		t.Fatalf("agent settings removed: %v", err)
	}
	if _, err := ts.server.agentStore().Chat("alex", chat.ID); err != nil {
		t.Fatalf("agent chat removed: %v", err)
	}
}

func TestAgentRunsToolsAsksAndLogs(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetStandard, Folders: []agent.FolderRule{{Root: "share/Docs", Path: "Privat", Mode: agent.ModeDeny}}})
	log := activity.New(ts.store)
	ts.server.opts.Activity = log
	dir := ts.withShare()
	os.WriteFile(filepath.Join(dir, "liste.md"), []byte("alt"), 0o644)
	os.MkdirAll(filepath.Join(dir, "Privat"), 0o755)
	os.WriteFile(filepath.Join(dir, "Privat", "geheim.txt"), []byte("x"), 0o644)
	fake.replies = []string{
		toolCall("files_list", map[string]any{"root": "share/Docs"}),
		toolCall("file_read", map[string]any{"root": "share/Docs", "path": "Privat/geheim.txt"}),
		toolCall("files_list", map[string]any{"root": "volume/tank", "path": "Docs"}),
		toolCall("file_read", map[string]any{"root": "volume/tank", "path": "Docs/Privat/geheim.txt"}),
		toolCall("file_write", map[string]any{"root": "share/Docs", "path": "Übersicht.docx", "content": "# Apps\n\nKeine."}),
		toolCall("file_write", map[string]any{"root": "share/Docs", "path": "liste.md", "content": "neu", "replace": true}),
		`{"role":"assistant","content":"Ersetzt."}`,
	}
	res, body := ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": "Mach eine Übersicht und ersetze die Liste"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %s", res.StatusCode, body)
	}
	var started agent.Chat
	json.Unmarshal(body, &started)
	chat := waitChat(t, ts, ts.client, started.ID)
	// Thinking level of the model, never set by the model.
	if fake.got[0]["think"] != "high" {
		t.Fatalf("think: %v", fake.got[0]["think"])
	}
	// No tool offered to the model has confirm.
	for _, tool := range fake.got[0]["tools"].([]any) {
		params, _ := json.Marshal(tool.(map[string]any)["function"].(map[string]any)["parameters"])
		if strings.Contains(string(params), `"confirm"`) {
			t.Fatalf("tool with confirm: %s", params)
		}
	}
	steps := map[string]agent.Entry{}
	for _, e := range chat.Entries {
		if e.Role == "tool" {
			steps[e.Tool+":"+string(e.Args)] = e
		}
	}
	var listed, private, created string
	for k, e := range steps {
		switch {
		case strings.HasPrefix(k, "files_list"):
			listed = e.Result
		case strings.HasPrefix(k, "file_read"):
			private = e.Result
		case strings.HasPrefix(k, "file_write") && strings.Contains(k, "docx"):
			created = e.Result
		}
	}
	if !strings.Contains(listed, "liste.md") || strings.Contains(listed, "Privat") {
		t.Fatalf("locked folder listed: %s", listed)
	}
	if !strings.Contains(private, "gesperrt") {
		t.Fatalf("locked file read: %s", private)
	}
	// The same folder through the volume root is locked as well.
	through := 0
	for k := range steps {
		if strings.Contains(k, "volume/tank") {
			through++
		}
	}
	if through != 2 {
		t.Fatalf("volume steps: %d", through)
	}
	for k, e := range steps {
		if strings.Contains(k, "volume/tank") {
			if strings.HasPrefix(k, "files_list") && (strings.Contains(e.Result, "Privat") || !strings.Contains(e.Result, "liste.md")) {
				t.Fatalf("locked folder listed through the volume: %s", e.Result)
			}
			if strings.HasPrefix(k, "file_read") && !strings.Contains(e.Result, "gesperrt") {
				t.Fatalf("locked file read through the volume: %s", e.Result)
			}
		}
	}
	if !strings.HasPrefix(created, "{") {
		t.Fatalf("new document: %s", created)
	}
	if _, err := os.Stat(filepath.Join(dir, "Übersicht.docx")); err != nil {
		t.Fatal("document not written")
	}
	// Replacing waits for the person; nothing was replaced yet.
	if chat.State != agent.StateWaiting || chat.Pending == nil || chat.Pending.Tool != "file_write" {
		t.Fatalf("not waiting: %s %+v", chat.State, chat.Pending)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "liste.md")); string(data) != "alt" {
		t.Fatal("replaced before approval")
	}
	// Approval works only in the web session of the owner of the chat.
	if res, _ := ts.request("POST", "/api/v1/agent/chats/"+chat.ID+"/decide", map[string]any{"id": "x", "approve": true}); res.StatusCode != http.StatusConflict {
		t.Fatalf("wrong id: %d", res.StatusCode)
	}
	if res, body := ts.request("POST", "/api/v1/agent/chats/"+chat.ID+"/decide", map[string]any{"id": chat.Pending.ID, "approve": true}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("approve: %d %s", res.StatusCode, body)
	}
	chat = waitChat(t, ts, ts.client, chat.ID)
	if data, _ := os.ReadFile(filepath.Join(dir, "liste.md")); string(data) != "neu" {
		t.Fatalf("not replaced after approval: %q", data)
	}
	if chat.State != agent.StateIdle || chat.Entries[len(chat.Entries)-1].Content != "Ersetzt." {
		t.Fatalf("end: %s %+v", chat.State, chat.Entries[len(chat.Entries)-1])
	}
	// The writes show up as "KI-Agent" in the activity log.
	var agentWrites int
	for _, e := range activityEntries(t, log) {
		if e.Via == activity.ViaAgent && strings.Contains(e.Action, "write-document") {
			agentWrites++
		}
	}
	if agentWrites != 2 {
		t.Fatalf("agent writes in the log: %d", agentWrites)
	}
}

func TestAgentDeniedToolsAreNotOffered(t *testing.T) {
	fake := &agentOllama{replies: []string{toolCall("file_write", map[string]any{"root": "share/Docs", "path": "x.txt", "content": "x"})}}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetRead})
	ts.withShare()
	res, body := ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": "schreib"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %s", res.StatusCode, body)
	}
	var chat agent.Chat
	json.Unmarshal(body, &chat)
	chat = waitChat(t, ts, ts.client, chat.ID)
	for _, tool := range fake.got[0]["tools"].([]any) {
		if name := tool.(map[string]any)["function"].(map[string]any)["name"]; name == "file_write" || name == "files_delete" {
			t.Fatalf("read-only agent offered %s", name)
		}
	}
	for _, e := range chat.Entries {
		if e.Role == "tool" && e.Tool == "file_write" && !strings.Contains(e.Result, "not allowed") {
			t.Fatalf("denied tool ran: %+v", e)
		}
	}
}

func TestAgentScheduleDue(t *testing.T) {
	at := func(s string) time.Time { v, _ := time.Parse("2006-01-02 15:04", s); return v }
	daily := agent.Schedule{Enabled: true, Every: "daily", At: "08:00"}
	if !agentDue(daily, at("2026-11-09 08:03")) || agentDue(daily, at("2026-11-09 07:59")) || agentDue(daily, at("2026-11-09 08:15")) {
		t.Fatal("daily window")
	}
	daily.LastRun = at("2026-11-09 08:01")
	if agentDue(daily, at("2026-11-09 08:03")) {
		t.Fatal("ran twice")
	}
	weekly := agent.Schedule{Enabled: true, Every: "weekly", At: "08:00", Weekday: 1}
	if !agentDue(weekly, at("2026-11-09 08:00")) || agentDue(weekly, at("2026-11-10 08:00")) {
		t.Fatal("weekly")
	}
	hourly := agent.Schedule{Enabled: true, Every: "hourly"}
	if !agentDue(hourly, at("2026-11-09 13:02")) || agentDue(hourly, at("2026-11-09 13:30")) {
		t.Fatal("hourly")
	}
	if agentDue(agent.Schedule{Every: "daily", At: "08:00"}, at("2026-11-09 08:00")) {
		t.Fatal("disabled ran")
	}
}

func TestAgentAssistForHomeAssistantOnly(t *testing.T) {
	fake := &agentOllama{replies: []string{`{"role":"assistant","content":"**Das Licht** ist aus.\n\n| a | b |\n|---|---|\n| x | y |"}`}}
	ts := agentServer(t, fake, agent.Policy{})
	post := func(path string, body any) (*http.Response, []byte) {
		return ts.do(&http.Client{}, "POST", path, body)
	}
	// Off: refused, even from the right address.
	if res, _ := post("/agent-ollama/api/chat", map[string]any{"messages": []any{}}); res.StatusCode != http.StatusForbidden {
		t.Fatalf("assist off: %d", res.StatusCode)
	}
	_, body := ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	settings["homeassistant"] = map[string]any{"assist": true}
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != 200 {
		t.Fatalf("assist on: %d %s", res.StatusCode, body)
	}
	// On, but Home Assistant lives elsewhere: refused.
	ha, _ := ts.home.Settings()
	ha.URL = "http://192.0.2.10:8123"
	if err := ts.home.SetSettings(ha); err != nil {
		t.Fatal(err)
	}
	if res, _ := post("/agent-ollama/api/tags", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("other address: %d", res.StatusCode)
	}
	ha.URL = "http://127.0.0.1:8123"
	ts.home.SetSettings(ha)
	res, body := ts.do(&http.Client{}, "GET", "/agent-ollama/api/tags", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), "titanos-agent") {
		t.Fatalf("tags: %d %s", res.StatusCode, body)
	}
	stream := false
	res, body = post("/agent-ollama/api/chat", map[string]any{"model": "titanos-agent:latest", "stream": stream, "messages": []map[string]string{{"role": "system", "content": "x"}, {"role": "user", "content": "Ist das Licht im Flur an?"}}})
	var reply struct {
		Message struct{ Content string } `json:"message"`
		Done    bool                     `json:"done"`
	}
	json.Unmarshal(body, &reply)
	if res.StatusCode != 200 || !reply.Done || reply.Message.Content != "Das Licht ist aus. a, b. x, y." {
		t.Fatalf("chat: %d %s", res.StatusCode, body)
	}
	list, _ := ts.server.agentStore().Chats("alex")
	if len(list) != 1 || list[0].Origin != "homeassistant" {
		t.Fatalf("assist chat: %+v", list)
	}
}

// The stream of a running chat goes through the whole handler chain of
// /api/v1 (activity log included) and delivers pieces before the end.
func TestAgentEventStream(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/show":
			w.Write([]byte(`{"capabilities":["tools"]}`))
		case "/api/chat":
			w.Write([]byte(`{"message":{"role":"assistant","content":"Hal"},"done":false}` + "\n"))
			w.(http.Flusher).Flush()
			<-release
			w.Write([]byte(`{"message":{"role":"assistant","content":"lo"},"done":false}` + "\n" + `{"message":{"role":"assistant","content":""},"done":true}` + "\n"))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	ts := newTestServer(t)
	ts.setup()
	dir := t.TempDir()
	ts.server.agentOpen = func() (*os.Root, error) { return os.OpenRoot(dir) }
	ts.server.opts.Activity = activity.New(ts.store)
	if res, body := ts.request("PUT", "/api/v1/agent/settings", map[string]any{"enabled": true, "ollama": srv.URL, "model": "m", "policy": map[string]any{}}); res.StatusCode != 200 {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	res, body := ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": "Hallo"})
	var chat agent.Chat
	json.Unmarshal(body, &chat)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %s", res.StatusCode, body)
	}
	req, _ := http.NewRequest("GET", ts.url+"/api/v1/agent/chats/"+chat.ID+"/events", nil)
	stream, err := ts.client.Do(req)
	if err != nil || stream.StatusCode != 200 || !strings.HasPrefix(stream.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream: %v %v", err, stream)
	}
	defer stream.Body.Close()
	buf := make([]byte, 64<<10)
	var got strings.Builder
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(got.String(), `"text":"Hal"`) {
		if time.Now().After(deadline) {
			t.Fatalf("no streamed piece before the end: %s", got.String())
		}
		n, err := stream.Body.Read(buf)
		got.Write(buf[:n])
		if err != nil {
			t.Fatalf("stream ended: %v %s", err, got.String())
		}
	}
	close(release)
	for !strings.Contains(got.String(), "event: end") {
		n, err := stream.Body.Read(buf)
		got.Write(buf[:n])
		if err != nil {
			break
		}
	}
	for _, want := range []string{"event: content", "event: entry", `"content":"Hallo"`, "event: end"} {
		if !strings.Contains(got.String(), want) {
			t.Errorf("stream lacks %s: %s", want, got.String())
		}
	}
}

func TestAgentHomeAssistantRules(t *testing.T) {
	box := &agentToolbox{settings: agentSettings{Policy: agent.Policy{Preset: agent.PresetAll}}}
	call := func(name string, args any) string {
		data, _ := json.Marshal(args)
		return box.haMode(name, data, agent.ModeAllow)
	}
	// Locks ask, also inside ha_call's list and deep inside an automation.
	if got := call("ha_call", map[string]any{"domain": "lock", "service": "unlock", "entities": []string{"lock.haustuer"}}); got != agent.ModeAsk {
		t.Fatalf("ha_call lock: %s", got)
	}
	auto := map[string]any{"id": "x", "config": map[string]any{"alias": "Tür", "actions": []any{map[string]any{"action": "lock.unlock", "target": map[string]any{"entity_id": "lock.haustuer"}}}}}
	if got := call("ha_automation_save", auto); got != agent.ModeAsk {
		t.Fatalf("automation with a lock: %s", got)
	}
	if got := call("ha_automation_save", map[string]any{"id": "x", "config": map[string]any{"alias": "?"}}); got != agent.ModeAsk {
		t.Fatalf("automation without devices: %s", got)
	}
	if got := call("ha_call", map[string]any{"domain": "light", "service": "turn_on", "entities": []string{"light.flur"}}); got != agent.ModeAllow {
		t.Fatalf("light: %s", got)
	}
	box.settings.HA.Entities = map[string]string{"entity:light.flur": agent.ModeDeny, "domain:lock": agent.ModeAllow}
	if got := call("ha_call", map[string]any{"domain": "light", "service": "turn_on", "entities": []string{"light.flur"}}); got != agent.ModeDeny {
		t.Fatalf("denied light: %s", got)
	}
	if got := call("ha_call", map[string]any{"domain": "lock", "service": "lock", "entities": []string{"lock.haustuer"}}); got != agent.ModeAllow {
		t.Fatalf("lock allowed by the person: %s", got)
	}
	// Rules by room: "Küche" matches area:kueche.
	box.settings.HA.Entities = map[string]string{"area:kueche": agent.ModeAsk}
	box.areas = map[string]string{"light.herd": "Küche"}
	if got := call("ha_call", map[string]any{"domain": "light", "service": "turn_on", "entities": []string{"light.herd"}}); got != agent.ModeAsk {
		t.Fatalf("area rule: %s", got)
	}
	if got := call("ha_call", map[string]any{"domain": "light", "service": "turn_on", "entities": []string{"light.flur"}}); got != agent.ModeAllow {
		t.Fatalf("other room: %s", got)
	}
}

func TestAgentKeepsItsVolume(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if v, ok := ts.server.agentVolume(nil); ok || v != "" {
		t.Fatalf("no volume: %q %v", v, ok)
	}
	if v, ok := ts.server.agentVolume([]string{"tank", "fast"}); !ok || v != "fast" {
		t.Fatalf("first: %q %v", v, ok)
	}
	// A new volume that sorts first does not move the chats.
	if v, ok := ts.server.agentVolume([]string{"archiv", "fast", "tank"}); !ok || v != "fast" {
		t.Fatalf("kept: %q %v", v, ok)
	}
	if v, ok := ts.server.agentVolume([]string{"archiv"}); ok || v != "fast" {
		t.Fatalf("missing volume: %q %v", v, ok)
	}
	var stored agentSettings
	if err := ts.store.ReadJSON(agentSettingsFile, &stored); err != nil || stored.Volume != "fast" {
		t.Fatalf("stored: %+v %v", stored, err)
	}
}

func TestAgentButtonInHomeAssistant(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{})
	store := ts.server.agentStore()
	store.SaveSchedules("alex", []agent.Schedule{
		{ID: "s1aaaaaaaaaa0001", Name: "Gesundheitscheck", Task: "Gesundheitscheck", Every: "weekly", At: "08:00", Enabled: true, Button: true},
		{ID: "s1aaaaaaaaaa0002", Name: "Ohne Knopf", Task: "x", Every: "daily", At: "08:00", Enabled: true},
	})
	buttons := ts.server.agentButtons()
	if len(buttons) != 1 || buttons[0].Key != "alex/s1aaaaaaaaaa0001" {
		t.Fatalf("buttons: %+v", buttons)
	}
	if err := ts.server.agentButton("alex/s1aaaaaaaaaa0002", "run"); err == nil {
		t.Fatal("task without a button ran")
	}
	if err := ts.server.agentButton("alex/s1aaaaaaaaaa0001", "run"); err != nil {
		t.Fatal(err)
	}
	list, _ := store.Chats("alex")
	if len(list) != 1 || list[0].Origin != "schedule" {
		t.Fatalf("chats: %+v", list)
	}
	waitChat(t, ts, ts.client, list[0].ID)
}

func TestAgentNASMapAndInstructions(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{})
	dir := ts.withShare()
	os.MkdirAll(filepath.Join(dir, "Scans"), 0o755)
	os.MkdirAll(filepath.Join(dir, "Steuer"), 0o755)
	res, body := ts.request("POST", "/api/v1/agent/map/refresh", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"Scans"`) || !strings.Contains(string(body), `"share/Docs"`) {
		t.Fatalf("map: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/agent/descriptions", map[string]string{"key": "folder:share/Docs/Scans", "text": "Scans vom Drucker"}); res.StatusCode != 200 {
		t.Fatalf("description: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("PUT", "/api/v1/agent/descriptions", map[string]string{"key": "nonsense", "text": "x"}); res.StatusCode != 400 {
		t.Fatal("bad key accepted")
	}
	// Instruction of the owner and of the account go into the prompt, with the map.
	_, body = ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	settings["instruction"] = "Das ist unser Familien-NAS."
	ts.request("PUT", "/api/v1/agent/settings", settings)
	ts.request("PUT", "/api/v1/agent/profile", map[string]string{"instruction": "Antworte sehr kurz."})
	_, body = ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": "Wo liegen Scans?"})
	var chat agent.Chat
	json.Unmarshal(body, &chat)
	waitChat(t, ts, ts.client, chat.ID)
	system := fake.got[0]["messages"].([]any)[0].(map[string]any)["content"].(string)
	for _, want := range []string{"Familien-NAS", "Antworte sehr kurz", "Share Docs (root share/Docs)", "Scans – Scans vom Drucker"} {
		if !strings.Contains(system, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	// A user without access to Docs sees no share in the map.
	ts.users.Create("gast", testPassword, auth.RoleUser)
	settings["users"] = []string{"gast"}
	ts.request("PUT", "/api/v1/agent/settings", settings)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	_, body = ts.do(guest, "GET", "/api/v1/agent/map", nil)
	if strings.Contains(string(body), "Docs") || strings.Contains(string(body), "Scans vom Drucker") {
		t.Fatalf("user sees other shares: %s", body)
	}
	if res, _ := ts.do(guest, "POST", "/api/v1/agent/map/refresh", nil); res.StatusCode != http.StatusForbidden {
		t.Fatal("user rebuilt the map")
	}
}

func TestAgentUndoAttachExport(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetAll})
	dir := ts.withShare()
	os.WriteFile(filepath.Join(dir, "liste.md"), []byte("alt"), 0o644)
	os.WriteFile(filepath.Join(dir, "weg.txt"), []byte("bleib"), 0o644)
	// The share needs its recycle bin for deleted files to come back.
	_, shareBody := ts.request("GET", "/api/v1/shares", nil)
	var shareList []map[string]any
	json.Unmarshal(shareBody, &shareList)
	for _, sh := range shareList {
		if sh["name"] == "Docs" {
			sh["recycle"] = map[string]any{"enabled": true, "days": 30}
			if res, body := ts.request("PUT", "/api/v1/shares/Docs", sh); res.StatusCode >= 300 {
				t.Fatalf("recycle bin: %d %s", res.StatusCode, body)
			}
		}
	}
	ts.request("POST", "/api/v1/agent/projects", map[string]any{"name": "Doku"})
	_, body := ts.request("GET", "/api/v1/agent/projects", nil)
	var projects struct{ Projects []agent.Project }
	json.Unmarshal(body, &projects)
	fake.replies = []string{
		toolCall("file_write", map[string]any{"root": "share/Docs", "path": "liste.md", "content": "neu", "replace": true}),
		toolCall("files_delete", map[string]any{"root": "share/Docs", "paths": []string{"weg.txt"}}),
		`{"role":"assistant","content":"Erledigt."}`,
	}
	res, body := ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": "Ersetze und lösche", "project": projects.Projects[0].ID})
	var chat agent.Chat
	json.Unmarshal(body, &chat)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %s", res.StatusCode, body)
	}
	chat = waitChat(t, ts, ts.client, chat.ID)
	steps := map[string]string{}
	for _, e := range chat.Entries {
		if e.Role == "tool" && e.OK {
			steps[e.Tool] = e.ID
		}
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "liste.md")); string(data) != "neu" {
		for _, e := range chat.Entries {
			t.Logf("%s %s %s", e.Role, e.Tool, e.Result)
		}
		t.Fatalf("not replaced: %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "weg.txt")); err == nil {
		t.Fatal("not deleted")
	}
	// Files of the project.
	_, body = ts.request("GET", "/api/v1/agent/projects/"+projects.Projects[0].ID+"/files", nil)
	if !strings.Contains(string(body), "liste.md") {
		t.Fatalf("project files: %s", body)
	}
	for _, tool := range []string{"file_write", "files_delete"} {
		if res, body := ts.request("POST", "/api/v1/agent/chats/"+chat.ID+"/undo", map[string]string{"step": steps[tool]}); res.StatusCode != 200 {
			t.Fatalf("undo %s: %d %s", tool, res.StatusCode, body)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "liste.md")); string(data) != "alt" {
		t.Fatalf("replace not taken back: %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "weg.txt")); string(data) != "bleib" {
		t.Fatalf("delete not taken back: %q", data)
	}
	// Attachments: up, read by the agent, away with the chat.
	req, _ := http.NewRequest("POST", ts.url+"/api/v1/agent/chats/"+chat.ID+"/attachments?name=notiz.md", strings.NewReader("# Notiz\nGeheimzahl ist keine."))
	req.Header.Set("Origin", ts.url)
	up, err := ts.client.Do(req)
	if err != nil || up.StatusCode != http.StatusCreated {
		t.Fatalf("upload: %v %v", err, up)
	}
	for _, bad := range []string{"../x", ".hidden"} {
		req, _ := http.NewRequest("POST", ts.url+"/api/v1/agent/chats/"+chat.ID+"/attachments?name="+url.QueryEscape(bad), strings.NewReader("x"))
		req.Header.Set("Origin", ts.url)
		if r, _ := ts.client.Do(req); r.StatusCode == http.StatusCreated && bad == ".hidden" {
			t.Fatalf("name %q accepted", bad)
		}
	}
	box := ts.server.newAgentToolbox(mustUser(t, ts, "alex"), ts.server.agentSettings())
	out, err := box.tools["attachment_read"].own(context.Background(), &chat, json.RawMessage(`{"name":"notiz.md"}`))
	if err != nil || !strings.Contains(out.Text, "Notiz") {
		t.Fatalf("attachment read: %v %+v", err, out)
	}
	// Export as Markdown and PDF.
	res, body = ts.request("GET", "/api/v1/agent/chats/"+chat.ID+"/export", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), "Ersetze und lösche") || !strings.Contains(string(body), "Erledigt.") {
		t.Fatalf("export: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/agent/chats/"+chat.ID+"/export?format=pdf", nil); res.StatusCode != 200 || !strings.HasPrefix(string(body), "%PDF") {
		t.Fatalf("pdf: %d", res.StatusCode)
	}
	if res, _ := ts.request("DELETE", "/api/v1/agent/chats/"+chat.ID, nil); res.StatusCode != http.StatusNoContent {
		t.Fatal("delete chat")
	}
	if _, err := ts.server.agentStore().Attachment("alex", chat.ID, "notiz.md"); err == nil {
		t.Fatal("attachment outlived its chat")
	}
}

func TestAgentPullAndPreload(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{})
	if res, body := ts.request("POST", "/api/v1/agent/models/pull", map[string]string{"model": "qwen3:8b"}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("pull: %d %s", res.StatusCode, body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, body := ts.request("GET", "/api/v1/agent/models/pull", nil)
		if strings.Contains(string(body), `"running":false`) {
			if !strings.Contains(string(body), `"status":"success"`) {
				t.Fatalf("pull state: %s", body)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pull did not end")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if res, _ := ts.request("POST", "/api/v1/agent/models/pull", map[string]string{"model": "bad name"}); res.StatusCode != http.StatusBadRequest {
		t.Fatal("bad model name accepted")
	}
	if res, _ := ts.request("POST", "/api/v1/agent/preload", map[string]string{}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("preload: %d", res.StatusCode)
	}
}
