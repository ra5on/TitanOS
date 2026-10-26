package update

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// clock is a fake clock for the scheduled checks.
type clock struct{ at time.Time }

func (c *clock) now() time.Time                       { return c.at }
func (c *clock) set(start time.Time, d time.Duration) { c.at = start.Add(d) }

// scheduled returns a manager whose Run began at start, with the given
// interval saved, and the fake clock it reads.
func scheduled(t *testing.T, checkEvery int) (*env, *clock, time.Time) {
	t.Helper()
	e := newEnv(t)
	e.publish(t, "1.1.0", ChannelStable, nil)
	start := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	c := &clock{at: start}
	e.m.now = c.now
	e.m.mu.Lock()
	e.m.started = start
	e.m.mu.Unlock()
	if err := e.m.SetSettings(Settings{Channel: ChannelStable, InstallTime: "03:30", CheckEvery: checkEvery}); err != nil {
		t.Fatal(err)
	}
	return e, c, start
}

// breakRelease makes the project page answer with something unsigned.
func (e *env) breakRelease() {
	e.release.mu.Lock()
	e.release.raw, e.release.sig = []byte("{}"), "nothing"
	e.release.mu.Unlock()
}

func TestScheduledChecksFollowTheInterval(t *testing.T) {
	for _, tc := range []struct {
		name       string
		checkEvery int
		interval   time.Duration
	}{
		{"default", 0, 24 * time.Hour},
		{"6h", 6 * 60, 6 * time.Hour},
		{"12h", 12 * 60, 12 * time.Hour},
		{"daily", CheckDaily, 24 * time.Hour},
		{"weekly", 7 * 24 * 60, 7 * 24 * time.Hour},
		{"custom 3h", 3 * 60, 3 * time.Hour},
		{"custom 1h", MinCheckEvery, time.Hour},
		{"custom 30d", MaxCheckEvery, 30 * 24 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, c, start := scheduled(t, tc.checkEvery)
			ctx := context.Background()
			// Nothing before the network had a few minutes after the start.
			c.set(start, 4*time.Minute)
			if e.m.scheduledCheck(ctx) {
				t.Fatal("checked right after the start")
			}
			if next := e.m.Status().NextCheck; next == nil || !next.Equal(start.Add(checkStartDelay)) {
				t.Fatalf("next check before the first = %v", next)
			}
			c.set(start, checkStartDelay)
			if !e.m.scheduledCheck(ctx) {
				t.Fatal("first check not made")
			}
			first := c.at
			if status := e.m.Status(); status.Available == nil || status.NextCheck == nil || !status.NextCheck.Equal(first.Add(tc.interval)) {
				t.Fatalf("after the first check: available=%v next=%v", status.Available, status.NextCheck)
			}
			c.set(first, tc.interval-time.Minute)
			if e.m.scheduledCheck(ctx) {
				t.Fatal("checked before the interval was over")
			}
			c.set(first, tc.interval)
			if !e.m.scheduledCheck(ctx) {
				t.Fatal("second check not made once the interval was over")
			}
		})
	}
}

// A check by button or after a login counts: the schedule goes on from it.
func TestEveryCheckMovesTheSchedule(t *testing.T) {
	e, c, start := scheduled(t, 6*60)
	ctx := context.Background()
	c.set(start, time.Hour)
	if err := e.m.Check(ctx); err != nil {
		t.Fatal(err)
	}
	c.set(start, 6*time.Hour+59*time.Minute)
	if e.m.scheduledCheck(ctx) {
		t.Fatal("checked again within six hours of the button")
	}
	c.set(start, 7*time.Hour)
	if !e.m.scheduledCheck(ctx) {
		t.Fatal("no check six hours after the button")
	}
}

func TestFailedCheckIsRetriedAfterADayAtMost(t *testing.T) {
	ctx := context.Background()
	t.Run("weekly", func(t *testing.T) {
		e, c, start := scheduled(t, 7*24*60)
		e.breakRelease()
		c.set(start, checkStartDelay)
		if !e.m.scheduledCheck(ctx) || e.m.Status().CheckError == "" {
			t.Fatal("first check did not fail")
		}
		failed := c.at
		if next := e.m.Status().NextCheck; next == nil || !next.Equal(failed.Add(24*time.Hour)) {
			t.Fatalf("retry at %v", next)
		}
		c.set(failed, 24*time.Hour-time.Minute)
		if e.m.scheduledCheck(ctx) {
			t.Fatal("retried within a day")
		}
		c.set(failed, 24*time.Hour)
		if !e.m.scheduledCheck(ctx) {
			t.Fatal("not retried after a day")
		}
		// Back to the interval once a check succeeds.
		e.publish(t, "1.1.0", ChannelStable, nil)
		c.set(failed, 48*time.Hour)
		if !e.m.scheduledCheck(ctx) || e.m.Status().CheckError != "" {
			t.Fatal("second retry not made or failed")
		}
		if next := e.m.Status().NextCheck; next == nil || !next.Equal(c.at.Add(7*24*time.Hour)) {
			t.Fatalf("after success the next check is %v", next)
		}
	})
	// Shorter intervals keep their interval after a failure, as daily
	// checks always did: never more often than chosen.
	for _, every := range []int{0, 6 * 60} {
		e, c, start := scheduled(t, every)
		e.breakRelease()
		c.set(start, checkStartDelay)
		e.m.scheduledCheck(ctx)
		interval := Settings{CheckEvery: every}.CheckInterval()
		if next := e.m.Status().NextCheck; next == nil || !next.Equal(c.at.Add(interval)) {
			t.Fatalf("every %d: retry at %v", every, next)
		}
	}
}

