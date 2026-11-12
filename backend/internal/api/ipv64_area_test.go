package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
	"github.com/ra5on/TitanOS/backend/internal/dnsprovider/ipv64fake"
)

func ipv64AreaServer(t *testing.T) (*testServer, *ipv64fake.Fake) {
	t.Helper()
	return ipv64AreaServerWith(t, nil)
}

// ipv64AreaServerWith is ipv64AreaServer with a last change to the options.
func ipv64AreaServerWith(t *testing.T, change func(*Options)) (*testServer, *ipv64fake.Fake) {
	t.Helper()
	fake := ipv64fake.New(t)
	ipv64Options = dnsprovider.Options{BaseURL: fake.Server.URL}
	t.Cleanup(func() { ipv64Options = dnsprovider.Options{} })
	saved := ipv64Wait
	ipv64Wait = time.Millisecond
	t.Cleanup(func() { ipv64Wait = saved })
	ts := newTestServerWith(t, change)
	ts.server.opts.DDNS.IPv64Options = ipv64Options
	ts.setup()
	return ts, fake
}

// Publishing is for owners and administrators at a web session; without a
// key nothing goes to IPv64, and the routes of the removed account area
// are gone.
func TestIPv64AppNeedsKeyRoleAndSession(t *testing.T) {
	ts, _ := ipv64AreaServer(t)
	if res, body := ts.request("GET", "/api/v1/ipv64/domains", nil); res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "ipv64_no_key") {
		t.Fatalf("no key: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/ipv64/apps/immich", nil); res.StatusCode != 200 || !strings.Contains(string(body), `"keySource":""`) {
		t.Fatalf("app without a key: %d %s", res.StatusCode, body)
	}
	// The assistant (or Home Assistant) acting as the owner may read, but
	// never publish.
	agent := func(method, path string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(`{"domain":"nas.ipv64.de","port":2283,"confirm":true}`)).WithContext(context.WithValue(context.Background(), inProcessIdentity{}, "alex"))
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := agent("PUT", "/api/v1/ipv64/apps/immich"); code != http.StatusForbidden {
		t.Fatalf("agent published: %d", code)
	}
	if code := agent("DELETE", "/api/v1/ipv64/apps/immich"); code != http.StatusForbidden {
		t.Fatalf("agent unpublished: %d", code)
	}
	if code := agent("GET", "/api/v1/ipv64/apps/immich"); code != http.StatusOK {
		t.Fatalf("agent read: %d", code)
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, route := range []struct{ method, path string }{{"GET", "/ipv64/domains"}, {"GET", "/ipv64/apps/x"}, {"PUT", "/ipv64/apps/x"}, {"DELETE", "/ipv64/apps/x"}} {
		if res, _ := ts.do(guest, route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Fatalf("user %s %s: %d", route.method, route.path, res.StatusCode)
		}
	}
	for _, route := range []struct{ method, path string }{{"GET", "/ipv64"}, {"GET", "/ipv64/me"}, {"PUT", "/ipv64/key"}, {"PUT", "/ipv64/blocklist"}, {"POST", "/ipv64/tunnel"}, {"GET", "/ipv64/assignments"}, {"PUT", "/ipv64/ddns"}, {"POST", "/ipv64/maintenance"}, {"GET", "/ipv64/healthchecks"}, {"GET", "/ipv64/failover"}, {"GET", "/ipv64/cdn"}, {"GET", "/ipv64/routers"}} {
		if res, _ := ts.request(route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusNotFound && res.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("removed route %s %s answers %d", route.method, route.path, res.StatusCode)
		}
	}
}

func TestIPv64AppPublish(t *testing.T) {
	ts, fake := ipv64AreaServer(t)
	fake.PendingServer = 2
	dyn := map[string]any{"hostnames": []string{"nas.ipv64.de"}, "ipv4": true, "provider": map[string]string{"type": "ipv64", "token": ipv64fake.Key}}
	if res, body := ts.request("PUT", "/api/v1/network/ddns", dyn); res.StatusCode != 200 {
		t.Fatalf("ddns: %d %s", res.StatusCode, body)
	}
	// Publishing an app.
	ts.server.ipv64.appPorts = func(app string) ([]int, bool) { return []int{2283, 443}, app == "immich" }
	if res, body := ts.request("GET", "/api/v1/ipv64/apps/immich", nil); res.StatusCode != 200 || !strings.Contains(string(body), `"ports":[2283]`) || !strings.Contains(string(body), `"keySource":"ddns"`) {
		t.Fatalf("app: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/ipv64/domains", nil); res.StatusCode != 200 || !strings.Contains(string(body), `"nas.ipv64.de"`) || strings.Contains(string(body), "update_token") {
		t.Fatalf("domains: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("PUT", "/api/v1/ipv64/apps/immich", map[string]any{"domain": "nas.ipv64.de", "port": 2283}); res.StatusCode != http.StatusBadRequest {
		t.Fatal("published without the confirmation")
	}
	if res, body := ts.request("PUT", "/api/v1/ipv64/apps/immich", map[string]any{"domain": "nas.ipv64.de", "port": 443, "confirm": true}); res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "ipv64_reserved_port") {
		t.Fatalf("web UI port: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("PUT", "/api/v1/ipv64/apps/immich", map[string]any{"domain": "nas.ipv64.de", "port": 9999, "confirm": true}); res.StatusCode != http.StatusBadRequest {
		t.Fatal("a port the app does not publish")
	}
	res, body := ts.request("PUT", "/api/v1/ipv64/apps/immich", map[string]any{"domain": "nas.ipv64.de", "prefix": "photos", "port": 2283, "firewall": true, "confirm": true})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("publish: %d %s", res.StatusCode, body)
	}
	if len(fake.CDNs) != 1 {
		t.Fatal("no CDN")
	}
	for _, cdn := range fake.CDNs {
		proxy := cdn["proxy"].(map[string]any)
		if proxy["state"] != "on" || proxy["frontend"] != "https_redirect" || proxy["backend"].(map[string]any)["port"] != float64(2283) || proxy["websocket"] != true || len(cdn["records"].([]any)) != 1 {
			t.Fatalf("CDN %+v", cdn)
		}
	}
	if !fake.Called("POST /domains/nas.ipv64.de/dyndns") {
		t.Fatal("record for the prefix not made")
	}
	if res, body := ts.request("GET", "/api/v1/ipv64/apps/immich", nil); !strings.Contains(string(body), `"domain":"nas.ipv64.de"`) || res.StatusCode != 200 {
		t.Fatalf("published app: %s", body)
	}
	// Another app on the same domain would need another backend port.
	ts.server.ipv64.appPorts = func(app string) ([]int, bool) { return []int{8096}, true }
	if res, body := ts.request("PUT", "/api/v1/ipv64/apps/jellyfin", map[string]any{"domain": "nas.ipv64.de", "port": 8096, "confirm": true}); res.StatusCode != http.StatusConflict {
		t.Fatalf("second backend port: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("DELETE", "/api/v1/ipv64/apps/immich", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("unpublish: %d %s", res.StatusCode, body)
	}
	if len(fake.CDNs) != 0 {
		t.Fatal("CDN made by TitanOS stayed")
	}
}
