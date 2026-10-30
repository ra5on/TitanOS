package api

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/appbackup"
)

// The stick manager shows settings without secrets and nothing else, and
// only to a browser session.
func TestStickShowsOnlySettingsWithoutSecrets(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	dir := ts.store.Dir()
	for name, content := range map[string]string{
		"homeassistant/settings.json":   `{"token":"x"}`,
		"plugins/netbird/settings.json": `{"setupKey":"x"}`,
		"plugins/nvidia/build-key.json": `{"privateKey":"x"}`,
		"plugins/settings.json":         `{}`,
		"docker/proxy-tailscale.key":    "tskey",
		"docker/settings.json":          `{}`,
		"system/ups.json":               `{"password":"x"}`,
		"system/settings.json":          `{}`,
		"system/settings.json.prev":     `{}`,
	} {
		os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o700)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, hidden := range []string{"homeassistant/settings.json", "plugins/netbird/settings.json", "plugins/nvidia/build-key.json", "docker/proxy-tailscale.key", "system/ups.json"} {
		if res, body := ts.request("GET", "/api/v1/files/text?root=stick&path="+hidden, nil); res.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d %s", hidden, res.StatusCode, body)
		}
	}
	for _, shown := range []string{"plugins/settings.json", "docker/settings.json", "system/settings.json"} {
		if res, body := ts.request("GET", "/api/v1/files/text?root=stick&path="+shown, nil); res.StatusCode != http.StatusOK {
			t.Errorf("%s: %d %s", shown, res.StatusCode, body)
		}
	}
	res, body := ts.request("GET", "/api/v1/files/list?root=stick", nil)
	if res.StatusCode != http.StatusOK || strings.Contains(string(body), "homeassistant") || !strings.Contains(string(body), `"plugins"`) {
		t.Errorf("stick: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("GET", "/api/v1/files/list?root=stick&path=plugins", nil)
	if res.StatusCode != http.StatusOK || strings.Contains(string(body), "netbird") || strings.Contains(string(body), "nvidia") || !strings.Contains(string(body), "settings.json") {
		t.Errorf("plugins: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("GET", "/api/v1/files/list?root=stick&path=system", nil)
	if res.StatusCode != http.StatusOK || strings.Contains(string(body), "ups.json") || !strings.Contains(string(body), "settings.json.prev") {
		t.Errorf("system: %d %s", res.StatusCode, body)
	}

	// An API token does not get the stick, even with the scope for files.
	owner, _ := ts.users.Get("alex")
	_, plain, err := ts.server.tokens.Create(owner, "files", []string{"files:read"})
	if err != nil {
		t.Fatal(err)
	}
	call := func(path string) (int, string) {
		req, _ := http.NewRequest("GET", ts.url+path, nil)
		req.Header.Set("Authorization", "Bearer "+plain)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(data)
	}
	if code, body := call("/api/v1/files/roots"); code != http.StatusOK || strings.Contains(body, `"stick"`) {
		t.Errorf("roots with a token: %d %s", code, body)
	}
	if code, body := call("/api/v1/files/list?root=stick"); code != http.StatusNotFound {
		t.Errorf("stick with a token: %d %s", code, body)
	}
}

// API tokens and SSH keys open the account like a password: adding one
// asks for it again, like switching two-factor login off.
func TestTokensAndSSHKeysNeedThePassword(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if res, body := ts.request("POST", "/api/v1/account/tokens", map[string]any{"name": "cli", "scopes": []string{"system:read"}}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "wrong_password" {
		t.Fatalf("token without the password: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/account/tokens", map[string]any{"name": "cli", "scopes": []string{"system:read"}, "password": testPassword}); res.StatusCode != http.StatusCreated || !strings.Contains(string(body), `"secret"`) {
		t.Fatalf("token: %d %s", res.StatusCode, body)
	}
	const key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGaa0YUn9eEcPL0FzQ0qzL9dXYrmsBrrmIjXzyEGPBaN test@laptop"
	if res, body := ts.request("POST", "/api/v1/account/ssh-keys", map[string]string{"key": key, "password": "wrong"}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "wrong_password" {
		t.Fatalf("key with a wrong password: %d %s", res.StatusCode, body)
	}
	// Failures count against the account like failed logins.
	for range 5 {
		ts.request("POST", "/api/v1/account/ssh-keys", map[string]string{"key": key, "password": "wrong"})
	}
	if res, body := ts.request("POST", "/api/v1/account/ssh-keys", map[string]string{"key": key, "password": testPassword}); res.StatusCode != http.StatusTooManyRequests || errorCode(t, body) != "rate_limited" {
		t.Fatalf("after failures: %d %s", res.StatusCode, body)
	}
}

// Strangers who guess wrong lock the account name for unknown devices,
// never for the browser of the owner.
func TestStrangersCannotLockTheOwnerOut(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	owner := ts.newClient()
	if res, body := ts.do(owner, "POST", "/api/v1/login", credentials{Name: "alex", Password: testPassword}); res.StatusCode != http.StatusOK {
		t.Fatalf("login: %d %s", res.StatusCode, body)
	}
	ts.do(owner, "POST", "/api/v1/logout", nil)

	attacker := ts.newClient()
	for range 6 {
		ts.do(attacker, "POST", "/api/v1/login", credentials{Name: "alex", Password: "guess"})
	}
	// In the test all come from 127.0.0.1; the lock of the address is a
	// matter of its own.
	ts.server.limiter.Reset("ip:127.0.0.1")
	if res, body := ts.do(ts.newClient(), "POST", "/api/v1/login", credentials{Name: "alex", Password: testPassword}); res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("unknown device during the lock: %d %s", res.StatusCode, body)
	}
	if res, body := ts.do(owner, "POST", "/api/v1/login", credentials{Name: "alex", Password: testPassword}); res.StatusCode != http.StatusOK {
		t.Fatalf("known device during the lock: %d %s", res.StatusCode, body)
	}
	// A forged device cookie does not count.
	forged := ts.newClient()
	req, _ := http.NewRequest("POST", ts.url+"/api/v1/login", strings.NewReader(`{"name":"alex","password":"`+testPassword+`"}`))
	req.AddCookie(&http.Cookie{Name: ts.server.deviceCookieName(), Value: "alex.AAAAAAAAAAAAAAAAAAAAAA.AAAA"})
	for range 6 {
		ts.do(attacker, "POST", "/api/v1/login", credentials{Name: "alex", Password: "guess"})
	}
	ts.server.limiter.Reset("ip:127.0.0.1")
	res, err := forged.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("forged device cookie: %d", res.StatusCode)
	}
}

// The gate of application backups is held through a change, but only
// checked when a console opens.
func TestConsolesDoNotHoldTheApplicationGate(t *testing.T) {
	ts := newTestServer(t)
	gate := &appbackup.Manager{}
	ts.server.opts.ApplicationBackups = gate
	// Not loaded yet counts as busy.
	free := func(w http.ResponseWriter, r *http.Request) {
		if gate.Gate.TryLock() {
			gate.Gate.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusConflict)
	}
	// An unloaded manager is busy: both refuse.
	rec := httptest.NewRecorder()
	ts.server.appLifecycle(false, free)(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusLocked {
		t.Fatalf("busy: %d", rec.Code)
	}
	ready := &appbackup.Manager{Store: ts.store}
	if err := ready.Load(); err != nil {
		t.Fatal(err)
	}
	ts.server.opts.ApplicationBackups = ready
	gate = ready
	rec = httptest.NewRecorder()
	ts.server.appLifecycle(false, free)(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("console holds the gate: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	ts.server.appLifecycle(true, free)(rec, httptest.NewRequest("POST", "/", nil))
	if rec.Code != http.StatusConflict {
		t.Errorf("change does not hold the gate: %d", rec.Code)
	}
}

// Passkey challenges are limited per source: a stranger pushes out only
// his own, and asks only so often.
func TestPasskeyChallengesPerSource(t *testing.T) {
	var p passkeyChallenges
	mine, _, ok := p.put(passkeyChallenge{source: "ip:192.0.2.1"})
	if !ok {
		t.Fatal("first challenge refused")
	}
	var first string
	for i := range passkeyBegins {
		id, _, ok := p.put(passkeyChallenge{source: "ip:198.51.100.7"})
		if !ok {
			t.Fatalf("challenge %d refused", i)
		}
		if first == "" {
			first = id
		}
	}
	if _, _, ok := p.put(passkeyChallenge{source: "ip:198.51.100.7"}); ok {
		t.Error("more challenges than the rate allows")
	}
	if _, ok := p.take(first); ok {
		t.Error("the stranger kept more than his share waiting")
	}
	if _, ok := p.take(mine); !ok {
		t.Error("the challenge of another source was pushed out")
	}
	if got := passkeySource(&http.Request{RemoteAddr: "[2001:db8:1:2:3:4:5:6]:443"}); got != "net:2001:db8:1:2::" {
		t.Errorf("IPv6 source %q", got)
	}
}

// The first setup only answers in the network of the NAS.
func TestSetupOnlyFromTheHomeNetwork(t *testing.T) {
	reached := false
	handler := setupLocal(func(w http.ResponseWriter, r *http.Request) { reached = true })
	for addr, want := range map[string]bool{
		"127.0.0.1:1": true, "192.168.1.20:1": true, "10.0.0.5:1": true, "[fd12::5]:1": true, "[fe80::1]:1": true, "[::1]:1": true,
		"203.0.113.9:1": false, "[2001:db8::9]:1": false, "8.8.8.8:1": false,
	} {
		reached = false
		req := httptest.NewRequest("POST", "/api/v1/setup", nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		handler(rec, req)
		if reached != want || (!want && (rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "setup_not_local"))) {
			t.Errorf("%s: reached %v, %d %s", addr, reached, rec.Code, rec.Body)
		}
	}
}

// index.html carries its policy also as a header, with what a meta tag
// cannot say; without TLS from Let's Encrypt there is no HSTS.
func TestContentSecurityPolicyHeader(t *testing.T) {
	ts := newTestServer(t)
	res, _ := ts.request("GET", "/", nil)
	csp := res.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "object-src 'none'") || strings.Contains(csp, "script-src") {
		t.Errorf("policy without a meta tag: %q", csp)
	}
	if res.Header.Get("Strict-Transport-Security") != "" {
		t.Error("HSTS without TLS")
	}
	if res, _ := ts.request("GET", "/api/v1/health", nil); res.Header.Get("Content-Security-Policy") != "" {
		t.Error("policy on API answers")
	}
	ui := fstest.MapFS{"index.html": {Data: []byte(`<!doctype html><head><meta http-equiv="content-security-policy" content="script-src 'self' 'sha256-abc='; object-src 'none'; base-uri 'self'"></head>`)}}
	if got, want := contentSecurityPolicy(ui), "script-src 'self' 'sha256-abc='; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'"; got != want {
		t.Errorf("policy %q, want %q", got, want)
	}
}

// A client that sends its JSON body too slowly is cut off.
func TestJSONBodyHasADeadline(t *testing.T) {
	ts := newTestServer(t)
	old := jsonReadTimeout
	jsonReadTimeout = 300 * time.Millisecond
	t.Cleanup(func() { jsonReadTimeout = old })
	conn, err := net.Dial("tcp", strings.TrimPrefix(ts.url, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	io.WriteString(conn, "POST /api/v1/login HTTP/1.1\r\nHost: x\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{\"name\":")
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusRequestTimeout {
		t.Fatalf("slow body: %d", res.StatusCode)
	}
}
