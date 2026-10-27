package vm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func berlin(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no time zone data:", err)
	}
	return loc
}

func TestPowerScheduleValidation(t *testing.T) {
	good := PowerSchedule{Enabled: true, Rules: []PowerRule{{Action: "start", Days: []int{5, 1, 1}, Time: "07:00"}, {Action: "stop", Days: []int{1}, Time: "22:00"}}}
	if err := checkPowerSchedule(&good); err != nil {
		t.Fatal(err)
	}
	if good.StopTimeout != defaultStopTimeout || len(good.Rules[0].Days) != 2 || good.Rules[0].Days[0] != 1 {
		t.Fatalf("not normalized: %+v", good)
	}
	empty := PowerSchedule{StopTimeout: defaultStopTimeout, Rules: []PowerRule{}}
	if err := checkPowerSchedule(&empty); err != nil || !isZeroSchedule(empty) {
		t.Fatalf("an unused schedule is kept: %+v %v", empty, err)
	}
	for name, s := range map[string]PowerSchedule{
		"no rule":       {Enabled: true},
		"no day":        {Enabled: true, Rules: []PowerRule{{Action: "start", Time: "07:00"}}},
		"bad day":       {Enabled: true, Rules: []PowerRule{{Action: "start", Days: []int{7}, Time: "07:00"}}},
		"bad time":      {Enabled: true, Rules: []PowerRule{{Action: "start", Days: []int{1}, Time: "24:00"}}},
		"short time":    {Enabled: true, Rules: []PowerRule{{Action: "start", Days: []int{1}, Time: "7:00"}}},
		"bad action":    {Enabled: true, Rules: []PowerRule{{Action: "reboot", Days: []int{1}, Time: "07:00"}}},
		"short timeout": {Enabled: true, StopTimeout: 5, Rules: []PowerRule{{Action: "stop", Days: []int{1}, Time: "07:00"}}},
		"long timeout":  {Enabled: true, StopTimeout: 7200, Rules: []PowerRule{{Action: "stop", Days: []int{1}, Time: "07:00"}}},
		"clash":         {Enabled: true, Rules: []PowerRule{{Action: "start", Days: []int{1, 2}, Time: "07:00"}, {Action: "stop", Days: []int{2}, Time: "07:00"}}},
		"many":          {Enabled: true, Rules: make([]PowerRule, maxPowerRules+1)},
	} {
		if err := checkPowerSchedule(&s); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
}

func isZeroSchedule(s PowerSchedule) bool {
	return !s.Enabled && s.Rules == nil && s.StopTimeout == 0 && !s.ForceOff
}

func TestPowerScheduleNextAcrossDST(t *testing.T) {
	loc := berlin(t)
	everyDay := []int{0, 1, 2, 3, 4, 5, 6}
	night := PowerSchedule{Enabled: true, Rules: []PowerRule{{Action: "start", Days: everyDay, Time: "02:30"}}}
	// Clocks go forward on 29 March 2026: 02:30 does not exist, it is 03:30.
	at, ok := night.next("start", time.Date(2026, 3, 29, 0, 0, 0, 0, loc))
	if !ok || !at.Equal(time.Date(2026, 3, 29, 1, 30, 0, 0, time.UTC)) {
		t.Fatalf("spring forward: %v", at)
	}
	if again, _ := night.next("start", at); !again.Equal(time.Date(2026, 3, 30, 0, 30, 0, 0, time.UTC)) {
		t.Fatalf("after spring forward: %v", again)
	}
	// Clocks go back on 25 October 2026: 02:30 exists twice, it counts once.
	first, _ := night.next("start", time.Date(2026, 10, 25, 0, 0, 0, 0, loc))
	second, _ := night.next("start", first)
	if !first.Equal(time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC)) || second.Sub(first) != 24*time.Hour {
		t.Fatalf("fall back: %v then %v", first, second)
	}
	if last, ok := night.last("start", first.Add(time.Hour)); !ok || !last.Equal(first) {
		t.Fatalf("last on the fall back day: %v", last)
	}
	// A morning start keeps its local time over the change.
	work := PowerSchedule{Enabled: true, Rules: []PowerRule{{Action: "start", Days: []int{1, 2, 3, 4, 5}, Time: "07:00"}, {Action: "stop", Days: []int{5}, Time: "18:00"}}}
	friday := time.Date(2026, 10, 23, 19, 0, 0, 0, loc)
	start, _ := work.next("start", friday)
	if !start.Equal(time.Date(2026, 10, 26, 6, 0, 0, 0, time.UTC)) {
		t.Fatalf("next Monday 07:00 CET: %v", start)
	}
	if stop, _ := work.next("stop", friday); !stop.Equal(time.Date(2026, 10, 30, 17, 0, 0, 0, time.UTC)) {
		t.Fatalf("next Friday 18:00 CET: %v", stop)
	}
	if last, ok := work.last("stop", friday); !ok || !last.Equal(time.Date(2026, 10, 23, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("last stop: %v", last)
	}
	if _, ok := (PowerSchedule{Rules: work.Rules}).next("start", friday); ok {
		t.Fatal("a switched off schedule has a next time")
	}
}

type powerEvents struct {
	mu       sync.Mutex
	notes    []string
	activity []string
}

func (e *powerEvents) notify(severity, kind, subject string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.notes = append(e.notes, severity+":"+kind+":"+subject)
}

func (e *powerEvents) record(action, target string, ok bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state := "ok"
	if !ok {
		state = "failed"
	}
	e.activity = append(e.activity, action+":"+target+":"+state)
}

func (e *powerEvents) last() (note, activity string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.notes) > 0 {
		note = e.notes[len(e.notes)-1]
	}
	if len(e.activity) > 0 {
		activity = e.activity[len(e.activity)-1]
	}
	return
}

