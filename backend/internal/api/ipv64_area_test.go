package api

import (
	"context"
	"encoding/json"
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
	fake := ipv64fake.New(t)
	ipv64Options = dnsprovider.Options{BaseURL: fake.Server.URL}
	t.Cleanup(func() { ipv64Options = dnsprovider.Options{} })
	saved := ipv64Wait
	ipv64Wait = time.Millisecond
	t.Cleanup(func() { ipv64Wait = saved })
	ts := newTestServer(t)
	ts.server.opts.DDNS.IPv64Options = ipv64Options
	ts.setup()
	return ts, fake
}

func decodeBody(t *testing.T, body []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
}

func TestIPv64AreaNeedsKeyRoleAndSession(t *testing.T) {
	ts, _ := ipv64AreaServer(t)
	if res, body := ts.request("GET", "/api/v1/ipv64/me", nil); res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "ipv64_no_key") {
		t.Fatalf("no key: %d %s", res.StatusCode, body)
	}
	// The own key of the area is checked against IPv64 before it is kept.
	if res, _ := ts.request("PUT", "/api/v1/ipv64/key", map[string]string{"apiKey": "wrong"}); res.StatusCode != http.StatusBadGateway {
		t.Fatalf("wrong key stored: %d", res.StatusCode)
	}
	res, body := ts.request("PUT", "/api/v1/ipv64/key", map[string]string{"apiKey": ipv64fake.Key})
	if res.StatusCode != 200 || strings.Contains(string(body), ipv64fake.Key) || !strings.Contains(string(body), `"keySet":true`) {
		t.Fatalf("key: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("GET", "/api/v1/ipv64/me", nil)
	if res.StatusCode != 200 || strings.Contains(string(body), "owner@example.org") || !strings.Contains(string(body), `"keySource":"own"`) {
		t.Fatalf("me: %d %s", res.StatusCode, body)
	}
	// The assistant (or Home Assistant) acting as the owner may read, but
	// never change anything at IPv64.
	agent := func(method, path string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(`{"name":"x","expiresIn":60}`)).WithContext(context.WithValue(context.Background(), agentIdentity{}, "alex"))
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := agent("POST", "/api/v1/ipv64/api-keys"); code != http.StatusForbidden {
		t.Fatalf("agent created a key: %d", code)
	}
	if code := agent("GET", "/api/v1/ipv64/me"); code != http.StatusOK {
		t.Fatalf("agent read: %d", code)
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, route := range []struct{ method, path string }{{"GET", "/ipv64"}, {"GET", "/ipv64/me"}, {"POST", "/ipv64/api-keys"}, {"PUT", "/ipv64/apps/x"}, {"POST", "/ipv64/tunnel"}} {
		if res, _ := ts.do(guest, route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Fatalf("user %s %s: %d", route.method, route.path, res.StatusCode)
		}
	}
}

func TestIPv64AreaKeysDomainsChecks(t *testing.T) {
	ts, fake := ipv64AreaServer(t)
	dyn := map[string]any{"hostnames": []string{"nas.ipv64.de"}, "ipv4": true, "provider": map[string]string{"type": "ipv64", "token": ipv64fake.Key, "updateToken": "update-nas.ipv64.de"}}
	if res, body := ts.request("PUT", "/api/v1/network/ddns", dyn); res.StatusCode != 200 {
		t.Fatalf("ddns: %d %s", res.StatusCode, body)
	}
	// Keys: the one of dynamic DNS is marked and cannot be deleted here.
	res, body := ts.request("GET", "/api/v1/ipv64/api-keys", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"usedBy":["ddns"]`) {
		t.Fatalf("keys: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("DELETE", "/api/v1/ipv64/api-keys/1", nil); res.StatusCode != http.StatusConflict || !strings.Contains(string(body), "TitanOS uses this key") {
		t.Fatalf("delete used key: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("DELETE", "/api/v1/ipv64/api-keys/2", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete other key: %d", res.StatusCode)
	}
	res, body = ts.request("POST", "/api/v1/ipv64/api-keys", map[string]any{"name": "phone", "expiresIn": 86400})
	if res.StatusCode != http.StatusCreated || !strings.Contains(string(body), "brand-new-full-key") {
		t.Fatalf("create key: %d %s", res.StatusCode, body)
	}
	if data, _ := ts.store.Read("ipv64/settings.json"); strings.Contains(string(data), "brand-new-full-key") {
		t.Fatal("a created key was stored")
	}
	if res, body := ts.request("GET", "/api/v1/ipv64/logs?before=20", nil); res.StatusCode != 200 || !strings.Contains(string(body), `"next_before":null`) {
		t.Fatalf("logs: %d %s", res.StatusCode, body)
	}

	// Domains: the update token never goes out; rotating it follows in DDNS.
	res, body = ts.request("GET", "/api/v1/ipv64/domains/nas.ipv64.de", nil)
	if res.StatusCode != 200 || strings.Contains(string(body), "update-nas") {
		t.Fatalf("domain: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("POST", "/api/v1/ipv64/domains/nas.ipv64.de/rotate-token", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"ddnsUpdated":true`) || strings.Contains(string(body), "rotated-") {
		t.Fatalf("rotate: %d %s", res.StatusCode, body)
	}
	if data, _ := ts.store.Read("ddns/settings.json"); !strings.Contains(string(data), "rotated-nas.ipv64.de") {
		t.Fatal("DDNS kept the old update token")
	}
	if res, _ := ts.request("PATCH", "/api/v1/ipv64/domains/nas.ipv64.de", map[string]any{"ipv6_prefix": "10.0.0.0/8"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad prefix: %d", res.StatusCode)
	}
	if res, _ := ts.request("PATCH", "/api/v1/ipv64/domains/nas.ipv64.de", map[string]any{"wildcard": true, "ignore": "ipv6"}); res.StatusCode != 200 {
		t.Fatalf("patch: %d", res.StatusCode)
	}
	if res, body := ts.request("POST", "/api/v1/ipv64/domains/nas.ipv64.de/records", map[string]string{"type": "TXT", "prefix": "x", "content": "hello"}); res.StatusCode != http.StatusCreated {
		t.Fatalf("record: %d %s", res.StatusCode, body)
	}

	// Healthchecks of every type; IDs instead of tokens.
	res, body = ts.request("POST", "/api/v1/ipv64/healthchecks", map[string]any{"name": "web", "type": "http", "url": "https://nas.example.org", "interval_seconds": 120})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create check: %d %s", res.StatusCode, body)
	}
	var check struct {
		ID string `json:"id"`
	}
	decodeBody(t, body, &check)
	for token := range fake.Checks {
		if strings.Contains(string(body), token) {
			t.Fatal("token of a check in the answer")
		}
	}
	if res, body := ts.request("POST", "/api/v1/ipv64/healthchecks", map[string]any{"name": "x", "type": "tcp", "target": "a.example"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("tcp without port: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("PATCH", "/api/v1/ipv64/healthchecks/"+check.ID, map[string]any{"name": "web2"}); res.StatusCode != 200 {
		t.Fatalf("patch check: %d", res.StatusCode)
	}
	if res, body := ts.request("GET", "/api/v1/ipv64/healthchecks/"+check.ID+"/stats?range=7d", nil); res.StatusCode != 200 || !strings.Contains(string(body), `"range":"7d"`) {
		t.Fatalf("stats: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("POST", "/api/v1/ipv64/healthchecks/"+check.ID+"/test-notification", nil); res.StatusCode != 200 {
		t.Fatalf("test: %d", res.StatusCode)
	}
	if res, _ := ts.request("POST", "/api/v1/ipv64/healthchecks/"+check.ID+"/test-notification", nil); res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second test within 30 s: %d", res.StatusCode)
	}

	// The check of "watch reachability": rotating its token keeps the NAS pinging.
	if res, body := ts.request("POST", "/api/v1/network/ipv64/heartbeat/on", map[string]any{"name": "titanos-test", "periodMinutes": 30}); res.StatusCode != 200 {
		t.Fatalf("heartbeat: %d %s", res.StatusCode, body)
	}
	old := ts.server.opts.DDNS.HeartbeatToken()
	nas := dnsprovider.HealthcheckID(old)
	res, body = ts.request("GET", "/api/v1/ipv64/healthchecks", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"nasCheck":"`+nas+`"`) {
		t.Fatalf("list: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("POST", "/api/v1/ipv64/healthchecks/"+nas+"/rotate-token", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"nasUpdated":true`) {
		t.Fatalf("rotate check: %d %s", res.StatusCode, body)
	}
	if fresh := ts.server.opts.DDNS.HeartbeatToken(); fresh == old || fresh == "" {
		t.Fatal("heartbeat token not followed")
	}

	// Integrations are created write-only and listed without secrets.
	res, body = ts.request("POST", "/api/v1/ipv64/integrations", map[string]any{"type": "telegram", "name": "phone", "chat_id": "42", "api_token": "bot-secret"})
	if res.StatusCode != http.StatusCreated || strings.Contains(string(body), "bot-secret") || strings.Contains(string(body), "1234…abcd") {
		t.Fatalf("integration: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/ipv64/integrations", map[string]any{"type": "carrier-pigeon", "name": "x"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad type: %d %s", res.StatusCode, body)
	}
	// Status pages take IDs of checks and keep the password write-only.
	res, body = ts.request("POST", "/api/v1/ipv64/status-pages", map[string]any{"name": "NAS", "slug": "meinnas", "password": "geheim", "design": 3, "healthchecks": []string{check.ID}})
	if res.StatusCode != http.StatusCreated || strings.Contains(string(body), "geheim") || !strings.Contains(string(body), check.ID) {
		t.Fatalf("status page: %d %s", res.StatusCode, body)
	}
}

func TestIPv64AreaMaintenanceAndPublish(t *testing.T) {
	ts, fake := ipv64AreaServer(t)
	fake.PendingServer = 2
	dyn := map[string]any{"hostnames": []string{"nas.ipv64.de"}, "ipv4": true, "provider": map[string]string{"type": "ipv64", "token": ipv64fake.Key}}
	if res, body := ts.request("PUT", "/api/v1/network/ddns", dyn); res.StatusCode != 200 {
		t.Fatalf("ddns: %d %s", res.StatusCode, body)
	}
	// One click: heartbeat check, offline page, policy, monitor and rule.
	res, body := ts.request("POST", "/api/v1/ipv64/maintenance", map[string]string{"domain": "nas.ipv64.de"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("maintenance: %d %s", res.StatusCode, body)
	}
	var entry struct {
		ID, Policy, OfflinePage string
		CreatedCheck            bool `json:"createdCheck"`
	}
	decodeBody(t, body, &entry)
	if !entry.CreatedCheck || ts.server.opts.DDNS.HeartbeatToken() == "" || len(fake.Offline) != 1 || len(fake.Policies) != 1 {
		t.Fatalf("not all made: %+v", entry)
	}
	policy := fake.Policies[entry.Policy]
	if len(policy["monitors"].([]any)) != 1 || len(policy["rules"].([]any)) != 1 || policy["mode"] != "auto_failover_and_fallback" {
		t.Fatalf("policy %+v", policy)
	}
	if res, _ := ts.request("POST", "/api/v1/ipv64/maintenance", map[string]string{"domain": "nas.ipv64.de"}); res.StatusCode != http.StatusConflict {
		t.Fatal("second maintenance page for the same domain")
	}
	if res, body := ts.request("DELETE", "/api/v1/ipv64/maintenance/"+entry.ID, map[string]bool{"deleteCheck": true}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("remove: %d %s", res.StatusCode, body)
	}
	if len(fake.Offline) != 0 || len(fake.Policies) != 0 || len(fake.Checks) != 0 || ts.server.opts.DDNS.HeartbeatToken() != "" {
		t.Fatal("something stayed after removing")
	}

	// Publishing an app.
	ts.server.ipv64.appPorts = func(app string) ([]int, bool) { return []int{2283, 443}, app == "immich" }
	if res, body := ts.request("GET", "/api/v1/ipv64/apps/immich", nil); res.StatusCode != 200 || !strings.Contains(string(body), `"ports":[2283]`) {
		t.Fatalf("app: %d %s", res.StatusCode, body)
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
	res, body = ts.request("PUT", "/api/v1/ipv64/apps/immich", map[string]any{"domain": "nas.ipv64.de", "prefix": "photos", "port": 2283, "firewall": true, "confirm": true})
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

func TestIPv64AreaRouterAndCDN(t *testing.T) {
	ts, fake := ipv64AreaServer(t)
	ts.request("PUT", "/api/v1/ipv64/key", map[string]string{"apiKey": ipv64fake.Key})
	res, body := ts.request("POST", "/api/v1/ipv64/routers", map[string]string{"name": "home"})
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("router: %d %s", res.StatusCode, body)
	}
	var router dnsprovider.IPv64Router
	decodeBody(t, body, &router)
	gw := router.Gateways[0].ID
	for _, call := range []struct {
		method, path string
		body         any
		want         int
	}{
		{"GET", "/ipv64/routers/" + router.ID, nil, 200},
		{"POST", "/ipv64/routers/" + router.ID + "/routes", map[string]any{"destination": "192.168.50.0/24", "gateway": "10.64.0.5", "peer": "pe1"}, 202},
		{"POST", "/ipv64/routers/" + router.ID + "/firewall", map[string]any{"chain": "forward", "action": "drop", "source": "10.0.0.0/8"}, 202},
		{"POST", "/ipv64/routers/" + router.ID + "/firewall", map[string]any{"chain": "sideways", "action": "drop", "source": "10.0.0.0/8"}, 400},
		{"POST", "/ipv64/routers/" + router.ID + "/public-ips", nil, 202},
		{"PATCH", "/ipv64/routers/" + router.ID + "/routes/1", map[string]any{"disabled": true}, 202},
		{"DELETE", "/ipv64/routers/" + router.ID + "/firewall/2", nil, 202},
		{"PATCH", "/ipv64/gateways/" + gw, map[string]any{"name": "main"}, 200},
		{"POST", "/ipv64/gateways/" + gw + "/do/rotate-keys", nil, 202},
		{"POST", "/ipv64/gateways/" + gw + "/peers", map[string]any{"name": "laptop"}, 202},
		{"POST", "/ipv64/cdn", map[string]any{"domain": "nas.ipv64.de"}, 201},
	} {
		if res, body := ts.request(call.method, "/api/v1"+call.path, call.body); res.StatusCode != call.want {
			t.Fatalf("%s %s: %d %s", call.method, call.path, res.StatusCode, body)
		}
	}
	var cdn string
	for id := range fake.CDNs {
		cdn = id
	}
	for _, call := range []struct {
		method, path string
		body         any
		want         int
	}{
		{"GET", "/ipv64/cdn/" + cdn, nil, 200},
		{"PATCH", "/ipv64/cdn/" + cdn, map[string]any{"backend_port": 443}, 400},
		{"PATCH", "/ipv64/cdn/" + cdn, map[string]any{"caching": map[string]any{"enabled": true, "time_minutes": 30}, "access_list": map[string]any{"enabled": true, "mode": "denylist"}}, 200},
		{"POST", "/ipv64/cdn/" + cdn + "/access-list", map[string]any{"address": "198.51.100.0/24"}, 201},
		{"POST", "/ipv64/cdn/" + cdn + "/mappings", map[string]any{"record": "rec1", "target_port": 25565, "transport": "tcp", "listen_port": 50500}, 201},
		{"POST", "/ipv64/cdn/" + cdn + "/mappings", map[string]any{"record": "rec1", "target_port": 443}, 400},
		{"POST", "/ipv64/cdn/" + cdn + "/action", map[string]any{"action": "renew-certificate"}, 202},
		{"POST", "/ipv64/cdn/" + cdn + "/action", map[string]any{"action": "explode"}, 400},
	} {
		if res, body := ts.request(call.method, "/api/v1"+call.path, call.body); res.StatusCode != call.want {
			t.Fatalf("%s %s: %d %s", call.method, call.path, res.StatusCode, body)
		}
	}
	// The tunnel is not set up in development mode.
	if res, _ := ts.request("POST", "/api/v1/ipv64/tunnel", map[string]string{"router": router.ID, "gateway": gw, "name": "nas"}); res.StatusCode != http.StatusConflict {
		t.Fatalf("tunnel in development mode: %d", res.StatusCode)
	}
	if res, body := ts.request("GET", "/api/v1/ipv64/tunnel", nil); res.StatusCode != 200 || !strings.Contains(string(body), `"readOnly":true`) {
		t.Fatalf("tunnel status: %d %s", res.StatusCode, body)
	}
}
