package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

type mcpReply struct {
	status int
	header http.Header
	body   []byte
	msg    map[string]any
}

// mcp posts one JSON-RPC body to /mcp. version is the MCP-Protocol-Version
// header, left out when empty.
func (ts *testServer) mcp(token, version, body string, header ...string) mcpReply {
	ts.t.Helper()
	req, _ := http.NewRequest("POST", ts.url+"/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if version != "" {
		req.Header.Set("MCP-Protocol-Version", version)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		ts.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	reply := mcpReply{status: res.StatusCode, header: res.Header, body: data}
	json.Unmarshal(data, &reply.msg)
	return reply
}

func (ts *testServer) mcpToken(user string, scopes ...string) string {
	ts.t.Helper()
	account, ok := ts.users.Get(user)
	if !ok {
		ts.t.Fatal("no account", user)
	}
	_, plain, err := ts.server.tokens.Create(account, "claude", scopes)
	if err != nil {
		ts.t.Fatal(err)
	}
	return plain
}

func (ts *testServer) mcpOn() {
	ts.t.Helper()
	if res, body := ts.request("PUT", "/api/v1/system/mcp", map[string]any{"enabled": true}); res.StatusCode != 200 {
		ts.t.Fatalf("switch on: %d %s", res.StatusCode, body)
	}
}

func rpc(id int, method string, params any) string {
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return string(data)
}

func mcpToolNames(t *testing.T, reply mcpReply) []string {
	t.Helper()
	result, _ := reply.msg["result"].(map[string]any)
	list, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list: %d %s", reply.status, reply.body)
	}
	var names []string
	for _, v := range list {
		names = append(names, v.(map[string]any)["name"].(string))
	}
	return names
}

// mcpCall calls a tool and returns its text and whether it is an error.
func (ts *testServer) mcpCall(token, name string, args any) (string, bool, map[string]any) {
	ts.t.Helper()
	reply := ts.mcp(token, "2025-06-18", rpc(7, "tools/call", map[string]any{"name": name, "arguments": args}))
	result, ok := reply.msg["result"].(map[string]any)
	if !ok {
		ts.t.Fatalf("%s: %d %s", name, reply.status, reply.body)
	}
	content := result["content"].([]any)[0].(map[string]any)
	return content["text"].(string), result["isError"] == true, result
}

