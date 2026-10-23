package maintenance

import (
	"context"
	"errors"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixture(t *testing.T) *Manager {
	t.Helper()
	m := New(config.Open(t.TempDir()))
	m.MountRoot = t.TempDir()
	os.Mkdir(filepath.Join(m.MountRoot, "data"), 0700)
	m.Mounted = func() []string { return []string{"data"} }
	return m
}
func settings() Settings {
	return Settings{Enabled: true, Volume: "data", Timezone: "Europe/Berlin", WindowFrom: "00:00", WindowTo: "00:00", Jobs: []Job{{ID: "daily", Name: "Check", Kind: "raid-check", Target: "data", Enabled: true, Every: "daily", Time: "03:00", Missed: "window"}}}
}
func idle(t *testing.T, m *Manager) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for m.Busy() {
		if time.Now().After(deadline) {
			t.Fatal("still running")
		}
		time.Sleep(time.Millisecond)
	}
}
func TestDisabledDoesNotAccessVolumes(t *testing.T) {
	m := fixture(t)
	m.Mounted = func() []string { t.Fatal("disabled scheduler touched volume"); return nil }
	if err := m.Tick(time.Now()); err != nil {
		t.Fatal(err)
	}
}
func TestDurableHistoryAndCrashFence(t *testing.T) {
	m := fixture(t)
	if err := m.Set(settings()); err != nil {
		t.Fatal(err)
	}
	m.Execute = func(context.Context, Job) error { return errors.New("mismatch") }
	if err := m.Start("daily"); err != nil {
		t.Fatal(err)
	}
	idle(t, m)
	if len(m.Status().History) != 1 || m.Status().History[0].Error != "mismatch" {
		t.Fatal(m.Status())
	}
	root, err := volumedata.Open(m.MountRoot, "data", "maintenance", m.Mounted())
	if err != nil {
		t.Fatal(err)
	}
	volumedata.WriteJSON(root, "state.json", persisted{States: map[string]State{"daily": {Running: true}}, History: []Result{}})
	root.Close()
	m.loadedVolume = ""
	if err := m.loadState(); err != nil {
		t.Fatal(err)
	}
	if !m.Status().Jobs[0].State.Blocked {
		t.Fatal("interrupted job not blocked")
	}
	if err := m.Start("daily"); err == nil {
		t.Fatal("unknown outcome replayed")
	}
	if err := m.Acknowledge("daily", "wrong"); err == nil {
		t.Fatal("unconfirmed review accepted")
	}
	if err := m.Acknowledge("daily", "daily"); err != nil {
		t.Fatal(err)
	}
}
func TestSerialExecutionAndVolumeLoss(t *testing.T) {
	m := fixture(t)
	m.Set(settings())
	started := make(chan struct{})
	m.Execute = func(ctx context.Context, j Job) error { close(started); <-ctx.Done(); return ctx.Err() }
	if err := m.Start("daily"); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := m.Start("daily"); err == nil {
		t.Fatal("parallel maintenance")
	}
	m.Mounted = func() []string { return nil }
	if err := m.loadState(); err == nil {
		t.Fatal("missing volume accepted")
	}
	idle(t, m)
	if !m.Status().Jobs[0].State.Blocked {
		t.Fatal("lost outcome not fenced")
	}
}
func TestCalendarDSTMissedAndOvernightWindow(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	j := settings().Jobs[0]
	j.Time = "02:30"
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, loc)
	prev, next := slots(j, now, loc)
	if prev == nil || next == nil || !next.After(now) || prev.After(now) {
		t.Fatal(prev, next)
	}
	s := settings()
	s.WindowFrom = "22:00"
	s.WindowTo = "06:00"
	if !inWindow(s, time.Date(2026, 10, 23, 23, 0, 0, 0, loc)) || inWindow(s, now) {
		t.Fatal("overnight window")
	}
	m := fixture(t)
	s = settings()
	s.Jobs[0].Missed = "skip"
	m.Set(s)
	m.Execute = func(context.Context, Job) error { t.Error("missed job executed"); return nil }
	if err := m.Tick(time.Date(2026, 10, 23, 12, 0, 0, 0, loc)); err != nil {
		t.Fatal(err)
	}
}

func TestAdmissionAndBlockedTargetsCannotBeBypassed(t *testing.T) {
	m := fixture(t)
	m.Set(settings())
	m.loadState()
	m.Execute = func(context.Context, Job) error { return nil }
	m.state.States["daily"] = State{Blocked: true, Error: "unknown"}
	changed := settings()
	changed.Jobs = nil
	if err := m.Set(changed); err == nil {
		t.Fatal("removed an unknown outcome without review")
	}
	m.state.States["daily"] = State{Running: true}
	if leave, err := m.Enter(); err == nil {
		leave()
		t.Fatal("heavy work admitted during maintenance")
	}
	m.state.States["daily"] = State{}
	leave, err := m.Enter()
	if err != nil {
		t.Fatal(err)
	}
	if m.admission.TryLock() {
		m.admission.Unlock()
		t.Fatal("maintenance entered before submitted work was registered")
	}
	leave()
	if !m.admission.TryLock() {
		t.Fatal("admission not released")
	}
	m.admission.Unlock()
}
