package maintenance

import (
	"context"
	"testing"
	"time"
)

func TestAuditCanDisableWhenHistoryVolumeIsUnavailable(t *testing.T) {
	m := fixture(t)
	s := settings()
	if err := m.Set(s); err != nil {
		t.Fatal(err)
	}
	if err := m.loadState(); err != nil {
		t.Fatal(err)
	}
	m.Mounted = func() []string { return nil }
	s.Enabled = false
	if err := m.Set(s); err != nil {
		t.Fatalf("cannot disable maintenance after its history volume disappeared: %v", err)
	}
	if m.Status().Settings.Enabled {
		t.Fatal("scheduler is still enabled")
	}
}

func TestDependenciesRejectCyclesAndSurviveOlderApplications(t *testing.T) {
	m := fixture(t)
	s := settings()
	child := s.Jobs[0]
	child.ID = "probe"
	child.DependsOn = "daily"
	s.Jobs = append(s.Jobs, child)
	if err := m.Set(s); err != nil {
		t.Fatal(err)
	}
	var stored Settings
	if err := m.store.ReadJSON(settingsFile, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Jobs[1].Enabled || stored.Jobs[1].DependencyEnabled == nil || !*stored.Jobs[1].DependencyEnabled {
		t.Fatal("older app could dispatch dependency independently", stored)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if !m.Status().Settings.Jobs[1].Enabled {
		t.Fatal("enabled choice was not restored")
	}
	s.Jobs[0].DependsOn = "probe"
	if err := m.Set(s); err == nil {
		t.Fatal("accepted dependency cycle")
	}
	s.Jobs[0].DependsOn = "missing"
	if err := m.Set(s); err == nil {
		t.Fatal("accepted missing dependency")
	}
}
func TestDependencyNeedsCurrentSuccessfulRunAndBackupPriorityIsOptional(t *testing.T) {
	m := fixture(t)
	s := settings()
	child := s.Jobs[0]
	child.ID = "probe"
	child.DependsOn = "daily"
	s.Jobs = append(s.Jobs, child)
	if err := m.Set(s); err != nil {
		t.Fatal(err)
	}
	m.Execute = func(context.Context, Job) error { return nil }
	if err := m.Start("probe"); err == nil {
		t.Fatal("ran without successful dependency")
	}
	if err := m.Start("daily"); err != nil {
		t.Fatal(err)
	}
	idle(t, m)
	if err := m.Start("probe"); err != nil {
		t.Fatal(err)
	}
	idle(t, m)
	m.Conflict = func(Job) string { return "backup-priority" }
	if err := m.Start("daily"); err != nil {
		t.Fatal("disabled priority blocked manual work", err)
	}
	idle(t, m)
	s.BackupsFirst = true
	if err := m.Set(s); err != nil {
		t.Fatal(err)
	}
	if err := m.Start("daily"); err == nil {
		t.Fatal("backup priority ignored")
	}
	m.mu.Lock()
	st := m.state.States["daily"]
	st.Error = "new failure"
	m.state.States["daily"] = st
	m.mu.Unlock()
	if err := m.Start("probe"); err == nil {
		t.Fatal("old success hid a new failure")
	}
}

// smart-short came in 0.41.0: on the stick it stays out of jobs, which an
// older version validates as a whole.
func TestLaterKindsStayReadableForOlderApplications(t *testing.T) {
	m := fixture(t)
	s := settings()
	short := s.Jobs[0]
	short.ID, short.Kind, short.Target, short.Name = "short", "smart-short", "SERIAL1", "Short test"
	short.DependsOn = ""
	s.Jobs = append(s.Jobs, short)
	if err := m.Set(s); err != nil {
		t.Fatal(err)
	}
	var stored Settings
	if err := m.store.ReadJSON(settingsFile, &stored); err != nil {
		t.Fatal(err)
	}
	for _, j := range stored.Jobs {
		if j.Kind == "smart-short" {
			t.Fatal("later kind in jobs")
		}
	}
	if len(stored.LaterJobs) != 1 || stored.LaterJobs[0].ID != "short" {
		t.Fatalf("later jobs: %+v", stored.LaterJobs)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, j := range m.Status().Settings.Jobs {
		found = found || j.ID == "short"
	}
	if !found {
		t.Fatal("later job lost on load")
	}
}

func TestJobsEveryFewHours(t *testing.T) {
	loc := time.UTC
	j := Job{Every: "hours", EveryHours: 2, Time: "00:30"}
	now := time.Date(2026, 10, 31, 13, 10, 0, 0, loc)
	prev, next := slots(j, now, loc)
	if !prev.Equal(time.Date(2026, 10, 31, 12, 30, 0, 0, loc)) || !next.Equal(time.Date(2026, 10, 31, 14, 30, 0, 0, loc)) {
		t.Fatalf("slots %v %v", prev, next)
	}
	m := fixture(t)
	s := settings()
	s.Jobs[0].Every, s.Jobs[0].EveryHours = "hours", 0
	if err := m.Set(s); err == nil {
		t.Fatal("hours without a distance accepted")
	}
	s.Jobs[0].EveryHours = 1
	if err := m.Set(s); err != nil {
		t.Fatal(err)
	}
	var stored Settings
	m.store.ReadJSON(settingsFile, &stored)
	if len(stored.LaterJobs) != 1 {
		t.Fatalf("hourly job readable for older versions: %+v", stored)
	}
}