func TestMCPHandshakeSwitchAndScope(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	plain := ts.mcpToken("alex", "mcp", "system:read")
	noMCP := ts.mcpToken("alex", "system:read")
	init := rpc(1, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "1"}})

	// Off by default.
	if r := ts.mcp(plain, "", init); r.status != http.StatusForbidden || !strings.Contains(string(r.body), "mcp_disabled") {
		t.Fatalf("off: %d %s", r.status, r.body)
	}
	if _, body := ts.request("GET", "/api/v1/system/mcp", nil); !strings.Contains(string(body), `"enabled":false`) {
		t.Fatal(string(body))
	}
	ts.mcpOn()
	if r := ts.mcp("", "", init); r.status != http.StatusUnauthorized || !strings.Contains(r.header.Get("WWW-Authenticate"), "Bearer") {
		t.Fatalf("no token: %d", r.status)
	}
	if r := ts.mcp("titanos_x_y", "", init); r.status != http.StatusUnauthorized {
		t.Fatalf("bad token: %d", r.status)
	}
	if r := ts.mcp(noMCP, "", init); r.status != http.StatusForbidden || !strings.Contains(string(r.body), "token_scope_required") {
		t.Fatalf("missing scope: %d %s", r.status, r.body)
	}
	// The public API does not open with the scope mcp alone.
	req, _ := http.NewRequest("GET", ts.url+"/api/v1/files/roots", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != http.StatusForbidden {
		t.Fatal("files open with system:read+mcp", res.StatusCode)
	}

	for want, asked := range map[string]string{"2025-06-18": "2025-06-18", "2025-03-26": "2025-03-26"} {
		r := ts.mcp(plain, "", rpc(1, "initialize", map[string]any{"protocolVersion": asked, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}}))
		result, _ := r.msg["result"].(map[string]any)
		info, _ := result["serverInfo"].(map[string]any)
		if r.status != 200 || result["protocolVersion"] != want || info["name"] != "titanos" || result["capabilities"].(map[string]any)["tools"] == nil || r.msg["id"] != float64(1) {
			t.Fatalf("initialize %s: %d %s", asked, r.status, r.body)
		}
	}
	r := ts.mcp(plain, "", rpc(1, "initialize", map[string]any{"protocolVersion": "1999-01-01"}))
	if r.msg["result"].(map[string]any)["protocolVersion"] != "2025-06-18" {
		t.Fatalf("unknown version: %s", r.body)
	}
	if r := ts.mcp(plain, "2025-06-18", `{"jsonrpc":"2.0","method":"notifications/initialized"}`); r.status != http.StatusAccepted || len(r.body) != 0 {
		t.Fatalf("notification: %d %s", r.status, r.body)
	}
	if r := ts.mcp(plain, "2024-11-05", rpc(2, "ping", nil)); r.status != http.StatusBadRequest {
		t.Fatalf("unsupported header: %d", r.status)
	}
	if r := ts.mcp(plain, "2025-06-18", rpc(2, "ping", nil)); r.status != 200 || r.msg["result"] == nil {
		t.Fatalf("ping: %s", r.body)
	}
	if r := ts.mcp(plain, "2025-06-18", rpc(3, "resources/list", nil)); r.msg["error"].(map[string]any)["code"] != float64(rpcMethodNotFound) {
		t.Fatalf("unknown method: %s", r.body)
	}
	if r := ts.mcp(plain, "2025-06-18", `{"jsonrpc":"2.0","id":4,`); r.status != http.StatusBadRequest || !strings.Contains(string(r.body), "-32700") {
		t.Fatalf("parse error: %d %s", r.status, r.body)
	}
	// Batches only in 2025-03-26.
	batch := "[" + rpc(5, "ping", nil) + "," + `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "," + rpc(6, "tools/list", nil) + "]"
	r = ts.mcp(plain, "2025-03-26", batch)
	var answers []map[string]any
	if json.Unmarshal(r.body, &answers); r.status != 200 || len(answers) != 2 || answers[0]["id"] != float64(5) || answers[1]["id"] != float64(6) {
		t.Fatalf("batch: %d %s", r.status, r.body)
	}
	if r := ts.mcp(plain, "2025-06-18", batch); r.status != http.StatusBadRequest {
		t.Fatalf("batch in 2025-06-18: %d", r.status)
	}
	// A client that only takes a stream gets one SSE event.
	r = ts.mcp(plain, "2025-06-18", rpc(8, "ping", nil), "Accept", "text/event-stream")
	data, ok := strings.CutPrefix(strings.SplitN(string(r.body), "\n", 3)[1], "data: ")
	var event map[string]any
	if !ok || r.header.Get("Content-Type") != "text/event-stream" || json.Unmarshal([]byte(data), &event) != nil || event["id"] != float64(8) {
		t.Fatalf("sse: %q %s", r.header.Get("Content-Type"), r.body)
	}
	req, _ = http.NewRequest("GET", ts.url+"/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatal("GET", res.StatusCode)
	}
	// A browser page of another site cannot call it.
	if r := ts.mcp(plain, "2025-06-18", rpc(9, "ping", nil), "Origin", "https://evil.example", "Sec-Fetch-Site", "cross-site"); r.status != http.StatusForbidden {
		t.Fatalf("cross-site: %d", r.status)
	}

	// structuredContent only for 2025-06-18.
	if _, isErr, result := ts.mcpCall(plain, "system_status", map[string]any{}); isErr || result["structuredContent"].(map[string]any)["version"] != "1.2.3" {
		t.Fatalf("system_status: %v", result)
	}
	old := ts.mcp(plain, "", rpc(10, "tools/call", map[string]any{"name": "system_status", "arguments": map[string]any{}}))
	if res := old.msg["result"].(map[string]any); res["structuredContent"] != nil || res["isError"] != false {
		t.Fatalf("2025-03-26 result: %s", old.body)
	}
	// Tools of scopes the token does not have do not exist for it.
	if r := ts.mcp(plain, "2025-06-18", rpc(11, "tools/call", map[string]any{"name": "files_list", "arguments": map[string]any{}})); r.msg["error"].(map[string]any)["code"] != float64(rpcInvalidParams) {
		t.Fatalf("tool without scope: %s", r.body)
	}
	if text, isErr, _ := ts.mcpCall(plain, "system_status", map[string]any{"extra": 1}); !isErr || !strings.Contains(text, "unknown field") {
		t.Fatalf("schema not enforced: %s", text)
	}
	// Switching off holds at once.
	ts.request("PUT", "/api/v1/system/mcp", map[string]any{"enabled": false})
	if r := ts.mcp(plain, "2025-06-18", rpc(12, "ping", nil)); r.status != http.StatusForbidden {
		t.Fatalf("after switching off: %d", r.status)
	}
}

func TestMCPSwitchNeedsAdministrator(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.request("POST", "/api/v1/users", map[string]any{"name": "reader", "password": testPassword, "role": "user"})
	reader := ts.newClient()
	ts.do(reader, "POST", "/api/v1/login", credentials{Name: "reader", Password: testPassword})
	if res, _ := ts.do(reader, "PUT", "/api/v1/system/mcp", map[string]any{"enabled": true}); res.StatusCode != http.StatusForbidden {
		t.Fatal(res.StatusCode)
	}
	if res, _ := ts.do(reader, "GET", "/api/v1/system/mcp", nil); res.StatusCode != http.StatusOK {
		t.Fatal(res.StatusCode)
	}
}

func TestMCPToolsFollowAccountRights(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	dir := ts.withShare()
	ts.mcpOn()
	ts.request("POST", "/api/v1/users", map[string]any{"name": "reader", "password": testPassword, "role": "user"})
	ts.request("POST", "/api/v1/users", map[string]any{"name": "other", "password": testPassword, "role": "user"})
	if res, body := ts.request("PUT", "/api/v1/shares/Docs", map[string]any{"name": "Docs", "volume": "tank", "smb": true, "access": map[string]string{"alex": "rw", "reader": "ro"}}); res.StatusCode >= 300 {
		t.Fatalf("access: %d %s", res.StatusCode, body)
	}
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi"), 0o644)
	owner := ts.mcpToken("alex", "mcp", "system:read", "files:read", "files:write", "docker:read", "docker:write", "vms:read", "vms:write")
	reader := ts.mcpToken("reader", "mcp", "system:read", "files:read", "files:write")
	other := ts.mcpToken("other", "mcp", "files:read")
	if _, _, err := ts.server.tokens.Create(mustUser(t, ts, "reader"), "x", []string{"mcp", "docker:read"}); err == nil {
		t.Fatal("a user got a docker token")
	}

	ownerTools := mcpToolNames(t, ts.mcp(owner, "2025-06-18", rpc(1, "tools/list", nil)))
	for _, want := range []string{"system_status", "storage_overview", "shares_list", "docker_list", "vm_list", "backup_status", "notifications", "files_list", "files_delete", "docker_container_stop", "vm_shutdown", "files_write_text"} {
		if !slices.Contains(ownerTools, want) {
			t.Errorf("owner misses %s: %v", want, ownerTools)
		}
	}
	readerTools := mcpToolNames(t, ts.mcp(reader, "2025-06-18", rpc(1, "tools/list", nil)))
	for _, name := range readerTools {
		if slices.Contains([]string{"system_status", "storage_overview", "shares_list", "docker_list", "vm_list", "backup_status", "notifications", "docker_container_stop", "vm_start"}, name) {
			t.Errorf("user sees %s", name)
		}
	}
	for _, want := range []string{"nas_info", "my_shares", "files_list", "files_mkdir"} {
		if !slices.Contains(readerTools, want) {
			t.Errorf("user misses %s: %v", want, readerTools)
		}
	}

	// Own shares only, rights of the share.
	if text, isErr, _ := ts.mcpCall(reader, "files_list", map[string]any{}); isErr || !strings.Contains(text, "share/Docs") {
		t.Fatalf("reader roots: %s", text)
	}
	if text, isErr, _ := ts.mcpCall(other, "files_list", map[string]any{}); isErr || strings.Contains(text, "share/Docs") {
		t.Fatalf("other sees Docs: %s", text)
	}
	if text, isErr, _ := ts.mcpCall(other, "files_list", map[string]any{"root": "share/Docs"}); !isErr {
		t.Fatalf("other lists Docs: %s", text)
	}
	if text, isErr, _ := ts.mcpCall(reader, "files_list", map[string]any{"root": "share/Docs"}); isErr || !strings.Contains(text, "hello.txt") {
		t.Fatalf("reader list: %s", text)
	}
	// Text files open through the editor, which needs write access, as over /api/v1.
	if text, isErr, _ := ts.mcpCall(reader, "files_read_text", map[string]any{"root": "share/Docs", "path": "hello.txt"}); !isErr {
		t.Fatalf("reader editor read: %s", text)
	}
	text, isErr, result := ts.mcpCall(owner, "files_read_text", map[string]any{"root": "share/Docs", "path": "hello.txt"})
	version, _ := result["structuredContent"].(map[string]any)["version"].(string)
	if isErr || version == "" {
		t.Fatalf("owner read: %s", text)
	}
	if text, isErr, _ := ts.mcpCall(owner, "files_write_text", map[string]any{"root": "share/Docs", "path": "hello.txt", "content": "neu", "version": version}); !isErr {
		t.Fatalf("written without confirm: %s", text)
	}
	if text, isErr, _ := ts.mcpCall(owner, "files_write_text", map[string]any{"root": "share/Docs", "path": "hello.txt", "content": "neu", "version": version, "confirm": true}); isErr {
		t.Fatalf("write: %s", text)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "hello.txt")); string(data) != "neu" {
		t.Fatalf("content: %q", data)
	}
	if text, isErr, _ := ts.mcpCall(reader, "files_mkdir", map[string]any{"root": "share/Docs", "path": "nope"}); !isErr {
		t.Fatalf("read-only share written: %s", text)
	}
	if _, err := os.Stat(filepath.Join(dir, "nope")); err == nil {
		t.Fatal("folder created on a read-only share")
	}
	if text, isErr, _ := ts.mcpCall(owner, "files_mkdir", map[string]any{"root": "share/Docs", "path": "Neu"}); isErr {
		t.Fatalf("owner mkdir: %s", text)
	}
	if info, err := os.Stat(filepath.Join(dir, "Neu")); err != nil || !info.IsDir() {
		t.Fatal("owner folder missing", err)
	}
	// The VM name cannot leave its route.
	if text, isErr, _ := ts.mcpCall(owner, "vm_start", map[string]any{"name": "../../files/mkdir"}); !isErr {
		t.Fatalf("path escape: %s", text)
	}

	// The role counts at every call, not when the token was made.
	if _, err := ts.users.Create("second", testPassword, auth.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.users.SetRole("alex", auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	demoted := mcpToolNames(t, ts.mcp(owner, "2025-06-18", rpc(2, "tools/list", nil)))
	if slices.Contains(demoted, "docker_list") || slices.Contains(demoted, "docker_container_start") || slices.Contains(demoted, "system_status") {
		t.Fatalf("demoted account keeps admin tools: %v", demoted)
	}
}

func mustUser(t *testing.T, ts *testServer, name string) auth.User {
	t.Helper()
	u, ok := ts.users.Get(name)
	if !ok {
		t.Fatal("no", name)
	}
	return u
}

func TestMCPDestructiveNeedsConfirmAndIsLogged(t *testing.T) {
	ts := newTestServer(t)
	log := activity.New(ts.store)
	ts.server.opts.Activity = log
	ts.setup()
	dir := ts.withShare()
	ts.mcpOn()
	file := filepath.Join(dir, "weg.txt")
	os.WriteFile(file, []byte("x"), 0o644)
	plain := ts.mcpToken("alex", "mcp", "system:read", "files:read", "files:write")

	tools := ts.mcp(plain, "2025-06-18", rpc(1, "tools/list", nil)).msg["result"].(map[string]any)["tools"].([]any)
	for _, v := range tools {
		tool := v.(map[string]any)
		if tool["name"] != "files_delete" {
			continue
		}
		schema := tool["inputSchema"].(map[string]any)
		required, _ := json.Marshal(schema["required"])
		confirm, _ := schema["properties"].(map[string]any)["confirm"].(map[string]any)
		if !strings.Contains(string(required), `"confirm"`) || confirm == nil || !strings.Contains(confirm["description"].(string), "explicitly") || tool["annotations"].(map[string]any)["destructiveHint"] != true {
			t.Fatalf("files_delete schema: %v", tool)
		}
	}
	for _, args := range []map[string]any{
		{"root": "share/Docs", "paths": []string{"weg.txt"}, "permanent": true},
		{"root": "share/Docs", "paths": []string{"weg.txt"}, "permanent": true, "confirm": false},
		{"root": "share/Docs", "paths": []string{"weg.txt"}, "permanent": true, "confirm": "yes"},
	} {
		if text, isErr, _ := ts.mcpCall(plain, "files_delete", args); !isErr {
			t.Fatalf("deleted without confirm=true: %v %s", args, text)
		}
		if _, err := os.Stat(file); err != nil {
			t.Fatal("file gone without confirmation")
		}
	}
	if text, isErr, _ := ts.mcpCall(plain, "files_delete", map[string]any{"root": "share/Docs", "paths": []string{"weg.txt"}, "permanent": true, "confirm": true}); isErr {
		t.Fatalf("delete: %s", text)
	}
	if _, err := os.Stat(file); err == nil {
		t.Fatal("file still there")
	}
	ts.mcpCall(plain, "system_status", map[string]any{})

	rows := activityEntries(t, log)
	del, ok := findActivity(rows, "POST /api/v1/files/delete")
	if !ok || del.Via != activity.ViaMCP || !strings.Contains(del.Target, "token claude") || !strings.Contains(del.Target, "files_delete") || !strings.Contains(del.Target, "weg.txt") || !del.OK || del.User != "alex" {
		t.Fatalf("delete entry: %+v %v", del, ok)
	}
	read, ok := findActivity(rows, "mcp_tool")
	if !ok || read.Via != activity.ViaMCP || !strings.Contains(read.Target, "system_status") {
		t.Fatalf("read entry: %+v", read)
	}
	refused := 0
	for _, e := range rows {
		if e.Via == activity.ViaMCP && !e.OK {
			refused++
		}
		if e.Via == activity.ViaToken {
			t.Fatalf("MCP call recorded twice: %+v", e)
		}
	}
	if refused != 3 {
		t.Fatalf("refused calls recorded: %d", refused)
	}
}

func TestMCPToolsComeOnlyFromTheExplicitLists(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.mcpOn()
	plain := ts.mcpToken("alex", auth.TokenScopes...)
	names := mcpToolNames(t, ts.mcp(plain, "2025-06-18", rpc(1, "tools/list", nil)))
	routeTools := map[string]publicRoute{}
	for _, route := range publicRoutes {
		if route.Tool != "" {
			if _, dup := routeTools[route.Tool]; dup {
				t.Fatal("tool twice", route.Tool)
			}
			routeTools[route.Tool] = route
		}
	}
	forbidden := []string{"terminal", "shell", "console", "serial", "screen", "update", "password", "2fa", "totp", "passkey", "token", "account", "user", "ssh", "network_", "firewall", "assistant", "secret", "key", "power", "backup_action", "luks", "config"}
	for _, name := range names {
		_, catalog := mcpCatalog[name]
		route, isRoute := routeTools[name]
		if catalog == isRoute {
			t.Errorf("%s: not exactly one source", name)
		}
		if isRoute && publicScope(route.Method, "/api/v1"+route.Path) != route.Scope {
			t.Errorf("%s is no public route", name)
		}
		for _, word := range forbidden {
			if strings.Contains(name, word) {
				t.Errorf("tool %s looks like a secret route (%s)", name, word)
			}
		}
	}
	for _, route := range publicRoutes {
		if strings.HasPrefix(route.Path, "/files/download") && route.Tool != "" {
			t.Error("binary download as a tool")
		}
		if route.Tool != "" && mcpNeedsConfirm(route) {
			_, required := routeSchema(route)
			if !slices.Contains(required, "confirm") {
				t.Errorf("%s without confirm", route.Tool)
			}
		}
	}
	// Every catalog entry exists for one of the roles.
	all := map[string]bool{}
	for _, tool := range ts.server.statusTools(mustUser(t, ts, "alex")) {
		all[tool.Name] = true
	}
	for _, tool := range ts.server.statusTools(auth.User{Name: "u", Role: auth.RoleUser}) {
		all[tool.Name] = true
	}
	for name := range mcpCatalog {
		if !all[name] {
			t.Errorf("catalog entry %s matches no tool", name)
		}
	}
	// Results never carry the routes the public API leaves out.
	for _, p := range []string{"/api/v1/terminal/shell", "/api/v1/account/tokens", "/api/v1/update/install", "/api/v1/network/confirm", "/api/v1/assistant/settings", "/api/v1/users", "/api/v1/config-backup/download", "/api/v1/system/mcp"} {
		for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
			if publicScope(method, p) != "" {
				t.Errorf("%s %s is public", method, p)
			}
		}
	}
}

func TestMCPRateLimitPerToken(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.mcpOn()
	now := time.Unix(1_800_000_000, 0)
	ts.server.mcp.now = func() time.Time { return now }
	a := ts.mcpToken("alex", "mcp", "system:read")
	b := ts.mcpToken("alex", "mcp", "system:read")
	for i := range mcpBurst {
		if r := ts.mcp(a, "2025-06-18", rpc(i, "ping", nil)); r.status != 200 {
			t.Fatalf("request %d: %d", i, r.status)
		}
	}
	r := ts.mcp(a, "2025-06-18", rpc(99, "ping", nil))
	if r.status != http.StatusTooManyRequests || r.header.Get("Retry-After") == "" || !bytes.Contains(r.body, []byte("-32000")) {
		t.Fatalf("over the limit: %d %s", r.status, r.body)
	}
	if r := ts.mcp(b, "2025-06-18", rpc(1, "ping", nil)); r.status != 200 {
		t.Fatal("other token limited", r.status)
	}
	now = now.Add(3 * time.Second)
	if r := ts.mcp(a, "2025-06-18", rpc(100, "ping", nil)); r.status != 200 {
		t.Fatal("no refill", r.status)
	}
}

// A batch costs one request per message and cannot be made arbitrarily long.
func TestMCPBatchCountsEveryMessage(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.mcpOn()
	now := time.Unix(1_800_000_000, 0)
	ts.server.mcp.now = func() time.Time { return now }
	token := ts.mcpToken("alex", "mcp", "system:read")

	batch := func(from, n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = rpc(from+i, "ping", nil)
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	if r := ts.mcp(token, "2025-03-26", batch(0, mcpMaxBatch+1)); r.status != http.StatusBadRequest {
		t.Fatalf("batch over the cap: %d %s", r.status, r.body)
	}

	// Two full batches use up the burst; the rest of the second gets errors.
	answered, limited := 0, 0
	for round := range 2 {
		r := ts.mcp(token, "2025-03-26", batch(round*100, mcpMaxBatch))
		if r.status != 200 {
			t.Fatalf("batch %d: %d %s", round, r.status, r.body)
		}
		var answers []map[string]any
		if err := json.Unmarshal(r.body, &answers); err != nil || len(answers) != mcpMaxBatch {
			t.Fatalf("batch %d answers: %v %s", round, err, r.body)
		}
		for _, a := range answers {
			if a["error"] != nil {
				limited++
			} else {
				answered++
			}
		}
	}
	// The cap-breaking batch above was refused before it cost anything
	// beyond its own request.
	if answered != mcpBurst-1 || limited != 2*mcpMaxBatch-answered {
		t.Fatalf("answered %d, limited %d of %d", answered, limited, 2*mcpMaxBatch)
	}
	if r := ts.mcp(token, "2025-03-26", rpc(999, "ping", nil)); r.status != http.StatusTooManyRequests {
		t.Fatalf("after the batches: %d", r.status)
	}
}
