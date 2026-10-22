package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// fakeChatOllama streams one reply per request to /api/chat: a function
// gets the request and returns the message chunks.
type fakeChatOllama struct {
	mu   sync.Mutex
	seen []map[string]any
	next func(n int, body map[string]any) []string
}

func (f *fakeChatOllama) start(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"qwen3:8b"},{"name":"tiny:1b"}]}`))
			return
		case "/api/show":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["model"] == "tiny:1b" {
				w.Write([]byte(`{"capabilities":["completion"]}`))
				return
			}
			w.Write([]byte(`{"capabilities":["completion","tools"]}`))
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.seen = append(f.seen, body)
		n := len(f.seen)
		f.mu.Unlock()
		for _, chunk := range f.next(n, body) {
			w.Write([]byte(`{"message":` + chunk + `,"done":false}` + "\n"))
		}
		w.Write([]byte(`{"message":{"role":"assistant","content":""},"done":true}` + "\n"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func toolNames(body map[string]any) []string {
	var names []string
	for _, tool := range body["tools"].([]any) {
		names = append(names, tool.(map[string]any)["function"].(map[string]any)["name"].(string))
	}
	return names
}

func lastContent(body map[string]any) string {
	messages := body["messages"].([]any)
	return messages[len(messages)-1].(map[string]any)["content"].(string)
}

type chatResponse struct {
	Chat struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
			Tool    string `json:"tool"`
			Problem *struct{ Code string }
			Action  *struct {
				ID, Key, Level, Method, Path, State, Confirm, ConfirmField string
				Body                                                       map[string]any
			}
		} `json:"messages"`
	} `json:"chat"`
}

func (ts *testServer) newChat(client *http.Client) string {
	ts.t.Helper()
	res, body := ts.do(client, "POST", "/api/v1/assistant/chats", nil)
	if res.StatusCode != http.StatusCreated {
		ts.t.Fatalf("new chat: %d %s", res.StatusCode, body)
	}
	var chat struct{ ID string }
	json.Unmarshal(body, &chat)
	return chat.ID
}

func (ts *testServer) chat(client *http.Client, id string) chatResponse {
	ts.t.Helper()
	res, body := ts.do(client, "GET", "/api/v1/assistant/chats/"+id, nil)
	if res.StatusCode != 200 {
		ts.t.Fatalf("chat: %d %s", res.StatusCode, body)
	}
	var chat chatResponse
	json.Unmarshal(body, &chat)
	return chat
}

func TestAssistantChat(t *testing.T) {
	fake := &fakeChatOllama{}
	fake.next = func(n int, body map[string]any) []string {
		switch n {
		case 1:
			return []string{`{"role":"assistant","content":"","tool_calls":[{"function":{"name":"system_status","arguments":{}}}]}`}
		case 2:
			var status map[string]any
			json.Unmarshal([]byte(lastContent(body)), &status)
			return []string{`{"role":"assistant","content":"Version "}`, `{"role":"assistant","content":"` + status["version"].(string) + `"}`}
		case 3:
			return []string{`{"role":"assistant","content":"Ich starte neu.","tool_calls":[{"function":{"name":"power_action","arguments":{"action":"reboot","reason":"Update"}}}]}`}
		default:
			return []string{`{"role":"assistant","content":"` + strings.ReplaceAll(lastContent(body), `"`, `'`) + `"}`}
		}
	}
	ollama := fake.start(t)

	ts := newTestServer(t)
	ts.setup()
	if res, body := ts.request("GET", "/api/v1/assistant/status", nil); res.StatusCode != 200 || !strings.Contains(string(body), `"available":false`) {
		t.Fatalf("status: %d %s", res.StatusCode, body)
	}
	// Off by default: no chat.
	if res, body := ts.request("POST", "/api/v1/assistant/chats", nil); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "assistant_off" {
		t.Fatalf("off: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/assistant/models", map[string]string{"ollama": ollama.URL}); res.StatusCode != 200 || !strings.Contains(string(body), `"tiny:1b"`) {
		t.Fatalf("models: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "ollama": "ftp://x", "model": "qwen3:8b"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad address: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "ollama": ollama.URL, "model": "qwen3:8b", "areas": map[string]string{"updates": "write"}}); res.StatusCode != 200 {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	// The chat lists only models that call tools.
	if res, body := ts.request("GET", "/api/v1/assistant/models", nil); res.StatusCode != 200 || strings.Contains(string(body), "tiny") || !strings.Contains(string(body), "qwen3") {
		t.Fatalf("chat models: %d %s", res.StatusCode, body)
	}

	id := ts.newChat(ts.client)
	res, body := ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]any{"text": "Welche Version?", "page": "/settings/about"})
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("message: %d %s", res.StatusCode, body)
	}
	text := string(body)
	if !strings.Contains(text, "event: delta\ndata: {\"text\":\"Version \"}") || !strings.Contains(text, "event: done") || !strings.Contains(text, `"tool":"system_status"`) {
		t.Fatalf("events: %s", text)
	}
	// Owners get the tools of administrators and the actions, no web.
	names := toolNames(fake.seen[0])
	for _, want := range []string{"system_status", "logs_search", "docs_search", "files_list", "container_action", "volume_create", "power_action"} {
		if !slices.Contains(names, want) {
			t.Fatalf("tool %s missing: %v", want, names)
		}
	}
	if slices.Contains(names, "web_search") || slices.Contains(names, "nas_info") {
		t.Fatalf("unexpected tools: %v", names)
	}
	chat := ts.chat(ts.client, id)
	if chat.Chat.Title != "Welche Version?" || len(chat.Chat.Messages) != 4 || chat.Chat.Messages[3].Content != "Version 1.2.3" {
		t.Fatalf("chat: %+v", chat)
	}

	// An action waits for the person; the browser calls the route itself.
	_, body = ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]any{"text": "Bitte neu starten"})
	if !strings.Contains(string(body), "event: action") {
		t.Fatalf("no action: %s", body)
	}
	chat = ts.chat(ts.client, id)
	card := chat.Chat.Messages[len(chat.Chat.Messages)-1].Action
	if card == nil || card.Key != "power.reboot" || card.Level != "danger" || card.Method != "POST" || card.Path != "/api/v1/system/power" || card.Body["action"] != "reboot" || card.State != "pending" {
		t.Fatalf("card: %+v", card)
	}
	select {
	case action := <-ts.power.actions:
		t.Fatalf("the assistant ran %s itself", action)
	default:
	}
	if res, body := ts.request("POST", "/api/v1/assistant/chats/"+id+"/actions/nope", map[string]string{"state": "done"}); res.StatusCode != http.StatusConflict {
		t.Fatalf("unknown action: %d %s", res.StatusCode, body)
	}
	_, body = ts.request("POST", "/api/v1/assistant/chats/"+id+"/actions/"+card.ID, map[string]string{"state": "rejected"})
	if !strings.Contains(string(body), "rejected") {
		t.Fatalf("model not told: %s", body)
	}

	// Answer again, edit a question, rename, list, search, delete.
	if res, body := ts.request("POST", "/api/v1/assistant/chats/"+id+"/regenerate", map[string]any{}); res.StatusCode != 200 || !strings.Contains(string(body), "event: done") {
		t.Fatalf("regenerate: %d %s", res.StatusCode, body)
	}
	edit := 0
	if res, body := ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]any{"text": "Neu gefragt", "edit": edit}); res.StatusCode != 200 {
		t.Fatalf("edit: %d %s", res.StatusCode, body)
	}
	if chat := ts.chat(ts.client, id); chat.Chat.Messages[0].Content != "Neu gefragt" || len(chat.Chat.Messages) != 2 {
		t.Fatalf("edited: %+v", chat.Chat.Messages)
	}
	if res, _ := ts.request("PUT", "/api/v1/assistant/chats/"+id, map[string]string{"title": "Versionen"}); res.StatusCode != http.StatusNoContent {
		t.Fatal("rename failed")
	}
	if _, body := ts.request("GET", "/api/v1/assistant/chats?search=gefragt", nil); !strings.Contains(string(body), "Versionen") || !strings.Contains(string(body), `"persistent":false`) {
		t.Fatalf("search: %s", body)
	}

	// Another administrator cannot see the chat.
	ts.users.Create("sam", testPassword, auth.RoleAdmin)
	sam := ts.newClient()
	ts.do(sam, "POST", "/api/v1/login", credentials{Name: "sam", Password: testPassword})
	if res, _ := ts.do(sam, "GET", "/api/v1/assistant/chats/"+id, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("other account read the chat: %d", res.StatusCode)
	}
	if res, _ := ts.do(sam, "DELETE", "/api/v1/assistant/chats/"+id, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("other account deleted the chat: %d", res.StatusCode)
	}

	// Ollama gone: a problem in the chat, not a broken page.
	ollama.Close()
	_, body = ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]any{"text": "Hallo?"})
	if !strings.Contains(string(body), "ollama_failed") || !strings.Contains(string(body), "event: done") {
		t.Fatalf("unreachable: %s", body)
	}
	if chat := ts.chat(ts.client, id); chat.Chat.Messages[len(chat.Chat.Messages)-1].Problem == nil {
		t.Fatal("problem not kept in the chat")
	}
	if res, _ := ts.request("DELETE", "/api/v1/assistant/chats/"+id, nil); res.StatusCode != http.StatusNoContent {
		t.Fatal("delete failed")
	}
	if res, _ := ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]any{"text": "x"}); res.StatusCode != http.StatusNotFound {
		t.Fatal("deleted chat answered")
	}
}

