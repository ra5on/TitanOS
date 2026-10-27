package api

import (
	"net/http"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// Downloads from a link resolve the target root with the account's rights
// like every file request: unknown and foreign roots look the same, the
// read-only stick is refused, anonymous requests never reach the handler.
func TestURLDownloadRights(t *testing.T) {
	ts := newTestServer(t)
	body := map[string]string{"root": "stick", "dir": "", "url": "https://example.org/a.iso", "name": "a.iso"}
	for _, path := range []string{"/api/v1/files/url-download", "/api/v1/files/url-download/probe"} {
		if res, _ := ts.request("POST", path, body); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s: %d", path, res.StatusCode)
		}
	}
	ts.setup()
	for _, path := range []string{"/api/v1/files/url-download", "/api/v1/files/url-download/probe"} {
		if res, data := ts.request("POST", path, body); res.StatusCode != http.StatusForbidden || errorCode(t, data) != "read_only_share" {
			t.Errorf("stick %s: %d %s", path, res.StatusCode, data)
		}
		other := map[string]string{"root": "share/fotos", "url": "https://example.org/a.iso", "name": "a.iso"}
		if res, data := ts.request("POST", path, other); res.StatusCode != http.StatusNotFound || errorCode(t, data) != "file_not_found" {
			t.Errorf("unknown root %s: %d %s", path, res.StatusCode, data)
		}
	}
	if res, data := ts.request("POST", "/api/v1/files/jobs/0123456789abcdef/retry", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("retry of an unknown job: %d %s", res.StatusCode, data)
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	if res, data := ts.do(guest, "POST", "/api/v1/files/url-download", body); res.StatusCode != http.StatusNotFound {
		t.Errorf("a user reached the stick: %d %s", res.StatusCode, data)
	}
}
