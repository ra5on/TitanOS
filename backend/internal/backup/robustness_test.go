package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A share whose volume is gone is not silently left out: the rest is saved,
// the job says what is missing, and old backups are not forgotten, because
// they are the only copy of it now.
func TestMissingShareKeepsOldBackups(t *testing.T) {
	e := newEnv(t)
	job := s3Job()
	job.Sources = []string{"media", "photos"}
	if err := e.m.Create(job, volumes); err != nil {
		t.Fatal(err)
	}
	var incomplete []string
	e.m.OnIncomplete = func(id string) { incomplete = append(incomplete, id) }
	all := e.m.Sources
	e.m.Sources = func() []Source { return all()[:1] } // photos' volume is gone
	e.m.Start("cloud")
	if got := e.result(t); got != "cloud: ok" {
		t.Fatalf("result: %s", got)
	}
	state := e.state("cloud")
	if !strings.Contains(state.Warning, "Not available, not saved: photos") || state.LastOK == nil {
		t.Errorf("state: %+v", state)
	}
	if len(incomplete) != 1 || incomplete[0] != "cloud" {
		t.Errorf("incomplete told: %v", incomplete)
	}
	if strings.Contains(e.calls(), "forget") {
		t.Error("old backups were forgotten while a share was missing")
	}
	// Back again: everything is saved and retention works as before.
	e.m.Sources = all
	e.m.Start("cloud")
	e.result(t)
	if state := e.state("cloud"); state.Warning != "" {
		t.Errorf("warning stayed: %q", state.Warning)
	}
	if !strings.Contains(e.calls(), "forget --tag titanos") {
		t.Error("retention did not run once all shares were back")
	}
	if len(incomplete) != 1 {
		t.Errorf("incomplete told again: %v", incomplete)
	}
}

// Applications are only stopped once the target is known to be there; an
// unreachable target is retried every half hour.
func TestApplicationsWaitForTheTarget(t *testing.T) {
	e := newEnv(t)
	job := s3Job()
	job.Applications = []Application{{Kind: "docker", Name: "database", Method: "stop"}}
	job.AllowStop = true
	if err := e.m.Create(job, volumes); err != nil {
		t.Fatal(err)
	}
	prepared := 0
	e.m.PrepareApplications = func(context.Context, Job, string, []string) ([]string, func() error, error) {
		prepared++
		return nil, func() error { return nil }, nil
	}
	e.flag(t, "unreachable", true)
	e.m.Start("cloud")
	if got := e.result(t); !strings.Contains(got, "cannot be reached") {
		t.Fatalf("result: %s", got)
	}
	if prepared != 0 {
		t.Fatalf("applications stopped %d times for an unreachable target", prepared)
	}
	e.flag(t, "unreachable", false)
	e.m.Start("cloud")
	if got := e.result(t); got != "cloud: ok" {
		t.Fatalf("result: %s", got)
	}
	if prepared != 1 {
		t.Fatalf("applications prepared %d times, want once", prepared)
	}
}

func TestCancelledRunWaitsForItsNextMoment(t *testing.T) {
	at := func(text string) *time.Time {
		parsed, err := time.ParseInLocation("2006-01-02 15:04", text, time.Local)
		if err != nil {
			t.Fatal(err)
		}
		return &parsed
	}
	daily := Job{Enabled: true, Every: "daily", Time: "02:30"}
	failed := State{LastRun: at("2026-10-14 02:31"), LastError: "unreachable"}
	if !due(daily, failed, *at("2026-10-14 03:10")) {
		t.Error("a failed run is not tried again")
	}
	stopped := State{LastRun: at("2026-10-14 02:31"), LastError: errCancelled}
	if due(daily, stopped, *at("2026-10-14 03:10")) {
		t.Error("a stopped run starts again by itself")
	}
	if !due(daily, stopped, *at("2026-10-15 02:30")) {
		t.Error("a stopped run does not take its next moment")
	}
}

// A job that never worked counts from its first failure; every retry moves
// LastRun on, which used to keep it from ever being overdue.
func TestNeverWorkedBecomesOverdue(t *testing.T) {
	e := newEnv(t)
	if err := e.m.Create(s3Job(), volumes); err != nil {
		t.Fatal(err)
	}
	e.flag(t, "unreachable", true)
	e.m.Start("cloud")
	e.result(t)
	first := e.state("cloud").FailingSince
	if first == nil {
		t.Fatal("first failure not remembered")
	}
	e.now = e.now.Add(3 * 24 * time.Hour)
	e.m.Start("cloud")
	e.result(t)
	state := e.state("cloud")
	if state.FailingSince == nil || !state.FailingSince.Equal(*first) {
		t.Fatalf("first failure moved: %v", state.FailingSince)
	}
	if !overdue(s3Job(), state, e.now) {
		t.Error("never worked for three days, but not overdue")
	}
	e.flag(t, "unreachable", false)
	e.m.Start("cloud")
	e.result(t)
	if state := e.state("cloud"); state.FailingSince != nil {
		t.Error("a backup that worked did not end the failures")
	}
}

// A job whose shares are all gone cannot run and has no state; it still
// becomes overdue.
func TestUnavailableJobBecomesOverdue(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	m := &Manager{now: func() time.Time { return now }, states: map[string]*State{}}
	var warned []string
	m.OnOverdue = func(job string) { warned = append(warned, job) }
	job := Job{ID: "fotos", Enabled: true, Every: "daily", Time: "02:30"}
	m.checkUnavailable(job)
	now = now.Add(24 * time.Hour)
	m.checkUnavailable(job)
	if len(warned) != 0 {
		t.Fatalf("warned after a day: %v", warned)
	}
	now = now.Add(25 * time.Hour)
	m.checkUnavailable(job)
	m.checkUnavailable(job)
	if len(warned) != 1 {
		t.Fatalf("warned %d times, want once", len(warned))
	}
}