func TestAssistantForUsers(t *testing.T) {
	fake := &fakeChatOllama{next: func(n int, body map[string]any) []string {
		return []string{`{"role":"assistant","content":"ok"}`}
	}}
	ollama := fake.start(t)
	ts := newTestServer(t)
	ts.setup()
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "ollama": ollama.URL, "model": "qwen3:8b", "web": true})
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})

	// Users may not chat unless the settings say so.
	if res, _ := ts.do(guest, "POST", "/api/v1/assistant/chats", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("user chatted: %d", res.StatusCode)
	}
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "ollama": ollama.URL, "model": "qwen3:8b", "users": true, "web": true})
	id := ts.newChat(guest)
	if res, body := ts.do(guest, "POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]any{"text": "Hallo"}); res.StatusCode != 200 {
		t.Fatalf("user message: %d %s", res.StatusCode, body)
	}
	names := toolNames(fake.seen[0])
	for _, admin := range []string{"system_status", "logs_search", "docker_list", "container_action", "power_action", "volume_create", "account_action", "web_search"} {
		if slices.Contains(names, admin) {
			t.Fatalf("user got %s: %v", admin, names)
		}
	}
	for _, want := range []string{"nas_info", "my_shares", "files_list", "file_delete", "docs_search"} {
		if !slices.Contains(names, want) {
			t.Fatalf("user lacks %s: %v", want, names)
		}
	}
	if system := fake.seen[0]["messages"].([]any)[0].(map[string]any)["content"].(string); !strings.Contains(system, "not an administrator") {
		t.Fatalf("system prompt: %s", system)
	}
	// Web for users needs its own switch.
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "ollama": ollama.URL, "model": "qwen3:8b", "users": true, "web": true, "webUsers": true})
	ts.do(guest, "POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]any{"text": "Und jetzt?"})
	if !slices.Contains(toolNames(fake.seen[1]), "web_search") {
		t.Fatal("web search missing for users")
	}
	// The settings stay with administrators.
	for _, route := range []struct{ method, path string }{{"GET", "/assistant"}, {"PUT", "/assistant"}, {"POST", "/assistant/models"}, {"POST", "/assistant/models/pull"}, {"POST", "/assistant/models/delete"}, {"GET", "/assistant/ollama-apps"}} {
		if res, _ := ts.do(guest, route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Fatalf("user %s %s: %d", route.method, route.path, res.StatusCode)
		}
	}
	// Deleting the account takes its chats along.
	if res, _ := ts.request("DELETE", "/api/v1/users/gast", nil); res.StatusCode != http.StatusNoContent {
		t.Fatal("delete account failed")
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	again := ts.newClient()
	ts.do(again, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	if _, body := ts.do(again, "GET", "/api/v1/assistant/chats", nil); strings.Contains(string(body), id) {
		t.Fatal("chats of a deleted account came back")
	}
}

func TestAssistantPermissions(t *testing.T) {
	ts := newTestServer(t)
	routes := []struct{ method, path string }{
		{"GET", "/assistant/status"}, {"GET", "/assistant"}, {"PUT", "/assistant"}, {"POST", "/assistant/models"}, {"GET", "/assistant/models"},
		{"GET", "/assistant/chats"}, {"POST", "/assistant/chats"}, {"DELETE", "/assistant/chats"}, {"GET", "/assistant/chats/0123456789abcdef"},
		{"POST", "/assistant/chats/0123456789abcdef/messages"}, {"POST", "/assistant/chats/0123456789abcdef/actions/x"},
	}
	for _, route := range routes {
		if res, _ := ts.request(route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s %s: %d", route.method, route.path, res.StatusCode)
		}
	}
	// What is left of the agents of 0.9: nothing.
	ts.setup()
	for _, route := range []struct{ method, path string }{{"POST", "/mcp"}, {"GET", "/account/api-keys"}, {"GET", "/system/api-access"}, {"POST", "/assistant/chat"}} {
		if res, _ := ts.request(route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusNotFound && res.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s still there: %d", route.method, route.path, res.StatusCode)
		}
	}
}

func TestHiddenPaths(t *testing.T) {
	for _, p := range []string{".titanos/assistant/alex/x.json", "app/.env", "x/prod.env", "a/../.titanos", "Upload.iso.titanos-part"} {
		if !hiddenPath(p) {
			t.Errorf("%s shown", p)
		}
	}
	for _, p := range []string{"Fotos/2024", "notes.txt", ".recycle/alex/a.txt", "environment.md"} {
		if hiddenPath(p) {
			t.Errorf("%s hidden", p)
		}
	}
}

func TestAssistantAreas(t *testing.T) {
	fake := &fakeChatOllama{next: func(n int, body map[string]any) []string {
		return []string{`{"role":"assistant","content":"ok"}`}
	}}
	ollama := fake.start(t)
	ts := newTestServer(t)
	ts.setup()
	if res, _ := ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "ollama": ollama.URL, "model": "qwen3:8b", "areas": map[string]string{"docker": "maybe"}}); res.StatusCode != http.StatusBadRequest {
		t.Fatal("unknown access level accepted")
	}
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "ollama": ollama.URL, "model": "qwen3:8b",
		"areas": map[string]string{"docker": "off", "vms": "read", "files": "write", "system": "write"}})
	id := ts.newChat(ts.client)
	ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]any{"text": "Hallo"})
	names := toolNames(fake.seen[0])
	for _, gone := range []string{"docker_list", "container_action", "app_action", "vm_action", "power_action", "update_action"} {
		if slices.Contains(names, gone) {
			t.Fatalf("%s offered: %v", gone, names)
		}
	}
	for _, want := range []string{"vm_list", "file_write", "files_move", "start_task", "api_routes", "api_change", "update_status"} {
		if !slices.Contains(names, want) {
			t.Fatalf("%s missing: %v", want, names)
		}
	}
}

