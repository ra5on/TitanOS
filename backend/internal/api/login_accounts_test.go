package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

type accountsAnswer struct {
	Style    string `json:"style"`
	Accounts []struct {
		Name string `json:"name"`
	} `json:"accounts"`
}

func askAccounts(t *testing.T, ts *testServer, remote string, headers map[string]string) (int, accountsAnswer) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/login/accounts", nil)
	req.RemoteAddr = remote
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	ts.server.ServeHTTP(w, req)
	if w.Code == http.StatusOK && w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("cacheable answer: %q", w.Header().Get("Cache-Control"))
	}
	var answer accountsAnswer
	_ = json.Unmarshal(w.Body.Bytes(), &answer)
	return w.Code, answer
}

func names(a accountsAnswer) []string {
	out := []string{}
	for _, x := range a.Accounts {
		out = append(out, x.Name)
	}
	return out
}

func TestLoginAccounts(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.users.Create("bea", testPassword, auth.RoleUser)
	ts.users.Create("zed", testPassword, auth.RoleUser)
	if _, err := ts.users.SetDisabled("zed", true, "alex"); err != nil {
		t.Fatal(err)
	}
	home := netip.MustParsePrefix("192.168.1.0/24")
	ts.server.loginAccounts.homeNetwork = home.Contains

	// Home network, default settings: all accounts but the locked one.
	code, answer := askAccounts(t, ts, "192.168.1.20:50000", nil)
	if code != 200 || answer.Style != "circles" || len(answer.Accounts) != 2 || names(answer)[0] != "alex" || names(answer)[1] != "bea" {
		t.Fatalf("home network: %d %+v", code, answer)
	}
	// Outside, through a tunnel or proxy, from the NAS itself: none.
	for _, tc := range []struct {
		remote  string
		headers map[string]string
	}{
		{"203.0.113.9:4000", nil},
		{"100.101.102.103:4000", nil}, // Tailscale
		{"127.0.0.1:4000", nil},       // cloudflared on the NAS
		{"[::1]:4000", nil},
		{"192.168.1.20:4000", map[string]string{"Cf-Connecting-Ip": "203.0.113.9"}},
		{"192.168.1.20:4000", map[string]string{"X-Forwarded-For": "192.168.1.30"}},
		{"192.168.1.20:4000", map[string]string{"Forwarded": "for=192.168.1.30"}},
	} {
		code, answer := askAccounts(t, ts, tc.remote, tc.headers)
		if code != 200 || len(answer.Accounts) != 0 || answer.Accounts == nil || answer.Style != "circles" {
			t.Errorf("%s %v: %d %+v", tc.remote, tc.headers, code, answer)
		}
	}
	// The real server sees the test client on loopback: external.
	res, body := ts.do(ts.newClient(), "GET", "/api/v1/login/accounts", nil)
	if res.StatusCode != 200 || string(body) != `{"style":"circles","accounts":[]}`+"\n" {
		t.Errorf("loopback: %d %s", res.StatusCode, body)
	}

	// Owner switches to a list and shows accounts outside too.
	save := loginSettings(func(v map[string]any) {
		v["accountsStyle"], v["showAccountsExternal"] = "list", true
	})
	if res, body := ts.request("PUT", "/api/v1/login-appearance", save); res.StatusCode != 200 {
		t.Fatalf("save: %d %s", res.StatusCode, body)
	}
	if code, answer := askAccounts(t, ts, "203.0.113.9:4000", map[string]string{"Cf-Connecting-Ip": "203.0.113.9"}); code != 200 || answer.Style != "list" || len(answer.Accounts) != 2 {
		t.Errorf("external allowed: %d %+v", code, answer)
	}
	// The editor reads the settings back; anonymous visitors do not.
	_, body = ts.request("GET", "/api/v1/login-appearance", nil)
	var look map[string]any
	_ = json.Unmarshal(body, &look)
	if look["accountsStyle"] != "list" || look["showAccounts"] != true || look["showAccountsExternal"] != true {
		t.Errorf("admin view: %s", body)
	}
	// Off: nobody gets names, the style still comes.
	save = loginSettings(func(v map[string]any) { v["accountsStyle"], v["showAccounts"] = "list", false })
	ts.request("PUT", "/api/v1/login-appearance", save)
	if code, answer := askAccounts(t, ts, "192.168.1.20:4000", nil); code != 200 || answer.Style != "list" || len(answer.Accounts) != 0 {
		t.Errorf("switched off: %d %+v", code, answer)
	}
	// An unknown style is refused; a missing one means circles.
	if res, _ := ts.request("PUT", "/api/v1/login-appearance", loginSettings(func(v map[string]any) { v["accountsStyle"] = "grid" })); res.StatusCode != 400 {
		t.Errorf("unknown style: %d", res.StatusCode)
	}
	ts.request("PUT", "/api/v1/login-appearance", loginSettings(nil))
	if code, answer := askAccounts(t, ts, "192.168.1.20:4000", nil); code != 200 || answer.Style != "circles" || len(answer.Accounts) != 2 {
		t.Errorf("older editor without the fields: %d %+v", code, answer)
	}
	// PLAN 69: single accounts hidden; the list is cleaned, only the editor sees it.
	save = loginSettings(func(v map[string]any) { v["hiddenAccounts"] = []string{"bea", "bea", "gone"} })
	if res, body := ts.request("PUT", "/api/v1/login-appearance", save); res.StatusCode != 200 {
		t.Fatalf("hide: %d %s", res.StatusCode, body)
	}
	if code, answer := askAccounts(t, ts, "192.168.1.20:4000", nil); code != 200 || len(answer.Accounts) != 1 || names(answer)[0] != "alex" {
		t.Errorf("hidden bea: %d %+v", code, answer)
	}
	_, body = ts.request("GET", "/api/v1/login-appearance", nil)
	look = map[string]any{}
	_ = json.Unmarshal(body, &look)
	if h, _ := look["hiddenAccounts"].([]any); len(h) != 2 || h[0] != "bea" || h[1] != "gone" {
		t.Errorf("admin view of hidden: %s", body)
	}
	res, body = ts.do(ts.newClient(), "GET", "/api/v1/login-appearance", nil)
	if res.StatusCode != 200 || strings.Contains(string(body), "hiddenAccounts") || strings.Contains(string(body), "bea") {
		t.Errorf("anonymous sees hidden accounts: %s", body)
	}
	if res, _ := ts.request("PUT", "/api/v1/login-appearance", loginSettings(func(v map[string]any) { v["hiddenAccounts"] = []string{"a\nb"} })); res.StatusCode != 400 {
		t.Errorf("control character: %d", res.StatusCode)
	}
}

func TestLoginAccountsRateLimit(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	now := time.Unix(1_800_000_000, 0)
	ts.server.loginAccounts.now = func() time.Time { return now }
	for i := range loginAccountsBurst {
		if code, _ := askAccounts(t, ts, "192.168.1.20:1", nil); code != 200 {
			t.Fatalf("request %d: %d", i, code)
		}
	}
	if code, _ := askAccounts(t, ts, "192.168.1.20:1", nil); code != http.StatusTooManyRequests {
		t.Fatalf("over the burst: %d", code)
	}
	if code, _ := askAccounts(t, ts, "192.168.1.21:1", nil); code != 200 {
		t.Fatalf("another address: %d", code)
	}
	now = now.Add(loginAccountsEvery)
	if code, _ := askAccounts(t, ts, "192.168.1.20:1", nil); code != 200 {
		t.Fatalf("after waiting: %d", code)
	}
	// An address guessing passwords gets no names.
	for range 6 {
		ts.server.limiter.Fail("ip:192.168.1.22")
	}
	if code, _ := askAccounts(t, ts, "192.168.1.22:1", nil); code != http.StatusTooManyRequests {
		t.Fatalf("guessing address: %d", code)
	}
}
