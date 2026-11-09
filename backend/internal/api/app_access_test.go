package api

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/appaccess"
	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/tlscert"
)

func TestAppAccessRoutes(t *testing.T) {
	ts := newTestServer(t)
	for _, name := range []string{"immich", "jellyfin"} {
		if err := ts.store.Write("apps/"+name+"/compose.yaml", []byte("services: {}\n")); err != nil {
			t.Fatal(err)
		}
	}
	routes := appproxy.NewManager(ts.store)
	routes.Disabled = true
	routes.Hostname = func() string { return "nas" }
	routes.Reserved = []int{443}
	ts.server.opts.AppProxy = routes
	rights := appaccess.New(ts.store)
	rights.Lookup = func(name string) (appaccess.Account, bool) {
		u, ok := ts.server.opts.Users.Get(name)
		return appaccess.Account{Name: u.Name, Admin: u.Role.CanAdminister(), Disabled: u.Disabled}, ok
	}
	rights.SessionUsable = ts.server.sessions.Usable
	ts.server.opts.AppAccess = rights
	routes.Access = rights

	for _, route := range []struct{ method, path string }{{"GET", "/apps/access"}, {"PUT", "/apps/access"}, {"GET", "/apps/mine"}, {"POST", "/apps/immich/window"}, {"GET", "/certs/ca"}, {"GET", "/certs/ca.crt"}} {
		if res, _ := ts.request(route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s %s: %d", route.method, route.path, res.StatusCode)
		}
	}
	ts.setup()
	if res, body := ts.request("POST", "/api/v1/users", map[string]any{"name": "anna", "password": testPassword, "role": "user"}); res.StatusCode != http.StatusCreated {
		t.Fatalf("user: %d %s", res.StatusCode, body)
	}
	anna := ts.newClient()
	ts.do(anna, "POST", "/api/v1/login", credentials{Name: "anna", Password: testPassword})
	if res, _ := ts.do(anna, "GET", "/api/v1/apps/access", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("a normal account sees the rights: %d", res.StatusCode)
	}
	if res, _ := ts.do(anna, "PUT", "/api/v1/apps/access", map[string]any{}); res.StatusCode != http.StatusForbidden {
		t.Fatalf("a normal account changes the rights: %d", res.StatusCode)
	}

	var view appAccessView
	res, body := ts.request("GET", "/api/v1/apps/access", nil)
	json.Unmarshal(body, &view)
	if res.StatusCode != http.StatusOK || view.NewApps != "admins" || len(view.Apps) != 2 || len(view.Accounts) != 2 || !view.Apps[0].Gate || len(view.Apps[0].Users) != 0 {
		t.Fatalf("access: %d %s", res.StatusCode, body)
	}

	// Without a tick anna may not, the owner may.
	if res, body := ts.do(anna, "POST", "/api/v1/apps/immich/window", nil); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "app_forbidden" {
		t.Fatalf("window without right: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/apps/immich/window", nil); res.StatusCode != http.StatusConflict || errorCode(t, body) != "proxy_no_route" {
		t.Fatalf("window without a name: %d %s", res.StatusCode, body)
	}
	routes.SetAutoApps(map[string]appproxy.AutoApp{"immich": {Port: 2283}, "jellyfin": {Port: 8096}})

	res, body = ts.request("PUT", "/api/v1/apps/access", map[string]any{"newApps": "admins", "apps": []map[string]any{
		{"app": "immich", "gate": true, "users": []string{"anna", "alex", "ghost"}},
		{"app": "jellyfin", "gate": false, "users": []string{}},
	}})
	json.Unmarshal(body, &view)
	if res.StatusCode != http.StatusOK || len(view.Apps) != 2 || strings.Join(view.Apps[0].Users, ",") != "anna" || view.Apps[1].Gate || view.Apps[0].URL != "https://immich.nas.local" {
		t.Fatalf("set: %d %s", res.StatusCode, body)
	}

	var mine struct {
		Apps []dockerApp `json:"apps"`
	}
	_, body = ts.do(anna, "GET", "/api/v1/apps/mine", nil)
	json.Unmarshal(body, &mine)
	if len(mine.Apps) != 1 || mine.Apps[0].App != "immich" || mine.Apps[0].URL != "https://immich.nas.local" {
		t.Fatalf("mine: %s", body)
	}

	var window struct {
		URL     string `json:"url"`
		Expires string `json:"expires"`
	}
	res, body = ts.do(anna, "POST", "/api/v1/apps/immich/window", nil)
	json.Unmarshal(body, &window)
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(window.URL, "https://immich.nas.local/.titanos/enter?t=") || window.Expires == "" {
		t.Fatalf("window: %d %s", res.StatusCode, body)
	}
	u, _ := url.Parse(window.URL)
	if _, _, ok := rights.Enter(u.Query().Get("t"), "immich"); !ok {
		t.Error("the token of the window does not let in")
	}

	// The link of the gate page: logged in, it sends on with a fresh token.
	client := ts.newClient()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	ts.do(client, "POST", "/api/v1/login", credentials{Name: "anna", Password: testPassword})
	res, _ = ts.do(client, "GET", "/api/v1/apps/immich/open?host=immich.nas.local", nil)
	if res.StatusCode != http.StatusSeeOther || !strings.HasPrefix(res.Header.Get("Location"), "https://immich.nas.local/.titanos/enter?t=") {
		t.Fatalf("open: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	anonymous := ts.newClient()
	anonymous.CheckRedirect = client.CheckRedirect
	if res, _ := ts.do(anonymous, "GET", "/api/v1/apps/immich/open", nil); res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/" {
		t.Fatalf("open without login: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	if res, _ := ts.do(client, "GET", "/api/v1/apps/jellyfin/open", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("open without right: %d", res.StatusCode)
	}
}

func TestCADownload(t *testing.T) {
	ts := newTestServer(t)
	certs, err := tlscert.NewManager(ts.store, "nas")
	if certs == nil {
		t.Fatal(err)
	}
	ts.server.opts.Certificate = certs
	ts.setup()
	res, body := ts.request("GET", "/api/v1/certs/ca.crt", nil)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "application/x-x509-ca-cert" || !strings.Contains(res.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("ca.crt: %d %v", res.StatusCode, res.Header)
	}
	block, _ := pem.Decode(body)
	if block == nil || block.Type != "CERTIFICATE" || strings.Contains(string(body), "PRIVATE") {
		t.Fatalf("ca.crt body: %s", body)
	}
	_, der := ts.request("GET", "/api/v1/certs/ca.cer", nil)
	ca, err := x509.ParseCertificate(der)
	if err != nil || !ca.IsCA || string(der) != string(block.Bytes) {
		t.Fatalf("ca.cer: %v", err)
	}
	if res, _ := ts.request("GET", "/api/v1/certs/ca-key.pem", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("other files: %d", res.StatusCode)
	}
	_, body = ts.request("GET", "/api/v1/certs/ca", nil)
	if !strings.Contains(string(body), `"available":true`) || !strings.Contains(string(body), "TitanOS nas CA") || strings.Contains(string(body), "PRIVATE") {
		t.Errorf("info: %s", body)
	}
}
