package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

var snapshotFolderRoutes = [][2]string{
	{"POST", "/storage/volumes/data/snapshots/before/folders/preview"},
	{"POST", "/storage/volumes/data/snapshots/before/folders/restore"},
	{"GET", "/storage/snapshot-restores"},
	{"POST", "/storage/snapshot-restores/0123/cancel"},
	{"DELETE", "/storage/snapshot-restores/0123"},
}

func TestSnapshotFolderRoutesNeedAdministrator(t *testing.T) {
	ts := newTestServer(t)
	for _, route := range snapshotFolderRoutes {
		if res, _ := ts.request(route[0], "/api/v1"+route[1], map[string]any{}); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %v: %d", route, res.StatusCode)
		}
	}
	ts.setup()
	ts.users.Create("other", testPassword, auth.RoleUser)
	client := ts.newClient()
	ts.do(client, "POST", "/api/v1/login", credentials{Name: "other", Password: testPassword})
	for _, route := range snapshotFolderRoutes {
		if res, body := ts.do(client, route[0], "/api/v1"+route[1], map[string]any{"path": "photos", "mode": "copy"}); res.StatusCode != http.StatusForbidden {
			t.Fatalf("user %v: %d %s", route, res.StatusCode, body)
		}
	}
}

func TestSnapshotFolderRoutesForAdministrators(t *testing.T) {
	ts := newTestServer(t)
	log := activity.New(ts.store)
	ts.server.opts.Activity = log
	ts.setup()
	ts.users.Create("helper", testPassword, auth.RoleAdmin)
	admin := ts.newClient()
	ts.do(admin, "POST", "/api/v1/login", credentials{Name: "helper", Password: testPassword})

	res, body := ts.do(admin, "GET", "/api/v1/storage/snapshot-restores", nil)
	var list struct {
		Restores []map[string]any `json:"restores"`
	}
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &list) != nil || list.Restores == nil || len(list.Restores) != 0 {
		t.Fatalf("list: %d %s", res.StatusCode, body)
	}
	for _, path := range []string{"/storage/snapshot-restores/0123/cancel"} {
		if res, body := ts.do(admin, "POST", "/api/v1"+path, nil); res.StatusCode != http.StatusNotFound || !strings.Contains(string(body), "snapshot_restore_not_found") {
			t.Fatalf("%s: %d %s", path, res.StatusCode, body)
		}
	}
	if res, body := ts.do(admin, "DELETE", "/api/v1/storage/snapshot-restores/0123", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("forget: %d %s", res.StatusCode, body)
	}
	// No such volume here: refused before anything is opened.
	for _, kind := range []string{"preview", "restore"} {
		res, body := ts.do(admin, "POST", "/api/v1/storage/volumes/data/snapshots/before/folders/"+kind, map[string]any{"path": "photos", "mode": "original", "conflict": "skip", "uuid": "x"})
		if res.StatusCode < 400 || res.StatusCode == http.StatusForbidden {
			t.Fatalf("%s: %d %s", kind, res.StatusCode, body)
		}
	}
	res, body = ts.do(admin, "GET", "/api/v1/operations", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"snapshotRestores":[]`) {
		t.Fatalf("operations: %d %s", res.StatusCode, body)
	}

	rows := activityEntries(t, log)
	if _, ok := findActivity(rows, "POST /api/v1/storage/volumes/{name}/snapshots/{snapshot}/folders/preview"); ok {
		t.Fatal("preview recorded")
	}
	e, ok := findActivity(rows, "POST /api/v1/storage/volumes/{name}/snapshots/{snapshot}/folders/restore")
	if !ok || e.User != "helper" || e.OK || !strings.Contains(e.Target, "data") || !strings.Contains(e.Target, "before") || !strings.Contains(e.Target, "photos") {
		t.Fatalf("restore not recorded: %+v %v", e, ok)
	}
}
