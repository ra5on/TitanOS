package vm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// The power schedule of a VM: start it and shut it down at set times on
// chosen weekdays. It is kept with the VM in /config/vms/<name>.json; what
// was already carried out lives in RAM and in /run, so that a restart of
// titand (an app update) does not start or stop anything twice. Nothing is
// read from /config to carry it out.

// PowerRule starts or shuts down a VM at one time on some weekdays.
type PowerRule struct {
	// Action is "start" or "stop".
	Action string `json:"action"`
	// Days are the weekdays, Sunday = 0.
	Days []int `json:"days"`
	// Time is the local time of the NAS, "15:04".
	Time string `json:"time"`
}

// PowerSchedule is when a VM is started and shut down on its own.
type PowerSchedule struct {
	Enabled bool        `json:"enabled"`
	Rules   []PowerRule `json:"rules"`
	// StopTimeout is how many seconds a scheduled shutdown waits for the
	// guest to power off after the ACPI request.
	StopTimeout int `json:"stopTimeout,omitempty"`
	// ForceOff switches the VM off when the guest did not shut down in time.
	ForceOff bool `json:"forceOff,omitempty"`
}

const (
	maxPowerRules      = 16
	defaultStopTimeout = 300
	minStopTimeout     = 30
	maxStopTimeout     = 3600
	// powerCatchUp is how late a scheduled action may still be carried out:
	// after a restart of titand, or a start right after the NAS woke up.
	powerCatchUp = 15 * time.Minute
	// powerRetryFor is how long a start or stop waits for a missing volume
	// or a running snapshot, clone or disk copy before it is left out.
	powerRetryFor   = 30 * time.Minute
	powerRetryEvery = time.Minute
)

var powerActions = []string{"start", "stop"}

// checkPowerSchedule validates a schedule and puts it into its normal form.
func checkPowerSchedule(s *PowerSchedule) error {
	invalid := func(why string) error { return fmt.Errorf("%w: power schedule: %s", ErrInvalid, why) }
	if len(s.Rules) > maxPowerRules {
		return invalid("too many rules")
	}
	if s.Enabled && len(s.Rules) == 0 {
		return invalid("no rule")
	}
	at := map[string]string{}
	for i := range s.Rules {
		r := &s.Rules[i]
		if !slices.Contains(powerActions, r.Action) {
			return invalid("unknown action")
		}
		clock, err := time.Parse("15:04", r.Time)
		if err != nil || len(r.Time) != 5 {
			return invalid("time")
		}
		r.Time = clock.Format("15:04")
		if len(r.Days) == 0 {
			return invalid("no weekday")
		}
		for _, d := range r.Days {
			if d < 0 || d > 6 {
				return invalid("weekday")
			}
		}
		r.Days = slices.Compact(slices.Sorted(slices.Values(r.Days)))
		for _, d := range r.Days {
			key := fmt.Sprint(d, r.Time)
			if other, ok := at[key]; ok && other != r.Action {
				return invalid("start and stop at the same time")
			}
			at[key] = r.Action
		}
	}
	if s.Rules == nil {
		s.Rules = []PowerRule{}
	}
	if s.StopTimeout == 0 {
		s.StopTimeout = defaultStopTimeout
	}
	if s.StopTimeout < minStopTimeout || s.StopTimeout > maxStopTimeout {
		return invalid("timeout")
	}
	if !s.Enabled && len(s.Rules) == 0 && !s.ForceOff && s.StopTimeout == defaultStopTimeout {
		// Nothing set: kept out of the file of the VM.
		*s = PowerSchedule{}
	}
	return nil
}

// slotOn returns when a rule acts on a calendar day, if it does. Days and
// times follow the local calendar of loc: a time that does not exist on the
// day clocks go forward comes an hour later, one that exists twice on the
// day they go back counts once.
func (r PowerRule) slotOn(year int, month time.Month, day int, loc *time.Location) (time.Time, bool) {
	clock, err := time.Parse("15:04", r.Time)
	if err != nil {
		return time.Time{}, false
	}
	noon := time.Date(year, month, day, 12, 0, 0, 0, loc)
	if !slices.Contains(r.Days, int(noon.Weekday())) {
		return time.Time{}, false
	}
	return time.Date(year, month, day, clock.Hour(), clock.Minute(), 0, 0, loc), true
}

