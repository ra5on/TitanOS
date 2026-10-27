package system

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func fakeSys(t *testing.T) string {
	t.Helper()
	sys := t.TempDir()
	for _, name := range []string{"enp1s0", "br0"} {
		os.MkdirAll(filepath.Join(sys, "class/net", name), 0755)
		os.WriteFile(filepath.Join(sys, "class/net", name, "address"), []byte("52:54:00:12:34:56\n"), 0644)
	}
	os.MkdirAll(filepath.Join(sys, "class/net/enp1s0/device"), 0755)
	os.MkdirAll(filepath.Join(sys, "class/rtc/rtc0"), 0755)
	os.WriteFile(filepath.Join(sys, "class/rtc/rtc0/wakealarm"), nil, 0644)
	return sys
}

func newScheduler(t *testing.T, now time.Time) (*PowerScheduler, string, map[string]bool) {
	t.Helper()
	sys := fakeSys(t)
	wol := map[string]bool{}
	p := NewPowerScheduler(config.Open(t.TempDir()))
	p.sys = sys
	p.now = func() time.Time { return now }
	p.wolGet = func(name string) (bool, bool, error) { return name == "enp1s0", wol[name], nil }
	p.wolSet = func(name string, on bool) error { wol[name] = on; return nil }
	if err := p.Load(); err != nil {
		t.Fatal(err)
	}
	return p, sys, wol
}

func TestScheduleNext(t *testing.T) {
	// Friday 2026-10-23 22:00 local.
	now := time.Date(2026, 10, 23, 22, 0, 0, 0, time.Local)
	off := ScheduleTime{Enabled: true, Days: []int{1, 2, 3, 4, 5}, Time: "23:30"}
	if at, ok := off.next(now); !ok || !at.Equal(time.Date(2026, 10, 23, 23, 30, 0, 0, time.Local)) {
		t.Fatalf("same day: %v", at)
	}
	on := ScheduleTime{Enabled: true, Days: []int{1}, Time: "07:00"}
	if at, _ := on.next(now); !at.Equal(time.Date(2026, 10, 26, 7, 0, 0, 0, time.Local)) {
		t.Fatalf("next Monday: %v", at)
	}
	if _, ok := (ScheduleTime{}).next(now); ok {
		t.Fatal("disabled has a next time")
	}
}

func TestScheduleAppliesWakeOnLANAndAlarm(t *testing.T) {
	now := time.Date(2026, 10, 23, 22, 0, 0, 0, time.Local)
	p, sys, wol := newScheduler(t, now)
	if err := p.Set(PowerSchedule{Off: ScheduleTime{Enabled: true, Days: []int{5}, Time: "23:00"}, On: ScheduleTime{Enabled: true, Days: []int{1}, Time: "07:00"}, WakeOnLAN: true}); err != nil {
		t.Fatal(err)
	}
	if !wol["enp1s0"] || wol["br0"] {
		t.Fatalf("wake-on-lan %v", wol)
	}
	data, _ := os.ReadFile(filepath.Join(sys, "class/rtc/rtc0/wakealarm"))
	want := time.Date(2026, 10, 26, 7, 0, 0, 0, time.Local).Unix()
	if strings.TrimSpace(string(data)) != strconv.FormatInt(want, 10) {
		t.Fatalf("alarm %q, want %d", data, want)
	}
	st := p.Status()
	if !st.RTC || len(st.Interfaces) != 1 || !st.Interfaces[0].Supported || st.NextOff == nil || st.NextOn == nil {
		t.Fatalf("%+v", st)
	}
	for _, bad := range []PowerSchedule{
		{Off: ScheduleTime{Enabled: true, Time: "23:00"}},
		{Off: ScheduleTime{Enabled: true, Days: []int{7}, Time: "23:00"}},
		{On: ScheduleTime{Enabled: true, Days: []int{1}, Time: "25:00"}},
	} {
		if err := p.Set(bad); !errors.Is(err, ErrPowerSchedule) {
			t.Errorf("%+v accepted", bad)
		}
	}
}

func TestScheduledShutdownWaitsForWork(t *testing.T) {
	now := time.Date(2026, 10, 23, 22, 0, 0, 0, time.Local)
	p, _, _ := newScheduler(t, now)
	p.now = func() time.Time { return now }
	busy := true
	p.Busy = func() (bool, string) { return busy, "backup" }
	var off int
	p.Poweroff = func() error { off++; return nil }
	var notes []string
	p.Notify = func(_, kind, subject string) { notes = append(notes, kind+":"+subject) }
	if err := p.Set(PowerSchedule{Off: ScheduleTime{Enabled: true, Days: []int{5}, Time: "22:30"}}); err != nil {
		t.Fatal(err)
	}
	due := p.step(time.Time{})
	now = now.Add(31 * time.Minute) // due
	due = p.step(due)
	due = p.step(due)
	if off != 0 || len(notes) != 1 || notes[0] != "power_postponed:backup" || p.Status().Postponed != "backup" {
		t.Fatalf("busy: off=%d notes=%v", off, notes)
	}
	busy = false
	now = now.Add(time.Hour)
	due = p.step(due)
	if off != 1 {
		t.Fatal("not shut down after the work ended")
	}
	// Six hours of work: this shutdown is given up, the next one counts.
	busy = true
	now = due
	due = p.step(due)
	now = now.Add(maxPostpone + time.Minute)
	next := p.step(due)
	if off != 1 || notes[len(notes)-1] != "power_skipped:backup" || !next.After(now) {
		t.Fatalf("give up: off=%d notes=%v next=%v", off, notes, next)
	}
}

func TestScheduleDisabledTouchesNothing(t *testing.T) {
	now := time.Date(2026, 10, 23, 22, 0, 0, 0, time.Local)
	sys := fakeSys(t)
	p := NewPowerScheduler(config.Open(t.TempDir()))
	p.sys, p.Disabled = sys, true
	p.now = func() time.Time { return now }
	p.wolSet = func(string, bool) error { t.Fatal("wake-on-lan set in development mode"); return nil }
	p.Load()
	off := 0
	p.Poweroff = func() error { off++; return nil }
	p.Set(PowerSchedule{Off: ScheduleTime{Enabled: true, Days: []int{5}, Time: "22:10"}, On: ScheduleTime{Enabled: true, Days: []int{1}, Time: "07:00"}, WakeOnLAN: true})
	due := p.step(time.Time{})
	now = now.Add(20 * time.Minute)
	p.step(due)
	if data, _ := os.ReadFile(filepath.Join(sys, "class/rtc/rtc0/wakealarm")); len(data) != 0 || off != 0 {
		t.Fatalf("alarm %q off %d", data, off)
	}
}

func TestShutdownSoonForVMSchedules(t *testing.T) {
	now := time.Date(2026, 10, 23, 22, 0, 0, 0, time.Local)
	p, _, _ := newScheduler(t, now)
	if p.ShutdownSoon(15 * time.Minute) {
		t.Fatal("nothing scheduled")
	}
	p.Set(PowerSchedule{Off: ScheduleTime{Enabled: true, Days: []int{5}, Time: "22:10"}})
	if !p.ShutdownSoon(15*time.Minute) || p.ShutdownSoon(5*time.Minute) {
		t.Fatal("shutdown in ten minutes")
	}
	// A due shutdown waiting for work counts as well.
	p.Busy = func() (bool, string) { return true, "backup" }
	due := p.step(time.Time{})
	now = now.Add(11 * time.Minute)
	p.now = func() time.Time { return now }
	p.step(due)
	if !p.ShutdownSoon(time.Minute) {
		t.Fatal("postponed shutdown not reported")
	}
}
