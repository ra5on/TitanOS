package api

import (
	"net/http"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/fileindex"
	"github.com/ra5on/TitanOS/backend/internal/thumbs"
)

// Thumbnails, the photo list and content search resolve the root with the
// account's rights like a download; the index settings are for administrators.
func TestThumbnailAndIndexRights(t *testing.T) {
	ts := newTestServer(t)
	ts.server.opts.Thumbs = thumbs.New()
	ts.server.opts.FileIndex = fileindex.New(ts.store)
	gets := []string{"/api/v1/files/thumb?root=stick&path=a.jpg", "/api/v1/files/photos?root=stick&path=", "/api/v1/files/index"}
	for _, path := range gets {
		if res, _ := ts.request("GET", path, nil); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s: %d", path, res.StatusCode)
		}
	}
	ts.setup()
	if res, data := ts.request("GET", "/api/v1/files/thumb?root=stick&path=users/x.jpg", nil); res.StatusCode != http.StatusUnsupportedMediaType || errorCode(t, data) != "thumb_unsupported" {
		t.Fatalf("stick thumbnail: %d %s", res.StatusCode, data)
	}
	if res, data := ts.request("GET", "/api/v1/files/thumb?root=share/fotos&path=a.jpg", nil); res.StatusCode != http.StatusNotFound || errorCode(t, data) != "file_not_found" {
		t.Fatalf("unknown root: %d %s", res.StatusCode, data)
	}
	if res, data := ts.request("POST", "/api/v1/files/search", map[string]any{"root": "stick", "query": "x", "content": true}); res.StatusCode != http.StatusConflict || errorCode(t, data) != "index_unavailable" {
		t.Fatalf("content search on the stick: %d %s", res.StatusCode, data)
	}
	if res, data := ts.request("GET", "/api/v1/files/index", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("index status: %d %s", res.StatusCode, data)
	}
	if res, data := ts.request("PUT", "/api/v1/files/index", map[string]any{"enabled": true, "time": "99:99"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid time: %d %s", res.StatusCode, data)
	}
	if res, data := ts.request("PUT", "/api/v1/files/index", map[string]any{"enabled": true, "volumes": []string{"data"}}); res.StatusCode != http.StatusOK {
		t.Fatalf("save: %d %s", res.StatusCode, data)
	}
	if _, err := ts.store.Read("files/index.json"); err != nil {
		t.Fatal("settings not on the stick")
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, path := range []string{"/api/v1/files/index"} {
		if res, _ := ts.do(guest, "GET", path, nil); res.StatusCode != http.StatusForbidden {
			t.Errorf("user reached %s: %d", path, res.StatusCode)
		}
	}
	if res, _ := ts.do(guest, "PUT", "/api/v1/files/index", map[string]any{"enabled": false}); res.StatusCode != http.StatusForbidden {
		t.Errorf("user changed the index: %d", res.StatusCode)
	}
	if res, _ := ts.do(guest, "GET", "/api/v1/files/thumb?root=stick&path=a.jpg", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("user reached the stick: %d", res.StatusCode)
	}
}
