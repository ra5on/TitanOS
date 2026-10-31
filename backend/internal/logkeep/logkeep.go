// Package logkeep keeps the journal beyond a restart. The journal itself
// lives in RAM (the root file system does); logkeep copies it as text to
// .titanos/logs on a data volume, one file per start, never to the stick.
//
// The file is opened, appended to and closed again for every write, so it
// never keeps a volume from being unmounted. To let sleeping disks sleep,
// lines are written every 30 minutes, at once when a warning or error is
// reported (Urgent), and when titand stops. What happened in the last
// minutes before a power cut can be lost; warnings and errors come first.
//
// A marker says that a start is running. If a later start finds the marker
// of an earlier one, that start did not end cleanly: power cut, crash or
// reset. Kernel crash records (pstore) are kept with it.
package logkeep

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/run"
)

const (
	// Dir is the folder on a volume, below its mount point.
	Dir = ".titanos/logs"
	// interval between two writes without anything urgent.
	interval = 30 * time.Minute
	// debounce gathers an urgent burst into one write.
	debounce = 30 * time.Second
	// maxBytes is what the kept starts may take together.
	maxBytes = 1 << 30
	// maxStarts is how many starts are kept at most.
	maxStarts = 50
	marker    = "running"
)

// Unclean describes a start that did not end cleanly.
type Unclean struct {
	Boot    string    `json:"boot"`
	Started time.Time `json:"started"`
	// LastLine is the time of the last line kept from that start.
	LastLine time.Time `json:"lastLine"`
	// Crash says that the kernel left crash records (pstore).
	Crash bool `json:"crash"`
}

// Manager copies the journal to a volume.
type Manager struct {
	runner run.Runner
	// Target returns the mount point of the volume for the logs; false
	// while there is none (no volume mounted).
	Target func() (string, bool)
	// OnUnclean is told once when an earlier start did not end cleanly.
	OnUnclean func(Unclean)
	// pstore is where systemd-pstore keeps kernel crash records.
	pstore string
	boot   string
	now    func() time.Time

	mu      sync.Mutex
	cursor  string
	dir     string
	started time.Time
	last    time.Time
	urgent  chan struct{}
	checked bool
}

// New returns a manager for the running start.
func New(runner run.Runner) *Manager {
	boot, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return &Manager{runner: runner, pstore: "/var/lib/systemd/pstore", boot: strings.TrimSpace(string(boot)), now: time.Now, urgent: make(chan struct{}, 1)}
}

// Urgent asks for a write soon, for a warning or an error.
func (m *Manager) Urgent() {
	select {
	case m.urgent <- struct{}{}:
	default:
	}
}

// Run writes until ctx ends, then a last time and marks the start clean.
func (m *Manager) Run(ctx context.Context) {
	m.mu.Lock()
	m.started = m.now()
	m.mu.Unlock()
	m.flush(context.Background())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	// Volumes can be mounted later: look again every two minutes until
	// there is a folder.
	look := time.NewTicker(2 * time.Minute)
	defer look.Stop()
	for {
		select {
		case <-ctx.Done():
			final, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			m.flush(final)
			m.finish()
			cancel()
			return
		case <-ticker.C:
			m.flush(ctx)
		case <-look.C:
			m.mu.Lock()
			ready := m.dir != ""
			m.mu.Unlock()
			if !ready {
				m.flush(ctx)
			}
		case <-m.urgent:
			select {
			case <-time.After(debounce):
			case <-ctx.Done():
			}
			m.flush(ctx)
		}
	}
}

// folder finds the folder and, the first time, checks the marker of the
// earlier start. Called with m.mu held.
func (m *Manager) folderLocked() (string, bool) {
	if m.Target == nil {
		return "", false
	}
	base, ok := m.Target()
	if !ok {
		return "", false
	}
	dir := filepath.Join(base, Dir)
	if dir != m.dir {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			slog.Warn("folder for kept logs not usable", "dir", dir, "err", err)
			return "", false
		}
		m.dir = dir
		m.checked = false
	}
	if !m.checked {
		m.checked = true
		m.checkMarkerLocked()
	}
	return dir, true
}

// checkMarkerLocked reports an earlier start that is still marked as
// running, keeps its crash records, and marks this start.
func (m *Manager) checkMarkerLocked() {
	path := filepath.Join(m.dir, marker)
	data, err := os.ReadFile(path)
	if err == nil {
		boot, started, _ := strings.Cut(strings.TrimSpace(string(data)), " ")
		if boot != "" && boot != m.boot {
			u := Unclean{Boot: boot}
			if t, err := time.Parse(time.RFC3339, started); err == nil {
				u.Started = t
			}
			if info, err := os.Stat(filepath.Join(m.dir, boot+".log")); err == nil {
				u.LastLine = info.ModTime()
			}
			u.Crash = m.keepCrashLocked(boot)
			slog.Warn("the earlier start did not end cleanly", "boot", boot, "crash", u.Crash)
			if m.OnUnclean != nil {
				go m.OnUnclean(u)
			}
		}
	}
	content := fmt.Sprintf("%s %s\n", m.boot, m.started.UTC().Format(time.RFC3339))
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		slog.Warn("start marker not written", "err", err)
	}
}

