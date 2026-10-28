package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestWireGuardRoutes(t *testing.T) {
	ts := newTestServer(t)
	routes := []struct{ method, path string }{{"GET", "/network/wireguard"}, {"PUT", "/network/wireguard"}, {"POST", "/network/wireguard/peers"}, {"POST", "/network/wireguard/peers/remove"}}
	for _, route := range routes {
		if res, _ := ts.request(route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s %s: %d", route.method, route.path, res.StatusCode)
		}
	}
	ts.setup()
	ts.request("POST", "/api/v1/users", map[string]any{"name": "reader", "password": testPassword, "role": "user"})
	user := ts.newClient()
	ts.do(user, "POST", "/api/v1/login", credentials{Name: "reader", Password: testPassword})
	for _, route := range routes {
		if res, _ := ts.do(user, route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Fatalf("user %s %s: %d", route.method, route.path, res.StatusCode)
		}
	}
	if res, body := ts.request("POST", "/api/v1/network/wireguard/peers", map[string]any{"name": "Handy"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("device before switching on: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/network/wireguard", map[string]any{"enabled": true, "endpoint": "nas.example.org"}); res.StatusCode != 200 || !strings.Contains(string(body), `"network":"10.`) {
		t.Fatalf("switch on: %d %s", res.StatusCode, body)
	}
	res, body := ts.request("POST", "/api/v1/network/wireguard/peers", map[string]any{"name": "Handy"})
	if res.StatusCode != 200 || !strings.Contains(string(body), "PrivateKey = ") || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("add: %d %s", res.StatusCode, body)
	}
	if _, status := ts.request("GET", "/api/v1/network/wireguard", nil); strings.Contains(string(status), "PrivateKey") || strings.Contains(string(status), "presharedKey") {
		t.Fatalf("secrets in the status: %s", status)
	}
	if res, _ := ts.request("PUT", "/api/v1/network/wireguard", map[string]any{"enabled": true, "endpoint": "bad host"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad endpoint: %d", res.StatusCode)
	}
}
