package system

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// The power schedule: shut down at set times, wake again through the clock
// of the mainboard (RTC), and Wake-on-LAN. Shutting down waits for running
// work; the RTC alarm and Wake-on-LAN depend on the hardware, which the
// status says.

const powerScheduleFile = "system/power-schedule.json"

// ScheduleTime is a clock time on some weekdays (0 = Sunday).
type ScheduleTime struct {
	Enabled bool   `json:"enabled"`
	Days    []int  `json:"days"`
	Time    string `json:"time"`
}

// PowerSchedule is /config/system/power-schedule.json.
type PowerSchedule struct {
	Off       ScheduleTime `json:"off"`
	On        ScheduleTime `json:"on"`
	WakeOnLAN bool         `json:"wakeOnLan"`
}

// WakeInterface is a network port and what it can do for Wake-on-LAN.
type WakeInterface struct {
	Name      string `json:"name"`
	MAC       string `json:"mac"`
	Supported bool   `json:"supported"`
	Enabled   bool   `json:"enabled"`
}

// PowerScheduleStatus is the schedule with what the hardware offers.
type PowerScheduleStatus struct {
	Settings   PowerSchedule   `json:"settings"`
	NextOff    *time.Time      `json:"nextOff,omitempty"`
	NextOn     *time.Time      `json:"nextOn,omitempty"`
	RTC        bool            `json:"rtc"`
	Interfaces []WakeInterface `json:"interfaces"`
	// Postponed is set while a shutdown waits for running work.
	Postponed string `json:"postponed,omitempty"`
	ReadOnly  bool   `json:"readOnly"`
	Error     string `json:"error,omitempty"`
}

// ErrPowerSchedule means a setting is not acceptable.
var ErrPowerSchedule = errors.New("system: invalid power schedule")

// PowerScheduler keeps the schedule and carries it out.
type PowerScheduler struct {
	store *config.Store
	// Disabled leaves hardware and power alone (development mode).
	Disabled bool
	// Busy says whether work is running that a shutdown would break off.
	Busy func() (bool, string)
	// Poweroff shuts the NAS down; Notify reports postponing and giving up.
	Poweroff func() error
	Notify   func(severity, kind, subject string)

	sys string
	now func() time.Time
	// wolGet and wolSet reach the network card; tests replace them.
	wolGet func(name string) (supported, enabled bool, err error)
	wolSet func(name string, enable bool) error
	mu     sync.Mutex
	set    PowerSchedule
	// postponedSince is when a due shutdown started to wait.
	postponedSince time.Time
	postponedFor   string
	problem        string
	loaded         bool
}

// NewPowerScheduler returns a scheduler with nothing scheduled.
func NewPowerScheduler(store *config.Store) *PowerScheduler {
	return &PowerScheduler{store: store, sys: "/sys", now: time.Now, wolGet: getWakeOnLAN, wolSet: setWakeOnLAN}
}

func validTime(t ScheduleTime) bool {
	if !t.Enabled {
		return true
	}
	if _, err := time.Parse("15:04", t.Time); err != nil || len(t.Days) == 0 {
		return false
	}
	for _, d := range t.Days {
		if d < 0 || d > 6 {
			return false
		}
	}
	return true
}

// next returns the next time after now on one of the days, in local time.
func (t ScheduleTime) next(now time.Time) (time.Time, bool) {
	if !t.Enabled {
		return time.Time{}, false
	}
	clock, err := time.Parse("15:04", t.Time)
	if err != nil {
		return time.Time{}, false
	}
	for i := 0; i <= 7; i++ {
		day := now.AddDate(0, 0, i)
		at := time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
		if at.After(now) && slices.Contains(t.Days, int(at.Weekday())) {
			return at, true
		}
	}
	return time.Time{}, false
}