// scheduledVM makes a VM with a schedule on Mondays: start at 07:00 and
// shut down at 07:30, which is forced off after a 30 "second" wait.
func scheduledVM(t *testing.T, m *Manager, mounted []string, since time.Time) *powerEvents {
	t.Helper()
	events := &powerEvents{}
	m.Timezone = func() string { return "Europe/Berlin" }
	m.Notify, m.Activity = events.notify, events.record
	m.powerUnit, m.powerPoll = time.Millisecond, time.Millisecond
	spec := VM{Name: "office", DisplayName: "Office", Template: "linux", MemoryMiB: 1024, VCPUs: 1,
		Disks: []Disk{{Format: "qcow2", SizeBytes: 2 * gib, Volume: "fast"}},
		PowerSchedule: PowerSchedule{Enabled: true, ForceOff: true, StopTimeout: 30, Rules: []PowerRule{
			{Action: "start", Days: []int{1}, Time: "07:00"}, {Action: "stop", Days: []int{1}, Time: "07:30"}}}}
	if err := m.Create(context.Background(), spec, mounted); err != nil {
		t.Fatal(err)
	}
	m.resetPower("office", since)
	return events
}

func (m *Manager) tick(t *testing.T, mounted []string, now time.Time) {
	t.Helper()
	m.now = func() time.Time { return now }
	m.runPowerSchedules(context.Background(), mounted, now)
	m.powerWork.Wait()
}

