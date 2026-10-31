package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/usb"
)

// runs makes two runs a day, at 03:00 and 15:00, for days days up to now.
func runs(now time.Time, days int, skip func(time.Time) bool) []version {
	var out []version
	for d := days; d >= 0; d-- {
		for _, h := range []int{3, 15} {
			t := time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.Local).AddDate(0, 0, -d)
			if t.After(now) || (skip != nil && skip(t)) {
				continue
			}
			out = append(out, version{ID: t.Format("2006-01-02T15"), Time: t})
		}
	}
	return out
}

func kept(k Keep, versions []version, now time.Time) []string {
	var ids []string
	for id := range keepVersions(k, versions, now) {
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
	if d := nthWeekday(time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local), -1, 5); d.Day() != 30 {
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
	old := []version{{"a", now.AddDate(-2, 0, 0)}}
	if len(kept(Keep{Mode: "rolling", MaxAgeMonths: 1}, old, now)) != 1 {
		t.Fatal("the newest must stay even when old")
	}
	if o := oldestFirst(all); o[0].ID != all[0].ID || len(o) != len(all)-1 {
		t.Fatal("oldest first")
	}
}

func TestCheckKeep(t *testing.T) {
	for _, bad := range []Keep{{Mode: "x"}, {WhenFull: "newest"}, {DailyAt: "3:00"}, {WeeklyDay: 8}, {MonthlyDay: 29}, {MonthlyNth: 1}, {MonthlyDay: 1, MonthlyNth: 1, MonthlyWeekday: 1}} {
		if checkKeep(bad) == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if err := checkKeep(Keep{Daily: 7, DailyAt: "03:00", MonthlyNth: -1, MonthlyWeekday: 7, WhenFull: "oldest"}); err != nil {
		t.Error(err)
	}
}

func TestFullTargetDropsOldestOnlyWhenAllowed(t *testing.T) {
	e := newEnv(t)
	job := s3Job()
	if err := e.m.Create(job, volumes); err != nil {
		t.Fatal(err)
	}
	write := func(lines string) { os.WriteFile(filepath.Join(e.dir, "versions"), []byte(lines), 0o644) }
	versions := "v1 2026-10-01T03:00:00Z\nv2 2026-10-02T03:00:00Z\nv3 2026-10-03T03:00:00Z\nv4 2026-10-04T03:00:00Z\n"
	write(versions)
	os.WriteFile(filepath.Join(e.dir, "full"), []byte("1\n"), 0o644)

	// Default: stop and report, nothing removed.
	e.m.Start("cloud")
	if got := e.result(t); !strings.Contains(got, "not enough space") {
		t.Fatalf("full target: %s", got)
	}
	if code := e.state("cloud").LastErrorCode; code != "backup_no_space" {
		t.Fatalf("code %q", code)
	}
	if data, _ := os.ReadFile(filepath.Join(e.dir, "versions")); string(data) != versions {
		t.Fatalf("removed without leave: %s", data)
	}

	// With leave: the oldest go, one by one, until the run fits.
	job.Keep = Keep{Daily: 7, WhenFull: "oldest"}
	if err := e.m.Update("cloud", job, volumes); err != nil {
		t.Fatal(err)
	}
	freed := 0
	e.m.OnFreed = func(_ string, n int) { freed = n }
	e.m.Start("cloud")
	if got := e.result(t); got != "cloud: ok" {
		t.Fatalf("with leave: %s", got)
	}
	if data, _ := os.ReadFile(filepath.Join(e.dir, "versions")); string(data) != "v4 2026-10-04T03:00:00Z\n" || freed != 3 {
		t.Fatalf("left %q, freed %d", data, freed)
	}
	if w := e.state("cloud").Warning; !strings.Contains(w, "removed the oldest 3") {
		t.Fatalf("warning %q", w)
	}
	// The newest is never removed: a target too small for even one more
	// run fails.
	os.WriteFile(filepath.Join(e.dir, "full"), []byte("0\n"), 0o644)
	e.m.Start("cloud")
	if got := e.result(t); !strings.Contains(got, "not enough space") {
		t.Fatalf("too small: %s", got)
	}
	if data, _ := os.ReadFile(filepath.Join(e.dir, "versions")); string(data) != "v4 2026-10-04T03:00:00Z\n" {
		t.Fatalf("newest removed: %q", data)
	}
}

func TestChosenVersionsAreForgottenByID(t *testing.T) {
	e := newEnv(t)
	job := s3Job()
	job.Keep = Keep{Daily: 1, DailyAt: "03:00"}
	if err := e.m.Create(job, volumes); err != nil {
		t.Fatal(err)
	}
	// Days and times are those of the NAS time zone.
	e.m.Timezone = func() string { return "Europe/Berlin" }
	berlin, _ := time.LoadLocation("Europe/Berlin")
	local := func(day, hour int) string {
		return time.Date(2026, 10, day, hour, 0, 0, 0, berlin).UTC().Format(time.RFC3339)
	}
	os.WriteFile(filepath.Join(e.dir, "versions"), []byte(fmt.Sprintf("a %s\nb %s\nc %s\n", local(13, 3), local(14, 3), local(14, 11))), 0o644)
	e.m.Start("cloud")
	if got := e.result(t); got != "cloud: ok" {
		t.Fatal(got)
	}
	if !strings.Contains(e.calls(), "forget --prune a |") {
		t.Fatalf("calls: %s", e.calls())
	}
	if strings.Contains(e.calls(), "--keep-daily") {
		t.Fatal("restic's own keep rules used as well")
	}
}

func TestEncryptedVersionsOnUSBHoldTheDrive(t *testing.T) {
	e := newEnv(t)
	drive := t.TempDir()
	holding := false
	e.m.WithUSB = func(ctx context.Context, id string, work func(*os.Root) error) error {
		if id != "ABCD-1234" {
			return errors.New("wrong drive")
		}
		root, err := os.OpenRoot(drive)
		if err != nil {
			return err
		}
		defer root.Close()
		holding = true
		defer func() { holding = false }()
		return work(root)
	}
	job := Job{ID: "usb", Sources: []string{"media"}, Enabled: true, Every: "daily", Time: "02:00", Destination: Destination{Type: "usb", DeviceID: "ABCD-1234", DeviceName: "Backup-HDD", Folder: "titanos"}}
	if err := e.m.Create(job, volumes); err != nil {
		t.Fatal(err)
	}
	e.m.Start("usb")
	if got := e.result(t); got != "usb: ok" {
		t.Fatal(got)
	}
	if !strings.Contains(e.calls(), "repo="+filepath.Join(drive, "titanos")+" ") {
		t.Fatalf("calls: %s", e.calls())
	}
	if holding {
		t.Fatal("drive still held after the run")
	}
	// Listing versions holds it only while it looks.
	if _, err := e.m.Snapshots(context.Background(), "usb"); err != nil || holding {
		t.Fatalf("snapshots: %v, held %v", err, holding)
	}
	e.m.WithUSB = func(context.Context, string, func(*os.Root) error) error { return usb.ErrNotFound }
	if _, err := e.m.Snapshots(context.Background(), "usb"); !errors.Is(err, usb.ErrNotFound) {
		t.Fatalf("missing drive: %v", err)
	}
}

func TestReadableCopyToAnotherVolume(t *testing.T) {
	e := newEnv(t)
	os.WriteFile(filepath.Join(e.m.mountRoot, "tank/media/a.txt"), []byte("a"), 0o600)
	mounts := filepath.Join(t.TempDir(), "mounts")
	os.WriteFile(mounts, []byte("/dev/md1 "+filepath.Join(e.m.mountRoot, "cold")+" ext4 rw 0 0\n"), 0o644)
	e.m.mountsFile = mounts
	job := Job{ID: "kopie", Sources: []string{"media"}, Every: "daily", Time: "02:00", Destination: Destination{Type: "local-copy", Volume: "cold", Folder: "kopie"}}
	if err := e.m.Create(job, volumes); err != nil {
		t.Fatal(err)
	}
	e.m.Start("kopie")
	if got := e.result(t); got != "kopie: ok" {
		t.Fatal(got)
	}
	if data, err := os.ReadFile(filepath.Join(e.m.mountRoot, "cold/kopie/media/a.txt")); err != nil || string(data) != "a" {
		t.Fatalf("copy: %q %v", data, err)
	}
	job.Destination.Volume = "tank"
	job.ID = "same"
	if err := e.m.Create(job, volumes); err == nil {
		t.Fatal("copy onto the volume of its shares accepted")
	}
}
