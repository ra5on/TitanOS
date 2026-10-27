package api

import (
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"net/http"
	"testing"
	"time"
)

func TestCoverageRetainsPartialManualOfflineAndProbeFailures(t *testing.T) {
	now := time.Now()
	old := now.Add(-72 * time.Hour)
	jobs := []backup.View{{Job: backup.Job{ID: "copy", Sources: []string{"Photos"}, Folders: map[string][]string{"Photos": {"selected"}}, Destination: backup.Destination{Type: "local", Volume: "missing"}}, State: backup.State{LastOK: &old, Probe: &backup.ProbeResult{At: now, Error: "corrupt"}}}}
	item := coverageFor("share", "Photos", true, jobs, []string{"data"}, now)
	if item.Status != "attention" || len(item.Jobs) != 1 || !item.Jobs[0].Partial || item.Jobs[0].Automatic || *item.Jobs[0].DestinationAvailable {
		t.Fatal(item)
	}
	if coverageFor("share", "Other", true, jobs, nil, now).Status != "unconfigured" {
		t.Fatal("invented coverage")
	}
}
func TestCSVTextDoesNotTurnDeviceNamesIntoSpreadsheetFormulas(t *testing.T) {
	for _, s := range []string{"=cmd()", "+1", "@value", "-value", "\t=1"} {
		if csvText(s) == s {
			t.Fatal(s)
		}
	}
}

// /backups/smb and /backups/target are fixed routes next to /backups/{id}. A
// job form sent there is a job change, never the settings of that route.
func TestJobFormsOnFixedBackupRoutes(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	job := map[string]any{"id": "target", "sources": []string{"media"}, "enabled": false, "every": "daily", "time": "02:30",
		"destination": map[string]any{"type": "s3", "endpoint": "https://s3.example.com", "bucket": "nas-backup", "accessKey": "AKIA1", "secretKey": "s3-secret-value"}}
	if res, body := ts.request("POST", "/api/v1/backups", job); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_backup_job" {
		t.Errorf("reserved name accepted: %d %s", res.StatusCode, body)
	}
	for _, route := range []string{"/api/v1/backups/target", "/api/v1/backups/smb"} {
		if res, body := ts.request("PUT", route, job); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "backup_job_not_found" {
			t.Errorf("%s: job form not treated as a job: %d %s", route, res.StatusCode, body)
		}
	}
	// The settings themselves still reach their handler.
	if res, body := ts.request("PUT", "/api/v1/backups/target", map[string]any{"enabled": true, "volume": "tank"}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "read_only" {
		t.Errorf("target settings: %d %s", res.StatusCode, body)
	}
}
