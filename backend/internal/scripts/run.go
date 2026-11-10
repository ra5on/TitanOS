package scripts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// output keeps the end of what a script writes (stdout and stderr), with
// the offset of its first byte, so that the page can ask for what is new.
type output struct {
	mu    sync.Mutex
	data  []byte
	base  int64
	limit int
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.data = append(o.data, p...)
	if over := len(o.data) - o.limit; over > 0 {
		o.data = slices.Clone(o.data[over:])
		o.base += int64(over)
	}
	return len(p), nil
}

// since returns the output from offset on, the offset after it and
// whether some bytes before were dropped already.
func (o *output) since(from int64) (string, int64, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	end := o.base + int64(len(o.data))
	skipped := false
	if from < o.base {
		from, skipped = o.base, from > 0 || o.base > 0
	}
	if from > end {
		from = end
	}
	return string(o.data[from-o.base:]), end, skipped
}

// tail returns at most n bytes from the end and whether more was written.
func (o *output) tail(n int) (string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	data := o.data
	cut := o.base > 0
	if len(data) > n {
		data, cut = data[len(data)-n:], true
	}
	return string(data), cut
}

func (o *output) shrink(n int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if over := len(o.data) - n; over > 0 {
		o.data = slices.Clone(o.data[over:])
		o.base += int64(over)
	}
}

// execution is one run under way (or the last one, finished).
type execution struct {
	id, name, trigger string
	started           time.Time
	out               *output
	cmd               *exec.Cmd
	done              chan struct{}
	mu                sync.Mutex
	reason            string
	result            Run
}

func (e *execution) stopReason(reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.reason == "" {
		e.reason = reason
	}
}

// Output is the live view of a run.
type Output struct {
	Running bool   `json:"running"`
	Text    string `json:"text"`
	Next    int64  `json:"next"`
	Skipped bool   `json:"skipped"`
	// Run is the run, with Outcome running while it lasts.
	Run *Run `json:"run,omitempty"`
}

// Output returns the output of the current or last run since the offset.
func (m *Manager) Output(id string, from int64) (Output, error) {
	m.mu.Lock()
	e := m.running[id]
	if e == nil {
		e = m.finished[id]
	}
	m.mu.Unlock()
	if e == nil {
		if _, ok := m.find(id); !ok {
			return Output{}, ErrNotFound
		}
		return Output{}, nil
	}
	text, next, skipped := e.out.since(from)
	select {
	case <-e.done:
		r := e.result
		r.Output = ""
		return Output{Text: text, Next: next, Skipped: skipped, Run: &r}, nil
	default:
		return Output{Running: true, Text: text, Next: next, Skipped: skipped, Run: &Run{Trigger: e.trigger, Started: e.started, Outcome: "running"}}, nil
	}
}

// Run starts a script by hand; the output is read with Output.
func (m *Manager) Run(id string) error {
	_, err := m.start(id, Manual, 0)
	return err
}

// Cancel ends a running script: SIGTERM to its process group, SIGKILL
// after CancelGrace.
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	e := m.running[id]
	m.mu.Unlock()
	if e == nil {
		return ErrNotRunning
	}
	m.terminate(e, "canceled")
	return nil
}

// Busy tells whether a script runs.
func (m *Manager) Busy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.running) > 0
}

func (m *Manager) terminate(e *execution, reason string) {
	e.stopReason(reason)
	if e.cmd == nil || e.cmd.Process == nil {
		return
	}
	pid := e.cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	go func() {
		select {
		case <-e.done:
		case <-time.After(m.CancelGrace):
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}()
}

func (m *Manager) scriptEnv(s Script) []string {
	return []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"LANG=C.UTF-8",
		"SHELL=" + m.Shell,
		"TITANOS_SCRIPT_ID=" + s.ID,
		"TITANOS_SCRIPT_NAME=" + s.Name,
	}
}