func TestPowerScheduleStartsAndStops(t *testing.T) {
	loc := berlin(t)
	m, runner := newTestManager(t)
	mounted := []string{"data", "fast"}
	monday := func(h, min int) time.Time { return time.Date(2026, 11, 2, h, min, 5, 0, loc) }
	events := scheduledVM(t, m, mounted, monday(6, 50))

	m.tick(t, mounted, monday(6, 59))
	if m.pid("office") != 0 {
		t.Fatal("started early")
	}
	view := m.List(mounted)[0].Schedule
	if view == nil || view.NextStart == nil || !view.NextStart.Equal(time.Date(2026, 11, 2, 6, 0, 0, 0, time.UTC)) || view.NextStop == nil {
		t.Fatalf("next times: %+v", view)
	}
	m.tick(t, mounted, monday(7, 0))
	if m.pid("office") == 0 {
		t.Fatalf("not started: %v", runner.commands)
	}
	if _, act := events.last(); act != "vm_schedule_start:office:ok" {
		t.Fatalf("activity %q", act)
	}
	// Someone shuts it down by hand: it stays down until the next start.
	if err := m.Action(context.Background(), "office", "poweroff"); err != nil {
		t.Fatal(err)
	}
	m.tick(t, mounted, monday(7, 5))
	if m.pid("office") != 0 {
		t.Fatal("started again after a manual stop")
	}
	// And started again by hand, the scheduled shutdown follows. The fake
	// guest ignores the ACPI request, so it is switched off after the wait.
	if err := m.Start(context.Background(), "office", mounted); err != nil {
		t.Fatal(err)
	}
	m.tick(t, mounted, monday(7, 30))
	if m.pid("office") != 0 {
		t.Fatal("still running after the scheduled stop")
	}
	note, act := events.last()
	if note != "warning:vm_schedule_forced:Office" || act != "vm_schedule_poweroff:office:ok" {
		t.Fatalf("forced off: %q %q", note, act)
	}
	if !strings.Contains(strings.Join(runner.commands, "\n"), "virsh --quiet shutdown office") {
		t.Fatalf("no ACPI request first: %v", runner.commands)
	}
	if next := m.List(mounted)[0].Schedule.NextStart; next == nil || !next.Equal(time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)) {
		t.Fatalf("next start a week later: %v", next)
	}
}

func TestPowerScheduleStopTimeoutWithoutForce(t *testing.T) {
	loc := berlin(t)
	m, _ := newTestManager(t)
	mounted := []string{"data", "fast"}
	events := scheduledVM(t, m, mounted, time.Date(2026, 11, 2, 7, 10, 0, 0, loc))
	vm, _ := m.get("office")
	vm.PowerSchedule.ForceOff = false
	m.vms["office"] = vm
	if err := m.Start(context.Background(), "office", mounted); err != nil {
		t.Fatal(err)
	}
	m.tick(t, mounted, time.Date(2026, 11, 2, 7, 30, 1, 0, loc))
	if m.pid("office") == 0 {
		t.Fatal("switched off without being allowed to")
	}
	if note, act := events.last(); note != "warning:vm_schedule_stop_timeout:Office" || act != "vm_schedule_stop:office:failed" {
		t.Fatalf("%q %q", note, act)
	}
}

