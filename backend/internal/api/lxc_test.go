package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/appbackup"
	"github.com/ra5on/TitanOS/backend/internal/lxc"
)

// System containers are for owners and administrators only, like VMs and
// Docker; in development mode nothing changes on the host.
func TestLXCRoutesNeedAnAdministrator(t *testing.T) {
	ts := newTestServer(t)
	routes := []struct{ method, path string }{
		{"GET", "/lxc"}, {"GET", "/lxc/images"}, {"POST", "/lxc"}, {"PUT", "/lxc/web"}, {"DELETE", "/lxc/web"},
		{"POST", "/lxc/web/start"}, {"GET", "/lxc/web/console"}, {"GET", "/lxc/web/config"},
		{"GET", "/lxc/web/snapshots"}, {"POST", "/lxc/web/snapshots"},
	}
	for _, route := range routes {
		if res, _ := ts.request(route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s %s: %d", route.method, route.path, res.StatusCode)
		}
	}
	ts.setup()
	if res, body := ts.request("POST", "/api/v1/users", map[string]any{"name": "reader", "password": testPassword, "role": "user"}); res.StatusCode != http.StatusCreated {
		t.Fatal(res.StatusCode, string(body))
	}
	user := ts.newClient()
	ts.do(user, "POST", "/api/v1/login", credentials{Name: "reader", Password: testPassword})
	for _, route := range routes {
		if res, _ := ts.do(user, route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Fatalf("user %s %s: %d", route.method, route.path, res.StatusCode)
		}
	}

	res, body := ts.request("GET", "/api/v1/lxc", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"containers":[]`) || !strings.Contains(string(body), `"dev":true`) {
		t.Fatalf("list: %d %s", res.StatusCode, body)
	}
	create := map[string]any{"name": "web", "volume": "data", "distribution": "debian", "release": "trixie", "network": "none"}
	if res, body := ts.request("POST", "/api/v1/lxc", create); res.StatusCode != http.StatusConflict || errorCode(t, body) != "read_only" {
		t.Fatalf("create in development mode: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/lxc/web/start", nil); res.StatusCode != http.StatusConflict || errorCode(t, body) != "read_only" {
		t.Fatalf("start in development mode: %d %s", res.StatusCode, body)
	}

	// A stored container is listed, and its generated configuration shown.
	if err := ts.store.WriteJSON("lxc/web.json", lxc.Container{Name: "web", Volume: "data", Distribution: "debian", Release: "trixie", Arch: "amd64", Network: "none", MAC: "00:16:3e:00:00:01"}); err != nil {
		t.Fatal(err)
	}
	if err := ts.server.opts.LXC.Load(); err != nil {
		t.Fatal(err)
	}
	if res, body := ts.request("GET", "/api/v1/lxc", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"name":"web"`) || !strings.Contains(string(body), `"state":"stopped"`) {
		t.Fatalf("list: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/lxc/web/config", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), "lxc.idmap") {
		t.Fatalf("config: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/lxc/nope/config", nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "lxc_not_found" {
		t.Fatalf("unknown: %d %s", res.StatusCode, body)
	}
}

// An application backup owns the lifecycle of applications: containers
// are not started, stopped or opened while it runs.
func TestLXCWaitsForApplicationBackups(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	gate := &appbackup.Manager{}
	ts.server.opts.ApplicationBackups = gate
	gate.Gate.Lock()
	defer gate.Gate.Unlock()
	for _, route := range []struct{ method, path string }{{"POST", "/lxc/web/stop"}, {"DELETE", "/lxc/web"}, {"POST", "/lxc"}, {"GET", "/lxc/web/console"}} {
		if res, body := ts.request(route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusLocked || errorCode(t, body) != "application_backup_running" {
			t.Fatalf("%s %s: %d %s", route.method, route.path, res.StatusCode, body)
		}
	}
	// Looking stays possible.
	if res, body := ts.request("GET", "/api/v1/lxc", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("list: %d %s", res.StatusCode, body)
	}
}
