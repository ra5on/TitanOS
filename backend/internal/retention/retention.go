package retention

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// Which versions stay (decided 31.10.2026). By the counts, each of the
// last Daily days, Weekly weeks and Monthly months that have versions keeps
// one: the run chosen for it (a time, a weekday, a day of the month or
// the first Monday …), or when that run is missing the next one after it
// in the same period, or, if there is none, the newest of the period. The
// newest version always stays. Days, weeks (Monday to Sunday) and months
// are those of the NAS time zone.

// Version is one stored backup, snapshot or copy as far as keeping it is
// concerned.
type Version struct {
	ID   string
	Time time.Time // local time of the NAS
}

// ErrInvalid means a Keep does not make sense.
var ErrInvalid = errors.New("retention: invalid")

func invalid(what string) error { return fmt.Errorf("%w: %s", ErrInvalid, what) }

// Check tells whether a Keep makes sense.
func Check(k Keep) error {
	if k.Mode != "" && k.Mode != "rolling" {
		return invalid("how versions are kept")
	}
	if k.Last < 0 || k.Last > 9999 {
		return invalid("how many versions to keep")
	}
	if k.WhenFull != "" && k.WhenFull != "oldest" {
		return invalid("what happens when the target is full")
	}
	for _, at := range []string{k.DailyAt, k.WeeklyAt, k.MonthlyAt} {
		if at != "" && !validClock(at) {
			return invalid("time of the version to keep")
		}
	}
	switch {
	case k.WeeklyDay < 0 || k.WeeklyDay > 7,
		k.MonthlyDay < -1 || k.MonthlyDay > 28,
		k.MonthlyNth < -1 || k.MonthlyNth > 4,
		k.MonthlyWeekday < 0 || k.MonthlyWeekday > 7,
		(k.MonthlyNth == 0) != (k.MonthlyWeekday == 0),
		k.MonthlyDay != 0 && k.MonthlyNth != 0,
		k.MaxAgeMonths < 0 || k.MaxAgeMonths > 1200:
		return invalid("which version to keep")
	}
	return nil
}

func validClock(at string) bool {
	_, err := time.Parse("15:04", at)
	return err == nil && len(at) == 5
}

// Chosen reports whether a job picks its versions itself instead of
// leaving that to restic's --keep-* flags.
func (k Keep) Chosen() bool {
	return k.Mode == "rolling" || k.Last > 0 || k.DailyAt != "" || k.WeeklyAt != "" || k.WeeklyDay != 0 ||
		k.MonthlyAt != "" || k.MonthlyDay != 0 || k.MonthlyNth != 0
}

// Select returns the IDs of the versions that stay, given all of them;
// now is the local time of the NAS.
func Select(k Keep, versions []Version, now time.Time) map[string]bool {
	keep := map[string]bool{}
	if len(versions) == 0 {
		return keep
	}
	sorted := slices.Clone(versions)
	slices.SortFunc(sorted, func(a, b Version) int { return a.Time.Compare(b.Time) })
	keep[sorted[len(sorted)-1].ID] = true
	if k.Mode == "rolling" {
		for _, v := range sorted {
			if k.MaxAgeMonths == 0 || !v.Time.Before(now.AddDate(0, -k.MaxAgeMonths, 0)) {
				keep[v.ID] = true
			}
		}
		return keep
	}
	if k.Last > 0 {
		for _, v := range sorted[max(0, len(sorted)-k.Last):] {
			keep[v.ID] = true
		}
		return keep
	}
	if k.Daily+k.Weekly+k.Monthly == 0 {
		for _, v := range sorted {
			keep[v.ID] = true
		}
		return keep
	}
	tier := func(count int, period func(time.Time) (time.Time, time.Time), target func(start time.Time) (time.Time, bool)) {
		if count <= 0 {
			return
		}
		// Periods from the newest back, each with its versions.
		var periods [][]Version
		var current time.Time
		for i := len(sorted) - 1; i >= 0; i-- {
			start, _ := period(sorted[i].Time)
			if len(periods) == 0 || !start.Equal(current) {
				if len(periods) == count {
					break
				}
				periods = append(periods, nil)
				current = start
			}
			periods[len(periods)-1] = append(periods[len(periods)-1], sorted[i])
		}
		for _, in := range periods {
			// in runs newest first.
			pick := in[0]
			start, _ := period(in[0].Time)
			if at, ok := target(start); ok {
				for j := len(in) - 1; j >= 0; j-- {
					if !in[j].Time.Before(at) {
						pick = in[j]
						break
					}
				}
			}
			keep[pick.ID] = true
		}
	}
	day := func(t time.Time) (time.Time, time.Time) {
		start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
		return start, start.AddDate(0, 0, 1)
	}
	week := func(t time.Time) (time.Time, time.Time) {
		d, _ := day(t)
		start := d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
		return start, start.AddDate(0, 0, 7)
	}
	month := func(t time.Time) (time.Time, time.Time) {
		start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
		return start, start.AddDate(0, 1, 0)
	}
	clock := func(start time.Time, at string) time.Time {
		c, _ := time.Parse("15:04", at)
		return time.Date(start.Year(), start.Month(), start.Day(), c.Hour(), c.Minute(), 0, 0, start.Location())
	}
	tier(k.Daily, day, func(start time.Time) (time.Time, bool) {
		if k.DailyAt == "" {
			return time.Time{}, false
		}
		return clock(start, k.DailyAt), true
	})
	tier(k.Weekly, week, func(start time.Time) (time.Time, bool) {
		if k.WeeklyDay == 0 && k.WeeklyAt == "" {
			return time.Time{}, false
		}
		d := start
		if k.WeeklyDay != 0 {
			d = start.AddDate(0, 0, k.WeeklyDay-1)
		}
		if k.WeeklyAt == "" {
			return d, true
		}
		return clock(d, k.WeeklyAt), true
	})
	tier(k.Monthly, month, func(start time.Time) (time.Time, bool) {
		d := start
		switch {
		case k.MonthlyDay == -1:
			d = start.AddDate(0, 1, -1)
		case k.MonthlyDay > 0:
			d = start.AddDate(0, 0, k.MonthlyDay-1)
		case k.MonthlyNth != 0:
			d = NthWeekday(start, k.MonthlyNth, k.MonthlyWeekday)
		case k.MonthlyAt == "":
			return time.Time{}, false
		}
		if k.MonthlyAt == "" {
			return d, true
		}
		return clock(d, k.MonthlyAt), true
	})
	return keep
}

