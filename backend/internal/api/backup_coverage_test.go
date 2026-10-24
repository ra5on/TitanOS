package api

import (
	"github.com/ra5on/TitanOS/backend/internal/backup"
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
