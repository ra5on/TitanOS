package nvr

import (
	"context"
	"math"
	"slices"
	"testing"
	"time"
)

// capRig is a running NVR whose two cameras have ten full hours of one
// GB each, the youngest two hours old.
func capRig(t *testing.T) (*rig, float64) {
	t.Helper()
	r := newRig(t, true)
	r.frigate.Seed(r.m.Cameras(), r.now)
	first := float64(r.now.Add(-12 * time.Hour).Unix())
	for _, camera := range []string{"haustuer", "einfahrt"} {
		var hours []devHour
		for h := range 10 {
			hours = append(hours, devHour{Start: first + float64(h)*3600, Seconds: 3600, MB: 1024})
		}
		r.frigate.SetHours(camera, hours)
	}
	r.frigate.SetHours("garten", nil)
	r.frigate.Set(func(f *DevFrigate) {
		// No kept event unless the test adds one.
		for i := range f.data.events {
			f.data.events[i].Kept = false
		}
	})
	r.up()
	return r, first
}

func (r *rig) limit(gb int) {
	r.t.Helper()
	s := r.m.Settings()
	s.Storage.LimitGB = gb
	if err := r.m.SetSettings(context.Background(), s, SetOptions{}); err != nil {
		r.t.Fatal(err)
	}
}

// storageLook lets the watch look at the space again.
func (r *rig) storageLook() StorageStatus {
	r.advance(storageEvery + time.Second)
	r.look()
	return r.m.Status(context.Background()).Storage
}

func gb(v *uint64) float64 {
	if v == nil {
		return -1
	}
	return math.Round(float64(*v)/(1<<30)*100) / 100
}

func (r *rig) deleted() [][2]float64 {
	r.frigate.mu.Lock()
	defer r.frigate.mu.Unlock()
	return slices.Clone(r.frigate.data.deleted)
}

func TestStorageFigures(t *testing.T) {
	r, first := capRig(t)
	st := r.m.Status(context.Background()).Storage
	// Twenty GB of recordings, a small export; no cap.
	if gb(st.RecordingsBytes) != 20 || st.ExportsBytes == nil || *st.ExportsBytes == 0 || *st.ExportsBytes > 1<<20 || *st.UsedBytes != *st.RecordingsBytes+*st.ExportsBytes {
		t.Errorf("%v GB recordings, exports %v", gb(st.RecordingsBytes), st.ExportsBytes)
	}
	if st.Cap != CapOff || st.LimitGB != 0 || st.LastCleanup != nil {
		t.Errorf("%+v", st)
	}
	// The recordings reach twelve hours back.
	if st.SpanDays == nil || math.Abs(*st.SpanDays-0.5) > 0.01 {
		t.Errorf("span %v", st.SpanDays)
	}
	if len(r.deleted()) != 0 {
		t.Error("deleted without a cap")
	}
	_ = first
}

func TestStorageCapDeletesTheOldestHours(t *testing.T) {
	r, first := capRig(t)
	// 25 GB: nothing to do.
	r.limit(25)
	if st := r.storageLook(); st.Cap != CapOK || len(r.deleted()) != 0 {
		t.Fatalf("below the cap: %+v, deleted %v", st, r.deleted())
	}
	// 12 GB: 20 are used, 95 % of 12 are 11.4. Each hour holds two GB
	// (both cameras): five hours go, ten GB stay.
	r.limit(12)
	st := r.storageLook()
	want := [][2]float64{}
	for h := range 5 {
		want = append(want, [2]float64{first + float64(h)*3600, first + float64(h+1)*3600})
	}
	if got := r.deleted(); !slices.Equal(got, want) {
		t.Fatalf("deleted %v\nwant %v", got, want)
	}
	if gb(st.RecordingsBytes) != 10 || st.Cap != CapOK || st.LastCleanup == nil || *st.LastCleanup != r.now.Unix() {
		t.Errorf("after the cleanup: %v GB, %+v", gb(st.RecordingsBytes), st)
	}
	// The recordings now begin five hours later.
	if st.SpanDays == nil || *st.SpanDays*24 < 7 || *st.SpanDays*24 > 7.5 {
		t.Errorf("span %v hours", *st.SpanDays*24)
	}
	// The owner is told once, with the cap.
	if got := r.noted(); len(got) != 1 || got[0] != "info nvr_storage_cap 12 GB" {
		t.Fatalf("messages: %v", got)
	}
	// Below the cap again: nothing more is deleted.
	if st := r.storageLook(); len(r.deleted()) != 5 || st.Cap != CapOK {
		t.Errorf("deleted again: %v", r.deleted())
	}
	// More is recorded the same day: deleted, but no second message.
	r.frigate.SetHours("garten", []devHour{{Start: first + 5*3600, Seconds: 3600, MB: 3 * 1024}})
	if st := r.storageLook(); len(r.deleted()) != 6 || gb(st.RecordingsBytes) != 8 {
		t.Errorf("second cleanup: %d spans, %v GB", len(r.deleted()), gb(st.RecordingsBytes))
	}
	if got := r.noted(); len(got) != 1 {
		t.Errorf("a second message the same day: %v", got)
	}
	// The next day it is told again; the day is kept on the volume.
	r.advance(24 * time.Hour)
	r.frigate.SetHours("garten", []devHour{{Start: float64(r.now.Add(-3 * time.Hour).Truncate(time.Hour).Unix()), Seconds: 3600, MB: 6 * 1024}})
	r.storageLook()
	r.look()
	if got := r.noted(); len(got) != 2 {
		t.Errorf("messages the next day: %v", got)
	}
	r.m.mu.Lock()
	day := r.m.state.CapNoticeDay
	r.m.mu.Unlock()
	if day != r.now.UTC().Format("2006-01-02") {
		t.Errorf("notice day %q", day)
	}
}

