package scripts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

type notes struct {
	mu   sync.Mutex
	list []string
}

func (n *notes) add(severity, kind, subject string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.list = append(n.list, severity+" "+kind+" "+subject)
}

func (n *notes) all() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string{}, n.list...)
}

// newTest gives a manager with a mounted volume "Data" in a temporary
// mount root and a stick in a temporary folder.
func newTest(t *testing.T) (*Manager, *notes, string) {
	t.Helper()
	dir := t.TempDir()
	stick := filepath.Join(dir, "config")
	root := filepath.Join(dir, "mnt")
	if err := os.MkdirAll(filepath.Join(root, "Data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stick, 0o700); err != nil {
		t.Fatal(err)
	}
	m := New(config.Open(stick))
	m.MountRoot = root
	mounted := []string{"Data"}
	var mu sync.Mutex
	m.Mounted = func() []string { mu.Lock(); defer mu.Unlock(); return append([]string{}, mounted...) }
	m.Timezone = func() string { return "UTC" }
	m.Shell = "/bin/sh"
	m.CancelGrace = 300 * time.Millisecond
	m.StartMarker = filepath.Join(dir, "run", "scripts-started")
	m.BootID = "boot-1"
	n := &notes{}
	m.Notify = n.add
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m, n, dir
}

func text(s string) *string { return &s }

func create(t *testing.T, m *Manager, name, trigger, content string) Script {
	t.Helper()
	d := Draft{Name: name, Trigger: trigger, Content: text(content)}
	if trigger == Scheduled {
		d.Schedule = &Schedule{Every: "custom", Cron: "* * * * *"}
	}
	s, err := m.Create(d)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func waitDone(t *testing.T, m *Manager, id string) Output {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out, err := m.Output(id, 0)
		if err != nil {
			t.Fatal(err)
		}
		if out.Run != nil && !out.Running {
			return out
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("script did not end")
	return Output{}
}

func TestRunCapturesOutputAndExitCode(t *testing.T) {
	m, n, dir := newTest(t)
	s := create(t, m, "Hallo", Manual, "echo out\necho err >&2\npwd\nexit 3\n")
	if err := m.Run(s.ID); err != nil {
		t.Fatal(err)
	}
	out := waitDone(t, m, s.ID)
	if out.Run.Outcome != "failed" || out.Run.Exit == nil || *out.Run.Exit != 3 {
		t.Fatalf("run = %+v", out.Run)
	}
	folder := filepath.Join(dir, "mnt", "Data", ".titanos", "scripts", s.ID)
	for _, want := range []string{"out\n", "err\n", folder + "\n"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("output %q lacks %q", out.Text, want)
		}
	}
	if len(n.all()) != 0 {
		t.Errorf("a manual run is not notified: %v", n.all())
	}
	runs, err := m.Runs(s.ID)
	if err != nil || len(runs) != 1 || runs[0].Outcome != "failed" || !strings.Contains(runs[0].Output, "out") {
		t.Fatalf("runs = %+v, %v", runs, err)
	}
	st := m.Status()
	if !st.Available || len(st.Scripts) != 1 || st.Scripts[0].Last == nil || st.Scripts[0].Last.Outcome != "failed" {
		t.Fatalf("status = %+v", st)
	}
	// The stick remembers the volume once the first script exists.
	data, err := os.ReadFile(filepath.Join(dir, "config", "system", "scripts.json"))
	if err != nil || !strings.Contains(string(data), `"Data"`) {
		t.Fatalf("settings = %s, %v", data, err)
	}
}

func TestShebangChoosesTheInterpreter(t *testing.T) {
	m, _, _ := newTest(t)
	s := create(t, m, "awk", Manual, "#!/usr/bin/awk -f\nBEGIN { print \"from awk\" }\n")
	if err := m.Run(s.ID); err != nil {
		t.Fatal(err)
	}
	out := waitDone(t, m, s.ID)
	if out.Run.Outcome != "ok" || !strings.Contains(out.Text, "from awk") {
		t.Fatalf("run = %+v, %q", out.Run, out.Text)
	}
	program, args := interpreter(strings.NewReader("echo hi"), "/bin/bash")
	if program != "/bin/bash" || args != nil {
		t.Errorf("no #! line: %s %v", program, args)
	}
	program, args = interpreter(strings.NewReader("#!/usr/bin/env python3 -u\n"), "/bin/bash")
	if program != "/usr/bin/env" || len(args) != 1 || args[0] != "python3 -u" {
		t.Errorf("env: %s %v", program, args)
	}
}

func TestOneRunAtATimeAndCancel(t *testing.T) {
	m, _, _ := newTest(t)
	// The shell ignores SIGTERM: the cancel must end it with SIGKILL.
	s := create(t, m, "lang", Manual, "trap '' TERM\necho begin\nsleep 30 &\nwait\n")
	if err := m.Run(s.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Run(s.ID); !errors.Is(err, ErrRunning) {
		t.Fatalf("second start: %v", err)
	}
	if err := m.Delete(s.ID); !errors.Is(err, ErrRunning) {
		t.Fatalf("delete while running: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := m.Cancel(s.ID); err != nil {
		t.Fatal(err)
	}
	out := waitDone(t, m, s.ID)
	if out.Run.Outcome != "canceled" {
		t.Fatalf("run = %+v", out.Run)
	}
	if err := m.Cancel(s.ID); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("cancel again: %v", err)
	}
}

func TestShutdownScriptsHaveATimeLimitAndAreNotified(t *testing.T) {
	m, n, _ := newTest(t)
	m.ShutdownLimit = 300 * time.Millisecond
	create(t, m, "hängt", Shutdown, "sleep 30\n")
	create(t, m, "schnell", Shutdown, "echo ok\n")
	create(t, m, "nicht beim Herunterfahren", Manual, "exit 1\n")
	var got []string
	began := time.Now()
	m.RunShutdown(context.Background(), func(name, state string, err error) { got = append(got, name+":"+state) })
	if time.Since(began) > 5*time.Second {
		t.Fatalf("shutdown scripts took %v", time.Since(began))
	}
	want := "hängt:running hängt:failed schnell:running schnell:stopped"
	if strings.Join(got, " ") != want {
		t.Fatalf("progress = %v", got)
	}
	if notes := n.all(); len(notes) != 1 || notes[0] != "error script_failed hängt" {
		t.Fatalf("notes = %v", notes)
	}
	if names := m.ShutdownScripts(); len(names) != 2 {
		t.Fatalf("names = %v", names)
	}
}

func TestRunLogKeepsTheLastTen(t *testing.T) {
	m, _, _ := newTest(t)
	s := create(t, m, "oft", Manual, "echo run\n")
	for range 12 {
		if err := m.Run(s.ID); err != nil {
			t.Fatal(err)
		}
		waitDone(t, m, s.ID)
	}
	runs, err := m.Runs(s.ID)
	if err != nil || len(runs) != keepRuns {
		t.Fatalf("runs = %d, %v", len(runs), err)
	}
	for i := 1; i < len(runs); i++ {
		if runs[i].Started.After(runs[i-1].Started) {
			t.Fatal("newest first")
		}
	}
}

func TestOutputIsBounded(t *testing.T) {
	o := &output{limit: 10}
	o.Write([]byte("0123456789abc"))
	text, next, skipped := o.since(0)
	if text != "3456789abc" || next != 13 || !skipped {
		t.Fatalf("%q %d %v", text, next, skipped)
	}
	text, next, skipped = o.since(11)
	if text != "bc" || next != 13 || skipped {
		t.Fatalf("%q %d %v", text, next, skipped)
	}
	if tail, cut := o.tail(4); tail != "9abc" || !cut {
		t.Fatalf("%q %v", tail, cut)
	}
}

func TestInterruptedRunIsMarkedOnLoad(t *testing.T) {
	m, _, dir := newTest(t)
	s := create(t, m, "weg", Manual, "true\n")
	path := filepath.Join(dir, "mnt", "Data", ".titanos", "scripts", s.ID, "runs.json")
	if err := os.WriteFile(path, []byte(`[{"trigger":"manual","started":"2026-11-04T10:00:00Z","durationMs":0,"outcome":"running"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	again := New(config.Open(filepath.Join(dir, "config")))
	again.MountRoot, again.Mounted = m.MountRoot, m.Mounted
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	st := again.Status()
	if len(st.Scripts) != 1 || st.Scripts[0].Last == nil || st.Scripts[0].Last.Outcome != "interrupted" {
		t.Fatalf("status = %+v", st)
	}
}

func TestStartScriptsRunOncePerBoot(t *testing.T) {
	m, _, dir := newTest(t)
	marker := filepath.Join(dir, "mark")
	s := create(t, m, "start", Start, "echo x >> "+marker+"\n")
	ctx := context.Background()
	m.Tick(ctx, time.Now())
	waitDone(t, m, s.ID)
	m.Tick(ctx, time.Now())
	// A restarted titand in the same boot does not run them again.
	again := New(config.Open(filepath.Join(dir, "config")))
	again.MountRoot, again.Mounted, again.StartMarker, again.BootID = m.MountRoot, m.Mounted, m.StartMarker, "boot-1"
	again.Load()
	again.Tick(ctx, time.Now())
	time.Sleep(300 * time.Millisecond)
	data, _ := os.ReadFile(marker)
	if string(data) != "x\n" {
		t.Fatalf("start scripts ran %q", data)
	}
	// The next boot runs them.
	next := New(config.Open(filepath.Join(dir, "config")))
	next.MountRoot, next.Mounted, next.StartMarker, next.BootID, next.Shell = m.MountRoot, m.Mounted, m.StartMarker, "boot-2", "/bin/sh"
	next.Load()
	next.Tick(ctx, time.Now())
	waitDone(t, next, s.ID)
	data, _ = os.ReadFile(marker)
	if string(data) != "x\nx\n" {
		t.Fatalf("start scripts after the next boot: %q", data)
	}
}

func TestScheduledRunsAndFailureNotice(t *testing.T) {
	m, n, _ := newTest(t)
	m.startDone = true
	s, err := m.Create(Draft{Name: "jede Stunde", Trigger: Scheduled, Schedule: &Schedule{Every: "hourly", Minute: 30}, Content: text("exit 2\n")})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	at := time.Date(2026, 11, 4, 9, 28, 10, 0, time.UTC)
	m.Tick(ctx, at)
	m.Tick(ctx, at.Add(time.Minute))
	if out, _ := m.Output(s.ID, 0); out.Run != nil {
		t.Fatal("ran before its minute")
	}
	// The tick for 9:30 came late (9:31): the minute is not lost.
	m.Tick(ctx, at.Add(3*time.Minute))
	out := waitDone(t, m, s.ID)
	if out.Run.Trigger != Scheduled || out.Run.Outcome != "failed" {
		t.Fatalf("run = %+v", out.Run)
	}
	if notes := n.all(); len(notes) != 1 || notes[0] != "error script_failed jede Stunde" {
		t.Fatalf("notes = %v", notes)
	}
	m.Tick(ctx, at.Add(4*time.Minute))
	if runs, _ := m.Runs(s.ID); len(runs) != 1 {
		t.Fatalf("ran twice: %d", len(runs))
	}
	if st := m.Status(); st.Scripts[0].Next == nil || st.Scripts[0].Next.Minute() != 30 {
		t.Fatalf("next = %+v", st.Scripts[0].Next)
	}
}

func TestMissingVolumeSkipsAndNotifiesOnce(t *testing.T) {
	m, n, _ := newTest(t)
	create(t, m, "x", Manual, "true\n")
	m.Mounted = func() []string { return nil }
	m.MissingGrace = 0
	ctx := context.Background()
	m.Tick(ctx, time.Now())
	m.Tick(ctx, time.Now())
	if notes := n.all(); len(notes) != 1 || notes[0] != "warning script_volume_missing Data" {
		t.Fatalf("notes = %v", notes)
	}
	st := m.Status()
	if st.Available || st.Volume != "Data" || len(st.Scripts) != 0 {
		t.Fatalf("status = %+v", st)
	}
	if err := m.Run("0000000000000000"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("run = %v", err)
	}
}

func TestUpdateWritesOnlyChanges(t *testing.T) {
	m, _, dir := newTest(t)
	s := create(t, m, "a", Manual, "echo 1\n")
	index := filepath.Join(dir, "mnt", "Data", ".titanos", "scripts", "index.json")
	before, _ := os.Stat(index)
	time.Sleep(20 * time.Millisecond)
	same, err := m.Update(s.ID, Draft{Name: "a", Trigger: Manual, Content: text("echo 1\n")})
	if err != nil || !same.Changed.Equal(s.Changed) {
		t.Fatalf("unchanged update: %+v, %v", same, err)
	}
	after, _ := os.Stat(index)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("index rewritten without a change")
	}
	changed, err := m.Update(s.ID, Draft{Name: "b", Trigger: Scheduled, Schedule: &Schedule{Every: "daily", Time: "03:15"}})
	if err != nil || changed.Name != "b" || changed.Schedule == nil {
		t.Fatalf("update: %+v, %v", changed, err)
	}
	_, content, err := m.Get(s.ID)
	if err != nil || content != "echo 1\n" {
		t.Fatalf("content kept: %q, %v", content, err)
	}
	if _, err := m.Update(s.ID, Draft{Name: "", Trigger: Manual}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty name: %v", err)
	}
	if _, err := m.Update(s.ID, Draft{Name: "x", Trigger: Scheduled, Schedule: &Schedule{Every: "custom", Cron: "61 * * * *"}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad cron: %v", err)
	}
	if err := m.Delete(s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(index) + "/" + s.ID); !os.IsNotExist(err) {
		t.Fatalf("folder left: %v", err)
	}
}

func TestVolumeChoice(t *testing.T) {
	m, _, dir := newTest(t)
	if st := m.Status(); st.Volume != "Data" || st.Chosen {
		t.Fatalf("default: %+v", st)
	}
	if err := m.SetVolume("Other"); !errors.Is(err, ErrVolume) {
		t.Fatalf("unmounted volume: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config", "system", "scripts.json")); !os.IsNotExist(err) {
		t.Fatal("nothing written before a choice")
	}
	if err := m.SetVolume("Data"); err != nil {
		t.Fatal(err)
	}
	if st := m.Status(); !st.Chosen {
		t.Fatalf("chosen: %+v", st)
	}
}

func TestDisabledRefusesRuns(t *testing.T) {
	m, _, _ := newTest(t)
	s := create(t, m, "x", Manual, "true\n")
	m.Disabled = true
	if err := m.Run(s.ID); !errors.Is(err, ErrDisabled) {
		t.Fatalf("run = %v", err)
	}
}
