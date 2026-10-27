package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
)

func TestAppProxyRoutes(t *testing.T) {
	ts := newTestServer(t)
	routes := appproxy.NewManager(ts.store)
	routes.Disabled = true
	routes.Hostname = func() string { return "nas" }
	routes.Reserved = []int{443}
	ts.server.opts.AppProxy = routes

	for _, route := range []struct{ method, path string }{{"GET", "/docker/proxy"}, {"PUT", "/docker/proxy"}, {"PUT", "/docker/proxy/apps/immich"}, {"DELETE", "/docker/proxy/apps/immich"}} {
		if res, _ := ts.request(route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s %s: %d", route.method, route.path, res.StatusCode)
		}
	}
	ts.setup()

	res, body := ts.request("PUT", "/api/v1/docker/proxy/apps/immich", map[string]any{"enabled": true, "name": "immich", "port": 2283})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("set route: %d %s", res.StatusCode, body)
	}
	var info struct {
		Suffixes []string           `json:"suffixes"`
		Apps     []appproxy.AppInfo `json:"apps"`
	}
	json.Unmarshal(body, &info)
	if len(info.Apps) != 1 || info.Apps[0].App != "immich" || info.Apps[0].URLs[0] != "https://immich.nas.local" || info.Suffixes[0] != "nas.local" {
		t.Fatalf("answer %s", body)
	}
	if res, body := ts.request("PUT", "/api/v1/docker/proxy/apps/other", map[string]any{"enabled": true, "name": "immich", "port": 2284}); res.StatusCode != http.StatusConflict {
		t.Fatalf("same name: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/docker/proxy/apps/other", map[string]any{"enabled": true, "name": "x", "port": 443}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("port of the web UI: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/docker/proxy", map[string]any{"domain": "home.example.org"}); res.StatusCode != http.StatusOK {
		t.Fatalf("domain: %d %s", res.StatusCode, body)
	}
	if routes.URL("immich") != "https://immich.home.example.org" {
		t.Fatalf("URL %q", routes.URL("immich"))
	}
	if res, _ := ts.request("DELETE", "/api/v1/docker/proxy/apps/immich", nil); res.StatusCode != http.StatusOK || routes.URL("immich") != "" {
		t.Fatalf("remove: %d", res.StatusCode)
	}
}

func TestProxyURLsOnContainers(t *testing.T) {
	routes := appproxy.NewManager(config.Open(t.TempDir()))
	routes.Hostname = func() string { return "nas" }
	routes.SetRoute("photos", appproxy.Route{Enabled: true, Name: "photos", Port: 2283})
	routes.SetRoute("hostnet", appproxy.Route{Enabled: true, Name: "hostnet", Port: 8096})
	routes.SetRoute("off", appproxy.Route{Enabled: false, Name: "off", Port: 9000})
	containers := []docker.Container{
		{Name: "photos-web", App: "photos", Ports: []docker.Port{{Host: 2283, Container: 3001}}},
		{Name: "photos-db", App: "photos", Ports: []docker.Port{{Host: 5432, Container: 5432}}},
		{Name: "hostnet-1", App: "hostnet"},
		{Name: "off-1", App: "off", Ports: []docker.Port{{Host: 9000}}},
		{Name: "loose"},
	}
	addProxyURLs(containers, routes)
	want := []string{"https://photos.nas.local", "", "https://hostnet.nas.local", "", ""}
	for i, c := range containers {
		if c.ProxyURL != want[i] {
			t.Errorf("%s: %q, want %q", c.Name, c.ProxyURL, want[i])
		}
	}
}
