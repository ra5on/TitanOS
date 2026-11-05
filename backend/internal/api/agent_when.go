package api

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/scripts"
)

// When planned tasks of the KI-Agent run (PLAN 49): once, every few
// minutes, hours or days, on chosen weekdays, weekly, monthly or by a cron
// line. A run that was missed (NAS off) runs once after the start: a
// recurring task within 12 hours, a one-off task within 7 days.

const (
	agentCatchUp     = 12 * time.Hour
	agentCatchUpOnce = 7 * 24 * time.Hour
	agentMinMinutes  = 5
)

// agentNext returns the first time after `after` a schedule is due, in loc.
func agentNext(sc agent.Schedule, after time.Time, loc *time.Location) (time.Time, bool) {
	after = after.In(loc)
	hh, mm := 0, 0
	clockOK := false
	if _, err := fmt.Sscanf(sc.At, "%d:%d", &hh, &mm); err == nil && clock.MatchString(sc.At) {
		clockOK = true
	}
	// The first day from after on whose time At lies after `after` and that
	// passes the filter.
	byDay := func(ok func(day time.Time) bool) (time.Time, bool) {
		if !clockOK {
			return time.Time{}, false
		}
		day := time.Date(after.Year(), after.Month(), after.Day(), 0, 0, 0, 0, loc)
		for range 400 {
			at := time.Date(day.Year(), day.Month(), day.Day(), hh, mm, 0, 0, loc)
			if at.After(after) && ok(day) {
				return at, true
			}
			day = day.AddDate(0, 0, 1)
		}
		return time.Time{}, false
	}
	switch sc.Every {
	case "once":
		if !sc.LastRun.IsZero() || !clockOK {
			return time.Time{}, false
		}
		d, err := time.ParseInLocation("2006-01-02", sc.Date, loc)
		if err != nil {
			return time.Time{}, false
		}
		return time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, loc), true
	case "hourly":
		return after.Truncate(time.Hour).Add(time.Hour), true
	case "minutes":
		return after.Truncate(time.Minute).Add(time.Duration(max(sc.Interval, agentMinMinutes)) * time.Minute), true
	case "hours":
		return after.Truncate(time.Minute).Add(time.Duration(max(sc.Interval, 1)) * time.Hour), true
	case "daily":
		return byDay(func(time.Time) bool { return true })
	case "days":
		n := max(sc.Interval, 1)
		start := sc.Created.In(loc)
		anchor := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
		return byDay(func(day time.Time) bool {
			days := int(day.Sub(anchor).Hours()/24 + 0.5)
			return days >= 0 && days%n == 0
		})
	case "weekdays":
		return byDay(func(day time.Time) bool { return slices.Contains(sc.Weekdays, int(day.Weekday())) })
	case "weekly":
		return byDay(func(day time.Time) bool { return int(day.Weekday()) == sc.Weekday })
	case "monthly":
		return byDay(func(day time.Time) bool { return day.Day() == sc.Day })
	case "cron":
		next, err := scripts.NextCron(sc.Cron, after)
		return next, err == nil
	}
	return time.Time{}, false
}

// agentDue reports whether a schedule should run at now, and whether that
// run is a missed one caught up. It runs once per due time.
func agentDue(sc agent.Schedule, now time.Time) bool {
	due, _ := agentDueAt(sc, now)
	return due
}

func agentDueAt(sc agent.Schedule, now time.Time) (bool, bool) {
	if !sc.Enabled || sc.Trigger != nil {
		return false, false
	}
	loc := now.Location()
	base := sc.LastRun
	if base.IsZero() {
		base = sc.Created
	}
	if base.IsZero() {
		// Unknown history (files of older versions): only the current slot.
		base = now.Add(-10 * time.Minute)
	}
	catch := agentCatchUp
	if sc.Every == "once" {
		catch = agentCatchUpOnce
	} else if base.Before(now.Add(-catch)) {
		base = now.Add(-catch)
	}
	next, ok := agentNext(sc, base, loc)
	if !ok || next.After(now) || next.Before(now.Add(-catch)) {
		return false, false
	}
	return true, now.Sub(next) > 10*time.Minute
}

// agentWhen describes a schedule in words for the confirmation in the chat
// and the list (German, like the rest of the agent's notes).
func agentWhen(sc agent.Schedule) string {
	days := []string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"}
	switch sc.Every {
	case "once":
		if d, err := time.Parse("2006-01-02", sc.Date); err == nil {
			return fmt.Sprintf("am %s um %s", d.Format("02.01.2006"), sc.At)
		}
	case "minutes":
		return fmt.Sprintf("alle %d Minuten", max(sc.Interval, agentMinMinutes))
	case "hours":
		if sc.Interval <= 1 {
			return "jede Stunde"
		}
		return fmt.Sprintf("alle %d Stunden", sc.Interval)
	case "hourly":
		return "jede volle Stunde"
	case "daily":
		return "täglich um " + sc.At
	case "days":
		if sc.Interval <= 1 {
			return "täglich um " + sc.At
		}
		return fmt.Sprintf("alle %d Tage um %s", sc.Interval, sc.At)
	case "weekdays":
		var names []string
		for _, d := range sc.Weekdays {
			if d >= 0 && d <= 6 {
				names = append(names, days[d])
			}
		}
		return strings.Join(names, ", ") + " um " + sc.At
	case "weekly":
		if sc.Weekday >= 0 && sc.Weekday <= 6 {
			return "jeden " + days[sc.Weekday] + " um " + sc.At
		}
	case "monthly":
		return fmt.Sprintf("monatlich am %d. um %s", sc.Day, sc.At)
	case "cron":
		return "nach Cron „" + sc.Cron + "“"
	case "trigger":
		return "bei einem Zustand in Home Assistant"
	}
	return sc.Every
}

// agentScheduleView is a planned task with its next run and its words, for
// the overview and the agent's own list.
type agentScheduleView struct {
	agent.Schedule
	Next string `json:"next,omitempty"`
	When string `json:"when"`
}

func (s *Server) agentScheduleViews(list []agent.Schedule) []agentScheduleView {
	loc := s.agentLocation()
	now := time.Now().In(loc)
	out := make([]agentScheduleView, 0, len(list))
	for _, sc := range list {
		v := agentScheduleView{Schedule: sc, When: agentWhen(sc)}
		if sc.Enabled && sc.Trigger == nil {
			base := now
			if sc.Every == "once" {
				base = sc.Created
			}
			if next, ok := agentNext(sc, base, loc); ok && (sc.Every != "once" || next.After(now.Add(-agentCatchUpOnce))) {
				v.Next = next.Format(time.RFC3339)
			}
		}
		out = append(out, v)
	}
	return out
}
