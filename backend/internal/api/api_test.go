package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/system"
)

var testUI = fstest.MapFS{
	"index.html":                  {Data: []byte("<!doctype html><title>index</title>")},
	"favicon.svg":                 {Data: []byte("<svg/>")},
	"_app/immutable/entry/app.js": {Data: []byte("console.log(1)")},
}

const testPassword = "a long test password"

type fakePower struct{ actions chan string }

func (p fakePower) Reboot() error   { p.actions <- "reboot"; return nil }
func (p fakePower) Poweroff() error { p.actions <- "poweroff"; return nil }

type testServer struct {
	t      *testing.T
	url    string
	store  *config.Store
	users  *auth.Users
	power  fakePower
	client *http.Client
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	store := config.Open(t.TempDir())
	users := auth.NewUsers(store)
	if err := users.Load(); err != nil {
		t.Fatal(err)
	}
	power := fakePower{actions: make(chan string, 1)}
	srv := httptest.NewServer(New(Options{
		UI: testUI, Store: store, Users: users, Monitor: system.NewMonitor(context.Background()),
		Power: power, Version: "1.2.3",
	}))
	t.Cleanup(srv.Close)
	ts := &testServer{t: t, url: srv.URL, store: store, users: users, power: power}
	ts.client = ts.newClient()
	return ts
}

// newClient returns a client with its own cookies: a separate browser.
func (ts *testServer) newClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

func (ts *testServer) do(client *http.Client, method, path string, body any) (*http.Response, []byte) {
	ts.t.Helper()
	var reader io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		reader = bytes.NewReader(b)
	default:
		data, _ := json.Marshal(b)
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, ts.url+path, reader)
	if err != nil {
		ts.t.Fatal(err)
	}
	res, err := client.Do(req)
	if err != nil {
		ts.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res, data
}

func (ts *testServer) request(method, path string, body any) (*http.Response, []byte) {
	ts.t.Helper()
	return ts.do(ts.client, method, path, body)
}

func (ts *testServer) setup() {
	ts.t.Helper()
	res, body := ts.request("POST", "/api/v1/setup", credentials{Name: "alex", Password: testPassword})
	if res.StatusCode != http.StatusCreated {
		ts.t.Fatalf("setup: %d %s", res.StatusCode, body)
	}
}

func errorCode(t *testing.T, body []byte) string {
	t.Helper()
	var res map[string]apiError
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("error body is not JSON: %s", body)
	}
	return res["error"].Code
}

func session(t *testing.T, body []byte) sessionResponse {
	t.Helper()
	var res sessionResponse
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("session body: %s", body)
	}
	return res
}

