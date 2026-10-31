package backup

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

// Which versions stay (decided 31.10.2026). By the counts, each of the
// last Daily days, Weekly weeks and Monthly months that have versions keeps
// one: the run chosen for it (a time, a weekday, a day of the month or
// the first Monday …), or when that run is missing the next one after it
// in the same period, or, if there is none, the newest of the period. The
// newest version always stays. Days, weeks (Monday to Sunday) and months
// are those of the NAS time zone.

// version is one stored backup as far as keeping it is concerned.
type version struct {
	ID   string
	Time time.Time // local time of the NAS
}

func checkKeep(k Keep) error {
	if k.Mode != "" && k.Mode != "rolling" {
		return invalid("how versions are kept")
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

// chosen reports whether a job picks its versions itself instead of
// leaving that to restic's --keep-* flags.
func (k Keep) chosen() bool {
	return k.Mode == "rolling" || k.DailyAt != "" || k.WeeklyAt != "" || k.WeeklyDay != 0 ||
		k.MonthlyAt != "" || k.MonthlyDay != 0 || k.MonthlyNth != 0
}

// keepVersions returns the IDs of the versions that stay, given all of
// them; now is the local time of the NAS.
func keepVersions(k Keep, versions []version, now time.Time) map[string]bool {
	keep := map[string]bool{}
	if len(versions) == 0 {
		return keep
	}
	sorted := slices.Clone(versions)
	slices.SortFunc(sorted, func(a, b version) int { return a.Time.Compare(b.Time) })
	keep[sorted[len(sorted)-1].ID] = true
	if k.Mode == "rolling" {
		for _, v := range sorted {
			if k.MaxAgeMonths == 0 || !v.Time.Before(now.AddDate(0, -k.MaxAgeMonths, 0)) {
				keep[v.ID] = true
			}
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
		var periods [][]version
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
			d = nthWeekday(start, k.MonthlyNth, k.MonthlyWeekday)
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

// nthWeekday is the nth (1 … 4, -1 = last) weekday (1 = Monday … 7 =
// Sunday) of the month that starts at start.
func nthWeekday(start time.Time, nth, weekday int) time.Time {
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

// oldestFirst returns the versions that may go when space runs out: all
// but the newest, oldest first.
func oldestFirst(versions []version) []version {
	sorted := slices.Clone(versions)
	slices.SortFunc(sorted, func(a, b version) int { return a.Time.Compare(b.Time) })
	if len(sorted) > 0 {
		sorted = sorted[:len(sorted)-1]
	}
	return sorted
}

// dropsOldest reports whether old versions may go when the target is full.
func (k Keep) dropsOldest() bool { return k.Mode == "rolling" || k.WhenFull == "oldest" }

// noSpace recognises what restic and its backends say when the target is
// full or over its quota.
func noSpace(out string) bool {
	lower := strings.ToLower(out)
	for _, sign := range []string{"no space left on device", "disk quota exceeded", "quotaexceeded", "insufficient storage", "insufficientstorage", "507 insufficient", "not enough space"} {
		if strings.Contains(lower, sign) {
			return true
		}
	}
	return false
}

type resticVersion struct {
	ID   string    `json:"id"`
	Time time.Time `json:"time"`
	Tags []string  `json:"tags"`
}

// versions lists the versions of a job, by tag group as forget groups them.
func (m *Manager) versions(ctx context.Context, job Job, env, base []string) (map[string][]version, error) {
	var output strings.Builder
	out, err := m.run(ctx, env, append(slices.Clone(base), "snapshots", "--json", "--tag", "titanos"), func(line string) { output.WriteString(line) })
	if err != nil {
		return nil, errors.New(lastLines(out, job.Password))
	}
	var raw []resticVersion
	if err := json.Unmarshal([]byte(output.String()), &raw); err != nil {
		return nil, err
	}
	groups := map[string][]version{}
	for _, v := range raw {
		tags := slices.Clone(v.Tags)
		slices.Sort(tags)
		key := strings.Join(tags, ",")
		groups[key] = append(groups[key], version{ID: v.ID, Time: m.local(v.Time)})
	}
	return groups, nil
}

// forgetChosen removes the versions keepVersions does not keep.
func (m *Manager) forgetChosen(ctx context.Context, job Job, env, base []string) error {
	groups, err := m.versions(ctx, job, env, base)
	if err != nil {
		return err
	}
	now := m.local(m.now())
	var drop []string
	for _, group := range groups {
		keep := keepVersions(job.Keep, group, now)
		for _, v := range group {
			if !keep[v.ID] {
				drop = append(drop, v.ID)
			}
		}
	}
	if len(drop) == 0 {
		return nil
	}
	slices.Sort(drop)
	if out, err := m.run(ctx, env, append(append(slices.Clone(base), "forget", "--prune"), drop...), nil); err != nil {
		return errors.New(lastLines(out, job.Password))
	}
	return nil
}

// dropOldest removes the oldest version of the job (of any tag group), if
// more than one is left, and frees its space. It returns its time.
func (m *Manager) dropOldest(ctx context.Context, job Job, env, base []string) (string, error) {
	groups, err := m.versions(ctx, job, env, base)
	if err != nil {
		return "", err
	}
	var all []version
	for _, group := range groups {
		// The newest of every group stays.
		all = append(all, oldestFirst(group)...)
	}
	if len(all) == 0 {
		return "", nil
	}
	slices.SortFunc(all, func(a, b version) int { return a.Time.Compare(b.Time) })
	oldest := all[0]
	if out, err := m.run(ctx, env, append(slices.Clone(base), "forget", "--prune", oldest.ID), nil); err != nil {
		return "", errors.New(lastLines(out, job.Password))
	}
	return oldest.Time.Format("2006-01-02 15:04"), nil
}