// next returns the first time after now the schedule does action.
func (s PowerSchedule) next(action string, now time.Time) (time.Time, bool) {
	var best time.Time
	if !s.Enabled {
		return best, false
	}
	for i := 0; i <= 8; i++ {
		day := time.Date(now.Year(), now.Month(), now.Day()+i, 12, 0, 0, 0, now.Location())
		for _, r := range s.Rules {
			if r.Action != action {
				continue
			}
			if at, ok := r.slotOn(day.Year(), day.Month(), day.Day(), now.Location()); ok && at.After(now) && (best.IsZero() || at.Before(best)) {
				best = at
			}
		}
		if !best.IsZero() {
			return best, true
		}
	}
	return best, false
}

// last returns the latest time at or before now, at most a day back, the
// schedule did action.
func (s PowerSchedule) last(action string, now time.Time) (time.Time, bool) {
	var best time.Time
	if !s.Enabled {
		return best, false
	}
	for i := 0; i >= -1; i-- {
		day := time.Date(now.Year(), now.Month(), now.Day()+i, 12, 0, 0, 0, now.Location())
		for _, r := range s.Rules {
			if r.Action != action {
				continue
			}
			if at, ok := r.slotOn(day.Year(), day.Month(), day.Day(), now.Location()); ok && !at.After(now) && at.After(best) {
				best = at
			}
		}
		if !best.IsZero() {
			return best, true
		}
	}
	return best, false
}

// powerPending is a scheduled start or stop that is due and not done yet.
type powerPending struct {
	action string
	slot   time.Time
	// until is when it is given up, retry when it is tried again.
	until, retry time.Time
	// wait says why it waits: "volume" or "busy".
	wait string
	// active is set while a start or stop is carried out.
	active bool
}

// powerRecord is what /run keeps across restarts of titand.
type powerRecord struct {
	Handled map[string]time.Time `json:"handled"`
}

func (m *Manager) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func powerKey(name, action string) string { return name + "/" + action }

// zone returns the time zone of the NAS as it is set now. Go keeps
// time.Local from the start of titand.
func (m *Manager) zone() *time.Location {
	if m.Timezone == nil {
		return time.Local
	}
	name := m.Timezone()
	m.powerMu.Lock()
	defer m.powerMu.Unlock()
	if m.powerZone != nil && m.powerZone.String() == name {
		return m.powerZone
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.Local
	}
	m.powerZone = loc
	return loc
}

// loadPowerLocked reads what was carried out before a restart of titand.
// Without that record the NAS itself has just started: a start up to
// powerCatchUp ago is still made (the NAS may have woken for it), a missed
// shutdown is not.
func (m *Manager) loadPowerLocked(now time.Time) {
	if m.powerHandled != nil {
		return
	}
	m.powerHandled, m.powerPending = map[string]time.Time{}, map[string]*powerPending{}
	data, err := os.ReadFile(m.powerFile)
	if err == nil {
		var record powerRecord
		if json.Unmarshal(data, &record) == nil && record.Handled != nil {
			m.powerHandled = record.Handled
			m.powerBoot = false
			return
		}
	}
	m.powerBoot = true
}

func (m *Manager) savePowerLocked() {
	data, _ := json.Marshal(powerRecord{Handled: m.powerHandled})
	if err := os.MkdirAll(filepath.Dir(m.powerFile), 0o700); err == nil {
		err = os.WriteFile(m.powerFile, data, 0o600)
		if err != nil {
			slog.Warn("VM schedule state not noted", "err", err)
		}
	}
}

// resetPower forgets what is due for a VM: its schedule changed or it is
// gone. Times up to now count as handled.
func (m *Manager) resetPower(name string, now time.Time) {
	m.powerMu.Lock()
	defer m.powerMu.Unlock()
	m.loadPowerLocked(now)
	if p := m.powerPending[name]; p != nil && p.active {
		return // a start or stop is being carried out; it ends by itself
	}
	delete(m.powerPending, name)
	for _, action := range powerActions {
		m.powerHandled[powerKey(name, action)] = now
	}
	m.savePowerLocked()
}

// PowerView is what the VM page shows about the schedule.
type PowerView struct {
	NextStart *time.Time `json:"nextStart,omitempty"`
	NextStop  *time.Time `json:"nextStop,omitempty"`
	// Waiting is the action that waits for a volume or other work.
	Waiting string `json:"waiting,omitempty"`
	// WaitReason is "volume" or "busy".
	WaitReason string `json:"waitReason,omitempty"`
}

