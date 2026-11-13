package nvr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Review PLAN 85-4: while the NVR's volume is away (a locked encrypted
// volume after a boot, a failed disk) the NVR can be switched off,
// changed otherwise and set up anew; switching it on, or moving it, still
// needs a mounted volume.
func TestSettingsWhileTheVolumeIsAway(t *testing.T) {
	r := newRig(t, true)
	r.up()
	ctx := context.Background()
	volume := r.m.Settings().Volume
	r.m.Dev = false
	r.m.Mounted = func() []string { return []string{"other"} }
	r.m.Engine = readyEngine{r.engine}
	r.look()

	// Another setting while it is on.
	s := r.m.Settings()
	s.NotifyOff = true
	if err := r.m.SetSettings(ctx, s, SetOptions{}); err != nil {
		t.Fatalf("change while the volume is away: %v", err)
	}
	// Switching it off.
	if err := r.m.Disable(ctx); err != nil {
		t.Fatalf("switch off while the volume is away: %v", err)
	}
	if got := r.m.Settings(); got.Enabled || got.Volume != volume || !got.NotifyOff {
		t.Errorf("settings: %+v", got)
	}
	// Switching it on again on the missing volume: no.
	s = r.m.Settings()
	s.Enabled = true
	if err := r.m.SetSettings(ctx, s, SetOptions{}); !errors.Is(err, ErrNoVolume) {
		t.Errorf("switch on onto the missing volume: %v", err)
	}
	// Moving to another missing volume: no; to a mounted one: yes.
	s.Volume = "gone"
	if err := r.m.SetSettings(ctx, s, SetOptions{}); !errors.Is(err, ErrNoVolume) {
		t.Errorf("move to a missing volume: %v", err)
	}
	s.Volume = "other"
	if err := r.m.SetSettings(ctx, s, SetOptions{ConfirmSleepingVolume: true}); err != nil {
		t.Errorf("move to a mounted volume: %v", err)
	}
}

// Review PLAN 85-4: export names may have 80 characters (they were held
// to a camera name's 64), and the default name of a camera with a long
// name fits.
func TestExportNames(t *testing.T) {
	long := strings.Repeat("Kamera ", 9) + "Hof" // 66 characters
	if !exportName(strings.Repeat("x", 80)) || exportName(strings.Repeat("x", 81)) {
		t.Error("80 characters")
	}
	r, first := capRig(t)
	ctx := context.Background()
	cam := r.m.Cameras()[0]
	cam.Name = strings.Repeat("Ä", 64)
	if _, err := r.m.UpdateCamera(ctx, cam.ID, cam, CameraOptions{}); err != nil {
		t.Fatalf("camera name of 64 characters: %v", err)
	}
	made, err := r.m.CreateExport(ctx, "anna", cam.ID, first, first+60, "")
	if err != nil {
		t.Fatalf("default name of a camera with 64 characters: %v", err)
	}
	if n := utf8.RuneCountInString(made.Name); n != 80 || !strings.HasPrefix(made.Name, strings.Repeat("Ä", 63)+" ") {
		t.Errorf("name %q (%d)", made.Name, n)
	}
	if _, err := r.m.CreateExport(ctx, "anna", cam.ID, first, first+60, long+" Abend"); err != nil {
		t.Errorf("an own name of %d characters: %v", utf8.RuneCountInString(long+" Abend"), err)
	}
}

