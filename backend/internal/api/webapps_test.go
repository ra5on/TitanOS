package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/webapps"
)

func TestFileContent(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	dir := ts.withShare()
	put := func(path, expect string, body string) (*http.Response, []byte) {
		return ts.request("PUT", "/api/v1/files/content?root=share/Docs&path="+path+"&expect="+expect, []byte(body))
	}
	// New file: expect=none.
	res, body := put("Bericht.docx", "none", "v1")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	var saved struct {
		Modified time.Time `json:"modified"`
		MTime    string    `json:"mtime"`
		Size     int64     `json:"size"`
	}
	json.Unmarshal(body, &saved)
	if data, _ := os.ReadFile(filepath.Join(dir, "Bericht.docx")); string(data) != "v1" || saved.Size != 2 || saved.MTime == "" {
		t.Fatalf("%s %+v", data, saved)
	}
	if res, body := put("Bericht.docx", "none", "again"); res.StatusCode != http.StatusConflict || errorCode(t, body) != "file_exists" {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	// File times come from a coarse kernel clock; saves are seconds apart.
	time.Sleep(20 * time.Millisecond)
	// Save with the time it was opened: replaces it, keeps .prev.
	res, body = put("Bericht.docx", saved.MTime, "version two")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "Bericht.docx")); string(data) != "version two" {
		t.Fatal(string(data))
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "Bericht.docx.prev")); string(data) != "v1" {
		t.Fatal(string(data))
	}
	// The old time again: someone saved in between.
	if res, body := put("Bericht.docx", saved.MTime, "stale"); res.StatusCode != http.StatusConflict || errorCode(t, body) != "file_changed" {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	// The RFC 3339 "modified" of the listing (whole seconds) also works.
	info, _ := os.Stat(filepath.Join(dir, "Bericht.docx"))
	listed := info.ModTime().UTC().Truncate(time.Second).Format(time.RFC3339Nano)
	if res, body := put("Bericht.docx", listed, "three"); res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	// Changed on disk: refused.
	os.Chtimes(filepath.Join(dir, "Bericht.docx"), time.Now(), time.Now().Add(time.Hour))
	ns := strconv.FormatInt(info.ModTime().UnixNano(), 10)
	if res, body := put("Bericht.docx", ns, "four"); res.StatusCode != http.StatusConflict {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	for _, bad := range []struct{ path, expect string }{{"Bericht.docx", ""}, {"Bericht.docx", "yesterday"}, {"../x.docx", "none"}, {"a.prev", "none"}} {
		if res, body := put(bad.path, bad.expect, "x"); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%v: %d %s", bad, res.StatusCode, body)
		}
	}
	// Deleted since it was opened.
	if res, body := put("Gone.docx", ns, "x"); res.StatusCode != http.StatusConflict || errorCode(t, body) != "file_changed" {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	// Other roots than the account's are refused; the stick is read-only.
	if res, _ := ts.request("PUT", "/api/v1/files/content?root=share/Nope&path=a.txt&expect=none", []byte("x")); res.StatusCode < 400 {
		t.Fatal(res.StatusCode)
	}
	if res, _ := ts.request("PUT", "/api/v1/files/content?root=stick&path=a.txt&expect=none", []byte("x")); res.StatusCode != http.StatusForbidden {
		t.Fatal(res.StatusCode)
	}
}

func TestFileAppsPreference(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	good := map[string]string{"docx": "webapp:wordcraft", "pdf": "webapp:pdfcraft", "txt": "editor"}
	if res, body := ts.request("PUT", "/api/v1/preferences/fileapps", good); res.StatusCode != http.StatusNoContent {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	_, body := ts.request("GET", "/api/v1/preferences", nil)
	if !strings.Contains(string(body), `"fileapps":{`) {
		t.Fatal(string(body))
	}
	for _, bad := range []any{
		map[string]string{"docx": "webapp:nope"},
		map[string]string{".docx": "editor"},
		map[string]string{"DOCX": "editor"},
		map[string]string{"docx": "docker:wordcraft"},
		map[string]int{"docx": 1},
	} {
		if res, body := ts.request("PUT", "/api/v1/preferences/fileapps", bad); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%v: %d %s", bad, res.StatusCode, body)
		}
	}
}

func TestWebappsRoutes(t *testing.T) {
	var manager *webapps.Manager
	ts := newTestServerWith(t, func(o *Options) {
		manager = webapps.New(o.Store)
		manager.Dev, manager.Version = true, "1.2.3"
		manager.Load()
		t.Cleanup(manager.Close)
		o.Webapps = manager
	})
	// Not signed in.
	if res, _ := ts.request("GET", "/api/v1/webapps", nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatal(res.StatusCode)
	}
	ts.setup()
	res, body := ts.request("GET", "/api/v1/webapps", nil)
	var list struct {
		Apps []webapps.Status `json:"apps"`
	}
	if res.StatusCode != 200 || json.Unmarshal(body, &list) != nil || len(list.Apps) != 28 || list.Apps[0].ID != "t-word" || !list.Apps[0].Trusted || list.Apps[0].State != "missing" {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/webapps/nope/install", nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "webapp_unknown" {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if res, body := ts.request("DELETE", "/api/v1/webapps/wordcraft", nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "webapp_not_installed" {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	// An empty choice only records the answer.
	if res, body := ts.request("POST", "/api/v1/webapps/defaults/install", map[string]any{"apps": []string{}}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if data, err := ts.store.Read("webapps.json"); err != nil || !strings.Contains(string(data), `"offered": true`) {
		t.Fatal(string(data), err)
	}
	// Files of an app that is not installed: 404 without sign-in.
	anon := ts.newClient()
	if res, _ := ts.do(anon, "GET", "/webapps/wordcraft/0.16.0/index.html", nil); res.StatusCode != http.StatusNotFound || res.Header.Get("Content-Security-Policy") == "" {
		t.Fatal(res.StatusCode, res.Header)
	}
	// The mediator comes from the web UI and does not collide with the apps.
	res, body = ts.do(anon, "GET", "/webapps/_host/host.js", nil)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/javascript; charset=utf-8" || string(body) != "// host" ||
		res.Header.Get("Access-Control-Allow-Origin") != "*" || strings.Contains(res.Header.Get("Content-Security-Policy"), "default-src") {
		t.Fatal(res.StatusCode, res.Header, string(body))
	}
	// Never the UI's index.html for a missing file.
	for _, path := range []string{"/webapps/_host/missing.js", "/webapps/_host/index.html", "/webapps/_host/..%2findex.html"} {
		if res, _ := ts.do(anon, "GET", path, nil); res.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d", path, res.StatusCode)
		}
	}
}

func TestWebappsMigrate(t *testing.T) {
	var manager *webapps.Manager
	ts := newTestServerWith(t, func(o *Options) {
		o.Store.WriteJSON("webapps.json", map[string]any{"installed": []map[string]string{{"id": "pdfcraft", "version": "0.19.0", "volume": "tank"}}})
		manager = webapps.New(o.Store)
		manager.Dev, manager.Version = true, "1.2.3" // no -webapps-dir: installs fail
		manager.Load()
		t.Cleanup(manager.Close)
		o.Webapps = manager
	})
	ts.setup()
	var mu sync.Mutex
	var removed []string
	ts.server.migration.detect = func(context.Context) []webapps.DockerApp {
		return []webapps.DockerApp{{App: "wordcraft", Webapp: "wordcraft", Running: true, Image: "ghcr.io/storytold/wordcraft:0.15"}, {App: "pdf", Webapp: "pdfcraft"}}
	}
	ts.server.migration.remove = func(_ context.Context, name string) error {
		mu.Lock()
		defer mu.Unlock()
		removed = append(removed, name)
		return nil
	}
	_, body := ts.request("GET", "/api/v1/webapps", nil)
	if !strings.Contains(string(body), `"docker":[{"app":"wordcraft","webapp":"wordcraft","running":true,"image":"ghcr.io/storytold/wordcraft:0.15"}`) {
		t.Fatal(string(body))
	}
	// Only detected Craft apps; their web app must come or be there.
	for _, bad := range []map[string]any{
		{"install": []string{}, "removeDocker": []string{"nextcloud"}},
		{"install": []string{}, "removeDocker": []string{"wordcraft"}},
		{"install": []string{"nope"}, "removeDocker": []string{}},
	} {
		if res, body := ts.request("POST", "/api/v1/webapps/migrate", bad); res.StatusCode != http.StatusBadRequest {
			t.Fatalf("%v: %d %s", bad, res.StatusCode, body)
		}
	}
	res, body := ts.request("POST", "/api/v1/webapps/migrate", map[string]any{"install": []string{"wordcraft"}, "removeDocker": []string{"wordcraft", "pdf"}})
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	deadline := time.Now().Add(10 * time.Second)
	var st *MigrationStatus
	for time.Now().Before(deadline) {
		if st = ts.server.migration.status(); !st.Running {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// WordCraft did not install: its Docker app stays. PdfCraft is there:
	// its Docker app goes.
	if st.Running || st.Items[0].State != "kept" || st.Items[1].State != "removed" {
		t.Fatalf("%+v", st)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(removed) != 1 || removed[0] != "pdf" {
		t.Fatal(removed)
	}
	// Plain users see no Docker apps.
	ts.request("POST", "/api/v1/users", map[string]any{"name": "bo", "password": testPassword, "role": "user"})
	user := ts.newClient()
	ts.do(user, "POST", "/api/v1/login", credentials{Name: "bo", Password: testPassword})
	_, body = ts.do(user, "GET", "/api/v1/webapps", nil)
	if !strings.Contains(string(body), `"docker":[]`) || strings.Contains(string(body), `"app":"wordcraft"`) {
		t.Fatal(string(body))
	}
}