// start begins a run; limit 0 means no time limit.
func (m *Manager) start(id, trigger string, limit time.Duration) (*execution, error) {
	if m.Disabled {
		return nil, ErrDisabled
	}
	if err := m.ensure(); err != nil {
		return nil, err
	}
	s, ok := m.find(id)
	if !ok {
		return nil, ErrNotFound
	}
	e := &execution{id: id, name: s.Name, trigger: trigger, started: time.Now().UTC(), out: &output{limit: liveOutput}, done: make(chan struct{})}
	m.mu.Lock()
	if m.running[id] != nil {
		m.mu.Unlock()
		return nil, ErrRunning
	}
	m.running[id] = e
	volume := m.loaded
	m.mu.Unlock()

	fail := func(err error) (*execution, error) {
		e.result = Run{Trigger: trigger, Started: e.started, Outcome: "error", Error: err.Error()}
		m.record(e, volume)
		return nil, err
	}
	root, err := m.open(volume)
	if err != nil {
		return fail(err)
	}
	content, err := readContent(root, id)
	if err == nil {
		err = m.saveRun(root, id, Run{Trigger: trigger, Started: e.started, Outcome: "running"})
	}
	root.Close()
	if err != nil {
		return fail(err)
	}
	dir := filepath.Join(m.MountRoot, volume, ".titanos", feature, id)
	program, args := interpreter(strings.NewReader(content), m.Shell)
	cmd := exec.Command(program, append(args, filepath.Join(dir, "script"))...)
	cmd.Dir = dir
	cmd.Env = m.scriptEnv(s)
	cmd.Stdout, cmd.Stderr = e.out, e.out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// A script that leaves a program behind holding its output must not
	// keep the run open.
	cmd.WaitDelay = 5 * time.Second
	e.cmd = cmd
	if err := cmd.Start(); err != nil {
		return fail(err)
	}
	slog.Info("script started", "script", s.Name, "trigger", trigger)
	go m.wait(e, volume, limit)
	return e, nil
}

func (m *Manager) wait(e *execution, volume string, limit time.Duration) {
	waited := make(chan error, 1)
	go func() { waited <- e.cmd.Wait() }()
	var timeout <-chan time.Time
	if limit > 0 {
		timer := time.NewTimer(limit)
		defer timer.Stop()
		timeout = timer.C
	}
	var err error
	for done := false; !done; {
		select {
		case err = <-waited:
			done = true
		case <-timeout:
			m.terminate(e, "timeout")
			timeout = nil
		}
	}
	r := Run{Trigger: e.trigger, Started: e.started, DurationMs: time.Since(e.started).Milliseconds()}
	if r.DurationMs == 0 {
		r.DurationMs = 1
	}
	if st := e.cmd.ProcessState; st != nil && st.Exited() {
		code := st.ExitCode()
		r.Exit = &code
	}
	e.mu.Lock()
	reason := e.reason
	e.mu.Unlock()
	switch {
	case reason != "":
		r.Outcome = reason
	case r.Exit != nil && *r.Exit == 0:
		r.Outcome = "ok"
	case r.Exit != nil:
		r.Outcome = "failed"
	default:
		r.Outcome = "failed"
		if err != nil {
			r.Error = err.Error()
		}
	}
	r.Output, r.Truncated = e.out.tail(keepOutput)
	e.result = r
	m.record(e, volume)
}

// record ends a run: kept on the volume, shown in the list, reported when
// an automatic run did not succeed.
func (m *Manager) record(e *execution, volume string) {
	r := e.result
	root, err := m.open(volume)
	if err == nil {
		err = m.saveRun(root, e.id, r)
		root.Close()
	}
	if err != nil {
		slog.Warn("scripts: run not recorded", "script", e.name, "err", err)
	}
	e.out.shrink(keepOutput)
	m.mu.Lock()
	delete(m.running, e.id)
	if m.loaded == volume && slices.ContainsFunc(m.scripts, func(s Script) bool { return s.ID == e.id }) {
		short := r
		short.Output = ""
		m.last[e.id] = short
		m.finished[e.id] = e
	}
	m.mu.Unlock()
	close(e.done)
	slog.Info("script ended", "script", e.name, "trigger", e.trigger, "outcome", r.Outcome)
	if e.trigger != Manual && r.Outcome != "ok" && r.Outcome != "canceled" && m.Notify != nil {
		m.Notify("error", "script_failed", e.name)
	}
}

// saveRun puts a run first in the kept runs, replacing the entry of the
// same run while it was running.
func (m *Manager) saveRun(root *os.Root, id string, r Run) error {
	m.io.Lock()
	defer m.io.Unlock()
	runs, err := readRuns(root, id)
	if err != nil {
		runs = []Run{}
	}
	runs = slices.DeleteFunc(runs, func(old Run) bool { return old.Outcome == "running" && old.Started.Equal(r.Started) })
	runs = append([]Run{r}, runs...)
	if len(runs) > keepRuns {
		runs = runs[:keepRuns]
	}
	if _, err := root.Lstat(id); err != nil {
		return err
	}
	return volumedata.WriteJSON(root, id+"/runs.json", runs)
}

