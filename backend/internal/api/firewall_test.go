package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

func TestFirewallRoutes(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	res, body := ts.request("GET", "/api/v1/firewall", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status: %d %s", res.StatusCode, body)
	}
	var status struct {
		Config struct {
			Enabled bool `json:"enabled"`
			Zones   []struct {
				ID      string `json:"id"`
				Builtin bool   `json:"builtin"`
			} `json:"zones"`
			Cells []struct {
				From     string `json:"from"`
				To       string `json:"to"`
				Default  string `json:"default"`
				Policies []struct {
					Service string `json:"service"`
				} `json:"policies"`
			} `json:"cells"`
			AutoBlock struct {
				Enabled bool `json:"enabled"`
			} `json:"autoBlock"`
		} `json:"config"`
		Services []struct {
			ID string `json:"id"`
		} `json:"services"`
		Blocked  []any `json:"blocked"`
		Recent   []any `json:"recent"`
		Networks []any `json:"networks"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatal(err)
	}
	// Off by default, and so is the automatic blocking.
	if status.Config.Enabled || status.Config.AutoBlock.Enabled || status.Blocked == nil || status.Recent == nil {
		t.Errorf("defaults: %s", body)
	}
	var zones []string
	for _, z := range status.Config.Zones {
		if z.Builtin {
			zones = append(zones, z.ID)
		}
	}
	if !slices.Equal(zones, []string{"home", "vpn", "internet", "docker"}) || status.Networks == nil {
		t.Errorf("zones: %s", body)
	}
	allowed := func(from, service string) bool {
		for _, c := range status.Config.Cells {
			if c.From == from && c.To == "nas" {
				for _, p := range c.Policies {
					if p.Service == service {
						return true
					}
				}
			}
		}
		return false
	}
	if !allowed("home", "discovery") || allowed("vpn", "discovery") || allowed("internet", "webui") || !allowed("docker", "apps") || allowed("docker", "smb") {
		t.Errorf("default matrix: %s", body)
	}
	if !slices.ContainsFunc(status.Services, func(s struct {
		ID string `json:"id"`
	}) bool {
		return s.ID == "nfs"
	}) {
		t.Errorf("services: %s", body)
	}
	if res, body := ts.request("PUT", "/api/v1/firewall", map[string]any{"enabled": true}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "read_only" {
		t.Errorf("change in development mode: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/firewall/confirm", nil); res.StatusCode != http.StatusConflict || errorCode(t, body) != "firewall_not_pending" {
		t.Errorf("confirm without a change: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/firewall/unblock", map[string]string{"address": "203.0.113.1"}); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "firewall_not_blocked" {
		t.Errorf("unblock: %d %s", res.StatusCode, body)
	}

	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, tc := range [][2]string{{"GET", "/api/v1/firewall"}, {"PUT", "/api/v1/firewall"}, {"POST", "/api/v1/firewall/confirm"}, {"POST", "/api/v1/firewall/revert"}, {"PUT", "/api/v1/firewall/autoblock"}, {"POST", "/api/v1/firewall/unblock"}, {"DELETE", "/api/v1/firewall/recent"}} {
		if res, _ := ts.do(guest, tc[0], tc[1], map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s as a plain user: %d, want 403", tc[0], tc[1], res.StatusCode)
		}
	}
	if scope := publicScope("GET", "/api/v1/firewall"); scope != "system:read" {
		t.Errorf("public scope: %q", scope)
	}
	for _, method := range []string{"PUT", "POST"} {
		for _, path := range []string{"/api/v1/firewall", "/api/v1/firewall/confirm", "/api/v1/firewall/autoblock", "/api/v1/firewall/unblock"} {
			if scope := publicScope(method, path); scope != "" {
				t.Errorf("%s %s open to tokens: %s", method, path, scope)
			}
		}
	}
}
