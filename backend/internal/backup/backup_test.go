package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// fakeRestic stands in for the program: it notes how it was called and
// answers the way restic does.
const fakeRestic = `#!/bin/sh
# Handle cancellation even if it arrives before the slow command is execed.
trap 'exit 130' INT
dir=$(dirname "$0")
echo "$* | repo=$RESTIC_REPOSITORY cache=$RESTIC_CACHE_DIR aws=$AWS_ACCESS_KEY_ID rest=$RESTIC_REST_USERNAME" >> "$dir/calls"
[ -n "$RESTIC_PASSWORD" ] || { echo "no password" >&2; exit 1; }
command=
skip=
for arg in "$@"; do
  if [ -n "$skip" ]; then skip=; continue; fi
  case "$arg" in
    -o|--limit-upload|--limit-download) skip=1 ;;
    -*) ;;
    *) command=$arg; break ;;
  esac
done
case "$command" in
  cat) [ -e "$dir/initialized" ] || { echo "Fatal: repository does not exist" >&2; exit 10; } ;;
  init) [ -e "$dir/unreachable" ] && { echo "Fatal: dial tcp: connection refused" >&2; exit 1; }; touch "$dir/initialized" ;;
  unlock|forget) ;;
  backup)
    echo '{"message_type":"status","percent_done":0.5,"total_bytes":2000,"bytes_done":1000}'
    # Replace the shell: a foreground child can inherit ignored SIGINT and
    # defer the shell trap until its sleep ends. Real restic is one process.
    [ -e "$dir/slow" ] && exec sleep 30
    if [ -e "$dir/broken" ]; then echo "Fatal: unable to save snapshot: server said no (password $RESTIC_PASSWORD)" >&2; exit 1; fi
    echo '{"message_type":"summary","total_bytes_processed":2000,"snapshot_id":"abcdef0123"}'
    if [ -e "$dir/unreadable" ]; then echo "error: open /mnt/tank/media/locked: permission denied" >&2; exit 3; fi ;;
  snapshots)
    echo '[{"time":"2026-10-12T02:30:00Z","short_id":"11111111","paths":["/mnt/tank/media"]},{"time":"2026-10-13T02:30:00Z","short_id":"22222222","paths":["/mnt/tank/media"],"summary":{"total_bytes_processed":2000}}]' ;;
  restore)
    target=
    while [ $# -gt 0 ]; do [ "$1" = "--target" ] && target=$2; shift; done
    echo '{"message_type":"status","percent_done":1,"total_bytes":2000,"bytes_restored":2000}'
    mkdir -p "$target/mnt/tank/media" && echo restored > "$target/mnt/tank/media/file" ;;
  check)
    if [ -e "$dir/corrupt" ]; then echo "error: pack 1234abcd: data damaged" >&2; exit 1; fi
    echo "no errors were found" ;;
  ls)
    for last in "$@"; do :; done
    echo '{"time":"2026-10-13T02:30:00Z","paths":["/mnt/tank/media"],"struct_type":"snapshot"}'
    case "$last" in
      /mnt/tank/media)
        echo '{"name":"media","type":"dir","path":"/mnt/tank/media","struct_type":"node"}'
        echo '{"name":"zebra.jpg","type":"file","path":"/mnt/tank/media/zebra.jpg","size":2048,"mtime":"2026-10-01T10:00:00Z","struct_type":"node"}'
        echo '{"name":"Alben","type":"dir","path":"/mnt/tank/media/Alben","struct_type":"node"}'
        echo '{"name":"apfel.png","type":"file","path":"/mnt/tank/media/apfel.png","size":10,"struct_type":"node"}' ;;
      *) echo '{"name":"mnt","type":"dir","path":"/mnt","struct_type":"node"}' ;;
    esac ;;
  *) echo "unknown command $command" >&2; exit 1 ;;
esac
`

