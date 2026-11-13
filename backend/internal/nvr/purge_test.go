package nvr

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// Owner 13.11.2026: "the old recordings of removed cameras cannot be
// deleted". PurgeDeletedCamera deletes them now: only that camera's
// recordings (in steps, from the oldest), then the camera in Frigate with
// its events (exports stay), then the stub leaves the stick and config.yml.
func TestPurgeDeletedCamera(t *testing.T) {
	r, first := capRig(t)
	ctx := context.Background()
	// A camera that exists has nothing to purge.
	if err := r.m.PurgeDeletedCamera(ctx, "einfahrt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("purge of a camera that exists: %v", err)
	}
	// An export of the camera, made by an account, finished.
	made, err := r.m.CreateExport(ctx, "anna", "einfahrt", first, first+600, "Lieferung")
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := r.m.Exports(ctx, "anna", false); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.m.RemoveCamera("einfahrt"); err != nil {
		t.Fatal(err)
	}
	r.look()
	r.look()
	if r.phase() != PhaseRunning {
		t.Fatalf("phase %s", r.phase())
	}
	exportsBefore := r.exportCount()
	if err := r.m.PurgeDeletedCamera(ctx, "einfahrt"); err != nil {
		t.Fatal(err)
	}

	// Every deletion asked for this camera only, from before its oldest
	// hour to after now, in steps of at most six hours.
	spans := r.deleted()
	if len(spans) == 0 {
		t.Fatal("no recordings deleted")
	}
	if spans[0][0] > first || spans[len(spans)-1][1] < float64(r.now.Unix()) {
		t.Errorf("deleted %v, first hour %v, now %v", spans, first, r.now.Unix())
	}
	for i, s := range spans {
		if s[1]-s[0] > purgeStep.Seconds() || (i > 0 && s[0] != spans[i-1][1]) {
			t.Errorf("span %d: %v", i, s)
		}
	}
	if days, err := r.m.RecordingsSummary(ctx, "einfahrt"); err == nil && len(days) != 0 {
		t.Errorf("recordings of the removed camera left: %v", days)
	}
	// The other camera keeps all of its recordings.
	if days, err := r.m.RecordingsSummary(ctx, "haustuer"); err != nil || hoursIn(days) != 10 {
		t.Errorf("haustuer: %d hours, %v", hoursIn(days), err)
	}
	// Frigate deleted the camera itself, without its exports.
	r.frigate.mu.Lock()
	purged := slices.Clone(r.frigate.data.purged)
	events := slices.ContainsFunc(r.frigate.data.events, func(e devEvent) bool { return e.Camera == "einfahrt" })
	r.frigate.mu.Unlock()
	if len(purged) != 1 || purged[0] != "einfahrt?delete_exports=false" || events {
		t.Errorf("camera deletion in Frigate: %v, events left %v", purged, events)
	}
	if got := r.exportCount(); got != exportsBefore {
		t.Errorf("exports %d, before %d", got, exportsBefore)
	}
	// Frigate lists exports only of the cameras in its config: the export
	// is asked for by its ID and stays the account's, done and with its
	// file (it vanished and lost its owner before, review PLAN 85-4).
	for range 2 {
		list, err := r.m.Exports(ctx, "anna", false)
		if err != nil {
			t.Fatal(err)
		}
		at := slices.IndexFunc(list, func(e Export) bool { return e.ID == made.ID })
		if at < 0 || list[at].State != ExportDone || list[at].Owner != "anna" || list[at].Camera != "einfahrt" {
			t.Fatalf("the export after the purge: %+v", list)
		}
		if video, _, err := list[at].ExportMedia(); err != nil || video == "" {
			t.Errorf("its file: %q %v", video, err)
		}
	}

	// Gone from the list, the stick and, after the next look, config.yml.
	if len(r.m.DeletedCameras()) != 0 {
		t.Errorf("still listed: %+v", r.m.DeletedCameras())
	}
	var noted []DeletedCamera
	if data := r.mustRead(deletedFile); json.Unmarshal(data, &noted) != nil || len(noted) != 0 {
		t.Errorf("on the stick: %s", data)
	}
	r.look()
	r.look()
	if _, ok := dig(t, parse(t, []byte(r.config())), "cameras").(map[string]any)["einfahrt"]; ok {
		t.Error("the stub is still in config.yml")
	}
	if r.phase() != PhaseRunning {
		t.Errorf("phase after the purge: %s", r.phase())
	}
	// Once more: nothing left to purge.
	if err := r.m.PurgeDeletedCamera(ctx, "einfahrt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second purge: %v", err)
	}
}