func TestPowerScheduleWaitsForVolumeAndWork(t *testing.T) {
	loc := berlin(t)
	m, _ := newTestManager(t)
	mounted := []string{"data", "fast"}
	events := scheduledVM(t, m, mounted, time.Date(2026, 11, 2, 6, 50, 0, 0, loc))
	at := func(min, sec int) time.Time { return time.Date(2026, 11, 2, 7, min, sec, 0, loc) }

	// The volume is missing: the start waits and is given up after 30 minutes.
	m.tick(t, []string{"data"}, at(0, 5))
	if view := m.List(mounted)[0].Schedule; view.Waiting != "start" || view.WaitReason != "volume" {
		t.Fatalf("waiting: %+v", view)
	}
	m.tick(t, []string{"data"}, at(0, 30)) // not tried again within the minute
	m.tick(t, []string{"data"}, at(29, 0))
	if m.pid("office") != 0 {
		t.Fatal("started without its volume")
	}
	// A stop at 07:30 replaces the start that still waits.
	m.tick(t, []string{"data"}, at(31, 0))
	if view := m.List(mounted)[0].Schedule; view.Waiting != "" {
		t.Fatalf("still waiting: %+v", view)
	}
	if note, act := events.last(); note != "warning:vm_schedule_start_skipped:Office" || act != "vm_schedule_start:office:failed" {
		t.Fatalf("left out: %q %q", note, act)
	}

	// Next week a snapshot or clone holds the VM: the start waits for it.
	next := func(min int) time.Time { return time.Date(2026, 11, 9, 7, min, 5, 0, loc) }
	lock, err := m.operation("office")
	if err != nil {
		t.Fatal(err)
	}
	m.tick(t, mounted, next(0))
	if view := m.List(mounted)[0].Schedule; view.WaitReason != "busy" || m.pid("office") != 0 {
		t.Fatalf("busy: %+v", view)
	}
	lock.Unlock()
	m.tick(t, mounted, next(2))
	if m.pid("office") == 0 {
		t.Fatal("not started after the work ended")
	}
	if _, act := events.last(); act != "vm_schedule_start:office:ok" {
		t.Fatalf("activity %q", act)
	}

	// Disk copy still running past the deadline: skipped with a warning.
	m.Action(context.Background(), "office", "poweroff")
	m.mu.Lock()
	m.jobs["office"] = &Job{State: "running"}
	m.mu.Unlock()
	later := func(min int) time.Time { return time.Date(2026, 11, 16, 7, min, 5, 0, loc) }
	events.notes = nil
	m.tick(t, mounted, later(0))
	m.tick(t, mounted, later(29)) // 07:29 is the last try before the deadline
	if note, _ := events.last(); note != "" {
		t.Fatalf("given up early: %q", note)
	}
	m.tick(t, mounted, later(31))
	if note, act := events.last(); note != "warning:vm_schedule_start_skipped:Office" || act != "vm_schedule_start:office:failed" {
		t.Fatalf("skipped: %q %q", note, act)
	}
}

func TestPowerScheduleYieldsToNASShutdown(t *testing.T) {
	loc := berlin(t)
	m, _ := newTestManager(t)
	mounted := []string{"data", "fast"}
	events := scheduledVM(t, m, mounted, time.Date(2026, 11, 2, 6, 50, 0, 0, loc))
	m.PowerOffSoon = func() bool { return true }
	m.tick(t, mounted, time.Date(2026, 11, 2, 7, 0, 5, 0, loc))
	if m.pid("office") != 0 {
		t.Fatal("started although the NAS shuts down")
	}
	if note, act := events.last(); note != "info:vm_schedule_start_power:Office" || act != "vm_schedule_start:office:failed" {
		t.Fatalf("%q %q", note, act)
	}
}

