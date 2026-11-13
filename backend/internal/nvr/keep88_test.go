package nvr

import (
	"context"
	"errors"
	"testing"
	"time"
)

// keptRig is a seeded NVR with an event that ended long enough ago.
func keptRig(t *testing.T) (*rig, Event) {
	t.Helper()
	r := seeded(t)
	events, _, err := r.m.Events(context.Background(), EventFilter{Cameras: []string{"haustuer"}, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.End != nil && !e.Kept && float64(r.now.Unix())-*e.End > 300 && float64(r.now.Unix())-e.Start < 6*3600 {
			return r, e
		}
	}
	t.Fatal("no event to keep")
	return nil, Event{}
}

// PLAN 88: "Behalten" sets Frigate's flag and saves the video as an
// export of the event's span with the seconds before and after.
func TestKeepEventSavesItsVideo(t *testing.T) {
	r, e := keptRig(t)
	ctx := context.Background()
	kept, err := r.m.KeepEvent(ctx, e.ID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !kept.Kept || kept.KeptExport == "" {
		t.Fatalf("kept %+v", kept)
	}
	r.frigate.Set(func(f *DevFrigate) {
		for _, d := range f.data.events {
			if d.ID == e.ID && !d.Kept {
				t.Error("Frigate's retain flag not set")
			}
		}
	})
	records, _ := r.m.exportRecords()
	record := records[kept.KeptExport]
	pre, post := r.m.eventCapture(e.Camera)
	if record.Event != e.ID || record.Owner != "" || record.KeptBy != "admin" ||
		record.From > e.Start-float64(pre) || record.To < *e.End+float64(post) || record.To-record.From > 120 {
		t.Errorf("export record %+v of event %v–%v", record, e.Start, *e.End)
	}
	// Keeping again changes nothing.
	again, err := r.m.KeepEvent(ctx, e.ID, "admin")
	if err != nil || again.KeptExport != kept.KeptExport {
		t.Errorf("again: %+v %v", again, err)
	}
	// The list shows it kept with its video.
	list, _, _ := r.m.Events(ctx, EventFilter{Cameras: []string{"haustuer"}, Limit: 200})
	for _, l := range list {
		if l.ID == e.ID && (!l.Kept || l.KeptExport != kept.KeptExport) {
			t.Errorf("listed %+v", l)
		}
	}
	// Every account that sees the camera sees the video; only
	// administrators change it.
	exports, _ := r.m.Exports(ctx, "sam", false)
	found := false
	for _, x := range exports {
		found = found || (x.ID == kept.KeptExport && x.Event == e.ID)
	}
	if !found {
		t.Error("the kept video is not listed for an account")
	}
	_, _ = r.m.Exports(ctx, "", true) // the stand-in finishes the export when listed
	if _, err := r.m.RenameExport(ctx, kept.KeptExport, "sam", false, "Meins"); !errors.Is(err, ErrExportKept) {
		t.Errorf("renamed by an account: %v", err)
	}
	if err := r.m.DeleteExport(ctx, kept.KeptExport, "sam", false); !errors.Is(err, ErrExportKept) {
		t.Errorf("deleted by an account: %v", err)
	}
	// Deleting the kept event asks first, then takes its video along.
	if err := r.m.DeleteEvent(ctx, e.ID, false); !errors.Is(err, ErrEventKept) {
		t.Fatalf("deleting a kept event without asking: %v", err)
	}
	if err := r.m.DeleteEvent(ctx, e.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, saved := r.m.keptExports()[e.ID]; saved {
		t.Error("the video of the deleted event stayed")
	}
}

func TestUnkeepEvent(t *testing.T) {
	r, e := keptRig(t)
	ctx := context.Background()
	kept, err := r.m.KeepEvent(ctx, e.ID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// While the video is being made, the event stays kept.
	if _, err := r.m.UnkeepEvent(ctx, e.ID); !errors.Is(err, ErrExportRunning) {
		t.Errorf("unkeep while saving: %v", err)
	}
	_, _ = r.m.Exports(ctx, "", true)
	_, _ = r.m.Exports(ctx, "", true)
	back, err := r.m.UnkeepEvent(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Kept || back.KeptExport != "" {
		t.Errorf("after unkeep %+v", back)
	}
	if _, err := r.m.Export(ctx, kept.KeptExport, "", true); !errors.Is(err, ErrExportNotFound) {
		t.Errorf("the saved video stayed: %v", err)
	}
	// Not kept any more: deleting asks nothing.
	if err := r.m.DeleteEvent(ctx, e.ID, false); err != nil {
		t.Error(err)
	}
}

// An event that goes on, or whose last seconds are not recorded yet, is
// kept later; one without a recording is kept without a video.
func TestKeepEventEdges(t *testing.T) {
	r := seeded(t)
	ctx := context.Background()
	now := float64(r.now.Unix())
	end := now - 10
	gapStart := float64(r.now.Truncate(time.Hour).Unix()) - 22*3600
	gapEnd := gapStart + 30
	r.frigate.Set(func(f *DevFrigate) {
		f.data.events = append(f.data.events,
			devEvent{ID: "1.1-running", Camera: "haustuer", Label: "person", Start: now - 30},
			devEvent{ID: "1.2-justended", Camera: "haustuer", Label: "person", Start: now - 40, End: &end},
			devEvent{ID: "1.3-gap", Camera: "haustuer", Label: "person", Start: gapStart, End: &gapEnd},
		)
	})
	for _, id := range []string{"1.1-running", "1.2-justended"} {
		if _, err := r.m.KeepEvent(ctx, id, "admin"); !errors.Is(err, ErrEventRunning) {
			t.Errorf("%s: %v", id, err)
		}
	}
	gap, err := r.m.KeepEvent(ctx, "1.3-gap", "admin")
	if err != nil || !gap.Kept || gap.KeptExport != "" {
		t.Errorf("in a gap: %+v %v", gap, err)
	}
	if _, err := r.m.KeepEvent(ctx, "1.9-nope", "admin"); !errors.Is(err, ErrEventNotFound) {
		t.Errorf("no such event: %v", err)
	}
}