func TestManualChecksOnly(t *testing.T) {
	e, c, start := scheduled(t, CheckManual)
	ctx := context.Background()
	for d := time.Duration(0); d <= 60*24*time.Hour; d += 6 * time.Hour {
		c.set(start, d)
		if e.m.scheduledCheck(ctx) {
			t.Fatalf("scheduled check after %v", d)
		}
	}
	if next := e.m.Status().NextCheck; next != nil {
		t.Fatalf("next check shown: %v", next)
	}
	if e.m.CheckAfterLogin() {
		t.Fatal("login started a check")
	}
	// The button still works.
	if err := e.m.Check(ctx); err != nil || e.m.Status().Available == nil {
		t.Fatalf("button check: %v", err)
	}
	if e.m.Status().NextCheck != nil {
		t.Fatal("a button check scheduled the next one")
	}
	// Installing by itself needs automatic checks.
	if err := e.m.SetSettings(Settings{Channel: ChannelStable, InstallTime: "03:30", CheckEvery: CheckManual, AutoInstall: true}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("manual with automatic installation: %v", err)
	}
}

func TestCheckEveryIsValidated(t *testing.T) {
	e := newEnv(t)
	for _, bad := range []int{-2, 1, 30, MinCheckEvery - 1, MaxCheckEvery + 1, 1 << 30} {
		if err := e.m.SetSettings(Settings{Channel: ChannelStable, InstallTime: "03:30", CheckEvery: bad}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("checkEvery %d accepted: %v", bad, err)
		}
	}
	for _, good := range []int{0, CheckManual, MinCheckEvery, 6 * 60, CheckDaily, MaxCheckEvery} {
		if err := e.m.SetSettings(Settings{Channel: ChannelStable, InstallTime: "03:30", CheckEvery: good}); err != nil {
			t.Fatalf("checkEvery %d: %v", good, err)
		}
	}
	// Once a day is written as the field left out, as before it existed.
	if err := e.m.SetSettings(Settings{Channel: ChannelStable, InstallTime: "03:30", CheckEvery: CheckDaily, AutoInstall: true}); err != nil {
		t.Fatal(err)
	}
	raw, err := e.store.Read(settingsFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "checkEvery") || e.m.Status().Settings.CheckEvery != 0 {
		t.Fatalf("daily stored as %s", raw)
	}
}

func TestMissingOrBrokenCheckEveryMeansDaily(t *testing.T) {
	e := newEnv(t)
	for _, file := range []string{
		`{"channel":"stable","autoInstall":false,"installTime":"03:30"}`,
		`{"channel":"stable","installTime":"03:30","checkEvery":5}`,
		`{"channel":"stable","installTime":"03:30","checkEvery":-7}`,
		`{"channel":"stable","installTime":"03:30","checkEvery":99999999}`,
	} {
		if err := e.store.Write(settingsFile, []byte(file)); err != nil {
			t.Fatal(err)
		}
		if err := e.m.Load(); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		e.m.mu.Lock()
		settings := e.m.settings
		e.m.mu.Unlock()
		if settings.Manual() || settings.CheckInterval() != 24*time.Hour {
			t.Fatalf("%s: interval %v manual %v", file, settings.CheckInterval(), settings.Manual())
		}
	}
}

// settingsBefore is update.json as 0.24.0 and the base before it read it.
// A rollback to that app must still understand a file this version wrote.
type settingsBefore struct {
	Channel     string `json:"channel"`
	AutoInstall bool   `json:"autoInstall"`
	InstallTime string `json:"installTime"`
	Source      string `json:"source,omitempty"`
}

func TestOlderVersionReadsTheNewSettings(t *testing.T) {
	e := newEnv(t)
	source := e.m.source()
	for _, settings := range []Settings{
		{Channel: ChannelStable, InstallTime: "04:10", AutoInstall: true, CheckEvery: 6 * 60},
		{Channel: ChannelStable, InstallTime: "02:00", AutoInstall: true, CheckEvery: 7 * 24 * 60},
		{Channel: ChannelStable, InstallTime: "05:45", CheckEvery: CheckManual},
	} {
		if err := e.m.SetSettings(settings); err != nil {
			t.Fatal(err)
		}
		raw, err := e.store.Read(settingsFile)
		if err != nil {
			t.Fatal(err)
		}
		var old settingsBefore
		if err := json.Unmarshal(raw, &old); err != nil {
			t.Fatalf("older version cannot read %s: %v", raw, err)
		}
		// The older version keeps everything it knows and, not knowing the
		// interval, checks once a day; manual mode never leaves automatic
		// installation on, so it does not install by itself either.
		if old.Channel != ChannelStable || old.InstallTime != settings.InstallTime || old.AutoInstall != settings.AutoInstall || old.Source != source {
			t.Fatalf("older version read %+v from %s", old, raw)
		}
		// What the older version writes back is read as once a day here.
		back, _ := json.Marshal(old)
		var now Settings
		if err := json.Unmarshal(back, &now); err != nil || now.CheckEvery != 0 || now.CheckInterval() != 24*time.Hour {
			t.Fatalf("file of the older version read as %+v: %v", now, err)
		}
	}
}