func TestAssistantAPIRoutes(t *testing.T) {
	source, err := os.ReadFile("api.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range apiRouteList() {
		if !strings.Contains(string(source), `"`+route+`"`) {
			t.Errorf("%s is not a route of the server", route)
		}
	}
	for _, secret := range []string{"/backups/{id}/password", "/config-backup/download", "/network/confirm", "/system/certificate", "/terminal/shell", "/files/download", "/users"} {
		for _, r := range apiRoutes {
			if r.Path == secret && (secret != "/users" || r.Method != "GET") {
				t.Errorf("%s %s is in the list", r.Method, r.Path)
			}
		}
	}
	r := apiRoute{"DELETE", "/vms/{name}/snapshots/{snapshot}", "", "", ""}
	if params, ok := r.match("DELETE", "/vms/win11/snapshots/vor%20Update"); !ok || params["snapshot"] != "vor Update" {
		t.Fatalf("match: %v %v", params, ok)
	}
	for _, bad := range []string{"/vms/win11/snapshots/..", "/vms/win11/snapshots/a%2Fb", "/vms/win11/snapshots"} {
		if _, ok := r.match("DELETE", bad); ok {
			t.Errorf("%s matched", bad)
		}
	}
}

func TestAssistantAPIRead(t *testing.T) {
	var reads []string
	fake := &fakeChatOllama{next: func(n int, body map[string]any) []string {
		switch n {
		case 1:
			return []string{`{"role":"assistant","content":"","tool_calls":[{"function":{"name":"api_read","arguments":{"path":"/about"}}},{"function":{"name":"api_read","arguments":{"path":"/backups/x/password"}}},{"function":{"name":"api_read","arguments":{"path":"https://evil/api/v1/about"}}}]}`}
		default:
			messages := body["messages"].([]any)
			for _, m := range messages {
				if m := m.(map[string]any); m["role"] == "tool" {
					reads = append(reads, m["content"].(string))
				}
			}
			return []string{`{"role":"assistant","content":"fertig"}`}
		}
	}}
	ollama := fake.start(t)
	ts := newTestServer(t)
	ts.setup()
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "ollama": ollama.URL, "model": "qwen3:8b"})
	id := ts.newChat(ts.client)
	ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]any{"text": "Version?"})
	if len(reads) != 3 || !strings.Contains(reads[0], "1.2.3") || !strings.Contains(reads[1], "not in api_routes") || !strings.Contains(reads[2], "without host") {
		t.Fatalf("reads: %q", reads)
	}
}

