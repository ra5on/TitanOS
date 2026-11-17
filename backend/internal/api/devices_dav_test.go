package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/cluster"
	"github.com/ra5on/TitanOS/backend/internal/davsync"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// newDAVTestServer is a test server with calendars on a temporary volume.
func newDAVTestServer(t *testing.T) *testServer {
	t.Helper()
	dir := t.TempDir()
	pim := pimstore.New()
	pim.Volume = func(feature string, create bool) (*os.Root, func(), error) {
		path := filepath.Join(dir, ".titanos", feature)
		if create {
			if err := os.MkdirAll(path, 0o700); err != nil {
				return nil, nil, err
			}
		}
		root, err := os.OpenRoot(path)
		return root, func() {}, err
	}
	ts := newTestServerWith(t, func(o *Options) {
		o.PIM = pim
		pim.Account = func(name string) (time.Time, bool) {
			u, ok := o.Users.Get(name)
			return u.Created, ok
		}
	})
	ts.setup()
	return ts
}

// davDo sends a request through the whole chain, following no redirect
// (a redirect to another handler must not count as "refused").
func (ts *testServer) davDo(method, path string, header map[string]string, basic ...string) (*http.Response, string) {
	ts.t.Helper()
	body := ""
	if method == "POST" {
		body = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	}
	req, err := http.NewRequest(method, ts.url+path, strings.NewReader(body))
	if err != nil {
		ts.t.Fatal(err)
	}
	if len(basic) == 2 {
		req.SetBasicAuth(basic[0], basic[1])
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		ts.t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return res, string(b)
}

// A device password reaches davsync and nothing else (TECHNIK 3.4.6,
// security question 1): paths that only look like /caldav/ after decoding
// or cleaning are refused by bearerAccess itself, not redirected.
func TestDeviceDAVOnlyDavsync(t *testing.T) {
	ts := newDAVTestServer(t)
	ts.withShare()
	ts.request("PUT", "/api/v1/system/webdav", map[string]any{"enabled": true})
	device := ts.newDevice(ts.client, "iPhone")

	res, body := ts.davDo("PROPFIND", "/caldav/", map[string]string{"Depth": "0"}, "alex", device.Password)
	if res.StatusCode != http.StatusMultiStatus || !strings.Contains(body, "/caldav/alex/") || res.Header.Get("DAV") == "" {
		t.Fatalf("PROPFIND /caldav/ with the device: %d %s", res.StatusCode, body)
	}
	res, _ = ts.davDo("PROPFIND", "/caldav/alex/calendars/", map[string]string{"Depth": "1"}, "alex", device.Password)
	if res.StatusCode != http.StatusMultiStatus {
		t.Fatalf("home set: %d", res.StatusCode)
	}
	if res, _ := ts.davDo("PROPFIND", "/.well-known/caldav", nil); res.StatusCode != http.StatusMovedPermanently || res.Header.Get("Location") != "/caldav/" {
		t.Errorf("well-known: %d %q", res.StatusCode, res.Header.Get("Location"))
	}

	for _, c := range []struct{ method, path string }{
		{"PROPFIND", "/caldavx"},
		{"PROPFIND", "/caldavx/alex/"},
		{"GET", "/caldav/../api/v1/system"},
		{"GET", "/caldav/%2e%2e/api/v1/system"},
		{"GET", "/caldav/%2E%2E/api/v1/system"},
		{"GET", "/carddav/./../api/v1/system"},
		{"GET", "/caldav//api/v1/system"},
		{"PROPFIND", "/.well-known/caldav/../../dav/"},
		{"PROPFIND", "/.well-known/caldav/"},
		{"GET", "/caldav%2F..%2Fapi/v1/system"},
		{"GET", "/caldav%2Fx"},
		{"GET", "/api/v1/system"},
	} {
		res, body := ts.davDo(c.method, c.path, nil, "alex", device.Password)
		if res.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "expected a Bearer token") {
			t.Errorf("%s %s with the device password: %d %s", c.method, c.path, res.StatusCode, body)
		}
	}
	// MCP checks its own token (switched off here, refused either way).
	if res, body := ts.davDo("POST", "/mcp", nil, "alex", device.Password); res.StatusCode < 400 || strings.Contains(body, "tools") {
		t.Errorf("/mcp with the device password: %d %s", res.StatusCode, body)
	}
	// /dav/ checks its own Basic sign-in: the device password is no WebDAV
	// password.
	res, _ = ts.davDo("PROPFIND", "/dav/", map[string]string{"Depth": "1"}, "alex", device.Password)
	if res.StatusCode != http.StatusUnauthorized || strings.Contains(res.Header.Get("WWW-Authenticate"), "Kalender") {
		t.Errorf("/dav/ with the device password: %d %q", res.StatusCode, res.Header.Get("WWW-Authenticate"))
	}
}

