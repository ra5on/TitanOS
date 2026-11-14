package nvr

import (
	"encoding/json"
	"slices"
	"strconv"
	"testing"
	"time"
)

// faceTry is Frigate's tracked_object_update for one try of the face
// recognition (data_processing/real_time/face.py).
func faceTry(r *noticeRig, id, name string) {
	payload := map[string]any{"type": "face", "id": id, "camera": "haustuer", "score": 0.0, "timestamp": float64(r.now.Unix()), "name": nil}
	if name != "" {
		payload["name"], payload["score"] = name, 0.85
	}
	data, _ := json.Marshal(payload)
	// The WebSocket carries the payload as a JSON string.
	text, _ := json.Marshal(string(data))
	r.m.feedMessage("tracked_object_update", text)
}

var faceEventNo int

func personEvent(r *noticeRig) Event {
	faceEventNo++
	return Event{ID: "1731412345." + strconv.Itoa(faceEventNo) + "-face", Camera: "haustuer", Label: "person", Start: float64(r.now.Unix())}
}

func unknownFaces(notes []Notice) []Notice {
	return slices.DeleteFunc(notes, func(n Notice) bool { return n.Kind != KindFaceUnknown })
}

// PLAN 88: a face Frigate tried and did not know is told – off by
// default, when the event ends or Frigate gives up (12 tries without a
// candidate), once, never for a known face.
func TestUnknownFaceMessage(t *testing.T) {
	r := newNoticeRig(t)
	chosen := created.Add(time.Hour)
	r.set("haustuer", func(n *Notify) {
		n.Recipients = []Recipient{{User: "alex", Bell: true, Added: chosen}, {User: "sam", Push: true, Added: chosen}}
	})
	// Off by default: nothing.
	e := personEvent(r)
	r.m.routeEvent(e, "new")
	faceTry(r, e.ID, "")
	r.m.routeEvent(e, "end")
	if got := unknownFaces(r.take()); len(got) != 0 {
		t.Fatalf("off: %+v", got)
	}
	r.set("haustuer", func(n *Notify) { n.FaceUnknown = true })
	// At the end of the event.
	r.advance(time.Hour)
	e = personEvent(r)
	r.m.routeEvent(e, "new")
	faceTry(r, e.ID, "")
	r.m.routeEvent(e, "update")
	if got := unknownFaces(r.take()); len(got) != 0 {
		t.Fatalf("before the end: %+v", got)
	}
	r.m.routeEvent(e, "end")
	got := unknownFaces(r.take())
	if len(got) != 1 || got[0].Message != "nvr_event_face_unknown" || got[0].Event != e.ID || got[0].Subject != "Haustür" {
		t.Fatalf("at the end: %+v", got)
	}
	// Only to accounts that may see names: sam watches live only.
	if !slices.Equal(users(got[0].Targets), []string{"alex"}) {
		t.Errorf("to %v", users(got[0].Targets))
	}
	// A known face: no unknown face.
	r.advance(time.Hour)
	e = personEvent(r)
	r.m.routeEvent(e, "new")
	faceTry(r, e.ID, "anna")
	e.SubLabel = "Anna"
	r.m.routeEvent(e, "end")
	if got := unknownFaces(r.take()); len(got) != 0 {
		t.Errorf("known face: %+v", got)
	}
	// No face tried: no message.
	r.advance(time.Hour)
	e = personEvent(r)
	r.m.routeEvent(e, "new")
	r.m.routeEvent(e, "end")
	if got := unknownFaces(r.take()); len(got) != 0 {
		t.Errorf("no face: %+v", got)
	}
	// Frigate gives up after 12 tries without a candidate: told then,
	// not again at the end.
	r.advance(time.Hour)
	e = personEvent(r)
	r.m.routeEvent(e, "new")
	for i := range maxFaceAttempts {
		faceTry(r, e.ID, "")
		if n := len(unknownFaces(r.take())); (i < maxFaceAttempts-1 && n != 0) || (i == maxFaceAttempts-1 && n != 1) {
			t.Fatalf("try %d: %d messages", i+1, n)
		}
	}
	r.m.routeEvent(e, "end")
	if got := unknownFaces(r.take()); len(got) != 0 {
		t.Errorf("again at the end: %+v", got)
	}
	// 12 tries with a candidate: the end decides (the name may come).
	r.advance(time.Hour)
	e = personEvent(r)
	r.m.routeEvent(e, "new")
	for range maxFaceAttempts {
		faceTry(r, e.ID, "anna")
	}
	if got := unknownFaces(r.take()); len(got) != 0 {
		t.Errorf("candidate: %+v", got)
	}
	e.SubLabel = "Anna"
	r.m.routeEvent(e, "end")
	if got := unknownFaces(r.take()); len(got) != 0 {
		t.Errorf("candidate became known: %+v", got)
	}
}

// The rules of every message about an object hold: not for a person that
// never moved, not outside the window, not outside the zones, pause.
func TestUnknownFaceFollowsTheRules(t *testing.T) {
	r := newNoticeRig(t)
	chosen := created.Add(time.Hour)
	r.set("haustuer", func(n *Notify) {
		n.FaceUnknown = true
		n.Recipients = []Recipient{{User: "alex", Bell: true, Added: chosen}}
	})
	end := func(edit func(*Event)) int {
		r.advance(time.Hour)
		e := personEvent(r)
		if edit != nil {
			edit(&e)
		}
		r.m.routeEvent(e, "new")
		faceTry(r, e.ID, "")
		r.m.routeEvent(e, "end")
		return len(unknownFaces(r.take()))
	}
	if n := end(func(e *Event) { e.stood = true }); n != 0 {
		t.Errorf("stood still: %d", n)
	}
	if n := end(func(e *Event) { e.Start -= 6 * 3600 }); n != 0 {
		t.Errorf("began before this titand: %d", n)
	}
	r.m.mu.Lock()
	r.m.cameras[0].Zones = []Zone{{ID: "weg", Name: "Weg", Points: []Point{{0, 0}, {1, 0}, {1, 1}}}}
	r.m.mu.Unlock()
	r.set("haustuer", func(n *Notify) { n.Zones = []string{"weg"} })
	if n := end(nil); n != 0 {
		t.Errorf("outside the zone: %d", n)
	}
	if n := end(func(e *Event) { e.zoneIDs = []string{"weg"} }); n != 1 {
		t.Errorf("in the zone: %d", n)
	}
	r.set("haustuer", func(n *Notify) {
		n.Zones = nil
		n.Window = Window{On: true, Days: []int{1}, From: "01:00", To: "02:00"}
	})
	if n := end(nil); n != 0 {
		t.Errorf("outside the window: %d", n)
	}
	// The pause of the kind: one within the pause.
	r.set("haustuer", func(n *Notify) { n.Window = Window{Days: []int{}} })
	if n := end(nil); n != 1 {
		t.Fatalf("first: %d", n)
	}
	e := personEvent(r)
	r.m.routeEvent(e, "new")
	faceTry(r, e.ID, "")
	r.m.routeEvent(e, "end")
	if n := len(unknownFaces(r.take())); n != 0 {
		t.Errorf("within the pause: %d", n)
	}
	// Bounded: tracks of events that never end go.
	for i := range maxFaceTracks + 10 {
		faceTry(r, "1.0"+strconv.Itoa(i)+"-x", "")
	}
	r.m.mu.Lock()
	n := len(r.m.rt.notices.faces)
	r.m.mu.Unlock()
	if n > maxFaceTracks {
		t.Errorf("%d face tracks", n)
	}
}
