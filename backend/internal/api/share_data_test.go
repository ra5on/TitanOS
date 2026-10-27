package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/shares"
)

func TestShareDataDeletion(t *testing.T) {
	ts := newTestServer(t)
	if res, _ := ts.request("GET", "/api/v1/shares/Docs/delete-data", nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous preview: %d", res.StatusCode)
	}
	ts.setup()
	dir := ts.withShare()
	os.MkdirAll(filepath.Join(dir, "a"), 0o755)
	os.WriteFile(filepath.Join(dir, "a", "x.txt"), []byte("hello"), 0o644)
	sibling := filepath.Join(filepath.Dir(dir), "Keep")
	os.MkdirAll(sibling, 0o755)

	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	if res, _ := ts.do(guest, "GET", "/api/v1/shares/Docs/delete-data", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("user preview: %d", res.StatusCode)
	}
	if res, _ := ts.do(guest, "POST", "/api/v1/shares/Docs/delete-data", map[string]string{"path": dir}); res.StatusCode != http.StatusForbidden {
		t.Fatalf("user deletion: %d", res.StatusCode)
	}

	res, body := ts.request("GET", "/api/v1/shares/Docs/delete-data", nil)
	var preview shares.DataPreview
	json.Unmarshal(body, &preview)
	if res.StatusCode != http.StatusOK || preview.Path != dir || preview.Files != 1 || preview.Bytes != 5 || !preview.Complete || len(preview.Blockers) != 0 {
		t.Fatalf("preview: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/shares/Nope/delete-data", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown share: %d %s", res.StatusCode, body)
	}

	// A backup job that saves the share stands in the way.
	backups := ts.server.opts.Backup
	backups.Sources = func() []backup.Source { return []backup.Source{{Name: "Docs", Volume: "tank", Path: dir}} }
	if err := backups.Create(backup.Job{ID: "nightly", Sources: []string{"Docs"}, Destination: backup.Destination{Type: "sftp", Host: "backup.lan", User: "nas", Path: "/srv"}, Every: "daily", Time: "03:00"}, []string{"tank"}); err != nil {
		t.Fatal(err)
	}
	_, body = ts.request("GET", "/api/v1/shares/Docs/delete-data", nil)
	if !strings.Contains(string(body), `"kind":"backup-source","name":"nightly"`) {
		t.Fatalf("backup job not listed: %s", body)
	}
	res, body = ts.request("POST", "/api/v1/shares/Docs/delete-data", map[string]string{"path": dir})
	if res.StatusCode != http.StatusConflict || !strings.Contains(string(body), `"code":"share_in_use"`) || !strings.Contains(string(body), `"blockers"`) {
		t.Fatalf("blocked deletion: %d %s", res.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(dir, "a", "x.txt")); err != nil {
		t.Fatal("refused deletion touched the data")
	}
	if err := backups.Delete("nightly"); err != nil {
		t.Fatal(err)
	}

	if res, body := ts.request("POST", "/api/v1/shares/Docs/delete-data", map[string]string{"path": sibling}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "share_changed" {
		t.Fatalf("other path: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("POST", "/api/v1/shares/Docs/delete-data", map[string]string{"path": dir})
	if res.StatusCode != http.StatusAccepted || !strings.Contains(string(body), `"phase":"checking"`) {
		t.Fatalf("start: %d %s", res.StatusCode, body)
	}
	for range 500 {
		if !ts.shares.PurgeBusy() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatal("folder still there")
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatal("neighbour folder deleted")
	}
	_, body = ts.request("GET", "/api/v1/shares", nil)
	if strings.Contains(string(body), `"Docs"`) {
		t.Fatalf("share still listed: %s", body)
	}
	_, body = ts.request("GET", "/api/v1/operations", nil)
	if !strings.Contains(string(body), `"shareDeletions":[{`) || !strings.Contains(string(body), `"phase":"done"`) {
		t.Fatalf("operations: %s", body)
	}
	_, body = ts.request("GET", "/api/v1/notifications", nil)
	if !strings.Contains(string(body), "share_data_deleted") {
		t.Fatalf("no notification: %s", body)
	}
}
