package nvr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// PLAN 88: "seen" is per account. What Frigate's flag (the NVR-wide "seen"
// of 0.80) says still counts for the events before titand kept its own.
func TestSeenPerAccount(t *testing.T) {
	r := seeded(t)
	ctx := context.Background()
	all := r.m.CameraIDs()
	list := func(account string) map[string]bool {
		t.Helper()
		events, _, err := r.m.Events(ctx, EventFilter{Cameras: all, Limit: 200, Account: account})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, e := range events {
			out[e.ID] = e.Reviewed
		}
		return out
	}
	anna, ben := list("anna"), list("ben")
	legacy := 0
	for id, seen := range anna {
		if seen != ben[id] {
			t.Fatalf("before any mark the accounts differ on %s", id)
		}
		if seen {
			legacy++
		}
	}
	if legacy == 0 || legacy == len(anna) {
		t.Fatalf("Frigate's flags of 0.80: %d of %d seen", legacy, len(anna))
	}
	var unseen, seenBefore Event
	events, _, _ := r.m.Events(ctx, EventFilter{Cameras: all, Limit: 200, Account: "anna"})
	for _, e := range events {
		if !e.Reviewed && unseen.ID == "" && e.End != nil {
			unseen = e
		}
		if e.Reviewed && seenBefore.ID == "" {
			seenBefore = e
		}
	}
	// anna marks one seen and one of 0.80 not seen: ben sees no change.
	if err := r.m.MarkSeen("anna", unseen, true); err != nil {
		t.Fatal(err)
	}
	if err := r.m.MarkSeen("anna", seenBefore, false); err != nil {
		t.Fatal(err)
	}
	anna, ben = list("anna"), list("ben")
	if !anna[unseen.ID] || anna[seenBefore.ID] || ben[unseen.ID] || !ben[seenBefore.ID] {
		t.Errorf("anna %v/%v ben %v/%v", anna[unseen.ID], anna[seenBefore.ID], ben[unseen.ID], ben[seenBefore.ID])
	}
	// Frigate's own flag is no longer set by titand.
	r.frigate.Set(func(f *DevFrigate) {
		for _, e := range f.data.events {
			if e.ID == unseen.ID && e.Reviewed {
				t.Error("Frigate's flag was set")
			}
		}
	})
	// The filter on "seen" is the account's.
	yes := true
	seenList, _, _ := r.m.Events(ctx, EventFilter{Cameras: all, Limit: 200, Account: "anna", Reviewed: &yes})
	if !slices.ContainsFunc(seenList, func(e Event) bool { return e.ID == unseen.ID }) || slices.ContainsFunc(seenList, func(e Event) bool { return e.ID == seenBefore.ID }) {
		t.Error("filter on seen is not anna's")
	}
	// One event, as the account sees it.
	if one, _ := r.m.Event(ctx, unseen.ID, "anna"); !one.Reviewed {
		t.Error("one event: not seen by anna")
	}
	if one, _ := r.m.Event(ctx, unseen.ID, "ben"); one.Reviewed {
		t.Error("one event: seen by ben")
	}
	// All seen for ben.
	if err := r.m.MarkAllSeen("ben", float64(r.now.Unix())); err != nil {
		t.Fatal(err)
	}
	for id, seen := range list("ben") {
		if !seen {
			t.Errorf("ben: %s not seen after all seen", id)
		}
	}
	// A new titand reads the marks from the volume.
	m2 := r.manager(r.m.FrigateBase(""))
	if err := m2.Load(); err != nil {
		t.Fatal(err)
	}
	if !m2.Seen("anna", unseen) || m2.Seen("anna", seenBefore) {
		t.Error("marks lost with a restart")
	}
	// A deleted account's marks go.
	r.m.ForgetSeen("anna")
	if r.m.Seen("anna", unseen) {
		t.Error("marks of a deleted account stayed")
	}
}

// Events after the cutover are never seen by Frigate's flag: it can not
// turn into a "seen" for everyone again.
func TestSeenFlagOfFrigateOnlyBeforeTheCutover(t *testing.T) {
	r := seeded(t)
	ctx := context.Background()
	_ = r.m.judge("anna") // titand starts keeping its own now
	r.advance(time.Minute)
	start := float64(r.now.Unix())
	end := start + 5
	r.frigate.Set(func(f *DevFrigate) {
		f.data.events = append([]devEvent{{ID: "1999999999.1-new", Camera: "haustuer", Label: "person", Start: start, End: &end, Reviewed: true}}, f.data.events...)
	})
	if e, err := r.m.Event(ctx, "1999999999.1-new", "anna"); err != nil || e.Reviewed {
		t.Errorf("after the cutover: %+v %v", e, err)
	}
}

// seen.json is bounded, and one that cannot be read is never written
// over.
func TestSeenFileIsBoundedAndKeptWhenUnreadable(t *testing.T) {
	r := seeded(t)
	start := float64(r.now.Unix()) - 3600
	for i := range maxSeenMarks + 25 {
		e := Event{ID: fmt.Sprintf("%.0f.%d-x", start, i), Start: start + float64(i)}
		if i%500 == 0 {
			if err := r.m.MarkSeen("anna", e, true); err != nil {
				t.Fatal(err)
			}
			continue
		}
		// Quicker than one write each: the same edit as MarkSeen.
		r.m.seenMu.Lock()
		data, _, _, _ := r.m.seenState()
		a := data.Accounts["anna"]
		a.Seen[e.ID] = e.Start
		r.m.seenMu.Unlock()
	}
	if err := r.m.MarkSeen("anna", Event{ID: "last", Start: start + 1e5}, true); err != nil {
		t.Fatal(err)
	}
	var stored seenData
	data, err := os.ReadFile(filepath.Join(r.dir, seenFile))
	if err != nil || json.Unmarshal(data, &stored) != nil {
		t.Fatal(err)
	}
	a := stored.Accounts["anna"]
	if len(a.Seen) != maxSeenMarks {
		t.Errorf("%d marks kept, want %d", len(a.Seen), maxSeenMarks)
	}
	if _, ok := a.Seen[fmt.Sprintf("%.0f.%d-x", start, 0)]; ok {
		t.Error("the oldest mark stayed")
	}
	if _, ok := a.Seen["last"]; !ok {
		t.Error("the newest mark is missing")
	}
	// Marks of events older than any retention fall out.
	r.advance(seenKeep + 48*time.Hour)
	if err := r.m.MarkSeen("anna", Event{ID: "later", Start: float64(r.now.Unix())}, true); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(r.dir, seenFile))
	stored = seenData{}
	_ = json.Unmarshal(data, &stored)
	if n := len(stored.Accounts["anna"].Seen); n != 1 {
		t.Errorf("%d old marks stayed", n)
	}
	// Unreadable: not written over, marks refused.
	if err := os.WriteFile(filepath.Join(r.dir, seenFile), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	m2 := r.manager(r.m.FrigateBase(""))
	if err := m2.Load(); err != nil {
		t.Fatal(err)
	}
	if err := m2.MarkSeen("anna", Event{ID: "x", Start: 1}, true); !errors.Is(err, ErrSeenUnreadable) {
		t.Errorf("unreadable: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(r.dir, seenFile)); string(data) != "{broken" {
		t.Error("an unreadable seen.json was written over")
	}
}