type env struct {
	m       *Manager
	dir     string
	results chan string
	now     time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	root := t.TempDir()
	e := &env{dir: filepath.Join(root, "bin"), results: make(chan string, 10), now: time.Date(2026, 10, 14, 12, 0, 0, 0, time.Local)}
	for _, dir := range []string{"bin", "config", "mnt/tank/media", "mnt/tank/photos", "mnt/cold", "run"} {
		os.MkdirAll(filepath.Join(root, dir), 0o755)
	}
	if err := os.WriteFile(filepath.Join(e.dir, "restic"), []byte(fakeRestic), 0o755); err != nil {
		t.Fatal(err)
	}
	e.m = NewManager(config.Open(filepath.Join(root, "config")))
	e.m.restic, e.m.mountRoot, e.m.workDir = filepath.Join(e.dir, "restic"), filepath.Join(root, "mnt"), filepath.Join(root, "run")
	e.m.now = func() time.Time { return e.now }
	e.m.Sources = func() []Source {
		return []Source{{Name: "media", Volume: "tank", Path: filepath.Join(root, "mnt/tank/media")}, {Name: "photos", Volume: "tank", Path: filepath.Join(root, "mnt/tank/photos")}}
	}
	e.m.OnResult = func(job string, err error) {
		if err != nil {
			e.results <- job + ": " + err.Error()
		} else {
			e.results <- job + ": ok"
		}
	}
	if err := e.m.Load(); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) flag(t *testing.T, name string, on bool) {
	t.Helper()
	if on {
		os.WriteFile(filepath.Join(e.dir, name), nil, 0o644)
	} else {
		os.Remove(filepath.Join(e.dir, name))
	}
}

func (e *env) calls() string {
	data, _ := os.ReadFile(filepath.Join(e.dir, "calls"))
	return string(data)
}

func (e *env) result(t *testing.T) string {
	t.Helper()
	select {
	case result := <-e.results:
		return result
	case <-time.After(20 * time.Second):
		t.Fatal("the job did not finish")
		return ""
	}
}

func (e *env) state(id string) State {
	for _, view := range e.m.List() {
		if view.Job.ID == id {
			return view.State
		}
	}
	return State{}
}

var volumes = []string{"tank", "cold"}

func s3Job() Job {
	return Job{ID: "cloud", Sources: []string{"media"}, Enabled: true, Every: "daily", Time: "02:30", Keep: Keep{Daily: 7, Monthly: 6},
		Destination: Destination{Type: "s3", Endpoint: "https://s3.example.com", Bucket: "nas-backup", Prefix: "keller", AccessKey: "AKIA123", SecretKey: "topsecret"}}
}

func TestBackupRunsAndRemembers(t *testing.T) {
	e := newEnv(t)
	if err := e.m.Create(s3Job(), volumes); err != nil {
		t.Fatal(err)
	}
	view := e.m.List()[0]
	// What the web UI sees holds no secret, only that they are set.
	if view.Job.Password != "" || view.Job.Destination.SecretKey != "" || !view.SecretSet["secretKey"] || view.SecretSet["password"] {
		t.Errorf("view: %+v", view)
	}
	password, _ := e.m.Password("cloud")
	if len(password) < 30 {
		t.Errorf("password: %q", password)
	}
	if err := e.m.Start("cloud"); err != nil {
		t.Fatal(err)
	}
	if got := e.result(t); got != "cloud: ok" {
		t.Fatalf("result: %s", got)
	}
	state := e.state("cloud")
	if state.Running || state.LastOK == nil || state.LastError != "" || state.Bytes != 2000 || state.Warning != "" {
		t.Errorf("state: %+v", state)
	}
	calls := e.calls()
	for _, want := range []string{
		"cat config --quiet | repo=s3:https://s3.example.com/nas-backup/keller", "init |", "unlock |",
		"backup --json --tag titanos --exclude .recycle --exclude *.titanos-part " + filepath.Join(e.m.mountRoot, "tank/media"),
		"cache=" + filepath.Join(e.m.mountRoot, "tank/.titanos/backup/cache"), "aws=AKIA123",
		"forget --tag titanos --group-by tags --prune", "--keep-daily 7", "--keep-monthly 6",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("%q missing in:\n%s", want, calls)
		}
	}
	if strings.Contains(calls, "--keep-weekly") || strings.Contains(calls, "topsecret") || strings.Contains(calls, password) {
		t.Errorf("calls: %s", calls)
	}
	// A restarted titand finds what the job remembers on the volume.
	again := NewManager(e.m.store)
	again.mountRoot, again.Sources = e.m.mountRoot, e.m.Sources
	again.Load()
	if state := again.List()[0].State; state.LastOK == nil || state.Bytes != 2000 {
		t.Errorf("after a restart: %+v", state)
	}
	// The second run finds the repository and does not make it again.
	os.Remove(filepath.Join(e.dir, "calls"))
	e.m.Start("cloud")
	e.result(t)
	if strings.Contains(e.calls(), "init |") {
		t.Errorf("made the repository again: %s", e.calls())
	}
}