func TestStorageCapKeepsKeptEvents(t *testing.T) {
	r, first := capRig(t)
	// A kept event in the oldest hour, ten minutes in, one minute long.
	end := first + 660
	r.frigate.Set(func(f *DevFrigate) {
		f.data.events = append(f.data.events, devEvent{ID: "9.5-kept", Camera: "haustuer", Label: "person", Start: first + 600, End: &end, Kept: true})
	})
	r.limit(19)
	st := r.storageLook()
	// Used 20, target 18.05. The oldest hour goes except the event with a
	// minute on both sides (three minutes of 60 stay, for both cameras:
	// Frigate deletes by time, not by camera). That leaves 18.1 GB, still
	// above the target: the watch goes on behind the kept part.
	want := [][2]float64{{first, first + 540}, {first + 720, first + 3600}, {first + 3600, first + 7200}}
	if got := r.deleted(); !slices.Equal(got, want) {
		t.Fatalf("deleted %v\nwant %v", got, want)
	}
	if got := gb(st.RecordingsBytes); got != 16.1 || st.Cap != CapOK {
		t.Errorf("%v GB, cap %s", got, st.Cap)
	}
	// The kept event's recording is still there.
	segments, err := r.m.Segments(context.Background(), "haustuer", first, first+3600)
	if err != nil || len(segments) == 0 || segments[0].Start != first+540 || segments[len(segments)-1].End != first+720 {
		t.Errorf("what is left of the first hour: %v %v", segments, err)
	}
}

func TestStorageCapNeverDeletesTheLastHourOrExports(t *testing.T) {
	r, first := capRig(t)
	// Everything is younger than what may be deleted.
	now := float64(r.now.Unix())
	r.frigate.SetHours("haustuer", []devHour{{Start: now - 3600, Seconds: 3600, MB: 5 * 1024}})
	r.frigate.SetHours("einfahrt", []devHour{{Start: now - 1800, Seconds: 1800, MB: 5 * 1024}})
	r.limit(2)
	if st := r.storageLook(); st.Cap != CapCleaning || len(r.deleted()) != 0 {
		t.Fatalf("%+v deleted %v", st, r.deleted())
	}
	if got := r.noted(); len(got) != 0 {
		t.Errorf("messages: %v", got)
	}
	// The exports alone are above the cap: nothing is deleted, the status
	// says why.
	hours := []devHour{}
	for h := range 4 {
		hours = append(hours, devHour{Start: first + float64(h)*3600, Seconds: 3600, MB: 1024})
	}
	r.frigate.SetHours("haustuer", hours)
	r.frigate.SetHours("einfahrt", nil)
	r.m.mu.Lock()
	for id := range r.m.rt.exportSizes {
		r.m.rt.exportSizes[id] = 3 << 30
	}
	r.m.mu.Unlock()
	st := r.storageLook()
	if st.Cap != CapExportsExceed || len(r.deleted()) != 0 || gb(st.ExportsBytes) != 3 || gb(st.RecordingsBytes) != 4 || gb(st.UsedBytes) != 7 {
		t.Errorf("%+v deleted %v", st, r.deleted())
	}
	if all, _ := r.m.Exports(context.Background(), "", true); len(all) != 3 {
		t.Errorf("%d exports", len(all))
	}
	// A larger cap: the recordings are deleted, the exports still count.
	r.limit(5)
	st = r.storageLook()
	if st.Cap != CapOK || gb(st.RecordingsBytes) != 1 || gb(st.ExportsBytes) != 3 || len(r.deleted()) != 3 {
		t.Errorf("%+v deleted %v", st, r.deleted())
	}
}