func TestPowerScheduleSurvivesTitandRestart(t *testing.T) {
	loc := berlin(t)
	m, _ := newTestManager(t)
	mounted := []string{"data", "fast"}
	scheduledVM(t, m, mounted, time.Date(2026, 11, 2, 6, 50, 0, 0, loc))
	m.tick(t, mounted, time.Date(2026, 11, 2, 7, 0, 5, 0, loc))
	if m.pid("office") == 0 {
		t.Fatal("not started")
	}
	m.Action(context.Background(), "office", "poweroff")

	// titand restarts (an app update): /run keeps what was done.
	again := NewManager(m.store, os.Getgid())
	again.mountRoot, again.runDir, again.pidDir, again.kvm, again.runner = m.mountRoot, m.runDir, m.pidDir, m.kvm, m.runner
	again.runGid, again.runUid, again.autostartFile, again.powerFile = m.runGid, m.runUid, m.autostartFile, m.powerFile
	again.Timezone = m.Timezone
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if v, _ := again.get("office"); !v.PowerSchedule.Enabled || len(v.PowerSchedule.Rules) != 2 {
		t.Fatalf("schedule not kept in /config: %+v", v.PowerSchedule)
	}
	again.tick(t, mounted, time.Date(2026, 11, 2, 7, 3, 0, 0, loc))
	if again.pid("office") != 0 {
		t.Fatal("started twice after a restart of titand")
	}

	// The NAS itself started (empty /run): a start up to 15 minutes ago is
	// still made, as after waking up for it; a missed shutdown is not.
	os.Remove(m.powerFile)
	boot := NewManager(m.store, os.Getgid())
	boot.mountRoot, boot.runDir, boot.pidDir, boot.kvm, boot.runner = m.mountRoot, m.runDir, m.pidDir, m.kvm, m.runner
	boot.runGid, boot.runUid, boot.autostartFile, boot.powerFile = m.runGid, m.runUid, m.autostartFile, m.powerFile
	boot.Timezone = m.Timezone
	boot.Load()
	boot.tick(t, mounted, time.Date(2026, 11, 2, 7, 10, 0, 0, loc))
	if boot.pid("office") == 0 {
		t.Fatal("start not made up after the NAS woke")
	}
	os.Remove(m.powerFile)
	woke := NewManager(m.store, os.Getgid())
	woke.mountRoot, woke.runDir, woke.pidDir, woke.kvm, woke.runner = m.mountRoot, m.runDir, m.pidDir, m.kvm, m.runner
	woke.powerFile, woke.Timezone = m.powerFile, m.Timezone
	woke.Load()
	woke.tick(t, mounted, time.Date(2026, 11, 2, 7, 40, 0, 0, loc))
	if woke.pid("office") == 0 {
		t.Fatal("a shutdown missed by ten minutes was carried out")
	}
	os.Remove(m.powerFile)
	late := NewManager(m.store, os.Getgid())
	late.mountRoot, late.pidDir, late.powerFile, late.Timezone = m.mountRoot, m.pidDir, m.powerFile, m.Timezone
	late.Load()
	late.Disabled = true // looks only
	late.runPowerSchedules(context.Background(), mounted, time.Date(2026, 11, 2, 7, 20, 0, 0, loc))
	if late.powerPending["office"] != nil {
		t.Fatal("a start 20 minutes ago is made up")
	}
}

func TestPowerScheduleStoredWithTheVM(t *testing.T) {
	m, _ := newTestManager(t)
	mounted := []string{"data", "fast"}
	ctx := context.Background()
	spec := VM{Name: "plain", Template: "linux", MemoryMiB: 1024, VCPUs: 1, Disks: []Disk{{Format: "qcow2", SizeBytes: 2 * gib, Volume: "fast"}}}
	if err := m.Create(ctx, spec, mounted); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(filepath.Dir(m.mountRoot), "config", "vms", "plain.json"))
	if strings.Contains(string(data), "powerSchedule") {
		t.Fatalf("an unused schedule is written: %s", data)
	}
	if m.List(mounted)[0].Schedule != nil {
		t.Fatal("a VM without schedule shows next times")
	}
	// The schedule changes while the VM runs; an update without it keeps it.
	if err := m.Start(ctx, "plain", mounted); err != nil {
		t.Fatal(err)
	}
	change := Change{MemoryMiB: 1024, VCPUs: 1, Network: "user", PowerSchedule: &PowerSchedule{Enabled: true, Rules: []PowerRule{{Action: "stop", Days: []int{3, 1}, Time: "23:15"}}}}
	if err := m.Update(ctx, "plain", change, mounted); err != nil {
		t.Fatal(err)
	}
	change.PowerSchedule = nil
	change.Description = "kept"
	if err := m.Update(ctx, "plain", change, mounted); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(filepath.Dir(m.mountRoot), "config", "vms", "plain.json"))
	var stored struct {
		PowerSchedule PowerSchedule `json:"powerSchedule"`
	}
	if err := json.Unmarshal(data, &stored); err != nil || !stored.PowerSchedule.Enabled || stored.PowerSchedule.Rules[0].Days[0] != 1 || stored.PowerSchedule.StopTimeout != defaultStopTimeout {
		t.Fatalf("stored: %s", data)
	}
	bad := Change{MemoryMiB: 1024, VCPUs: 1, Network: "user", PowerSchedule: &PowerSchedule{Enabled: true}}
	if err := m.Update(ctx, "plain", bad, mounted); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid schedule accepted: %v", err)
	}
	// A clone does not take the schedule along.
	m.Action(ctx, "plain", "poweroff")
}