// Review PLAN 85-4: Frigate tracks anew after every restart; a parked car
// that never moved came as a new "Fahrzeug" each time. As Frigate's own
// review items, only objects that moved are told; Home Assistant still
// sees the car. An event that began well before this titand started was
// told by the one before (an app update) and is not told again.
func TestNotifyOnlyObjectsThatMoved(t *testing.T) {
	recipients := func(n *Notify) {
		n.HomeAssistant = true
		n.Recipients = []Recipient{{User: "alex", Bell: true, Added: created.Add(time.Hour)}}
	}
	r := newNoticeRig(t)
	r.set("haustuer", recipients)
	feed := func(id string, changes int, start float64) {
		r.m.feedMessage("events", json.RawMessage(strconv.Quote(fmt.Sprintf(
			`{"type":"new","after":{"id":%q,"camera":"haustuer","label":"car","start_time":%v,"position_changes":%d}}`, id, start, changes))))
	}
	now := float64(r.m.now().Unix())
	feed("1731412345.1-parked", 0, now)
	if got := r.take(); len(got) != 0 {
		t.Fatalf("a parked car was told: %+v", got)
	}
	if ha := r.m.HACameras(); len(ha) == 0 || !ha[0].Active[KindVehicle] {
		t.Errorf("Home Assistant does not see the car: %+v", ha)
	}
	// It drives off: told once.
	feed("1731412345.1-parked", 1, now)
	feed("1731412345.1-parked", 2, now)
	if got := r.take(); len(got) != 1 {
		t.Fatalf("the car that moved: %+v", got)
	}
	// An event that began ten minutes before this titand routed anything.
	r2 := newNoticeRig(t)
	r2.set("haustuer", recipients)
	r2.m.feedMessage("events", json.RawMessage(strconv.Quote(fmt.Sprintf(
		`{"type":"update","after":{"id":"1731412345.2-old","camera":"haustuer","label":"person","start_time":%v,"position_changes":5}}`, float64(r2.m.now().Unix())-600))))
	if got := r2.take(); len(got) != 0 {
		t.Fatalf("an event of the titand before was told again: %+v", got)
	}
}

// Review PLAN 85-4: events of the very same moment (two objects of one
// frame) were split by a page and the rest of the group was lost (Frigate's
// "before" is strict). Paged through with small pages, every event comes
// exactly once, newest first.
func TestEventPagesKeepGroupsOfOneMoment(t *testing.T) {
	r, _ := capRig(t)
	base := float64(r.now.Unix()) - 3600
	starts := []float64{100, 90, 90, 90, 80, 70, 70, 60, 50, 50, 50, 50, 40}
	var events []devEvent
	for i, s := range starts {
		events = append(events, devEvent{ID: fmt.Sprintf("%.6f-e%02d", base+s, i), Camera: "haustuer", Label: "person", Start: base + s})
	}
	r.frigate.Set(func(f *DevFrigate) { f.data.events = events })
	for _, limit := range []int{1, 2, 3, 4} {
		seen := map[string]int{}
		var order []float64
		before := 0.0
		for pages := 0; ; pages++ {
			if pages > 20 {
				t.Fatalf("limit %d: no end", limit)
			}
			list, next, err := r.m.Events(context.Background(), EventFilter{Cameras: []string{"haustuer"}, Limit: limit, Before: before})
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range list {
				seen[e.ID]++
				order = append(order, e.Start)
			}
			if next == 0 {
				break
			}
			before = next
		}
		for _, e := range events {
			if seen[e.ID] != 1 {
				t.Errorf("limit %d: %s came %d times", limit, e.ID, seen[e.ID])
			}
		}
		for i := 1; i < len(order); i++ {
			if order[i] > order[i-1] {
				t.Errorf("limit %d: not newest first: %v", limit, order)
				break
			}
		}
	}
}

// Review PLAN 85-4: Frigate writes a guess "Anna-Lena" as "Anna_Lena"
// into the file of an unassigned face; the list names the known person.
func TestFaceGuessWithHyphen(t *testing.T) {
	r, _ := capRig(t)
	cam := r.m.Cameras()[0]
	cam.Detect.Faces = true
	if _, err := r.m.UpdateCamera(context.Background(), cam.ID, cam, CameraOptions{}); err != nil {
		t.Fatal(err)
	}
	r.frigate.Set(func(f *DevFrigate) {
		f.data.faces = map[string][]string{
			"Anna-Lena": {"a.webp"},
			"train":     {"1731412345.5-ab12-1731412399.25-Anna_Lena-0.82.webp", "1731412345.6-cd34-1731412400.5-Unbekannt_X-0.70.webp"},
		}
	})
	faces, err := r.m.Faces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	guesses := map[string]string{}
	for _, a := range faces.Attempts {
		guesses[a.File] = a.Guess
	}
	if got := guesses["1731412345.5-ab12-1731412399.25-Anna_Lena-0.82.webp"]; got != "Anna-Lena" {
		t.Errorf("guess %q", got)
	}
	if got := guesses["1731412345.6-cd34-1731412400.5-Unbekannt_X-0.70.webp"]; got != "Unbekannt_X" {
		t.Errorf("a guess without a known person stays: %q", got)
	}
}