func TestFailuresAndWarnings(t *testing.T) {
	e := newEnv(t)
	e.m.Create(s3Job(), volumes)
	password, _ := e.m.Password("cloud")

	e.flag(t, "unreachable", true)
	e.m.Start("cloud")
	if got := e.result(t); !strings.Contains(got, "cannot be reached") || !strings.Contains(got, "connection refused") {
		t.Errorf("unreachable: %s", got)
	}
	e.flag(t, "unreachable", false)

	e.flag(t, "broken", true)
	e.m.Start("cloud")
	got := e.result(t)
	if !strings.Contains(got, "server said no") || strings.Contains(got, password) {
		t.Errorf("broken: %s", got)
	}
	if state := e.state("cloud"); state.LastError == "" || state.LastOK != nil || state.LastRun == nil {
		t.Errorf("state: %+v", state)
	}
	e.flag(t, "broken", false)

	// Some files could not be read: the backup counts, with a warning.
	e.flag(t, "unreadable", true)
	e.m.Start("cloud")
	if got := e.result(t); got != "cloud: ok" {
		t.Errorf("unreadable: %s", got)
	}
	if state := e.state("cloud"); state.LastOK == nil || !strings.Contains(state.Warning, "permission denied") {
		t.Errorf("state: %+v", state)
	}
}

