package nvr

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

// seeded is a rig that runs, with the example data of development mode.
func seeded(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, true)
	r.frigate.Seed(r.m.Cameras(), r.now)
	r.up()
	return r
}

// withFeed starts the connection to the stand-in's WebSocket and waits
// until it stands.
func (r *rig) withFeed() {
	r.t.Helper()
	old := feedRetry
	feedRetry = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.m.runFeed(ctx); close(done) }()
	r.t.Cleanup(func() { cancel(); <-done; feedRetry = old })
	r.wait(func() bool { return r.frigate.Connected() == 1 && len(r.frigate.Commands()) == 0 })
	// "onConnect" was answered: the feed runs.
	r.wait(func() bool {
		r.m.feed.mu.Lock()
		defer r.m.feed.mu.Unlock()
		return r.m.feed.running && len(r.m.feed.activity) > 0
	})
}

func (r *rig) wait(ok func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			r.t.Fatal("not reached in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func next(t *testing.T, ch <-chan FeedMessage) FeedMessage {
	t.Helper()
	select {
	case message := <-ch:
		return message
	case <-time.After(3 * time.Second):
		t.Fatal("no message on the feed")
		return FeedMessage{}
	}
}

func silent(t *testing.T, ch <-chan FeedMessage) {
	t.Helper()
	select {
	case message := <-ch:
		t.Fatalf("unexpected message: %+v", message)
	case <-time.After(80 * time.Millisecond):
	}
}

func TestEvents(t *testing.T) {
	r := seeded(t)
	ctx := context.Background()
	all := []string{"haustuer", "einfahrt", "garten"}
	events, cursor, err := r.m.Events(ctx, EventFilter{Cameras: all, Limit: 200})
	if err != nil || cursor != 0 || len(events) < 20 {
		t.Fatalf("%d events, next %v, %v", len(events), cursor, err)
	}
	// Newest first; the one going on has no end and no snapshot.
	if !slices.IsSortedFunc(events, func(a, b Event) int { return int(b.Start - a.Start) }) {
		t.Error("not newest first")
	}
	first := events[0]
	if first.End != nil || first.HasSnap || first.Camera != "haustuer" || !first.HasClip || first.Score <= 0 || first.Zones == nil {
		t.Errorf("ongoing event: %+v", first)
	}
	var face, plate, kept, reviewed int
	for _, e := range events {
		if e.SubLabel == "Anna" && e.Label == "person" {
			face++
		}
		if e.Plate == "M-AB 1234" && e.SubLabel == "Annas Auto" {
			plate++
		}
		if e.Kept {
			kept++
		}
		if e.Reviewed {
			reviewed++
		}
	}
	if face != 1 || plate != 1 || kept != 1 || reviewed == 0 || reviewed == len(events) {
		t.Errorf("face %d plate %d kept %d reviewed %d of %d", face, plate, kept, reviewed, len(events))
	}
	// Only the cameras asked for; none means nothing (not "all").
	garden, _, _ := r.m.Events(ctx, EventFilter{Cameras: []string{"garten"}, Limit: 200})
	for _, e := range garden {
		if e.Camera != "garten" {
			t.Errorf("event of %s in the list of garten", e.Camera)
		}
	}
	if none, _, err := r.m.Events(ctx, EventFilter{Limit: 50}); err != nil || len(none) != 0 {
		t.Errorf("no cameras: %d events %v", len(none), err)
	}
	// Pages: the cursor goes on where the page ended, without doubles.
	var paged []Event
	before := 0.0
	for range 20 {
		page, nextCursor, err := r.m.Events(ctx, EventFilter{Cameras: all, Limit: 7, Before: before})
		if err != nil {
			t.Fatal(err)
		}
		paged = append(paged, page...)
		if before = nextCursor; nextCursor == 0 {
			break
		}
	}
	if len(paged) != len(events) || paged[len(paged)-1].ID != events[len(events)-1].ID {
		t.Errorf("paged %d, all %d", len(paged), len(events))
	}
	// Filters.
	cars, _, _ := r.m.Events(ctx, EventFilter{Cameras: all, Labels: []string{"car"}, Limit: 200})
	if len(cars) == 0 || slices.ContainsFunc(cars, func(e Event) bool { return e.Label != "car" }) {
		t.Errorf("label filter: %d", len(cars))
	}
	recent, _, _ := r.m.Events(ctx, EventFilter{Cameras: all, From: float64(r.now.Unix()) - 3600, Limit: 200})
	if len(recent) == 0 || len(recent) >= len(events) || slices.ContainsFunc(recent, func(e Event) bool { return e.Start < float64(r.now.Unix())-3600 }) {
		t.Errorf("from filter: %d of %d", len(recent), len(events))
	}
	yes := true
	seen, _, _ := r.m.Events(ctx, EventFilter{Cameras: all, Reviewed: &yes, Limit: 200})
	if len(seen) != reviewed {
		t.Errorf("reviewed filter: %d, want %d", len(seen), reviewed)
	}
	// One event, marking it, deleting it.
	one, err := r.m.Event(ctx, first.ID)
	if err != nil || one.ID != first.ID || one.Reviewed {
		t.Fatalf("%+v %v", one, err)
	}
	if err := r.m.SetReviewed(ctx, first.ID, true); err != nil {
		t.Fatal(err)
	}
	if one, _ := r.m.Event(ctx, first.ID); !one.Reviewed {
		t.Error("not marked as seen")
	}
	if err := r.m.DeleteEvent(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"deleted":   func() error { _, err := r.m.Event(ctx, first.ID); return err }(),
		"path":      func() error { _, err := r.m.Event(ctx, "../config"); return err }(),
		"slash":     r.m.DeleteEvent(ctx, "a/b"),
		"dots":      r.m.SetReviewed(ctx, "a..b", true),
		"not there": r.m.SetReviewed(ctx, "1.5-nope", true),
	} {
		if !errors.Is(err, ErrEventNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// While the NVR does not run.
	_ = r.m.Disable(ctx)
	r.look()
	if _, _, err := r.m.Events(ctx, EventFilter{Cameras: all}); !errors.Is(err, ErrNotRunning) {
		t.Errorf("while off: %v", err)
	}
}

func TestRecordings(t *testing.T) {
	r := seeded(t)
	ctx := context.Background()
	days, err := r.m.RecordingsSummary(ctx, "haustuer")
	if err != nil || len(days) < 3 {
		t.Fatalf("%d days %v", len(days), err)
	}
	// Newest day first, hours in order, each with its start.
	if days[0].Day != "2026-11-12" || days[1].Day != "2026-11-11" || days[0].Hours[0].Hour != 0 || days[0].Hours[0].Start != time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC).Unix() {
		t.Errorf("days: %+v", days[0])
	}
	hours := 0
	for _, d := range days {
		hours += len(d.Hours)
		for _, h := range d.Hours {
			if h.Seconds <= 0 || h.Seconds > 3600 {
				t.Errorf("hour %s %d: %d seconds", d.Day, h.Hour, h.Seconds)
			}
		}
	}
	// 72 full hours less the gap of five (the rig's clock is on the hour).
	if hours != 67 {
		t.Errorf("%d hours", hours)
	}
	// The NAS's time zone moves the days.
	r.m.Timezone = func() string { return "Asia/Tokyo" }
	if tokyo, _ := r.m.RecordingsSummary(ctx, "haustuer"); tokyo[0].Hours[len(tokyo[0].Hours)-1].Hour != 16 {
		t.Errorf("Tokyo: last hour %+v", tokyo[0].Hours[len(tokyo[0].Hours)-1])
	}
	now := float64(r.now.Unix())
	segments, err := r.m.Segments(ctx, "haustuer", now-24*3600, now)
	if err != nil || len(segments) < 6000 {
		t.Fatalf("%d segments %v", len(segments), err)
	}
	gap, motion, objects := 0.0, 0, 0
	for i, s := range segments {
		if s.End <= s.Start || (i > 0 && s.Start < segments[i-1].End) {
			t.Fatalf("segment %d: %+v", i, s)
		}
		if i > 0 {
			gap = max(gap, s.Start-segments[i-1].End)
		}
		if s.Motion > 0 {
			motion++
		}
		if s.Objects > 0 {
			objects++
		}
	}
	// The five hours without recordings are a hole between two segments.
	if gap != 5*3600 || motion == 0 || motion == len(segments) || objects == 0 {
		t.Errorf("gap %v, %d with motion, %d with objects of %d", gap, motion, objects, len(segments))
	}
	for name, err := range map[string]error{
		"too long":       func() error { _, err := r.m.Segments(ctx, "haustuer", now-25*3600, now); return err }(),
		"backwards":      func() error { _, err := r.m.Segments(ctx, "haustuer", now, now-10); return err }(),
		"no from":        func() error { _, err := r.m.Segments(ctx, "haustuer", 0, now); return err }(),
		"unknown camera": func() error { _, err := r.m.Segments(ctx, "keller", now-10, now); return err }(),
		"summary":        func() error { _, err := r.m.RecordingsSummary(ctx, "keller"); return err }(),
	} {
		if !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestMediaPassesRanges(t *testing.T) {
	r := seeded(t)
	ctx := context.Background()
	res, err := r.m.Media(ctx, "/vod/haustuer/start/1/end/2/master.m3u8", "", nil)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("%v %v", res, err)
	}
	playlist, _ := io.ReadAll(res.Body)
	res.Body.Close()
	// The playlists name their files relative to themselves.
	if !strings.Contains(string(playlist), "\nindex-v1.m3u8\n") || strings.Contains(string(playlist), "http") {
		t.Errorf("master: %s", playlist)
	}
	header := map[string][]string{"Range": {"bytes=100-199"}}
	res, err = r.m.Media(ctx, "/vod/haustuer/start/1/end/2/seg-0-v1.m4s", "", header)
	if err != nil || res.StatusCode != 206 || res.Header.Get("Content-Range") == "" {
		t.Fatalf("range: %v %v", res, err)
	}
	part, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if len(part) != 100 {
		t.Errorf("%d bytes of a range of 100", len(part))
	}
}

func TestSnapshotIsCachedForASecond(t *testing.T) {
	r := seeded(t)
	ctx := context.Background()
	first, err := r.m.Snapshot(ctx, "haustuer", 180)
	if err != nil {
		t.Fatal(err)
	}
	// Frigate goes away: within the second the same picture still comes.
	r.frigate.Set(func(f *DevFrigate) { f.Down = true })
	again, err := r.m.Snapshot(ctx, "haustuer", 180)
	if err != nil || &again[0] != &first[0] {
		t.Errorf("not from the cache: %v", err)
	}
	// Another size is another picture.
	if _, err := r.m.Snapshot(ctx, "haustuer", 360); err == nil {
		t.Error("another height came from the cache")
	}
	r.m.mu.Lock()
	for key, entry := range r.m.snapshots {
		entry.at = entry.at.Add(-2 * time.Second)
		r.m.snapshots[key] = entry
	}
	r.m.mu.Unlock()
	if _, err := r.m.Snapshot(ctx, "haustuer", 180); err == nil {
		t.Error("an old picture came from the cache")
	}
}

func TestExports(t *testing.T) {
	r := seeded(t)
	ctx := context.Background()
	now := float64(r.now.Unix())
	// The examples belong to nobody: administrators see them, anna not.
	all, err := r.m.Exports(ctx, "", true)
	if err != nil || len(all) != 3 {
		t.Fatalf("%d exports %v", len(all), err)
	}
	states := map[string]Export{}
	for _, e := range all {
		states[e.State] = e
	}
	if done := states[ExportDone]; done.SizeBytes == nil || *done.SizeBytes < 1000 || done.Name != "Paketbote" || done.From == 0 || done.Owner != "" {
		t.Errorf("done: %+v", done)
	}
	if running := states[ExportRunning]; running.ID == "" || running.SizeBytes != nil {
		t.Errorf("running: %+v", running)
	}
	if failed := states[ExportFailed]; failed.ID == "" {
		t.Errorf("failed: %+v", all)
	}
	if mine, _ := r.m.Exports(ctx, "anna", false); len(mine) != 0 {
		t.Errorf("anna sees %d exports of others", len(mine))
	}
	// anna makes one: queued, then running, then done.
	made, err := r.m.CreateExport(ctx, "anna", "haustuer", now-1800, now-1700, "")
	if err != nil || made.State != ExportRunning || made.Owner != "anna" || !strings.HasPrefix(made.Name, "Haustür 2026-11-12") {
		t.Fatalf("%+v %v", made, err)
	}
	if _, _, err := made.ExportMedia(); !errors.Is(err, ErrExportRunning) {
		t.Errorf("media of a running export: %v", err)
	}
	var export Export
	for range 4 {
		if export, err = r.m.Export(ctx, made.ID, "anna", false); err != nil {
			t.Fatal(err)
		}
	}
	if export.State != ExportDone || export.SizeBytes == nil || export.From != now-1800 || export.To != now-1700 {
		t.Fatalf("after a while: %+v", export)
	}
	video, thumbnail, err := export.ExportMedia()
	if err != nil || video != "/exports/"+made.ID+".mp4" || thumbnail != "/clips/export/"+made.ID+".webp" {
		t.Errorf("media: %q %q %v", video, thumbnail, err)
	}
	res, err := r.m.Media(ctx, video, "", map[string][]string{"Range": {"bytes=0-9"}})
	if err != nil || res.StatusCode != 206 {
		t.Fatalf("download: %v %v", res, err)
	}
	res.Body.Close()
	// Ben does not see anna's export; an administrator does.
	if _, err := r.m.Export(ctx, made.ID, "ben", false); !errors.Is(err, ErrExportNotFound) {
		t.Errorf("ben: %v", err)
	}
	if e, err := r.m.Export(ctx, made.ID, "owner", true); err != nil || e.Owner != "anna" {
		t.Errorf("administrator: %+v %v", e, err)
	}
	if err := r.m.DeleteExport(ctx, made.ID, "ben", false); !errors.Is(err, ErrExportNotFound) {
		t.Errorf("ben deletes: %v", err)
	}
	renamed, err := r.m.RenameExport(ctx, made.ID, "anna", false, "Paket")
	if err != nil || renamed.Name != "Paket" {
		t.Errorf("rename: %+v %v", renamed, err)
	}
	if e, _ := r.m.Export(ctx, made.ID, "anna", false); e.Name != "Paket" {
		t.Errorf("after rename: %+v", e)
	}
	for name, err := range map[string]error{
		"empty name":     func() error { _, err := r.m.RenameExport(ctx, made.ID, "anna", false, " "); return err }(),
		"too long":       func() error { _, err := r.m.CreateExport(ctx, "anna", "haustuer", now-90000, now, ""); return err }(),
		"backwards":      func() error { _, err := r.m.CreateExport(ctx, "anna", "haustuer", now, now-5, ""); return err }(),
		"unknown camera": func() error { _, err := r.m.CreateExport(ctx, "anna", "keller", now-50, now, ""); return err }(),
	} {
		if !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Nothing recorded in the time asked for.
	if _, err := r.m.CreateExport(ctx, "anna", "haustuer", now-200*3600, now-199*3600, ""); !errors.Is(err, ErrExportFailed) {
		t.Errorf("nothing recorded: %v", err)
	}
	// A running export is not deleted; the owners survive a restart.
	if err := r.m.DeleteExport(ctx, states[ExportRunning].ID, "", true); !errors.Is(err, ErrExportRunning) {
		t.Errorf("delete running: %v", err)
	}
	r.m.mu.Lock()
	r.m.rt.exports = nil
	r.m.mu.Unlock()
	if mine, _ := r.m.Exports(ctx, "anna", false); len(mine) != 1 || mine[0].ID != made.ID {
		t.Errorf("after a restart: %+v", mine)
	}
	// The account is deleted: the export stays, for administrators.
	r.m.ForgetExportOwner("anna")
	if mine, _ := r.m.Exports(ctx, "anna", false); len(mine) != 0 {
		t.Error("a new account anna sees the old one's export")
	}
	if e, err := r.m.Export(ctx, made.ID, "", true); err != nil || e.Owner != "" {
		t.Errorf("%+v %v", e, err)
	}
	if err := r.m.DeleteExport(ctx, made.ID, "", true); err != nil {
		t.Fatal(err)
	}
	if err := r.m.DeleteExport(ctx, states[ExportFailed].ID, "", true); err != nil {
		t.Errorf("delete failed export: %v", err)
	}
	if left, _ := r.m.Exports(ctx, "", true); len(left) != 2 {
		t.Errorf("%d exports left", len(left))
	}
}

func TestFeedAndRights(t *testing.T) {
	r := seeded(t)
	r.withFeed()
	allowed := map[string]bool{"haustuer": true}
	messages, cancel := r.m.Subscribe(func(camera string) bool { return allowed[camera] })
	defer cancel()
	if hello := next(t, messages); hello.T != "hello" || hello.Running == nil || !*hello.Running {
		t.Fatalf("%+v", hello)
	}
	// What is in the picture right now (the example has an event going on).
	activity := next(t, messages)
	if activity.T != "activity" || activity.Camera != "haustuer" || !*activity.Motion || !slices.Equal(*activity.Objects, []string{"person"}) {
		t.Fatalf("%+v", activity)
	}
	silent(t, messages)
	// An event of a camera the window may see, and one it may not.
	now := float64(r.now.Unix())
	r.frigate.AddEvent("1.5-new1", "einfahrt", "car", "", "", now)
	r.frigate.AddEvent("1.5-new2", "haustuer", "dog", "", "", now)
	event := next(t, messages)
	if event.T != "event" || event.Phase != "new" || event.Event.ID != "1.5-new2" || event.Event.Camera != "haustuer" || event.Event.Label != "dog" {
		t.Fatalf("%+v %+v", event, event.Event)
	}
	// Motion ends, and a count of a label changes.
	r.frigate.Publish("haustuer/motion", "OFF")
	if a := next(t, messages); a.T != "activity" || *a.Motion {
		t.Fatalf("%+v", a)
	}
	r.frigate.Publish("haustuer/person", "0")
	if a := next(t, messages); len(*a.Objects) != 0 {
		t.Fatalf("%+v", *a.Objects)
	}
	// Nothing changed: no message. Topics TitanOS does not pass on: none.
	r.frigate.Publish("haustuer/person", "0")
	r.frigate.Publish("haustuer/detect/state", "ON")
	r.frigate.Publish("stats", "{}")
	r.frigate.Publish("unknown/motion", "ON")
	silent(t, messages)
	// A right given while the window is open works at once.
	allowed["einfahrt"] = true
	r.frigate.Publish("einfahrt/motion", "ON")
	if a := next(t, messages); a.Camera != "einfahrt" {
		t.Fatalf("%+v", a)
	}
	// Frigate goes away: the windows are told, and again when it is back.
	r.frigate.Set(func(f *DevFrigate) { f.Down = true })
	_ = r.m.Disable(context.Background())
	r.look()
	if hello := next(t, messages); hello.T != "hello" || *hello.Running {
		t.Fatalf("%+v", hello)
	}
	// A window that does not read loses messages; nobody waits for it.
	slow, cancelSlow := r.m.Subscribe(func(string) bool { return true })
	defer cancelSlow()
	for range 500 {
		r.m.feed.broadcast("haustuer", FeedMessage{T: "activity", Camera: "haustuer"})
	}
	if len(slow) != cap(slow) {
		t.Errorf("%d of %d buffered", len(slow), cap(slow))
	}
}

func TestRoutingOfMessages(t *testing.T) {
	r := seeded(t)
	r.withFeed()
	now := float64(r.now.Unix())
	set := func(id string, edit func(*Camera)) {
		c, _ := r.m.Camera(id)
		edit(&c)
		if _, err := r.m.UpdateCamera(id, c, false); err != nil {
			t.Fatal(err)
		}
	}
	// The front door: bell and push for persons and faces, nothing for dogs.
	set("haustuer", func(c *Camera) {
		c.Notify = Notify{Titan: true, Push: true, HomeAssistant: true, Kinds: []string{"person", "face"}}
	})
	// The drive: e-mail and agent for vehicles and plates.
	set("einfahrt", func(c *Camera) {
		c.Notify = Notify{Email: true, Agent: true, Kinds: []string{"car", "plate"}}
	})
	// The garden wants birds, but no way at all.
	set("garten", func(c *Camera) { c.Notify = Notify{Kinds: []string{"bird"}} })
	routed := func() []Notice {
		r.mu.Lock()
		defer r.mu.Unlock()
		return slices.Clone(r.routed)
	}
	r.frigate.AddEvent("1.5-a", "haustuer", "dog", "", "", now)
	r.frigate.AddEvent("1.5-b", "haustuer", "person", "", "", now)
	r.wait(func() bool { return len(routed()) == 1 })
	got := routed()[0]
	if got.Kind != KindPerson || got.Message != "nvr_event_person" || got.Subject != "Haustür" || got.Severity != "info" || !got.Titan || !got.Push || got.Email || got.Agent {
		t.Fatalf("%+v", got)
	}
	// Another person within the minute: not again.
	r.frigate.AddEvent("1.5-c", "haustuer", "person", "", "", now)
	// A face is its own kind: reported once per event, however often
	// Frigate updates it.
	r.frigate.AddEvent("1.5-d", "haustuer", "person", "Anna", "", now)
	r.frigate.AddEvent("1.5-d", "haustuer", "person", "Anna", "", now)
	r.wait(func() bool { return len(routed()) == 2 })
	if got := routed()[1]; got.Kind != KindFace || got.Message != "nvr_event_face_known" || got.Subject != "Anna · Haustür" {
		t.Fatalf("%+v", got)
	}
	// A car with a known plate at the drive: the vehicle and the plate.
	r.frigate.AddEvent("1.5-e", "einfahrt", "car", "Annas Auto", "M-AB 1234", now)
	r.frigate.AddEvent("1.5-f", "garten", "bird", "", "", now)
	r.wait(func() bool { return len(routed()) == 4 })
	vehicle, plate := routed()[2], routed()[3]
	if vehicle.Kind != KindVehicle || vehicle.Message != "nvr_event_vehicle" || vehicle.Titan || !vehicle.Email || !vehicle.Agent {
		t.Errorf("%+v", vehicle)
	}
	if plate.Kind != KindPlate || plate.Subject != "Annas Auto (M-AB 1234) · Einfahrt" {
		t.Errorf("%+v", plate)
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(routed()); n != 4 {
		t.Fatalf("%d messages: %+v", n, routed())
	}
	// After the pause a person is reported again.
	r.advance(61 * time.Second)
	r.frigate.AddEvent("1.5-g", "haustuer", "person", "", "", now)
	r.wait(func() bool { return len(routed()) == 5 })

	// Home Assistant: only the front door chose it.
	ha := r.m.HACameras()
	if len(ha) != 1 || ha[0].ID != "haustuer" || !ha[0].Active[KindPerson] || !ha[0].Active[KindFace] || ha[0].Active[KindVehicle] || ha[0].Online == nil || !*ha[0].Online {
		t.Fatalf("%+v", ha)
	}
	if ha[0].LastEvent != "person: person" {
		t.Errorf("last event %q", ha[0].LastEvent)
	}
	r.advance(2 * time.Minute)
	if ha := r.m.HACameras(); ha[0].Active[KindPerson] || ha[0].LastEvent == "" {
		t.Errorf("after the hold: %+v", ha[0])
	}
}

func TestPTZ(t *testing.T) {
	r := seeded(t)
	r.withFeed()
	ctx := context.Background()
	info, err := r.m.PTZ(ctx, "garten")
	if err != nil || !info.Supported || !slices.Equal(info.Features, []string{"pt", "zoom"}) || !slices.Equal(info.Presets, []string{"home", "tor", "terrasse"}) {
		t.Fatalf("%+v %v", info, err)
	}
	// A camera that is not set up to move is not asked at all.
	if info, err := r.m.PTZ(ctx, "haustuer"); err != nil || info.Supported || info.Presets == nil {
		t.Errorf("%+v %v", info, err)
	}
	old := PTZStopAfter
	PTZStopAfter = 60 * time.Millisecond
	defer func() { PTZStopAfter = old }()
	for _, step := range []struct{ action, preset string }{{"move_left", ""}, {"move_left", ""}, {"zoom_in", ""}} {
		if err := r.m.PTZCommand(ctx, "anna", "garten", step.action, step.preset); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Nobody says stop: titand does, once, after the last request.
	r.wait(func() bool { return len(r.frigate.Commands()) == 4 })
	time.Sleep(100 * time.Millisecond)
	want := []DevCommand{{"garten/ptz", "MOVE_LEFT"}, {"garten/ptz", "MOVE_LEFT"}, {"garten/ptz", "ZOOM_IN"}, {"garten/ptz", "STOP"}}
	if got := r.frigate.Commands(); !slices.Equal(got, want) {
		t.Fatalf("%v", got)
	}
	// A stop from the page cancels titand's own; a preset needs none.
	_ = r.m.PTZCommand(ctx, "anna", "garten", "move_up", "")
	_ = r.m.PTZCommand(ctx, "anna", "garten", "stop", "")
	_ = r.m.PTZCommand(ctx, "anna", "garten", "preset", "tor")
	time.Sleep(120 * time.Millisecond)
	want = append(want, DevCommand{"garten/ptz", "MOVE_UP"}, DevCommand{"garten/ptz", "STOP"}, DevCommand{"garten/ptz", "preset_tor"})
	if got := r.frigate.Commands(); !slices.Equal(got, want) {
		t.Fatalf("%v", got)
	}
	for name, tc := range map[string]struct {
		camera, action, preset string
		want                   error
	}{
		"unknown action":   {"garten", "self_destruct", "", ErrInvalid},
		"preset injection": {"garten", "preset", "x\"}/../y", ErrInvalid},
		"no preset":        {"garten", "preset", "", ErrInvalid},
		"camera cannot":    {"haustuer", "move_up", "", ErrPTZUnsupported},
		"unknown camera":   {"keller", "move_up", "", ErrNotFound},
	} {
		if err := r.m.PTZCommand(ctx, "anna", tc.camera, tc.action, tc.preset); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// More than ten a second from one account are refused; another
	// account is not held up by it.
	time.Sleep(1100 * time.Millisecond)
	limited := 0
	for range 15 {
		if errors.Is(r.m.PTZCommand(ctx, "anna", "garten", "stop", ""), ErrRateLimited) {
			limited++
		}
	}
	if limited != 5 {
		t.Errorf("%d of 15 refused", limited)
	}
	if err := r.m.PTZCommand(ctx, "ben", "garten", "stop", ""); err != nil {
		t.Errorf("ben: %v", err)
	}
	// Talking back: only cameras set up for it.
	if address, err := r.m.Talk("haustuer"); err != nil || !strings.HasSuffix(address, "/live/webrtc/api/ws?src=haustuer_twoway") {
		t.Errorf("%q %v", address, err)
	}
	if _, err := r.m.Talk("garten"); !errors.Is(err, ErrNotRunning) {
		t.Errorf("garten: %v", err)
	}
}

func TestFaces(t *testing.T) {
	r := seeded(t)
	ctx := context.Background()
	faces, err := r.m.Faces(ctx)
	if err != nil || !faces.Enabled || len(faces.Faces) != 2 || faces.Faces[0].Name != "Anna" || len(faces.Faces[0].Images) != 2 || len(faces.Attempts) != 2 {
		t.Fatalf("%+v %v", faces, err)
	}
	// Newest first; the guess and the score come from the file's name.
	guess, unknown := faces.Attempts[0], faces.Attempts[1]
	if guess.Guess != "Anna" || guess.Score != 0.62 || guess.At != float64(r.now.Unix())-4000 || unknown.Guess != "" || unknown.Score != 0.31 {
		t.Errorf("%+v %+v", guess, unknown)
	}
	if err := r.m.CreateFace(ctx, "Jörg"); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"exists":         r.m.CreateFace(ctx, "Anna"),
		"train":          r.m.CreateFace(ctx, "train"),
		"path":           r.m.CreateFace(ctx, "../x"),
		"space":          r.m.CreateFace(ctx, "Anna B"),
		"rename to used": r.m.RenameFace(ctx, "Ben", "Anna"),
		"rename missing": r.m.RenameFace(ctx, "Nobody", "X"),
		"delete missing": r.m.DeleteFace(ctx, "Nobody", ""),
		"delete train":   r.m.DeleteFace(ctx, "train", ""),
		"file path":      r.m.DeleteFace(ctx, "Anna", "../../config.yml"),
		"wrong type":     r.m.AddFaceImage(ctx, "Anna", "image/gif", make([]byte, 500)),
		"too big":        r.m.AddFaceImage(ctx, "Anna", "image/png", make([]byte, MaxFaceImage+1)),
		"no face":        r.m.AddFaceImage(ctx, "Anna", "image/jpeg", make([]byte, 100)),
		"assign missing": r.m.AssignAttempt(ctx, "nope.webp", "Anna"),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := r.m.AddFaceImage(ctx, "Jörg", "image/jpeg", make([]byte, 2000)); err != nil {
		t.Fatal(err)
	}
	if err := r.m.RenameFace(ctx, "Jörg", "Joerg"); err != nil {
		t.Fatal(err)
	}
	if err := r.m.AssignAttempt(ctx, unknown.File, "Ben"); err != nil {
		t.Fatal(err)
	}
	if err := r.m.DeleteFace(ctx, "train", guess.File); err != nil {
		t.Fatal(err)
	}
	if err := r.m.DeleteFace(ctx, "Anna", faces.Faces[0].Images[0]); err != nil {
		t.Fatal(err)
	}
	faces, _ = r.m.Faces(ctx)
	names := []string{}
	for _, f := range faces.Faces {
		names = append(names, f.Name+":"+string(rune('0'+len(f.Images))))
	}
	if !slices.Equal(names, []string{"Anna:1", "Ben:2", "Joerg:1"}) || len(faces.Attempts) != 0 {
		t.Errorf("%v, %d attempts", names, len(faces.Attempts))
	}
	// A picture is only handed out if the library lists it.
	if path, err := r.m.FaceImagePath(ctx, "Ben", faces.Faces[1].Images[0]); err != nil || !strings.HasPrefix(path, "/clips/faces/Ben/") {
		t.Errorf("%q %v", path, err)
	}
	for _, tc := range [][2]string{{"Ben", "nope.webp"}, {"Ben", "../Anna/x.webp"}, {"..", "x.webp"}, {"Ben", "x.txt"}} {
		if _, err := r.m.FaceImagePath(ctx, tc[0], tc[1]); !errors.Is(err, ErrFaceNotFound) {
			t.Errorf("%v: %v", tc, err)
		}
	}
	if err := r.m.DeleteFace(ctx, "Joerg", ""); err != nil {
		t.Fatal(err)
	}
	if faces, _ := r.m.Faces(ctx); len(faces.Faces) != 2 {
		t.Errorf("%d persons", len(faces.Faces))
	}
	// No camera recognises faces: the library is off.
	c, _ := r.m.Camera("haustuer")
	c.Detect.Faces = false
	if _, err := r.m.UpdateCamera("haustuer", c, false); err != nil {
		t.Fatal(err)
	}
	if faces, err := r.m.Faces(ctx); err != nil || faces.Enabled || len(faces.Faces) != 0 {
		t.Errorf("%+v %v", faces, err)
	}
	if err := r.m.CreateFace(ctx, "X"); !errors.Is(err, ErrFacesOff) {
		t.Errorf("%v", err)
	}
	plates, err := r.m.RecognizedPlates(ctx)
	if err != nil || len(plates) != 2 || plates[0].Plate != "B-XY 987" && plates[0].Plate != "M-AB 1234" || plates[0].Count != 1 || plates[0].LastSeen == nil || plates[0].Camera != "einfahrt" {
		t.Errorf("%+v %v", plates, err)
	}
}

func TestKnownPlates(t *testing.T) {
	ok := Defaults()
	ok.Plates = []KnownPlate{{Name: "Annas Auto", Plates: []string{"M-AB 1234", "MAB12*", "M-AB 1234"}}, {Name: "Firma", Plates: []string{"EMP-[0-9]{3}[A-Z]"}}}
	if err := ValidateSettings(ok, nil); err != nil {
		t.Fatal(err)
	}
	clean, _ := cleanPlates(ok.Plates)
	if len(clean[0].Plates) != 2 {
		t.Errorf("doubles kept: %v", clean[0].Plates)
	}
	for name, plates := range map[string][]KnownPlate{
		"no name":     {{Name: "", Plates: []string{"A"}}},
		"no plate":    {{Name: "A", Plates: nil}},
		"double name": {{Name: "A", Plates: []string{"1"}}, {Name: "A", Plates: []string{"2"}}},
		"quote":       {{Name: "A", Plates: []string{`A"B`}}},
		"newline":     {{Name: "A", Plates: []string{"A\nlpr: x"}}},
		"bad pattern": {{Name: "A", Plates: []string{"A[1"}}},
		"empty plate": {{Name: "A", Plates: []string{""}}},
	} {
		s := Defaults()
		s.Plates = plates
		if err := ValidateSettings(s, nil); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
