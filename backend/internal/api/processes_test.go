package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"syscall"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/procs"
)

// processServer serves the fixture /proc tree of internal/procs; signals
// are recorded, never sent.
func processServer(t *testing.T) (*testServer, *[]int) {
	t.Helper()
	sent := &[]int{}
	ts := newTestServerWith(t, func(o *Options) {
		o.Processes = &procs.Sampler{Root: "../procs/testdata/proc", Passwd: "../procs/testdata/etc/passwd", Self: 100, Parent: 1, PageSize: 4096,
			// The fd stands for the pid; nothing is opened or sent.
			OpenPID:  func(pid int) (int, error) { return pid, nil },
			SendPID:  func(fd int, sig syscall.Signal) error { *sent = append(*sent, fd, int(sig)); return nil },
			ClosePID: func(int) error { return nil }}
	})
	ts.server.opts.Activity = activity.New(ts.store)
	ts.setup()
	return ts, sent
}

func TestProcessList(t *testing.T) {
	ts, _ := processServer(t)
	res, body := ts.request("GET", "/api/v1/system/processes", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list: %d %s", res.StatusCode, body)
	}
	var list procs.List
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Processes) != 7 || list.CPUs != 2 {
		t.Fatalf("list %+v", list)
	}
	for _, p := range list.Processes {
		if p.PID == 300 && (p.Owner.Kind != "vm" || p.Owner.Name != "win-11") {
			t.Fatalf("vm owner %+v", p.Owner)
		}
	}
}

func TestProcessSignalRefusals(t *testing.T) {
	ts, sent := processServer(t)
	cases := []struct {
		pid    string
		body   map[string]any
		status int
		code   string
	}{
		{"1", map[string]any{"signal": "kill", "start": 1}, 403, "process_protected"},
		{"2", map[string]any{"signal": "kill", "start": 2}, 403, "process_protected"},
		{"15", map[string]any{"signal": "term", "start": 20}, 403, "process_protected"},
		{"100", map[string]any{"signal": "kill", "start": 500}, 403, "process_protected"},
		{"101", map[string]any{"signal": "kill", "start": 501}, 404, "process_not_found"},
		{"abc", map[string]any{"signal": "kill", "start": 1}, 404, "process_not_found"},
		{"600", map[string]any{"signal": "kill", "start": 1}, 409, "process_changed"},
		{"600", map[string]any{"signal": "stop", "start": 1300}, 400, "invalid_signal"},
	}
	for _, c := range cases {
		res, body := ts.request("POST", "/api/v1/system/processes/"+c.pid+"/signal", c.body)
		if res.StatusCode != c.status || errorCode(t, body) != c.code {
			t.Errorf("pid %s: %d %s", c.pid, res.StatusCode, body)
		}
	}
	if len(*sent) != 0 {
		t.Fatalf("signals sent: %v", *sent)
	}
	if res, body := ts.request("POST", "/api/v1/system/processes/600/signal", map[string]any{"signal": "term", "start": 1300}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("term: %d %s", res.StatusCode, body)
	}
	if len(*sent) != 2 || (*sent)[0] != 600 || (*sent)[1] != int(syscall.SIGTERM) {
		t.Fatalf("sent %v", *sent)
	}
	rows := activityEntries(t, ts.server.opts.Activity)
	e, ok := findActivity(rows, "POST /api/v1/system/processes/{pid}/signal")
	if !ok || !strings.Contains(e.Target, "bash") || !strings.Contains(e.Target, "term") || e.User != "alex" {
		t.Fatalf("activity %+v %v", e, ok)
	}
}

func TestProcessRoutesRoles(t *testing.T) {
	ts, sent := processServer(t)
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	if res, _ := ts.do(guest, "GET", "/api/v1/system/processes", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("user lists processes: %d", res.StatusCode)
	}
	if res, _ := ts.do(guest, "POST", "/api/v1/system/processes/600/signal", map[string]any{"signal": "kill", "start": 1300}); res.StatusCode != http.StatusForbidden {
		t.Fatalf("user signals: %d", res.StatusCode)
	}
	// The overview of users reads disk throughput from /system.
	if res, body := ts.do(guest, "GET", "/api/v1/system", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"disks":`) {
		t.Fatalf("user system: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(ts.newClient(), "GET", "/api/v1/system/processes", nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", res.StatusCode)
	}

	// An API token, even of the owner with every scope, reaches neither
	// route, and MCP offers no tool for them.
	token := ts.mcpToken("alex", "mcp", "system:read", "system:write", "files:read", "files:write", "docker:read", "docker:write", "vms:read", "vms:write")
	for _, call := range [][2]string{{"GET", "/api/v1/system/processes"}, {"POST", "/api/v1/system/processes/600/signal"}} {
		req, _ := http.NewRequest(call[0], ts.url+call[1], strings.NewReader(`{"signal":"kill","start":1300}`))
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode < 400 {
			t.Errorf("token reached %s %s: %d", call[0], call[1], res.StatusCode)
		}
	}
	ts.mcpOn()
	for _, name := range mcpToolNames(t, ts.mcp(token, "2025-06-18", rpc(1, "tools/list", nil))) {
		if strings.Contains(name, "process") {
			t.Errorf("MCP offers %s", name)
		}
	}
	for _, r := range publicRoutes {
		if strings.Contains(r.Path, "processes") {
			t.Errorf("public route %s %s", r.Method, r.Path)
		}
	}
	if len(*sent) != 0 {
		t.Fatalf("signals sent: %v", *sent)
	}
}

func TestProcessSignalDevMode(t *testing.T) {
	ts, sent := processServer(t)
	ts.server.opts.Processes.Disabled = true
	if res, body := ts.request("POST", "/api/v1/system/processes/600/signal", map[string]any{"signal": "term", "start": 1300}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "dev_mode" {
		t.Fatalf("dev: %d %s", res.StatusCode, body)
	}
	if len(*sent) != 0 {
		t.Fatalf("sent %v", *sent)
	}
}