func TestStopAndBusy(t *testing.T) {
	e := newEnv(t)
	e.m.Create(s3Job(), volumes)
	e.flag(t, "slow", true)
	if err := e.m.Start("cloud"); err != nil {
		t.Fatal(err)
	}
	for range 200 {
		if e.state("cloud").Percent > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	state := e.state("cloud")
	if !state.Running || state.Phase != "backup" || state.Percent != 50 || state.BytesTotal != 2000 {
		t.Errorf("while running: %+v", state)
	}
	if err := e.m.Start("cloud"); !errors.Is(err, ErrRunning) {
		t.Errorf("second start: %v", err)
	}
	if err := e.m.Delete("cloud"); !errors.Is(err, ErrRunning) {
		t.Errorf("delete while running: %v", err)
	}
	if err := e.m.Stop("cloud"); err != nil {
		t.Fatal(err)
	}
	for range 300 {
		if !e.state("cloud").Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if state := e.state("cloud"); state.Running || state.LastError != "cancelled" || state.LastOK != nil {
		t.Errorf("after stopping: %+v", state)
	}
	select {
	case result := <-e.results:
		t.Errorf("a cancelled backup was reported: %s", result)
	default:
	}
}

func TestDestinations(t *testing.T) {
	e := newEnv(t)
	local := Job{ID: "kopie", Sources: []string{"media", "photos"}, Enabled: true, Every: "weekly", Weekday: 0, Time: "04:00",
		Destination: Destination{Type: "local", Volume: "cold"}}
	rest := Job{ID: "server", Sources: []string{"media"}, Every: "daily", Time: "01:00",
		Destination: Destination{Type: "rest", URL: "https://backup.example:8000/nas", Username: "nas", Password: "restpw"}}
	sftp := Job{ID: "box", Sources: []string{"media"}, Every: "daily", Time: "01:00",
		Destination: Destination{Type: "sftp", Host: "u123.your-storagebox.de", User: "u123", Path: "backups/nas"}}
	for _, job := range []Job{local, rest, sftp} {
		if err := e.m.Create(job, volumes); err != nil {
			t.Fatalf("%s: %v", job.ID, err)
		}
	}
	if err := e.m.Create(local, volumes); !errors.Is(err, ErrExists) {
		t.Errorf("same name twice: %v", err)
	}
	// The local one needs its volume mounted; here /proc knows nothing of it.
	e.m.Start("kopie")
	if got := e.result(t); !strings.Contains(got, "not mounted") {
		t.Errorf("local without the volume: %s", got)
	}
	e.m.Start("server")
	e.result(t)
	e.m.Start("box")
	e.result(t)
	calls := e.calls()
	for _, want := range []string{
		"repo=rest:https://backup.example:8000/nas", "rest=nas",
		"-o sftp.command=ssh -i " + filepath.Join(e.m.workDir, "box-"), "StrictHostKeyChecking=accept-new", "-p 22 u123@u123.your-storagebox.de -s sftp",
		"repo=sftp:u123@u123.your-storagebox.de:backups/nas",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("%q missing in:\n%s", want, calls)
		}
	}
	if strings.Contains(calls, "restpw") {
		t.Error("the password of the server is on a command line")
	}
	// The key of this NAS is made once and stays; the copy for the run is gone.
	key, err := e.m.PublicKey()
	if err != nil || !strings.HasPrefix(key, "ssh-ed25519 ") || !strings.HasSuffix(key, " titanos-backup") {
		t.Errorf("public key: %q %v", key, err)
	}
	if again, _ := e.m.PublicKey(); again != key {
		t.Error("the key changed")
	}
	if _, err := os.Stat(filepath.Join(e.m.workDir, "box")); err == nil {
		t.Error("the private key was left in the work directory")
	}
	defaults := e.m.List()
	if defaults[0].Job.Destination.Folder != "backups/kopie" || defaults[2].Job.Destination.Port != 22 {
		t.Errorf("defaults: %+v %+v", defaults[0].Job.Destination, defaults[2].Job.Destination)
	}

	// Changing a job keeps its secrets and its password.
	password, _ := e.m.Password("server")
	changed := rest
	changed.Destination.Password, changed.Time = "", "05:00"
	if err := e.m.Update("server", changed, volumes); err != nil {
		t.Fatal(err)
	}
	if after, _ := e.m.Password("server"); after != password {
		t.Error("the password of the backup changed")
	}
	if job, _ := e.m.find("server"); job.Destination.Password != "restpw" || job.Time != "05:00" {
		t.Errorf("after the change: %+v", job)
	}
	if err := e.m.Delete("server"); err != nil || len(e.m.List()) != 2 {
		t.Errorf("delete: %v", err)
	}
}

func TestInvalidJobs(t *testing.T) {
	e := newEnv(t)
	for name, change := range map[string]func(*Job){
		"name":                func(j *Job) { j.ID = "Mein Backup" },
		"no share":            func(j *Job) { j.Sources = nil },
		"unknown share":       func(j *Job) { j.Sources = []string{"gone"} },
		"time":                func(j *Job) { j.Time = "25:00" },
		"rhythm":              func(j *Job) { j.Every = "yearly" },
		"interval":            func(j *Job) { j.Every, j.Interval = "hourly", 5 },
		"month day":           func(j *Job) { j.Every, j.MonthDay = "monthly", 31 },
		"weekday":             func(j *Job) { j.Every, j.Weekdays = "weekly", []int{1, 7} },
		"second time":         func(j *Job) { j.Times = []string{"03:00", "3pm"} },
		"half a window":       func(j *Job) { j.WindowFrom = "01:00" },
		"empty window":        func(j *Job) { j.WindowFrom, j.WindowTo = "01:00", "01:00" },
		"keep":                func(j *Job) { j.Keep.Daily = -1 },
		"kind":                func(j *Job) { j.Destination.Type = "ftp" },
		"endpoint":            func(j *Job) { j.Destination.Endpoint = "s3.example.com" },
		"endpoint with login": func(j *Job) { j.Destination.Endpoint = "https://user:pw@s3.example.com" },
		"bucket":              func(j *Job) { j.Destination.Bucket = "My Bucket" },
		"no secret":           func(j *Job) { j.Destination.SecretKey = "" },
		"same volume":         func(j *Job) { j.Destination = Destination{Type: "local", Volume: "tank"} },
		"unknown volume":      func(j *Job) { j.Destination = Destination{Type: "local", Volume: "nowhere"} },
		"folder leaves":       func(j *Job) { j.Destination = Destination{Type: "local", Volume: "cold", Folder: "../etc"} },
		"sftp host": func(j *Job) {
			j.Destination = Destination{Type: "sftp", Host: "host -oProxyCommand=evil", User: "u", Path: "x"}
		},
		"sftp user": func(j *Job) {
			j.Destination = Destination{Type: "sftp", Host: "host", User: "-oProxyCommand=evil", Path: "x"}
		},
		"sftp path":    func(j *Job) { j.Destination = Destination{Type: "sftp", Host: "host", User: "u", Path: "-x"} },
		"rest address": func(j *Job) { j.Destination = Destination{Type: "rest", URL: "ftp://x"} },
	} {
		job := s3Job()
		change(&job)
		if err := e.m.Create(job, volumes); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(e.m.List()) != 0 {
		t.Error("an invalid job was stored")
	}
}

func TestSnapshotsAndRestore(t *testing.T) {
	e := newEnv(t)
	e.m.Create(s3Job(), volumes)
	snapshots, err := e.m.Snapshots(context.Background(), "cloud")
	if err != nil || len(snapshots) != 2 || snapshots[0].ID != "22222222" || snapshots[0].Bytes != 2000 || snapshots[1].Bytes != 0 {
		t.Fatalf("snapshots: %+v %v", snapshots, err)
	}
	if _, err := e.m.Restore("cloud", "../../etc", "tank"); !errors.Is(err, ErrInvalid) {
		t.Errorf("odd backup id: %v", err)
	}
	if _, err := e.m.Restore("cloud", "22222222", "nowhere"); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown volume: %v", err)
	}
	target, err := e.m.Restore("cloud", "22222222", "tank")
	if err != nil {
		t.Fatal(err)
	}
	// Into a folder of its own, never over the share.
	if want := filepath.Join(e.m.mountRoot, "tank/restored/cloud-20261014-120000"); !strings.HasPrefix(target, want+"-") {
		t.Errorf("target: %s, want %s", target, want)
	}
	for range 300 {
		if state := e.state("cloud"); !state.Running && state.RestoredTo != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if data, err := os.ReadFile(filepath.Join(target, "mnt/tank/media/file")); err != nil || string(data) != "restored\n" {
		t.Errorf("restored file: %q %v", data, err)
	}
	if state := e.state("cloud"); state.RestoredTo != target || state.LastRun != nil {
		t.Errorf("state: %+v", state)
	}
}

func TestSchedule(t *testing.T) {
	at := func(text string) time.Time {
		parsed, err := time.ParseInLocation("2006-01-02 15:04", text, time.Local)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	ran := func(text string) State { last := at(text); return State{LastRun: &last} }
	daily := Job{Enabled: true, Every: "daily", Time: "02:30"}
	weekly := Job{Enabled: true, Every: "weekly", Weekday: 0, Time: "04:00"} // Sundays; 2026-10-11 and -18 are Sundays
	cases := []struct {
		name  string
		job   Job
		state State
		now   string
		want  bool
	}{
		{"never ran", daily, State{}, "2026-10-14 15:00", true},
		{"ran tonight", daily, ran("2026-10-14 02:30"), "2026-10-14 15:00", false},
		{"ran yesterday, time not yet come", daily, ran("2026-10-13 02:31"), "2026-10-14 02:29", false},
		{"ran yesterday, time has come", daily, ran("2026-10-13 02:31"), "2026-10-14 02:30", true},
		{"NAS was off for days", daily, ran("2026-10-10 02:31"), "2026-10-14 01:00", true},
		{"switched off", Job{Every: "daily", Time: "02:30"}, State{}, "2026-10-14 15:00", false},
		{"running", daily, State{Running: true}, "2026-10-14 15:00", false},
		{"weekly, ran this Sunday", weekly, ran("2026-10-11 04:01"), "2026-10-14 15:00", false},
		{"weekly, Sunday morning before the time", weekly, ran("2026-10-11 04:01"), "2026-10-18 03:59", false},
		{"weekly, Sunday at the time", weekly, ran("2026-10-11 04:01"), "2026-10-18 04:00", true},
		{"weekly, missed last Sunday", weekly, ran("2026-10-04 04:01"), "2026-10-14 15:00", true},
	}
	for _, tc := range cases {
		if got := due(tc.job, tc.state, at(tc.now)); got != tc.want {
			t.Errorf("%s: due = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestScheduleKinds(t *testing.T) {
	at := func(text string) time.Time {
		parsed, err := time.ParseInLocation("2006-01-02 15:04", text, time.Local)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	ran := func(text string) State { last := at(text); return State{LastRun: &last} }
	hourly := Job{Enabled: true, Every: "hourly", Interval: 6, Time: "00:15"}
	twice := Job{Enabled: true, Every: "daily", Times: []string{"15:00", "03:00"}, Time: "03:00"}
	days := Job{Enabled: true, Every: "weekly", Weekdays: []int{1, 3, 5}, Times: []string{"03:00"}, Time: "03:00"} // Mon, Wed, Fri; 2026-10-14 is a Wednesday
	monthly := Job{Enabled: true, Every: "monthly", MonthDay: 1, Time: "04:00"}
	night := twice
	night.WindowFrom, night.WindowTo = "01:00", "06:00"
	late := hourly
	late.WindowFrom, late.WindowTo = "22:00", "04:00" // over midnight
	cases := []struct {
		name  string
		job   Job
		state State
		now   string
		want  bool
	}{
		{"hourly, slot at 12:15 passed", hourly, ran("2026-10-14 06:20"), "2026-10-14 12:15", true},
		{"hourly, between slots", hourly, ran("2026-10-14 12:16"), "2026-10-14 17:00", false},
		{"two times, second one owed", twice, ran("2026-10-14 03:01"), "2026-10-14 15:00", true},
		{"two times, both done", twice, ran("2026-10-14 15:01"), "2026-10-14 20:00", false},
		{"weekdays, Wednesday owed", days, ran("2026-10-12 03:01"), "2026-10-14 03:00", true},
		{"weekdays, Thursday has nothing", days, ran("2026-10-14 03:01"), "2026-10-15 12:00", false},
		{"weekdays, Friday owed", days, ran("2026-10-14 03:01"), "2026-10-16 03:00", true},
		{"monthly, before the first", monthly, ran("2026-10-01 04:01"), "2026-10-30 12:00", false},
		{"monthly, first of the month", monthly, ran("2026-10-01 04:01"), "2026-11-01 04:00", true},
		{"window waits", night, ran("2026-10-13 15:01"), "2026-10-14 03:30", true},
		{"window closed", night, ran("2026-10-13 15:01"), "2026-10-14 15:30", false},
		{"window over midnight, open", late, ran("2026-10-14 12:16"), "2026-10-14 23:00", true},
		{"window over midnight, closed", late, ran("2026-10-14 12:16"), "2026-10-14 18:00", false},
	}
	for _, tc := range cases {
		if got := due(tc.job, tc.state, at(tc.now)); got != tc.want {
			t.Errorf("%s: due = %v, want %v", tc.name, got, tc.want)
		}
	}

	// A failed attempt is tried again after a while, once.
	failed := ran("2026-10-14 03:01")
	failed.LastError = "unreachable"
	last := at("2026-10-13 15:01")
	failed.LastOK = &last
	if due(twice, failed, at("2026-10-14 03:20")) || !due(twice, failed, at("2026-10-14 03:31")) {
		t.Error("retry after a failed attempt")
	}
	failed.LastOK = failed.LastRun
	if due(twice, failed, at("2026-10-14 03:40")) {
		t.Error("retry although the slot was met")
	}

	next, ok := night.NextRun(at("2026-10-14 15:30"))
	if !ok || !next.Equal(at("2026-10-15 03:00")) {
		t.Errorf("next = %v", next)
	}
	// A slot outside the window waits for the window to open.
	night.Times = []string{"22:00"}
	if next, _ := night.NextRun(at("2026-10-14 15:30")); !next.Equal(at("2026-10-15 01:00")) {
		t.Errorf("next with the window closed = %v", next)
	}
	if _, ok := (Job{Every: "daily", Time: "01:00"}).NextRun(at("2026-10-14 15:30")); ok {
		t.Error("a job that is off has no next run")
	}
}

func TestDevelopmentModeRunsNothing(t *testing.T) {
	e := newEnv(t)
	e.m.Create(s3Job(), volumes)
	e.m.Disabled = true
	if err := e.m.Start("cloud"); !errors.Is(err, ErrDisabled) {
		t.Errorf("start: %v", err)
	}
	if _, err := e.m.Restore("cloud", "22222222", "tank"); !errors.Is(err, ErrDisabled) {
		t.Errorf("restore: %v", err)
	}
	if e.calls() != "" {
		t.Errorf("restic was called: %s", e.calls())
	}
}

func TestOverdue(t *testing.T) {
	at := func(text string) *time.Time {
		parsed, err := time.ParseInLocation("2006-01-02 15:04", text, time.Local)
		if err != nil {
			t.Fatal(err)
		}
		return &parsed
	}
	ok := func(text string) State {
		return State{LastRun: at("2026-10-19 02:30"), LastOK: at(text), LastError: "unreachable"}
	}
	daily := Job{Enabled: true, Every: "daily", Time: "02:30"}
	hourly := Job{Enabled: true, Every: "hourly", Interval: 1, Time: "00:15"}
	weekly := Job{Enabled: true, Every: "weekly", Weekday: 0, Time: "04:00"} // Sundays
	monthly := Job{Enabled: true, Every: "monthly", MonthDay: 1, Time: "03:00"}
	cases := []struct {
		name  string
		job   Job
		state State
		now   string
		want  bool
	}{
		{"never tried", daily, State{}, "2026-10-19 12:00", false},
		{"daily, one run missed", daily, ok("2026-10-18 02:31"), "2026-10-19 12:00", false},
		{"daily, two runs missed but not two days", daily, ok("2026-10-17 02:31"), "2026-10-19 02:30", false},
		{"daily, two runs and two days", daily, ok("2026-10-17 02:31"), "2026-10-19 02:31", true},
		{"hourly, failing for a day", hourly, ok("2026-10-18 13:00"), "2026-10-19 12:00", false},
		{"hourly, failing for two days", hourly, ok("2026-10-17 11:00"), "2026-10-19 12:00", true},
		{"weekly, one Sunday missed", weekly, ok("2026-10-11 04:01"), "2026-10-19 12:00", false},
		{"weekly, two Sundays missed", weekly, ok("2026-10-04 04:01"), "2026-10-19 12:00", true},
		{"monthly, one month missed", monthly, ok("2026-09-01 03:01"), "2026-10-19 12:00", false},
		{"monthly, not worked for a year", monthly, ok("2025-09-01 03:01"), "2026-10-19 12:00", true},
		{"failed from the start", daily, State{LastRun: at("2026-10-16 02:30"), LastError: "x"}, "2026-10-19 12:00", true},
		{"switched off", Job{Every: "daily", Time: "02:30"}, ok("2026-10-01 02:31"), "2026-10-19 12:00", false},
		{"running", daily, State{Running: true, LastOK: at("2026-10-01 02:31")}, "2026-10-19 12:00", false},
	}
	for _, tc := range cases {
		if got := overdue(tc.job, tc.state, *at(tc.now)); got != tc.want {
			t.Errorf("%s: overdue = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The warning comes once, keeps quiet while a run is going, and comes again
// only after a backup worked in between.
func TestOverdueWarnsOnce(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	m := &Manager{now: func() time.Time { return now }, states: map[string]*State{}}
	var warned []string
	m.OnOverdue = func(job string) { warned = append(warned, job) }
	job := Job{ID: "fotos", Enabled: true, Every: "daily", Time: "02:30"}
	old, recent := now.Add(-5*24*time.Hour), now.Add(-time.Hour)
	late := State{LastRun: &recent, LastOK: &old, LastError: "unreachable"}
	m.checkOverdue(job, late)
	m.checkOverdue(job, late)
	m.checkOverdue(job, State{Running: true, LastOK: &old})
	m.checkOverdue(job, late)
	if len(warned) != 1 {
		t.Fatalf("warned %d times, want once", len(warned))
	}
	m.checkOverdue(job, State{LastRun: &recent, LastOK: &recent})
	m.checkOverdue(job, late)
	if len(warned) != 2 {
		t.Fatalf("no new warning after a backup worked in between: %v", warned)
	}
}