// Load reads the schedule and sets Wake-on-LAN and the wake alarm.
func (p *PowerScheduler) Load() error {
	var s PowerSchedule
	if err := p.store.ReadJSON(powerScheduleFile, &s); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.set, p.loaded = s, true
	p.problem = errorString(p.applyLocked())
	return nil
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Set stores a new schedule and applies it.
func (p *PowerScheduler) Set(s PowerSchedule) error {
	if !validTime(s.Off) || !validTime(s.On) {
		return fmt.Errorf("%w: choose at least one day and a time", ErrPowerSchedule)
	}
	for _, t := range []*ScheduleTime{&s.Off, &s.On} {
		t.Days = slices.Compact(slices.Sorted(slices.Values(t.Days)))
		if t.Days == nil {
			t.Days = []int{}
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.loaded {
		return errors.New("power schedule was not loaded")
	}
	if err := p.store.WriteJSON(powerScheduleFile, s); err != nil {
		return err
	}
	p.set = s
	p.postponedSince, p.postponedFor = time.Time{}, ""
	err := p.applyLocked()
	p.problem = errorString(err)
	return err
}

// applyLocked sets Wake-on-LAN on every port that has it, and the wake
// alarm to the next time to switch on.
func (p *PowerScheduler) applyLocked() error {
	if p.Disabled {
		return nil
	}
	var failures []error
	for _, iface := range p.interfaces() {
		if !iface.Supported {
			continue
		}
		if err := p.wolSet(iface.Name, p.set.WakeOnLAN); err != nil {
			failures = append(failures, fmt.Errorf("Wake-on-LAN on %s: %w", iface.Name, err))
		}
	}
	if err := p.setAlarm(); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (p *PowerScheduler) alarmPath() string { return filepath.Join(p.sys, "class/rtc/rtc0/wakealarm") }

// setAlarm writes the next switch-on time into the RTC, or clears it.
func (p *PowerScheduler) setAlarm() error {
	path := p.alarmPath()
	if _, err := os.Stat(path); err != nil {
		if p.set.On.Enabled {
			return errors.New("this mainboard offers no wake alarm (RTC)")
		}
		return nil
	}
	write := func(v string) error { return os.WriteFile(path, []byte(v), 0) }
	// The alarm only takes a new time after it was cleared.
	if err := write("0"); err != nil {
		return fmt.Errorf("wake alarm: %w", err)
	}
	if at, ok := p.set.On.next(p.now()); ok {
		if err := write(strconv.FormatInt(at.Unix(), 10)); err != nil {
			return fmt.Errorf("wake alarm: %w", err)
		}
	}
	return nil
}

// interfaces lists the physical network ports.
func (p *PowerScheduler) interfaces() []WakeInterface {
	entries, _ := os.ReadDir(filepath.Join(p.sys, "class/net"))
	var list []WakeInterface
	for _, e := range entries {
		name := e.Name()
		// Only real devices: they have a device link; bridges, bonds and
		// VLANs have none.
		if _, err := os.Stat(filepath.Join(p.sys, "class/net", name, "device")); err != nil {
			continue
		}
		mac, _ := os.ReadFile(filepath.Join(p.sys, "class/net", name, "address"))
		iface := WakeInterface{Name: name, MAC: strings.TrimSpace(string(mac))}
		if _, err := net.ParseMAC(iface.MAC); err != nil {
			continue
		}
		if !p.Disabled {
			iface.Supported, iface.Enabled, _ = p.wolGet(name)
		}
		list = append(list, iface)
	}
	return list
}

// Status returns the schedule, the next times and what the hardware offers.
func (p *PowerScheduler) Status() PowerScheduleStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	st := PowerScheduleStatus{Settings: p.set, Interfaces: p.interfaces(), ReadOnly: p.Disabled, Error: p.problem}
	if st.Interfaces == nil {
		st.Interfaces = []WakeInterface{}
	}
	if _, err := os.Stat(p.alarmPath()); err == nil {
		st.RTC = true
	}
	if at, ok := p.set.Off.next(now); ok {
		st.NextOff = &at
	}
	if at, ok := p.set.On.next(now); ok {
		st.NextOn = &at
	}
	if !p.postponedSince.IsZero() {
		st.Postponed = p.postponedFor
	}
	return st
}

// maxPostpone is how long a due shutdown waits for running work.
const maxPostpone = 6 * time.Hour

// Run checks the schedule every half minute until ctx ends.
func (p *PowerScheduler) Run(ctx context.Context) {
	p.mu.Lock()
	due, _ := p.set.Off.next(p.now())
	p.mu.Unlock()
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		due = p.step(due)
	}
}

// step looks once: due is the shutdown waited for; it returns the next one.
func (p *PowerScheduler) step(due time.Time) time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	next, scheduled := p.set.Off.next(now)
	if !scheduled {
		p.postponedSince = time.Time{}
		return time.Time{}
	}
	// A changed schedule moves the time waited for.
	if due.IsZero() || p.postponedSince.IsZero() && due.After(now) {
		return next
	}
	if now.Before(due) {
		return due
	}
	if p.Busy != nil {
		if busy, why := p.Busy(); busy {
			if p.postponedSince.IsZero() {
				p.postponedSince, p.postponedFor = now, why
				p.notify("info", "power_postponed", why)
			}
			if now.Sub(p.postponedSince) < maxPostpone {
				return due // look again in half a minute
			}
			p.notify("warning", "power_skipped", why)
			p.postponedSince, p.postponedFor = time.Time{}, ""
			return next
		}
	}
	p.postponedSince, p.postponedFor = time.Time{}, ""
	if p.Disabled || p.Poweroff == nil {
		slog.Info("scheduled shutdown not carried out (development mode)")
		return next
	}
	// The wake alarm is set again right before, in case it passed meanwhile.
	if err := p.setAlarm(); err != nil {
		slog.Warn("wake alarm before scheduled shutdown", "err", err)
	}
	slog.Info("scheduled shutdown")
	if err := p.Poweroff(); err != nil {
		slog.Error("scheduled shutdown failed", "err", err)
	}
	return next
}

func (p *PowerScheduler) notify(severity, kind, subject string) {
	if p.Notify != nil {
		p.Notify(severity, kind, subject)
	}
}
