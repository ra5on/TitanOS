package retention

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// runs makes two runs a day, at 03:00 and 15:00, for days days up to now.
func runs(now time.Time, days int, skip func(time.Time) bool) []Version {
	var out []Version
	for d := days; d >= 0; d-- {
		for _, h := range []int{3, 15} {
			t := time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.Local).AddDate(0, 0, -d)
			if t.After(now) || (skip != nil && skip(t)) {
				continue
			}
			out = append(out, Version{ID: t.Format("2006-01-02T15"), Time: t})
		}
	}
	return out
}

func kept(k Keep, versions []Version, now time.Time) []string {
	var ids []string
	for id := range Select(k, versions, now) {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func TestKeepChosenRuns(t *testing.T) {
	now := time.Date(2026, 10, 31, 16, 0, 0, 0, time.Local) // a Saturday
	all := runs(now, 200, nil)
	k := Keep{Daily: 3, DailyAt: "03:00", Weekly: 2, WeeklyDay: 7, WeeklyAt: "03:00", Monthly: 3, MonthlyNth: 1, MonthlyWeekday: 1, MonthlyAt: "03:00"}
	got := kept(k, all, now)
	want := []string{
		"2026-08-03T03", "2026-09-07T03", "2026-10-05T03", // first Mondays
		"2026-10-25T03",                                   // Sunday of the last full week
		"2026-10-29T03", "2026-10-30T03", "2026-10-31T03", // the last three days
		"2026-10-31T15", // the newest always stays
	}
	// The running week has no Sunday yet: its newest run stands in.
	if !slices.Equal(got, want) {
		t.Fatalf("kept\n%v\nwant\n%v", got, want)
	}

	// A missing chosen run: the next one after it in the same period.
	missing := runs(now, 200, func(t time.Time) bool { return t.Format("2006-01-02T15") == "2026-10-05T03" })
	if got := kept(k, missing, now); !slices.Contains(got, "2026-10-05T15") || slices.Contains(got, "2026-10-05T03") {
		t.Fatalf("missed run: %v", got)
	}

	// Fixed day and the last day of the month.
	k = Keep{Monthly: 2, MonthlyDay: 1, MonthlyAt: "15:00"}
	if got := kept(k, all, now); fmt.Sprint(got) != "[2026-09-01T15 2026-10-01T15 2026-10-31T15]" {
		t.Fatalf("day 1: %v", got)
	}
	k = Keep{Monthly: 2, MonthlyDay: -1}
	if got := kept(k, all, now); fmt.Sprint(got) != "[2026-09-30T03 2026-10-31T03 2026-10-31T15]" {
		t.Fatalf("last day: %v", got)
	}
	k = Keep{Monthly: 1, MonthlyNth: -1, MonthlyWeekday: 5}
	if got := kept(k, all, now); fmt.Sprint(got) != "[2026-10-30T03 2026-10-31T15]" {
		t.Fatalf("last Friday: %v", got)
	}
	if d := NthWeekday(time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local), -1, 5); d.Day() != 30 {
		t.Fatalf("last Friday of October: %v", d)
	}
}

func TestKeepWithoutChoiceMatchesNewestPerPeriod(t *testing.T) {
	now := time.Date(2026, 10, 31, 16, 0, 0, 0, time.Local)
	got := kept(Keep{Daily: 2}, runs(now, 10, nil), now)
	if fmt.Sprint(got) != "[2026-10-30T15 2026-10-31T15]" {
		t.Fatalf("%v", got)
	}
	if len(kept(Keep{}, runs(now, 10, nil), now)) != 22 {
		t.Fatal("no counts must keep all")
	}
}

func TestRollingKeepsAllOrByAge(t *testing.T) {
	now := time.Date(2026, 10, 31, 16, 0, 0, 0, time.Local)
	all := runs(now, 100, nil)
	if len(kept(Keep{Mode: "rolling"}, all, now)) != len(all) {
		t.Fatal("rolling dropped versions")
	}
	got := kept(Keep{Mode: "rolling", MaxAgeMonths: 1}, all, now)
	if got[0] != "2026-10-02T03" { // one month back from 31 October is 1 October 16:00
		t.Fatalf("oldest kept %s", got[0])
	}
	old := []Version{{"a", now.AddDate(-2, 0, 0)}}
	if len(kept(Keep{Mode: "rolling", MaxAgeMonths: 1}, old, now)) != 1 {
		t.Fatal("the newest must stay even when old")
	}
	if o := OldestFirst(all); o[0].ID != all[0].ID || len(o) != len(all)-1 {
		t.Fatal("oldest first")
	}
}

func TestKeepLastN(t *testing.T) {
	now := time.Date(2026, 10, 31, 16, 0, 0, 0, time.Local)
	all := runs(now, 10, nil)
	k := Keep{Last: 5, Daily: 7, Weekly: 4}
	if !k.Chosen() {
		t.Fatal("last must be picked by Select")
	}
	got := kept(k, all, now)
	want := []string{"2026-10-29T03", "2026-10-29T15", "2026-10-30T03", "2026-10-30T15", "2026-10-31T03", "2026-10-31T15"}[1:]
	if !slices.Equal(got, want) {
		t.Fatalf("kept %v, want %v", got, want)
	}
	if len(kept(Keep{Last: 50}, all, now)) != len(all) {
		t.Fatal("fewer versions than last must all stay")
	}
	if len(kept(Keep{Last: 2, Mode: "rolling"}, all, now)) != len(all) {
		t.Fatal("rolling keeps all")
	}
}

func TestCheckKeep(t *testing.T) {
	for _, bad := range []Keep{{Last: -1}, {Last: 10000}, {Mode: "x"}, {WhenFull: "newest"}, {DailyAt: "3:00"}, {WeeklyDay: 8}, {MonthlyDay: 29}, {MonthlyNth: 1}, {MonthlyDay: 1, MonthlyNth: 1, MonthlyWeekday: 1}} {
		if Check(bad) == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if err := Check(Keep{Daily: 7, DailyAt: "03:00", MonthlyNth: -1, MonthlyWeekday: 7, WhenFull: "oldest"}); err != nil {
		t.Error(err)
	}
}
