package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/storage"
)

func TestDiskSleepSettingsAPI(t *testing.T) {
	ts := newTestServer(t)
	for _, method := range []string{"GET", "PUT"} {
		if res, _ := ts.request(method, "/api/v1/storage/disk-sleep", nil); res.StatusCode != http.StatusUnauthorized {
			t.Fatal(res.StatusCode)
		}
	}
	ts.setup()
	for _, minutes := range []int{5, 10, 15, 30, 60, 120, 180, 0} {
		res, body := ts.request("PUT", "/api/v1/storage/disk-sleep", map[string]int{"idleMinutes": minutes})
		var status storage.DiskSleepStatus
		if json.Unmarshal(body, &status) != nil || res.StatusCode != http.StatusOK || status.IdleMinutes != minutes || !status.Available {
			t.Fatalf("save %d: %d %s", minutes, res.StatusCode, body)
		}
	}
	for _, body := range []any{map[string]int{"idleMinutes": -1}, map[string]int{"idleMinutes": 6}, map[string]any{}, map[string]any{"idleMinutes": nil}, map[string]any{"idleMinutes": 1.5}} {
		if res, data := ts.request("PUT", "/api/v1/storage/disk-sleep", body); res.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid setting: %d %s", res.StatusCode, data)
		}
	}
	if _, err := ts.users.Create("guest", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "guest", Password: testPassword})
	for _, method := range []string{"GET", "PUT"} {
		if res, _ := ts.do(guest, method, "/api/v1/storage/disk-sleep", map[string]int{"idleMinutes": 5}); res.StatusCode != http.StatusForbidden {
			t.Fatal("plain user", res.StatusCode)
		}
	}
	res, body := ts.request("GET", "/api/v1/storage/disk-sleep", nil)
	var status storage.DiskSleepStatus
	json.Unmarshal(body, &status)
	if res.StatusCode != http.StatusOK || status.IdleMinutes != 0 {
		t.Fatalf("rejected request changed settings: %d %s", res.StatusCode, body)
	}
}