func TestSetupLoginLogout(t *testing.T) {
	ts := newTestServer(t)

	_, body := ts.request("GET", "/api/v1/session", nil)
	if s := session(t, body); !s.SetupRequired || !s.ConfigAvailable || s.User != nil || s.Version != "" {
		t.Fatalf("fresh system: %+v", s)
	}

	// Nothing but the public endpoints works before login.
	if res, _ := ts.request("GET", "/api/v1/system", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("system without login: %d", res.StatusCode)
	}

	for _, tc := range []struct{ name, password, code string }{
		{"Alex Example", testPassword, "invalid_name"},
		{"root", testPassword, "reserved_name"},
		{"alex", "short", "password_too_short"},
	} {
		res, body := ts.request("POST", "/api/v1/setup", credentials{Name: tc.name, Password: tc.password})
		if res.StatusCode != http.StatusBadRequest || errorCode(t, body) != tc.code {
			t.Errorf("setup %q: %d %s, want %s", tc.name, res.StatusCode, body, tc.code)
		}
	}

	ts.setup()
	_, body = ts.request("GET", "/api/v1/session", nil)
	if s := session(t, body); s.SetupRequired || s.User == nil || s.User.Name != "alex" || s.User.Role != auth.RoleOwner || s.Version != "1.2.3" {
		t.Fatalf("after setup: %+v", s)
	}

	// Setup is closed for good once the owner exists.
	other := ts.newClient()
	res, body := ts.do(other, "POST", "/api/v1/setup", credentials{Name: "eve", Password: testPassword})
	if res.StatusCode != http.StatusConflict || errorCode(t, body) != "setup_done" {
		t.Errorf("second setup: %d %s", res.StatusCode, body)
	}

	if res, _ := ts.request("POST", "/api/v1/logout", nil); res.StatusCode != http.StatusNoContent {
		t.Errorf("logout: %d", res.StatusCode)
	}
	if res, _ := ts.request("GET", "/api/v1/system", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("system after logout: %d", res.StatusCode)
	}

	res, body = ts.request("POST", "/api/v1/login", credentials{Name: "alex", Password: "the wrong password"})
	if res.StatusCode != http.StatusUnauthorized || errorCode(t, body) != "invalid_credentials" {
		t.Errorf("wrong password: %d %s", res.StatusCode, body)
	}
	// Names are not case sensitive at the login form.
	res, body = ts.request("POST", "/api/v1/login", credentials{Name: " Alex ", Password: testPassword})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login: %d %s", res.StatusCode, body)
	}
	cookie := res.Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Value == "" {
		t.Errorf("session cookie = %+v", cookie)
	}
	if res, body := ts.request("GET", "/api/v1/system", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"version":"1.2.3"`) {
		t.Errorf("system after login: %d %s", res.StatusCode, body)
	}
}

func TestLoginIsRateLimited(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	attacker := ts.newClient()

	for i := range 5 {
		res, _ := ts.do(attacker, "POST", "/api/v1/login", credentials{Name: "alex", Password: "guess number one"})
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i+1, res.StatusCode)
		}
	}
	res, body := ts.do(attacker, "POST", "/api/v1/login", credentials{Name: "alex", Password: testPassword})
	if res.StatusCode != http.StatusTooManyRequests || errorCode(t, body) != "rate_limited" {
		t.Fatalf("after 5 failures even the right password must wait: %d %s", res.StatusCode, body)
	}
	var parsed map[string]apiError
	json.Unmarshal(body, &parsed)
	if parsed["error"].RetryAfter <= 0 {
		t.Errorf("retryAfter = %d", parsed["error"].RetryAfter)
	}
}

func TestPreferences(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()

	_, body := ts.request("GET", "/api/v1/preferences", nil)
	if strings.TrimSpace(string(body)) != `{"appearance":null,"dashboard":null}` {
		t.Errorf("fresh preferences = %s", body)
	}

	if res, body := ts.request("PUT", "/api/v1/preferences/appearance", map[string]any{"theme": "hell"}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("put: %d %s", res.StatusCode, body)
	}
	_, body = ts.request("GET", "/api/v1/preferences", nil)
	if !strings.Contains(string(body), `"appearance":{"theme":"hell"}`) {
		t.Errorf("preferences = %s", body)
	}
	if stored, err := ts.store.Read("users/alex/appearance.json"); err != nil || !strings.Contains(string(stored), "hell") {
		t.Errorf("not written to the store: %s %v", stored, err)
	}

	if res, _ := ts.request("PUT", "/api/v1/preferences/appearance", []byte(`["not", "an", "object"]`)); res.StatusCode != http.StatusBadRequest {
		t.Errorf("array accepted: %d", res.StatusCode)
	}
	if res, _ := ts.request("PUT", "/api/v1/preferences/account", map[string]any{"role": "owner"}); res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown kind accepted: %d", res.StatusCode)
	}
	if res, _ := ts.request("PUT", "/api/v1/preferences/dashboard", []byte(`{"x":"`+strings.Repeat("a", maxJSONBody)+`"}`)); res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body: %d", res.StatusCode)
	}
}

func TestBackground(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()

	if res, _ := ts.request("GET", "/api/v1/background", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("no image yet: %d", res.StatusCode)
	}
	if res, body := ts.request("PUT", "/api/v1/background", []byte("<script>alert(1)</script>")); res.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("HTML accepted as image: %d %s", res.StatusCode, body)
	}

	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	if res, body := ts.request("PUT", "/api/v1/background", png); res.StatusCode != http.StatusNoContent {
		t.Fatalf("upload: %d %s", res.StatusCode, body)
	}
	res, body := ts.request("GET", "/api/v1/background", nil)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/png" || !bytes.Equal(body, png) {
		t.Errorf("download: %d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if res, _ := ts.request("DELETE", "/api/v1/background", nil); res.StatusCode != http.StatusNoContent {
		t.Errorf("delete: %d", res.StatusCode)
	}
	if res, _ := ts.request("GET", "/api/v1/background", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("after delete: %d", res.StatusCode)
	}
}

func TestPower(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()

	if res, _ := ts.request("POST", "/api/v1/system/power", map[string]string{"action": "format-everything"}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown action: %d", res.StatusCode)
	}
	if res, _ := ts.request("POST", "/api/v1/system/power", map[string]string{"action": "reboot"}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("reboot: %d", res.StatusCode)
	}
	if got := <-ts.power.actions; got != "reboot" {
		t.Errorf("carried out %q", got)
	}

	// A plain user may not restart the NAS.
	if _, err := ts.users.Create("gast", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	guest := ts.newClient()
	if res, body := ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword}); res.StatusCode != http.StatusOK {
		t.Fatalf("login: %d %s", res.StatusCode, body)
	}
	res, body := ts.do(guest, "POST", "/api/v1/system/power", map[string]string{"action": "poweroff"})
	if res.StatusCode != http.StatusForbidden || errorCode(t, body) != "forbidden" {
		t.Errorf("user powering off: %d %s", res.StatusCode, body)
	}
}

func TestCrossSiteRequestsAreRejected(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()

	req, _ := http.NewRequest("POST", ts.url+"/api/v1/system/power", strings.NewReader(`{"action":"poweroff"}`))
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	res, err := ts.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site request: %d, want 403", res.StatusCode)
	}
	select {
	case action := <-ts.power.actions:
		t.Errorf("cross-site request carried out %q", action)
	default:
	}
}

func TestConfigUnavailable(t *testing.T) {
	store := config.Open("/proc/titanos-no-such-dir")
	users := auth.NewUsers(store)
	srv := httptest.NewServer(New(Options{UI: testUI, Store: store, Users: users, Monitor: system.NewMonitor(context.Background()), Power: fakePower{}}))
	defer srv.Close()
	ts := &testServer{t: t, url: srv.URL, client: http.DefaultClient}

	_, body := ts.request("GET", "/api/v1/session", nil)
	if s := session(t, body); s.ConfigAvailable || s.SetupRequired {
		t.Errorf("without stick: %+v, want neither config nor setup", s)
	}
	res, body := ts.request("POST", "/api/v1/setup", credentials{Name: "alex", Password: testPassword})
	if res.StatusCode != http.StatusServiceUnavailable || errorCode(t, body) != "config_unavailable" {
		t.Errorf("setup without stick: %d %s", res.StatusCode, body)
	}
}

func TestHealthAndUnknownAPIPath(t *testing.T) {
	ts := newTestServer(t)
	if res, body := ts.request("GET", "/api/v1/health", nil); res.StatusCode != http.StatusOK || strings.Contains(string(body), "1.2.3") {
		t.Errorf("health: %d %s (must not tell the version)", res.StatusCode, body)
	}
	res, body := ts.request("GET", "/api/v1/does-not-exist", nil)
	if res.StatusCode != http.StatusNotFound || errorCode(t, body) != "not_found" {
		t.Errorf("unknown path: %d %s", res.StatusCode, body)
	}
}

func TestUI(t *testing.T) {
	ts := newTestServer(t)
	tests := []struct {
		name      string
		target    string
		wantBody  string
		wantCache string
	}{
		{"root", "/", "<title>index</title>", "no-cache"},
		{"client-side route", "/storage/volumes", "<title>index</title>", "no-cache"},
		{"directory", "/_app/immutable/", "<title>index</title>", "no-cache"},
		{"file", "/favicon.svg", "<svg/>", "no-cache"},
		{"hashed file", "/_app/immutable/entry/app.js", "console.log(1)", "public, max-age=31536000, immutable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, body := ts.request("GET", tt.target, nil)
			if res.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", res.StatusCode)
			}
			if !strings.Contains(string(body), tt.wantBody) {
				t.Errorf("body = %q, want it to contain %q", body, tt.wantBody)
			}
			if cc := res.Header.Get("Cache-Control"); cc != tt.wantCache {
				t.Errorf("Cache-Control = %q, want %q", cc, tt.wantCache)
			}
			if res.Header.Get("X-Frame-Options") != "DENY" {
				t.Error("X-Frame-Options missing")
			}
		})
	}
	if res, _ := ts.request("POST", "/", nil); res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /: %d, want 405", res.StatusCode)
	}
}
