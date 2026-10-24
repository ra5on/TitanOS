package backup

import (
	"fmt"
	"slices"
	"time"
)

// A schedule says when a job is owed a run. Jobs written before schedules
// grew only have Every (daily or weekly), Weekday and Time; they keep working:
// an empty Weekdays means "Weekday", an empty Times means "Time".
//
// Every is one of
//   - hourly: every Interval hours (1, 2, 3, 4, 6, 12) from midnight; the
//     minute is the one of the first time.
//   - daily: at each of the times.
//   - weekly: on each of the weekdays (0 = Sunday), at each of the times.
//   - monthly: on MonthDay (1 to 28), at each of the times.
//
// With a window (WindowFrom to WindowTo, HH:MM, may pass midnight) a run
// that is owed waits until the window is open.

// intervals are the hours "hourly" may repeat after.
var intervals = []int{1, 2, 3, 4, 6, 12}

const (
	// retryAfter is how long a failed attempt is left alone. A target that
	// is not reachable right now (the other NAS is off) is tried again.
	retryAfter = 30 * time.Minute
	// lookBack reaches back over the longest gap between two runs.
	lookBack  = 35 * 24 * time.Hour
	lookAhead = 62 * 24 * time.Hour
	// overdueAfter is the least time without a backup that is worth a
	// warning, however often a job is meant to run.
	overdueAfter = 48 * time.Hour
)

// times returns the times of day a job runs at, in order.
func (j Job) times() []string {
	times := slices.Clone(j.Times)
	if len(times) == 0 {
		times = []string{j.Time}
	}
	slices.Sort(times)
	return slices.Compact(times)
}

func (j Job) weekdays() []int {
	if len(j.Weekdays) == 0 {
		return []int{j.Weekday}
	}
	return j.Weekdays
}

func clock(text string) (hour, minute int) {
	fmt.Sscanf(text, "%d:%d", &hour, &minute)
	return
}

// checkSchedule tells whether the schedule of a job makes sense.
func checkSchedule(j Job) error {
	if j.Time != "" && !clockForm.MatchString(j.Time) || j.Weekday < 0 || j.Weekday > 6 {
		return invalid("schedule")
	}
	if len(j.Times) > 8 || len(j.Times) == 0 && !clockForm.MatchString(j.Time) {
		return invalid("schedule")
	}
	for _, text := range j.Times {
		if !clockForm.MatchString(text) {
			return invalid("schedule")
		}
	}
	switch j.Every {
	case "daily":
	case "hourly":
		if !slices.Contains(intervals, j.Interval) {
			return invalid("schedule")
		}
	case "weekly":
		if len(j.Weekdays) > 7 {
			return invalid("schedule")
		}
		for _, day := range j.Weekdays {
			if day < 0 || day > 6 {
				return invalid("schedule")
			}
		}
	case "monthly":
		if j.MonthDay < 1 || j.MonthDay > 28 {
			return invalid("schedule")
		}
	default:
		return invalid("schedule")
	}
	if (j.WindowFrom == "") != (j.WindowTo == "") ||
		j.WindowFrom != "" && (!clockForm.MatchString(j.WindowFrom) || !clockForm.MatchString(j.WindowTo) || j.WindowFrom == j.WindowTo) {
		return invalid("time window")
	}
	return nil
}

// slots returns the scheduled moments from from to to, oldest first.
func (j Job) slots(from, to time.Time) []time.Time {
	var slots []time.Time
	first := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	times := j.times()
	for day := first; !day.After(to); day = day.AddDate(0, 0, 1) {
		var moments [][2]int
		switch j.Every {
		case "hourly":
			_, minute := clock(times[0])
			for hour := 0; hour < 24; hour += j.Interval {
				moments = append(moments, [2]int{hour, minute})
			}
		case "daily", "weekly", "monthly":
			switch {
			case j.Every == "weekly" && !slices.Contains(j.weekdays(), int(day.Weekday())):
			case j.Every == "monthly" && day.Day() != j.MonthDay:
			default:
				for _, text := range times {
					hour, minute := clock(text)
					moments = append(moments, [2]int{hour, minute})
				}
			}
		}
		for _, moment := range moments {
			at := time.Date(day.Year(), day.Month(), day.Day(), moment[0], moment[1], 0, 0, day.Location())
			if !at.Before(from) && !at.After(to) {
				slots = append(slots, at)
			}
		}
	}
	return slots
}

// lastSlot returns the latest scheduled moment that has passed.
func (j Job) lastSlot(now time.Time) (time.Time, bool) {
	slots := j.slots(now.Add(-lookBack), now)
	if len(slots) == 0 {
		return time.Time{}, false
	}
	return slots[len(slots)-1], true
}

// inWindow reports whether a run may start at this moment.
func (j Job) inWindow(at time.Time) bool {
	if j.WindowFrom == "" {
		return true
	}
	fromHour, fromMinute := clock(j.WindowFrom)
	toHour, toMinute := clock(j.WindowTo)
	from, to, now := fromHour*60+fromMinute, toHour*60+toMinute, at.Hour()*60+at.Minute()
	if from < to {
		return now >= from && now < to
	}
	return now >= from || now < to
}

// NextRun returns when the job runs next, if it is on.
func (j Job) NextRun(now time.Time) (time.Time, bool) {
	if !j.Enabled {
		return time.Time{}, false
	}
	for _, slot := range j.slots(now, now.Add(lookAhead)) {
		if slot.Equal(now) {
			continue
		}
		// Outside the window the run waits for it to open.
		for step := 0; !j.inWindow(slot) && step < 24*60+1; step++ {
			slot = slot.Add(time.Minute)
		}
		return slot, true
	}
	return time.Time{}, false
}

// due reports whether a job should run now: a scheduled moment has passed
// without a run since, or the last attempt failed a while ago.
func due(job Job, state State, now time.Time) bool {
	if !job.Enabled || state.Running || !job.inWindow(now) {
		return false
	}
	slot, ok := job.lastSlot(now)
	if !ok {
		return false
	}
	switch {
	case state.LastRun == nil:
		return true
	case state.LastRun.Before(slot):
		return true
	}
	// A target that was not there is tried again until it works.
	return state.LastError != "" && now.Sub(*state.LastRun) >= retryAfter && (state.LastOK == nil || state.LastOK.Before(slot))
}

// Overdue reports whether a job has gone too long without a backup that
// worked; see overdue.
func (m *Manager) Overdue(view View) bool {
	return overdue(view.Job, view.State, m.now())
}

// overdue reports whether a job has gone without a backup that worked for at
// least two of its scheduled moments and at least overdueAfter. A job that has
// never been tried is not overdue: it starts right away.
func overdue(job Job, state State, now time.Time) bool {
	since := state.LastOK
	if since == nil {
		since = state.LastRun
	}
	if !job.Enabled || state.Running || since == nil || now.Sub(*since) < overdueAfter {
		return false
	}
	// Every schedule has a moment within lookBack; further back there are
	// more than two anyway.
	from := since.Add(time.Second)
	if from.Before(now.Add(-2 * lookBack)) {
		return true
	}
	return len(job.slots(from, now)) >= 2
}

// DueSoon lets maintenance yield before a scheduled backup without interrupting
// already running work. It only inspects in-memory settings and states.
func (m *Manager) DueSoon(now time.Time, ahead time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, job := range m.jobs {
		if !job.Enabled {
			continue
		}
		state := State{}
		if st := m.states[job.ID]; st != nil {
			state = *st
		}
		if due(job, state, now) {
			return true
		}
		if next, ok := job.NextRun(now); ok && !next.After(now.Add(ahead)) {
			return true
		}
	}
	return false
}