// Without a running Frigate nothing is deleted and the stub stays, so the
// owner can try again.
func TestPurgeDeletedCameraNeedsFrigate(t *testing.T) {
	r, _ := capRig(t)
	if err := r.m.RemoveCamera("einfahrt"); err != nil {
		t.Fatal(err)
	}
	r.look()
	r.m.mu.Lock()
	r.m.rt.phase = PhaseStarting
	r.m.mu.Unlock()
	if err := r.m.PurgeDeletedCamera(context.Background(), "einfahrt"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("purge while Frigate starts: %v", err)
	}
	if len(r.deleted()) != 0 || len(r.m.DeletedCameras()) != 1 {
		t.Errorf("deleted %v, listed %v", r.deleted(), r.m.DeletedCameras())
	}
	if !strings.Contains(string(r.mustRead(deletedFile)), "einfahrt") {
		t.Error("the stub left the stick")
	}
}

func hoursIn(days []RecordedDay) int {
	n := 0
	for _, d := range days {
		n += len(d.Hours)
	}
	return n
}

func (r *rig) exportCount() int {
	r.frigate.mu.Lock()
	defer r.frigate.mu.Unlock()
	return len(r.frigate.data.exports)
}

// The purge runs in the background (a closed window must not stop it):
// StartPurge returns at once, the camera is listed as purging, a second
// start and a camera with its ID are refused meanwhile, and in the end the
// camera is gone. A failure goes to the bell and leaves the stub.
func TestStartPurgeInTheBackground(t *testing.T) {
	r, _ := capRig(t)
	if err := r.m.RemoveCamera("einfahrt"); err != nil {
		t.Fatal(err)
	}
	r.look()
	r.look()
	// Frigate holds its answer to the camera's deletion until released.
	release := make(chan struct{})
	r.frigate.Set(func(f *DevFrigate) { f.data.holdPurge = release })
	if err := r.m.StartPurge("einfahrt"); err != nil {
		t.Fatal(err)
	}
	if !r.m.Purging("einfahrt") {
		t.Fatal("not marked as purging")
	}
	if err := r.m.StartPurge("einfahrt"); !errors.Is(err, ErrPurgeRunning) {
		t.Errorf("second start: %v", err)
	}
	c := r.m.Cameras()[0]
	c.ID, c.Name = "einfahrt", "Einfahrt neu"
	if _, err := r.m.AddCamera(context.Background(), c, CameraOptions{}); !errors.Is(err, ErrPurgeRunning) {
		t.Errorf("a camera with the purged ID: %v", err)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for r.m.Purging("einfahrt") || len(r.m.DeletedCameras()) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("purge did not end: purging %v, listed %v", r.m.Purging("einfahrt"), r.m.DeletedCameras())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStartPurgeFailureGoesToTheBell(t *testing.T) {
	r, _ := capRig(t)
	if err := r.m.RemoveCamera("einfahrt"); err != nil {
		t.Fatal(err)
	}
	r.look()
	r.look()
	r.frigate.Set(func(f *DevFrigate) { f.data.failPurge = true })
	if err := r.m.StartPurge("einfahrt"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for r.m.Purging("einfahrt") {
		if time.Now().After(deadline) {
			t.Fatal("purge did not end")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := r.noted(); !slices.Contains(got, "warning nvr_purge_failed Einfahrt") {
		t.Errorf("messages: %v", got)
	}
	if len(r.m.DeletedCameras()) != 1 {
		t.Errorf("the stub left: %v", r.m.DeletedCameras())
	}
}