func TestAssistantTask(t *testing.T) {
	fake := &fakeChatOllama{next: func(n int, body map[string]any) []string {
		switch n {
		case 1:
			return []string{`{"role":"assistant","content":"","tool_calls":[{"function":{"name":"start_task","arguments":{"title":"Aufräumen","steps":["Zeitzone setzen","Benachrichtigung testen"],"reason":"mehrere Schritte"}}}]}`}
		case 2:
			return []string{`{"role":"assistant","content":"","tool_calls":[{"function":{"name":"api_change","arguments":{"method":"PUT","path":"/system/timezone","body":{"timezone":"Europe/Berlin"},"summary":"Zeitzone Berlin","reason":"Plan"}}}]}`}
		default:
			return []string{`{"role":"assistant","content":"Erledigt."}`}
		}
	}}
	ollama := fake.start(t)
	ts := newTestServer(t)
	ts.setup()
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "ollama": ollama.URL, "model": "qwen3:8b", "areas": map[string]string{"system": "write"}})
	id := ts.newChat(ts.client)
	ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]any{"text": "Räum auf"})
	chat := ts.chat(ts.client, id)
	task := chat.Chat.Messages[len(chat.Chat.Messages)-1].Action
	if task == nil || task.Key != "task" || task.Method != "" || task.Path != "" {
		t.Fatalf("task card: %+v", task)
	}
	// Approving the plan runs nothing by itself; the next action is marked
	// to run without asking.
	ts.request("POST", "/api/v1/assistant/chats/"+id+"/actions/"+task.ID, map[string]string{"state": "done"})
	chat = ts.chat(ts.client, id)
	change := chat.Chat.Messages[len(chat.Chat.Messages)-1]
	if change.Action == nil || change.Action.Key != "api.change" || change.Action.Path != "/api/v1/system/timezone" || change.Action.Body["timezone"] != "Europe/Berlin" {
		t.Fatalf("change: %+v", change.Action)
	}
	var raw struct {
		Chat struct {
			Messages []struct{ Action *struct{ Auto bool } }
			Task     *struct{ Approved bool }
		}
	}
	_, body := ts.request("GET", "/api/v1/assistant/chats/"+id, nil)
	json.Unmarshal(body, &raw)
	if last := raw.Chat.Messages[len(raw.Chat.Messages)-1]; last.Action == nil || !last.Action.Auto || raw.Chat.Task == nil || !raw.Chat.Task.Approved {
		t.Fatalf("not automatic: %s", body)
	}
	ts.request("POST", "/api/v1/assistant/chats/"+id+"/actions/"+change.Action.ID, map[string]string{"state": "done"})
	_, body = ts.request("GET", "/api/v1/assistant/chats/"+id, nil)
	raw.Chat.Task = nil
	json.Unmarshal(body, &raw)
	if raw.Chat.Task != nil {
		t.Fatal("task still open after the final answer")
	}
}

