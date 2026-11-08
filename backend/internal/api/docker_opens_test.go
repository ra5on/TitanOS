package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

func TestDockerFileAppsPreferenceIsAKindOfItsOwn(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if res, body := ts.request("PUT", "/api/v1/preferences/fileappsdocker", map[string]string{"3mf": "docker:bambu-lab", "stl": "docker:bambu-lab"}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	_, body := ts.request("GET", "/api/v1/preferences", nil)
	if !strings.Contains(string(body), `"fileappsdocker":{"3mf":"docker:bambu-lab"`) {
		t.Fatal(string(body))
	}
	for _, bad := range []any{
		map[string]string{"3mf": "webapp:wordcraft"},
		map[string]string{"3mf": "docker:../x"},
		map[string]string{".3mf": "docker:bambu"},
		map[string]string{"3mf": "docker:"},
		map[string]int{"3mf": 1},
	} {
		if res, body := ts.request("PUT", "/api/v1/preferences/fileappsdocker", bad); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%v: %d %s", bad, res.StatusCode, body)
		}
	}
	// fileapps itself keeps refusing Docker apps: titand 0.66 reads it after going back.
	if res, _ := ts.request("PUT", "/api/v1/preferences/fileapps", map[string]string{"3mf": "docker:bambu-lab"}); res.StatusCode != http.StatusBadRequest {
		t.Fatal(res.StatusCode)
	}
}

func TestDockerOpenRoutes(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	dir := ts.withShare()
	if err := os.WriteFile(filepath.Join(dir, "teil.stl"), []byte("solid x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Without an engine (development mode) the menu has no Docker apps.
	for _, q := range []string{"", "?root=share/Docs&path=teil.stl"} {
		res, body := ts.request("GET", "/api/v1/docker/openers"+q, nil)
		if res.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "[]" {
			t.Fatalf("%s: %d %s", q, res.StatusCode, body)
		}
	}
	// A file that is not there, or outside the account's roots.
	if res, _ := ts.request("POST", "/api/v1/docker/apps/bambu/open", map[string]string{"root": "share/Docs", "path": "nope.stl"}); res.StatusCode != http.StatusNotFound {
		t.Errorf("missing file: %d", res.StatusCode)
	}
	if res, _ := ts.request("POST", "/api/v1/docker/apps/bambu/open", map[string]string{"root": "share/Docs", "path": "../../etc/passwd"}); res.StatusCode < 400 {
		t.Errorf("escape: %d", res.StatusCode)
	}

	// A plain user may neither change opens nor mount shares, and may only
	// open with apps the account may use.
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, r := range []struct{ method, path string }{
		{"PUT", "/docker/apps/bambu/opens"},
		{"GET", "/docker/apps/bambu/opens"},
		{"POST", "/docker/apps/bambu/shares"},
		{"DELETE", "/docker/apps/bambu/shares/Docs"},
		{"POST", "/docker/apps/bambu/open"},
	} {
		if res, _ := ts.do(guest, r.method, "/api/v1"+r.path, map[string]any{"share": "Docs", "root": "share/Docs", "path": "teil.stl"}); res.StatusCode != http.StatusForbidden {
			t.Errorf("user %s %s: %d", r.method, r.path, res.StatusCode)
		}
	}
	// Unknown shares and the agent's per-account share are not mounted.
	if res, _ := ts.request("POST", "/api/v1/docker/apps/bambu/shares", map[string]any{"share": "Nope"}); res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown share: %d", res.StatusCode)
	}
}

func TestDockerOpenRoutesAreNotPublic(t *testing.T) {
	for _, route := range publicRoutes {
		if strings.Contains(route.Path, "/openers") || strings.Contains(route.Path, "/opens") || strings.HasSuffix(route.Path, "/open") && strings.Contains(route.Path, "/docker/") || strings.Contains(route.Path, "/docker/apps/{name}/shares") {
			t.Errorf("%s %s must stay out of publicRoutes", route.Method, route.Path)
		}
	}
}