// Account password, WebDAV password and API token (as Bearer or as Basic
// password) get the challenge of davsync (security question 2).
func TestDeviceDAVRefusesOtherSecrets(t *testing.T) {
	ts := newDAVTestServer(t)
	ts.newDevice(ts.client, "iPhone")
	_, body := ts.request("POST", "/api/v1/account/webdav", map[string]any{"password": testPassword})
	var dav struct{ Password string }
	if json.Unmarshal(body, &dav) != nil || dav.Password == "" {
		t.Fatalf("WebDAV password: %s", body)
	}
	token := ts.mcpToken("alex", "system:read")
	challenge := `Basic realm="TitanOS Kalender", charset="UTF-8"`
	for what, password := range map[string]string{"account password": testPassword, "WebDAV password": dav.Password, "API token": token} {
		res, _ := ts.davDo("PROPFIND", "/caldav/", map[string]string{"Depth": "0"}, "alex", password)
		if res.StatusCode != http.StatusUnauthorized || res.Header.Get("WWW-Authenticate") != challenge {
			t.Errorf("%s: %d %q", what, res.StatusCode, res.Header.Get("WWW-Authenticate"))
		}
	}
	for _, path := range []string{"/caldav/", "/carddav/alex/", "/caldav/alex/calendars/privat/"} {
		res, _ := ts.davDo("PROPFIND", path, map[string]string{"Authorization": "Bearer " + token, "Depth": "0"})
		if res.StatusCode != http.StatusUnauthorized || res.Header.Get("WWW-Authenticate") != challenge {
			t.Errorf("Bearer at %s: %d %q", path, res.StatusCode, res.Header.Get("WWW-Authenticate"))
		}
	}
}

// Calendars sync with each node itself: a request through the cluster is
// refused before anything else (TECHNIK 4.1.3).
func TestDeviceDAVNotThroughCluster(t *testing.T) {
	ts := newDAVTestServer(t)
	device := ts.newDevice(ts.client, "iPhone")
	for _, path := range []string{"/caldav", "/caldav/", "/carddav/alex/addressbooks/", "/.well-known/caldav", "/.well-known/carddav"} {
		res, body := ts.davDo("PROPFIND", path, map[string]string{cluster.Header: "node-b", "Depth": "0"}, "alex", device.Password)
		if res.StatusCode != http.StatusForbidden || !strings.Contains(body, "cluster_local_only") {
			t.Errorf("%s through the cluster: %d %s", path, res.StatusCode, body)
		}
	}
}

// CrossOriginProtection stays in front: a browser on another site cannot
// send PROPFIND (or REPORT) with the device's credentials, and OPTIONS (its
// preflight) gets no CORS headers (security question 19).
func TestDeviceDAVCrossOrigin(t *testing.T) {
	ts := newDAVTestServer(t)
	device := ts.newDevice(ts.client, "iPhone")
	for _, h := range []map[string]string{
		{"Origin": "https://evil.example", "Depth": "0"},
		{"Sec-Fetch-Site": "cross-site", "Depth": "0"},
	} {
		for _, method := range []string{"PROPFIND", "REPORT"} {
			res, body := ts.davDo(method, "/caldav/alex/calendars/", h, "alex", device.Password)
			if res.StatusCode != http.StatusForbidden || strings.Contains(body, "multistatus") {
				t.Errorf("%s %v: %d %s", method, h, res.StatusCode, body)
			}
		}
	}
	res, _ := ts.davDo("OPTIONS", "/caldav/alex/", map[string]string{"Origin": "https://evil.example", "Access-Control-Request-Method": "PROPFIND"})
	for k := range res.Header {
		if strings.HasPrefix(k, "Access-Control-") {
			t.Errorf("OPTIONS answers %s", k)
		}
	}
	// iOS sends no Origin: it passes.
	if res, _ := ts.davDo("PROPFIND", "/caldav/alex/", map[string]string{"Depth": "0"}, "alex", device.Password); res.StatusCode != http.StatusMultiStatus {
		t.Errorf("without Origin: %d", res.StatusCode)
	}
}

// The chain's predicate: only canonical davsync paths without an encoded
// "/" (the mux matches escaped segments).
func TestDeviceDAVPathPredicate(t *testing.T) {
	for raw, want := range map[string]bool{
		"/caldav/": true, "/caldav": true, "/carddav/alex/addressbooks/kontakte/": true, "/.well-known/carddav": true,
		"/caldav/alex/calendars/privat/a%20b.ics": true,
		"/caldav/../api/v1/system":                false, "/caldav/%2e%2e/api": false, "/caldav%2Fx": false, "/caldav/alex%2fx/": false,
		"/caldavx": false, "/dav/": false, "/.well-known/caldav/../../dav/": false,
	} {
		req, err := http.NewRequest("PROPFIND", "http://nas"+raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := davsyncPath(req); got != want {
			t.Errorf("davsyncPath(%q) = %v, want %v (Path %q RawPath %q)", raw, got, want, req.URL.Path, req.URL.RawPath)
		}
	}
	if len(davsync.Patterns) != 6 {
		t.Errorf("patterns %v", davsync.Patterns)
	}
}
