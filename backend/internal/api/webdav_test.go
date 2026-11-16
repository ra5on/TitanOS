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
)

func (ts *testServer) dav(method, path, user, password, body string, header map[string]string) (*http.Response, string) {
	ts.t.Helper()
	req, _ := http.NewRequest(method, ts.url+path, strings.NewReader(body))
	if user != "" {
		req.SetBasicAuth(user, password)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		ts.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res, string(data)
}

func TestWebDAV(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	dir := ts.withShare()
	ts.request("POST", "/api/v1/users", map[string]any{"name": "reader", "password": testPassword, "role": "user"})
	if res, body := ts.request("PUT", "/api/v1/shares/Docs", map[string]any{"name": "Docs", "volume": "tank", "smb": true, "access": map[string]string{"alex": "rw", "reader": "ro"}}); res.StatusCode >= 300 {
		t.Fatalf("access: %d %s", res.StatusCode, body)
	}
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi"), 0o644)
	os.WriteFile(filepath.Join(dir, ".secret.titanos-part"), []byte("x"), 0o644)

	// Off: nothing answers.
	if res, _ := ts.dav("PROPFIND", "/dav/", "alex", "x", "", map[string]string{"Depth": "1"}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("off: %d", res.StatusCode)
	}
	ts.request("PUT", "/api/v1/system/webdav", map[string]any{"enabled": true})
	// A session alone makes no WebDAV password: it takes the password again.
	if res, body := ts.request("POST", "/api/v1/account/webdav", map[string]any{}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "wrong_password" {
		t.Fatalf("password without confirmation: %d %s", res.StatusCode, body)
	}
	res, body := ts.request("POST", "/api/v1/account/webdav", map[string]any{"password": testPassword})
	var created struct{ User, Password string }
	if json.Unmarshal(body, &created); res.StatusCode != 200 || len(created.Password) < 20 || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("password: %d %s", res.StatusCode, body)
	}
	if _, body := ts.request("GET", "/api/v1/account/webdav", nil); !strings.Contains(string(body), `"set":true`) || strings.Contains(string(body), created.Password) {
		t.Fatalf("status: %s", body)
	}
	if res, _ := ts.dav("PROPFIND", "/dav/", "alex", testPassword, "", map[string]string{"Depth": "1"}); res.StatusCode != http.StatusUnauthorized || !strings.Contains(res.Header.Get("WWW-Authenticate"), "Basic") {
		t.Fatalf("account password accepted: %d", res.StatusCode)
	}
	res, list := ts.dav("PROPFIND", "/dav/", "alex", created.Password, "", map[string]string{"Depth": "1"})
	if res.StatusCode != http.StatusMultiStatus || !strings.Contains(list, "/dav/Docs/") {
		t.Fatalf("top: %d %s", res.StatusCode, list)
	}
	res, list = ts.dav("PROPFIND", "/dav/Docs/", "alex", created.Password, "", map[string]string{"Depth": "1"})
	if res.StatusCode != http.StatusMultiStatus || !strings.Contains(list, "hello.txt") || strings.Contains(list, "titanos-part") {
		t.Fatalf("share: %d %s", res.StatusCode, list)
	}
	if res, _ := ts.dav("PUT", "/dav/Docs/neu.txt", "alex", created.Password, "Inhalt", nil); res.StatusCode != http.StatusCreated {
		t.Fatalf("put: %d", res.StatusCode)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "neu.txt")); string(data) != "Inhalt" {
		t.Fatal("file not written")
	}
	if res, got := ts.dav("GET", "/dav/Docs/hello.txt", "alex", created.Password, "", nil); res.StatusCode != 200 || got != "hi" {
		t.Fatalf("get: %d %q", res.StatusCode, got)
	}
	if res, _ := ts.dav("MKCOL", "/dav/Docs/Ordner", "alex", created.Password, "", nil); res.StatusCode != http.StatusCreated {
		t.Fatalf("mkcol: %d", res.StatusCode)
	}
	if res, _ := ts.dav("MOVE", "/dav/Docs/neu.txt", "alex", created.Password, "", map[string]string{"Destination": ts.url + "/dav/Docs/Ordner/neu.txt"}); res.StatusCode != http.StatusCreated {
		t.Fatalf("move: %d", res.StatusCode)
	}
	if res, _ := ts.dav("DELETE", "/dav/Docs/Ordner", "alex", created.Password, "", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(dir, "Ordner")); err == nil {
		t.Fatal("not deleted")
	}
	// Nothing at the top, nothing internal, no way out.
	for _, c := range []struct{ method, path string }{{"PUT", "/dav/top.txt"}, {"MKCOL", "/dav/New"}, {"PUT", "/dav/Docs/.recycle/x"}, {"GET", "/dav/Docs/../../etc/passwd"}, {"GET", "/dav/Other/x"}} {
		if res, _ := ts.dav(c.method, c.path, "alex", created.Password, "x", nil); res.StatusCode < 400 {
			t.Fatalf("%s %s: %d", c.method, c.path, res.StatusCode)
		}
	}
	// Read-only access stays read-only.
	reader := ts.newClient()
	ts.do(reader, "POST", "/api/v1/login", credentials{Name: "reader", Password: testPassword})
	_, body = ts.do(reader, "POST", "/api/v1/account/webdav", map[string]any{"password": testPassword})
	var readerPass struct{ Password string }
	json.Unmarshal(body, &readerPass)
	if res, got := ts.dav("GET", "/dav/Docs/hello.txt", "reader", readerPass.Password, "", nil); res.StatusCode != 200 || got != "hi" {
		t.Fatalf("reader get: %d", res.StatusCode)
	}
	if res, _ := ts.dav("PUT", "/dav/Docs/evil.txt", "reader", readerPass.Password, "x", nil); res.StatusCode < 400 {
		t.Fatalf("reader put: %d", res.StatusCode)
	}
	// A new account password ends the WebDAV password too: whoever made it
	// with a stolen session must not keep the way to the shares.
	if res, body := ts.request("POST", "/api/v1/users/reader/password", map[string]string{"password": testPassword + " new"}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("set password: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.dav("GET", "/dav/Docs/hello.txt", "reader", readerPass.Password, "", nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("WebDAV password after a password change: %d", res.StatusCode)
	}
	// Guessing is slowed down.
	limited := false
	for range 12 {
		if res, _ := ts.dav("PROPFIND", "/dav/", "alex", "wrong", "", nil); res.StatusCode == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("no limit on wrong passwords")
	}
}

// A WebDAV client sends its password with every request; the stick is read
// once for it, not for each request (no reads of /config in what a client
// repeats). Changes made through titand still count at once, and a new
// account of the same name never takes the password of the old one.
func TestWebDAVPasswordIsReadOnce(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.withShare()
	ts.request("PUT", "/api/v1/system/webdav", map[string]any{"enabled": true})
	newPassword := func() string {
		_, body := ts.request("POST", "/api/v1/account/webdav", map[string]any{"password": testPassword})
		var created struct{ Password string }
		json.Unmarshal(body, &created)
		return created.Password
	}
	first := newPassword()
	if res, _ := ts.dav("PROPFIND", "/dav/", "alex", first, "", map[string]string{"Depth": "1"}); res.StatusCode != http.StatusMultiStatus {
		t.Fatalf("first: %d", res.StatusCode)
	}
	// Taken away behind titand's back: the answer comes from RAM.
	file := filepath.Join(ts.store.Dir(), "users", "alex", "webdav.json")
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if res, _ := ts.dav("PROPFIND", "/dav/", "alex", first, "", map[string]string{"Depth": "1"}); res.StatusCode != http.StatusMultiStatus {
		t.Fatalf("the stick was read again: %d", res.StatusCode)
	}
	// A new password replaces the old one at once.
	second := newPassword()
	if res, _ := ts.dav("PROPFIND", "/dav/", "alex", first, "", map[string]string{"Depth": "1"}); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old password after a new one: %d", res.StatusCode)
	}
	if res, _ := ts.dav("PROPFIND", "/dav/", "alex", second, "", map[string]string{"Depth": "1"}); res.StatusCode != http.StatusMultiStatus {
		t.Fatalf("new password: %d", res.StatusCode)
	}
	// Deleting it under "Mein Konto" ends it at once.
	ts.request("DELETE", "/api/v1/account/webdav", nil)
	if res, _ := ts.dav("PROPFIND", "/dav/", "alex", second, "", map[string]string{"Depth": "1"}); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("deleted password: %d", res.StatusCode)
	}

	// An account deleted and made again under the same name does not get
	// the WebDAV password of the old one.
	ts.request("POST", "/api/v1/users", map[string]any{"name": "carol", "password": testPassword, "role": "user"})
	carol := ts.newClient()
	ts.do(carol, "POST", "/api/v1/login", credentials{Name: "carol", Password: testPassword})
	_, body := ts.do(carol, "POST", "/api/v1/account/webdav", map[string]any{"password": testPassword})
	var created struct{ Password string }
	json.Unmarshal(body, &created)
	if res, _ := ts.dav("PROPFIND", "/dav/", "carol", created.Password, "", map[string]string{"Depth": "1"}); res.StatusCode != http.StatusMultiStatus {
		t.Fatalf("carol: %d", res.StatusCode)
	}
	if res, body := ts.request("DELETE", "/api/v1/users/carol", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete carol: %d %s", res.StatusCode, body)
	}
	time.Sleep(1100 * time.Millisecond) // a new account, a new second
	ts.request("POST", "/api/v1/users", map[string]any{"name": "carol", "password": testPassword, "role": "user"})
	if res, _ := ts.dav("PROPFIND", "/dav/", "carol", created.Password, "", map[string]string{"Depth": "1"}); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("password of the deleted carol: %d", res.StatusCode)
	}
}
