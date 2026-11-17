package api

import (
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/update"
)

// Owner, 17 Nov 2026: asked whether there is an update, the agent took the
// base 0.99 for the running version. update_status names the running
// version as TitanOS shows it and says in one sentence what holds.
func TestUpdateStatusNamesTheRunningVersion(t *testing.T) {
	checked := time.Date(2026, 11, 17, 8, 15, 0, 0, time.Local)
	app := update.Status{
		Version:   "1.4.0",
		Base:      "0.82.0",
		LastCheck: &checked,
		Slots: []update.Slot{
			{Name: "a", Version: "0.82.0", Running: true, Complete: true},
			{Name: "b", Version: "0.99.0", Complete: true},
		},
	}
	res := updateStatusView(app)
	if res["running"] != "1.04" || res["upToDate"] != true {
		t.Fatalf("running %v, upToDate %v", res["running"], res["upToDate"])
	}
	answer, _ := res["answer"].(string)
	if !strings.Contains(answer, "1.04") || strings.Contains(answer, "0.99") || strings.Contains(answer, "0.82") || !strings.Contains(answer, "kein neueres Release") {
		t.Fatalf("answer: %q", answer)
	}
	for _, old := range []string{"version", "baseVersion", "slots", "newest"} {
		if _, ok := res[old]; ok {
			t.Fatalf("%s is still in the result: %v", old, res)
		}
	}
	if res["systemImageBase"] != "0.82" {
		t.Fatalf("systemImageBase: %v", res["systemImageBase"])
	}
	if note, _ := res["note"].(string); !strings.Contains(note, "not the running version") {
		t.Fatalf("note: %q", note)
	}

	newer := app
	newer.Available = &update.Manifest{Version: "1.5.0"}
	newer.Kind = update.KindApp
	res = updateStatusView(newer)
	if res["newest"] != "1.05" || res["upToDate"] != false || !strings.Contains(res["answer"].(string), "1.05 ist verfügbar") {
		t.Fatalf("newer: %v", res)
	}

	// The running version with the other kernel is no newer release.
	kernel := app
	kernel.Available = &update.Manifest{Version: "1.4.0"}
	kernel.KernelSwitch = true
	res = updateStatusView(kernel)
	if _, ok := res["newest"]; ok || res["upToDate"] != true {
		t.Fatalf("kernel switch: %v", res)
	}

	// A failed check or none at all: never "up to date".
	failed := app
	failed.CheckError = "github.com not reachable"
	res = updateStatusView(failed)
	if res["upToDate"] != false || strings.Contains(res["answer"].(string), "kein neueres") || !strings.Contains(res["answer"].(string), "konnte nicht prüfen") {
		t.Fatalf("failed check: %v", res)
	}
	never := app
	never.LastCheck = nil
	res = updateStatusView(never)
	if res["upToDate"] != false || strings.Contains(res["answer"].(string), "kein neueres") {
		t.Fatalf("never checked: %v", res)
	}
}
