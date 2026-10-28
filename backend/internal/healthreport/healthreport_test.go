package healthreport

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func healthy() *bool { b := true; return &b }
func failing() *bool { b := false; return &b }

func sample() Data {
	ok := time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC)
	return Data{
		Version: "0.34.0", Uptime: 50 * time.Hour, MemoryPercent: 41,
		Volumes: []Volume{{Name: "data", State: "ok", Mounted: true, Size: 4e12, Used: 1e12}},
		Disks:   []Disk{{Name: "sda", Model: "WD Red", Healthy: healthy(), TempC: 38}, {Name: "sdb", Model: "WD Red", Healthy: healthy(), TempC: 40}},
		Backups: []Backup{{Name: "täglich", Enabled: true, LastOK: &ok}},
	}
}

func TestAllGood(t *testing.T) {
	r := Build(sample(), "de", time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC))
	if r.Status != OK || r.Title != "Bericht – alles in Ordnung" {
		t.Fatalf("%s %q", r.Status, r.Title)
	}
	text := r.Text()
	for _, want := range []string{"✔ Volumes", "data: in Ordnung · 25 % belegt (1.0 TB / 4.0 TB)", "2 Platten, davon 2 ohne SMART-Fehler · wärmste 40 °C", "täglich: zuletzt gelungen 27.10. 03:00", "TitanOS 0.34.0 · läuft seit 2 Tagen", "0 Fehler und 0 Warnungen"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in\n%s", want, text)
		}
	}
}

func TestProblemsRaiseTheStatus(t *testing.T) {
	d := sample()
	d.Volumes[0].State = "degraded"
	r := Build(d, "en", time.Now())
	if r.Status != Warning || !strings.Contains(r.Text(), "⚠ Volumes") || !strings.Contains(r.Text(), "data: without redundancy") {
		t.Fatalf("degraded: %s\n%s", r.Status, r.Text())
	}
	d = sample()
	d.Disks[1].Healthy = failing()
	d.Backups[0].Overdue = true
	r = Build(d, "en", time.Now())
	if r.Status != Error || !strings.Contains(r.Text(), "sdb (WD Red): SMART reports a failure") || !strings.Contains(r.Text(), "täglich: overdue") || !strings.Contains(r.Title, "action needed") {
		t.Fatalf("errors: %s\n%s", r.Status, r.Text())
	}
	d = sample()
	d.Volumes[0].Used = 3.7e12
	d.UpdateAvailable = "0.35.0"
	d.UPS = &UPS{Connected: true, OnBattery: true, Charge: 80}
	d.Errors, d.Warnings = 1, 3
	r = Build(d, "en", time.Now())
	for _, want := range []string{"92 % used", "Update to 0.35.0 available", "UPS: on battery", "1 errors and 3 warnings"} {
		if !strings.Contains(r.Text(), want) {
			t.Fatalf("missing %q in\n%s", want, r.Text())
		}
	}
	d = sample()
	d.Volumes[0] = Volume{Name: "safe", State: "missing", Locked: true}
	d.Backups = nil
	r = Build(d, "de", time.Now())
	if !strings.Contains(r.Text(), "safe: gesperrt (Passwort nötig)") || !strings.Contains(r.Text(), "Kein Sicherungsauftrag eingeschaltet.") || r.Status != Warning {
		t.Fatalf("locked: %s\n%s", r.Status, r.Text())
	}
}

func TestLastDue(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	weekly := Settings{Every: "weekly", Weekday: 1, Time: "08:00", Day: 1}
	// Wednesday 28 Oct 2026: last due Monday 26 Oct 08:00.
	if got := lastDue(weekly, time.Date(2026, 10, 28, 12, 0, 0, 0, berlin)); !got.Equal(time.Date(2026, 10, 26, 8, 0, 0, 0, berlin)) {
		t.Fatalf("weekly: %v", got)
	}
	// Monday 07:59: still the week before.
	if got := lastDue(weekly, time.Date(2026, 10, 26, 7, 59, 0, 0, berlin)); !got.Equal(time.Date(2026, 10, 19, 8, 0, 0, 0, berlin)) {
		t.Fatalf("weekly before time: %v", got)
	}
	monthly := Settings{Every: "monthly", Day: 28, Time: "20:30", Weekday: 0}
	if got := lastDue(monthly, time.Date(2026, 10, 28, 20, 0, 0, 0, berlin)); !got.Equal(time.Date(2026, 9, 28, 20, 30, 0, 0, berlin)) {
		t.Fatalf("monthly: %v", got)
	}
}

func TestScheduleSendsOncePerPeriod(t *testing.T) {
	store := config.Open(t.TempDir())
	m := New(store)
	m.sentFile = filepath.Join(t.TempDir(), "sent")
	now := time.Date(2026, 10, 26, 8, 1, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m.Timezone = func() string { return "UTC" }
	m.Collect = sample
	sent := 0
	fail := false
	m.Send = func(title, text, status string) error {
		if fail {
			return errors.New("smtp down")
		}
		sent++
		return nil
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	m.tick()
	if sent != 0 {
		t.Fatal("sent while off")
	}
	if err := m.Set(Settings{Enabled: true, Every: "weekly", Weekday: 1, Day: 1, Time: "08:00"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(Settings{Enabled: true, Every: "daily", Day: 1, Time: "08:00"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid schedule accepted")
	}
	m.tick()
	m.tick()
	if sent != 1 {
		t.Fatalf("sent %d times, want once", sent)
	}
	// After a restart of titand the note in /run keeps it from going twice.
	again := New(store)
	again.sentFile, again.now, again.Timezone, again.Collect, again.Send = m.sentFile, m.now, m.Timezone, sample, m.Send
	again.Load()
	if at, err := readSent(again.sentFile); err == nil {
		again.lastSent = at
	}
	again.tick()
	if sent != 1 {
		t.Fatal("sent again after a restart")
	}
	// A report more than a day late is skipped; a failure waits an hour.
	now = time.Date(2026, 11, 2, 8, 0, 30, 0, time.UTC)
	fail = true
	m.tick()
	fail = false
	now = now.Add(10 * time.Minute)
	m.tick()
	if sent != 1 {
		t.Fatal("retried within the hour")
	}
	now = now.Add(time.Hour)
	m.tick()
	if sent != 2 {
		t.Fatalf("not retried after an hour: %d", sent)
	}
	now = time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	m.tick()
	if sent != 3 {
		t.Fatal("next week not sent")
	}
	now = time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	m.tick()
	if sent != 3 {
		t.Fatal("sent more than a day late")
	}
	if next := m.Next(); !next.Equal(time.Date(2026, 11, 23, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("next %v", next)
	}
}
