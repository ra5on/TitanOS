package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	res, body := ts.request("POST", "/api/v1/account/webdav", map[string]any{})
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
	_, body = ts.do(reader, "POST", "/api/v1/account/webdav", map[string]any{})
	var readerPass struct{ Password string }
	json.Unmarshal(body, &readerPass)
	if res, got := ts.dav("GET", "/dav/Docs/hello.txt", "reader", readerPass.Password, "", nil); res.StatusCode != 200 || got != "hi" {
		t.Fatalf("reader get: %d", res.StatusCode)
	}
	if res, _ := ts.dav("PUT", "/dav/Docs/evil.txt", "reader", readerPass.Password, "x", nil); res.StatusCode < 400 {
		t.Fatalf("reader put: %d", res.StatusCode)
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
