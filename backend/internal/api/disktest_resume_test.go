package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// Continuing a disk test and dismissing the notices about dropped
// checkpoints belong to owners and administrators only.
func TestDiskTestResumeRoutes(t *testing.T) {
	ts := newTestServer(t)
	routes := []struct{ method, path string }{{"POST", "/storage/tests/sda/resume"}, {"DELETE", "/storage/test-notices"}}
	for _, route := range routes {
		if res, _ := ts.request(route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s: %d", route.path, res.StatusCode)
		}
	}
	ts.setup()
	if res, body := ts.request("GET", "/api/v1/storage/tests", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"notices":[]`) || !strings.Contains(string(body), `"tests":[]`) {
		t.Fatalf("list: %d %s", res.StatusCode, body)
	}
	// Nothing waits on sda: nothing is continued, and no disk is looked at.
	if res, body := ts.request("POST", "/api/v1/storage/tests/sda/resume", map[string]string{"confirm": "sda"}); res.StatusCode != http.StatusNotFound || !strings.Contains(string(body), "disk_test_not_found") {
		t.Fatalf("resume without a waiting test: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("DELETE", "/api/v1/storage/test-notices", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"notices":[]`) {
		t.Fatalf("dismiss notices: %d %s", res.StatusCode, body)
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, route := range routes {
		if res, _ := ts.do(guest, route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Fatalf("user %s: %d", route.path, res.StatusCode)
		}
	}
}
