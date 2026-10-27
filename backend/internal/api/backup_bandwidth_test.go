package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestBackupBandwidthValidation(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	job := map[string]any{"id": "cloud", "sources": []string{"media"}, "enabled": false, "every": "daily", "time": "02:30",
		"destination": map[string]any{"type": "s3", "endpoint": "https://s3.example.com", "bucket": "nas-backup", "accessKey": "AKIA1", "secretKey": "s3-secret-value"}}
	for _, bad := range []map[string]any{{"limit": -1}, {"limit": 10001}, {"limit": 5, "windowFrom": "22:00"}, {"limit": 5, "windowFrom": "22:00", "windowTo": "22:00"}, {"windowLimit": 5}, {"limit": 5, "windowFrom": "7:00", "windowTo": "08:00"}} {
		job["bandwidth"] = bad
		if res, body := ts.request("POST", "/api/v1/backups", job); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_backup_job" {
			t.Errorf("%v accepted: %d %s", bad, res.StatusCode, body)
		}
	}
	job["bandwidth"] = map[string]any{"limit": 10, "windowFrom": "22:00", "windowTo": "06:00", "windowLimit": 0}
	if res, body := ts.request("POST", "/api/v1/backups", job); res.StatusCode != http.StatusOK {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	var list struct {
		Jobs []struct {
			Job struct {
				Bandwidth *struct {
					Limit      int    `json:"limit"`
					WindowFrom string `json:"windowFrom"`
					WindowTo   string `json:"windowTo"`
				} `json:"bandwidth"`
			} `json:"job"`
		} `json:"jobs"`
	}
	read := func() {
		list.Jobs = nil
		res, body := ts.request("GET", "/api/v1/backups", nil)
		if res.StatusCode != http.StatusOK || json.Unmarshal(body, &list) != nil || len(list.Jobs) != 1 {
			t.Fatalf("list: %d %s", res.StatusCode, body)
		}
	}
	read()
	if b := list.Jobs[0].Job.Bandwidth; b == nil || b.Limit != 10 || b.WindowFrom != "22:00" || b.WindowTo != "06:00" {
		t.Fatalf("stored: %+v", b)
	}
	// Turning it off removes the field.
	job["bandwidth"] = map[string]any{"limit": 0}
	if res, body := ts.request("PUT", "/api/v1/backups/cloud", job); res.StatusCode != http.StatusOK {
		t.Fatalf("update: %d %s", res.StatusCode, body)
	}
	read()
	if list.Jobs[0].Job.Bandwidth != nil {
		t.Errorf("off kept: %+v", list.Jobs[0].Job.Bandwidth)
	}
}
