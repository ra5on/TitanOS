package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/nvr"
)

// PLAN 88: "Behalten" over the API – administrators only; the video is
// an export every account with the camera's recordings sees; deleting a
// kept event asks first.
func TestNVRKeepEvent(t *testing.T) {
	ts, _ := nvrServer(t)
	ts.setup()
	nvrRunning(t, ts)
	guest := ts.guest("gast")
	grant(t, ts, "gast", map[string]string{"haustuer": "recordings"})
	var all eventList
	getJSON(t, ts, ts.client, "/nvr/events?camera=haustuer&limit=200", &all)
	var target, running nvr.Event
	for _, e := range all.Events {
		if e.End == nil && running.ID == "" {
			running = e
		}
		if e.End != nil && !e.Kept && target.ID == "" && float64(time.Now().Unix())-*e.End > 300 && float64(time.Now().Unix())-e.Start < 6*3600 {
			target = e
		}
	}
	if target.ID == "" || running.ID == "" {
		t.Fatalf("events: %+v", all.Events)
	}
	path := "/api/v1/nvr/events/" + target.ID + "/keep"
	if res, _ := ts.do(guest, "POST", path, nil); res.StatusCode != http.StatusForbidden {
		t.Errorf("an account keeps: %d", res.StatusCode)
	}
	if res, body := ts.request("POST", "/api/v1/nvr/events/"+running.ID+"/keep", nil); res.StatusCode != http.StatusConflict || errorCode(t, body) != "nvr_event_running" {
		t.Errorf("running: %d %s", res.StatusCode, body)
	}
	res, body := ts.request("POST", path, nil)
	var kept nvr.Event
	if res.StatusCode != 200 || json.Unmarshal(body, &kept) != nil || !kept.Kept || kept.KeptExport == "" {
		t.Fatalf("keep: %d %s", res.StatusCode, body)
	}
	// The account sees the kept event and its video, and may play it.
	var seen nvr.Event
	if getJSON(t, ts, guest, "/nvr/events/"+target.ID, &seen); !seen.Kept || seen.KeptExport != kept.KeptExport {
		t.Errorf("account sees %+v", seen)
	}
	var exports struct {
		Exports []nvr.Export `json:"exports"`
	}
	for range 3 { // the stand-in finishes an export after two lists
		getJSON(t, ts, guest, "/nvr/exports", &exports)
	}
	found := false
	for _, e := range exports.Exports {
		found = found || (e.ID == kept.KeptExport && e.Event == target.ID && e.State == nvr.ExportDone)
	}
	if !found {
		t.Fatalf("the account does not see the kept video: %+v", exports.Exports)
	}
	if res, _ := ts.do(guest, "GET", "/api/v1/nvr/exports/"+kept.KeptExport+"/download", nil); res.StatusCode != 200 {
		t.Errorf("account plays the kept video: %d", res.StatusCode)
	}
	if res, body := ts.do(guest, "DELETE", "/api/v1/nvr/exports/"+kept.KeptExport, nil); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "nvr_export_kept" {
		t.Errorf("account deletes the kept video: %d %s", res.StatusCode, body)
	}
	// Without the camera's recordings: neither.
	grant(t, ts, "gast", map[string]string{"haustuer": "live"})
	if res, _ := ts.do(guest, "GET", "/api/v1/nvr/exports/"+kept.KeptExport+"/download", nil); res.StatusCode != 404 {
		t.Errorf("without the right: %d", res.StatusCode)
	}
	// Deleting asks first.
	if res, body := ts.request("DELETE", "/api/v1/nvr/events/"+target.ID, nil); res.StatusCode != http.StatusConflict || errorCode(t, body) != "nvr_event_kept" {
		t.Errorf("delete kept without asking: %d %s", res.StatusCode, body)
	}
	// Un-keep gives it back to the camera's days, its video goes.
	res, body = ts.request("DELETE", path, nil)
	var back nvr.Event
	if res.StatusCode != 200 || json.Unmarshal(body, &back) != nil || back.Kept || back.KeptExport != "" {
		t.Fatalf("unkeep: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("DELETE", "/api/v1/nvr/events/"+target.ID, nil); res.StatusCode != http.StatusNoContent {
		t.Errorf("delete after unkeep: %d", res.StatusCode)
	}
	// Kept again and deleted with the confirmation.
	var other nvr.Event
	for _, e := range all.Events {
		if e.End != nil && !e.Kept && e.ID != target.ID && float64(time.Now().Unix())-*e.End > 300 && float64(time.Now().Unix())-e.Start < 6*3600 {
			other = e
			break
		}
	}
	if res, _ := ts.request("POST", "/api/v1/nvr/events/"+other.ID+"/keep", nil); res.StatusCode != 200 {
		t.Fatalf("keep other: %d", res.StatusCode)
	}
	if res, _ := ts.request("DELETE", "/api/v1/nvr/events/"+other.ID+"?confirmKept=1", nil); res.StatusCode != http.StatusNoContent {
		t.Errorf("delete confirmed: %d", res.StatusCode)
	}
}

// PLAN 88: "seen" is the account's own.
func TestNVRSeenPerAccount(t *testing.T) {
	ts, _ := nvrServer(t)
	ts.setup()
	nvrRunning(t, ts)
	guest := ts.guest("gast")
	grant(t, ts, "gast", map[string]string{"haustuer": "recordings"})
	var mine, owner eventList
	getJSON(t, ts, guest, "/nvr/events?reviewed=0&limit=200", &mine)
	if len(mine.Events) == 0 {
		t.Fatal("nothing unseen")
	}
	event := mine.Events[len(mine.Events)-1]
	if res, _ := ts.do(guest, "POST", "/api/v1/nvr/events/"+event.ID+"/reviewed", map[string]any{"reviewed": true}); res.StatusCode != 200 {
		t.Fatalf("mark: %d", res.StatusCode)
	}
	var one nvr.Event
	if getJSON(t, ts, guest, "/nvr/events/"+event.ID, &one); !one.Reviewed {
		t.Error("not seen by the account that marked it")
	}
	if getJSON(t, ts, ts.client, "/nvr/events/"+event.ID, &one); one.Reviewed {
		t.Error("seen by the owner too (NVR-wide)")
	}
	// All seen: for the account that asks only.
	if res, body := ts.do(guest, "POST", "/api/v1/nvr/events/seen", map[string]any{}); res.StatusCode != 200 {
		t.Fatalf("all seen: %d %s", res.StatusCode, body)
	}
	getJSON(t, ts, guest, "/nvr/events?reviewed=0&limit=200", &mine)
	for _, e := range mine.Events {
		if e.End != nil {
			t.Errorf("unseen after all seen: %+v", e)
		}
	}
	getJSON(t, ts, ts.client, "/nvr/events?reviewed=0&limit=200", &owner)
	if len(owner.Events) < 2 {
		t.Errorf("the owner's unseen events went too: %d", len(owner.Events))
	}
}

// PLAN 88: a camera with zones, masks, both days and unknown faces over
// the API; checked on the server, lists replaced as a whole.
func TestNVRCameraZonesAndDays(t *testing.T) {
	ts, _ := nvrServer(t)
	ts.setup()
	nvrRunning(t, ts)
	square := [][]float64{{0.1, 0.1}, {0.5, 0.1}, {0.5, 0.5}, {0.1, 0.5}}
	change := map[string]any{
		"zones":     []map[string]any{{"name": "Weg", "points": square}, {"name": "Beet", "points": square}},
		"zonesOnly": true,
		"masks":     map[string]any{"motion": []map[string]any{{"points": square}}, "objects": []map[string]any{{"points": square}}},
		"record":    map[string]any{"mode": "continuous", "recordDays": 7, "eventDays": 30, "preSeconds": 5, "postSeconds": 5},
		"notify":    map[string]any{"faceUnknown": true, "zones": []string{"beet"}},
	}
	res, body := ts.request("PUT", "/api/v1/nvr/cameras/haustuer", change)
	if res.StatusCode != 200 {
		t.Fatalf("change: %d %s", res.StatusCode, body)
	}
	var view nvr.CameraView
	getJSON(t, ts, ts.client, "/nvr/cameras/haustuer", &view)
	if len(view.Zones) != 2 || view.Zones[0].ID != "weg" || !view.ZonesOnly || len(view.Masks.Motion) != 1 || len(view.Masks.Objects) != 1 ||
		view.Record.RecordDays != 7 || view.Record.EventDays != 30 || view.Record.RetainDays != 30 || !view.Notify.FaceUnknown || len(view.Notify.Zones) != 1 {
		t.Fatalf("view %+v", view)
	}
	// One zone fewer: the list is replaced; the messages' zone must go too.
	res, body = ts.request("PUT", "/api/v1/nvr/cameras/haustuer", map[string]any{"zones": []map[string]any{{"id": "weg", "name": "Weg", "points": square}}})
	if res.StatusCode != 400 || errorCode(t, body) != "nvr_invalid" {
		t.Errorf("removed zone still chosen for messages: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("PUT", "/api/v1/nvr/cameras/haustuer", map[string]any{
		"zones": []map[string]any{{"id": "weg", "name": "Weg", "points": square[:3]}}, "notify": map[string]any{"zones": []string{}},
		"masks": map[string]any{"motion": []map[string]any{}},
	})
	if res.StatusCode != 200 {
		t.Fatalf("fewer zones: %d %s", res.StatusCode, body)
	}
	getJSON(t, ts, ts.client, "/nvr/cameras/haustuer", &view)
	if len(view.Zones) != 1 || len(view.Zones[0].Points) != 3 || len(view.Masks.Motion) != 0 || len(view.Masks.Objects) != 1 {
		t.Errorf("after the change %+v", view)
	}
	// Fewer days ask first.
	res, body = ts.request("PUT", "/api/v1/nvr/cameras/haustuer", map[string]any{"record": map[string]any{"mode": "continuous", "recordDays": 7, "eventDays": 10, "preSeconds": 5, "postSeconds": 5}})
	if res.StatusCode != http.StatusConflict || errorCode(t, body) != "nvr_recordings_shrink" {
		t.Errorf("fewer event days: %d %s", res.StatusCode, body)
	}
	for name, bad := range map[string]map[string]any{
		"point outside": {"zones": []map[string]any{{"name": "A", "points": [][]float64{{0, 0}, {2, 0}, {1, 1}}}}},
		"two points":    {"masks": map[string]any{"objects": []map[string]any{{"points": square[:2]}}}},
		"no name":       {"zones": []map[string]any{{"name": "", "points": square}}},
		"days":          {"record": map[string]any{"mode": "continuous", "recordDays": 400, "eventDays": 30, "preSeconds": 5, "postSeconds": 5}},
	} {
		if res, body := ts.request("PUT", "/api/v1/nvr/cameras/haustuer", bad); res.StatusCode != 400 || errorCode(t, body) != "nvr_invalid" {
			t.Errorf("%s: %d %s", name, res.StatusCode, body)
		}
	}
}