func TestWithoutKept(t *testing.T) {
	r := seeded(t)
	base, _ := r.m.base()
	end1, end2 := 1500.0, 2300.0
	r.frigate.Set(func(f *DevFrigate) {
		f.data.events = []devEvent{
			{ID: "1", Camera: "haustuer", Start: 1000, End: &end1, Kept: true},
			{ID: "2", Camera: "haustuer", Start: 1520, End: &end2, Kept: true}, // overlaps the margin of the first
			{ID: "3", Camera: "haustuer", Start: 2800, End: &end2},             // not kept
			{ID: "4", Camera: "haustuer", Start: 3550, Kept: true},             // still going on
		}
	})
	got, known := r.m.withoutKept(context.Background(), base, 0, 3600)
	want := [][2]float64{{0, 940}, {2360, 3490}}
	if !slices.Equal(got, want) || !known {
		t.Errorf("%v %v, want %v", got, known, want)
	}
	// Frigate does not answer: nothing of the hour is deleted.
	r.frigate.Set(func(f *DevFrigate) { f.Down = true })
	if got, known := r.m.withoutKept(context.Background(), base, 0, 3600); got != nil || known {
		t.Errorf("%v %v", got, known)
	}
}

// Exports between 95 and 100 % of the cap: only what is needed to get
// below the cap goes, never all recordings (review of 12.11.2026).
func TestStorageCapWithExportsNearTheCap(t *testing.T) {
	r, first := capRig(t)
	hours := []devHour{}
	for h := range 10 {
		hours = append(hours, devHour{Start: first + float64(h)*3600, Seconds: 3600, MB: 1024})
	}
	r.frigate.SetHours("haustuer", hours)
	r.frigate.SetHours("einfahrt", nil)
	r.storageLook()
	r.m.mu.Lock()
	for id := range r.m.rt.exportSizes {
		r.m.rt.exportSizes[id] = 96 << 30
	}
	r.m.mu.Unlock()
	r.limit(100)
	st := r.storageLook()
	// 96 GB of exports leave 4 GB for recordings under the cap of 100.
	if got := gb(st.RecordingsBytes); got != 4 || st.Cap != CapOK || len(r.deleted()) != 6 {
		t.Errorf("%v GB of recordings left (want 4), cap %s, %d spans deleted", got, st.Cap, len(r.deleted()))
	}
}

func TestRecordingsTarget(t *testing.T) {
	const g = 1 << 30
	for _, tc := range []struct{ limit, exports, want float64 }{
		{100, 0, 95},
		{100, 50, 45},
		{100, 90, 5}, // the 5 % that stay for recordings
		{100, 92, 5}, // still fits below the cap
		{100, 96, 4}, // only what is left below the cap
		{100, 99.5, 0.5},
		{100, 100, 0}, // exports_exceed: enforceCap deletes nothing then
		{100, 120, 0},
		{5, 3, 1.75},
	} {
		got := float64(recordingsTarget(uint64(tc.limit*g), uint64(tc.exports*g))) / g
		if math.Abs(got-tc.want) > 0.001 {
			t.Errorf("limit %v, exports %v: %v, want %v", tc.limit, tc.exports, got, tc.want)
		}
	}
}

// Exports at exactly the cap: nothing is deleted, the status says why.
func TestStorageCapExportsAtTheCap(t *testing.T) {
	r, _ := capRig(t)
	r.storageLook()
	r.m.mu.Lock()
	for id := range r.m.rt.exportSizes {
		r.m.rt.exportSizes[id] = 30 << 30
	}
	r.m.mu.Unlock()
	r.limit(30)
	if st := r.storageLook(); st.Cap != CapExportsExceed || len(r.deleted()) != 0 || gb(st.RecordingsBytes) != 20 {
		t.Errorf("%+v deleted %v", st, r.deleted())
	}
}

// Frigate does not say which events are kept: the cleanup stops instead
// of going on with younger hours.
func TestStorageCapStopsWithoutKeptEvents(t *testing.T) {
	r, _ := capRig(t)
	r.frigate.Set(func(f *DevFrigate) { f.FailKept = true })
	r.limit(12)
	if st := r.storageLook(); st.Cap != CapCleaning || len(r.deleted()) != 0 {
		t.Errorf("%+v deleted %v", st, r.deleted())
	}
}
