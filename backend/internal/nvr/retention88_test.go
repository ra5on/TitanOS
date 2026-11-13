package nvr

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// PLAN 88: days for recordings without an event and days for events,
// apart. Equal days are stored as 0.80 stores them.
func TestRetentionDaysAreStoredReadableBy080(t *testing.T) {
	base := testCamera("hof", "Hof", nil)
	for _, tc := range []struct {
		mode                           string
		retain, record, event          int
		wantRetain, wantRecord, wantEv int
	}{
		// A camera of 0.80: only retainDays.
		{RecordContinuous, 14, 0, 0, 14, 0, 0},
		// Both named and equal: as 0.80 stores it.
		{RecordContinuous, 30, 7, 7, 7, 0, 0},
		// Apart: retainDays is the larger (0.80 then keeps everything longer).
		{RecordContinuous, 1, 7, 30, 30, 7, 30},
		{RecordMotion, 1, 60, 10, 60, 60, 10},
		{RecordOff, 1, 3, 90, 90, 3, 90},
		// Events record nothing else: the record days are the event days.
		{RecordEvents, 1, 60, 10, 10, 0, 0},
	} {
		c := base
		c.Record = Record{Mode: tc.mode, RetainDays: tc.retain, RecordDays: tc.record, EventDays: tc.event, PreSeconds: 5, PostSeconds: 5}
		got, err := ValidateCamera(c)
		if err != nil {
			t.Fatalf("%+v: %v", tc, err)
		}
		r := got.Record
		if r.RetainDays != tc.wantRetain || r.RecordDays != tc.wantRecord || r.EventDays != tc.wantEv {
			t.Errorf("%+v: stored %d/%d/%d", tc, r.RetainDays, r.RecordDays, r.EventDays)
		}
		// What 0.80 reads: retainDays in 1..365, nothing it would refuse.
		data, _ := json.Marshal(got)
		var old struct {
			Record struct {
				RetainDays int `json:"retainDays"`
			} `json:"record"`
		}
		if json.Unmarshal(data, &old) != nil || old.Record.RetainDays < 1 || old.Record.RetainDays > 365 {
			t.Errorf("%+v: 0.80 reads retainDays %d", tc, old.Record.RetainDays)
		}
		// The page reads both days.
		view := got.View(CameraState{}).Record
		if view.RecordDays == 0 || view.EventDays == 0 {
			t.Errorf("%+v: view %+v", tc, view)
		}
	}
	for _, bad := range []Record{
		{Mode: RecordContinuous, RetainDays: 0},
		{Mode: RecordContinuous, RetainDays: 7, RecordDays: 366},
		{Mode: RecordContinuous, RetainDays: 7, EventDays: -1},
		{Mode: RecordMotion, RetainDays: 7, EventDays: 400},
	} {
		c := base
		bad.PreSeconds, bad.PostSeconds = 5, 5
		c.Record = bad
		if _, err := ValidateCamera(c); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}

// Frigate's keys: continuous and motion recordings by the record days,
// alerts and detections (and the snapshots) by the event days.
func TestRetentionDaysInConfig(t *testing.T) {
	for _, tc := range []struct {
		mode                              string
		continuous, motion, events, snaps int
	}{
		{RecordContinuous, 7, 7, 30, 30},
		{RecordMotion, 0, 7, 30, 30},
		{RecordEvents, 0, 0, 30, 30},
		{RecordOff, 7, 7, 30, 30},
	} {
		c := testCamera("hof", "Hof", func(c *Camera) {
			c.Record = Record{Mode: tc.mode, RecordDays: 7, EventDays: 30, PreSeconds: 5, PostSeconds: 5}
		})
		out, err := Render(secretSettings(), []Camera{c}, nil, RenderHardware{Detector: DetectorCPU})
		if err != nil {
			t.Fatal(err)
		}
		cfg := parse(t, out)
		rec := dig(t, cfg, "cameras", "hof", "record")
		if got := dig(t, rec, "continuous", "days"); got != tc.continuous {
			t.Errorf("%s: continuous %v", tc.mode, got)
		}
		if got := dig(t, rec, "motion", "days"); got != tc.motion {
			t.Errorf("%s: motion %v", tc.mode, got)
		}
		for _, class := range []string{"alerts", "detections"} {
			if got := dig(t, rec, class, "retain", "days"); got != tc.events {
				t.Errorf("%s: %s %v", tc.mode, class, got)
			}
		}
		if got := dig(t, cfg, "cameras", "hof", "snapshots", "retain", "default"); got != tc.snaps {
			t.Errorf("%s: snapshots %v", tc.mode, got)
		}
		// The global floor covers the longer of the two.
		if got := dig(t, cfg, "record", "motion", "days"); got != 30 {
			t.Errorf("%s: global %v", tc.mode, got)
		}
	}
	c := testCamera("hof", "Hof", func(c *Camera) {
		c.Record = Record{Mode: RecordContinuous, RecordDays: 90, EventDays: 120, PreSeconds: 5, PostSeconds: 5}
	})
	out, _ := Render(secretSettings(), []Camera{c}, nil, RenderHardware{Detector: DetectorCPU})
	if got := dig(t, parse(t, out), "record", "continuous", "days"); got != 120 {
		t.Errorf("global floor with 120 event days: %v", got)
	}
}

// Shorter days ask first – each of the two, where it counts.
func TestSplitRetentionShrinkNeedsConfirmation(t *testing.T) {
	type days struct {
		mode          string
		record, event int
	}
	for _, step := range []struct {
		from, to days
		shrinks  bool
	}{
		{days{RecordContinuous, 14, 14}, days{RecordContinuous, 7, 14}, true},
		{days{RecordContinuous, 14, 30}, days{RecordContinuous, 14, 7}, true},
		{days{RecordContinuous, 7, 30}, days{RecordContinuous, 14, 60}, false},
		{days{RecordMotion, 7, 30}, days{RecordMotion, 7, 20}, true},
		{days{RecordOff, 14, 30}, days{RecordOff, 14, 7}, true},
		{days{RecordOff, 14, 30}, days{RecordOff, 10, 30}, true},
		// Events keep no recordings without an event: fewer record days
		// after it delete nothing.
		{days{RecordEvents, 30, 30}, days{RecordContinuous, 7, 30}, false},
		{days{RecordEvents, 30, 30}, days{RecordOff, 7, 30}, false},
		{days{RecordEvents, 30, 30}, days{RecordContinuous, 7, 20}, true},
		{days{RecordContinuous, 7, 30}, days{RecordEvents, 30, 30}, true},
	} {
		r := newRig(t, true)
		ctx := context.Background()
		c, _ := r.m.Camera("haustuer")
		c.Record.Mode, c.Record.RecordDays, c.Record.EventDays = step.from.mode, step.from.record, step.from.event
		if _, err := r.m.UpdateCamera(ctx, "haustuer", c, CameraOptions{ConfirmShrink: true}); err != nil {
			t.Fatalf("%+v: setting up: %v", step, err)
		}
		c, _ = r.m.Camera("haustuer")
		c.Record.Mode, c.Record.RecordDays, c.Record.EventDays = step.to.mode, step.to.record, step.to.event
		_, err := r.m.UpdateCamera(ctx, "haustuer", c, CameraOptions{})
		if got := err != nil; got != step.shrinks {
			t.Errorf("%+v → %+v without confirmation: %v (want refused: %v)", step.from, step.to, err, step.shrinks)
		}
		if step.shrinks {
			if _, err := r.m.UpdateCamera(ctx, "haustuer", c, CameraOptions{ConfirmShrink: true}); err != nil {
				t.Errorf("%+v → %+v confirmed: %v", step.from, step.to, err)
			}
		}
	}
}

// A removed camera keeps both days in its stub (and on the stick for
// 0.80 the larger as retainDays); taking its ID back with fewer days asks.
func TestRemovedCameraKeepsBothDays(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	c, _ := r.m.Camera("haustuer")
	c.Record = Record{Mode: RecordContinuous, RecordDays: 5, EventDays: 40, PreSeconds: 5, PostSeconds: 5}
	if _, err := r.m.UpdateCamera(ctx, "haustuer", c, CameraOptions{ConfirmShrink: true}); err != nil {
		t.Fatal(err)
	}
	if err := r.m.RemoveCamera("haustuer"); err != nil {
		t.Fatal(err)
	}
	d, ok := r.m.DeletedCamera("haustuer")
	if !ok || d.RetainDays != 40 || d.RecordDays != 5 || d.EventDays != 40 {
		t.Fatalf("stub %+v", d)
	}
	if want := d.DeletedAt.Add(40 * 24 * time.Hour); !d.ExpiresAt().Equal(want) {
		t.Errorf("expires %v, want %v", d.ExpiresAt(), want)
	}
	out, err := Render(secretSettings(), nil, []DeletedCamera{d}, RenderHardware{Detector: DetectorCPU})
	if err != nil {
		t.Fatal(err)
	}
	rec := dig(t, parse(t, out), "cameras", "haustuer", "record")
	if dig(t, rec, "continuous", "days") != 5 || dig(t, rec, "motion", "days") != 5 || dig(t, rec, "alerts", "retain", "days") != 40 {
		t.Errorf("stub record %v", rec)
	}
	view := d.View().Record
	if view.RecordDays != 5 || view.EventDays != 40 {
		t.Errorf("stub view %+v", view)
	}
	// Back with the ID: fewer event days than the stub keeps asks first.
	back := testCamera("haustuer", "Haustür neu", func(c *Camera) {
		c.Record = Record{Mode: RecordContinuous, RecordDays: 5, EventDays: 10, PreSeconds: 5, PostSeconds: 5}
	})
	if _, err := r.m.AddCamera(ctx, back, CameraOptions{}); err == nil {
		t.Error("taking the ID back with fewer event days did not ask")
	}
	back.Record.EventDays = 40
	if _, err := r.m.AddCamera(ctx, back, CameraOptions{}); err != nil {
		t.Errorf("same days: %v", err)
	}
}

// A stub of 0.80 (retainDays only) still reads and renders as before; one
// of 0.81 has days 0.80 accepts.
func TestDeletedCameraFilesBothWays(t *testing.T) {
	old := `[{"id":"alt","name":"Alt","retainDays":12,"deletedAt":"2026-11-01T09:00:00Z"}]`
	var list []DeletedCamera
	if err := json.Unmarshal([]byte(old), &list); err != nil || !validDeleted(list[0]) {
		t.Fatalf("0.80 stub: %v %+v", err, list)
	}
	out, _ := Render(secretSettings(), nil, list, RenderHardware{Detector: DetectorCPU})
	rec := dig(t, parse(t, out), "cameras", "alt", "record")
	if dig(t, rec, "continuous", "days") != 12 || dig(t, rec, "detections", "retain", "days") != 12 {
		t.Errorf("0.80 stub record %v", rec)
	}
	bad := DeletedCamera{ID: "alt", Name: "Alt", RetainDays: 10, EventDays: 20, DeletedAt: time.Now()}
	if validDeleted(bad) {
		t.Error("event days beyond retainDays accepted")
	}
}
