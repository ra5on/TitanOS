package configbackup

import (
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestSchedule(t *testing.T) {
	zone, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, zone)
	s := Settings{Enabled: true, Schedule: "daily", Time: "03:00"}
	next := nextScheduled(s, now)
	if next == nil || next.Day() != 19 || next.Hour() != 3 {
		t.Fatal(next)
	}
	s.Schedule = "weekly"
	s.Weekday = 0
	next = nextScheduled(s, now)
	if next == nil || next.Day() != 25 || next.Hour() != 3 {
		t.Fatal(next)
	}
	s.Enabled = false
	if nextScheduled(s, now) != nil {
		t.Fatal("disabled")
	}
	for _, clock := range []string{"25:00", "3:00", "12:70", ""} {
		s.Enabled = true
		s.Time = clock
		if checkSchedule(s) == nil {
			t.Fatal(clock)
		}
	}
	s.Time = "03:00"
	s.Schedule = "yearly"
	if checkSchedule(s) == nil {
		t.Fatal("unknown schedule")
	}
}

func TestChangeDueByPace(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	changed := now.Add(-10 * time.Minute)
	if changeDue(PaceChange, now, now.Add(-time.Minute), now.Add(-48*time.Hour)) {
		t.Fatal("a change is saved before the configuration was left alone")
	}
	if !changeDue(PaceChange, now, changed, now.Add(-time.Hour)) || !changeDue("", now, changed, now.Add(-time.Hour)) {
		t.Fatal("pace change does not save a settled change")
	}
	if changeDue(PaceDaily, now, changed, now.Add(-23*time.Hour)) {
		t.Fatal("pace daily saves twice within a day")
	}
	if !changeDue(PaceDaily, now, changed, now.Add(-24*time.Hour)) {
		t.Fatal("pace daily does not save a change after a day")
	}
}

func TestPaceDefaultsKeepAStoredChoice(t *testing.T) {
	// No file: an existing NAS that never changed its settings keeps
	// "after every change"; an update must not make backups rarer.
	fresh := config.Open(t.TempDir())
	m := NewManager(fresh, "0.77.0")
	if err := m.Load(); err != nil || m.current().Pace != PaceChange {
		t.Fatalf("no settings file: %v %q", err, m.current().Pace)
	}
	// Only a NAS that is being set up gets "daily", written down so that it
	// stays; a NAS with settings keeps them.
	if err := m.NewInstall(); err != nil || m.current().Pace != PaceDaily {
		t.Fatalf("new install: %v %q", err, m.current().Pace)
	}
	again := NewManager(fresh, "0.77.0")
	if err := again.Load(); err != nil || again.current().Pace != PaceDaily {
		t.Fatalf("new install after a restart: %v %q", err, again.current().Pace)
	}
	if err := again.SetSettings(Settings{Enabled: true, Keep: 14, Pace: PaceChange}, nil); err != nil {
		t.Fatal(err)
	}
	if err := again.NewInstall(); err != nil || again.current().Pace != PaceChange {
		t.Fatalf("NewInstall changed stored settings: %v %q", err, again.current().Pace)
	}
	// A file from before 0.77 has no pace: every change, as it was.
	store := config.Open(t.TempDir())
	if err := store.WriteJSON(settingsFile, map[string]any{"enabled": true, "volume": "", "keep": 14, "schedule": "", "time": "", "weekday": 0}); err != nil {
		t.Fatal(err)
	}
	m = NewManager(store, "0.77.0")
	if err := m.Load(); err != nil || m.current().Pace != PaceChange {
		t.Fatalf("stored choice: %v %q", err, m.current().Pace)
	}
	// A request without the field keeps the pace; an unknown one is refused.
	if err := m.SetSettings(Settings{Enabled: true, Keep: 14}, nil); err != nil || m.current().Pace != PaceChange {
		t.Fatalf("kept: %v %q", err, m.current().Pace)
	}
	if err := m.SetSettings(Settings{Enabled: true, Keep: 14, Pace: "hourly"}, nil); err == nil {
		t.Fatal("unknown pace accepted")
	}
	if err := m.SetSettings(Settings{Enabled: true, Keep: 14, Pace: PaceDaily}, nil); err != nil || m.current().Pace != PaceDaily {
		t.Fatalf("daily: %v %q", err, m.current().Pace)
	}
}