// powerView returns next times and waiting actions. m.mu may be held.
func (m *Manager) powerView(v VM, loc *time.Location, now time.Time) *PowerView {
	if !v.PowerSchedule.Enabled {
		return nil
	}
	now = now.In(loc)
	view := &PowerView{}
	if at, ok := v.PowerSchedule.next("start", now); ok {
		view.NextStart = &at
	}
	if at, ok := v.PowerSchedule.next("stop", now); ok {
		view.NextStop = &at
	}
	m.powerMu.Lock()
	if p := m.powerPending[v.Name]; p != nil {
		view.Waiting, view.WaitReason = p.action, p.wait
	}
	m.powerMu.Unlock()
	return view
}

// runPowerSchedules starts and stops VMs whose time has come.
func (m *Manager) runPowerSchedules(ctx context.Context, mounted []string, now time.Time) {
	now = now.In(m.zone())
	m.mu.Lock()
	scheduled := map[string]VM{}
	for name, v := range m.vms {
		if v.PowerSchedule.Enabled {
			scheduled[name] = v
		}
	}
	m.mu.Unlock()

	m.powerMu.Lock()
	m.loadPowerLocked(now)
	changed := false
	var dropped []powerPending
	for key := range m.powerHandled {
		if name, _, _ := cutKey(key); scheduled[name].Name == "" {
			delete(m.powerHandled, key)
			changed = true
		}
	}
	for name, p := range m.powerPending {
		if scheduled[name].Name == "" && !p.active {
			delete(m.powerPending, name)
		}
	}
	for name, v := range scheduled {
		for _, action := range powerActions {
			key := powerKey(name, action)
			handled, known := m.powerHandled[key]
			if !known {
				// A schedule seen for the first time: only after the start
				// of the NAS may a start just missed still be made.
				if !(m.powerBoot && action == "start") {
					m.powerHandled[key] = now
					changed = true
					continue
				}
			}
			slot, ok := v.PowerSchedule.last(action, now)
			if !ok || !slot.After(handled) || now.Sub(slot) > powerCatchUp {
				continue
			}
			m.powerHandled[key] = slot
			changed = true
			if old := m.powerPending[name]; old != nil {
				if old.active {
					continue // the one being carried out finishes first
				}
				slog.Info("scheduled VM action replaced by a later one", "vm", name, "left out", old.action, "now", action)
				dropped = append(dropped, powerPending{action: old.action, wait: name})
			}
			m.powerPending[name] = &powerPending{action: action, slot: slot, until: slot.Add(powerRetryFor)}
		}
	}
	if changed {
		m.savePowerLocked()
	}
	var due []string
	for name, p := range m.powerPending {
		if !p.active && !now.Before(p.retry) {
			due = append(due, name)
		}
	}
	m.powerMu.Unlock()
	for _, old := range dropped {
		// A start or stop that still waited is left out: a later one is due.
		v := scheduled[old.wait]
		m.powerNote(v, old.action, false)
		m.notify("warning", "vm_schedule_"+old.action+"_skipped", v.Label())
	}
	slices.Sort(due)
	for _, name := range due {
		m.tryPower(ctx, scheduled[name], mounted, now)
	}
}

func cutKey(key string) (name, action string, ok bool) {
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '/' {
			return key[:i], key[i+1:], true
		}
	}
	return key, "", false
}

// busyFor reports whether a snapshot, clone, restore or disk copy runs for a VM.
func (m *Manager) busyFor(name string) bool {
	m.mu.Lock()
	job, snapshot := m.jobs[name], m.snapshotJobs[name]
	m.mu.Unlock()
	if job != nil || snapshot != nil && snapshot.State == "running" {
		return true
	}
	lock, err := m.operation(name)
	if err != nil {
		return true
	}
	lock.Unlock()
	return false
}