// keepCrashLocked copies kernel crash records next to the log of the
// start they belong to (the one before this).
func (m *Manager) keepCrashLocked(boot string) bool {
	entries, err := os.ReadDir(m.pstore)
	if err != nil || len(entries) == 0 {
		return false
	}
	dst := filepath.Join(m.dir, boot+"-crash")
	found := false
	_ = filepath.WalkDir(m.pstore, func(p string, d fs.DirEntry, err error) error { // the callback collects what it can; unreadable entries are skipped
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 16<<20 {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(m.pstore, p)
		target := filepath.Join(dst, strings.ReplaceAll(rel, string(filepath.Separator), "_"))
		if os.MkdirAll(dst, 0o700) == nil && os.WriteFile(target, data, 0o600) == nil {
			found = true
		}
		return nil
	})
	return found
}

// flush appends the lines since the last write.
func (m *Manager) flush(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dir, ok := m.folderLocked()
	if !ok {
		return
	}
	args := []string{"--no-pager", "-o", "short-iso", "--show-cursor", "-b", "0"}
	if m.cursor != "" {
		args = append(args, "--after-cursor="+m.cursor)
	}
	out, err := m.runner.Run(ctx, "", "journalctl", args...)
	if err != nil {
		slog.Warn("journal not read for keeping", "err", err)
		return
	}
	text, cursor := splitCursor(out)
	if cursor != "" {
		m.cursor = cursor
	}
	if strings.TrimSpace(text) != "" {
		f, err := os.OpenFile(filepath.Join(dir, m.boot+".log"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
		if err != nil {
			slog.Warn("kept log not written", "err", err)
			return
		}
		_, werr := f.WriteString(text)
		cerr := f.Close()
		if err := errors.Join(werr, cerr); err != nil {
			slog.Warn("kept log not written", "err", err)
			return
		}
		m.last = m.now()
	}
	prune(dir, m.boot)
}

// finish marks this start as cleanly ended.
func (m *Manager) finish() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dir == "" {
		return
	}
	if err := os.Remove(filepath.Join(m.dir, marker)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("start marker not removed", "err", err)
	}
}

// splitCursor separates the "-- cursor: …" line journalctl adds.
func splitCursor(out string) (string, string) {
	lines := strings.Split(out, "\n")
	var keep []string
	cursor := ""
	for _, line := range lines {
		if c, ok := strings.CutPrefix(line, "-- cursor: "); ok {
			cursor = strings.TrimSpace(c)
			continue
		}
		if line == "-- No entries --" {
			continue
		}
		keep = append(keep, line)
	}
	text := strings.Join(keep, "\n")
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text, cursor
}

// prune keeps the newest starts within the limits; the current one stays.
func prune(dir, current string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type kept struct {
		name string
		mod  time.Time
		size int64
	}
	var logs []kept
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		logs = append(logs, kept{e.Name(), info.ModTime(), info.Size()})
	}
	sort.Slice(logs, func(i, j int) bool { return logs[i].mod.After(logs[j].mod) })
	var total int64
	for i, l := range logs {
		total += l.size
		if l.name == current+".log" || i < maxStarts && total <= maxBytes {
			continue
		}
		boot := strings.TrimSuffix(l.name, ".log")
		os.Remove(filepath.Join(dir, l.name))
		os.RemoveAll(filepath.Join(dir, boot+"-crash"))
	}
}

// Start is a kept start for the web UI.
type Start struct {
	Boot    string    `json:"boot"`
	Last    time.Time `json:"last"`
	Size    int64     `json:"size"`
	Current bool      `json:"current"`
	Crash   bool      `json:"crash"`
}

// Starts lists the kept starts, newest first.
func (m *Manager) Starts() []Start {
	m.mu.Lock()
	dir, current := m.dir, m.boot
	m.mu.Unlock()
	list := []Start{}
	if dir == "" {
		return list
	}
	entries, _ := os.ReadDir(dir)
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	for _, e := range entries {
		boot, ok := strings.CutSuffix(e.Name(), ".log")
		if !ok || e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		list = append(list, Start{Boot: boot, Last: info.ModTime(), Size: info.Size(), Current: boot == current, Crash: names[boot+"-crash"]})
	}
	slices.SortFunc(list, func(a, b Start) int { return b.Last.Compare(a.Last) })
	return list
}

// ErrNoStart means there is no kept log of that start.
var ErrNoStart = errors.New("logkeep: no kept log of that start")

// Previous returns the kept log of the start before this one.
func (m *Manager) Previous() (string, error) {
	for _, s := range m.Starts() {
		if !s.Current {
			m.mu.Lock()
			dir := m.dir
			m.mu.Unlock()
			data, err := os.ReadFile(filepath.Join(dir, s.Boot+".log"))
			if err != nil {
				return "", err
			}
			return string(data), nil
		}
	}
	return "", ErrNoStart
}
