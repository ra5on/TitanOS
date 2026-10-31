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

func TestDiskSleepOverridesAPI(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.withShare() // volume "tank"
	ts.disks.UseRunner(&smartSystem{})
	put := func(body any) (int, storage.DiskSleepStatus, string) {
		res, data := ts.request("PUT", "/api/v1/storage/disk-sleep", body)
		var status storage.DiskSleepStatus
		json.Unmarshal(data, &status)
		return res.StatusCode, status, string(data)
	}
	code, status, body := put(map[string]any{"idleMinutes": 30, "volumes": map[string]int{"tank": 0}, "disks": map[string]int{"serial:WD-2": 60}})
	if code != http.StatusOK || status.Volumes["tank"] != 0 || status.Disks["serial:WD-2"] != 60 || len(status.DiskStatus) != 2 || len(status.VolumeStatus) != 1 {
		t.Fatalf("save overrides: %d %s", code, body)
	}
	for _, disk := range status.DiskStatus {
		if disk.Name == "sdb" && (disk.ID != "serial:WD-2" || disk.Effective != 60 || disk.Origin != storage.SleepFromDisk) ||
			disk.Name == "sda" && (disk.Effective != 30 || disk.Origin != storage.SleepFromDefault) {
			t.Fatalf("effective values: %+v", disk)
		}
	}
	// Only the default: the overrides stay (older clients, MCP).
	if code, status, body := put(map[string]int{"idleMinutes": 10}); code != http.StatusOK || status.IdleMinutes != 10 || len(status.Volumes) != 1 || status.Volumes["tank"] != 0 || status.Disks["serial:WD-2"] != 60 {
		t.Fatalf("default only: %d %s", code, body)
	}
	for _, bad := range []map[string]any{
		{"idleMinutes": 10, "volumes": map[string]int{"other": 5}},
		{"idleMinutes": 10, "disks": map[string]int{"sda": 5}},
		{"idleMinutes": 10, "disks": map[string]int{"serial:unknown": 5}},
		{"idleMinutes": 10, "volumes": map[string]int{"tank": 7}},
		{"idleMinutes": 10, "disks": map[string]any{"serial:WD-1": "5"}},
		{"volumes": map[string]int{"tank": 5}},
	} {
		if code, _, body := put(bad); code != http.StatusBadRequest {
			t.Errorf("accepted %v: %d %s", bad, code, body)
		}
	}
	res, data := ts.request("GET", "/api/v1/storage/disk-sleep", nil)
	json.Unmarshal(data, &status)
	if res.StatusCode != http.StatusOK || status.IdleMinutes != 10 || status.Volumes["tank"] != 0 || status.Disks["serial:WD-2"] != 60 {
		t.Fatalf("refused requests changed settings: %s", data)
	}
	// Empty maps remove all overrides.
	if code, status, body := put(map[string]any{"idleMinutes": 10, "volumes": map[string]int{}, "disks": map[string]int{}}); code != http.StatusOK || status.Volumes != nil || status.Disks != nil {
		t.Fatalf("remove overrides: %d %s", code, body)
	}
}