// Maintenance does not wait for a job whose state is unknown: it may never
// be able to run.
func TestDueSoonIgnoresUnknownStates(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.Local)
	m := &Manager{now: func() time.Time { return now }, states: map[string]*State{},
		jobs: []Job{{ID: "fotos", Enabled: true, Every: "daily", Time: "02:30"}}}
	if m.DueSoon(now, 5*time.Minute) {
		t.Error("a job without a known state holds maintenance back")
	}
	m.states["fotos"] = &State{StatusError: "backup state volume unavailable"}
	if m.DueSoon(now, 5*time.Minute) {
		t.Error("a job whose volume is missing holds maintenance back")
	}
	m.states["fotos"] = &State{}
	if !m.DueSoon(now, 5*time.Minute) {
		t.Error("a job that is due does not hold maintenance back")
	}
	if !m.DueSoon(time.Date(2026, 10, 15, 2, 27, 0, 0, time.Local), 5*time.Minute) {
		t.Error("a job about to start does not hold maintenance back")
	}
}

func TestScheduleFollowsTheTimeZone(t *testing.T) {
	zone := "UTC"
	m := &Manager{Timezone: func() string { return zone }}
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	if got := m.local(at); got.Location().String() != "UTC" || got.Hour() != 12 {
		t.Errorf("UTC: %v", got)
	}
	zone = "Asia/Tokyo"
	if got := m.local(at); got.Location().String() != "Asia/Tokyo" || got.Hour() != 21 {
		t.Errorf("changed zone not followed: %v", got)
	}
	zone = "No/Such_Zone"
	if got := m.local(at); !got.Equal(at) {
		t.Errorf("unknown zone: %v", got)
	}
}

// The stored REST password goes only to the server it was given for.
func TestSecretsStayWithTheirServer(t *testing.T) {
	e := newEnv(t)
	rest := Job{ID: "server", Sources: []string{"media"}, Enabled: true, Every: "daily", Time: "03:00",
		Destination: Destination{Type: "rest", URL: "https://backup.example:8000/nas", Username: "nas", Password: "restpw"}}
	if err := e.m.Create(rest, volumes); err != nil {
		t.Fatal(err)
	}
	same := rest
	same.Destination.Password = ""
	same.Time = "04:00"
	if err := e.m.Update("server", same, volumes); err != nil {
		t.Fatal(err)
	}
	if job, _ := e.m.find("server"); job.Destination.Password != "restpw" {
		t.Error("an unchanged server lost its password")
	}
	moved := same
	moved.Destination.URL = "https://elsewhere.example/nas"
	if err := e.m.Update("server", moved, volumes); err != nil {
		t.Fatal(err)
	}
	if job, _ := e.m.find("server"); job.Destination.Password != "" {
		t.Error("the password went along to another server")
	}
	cloud := s3Job()
	if err := e.m.Create(cloud, volumes); err != nil {
		t.Fatal(err)
	}
	cloud.Destination.SecretKey = ""
	cloud.Destination.Endpoint = "https://other.example.com"
	if err := e.m.Update("cloud", cloud, volumes); !errors.Is(err, ErrInvalid) {
		t.Errorf("S3 secret kept for another endpoint: %v", err)
	}
}

func TestRouteNamesAreReserved(t *testing.T) {
	e := newEnv(t)
	for _, name := range []string{"target", "SMB", "progress"} {
		job := s3Job()
		job.ID = name
		if err := e.m.Create(job, volumes); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Every look into an SFTP repository has its own key folder, and what ssh
// learnt in one is not lost by another.
func TestSFTPCallsDoNotShareTheirKey(t *testing.T) {
	e := newEnv(t)
	sftp := Job{ID: "box", Sources: []string{"media"}, Enabled: true, Every: "daily", Time: "03:00",
		Destination: Destination{Type: "sftp", Host: "u123.your-storagebox.de", Port: 22, User: "u123", Path: "backups/nas"}}
	if err := e.m.Create(sftp, volumes); err != nil {
		t.Fatal(err)
	}
	_, firstArgs, _, firstCleanup, err := e.m.repository(sftp)
	if err != nil {
		t.Fatal(err)
	}
	_, secondArgs, _, secondCleanup, err := e.m.repository(sftp)
	if err != nil {
		t.Fatal(err)
	}
	dir := func(args []string) string {
		for _, arg := range args {
			if _, rest, ok := strings.Cut(arg, "ssh -i "); ok {
				key, _, _ := strings.Cut(rest, " ")
				return filepath.Dir(key)
			}
		}
		t.Fatal("no key in", args)
		return ""
	}
	first, second := dir(firstArgs), dir(secondArgs)
	if first == second {
		t.Fatal("both calls use the same folder")
	}
	os.WriteFile(filepath.Join(first, "known_hosts"), []byte("first.example ssh-ed25519 AAAA1\n"), 0o600)
	os.WriteFile(filepath.Join(second, "known_hosts"), []byte("second.example ssh-ed25519 AAAA2\n"), 0o600)
	secondCleanup()
	if _, err := os.Stat(filepath.Join(first, "key")); err != nil {
		t.Fatal("the other call lost its key:", err)
	}
	firstCleanup()
	hosts, _ := e.m.store.Read(hostFile)
	if !strings.Contains(string(hosts), "first.example") || !strings.Contains(string(hosts), "second.example") {
		t.Errorf("known_hosts lost a server: %q", hosts)
	}
	if _, err := os.Stat(first); !errors.Is(err, os.ErrNotExist) {
		t.Error("key folder left behind")
	}
}
