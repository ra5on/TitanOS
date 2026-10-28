package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/ddns"
	"github.com/ra5on/TitanOS/backend/internal/letsencrypt"
)

func disabledLetsEncrypt(store *config.Store) *letsencrypt.Manager {
	m := letsencrypt.New(store)
	m.Disabled = true
	m.Load()
	return m
}

func disabledDDNS(store *config.Store) *ddns.Manager {
	m := ddns.New(store)
	m.Disabled = true
	m.Load()
	return m
}

func TestLetsEncryptAndDDNSKeepTokens(t *testing.T) {
	ts := newTestServer(t)
	routes := []struct{ method, path string }{
		{"GET", "/system/letsencrypt"}, {"PUT", "/system/letsencrypt"}, {"POST", "/system/letsencrypt/renew"},
		{"GET", "/network/ddns"}, {"PUT", "/network/ddns"}, {"POST", "/network/ddns/update"},
	}
	for _, route := range routes {
		if res, _ := ts.request(route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s %s: %d", route.method, route.path, res.StatusCode)
		}
	}
	ts.setup()
	acme := map[string]any{
		"enabled": true, "domains": []string{"nas.example.org"}, "challenge": "dns-01",
		"provider": map[string]string{"type": "cloudflare", "token": "very-secret-token"},
	}
	res, body := ts.request("PUT", "/api/v1/system/letsencrypt", acme)
	if res.StatusCode != http.StatusOK || strings.Contains(string(body), "very-secret-token") || !strings.Contains(string(body), `"token":true`) {
		t.Fatalf("put: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/system/letsencrypt", nil); res.StatusCode != 200 || strings.Contains(string(body), "very-secret-token") {
		t.Fatalf("get: %d %s", res.StatusCode, body)
	}
	// The page learns whether the firewall keeps port 80 from the internet
	// (HTTP-01); with the firewall off nothing is in the way.
	if _, body := ts.request("GET", "/api/v1/system/letsencrypt", nil); !strings.Contains(string(body), `"firewall":false`) || !strings.Contains(string(body), `"internetHttp":`) {
		t.Fatalf("firewall fields: %s", body)
	}
	acme["domains"] = []string{"*.example.org"}
	acme["challenge"] = "http-01"
	if res, body := ts.request("PUT", "/api/v1/system/letsencrypt", acme); res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "invalid_letsencrypt") {
		t.Fatalf("wildcard over HTTP: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("POST", "/api/v1/system/letsencrypt/renew", nil); res.StatusCode != http.StatusAccepted {
		t.Fatalf("renew: %d", res.StatusCode)
	}

	dyn := map[string]any{
		"enabled": true, "hostnames": []string{"nas.example.org"}, "ipv4": true, "ipv6": true,
		"provider": map[string]string{"type": "dyndns2", "server": "https://dyn.example/nic/update", "user": "me", "password": "pw-secret"},
	}
	res, body = ts.request("PUT", "/api/v1/network/ddns", dyn)
	if res.StatusCode != http.StatusOK || strings.Contains(string(body), "pw-secret") || !strings.Contains(string(body), `"password":true`) {
		t.Fatalf("ddns put: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("POST", "/api/v1/network/ddns/update", nil); res.StatusCode != http.StatusAccepted {
		t.Fatalf("update now: %d", res.StatusCode)
	}
	dyn["interval"] = 1
	if res, _ := ts.request("PUT", "/api/v1/network/ddns", dyn); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad interval: %d", res.StatusCode)
	}

	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, route := range routes {
		if res, _ := ts.do(guest, route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Fatalf("user %s %s: %d", route.method, route.path, res.StatusCode)
		}
	}
}
