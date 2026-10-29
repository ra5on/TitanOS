package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/docker"
)

// fakeTailnetRunner plays systemctl and systemd-run for the devices of
// the app proxy; tailscale itself never answers (the devices only start).
type fakeTailnetRunner struct{ calls []string }

func (f *fakeTailnetRunner) Run(_ context.Context, _, name string, args ...string) (string, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	return "", nil
}

func TestAppTailnetOneClick(t *testing.T) {
	ts := newTestServer(t)
	routes := appproxy.NewManager(ts.store)
	routes.Disabled = true
	routes.Hostname = func() string { return "nas" }
	routes.Reserved = []int{443}
	mount := t.TempDir()
	os.MkdirAll(filepath.Join(mount, "data"), 0o755)
	binary := filepath.Join(t.TempDir(), "tailscaled")
	os.WriteFile(binary, nil, 0o755)
	fake := &fakeTailnetRunner{}
	routes.Tailnet.Configure(fake, mount, t.TempDir(), binary)
	active := false
	routes.Tailnet.Volume = func() (string, bool) { return "data", active }
	ts.server.opts.AppProxy = routes

	for _, route := range []struct{ method, path string }{{"POST", "/tailnet"}, {"DELETE", "/tailnet"}, {"POST", "/tailnet/check"}} {
		if res, _ := ts.request(route.method, "/api/v1/docker/proxy/apps/immich"+route.path, map[string]any{}); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s %s: %d", route.method, route.path, res.StatusCode)
		}
	}
	ts.setup()
	code := func(body []byte) string {
		var e struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		json.Unmarshal(body, &e)
		return e.Error.Code
	}

	// The plugin is not active: explained, nothing stored.
	res, body := ts.request("POST", "/api/v1/docker/proxy/apps/immich/tailnet", map[string]any{"port": 2283, "tailscaleKey": "tskey-auth-abc"})
	if res.StatusCode != http.StatusConflict || !strings.Contains(code(body), "tailnet_unavailable") {
		t.Fatalf("plugin inactive: %d %s", res.StatusCode, body)
	}
	if routes.TailnetInfo().KeySet {
		t.Fatal("key stored without the plugin")
	}
	active = true

	// No key yet: the dialog asks for one.
	res, body = ts.request("POST", "/api/v1/docker/proxy/apps/immich/tailnet", map[string]any{"port": 2283})
	if res.StatusCode != http.StatusConflict || !strings.Contains(code(body), "tailnet_key_missing") {
		t.Fatalf("no key: %d %s", res.StatusCode, body)
	}
	// No port given and none known: refused.
	res, body = ts.request("POST", "/api/v1/docker/proxy/apps/immich/tailnet", map[string]any{"tailscaleKey": "tskey-auth-abc"})
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(code(body), "proxy_no_port") {
		t.Fatalf("no port: %d %s", res.StatusCode, body)
	}

	res, body = ts.request("POST", "/api/v1/docker/proxy/apps/Immich_X/tailnet", map[string]any{"port": 2283})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad app name: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("POST", "/api/v1/docker/proxy/apps/my_app/tailnet", map[string]any{"port": 2283})
	if res.StatusCode != http.StatusOK || strings.Contains(string(body), "tskey") {
		t.Fatalf("publish: %d %s", res.StatusCode, body)
	}
	var status appproxy.Status
	json.Unmarshal(body, &status)
	if len(status.Apps) != 1 || status.Apps[0].Name != "my-app" || !status.Apps[0].Tailscale || !status.Apps[0].Enabled || status.Apps[0].Tailnet == nil || !status.Tailnet.KeySet {
		t.Fatalf("status %s", body)
	}
	// The device only starts; asking it is too early.
	if res, body := ts.request("POST", "/api/v1/docker/proxy/apps/my_app/tailnet/check", nil); res.StatusCode != http.StatusConflict || !strings.Contains(code(body), "tailnet_not_ready") {
		t.Fatalf("check: %d %s", res.StatusCode, body)
	}

	res, body = ts.request("DELETE", "/api/v1/docker/proxy/apps/my_app/tailnet", nil)
	status = appproxy.Status{}
	json.Unmarshal(body, &status)
	if res.StatusCode != http.StatusOK || status.Apps[0].Tailscale || status.Apps[0].Enabled {
		t.Fatalf("remove: %d %s", res.StatusCode, body)
	}
}

func TestWebPort(t *testing.T) {
	containers := []docker.Container{
		{App: "other", WebUI: "9000"},
		{App: "immich", Ports: []docker.Port{{Host: 5353, Protocol: "udp"}, {Host: 5432, Protocol: "tcp"}}},
		{App: "immich", WebUI: "http://[host]:2283/photos"},
		{App: "plain", Ports: []docker.Port{{Host: 8080, Protocol: "tcp"}}},
		{App: "url", WebUI: "https://example.org"},
	}
	for app, want := range map[string]int{"immich": 2283, "plain": 8080, "url": 0, "missing": 0, "other": 9000} {
		if got := webPort(containers, app); got != want {
			t.Errorf("%s: %d, want %d", app, got, want)
		}
	}
}
