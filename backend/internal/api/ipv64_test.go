package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
)

// fakeIPv64API answers the few calls of API v2 these routes make.
func fakeIPv64API(t *testing.T) (*httptest.Server, *[]string) {
	var mu sync.Mutex
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer acct-key" {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(map[string]any{"status": 401, "code": "invalid_token", "detail": "The API key is invalid."})
			return
		}
		data := func(status int, v any) {
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]any{"data": v})
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v2/me":
			data(200, map[string]any{"email": "me@example.org", "plan": map[string]any{"name": "Free"}, "usage": map[string]any{"dyndns_updates_24h": map[string]any{"used": 2, "limit": 48}}})
		case "GET /api/v2/domains":
			data(200, []map[string]any{{"id": 1, "name": "nas.ipv64.de", "kind": "dyndns", "update_token": "domain-secret"}})
		case "GET /api/v2/healthchecks":
			data(200, []map[string]any{})
		case "GET /api/v2/integrations":
			data(200, []map[string]any{{"id": 0, "type": "account_email", "name": "E-Mail"}})
		case "POST /api/v2/healthchecks":
			data(201, map[string]any{"token": "check-secret", "name": "titanos-test", "type": "push", "push": map[string]any{"period_seconds": 1800, "grace_seconds": 600}})
		case "POST /api/v2/healthchecks/check-secret/test-notification":
			data(200, map[string]bool{"sent": true})
		case "DELETE /api/v2/healthchecks/check-secret":
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func TestIPv64Routes(t *testing.T) {
	server, calls := fakeIPv64API(t)
	ipv64Options = dnsprovider.Options{BaseURL: server.URL}
	defer func() { ipv64Options = dnsprovider.Options{} }()
	ts := newTestServer(t)
	ts.server.opts.DDNS.IPv64Options = ipv64Options
	ts.setup()

	if res, body := ts.request("POST", "/api/v1/network/ipv64/account", map[string]string{}); res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "ipv64_no_key") {
		t.Fatalf("no key: %d %s", res.StatusCode, body)
	}
	res, body := ts.request("POST", "/api/v1/network/ipv64/account", map[string]string{"apiKey": "acct-key"})
	if res.StatusCode != 200 || !strings.Contains(string(body), `"plan":"Free"`) || strings.Contains(string(body), "me@example.org") {
		t.Fatalf("account: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/network/ipv64/account", map[string]string{"apiKey": "wrong"}); res.StatusCode != http.StatusBadGateway || !strings.Contains(string(body), "ipv64_unauthorized") {
		t.Fatalf("wrong key: %d %s", res.StatusCode, body)
	}

	// Once stored for dynamic DNS, the key is taken from there.
	dyn := map[string]any{"hostnames": []string{"nas.ipv64.de"}, "ipv4": true, "provider": map[string]string{"type": "ipv64", "token": "acct-key"}}
	if res, body := ts.request("PUT", "/api/v1/network/ddns", dyn); res.StatusCode != 200 || strings.Contains(string(body), "acct-key") {
		t.Fatalf("ddns: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("POST", "/api/v1/network/ipv64/domains", map[string]string{})
	if res.StatusCode != 200 || !strings.Contains(string(body), "nas.ipv64.de") || strings.Contains(string(body), "domain-secret") || !strings.Contains(string(body), "home64.de") {
		t.Fatalf("domains: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("POST", "/api/v1/network/ipv64/healthchecks", map[string]string{}); res.StatusCode != 200 {
		t.Fatalf("healthchecks: %d", res.StatusCode)
	}
	before := len(*calls)
	res, body = ts.request("POST", "/api/v1/network/ipv64/heartbeat/on", map[string]any{"name": "titanos-test", "periodMinutes": 30})
	if res.StatusCode != 200 || strings.Contains(string(body), "check-secret") || !strings.Contains(string(body), `"heartbeat":true`) {
		t.Fatalf("on: %d %s", res.StatusCode, body)
	}
	if !strings.Contains(strings.Join((*calls)[before:], ","), "POST /api/v2/healthchecks") {
		t.Fatal("no healthcheck created", *calls)
	}
	if res, _ := ts.request("POST", "/api/v1/network/ipv64/heartbeat/test", map[string]string{}); res.StatusCode != 200 {
		t.Fatalf("test: %d", res.StatusCode)
	}
	if res, _ := ts.request("POST", "/api/v1/network/ipv64/heartbeat/test", map[string]string{}); res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second test within 30 s: %d", res.StatusCode)
	}
	res, body = ts.request("POST", "/api/v1/network/ipv64/heartbeat/off", map[string]string{"action": "delete"})
	if res.StatusCode != 200 || !strings.Contains(string(body), `"heartbeat":false`) {
		t.Fatalf("off: %d %s", res.StatusCode, body)
	}
	if ts.server.opts.DDNS.HeartbeatToken() != "" {
		t.Fatal("token kept after deleting the check")
	}

	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	if res, _ := ts.do(guest, "POST", "/api/v1/network/ipv64/domains", map[string]string{}); res.StatusCode != http.StatusForbidden {
		t.Fatalf("user: %d", res.StatusCode)
	}
}