func TestAssistantDocuments(t *testing.T) {
	var lastUser string
	fake := &fakeChatOllama{next: func(n int, body map[string]any) []string {
		messages := body["messages"].([]any)
		for _, m := range messages {
			if m := m.(map[string]any); m["role"] == "user" {
				lastUser = m["content"].(string)
			}
		}
		if n == 1 {
			return []string{`{"role":"assistant","content":"","tool_calls":[{"function":{"name":"file_write","arguments":{"root":"share/Docs","path":"Berichte/Plan","format":"docx","content":"# Plan\n\n- eins\n- zwei","reason":"gewünscht"}}}]}`}
		}
		return []string{`{"role":"assistant","content":"ok"}`}
	}}
	ollama := fake.start(t)
	ts := newTestServer(t)
	ts.setup()
	dir := ts.withShare()
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "ollama": ollama.URL, "model": "qwen3:8b"})

	// The model proposes; nothing is written until the card is confirmed.
	id := ts.newChat(ts.client)
	ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]any{"text": "Schreib einen Plan"})
	msgs := ts.chat(ts.client, id).Chat.Messages
	card := msgs[len(msgs)-1].Action
	if card == nil || card.Key != "file.write" || card.Path != "/api/v1/assistant/files/write" || card.Body["path"] != "Berichte/Plan.docx" {
		t.Fatalf("card: %+v", card)
	}
	if _, err := os.Stat(filepath.Join(dir, "Berichte/Plan.docx")); err == nil {
		t.Fatal("written before confirmation")
	}
	// The browser calls the route of the card.
	if res, body := ts.request("POST", "/api/v1/assistant/files/write", card.Body); res.StatusCode != http.StatusCreated {
		t.Fatalf("write: %d %s", res.StatusCode, body)
	}
	data, err := os.ReadFile(filepath.Join(dir, "Berichte/Plan.docx"))
	if err != nil || !bytes.HasPrefix(data, []byte("PK")) {
		t.Fatalf("docx: %v", err)
	}
	// Excel and PDF; a file that exists is only replaced when asked.
	if res, body := ts.request("POST", "/api/v1/assistant/files/write", map[string]any{"root": "share/Docs", "path": "Liste.xlsx", "sheets": []map[string]any{{"name": "A", "rows": [][]any{{"x", 1}}}}}); res.StatusCode != http.StatusCreated {
		t.Fatalf("xlsx: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("POST", "/api/v1/assistant/files/write", map[string]any{"root": "share/Docs", "path": "Notiz.pdf", "content": "# Hallo"}); res.StatusCode != http.StatusCreated {
		t.Fatal("pdf failed")
	}
	if res, _ := ts.request("POST", "/api/v1/assistant/files/write", map[string]any{"root": "share/Docs", "path": "Notiz.pdf", "content": "# Neu"}); res.StatusCode != http.StatusConflict {
		t.Fatalf("replaced without asking: %d", res.StatusCode)
	}
	if res, _ := ts.request("POST", "/api/v1/assistant/files/write", map[string]any{"root": "share/Docs", "path": "Notiz.pdf", "content": "# Neu", "replace": true}); res.StatusCode != http.StatusCreated {
		t.Fatal("replace failed")
	}
	if _, err := os.Stat(filepath.Join(dir, "Notiz.pdf.prev")); err != nil {
		t.Fatal("old version not kept")
	}
	for _, bad := range []map[string]any{
		{"root": "share/Docs", "path": "x.docx", "format": "pdf", "content": "a"},
		{"root": "share/Docs", "path": "a.json", "content": "{nope"},
		{"root": "share/Docs", "path": ".titanos/x.txt", "content": "a"},
		{"root": "stick", "path": "x.txt", "content": "a"},
		{"root": "share/Docs", "path": "../x.txt", "content": "a"},
	} {
		if res, _ := ts.request("POST", "/api/v1/assistant/files/write", bad); res.StatusCode < 400 {
			t.Errorf("accepted %v", bad)
		}
	}

	// Sorting: moves create missing folders and never replace.
	os.WriteFile(filepath.Join(dir, "a.jpg"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.jpg"), []byte("b"), 0o644)
	if res, body := ts.request("POST", "/api/v1/assistant/files/move", map[string]any{"root": "share/Docs", "moves": []map[string]string{{"from": "a.jpg", "to": "Fotos/2024/a.jpg"}, {"from": "b.jpg", "to": "Fotos/2025/b.jpg"}}}); res.StatusCode != http.StatusOK {
		t.Fatalf("move: %d %s", res.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(dir, "Fotos/2025/b.jpg")); err != nil {
		t.Fatal("not moved")
	}
	if res, _ := ts.request("POST", "/api/v1/assistant/files/move", map[string]any{"root": "share/Docs", "moves": []map[string]string{{"from": "Liste.xlsx", "to": "Notiz.pdf"}}}); res.StatusCode != http.StatusConflict {
		t.Fatal("move replaced a file")
	}

	// A Word file from the NAS arrives at the model as text.
	ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]any{"text": "Lies das", "attachments": []map[string]string{{"kind": "nas", "name": "x", "root": "share/Docs", "path": "Berichte/Plan.docx"}}})
	if !strings.Contains(lastUser, "Lies das") || !strings.Contains(lastUser, "# Plan") || !strings.Contains(lastUser, "- eins") {
		t.Fatalf("attachment: %q", lastUser)
	}
	if res, _ := ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]any{"text": "x", "attachments": []map[string]string{{"kind": "nas", "name": "x", "root": "stick", "path": "users/alex/account.json"}}}); res.StatusCode != http.StatusBadRequest {
		t.Fatal("secret of the stick attached")
	}
}