// runAll runs the scripts of a trigger one after the other.
func (m *Manager) runAll(ctx context.Context, trigger string, limit time.Duration, progress func(name, state string, err error)) {
	if progress == nil {
		progress = func(string, string, error) {}
	}
	m.mu.Lock()
	var list []Script
	for _, s := range m.scripts {
		if s.Trigger == trigger {
			list = append(list, s)
		}
	}
	m.mu.Unlock()
	for _, s := range list {
		if ctx.Err() != nil {
			return
		}
		progress(s.Name, "running", nil)
		e, err := m.start(s.ID, trigger, limit)
		if err != nil {
			progress(s.Name, "failed", err)
			continue
		}
		select {
		case <-e.done:
		case <-ctx.Done():
			m.terminate(e, "canceled")
			<-e.done
		}
		if e.result.Outcome == "ok" {
			progress(s.Name, "stopped", nil)
		} else {
			progress(s.Name, "failed", fmt.Errorf("%s", e.result.Outcome))
		}
	}
}

// RunShutdown runs the shutdown scripts one after the other, each for at
// most ShutdownLimit, before the guests end and the volumes go. Progress
// reports each with running, stopped (done) or failed.
func (m *Manager) RunShutdown(ctx context.Context, progress func(name, state string, err error)) {
	if m.Disabled || m.ensure() != nil {
		return
	}
	m.runAll(ctx, Shutdown, m.ShutdownLimit, progress)
}

func bootID() string {
	data, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return strings.TrimSpace(string(data))
}

// startOnce tells whether the start scripts are still due in this boot
// and remembers that they are not any more.
func (m *Manager) startOnce() bool {
	m.mu.Lock()
	if m.startDone {
		m.mu.Unlock()
		return false
	}
	m.startDone = true
	m.mu.Unlock()
	if m.StartMarker == "" {
		return true
	}
	id := m.BootID
	if id == "" {
		id = bootID()
	}
	if data, err := os.ReadFile(m.StartMarker); err == nil && strings.TrimSpace(string(data)) == id {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(m.StartMarker), 0o755); err == nil {
		if err := os.WriteFile(m.StartMarker, []byte(id+"\n"), 0o600); err != nil {
			slog.Warn("scripts: start marker not written", "err", err)
		}
	}
	return true
}

// Tick runs what is due: the start scripts once the volume is mounted, the
// scheduled scripts of every minute since the last tick. It touches the
// volume only when something runs (or the first time after a mount).
func (m *Manager) Tick(ctx context.Context, now time.Time) {
	err := m.ensure()
	m.mu.Lock()
	chosen := m.volume
	m.mu.Unlock()
	if err != nil {
		m.mu.Lock()
		report := chosen != "" && !m.missing && now.Sub(m.started) >= m.MissingGrace
		if report {
			m.missing = true
		}
		m.lastTick = time.Time{}
		m.mu.Unlock()
		if report && m.Notify != nil {
			m.Notify("warning", "script_volume_missing", chosen)
		}
		return
	}
	m.mu.Lock()
	m.missing = false
	m.mu.Unlock()
	if m.Disabled {
		return
	}
	if m.startOnce() {
		go m.runAll(ctx, Start, 0, nil)
	}
	loc := m.location()
	minute := now.In(loc).Truncate(time.Minute)
	m.mu.Lock()
	from := m.lastTick
	m.lastTick = minute
	var due []Script
	for _, s := range m.scripts {
		if s.Trigger == Scheduled && s.Schedule != nil {
			due = append(due, s)
		}
	}
	m.mu.Unlock()
	// The minutes since the last tick, at most ten back; none caught up
	// from before titand started or the volume came.
	first := minute
	if !from.IsZero() {
		first = from.Add(time.Minute)
		if minute.Sub(first) > 10*time.Minute {
			first = minute.Add(-10 * time.Minute)
		}
	}
	for _, s := range due {
		expr, err := s.Schedule.Expression()
		if err != nil {
			continue
		}
		c, err := parseCron(expr)
		if err != nil {
			continue
		}
		for at := first; !at.After(minute); at = at.Add(time.Minute) {
			if c.due(at) {
				if _, err := m.start(s.ID, Scheduled, 0); err != nil && !errors.Is(err, ErrRunning) {
					slog.Warn("scheduled script not started", "script", s.Name, "err", err)
				} else if errors.Is(err, ErrRunning) {
					slog.Info("scheduled script skipped: still running", "script", s.Name)
				}
				break
			}
		}
	}
}

// Loop ticks every 20 seconds until ctx ends; then running scripts are
// ended.
func (m *Manager) Loop(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	m.Tick(ctx, time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			m.Tick(ctx, now)
		}
	}
}
