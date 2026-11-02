package nvr

import (
	"testing"
	"time"
)

func TestArmingSchedule(t *testing.T) {
	// Mondays to Fridays 22:00–06:00, and Sunday 10:00–12:00.
	a := Arming{Mode: "schedule", Schedule: []Window{{Days: []int{1, 2, 3, 4, 5}, From: "22:00", To: "06:00"}, {Days: []int{0}, From: "10:00", To: "12:00"}}}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	at := func(day, hour, min int) time.Time { // 2026-11-01 is a Sunday
		return time.Date(2026, 11, 1+day, hour, min, 0, 0, time.Local)
	}
	cases := []struct {
		when time.Time
		want bool
	}{
		{at(1, 23, 0), true},  // Monday night
		{at(2, 5, 59), true},  // Tuesday early: from Monday's window
		{at(2, 6, 0), false},  // window ends
		{at(1, 5, 0), false},  // Monday early: Sunday has no night window
		{at(6, 3, 0), true},   // Saturday early: from Friday's window
		{at(6, 23, 0), false}, // Saturday night: not in the list
		{at(0, 11, 0), true},  // Sunday morning
		{at(0, 12, 0), false},
	}
	for _, c := range cases {
		if got := a.Active(c.when); got != c.want {
			t.Errorf("%s: %v, want %v", c.when.Format("Mon 15:04"), got, c.want)
		}
	}
	if (Arming{Mode: "on"}).Active(time.Now()) != true || (Arming{Mode: "off"}).Active(time.Now()) {
		t.Fatal("on/off")
	}
	for _, bad := range []Arming{{Mode: "maybe"}, {Mode: "schedule", Schedule: []Window{{Days: []int{7}, From: "1:00", To: "2:00"}}}, {Mode: "schedule", Schedule: []Window{{Days: []int{1}, From: "25:00", To: "02:00"}}}, {Mode: "schedule", Schedule: []Window{{From: "01:00", To: "02:00"}}}} {
		if bad.Validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestArmingOnlyChosenCamerasAndQuietTime(t *testing.T) {
	s := newTestStore(t)
	m := &Manager{Store: s}
	var got []string
	m.Notify = func(_, kind, subject string) { got = append(got, subject) }
	st := s.Settings()
	st.Arming = Arming{Mode: "on", Cameras: []string{"cam-1"}, QuietMinutes: 1}
	if err := s.SetSettings(st); err != nil {
		t.Fatal(err)
	}
	ev := Event{Kind: "motion", Start: time.Now()}
	m.onEvent(Camera{ID: "cam-2", Name: "Garten"}, ev)
	m.onEvent(Camera{ID: "cam-1", Name: "Hof"}, ev)
	if len(got) != 1 || got[0] != "Hof" {
		t.Fatalf("notices: %v", got)
	}
	// After the quiet time the next one goes out.
	m.notified["cam-1"] = time.Now().Add(-2 * time.Minute)
	m.onEvent(Camera{ID: "cam-1", Name: "Hof"}, ev)
	if len(got) != 2 {
		t.Fatalf("notices after quiet time: %v", got)
	}
}

func TestOldArmedSwitchIsTakenOver(t *testing.T) {
	s := newTestStore(t)
	if err := s.cfg.WriteJSON(camerasFile, map[string]any{"settings": map[string]any{"enabled": true, "keepDays": 7, "armed": true}}); err != nil {
		t.Fatal(err)
	}
	if got := s.Settings().Arming.Mode; got != "on" {
		t.Fatalf("mode %q", got)
	}
}