// NthWeekday is the nth (1 … 4, -1 = last) weekday (1 = Monday … 7 =
// Sunday) of the month that starts at start.
func NthWeekday(start time.Time, nth, weekday int) time.Time {
	want := time.Weekday(weekday % 7)
	if nth == -1 {
		d := start.AddDate(0, 1, -1)
		for d.Weekday() != want {
			d = d.AddDate(0, 0, -1)
		}
		return d
	}
	d := start
	for d.Weekday() != want {
		d = d.AddDate(0, 0, 1)
	}
	return d.AddDate(0, 0, 7*(nth-1))
}

// OldestFirst returns the versions that may go when space runs out: all
// but the newest, oldest first.
func OldestFirst(versions []Version) []Version {
	sorted := slices.Clone(versions)
	slices.SortFunc(sorted, func(a, b Version) int { return a.Time.Compare(b.Time) })
	if len(sorted) > 0 {
		sorted = sorted[:len(sorted)-1]
	}
	return sorted
}

// Keep says how many backups stay: the newest of each of that many days,
// weeks and months. All zero keeps everything.
//
// Since 0.41.0 (decided 31.10.2026) the optional fields below pick which
// run of a day, week or month stays, and Mode "rolling" keeps every run
// until the target is full. Older versions read only the three counts;
// a rolling job leaves them at zero, which there also keeps everything.

type Keep struct {
	// Last keeps the newest that many versions (0.42.0, decided
	// 31.10.2026: "Aufbewahren" is one number). It wins over the counts
	// below; older versions do not know it and keep everything.
	Last    int `json:"last,omitempty"`
	Daily   int `json:"daily"`
	Weekly  int `json:"weekly"`
	Monthly int `json:"monthly"`
	// Mode is "" (by the counts above) or "rolling".
	Mode string `json:"mode,omitempty"`
	// DailyAt, WeeklyAt and MonthlyAt are the run (HH:MM) that stays for
	// the day, week or month; empty keeps the newest, as before.
	DailyAt  string `json:"dailyAt,omitempty"`
	WeeklyAt string `json:"weeklyAt,omitempty"`
	// WeeklyDay is the weekday, 1 = Monday … 7 = Sunday; 0 = newest.
	WeeklyDay int    `json:"weeklyDay,omitempty"`
	MonthlyAt string `json:"monthlyAt,omitempty"`
	// MonthlyDay is the day of the month, 1 … 28 or -1 for the last one.
	// Instead, MonthlyNth (1 … 4, -1 = last) with MonthlyWeekday (1 … 7)
	// names a weekday such as the first Monday. Neither set: newest.
	MonthlyDay     int `json:"monthlyDay,omitempty"`
	MonthlyNth     int `json:"monthlyNth,omitempty"`
	MonthlyWeekday int `json:"monthlyWeekday,omitempty"`
	// WhenFull is "" (stop and report) or "oldest": when the target is
	// full, the oldest versions go, as few as needed, never the newest.
	// A rolling job always works like "oldest".
	WhenFull string `json:"whenFull,omitempty"`
	// MaxAgeMonths, for rolling jobs, also removes versions older than
	// that; 0 keeps them until space runs out.
	MaxAgeMonths int `json:"maxAgeMonths,omitempty"`
}

// DropsOldest reports whether old versions may go when the target is full.
func (k Keep) DropsOldest() bool { return k.Mode == "rolling" || k.WhenFull == "oldest" }