// tryPower carries out a due start or stop, or lets it wait.
func (m *Manager) tryPower(ctx context.Context, v VM, mounted []string, now time.Time) {
	m.powerMu.Lock()
	p := m.powerPending[v.Name]
	if p == nil || p.active {
		m.powerMu.Unlock()
		return
	}
	action := p.action
	m.powerMu.Unlock()
	done := func() {
		m.powerMu.Lock()
		if m.powerPending[v.Name] == p {
			delete(m.powerPending, v.Name)
		}
		m.powerMu.Unlock()
	}
	wait := func(why string) {
		m.powerMu.Lock()
		defer m.powerMu.Unlock()
		if now.After(p.until) {
			if m.powerPending[v.Name] == p {
				delete(m.powerPending, v.Name)
			}
			slog.Warn("scheduled VM action left out", "vm", v.Name, "action", action, "waited for", why)
			m.powerNote(v, action, false)
			m.notify("warning", "vm_schedule_"+action+"_skipped", v.Label())
			return
		}
		p.wait, p.retry = why, now.Add(powerRetryEvery)
	}
	running := m.isRunning(v.Name)
	switch {
	case action == "start" && running, action == "stop" && !running:
		done()
		return
	case action == "start" && m.PowerOffSoon != nil && m.PowerOffSoon():
		// The NAS shuts down by its own schedule: that wins.
		done()
		slog.Info("scheduled VM start left out: the NAS shuts down by schedule", "vm", v.Name)
		m.powerNote(v, action, false)
		m.notify("info", "vm_schedule_start_power", v.Label())
		return
	case action == "start" && !m.available(v, mounted):
		wait("volume")
		return
	case m.busyFor(v.Name):
		wait("busy")
		return
	}
	m.powerMu.Lock()
	p.active, p.wait = true, ""
	m.powerMu.Unlock()
	m.powerWork.Add(1)
	go func() {
		defer m.powerWork.Done()
		var err error
		if action == "start" {
			err = m.Start(ctx, v.Name, mounted)
		} else {
			err = m.scheduledStop(ctx, v)
		}
		m.powerMu.Lock()
		p.active = false
		m.powerMu.Unlock()
		switch {
		case err == nil, action == "start" && errors.Is(err, ErrRunning), action == "stop" && errors.Is(err, ErrNotRunning):
			done()
			if err == nil {
				slog.Info("scheduled VM "+action, "vm", v.Name)
				m.powerNote(v, action, true)
			}
		case errors.Is(err, ErrSnapshotBusy), errors.Is(err, ErrPreparing):
			wait("busy")
		case errors.Is(err, ErrUnknownVolume):
			wait("volume")
		case errors.Is(err, errStopTimeout):
			done()
			slog.Warn("scheduled VM shutdown: the guest did not power off in time", "vm", v.Name)
			m.powerNote(v, action, false)
			m.notify("warning", "vm_schedule_stop_timeout", v.Label())
		case errors.Is(err, errForcedOff):
			done()
			slog.Warn("scheduled VM shutdown: switched off after the timeout", "vm", v.Name)
			m.powerNote(v, "poweroff", true)
			m.notify("warning", "vm_schedule_forced", v.Label())
		default:
			done()
			slog.Error("scheduled VM "+action+" failed", "vm", v.Name, "err", err)
			m.powerNote(v, action, false)
			m.notify("error", "vm_schedule_"+action+"_failed", v.Label())
		}
	}()
}

var (
	errStopTimeout = errors.New("vm: the guest did not shut down in time")
	errForcedOff   = errors.New("vm: switched off after the guest did not shut down in time")
)

// isRunning reports whether a VM runs or is being started.
func (m *Manager) isRunning(name string) bool {
	m.mu.Lock()
	starting := m.starting[name]
	m.mu.Unlock()
	return starting || m.pid(name) != 0
}

// scheduledStop asks the guest to shut down, waits for it and, if set,
// switches the VM off when it did not.
func (m *Manager) scheduledStop(ctx context.Context, v VM) error {
	if err := m.Action(ctx, v.Name, "shutdown"); err != nil {
		return err
	}
	deadline := time.Now().Add(time.Duration(v.PowerSchedule.StopTimeout) * m.powerUnit)
	for m.pid(v.Name) != 0 {
		if time.Now().After(deadline) {
			if !v.PowerSchedule.ForceOff {
				return errStopTimeout
			}
			if err := m.Action(ctx, v.Name, "poweroff"); err != nil && !errors.Is(err, ErrNotRunning) {
				return err
			}
			return errForcedOff
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.powerPoll):
		}
	}
	return nil
}

// powerNote records a scheduled action in the activity log.
func (m *Manager) powerNote(v VM, action string, ok bool) {
	if m.Activity != nil {
		m.Activity("vm_schedule_"+action, v.Name, ok)
	}
}

func (m *Manager) notify(severity, kind, subject string) {
	if m.Notify != nil {
		m.Notify(severity, kind, subject)
	}
}
