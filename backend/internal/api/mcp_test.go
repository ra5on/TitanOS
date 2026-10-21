package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// mcpCall sends one JSON-RPC message with an API key.
func (ts *testServer) mcpCall(token string, message any) (*http.Response, map[string]any) {
	ts.t.Helper()
	var body io.Reader
	switch m := message.(type) {
	case string:
		body = strings.NewReader(m)
	default:
		data, _ := json.Marshal(m)
		body = bytes.NewReader(data)
	}
	req, _ := http.NewRequest("POST", ts.url+"/api/v1/mcp", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// A program, not a browser: no cookie jar.
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		ts.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	var answer map[string]any
	if len(data) > 0 {
		if err := json.Unmarshal(data, &answer); err != nil {
			ts.t.Fatalf("answer is not JSON: %s", data)
		}
	}
	return res, answer
}

func rpc(id int, method string, params any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
}

func (ts *testServer) newAPIKey(scopes ...string) string {
	ts.t.Helper()
	res, body := ts.request("POST", "/api/v1/account/api-keys", auth.NewAPIKey{Name: "agent-" + strings.Join(scopes, "-"), Scopes: scopes, ExpiresDays: 90})
	if res.StatusCode != http.StatusCreated {
		ts.t.Fatalf("create key: %d %s", res.StatusCode, body)
	}
	var created struct{ Token string }
	json.Unmarshal(body, &created)
	return created.Token
}

func toolNames(answer map[string]any) []string {
	var names []string
	result, _ := answer["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	for _, tool := range tools {
		names = append(names, tool.(map[string]any)["name"].(string))
	}
	return names
}

func TestMCPWithAPIKey(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()

	// Off by default: keys can be made but do not work yet.
	if res, body := ts.request("GET", "/api/v1/system/api-access", nil); res.StatusCode != 200 || !strings.Contains(string(body), `"enabled":false`) {
		t.Fatalf("switch: %d %s", res.StatusCode, body)
	}
	token := ts.newAPIKey(auth.ScopeStatus, auth.ScopeStorage)
	if res, _ := ts.mcpCall(token, rpc(1, "ping", nil)); res.StatusCode != http.StatusForbidden {
		t.Fatalf("switched off: %d", res.StatusCode)
	}
	if res, _ := ts.request("PUT", "/api/v1/system/api-access", map[string]bool{"enabled": true}); res.StatusCode != 200 {
		t.Fatalf("switch on: %d", res.StatusCode)
	}

	res, answer := ts.mcpCall(token, rpc(1, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "1"}}))
	result, _ := answer["result"].(map[string]any)
	if res.StatusCode != 200 || result["protocolVersion"] != "2025-06-18" || result["capabilities"].(map[string]any)["tools"] == nil {
		t.Fatalf("initialize: %d %v", res.StatusCode, answer)
	}
	// An unknown version gets the server's own.
	if _, answer := ts.mcpCall(token, rpc(2, "initialize", map[string]any{"protocolVersion": "1999-01-01"})); answer["result"].(map[string]any)["protocolVersion"] != "2025-06-18" {
		t.Fatalf("version: %v", answer)
	}
	if res, _ := ts.mcpCall(token, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("notification: %d", res.StatusCode)
	}

	// Only the tools of the key's scopes are listed and callable.
	_, answer = ts.mcpCall(token, rpc(3, "tools/list", nil))
	names := toolNames(answer)
	for _, want := range []string{"system_status", "notifications", "update_status", "ups_status", "storage_overview", "shares_list"} {
		if !slices.Contains(names, want) {
			t.Fatalf("%s missing from %v", want, names)
		}
	}
	for _, other := range []string{"docker_list", "logs_search", "container_logs", "vm_list", "backup_status"} {
		if slices.Contains(names, other) {
			t.Fatalf("%s listed without its scope", other)
		}
	}
	_, answer = ts.mcpCall(token, rpc(4, "tools/call", map[string]any{"name": "docker_list"}))
	if answer["error"] == nil {
		t.Fatalf("tool outside the scopes ran: %v", answer)
	}

	for _, tool := range []string{"system_status", "notifications", "update_status", "ups_status", "storage_overview", "shares_list"} {
		_, answer = ts.mcpCall(token, rpc(5, "tools/call", map[string]any{"name": tool, "arguments": map[string]any{}}))
		result, _ := answer["result"].(map[string]any)
		if result == nil || result["isError"] == true || result["structuredContent"] == nil {
			t.Fatalf("%s: %v", tool, answer)
		}
		text := result["content"].([]any)[0].(map[string]any)["text"].(string)
		if !json.Valid([]byte(text)) {
			t.Fatalf("%s: text is not the JSON of the result: %s", tool, text)
		}
	}
	_, answer = ts.mcpCall(token, rpc(6, "tools/call", map[string]any{"name": "system_status"}))
	if version := answer["result"].(map[string]any)["structuredContent"].(map[string]any)["version"]; version != "1.2.3" {
		t.Fatalf("system_status: %v", answer)
	}

	// A wrong argument is told to the agent as the result, so it can try again.
	_, answer = ts.mcpCall(token, rpc(7, "tools/call", map[string]any{"name": "notifications", "arguments": map[string]any{"count": 3}}))
	if result := answer["result"].(map[string]any); result["isError"] != true || !strings.Contains(result["content"].([]any)[0].(map[string]any)["text"].(string), "count") {
		t.Fatalf("unknown argument: %v", answer)
	}
	_, answer = ts.mcpCall(token, rpc(8, "tools/call", map[string]any{"name": "rm_rf"}))
	if answer["error"].(map[string]any)["code"].(float64) != -32602 {
		t.Fatalf("unknown tool: %v", answer)
	}
	_, answer = ts.mcpCall(token, rpc(9, "resources/list", nil))
	if answer["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Fatalf("unknown method: %v", answer)
	}
	if res, _ := ts.mcpCall(token, `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("batch: %d", res.StatusCode)
	}
	if res, _ := ts.mcpCall(token, `{nope`); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad JSON: %d", res.StatusCode)
	}
	req, _ := http.NewRequest("GET", ts.url+"/api/v1/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", res.StatusCode)
	}
	req, _ = http.NewRequest("POST", ts.url+"/api/v1/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("MCP-Protocol-Version", "1999-01-01")
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unsupported protocol version header: %d", res.StatusCode)
	}

	// The key opens nothing but the MCP endpoint.
	for _, path := range []string{"/api/v1/storage", "/api/v1/system", "/api/v1/account/api-keys", "/api/v1/notifications"} {
		req, _ := http.NewRequest("GET", ts.url+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		if res, _ := http.DefaultClient.Do(req); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("key opened %s: %d", path, res.StatusCode)
		}
	}

	// Logs come only with their scope; an unknown source is the agent's mistake.
	logs := ts.newAPIKey(auth.ScopeLogs)
	_, answer = ts.mcpCall(logs, rpc(10, "tools/list", nil))
	if names := toolNames(answer); !slices.Equal(names, []string{"logs_search"}) {
		t.Fatalf("logs key: %v", names)
	}
	_, answer = ts.mcpCall(logs, rpc(11, "tools/call", map[string]any{"name": "logs_search", "arguments": map[string]any{"source": "/etc"}}))
	if answer["result"].(map[string]any)["isError"] != true {
		t.Fatalf("unknown source: %v", answer)
	}

	// Removing the key ends it at once.
	var list apiKeysResponse
	_, body := ts.request("GET", "/api/v1/account/api-keys", nil)
	json.Unmarshal(body, &list)
	if len(list.Keys) != 2 || list.Keys[0].LastUsed == nil || !list.Enabled || len(list.Scopes) != len(auth.Scopes) {
		t.Fatalf("list: %s", body)
	}
	if strings.Contains(string(body), token[len(auth.APIKeyPrefix)+9:]) {
		t.Fatal("the list shows a secret")
	}
	if res, _ := ts.request("DELETE", "/api/v1/account/api-keys/"+list.Keys[0].ID, nil); res.StatusCode != 200 {
		t.Fatalf("remove: %d", res.StatusCode)
	}
	if res, _ := ts.mcpCall(token, rpc(12, "ping", nil)); res.StatusCode != http.StatusUnauthorized || res.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("removed key: %d", res.StatusCode)
	}
}

func TestMCPWithoutKey(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.request("PUT", "/api/v1/system/api-access", map[string]bool{"enabled": true})

	// The session cookie does not open the MCP endpoint.
	res, _ := ts.request("POST", "/api/v1/mcp", rpc(1, "ping", nil))
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("cookie: %d", res.StatusCode)
	}
	// Guessing is slowed down like logins.
	limited := false
	for range 30 {
		res, _ := ts.mcpCall(auth.APIKeyPrefix+"deadbeef_guess", rpc(1, "ping", nil))
		if res.StatusCode == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("wrong keys are not slowed down")
	}
}

func TestAPIKeyPermissions(t *testing.T) {
	ts := newTestServer(t)
	for _, route := range []struct{ method, path string }{{"GET", "/account/api-keys"}, {"POST", "/account/api-keys"}, {"DELETE", "/account/api-keys/x"}, {"GET", "/system/api-access"}, {"PUT", "/system/api-access"}, {"DELETE", "/users/alex/api-keys"}} {
		if res, _ := ts.request(route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s %s: %d", route.method, route.path, res.StatusCode)
		}
	}
	ts.setup()
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, route := range []struct{ method, path string }{{"GET", "/account/api-keys"}, {"POST", "/account/api-keys"}, {"PUT", "/system/api-access"}, {"DELETE", "/users/alex/api-keys"}} {
		if res, _ := ts.do(guest, route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Fatalf("user %s %s: %d", route.method, route.path, res.StatusCode)
		}
	}

	// An administrator may not take an owner's keys; the owner may take theirs.
	ts.users.Create("sam", testPassword, auth.RoleAdmin)
	admin := ts.newClient()
	ts.do(admin, "POST", "/api/v1/login", credentials{Name: "sam", Password: testPassword})
	ts.newAPIKey(auth.ScopeStatus)
	if res, _ := ts.do(admin, "DELETE", "/api/v1/users/alex/api-keys", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("admin removed owner keys: %d", res.StatusCode)
	}
	if res, body := ts.do(admin, "POST", "/api/v1/account/api-keys", auth.NewAPIKey{Name: "sam-agent", Scopes: []string{auth.ScopeDocker}}); res.StatusCode != http.StatusCreated {
		t.Fatalf("admin key: %d %s", res.StatusCode, body)
	}
	_, body := ts.request("GET", "/api/v1/users", nil)
	if !strings.Contains(string(body), `"name":"sam","role":"admin"`) || !strings.Contains(string(body), `"apiKeys":1`) {
		t.Fatalf("users list: %s", body)
	}
	if res, _ := ts.request("DELETE", "/api/v1/users/sam/api-keys", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("owner removes keys: %d", res.StatusCode)
	}
	if _, body := ts.do(admin, "GET", "/api/v1/account/api-keys", nil); strings.Contains(string(body), "sam-agent") {
		t.Fatalf("keys left: %s", body)
	}
	if res, body := ts.request("POST", "/api/v1/account/api-keys", auth.NewAPIKey{Name: "x", Scopes: []string{"docker:act"}}); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_scope" {
		t.Fatalf("action scope: %d %s", res.StatusCode, body)
	}
}
