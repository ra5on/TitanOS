// Package scripts runs the person's own shell scripts on the NAS (Tools ›
// Scripts, owner decision of 4 Nov 2026): by hand with live output, on a
// schedule, once at start when the data volumes are mounted and at
// shutdown before they are unmounted.
//
// The scripts live on a data volume under .titanos/scripts, never on the
// stick; the stick only remembers which volume (system/scripts.json),
// written when the person chooses it. The list of scripts is read from the
// volume once when it is mounted and kept in memory, so the scheduler
// looks at nothing on the disks until a script is actually due.
//
// Scripts run as root. They are reachable only through the web session of
// an owner or administrator, never through API tokens or MCP.
package scripts

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/naming"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

const (
	settingsFile = "system/scripts.json"
	feature      = "scripts"
	indexFile    = "index.json"
	// MaxContent bounds one script (256 KiB).
	MaxContent = 256 << 10
	// keepRuns is how many runs per script are kept on the volume.
	keepRuns = 10
	// keepOutput is how much output of a run is kept on the volume (the end).
	keepOutput = 64 << 10
	// liveOutput is how much output of a running script is kept in memory.
	liveOutput = 1 << 20
	maxScripts = 200
)

// Triggers.
const (
	Manual    = "manual"
	Scheduled = "schedule"
	Start     = "start"
	Shutdown  = "shutdown"
)

var (
	ErrNotFound    = errors.New("scripts: no such script")
	ErrRunning     = errors.New("scripts: the script is already running")
	ErrNotRunning  = errors.New("scripts: the script is not running")
	ErrUnavailable = errors.New("scripts: the volume of the scripts is not available")
	ErrDisabled    = errors.New("scripts: scripts do not run in development mode")
	ErrInvalid     = errors.New("scripts: invalid script")
	ErrVolume      = errors.New("scripts: choose a mounted data volume")
	ErrTooMany     = errors.New("scripts: too many scripts")
)

// Script is one script, without its content.
type Script struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Trigger     string    `json:"trigger"`
	Schedule    *Schedule `json:"schedule,omitempty"`
	Changed     time.Time `json:"changed"`
}

// Run is one run of a script.
type Run struct {
	Trigger string    `json:"trigger"`
	Started time.Time `json:"started"`
	// DurationMs is 0 while running.
	DurationMs int64 `json:"durationMs"`
	// Exit is the exit code, nil when the script did not end by itself.
	Exit *int `json:"exit,omitempty"`
	// Outcome: running, ok, failed (exit ≠ 0), timeout, canceled,
	// error (could not start) or interrupted (titand ended during the run).
	Outcome string `json:"outcome"`
	Error   string `json:"error,omitempty"`
	Output  string `json:"output,omitempty"`
	// Truncated: only the end of the output is kept.
	Truncated bool `json:"truncated,omitempty"`
}

// View is a script as the list shows it.
type View struct {
	Script
	Running bool       `json:"running"`
	Last    *Run       `json:"last,omitempty"`
	Next    *time.Time `json:"next,omitempty"`
}

// Status is the page.
type Status struct {
	// Volume holds the scripts; Chosen tells whether the person chose it
	// (otherwise it is the default and not stored yet).
	Volume    string `json:"volume"`
	Chosen    bool   `json:"chosen"`
	Available bool   `json:"available"`
	Disabled  bool   `json:"disabled"`
	Error     string `json:"error,omitempty"`
	// Volumes are the mounted data volumes to choose from.
	Volumes []string `json:"volumes"`
	Scripts []View   `json:"scripts"`
}

// Draft is what the person sends to create or change a script.
type Draft struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Trigger     string    `json:"trigger"`
	Schedule    *Schedule `json:"schedule,omitempty"`
	Content     *string   `json:"content,omitempty"`
}

type settings struct {
	Volume string `json:"volume"`
}

type index struct {
	Scripts []Script `json:"scripts"`
}

// Manager owns the scripts and their runs.
type Manager struct {
	store *config.Store
	// MountRoot is /mnt; tests use a folder of their own.
	MountRoot string
	Mounted   func() []string
	// DefaultVolume suggests a volume while none is chosen (the plugin
	// volume, else the first mounted one).
	DefaultVolume func() string
	// Timezone is the zone of the NAS as set now (Go keeps time.Local).
	Timezone func() string
	// Notify reports failed automatic runs and a missing volume.
	Notify func(severity, kind, subject string)
	// Disabled refuses runs (development mode).
	Disabled bool
	// Shell runs scripts without a #! line.
	Shell string
	// ShutdownLimit bounds each shutdown script, CancelGrace the time
	// between SIGTERM and SIGKILL.
	ShutdownLimit, CancelGrace time.Duration
	// StartMarker remembers in /run that the start scripts ran in this
	// boot, so that a restart of titand (app update) does not repeat them.
	StartMarker string
	// MissingGrace is how long the volume may be missing after start
	// before a notification says so.
	MissingGrace time.Duration
	// BootID tells boots apart; empty reads it from the kernel.
	BootID string

	mu        sync.Mutex
	io        sync.Mutex
	volume    string
	loaded    string
	scripts   []Script
	last      map[string]Run
	running   map[string]*execution
	finished  map[string]*execution
	problem   string
	lastTick  time.Time
	started   time.Time
	startDone bool
	missing   bool
	zone      *time.Location
}

// New returns a manager working on the real system.
func New(store *config.Store) *Manager {
	return &Manager{store: store, MountRoot: "/mnt", Shell: "/bin/bash", ShutdownLimit: 60 * time.Second, CancelGrace: 10 * time.Second,
		StartMarker: "/run/titanos/scripts-started", MissingGrace: 10 * time.Minute, last: map[string]Run{}, running: map[string]*execution{}, finished: map[string]*execution{}, started: time.Now()}
}

// Load reads the chosen volume from /config, once at start.
func (m *Manager) Load() error {
	var s settings
	if err := m.store.ReadJSON(settingsFile, &s); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if s.Volume != "" && !naming.ValidVolume(s.Volume) {
		return errors.New("scripts: invalid volume in settings")
	}
	m.mu.Lock()
	m.volume = s.Volume
	m.mu.Unlock()
	return nil
}

func (m *Manager) mounted() []string {
	if m.Mounted == nil {
		return nil
	}
	return m.Mounted()
}

// volumeLocked is the chosen volume or the suggestion.
func (m *Manager) volumeLocked() string {
	if m.volume != "" {
		return m.volume
	}
	if m.DefaultVolume != nil {
		if v := m.DefaultVolume(); v != "" {
			return v
		}
	}
	if list := m.mounted(); len(list) > 0 {
		return list[0]
	}
	return ""
}

func (m *Manager) location() *time.Location {
	if m.Timezone == nil {
		return time.Local
	}
	name := m.Timezone()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.zone != nil && m.zone.String() == name {
		return m.zone
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.Local
	}
	m.zone = loc
	return loc
}

func (m *Manager) open(volume string) (*os.Root, error) {
	root, err := volumedata.Open(m.MountRoot, volume, feature, m.mounted())
	if errors.Is(err, volumedata.ErrUnavailable) {
		return nil, ErrUnavailable
	}
	return root, err
}

// ensure loads the scripts of the current volume into memory, once per
// mount. It answers ErrUnavailable while the volume is not mounted.
func (m *Manager) ensure() error {
	m.io.Lock()
	defer m.io.Unlock()
	m.mu.Lock()
	volume := m.volumeLocked()
	loaded := m.loaded
	m.mu.Unlock()
	if volume == "" || !slices.Contains(m.mounted(), volume) {
		m.mu.Lock()
		if len(m.running) == 0 {
			m.loaded, m.scripts, m.last, m.finished = "", nil, map[string]Run{}, map[string]*execution{}
		}
		m.mu.Unlock()
		return ErrUnavailable
	}
	if loaded == volume {
		return nil
	}
	root, err := m.open(volume)
	if err != nil {
		m.setProblem(err)
		return err
	}
	defer root.Close()
	var idx index
	if err := volumedata.ReadJSON(root, indexFile, &idx, 4<<20); err != nil && !errors.Is(err, fs.ErrNotExist) {
		m.setProblem(err)
		return err
	}
	last := map[string]Run{}
	for _, s := range idx.Scripts {
		runs, err := readRuns(root, s.ID)
		if err != nil {
			slog.Warn("scripts: run log unreadable", "script", s.ID, "err", err)
			continue
		}
		// titand ended during a run: its outcome is unknown.
		changed := false
		for i := range runs {
			if runs[i].Outcome == "running" {
				runs[i].Outcome, changed = "interrupted", true
			}
		}
		if changed {
			if err := volumedata.WriteJSON(root, s.ID+"/runs.json", runs); err != nil {
				slog.Warn("scripts: run log not written", "script", s.ID, "err", err)
			}
		}
		if len(runs) > 0 {
			r := runs[0]
			r.Output = ""
			last[s.ID] = r
		}
	}
	m.mu.Lock()
	m.loaded, m.scripts, m.last, m.problem = volume, idx.Scripts, last, ""
	m.mu.Unlock()
	return nil
}

func (m *Manager) setProblem(err error) {
	m.mu.Lock()
	m.problem = err.Error()
	m.mu.Unlock()
}

func readRuns(root *os.Root, id string) ([]Run, error) {
	var runs []Run
	err := volumedata.ReadJSON(root, id+"/runs.json", &runs, 4<<20)
	if errors.Is(err, fs.ErrNotExist) {
		return []Run{}, nil
	}
	return runs, err
}

// Status lists the scripts from memory; it reads the volume only the first
// time after it was mounted.
func (m *Manager) Status() Status {
	err := m.ensure()
	loc := m.location()
	m.mu.Lock()
	defer m.mu.Unlock()
	out := Status{Volume: m.volumeLocked(), Chosen: m.volume != "", Available: err == nil && m.loaded != "", Disabled: m.Disabled, Error: m.problem, Volumes: append([]string{}, m.mounted()...), Scripts: []View{}}
	if err != nil && !errors.Is(err, ErrUnavailable) {
		out.Error = err.Error()
	}
	now := time.Now().In(loc)
	for _, s := range m.scripts {
		v := View{Script: s, Running: m.running[s.ID] != nil}
		if r, ok := m.last[s.ID]; ok {
			v.Last = &r
		}
		if s.Trigger == Scheduled && s.Schedule != nil {
			if expr, err := s.Schedule.Expression(); err == nil {
				if c, err := parseCron(expr); err == nil {
					v.Next = c.next(now)
				}
			}
		}
		out.Scripts = append(out.Scripts, v)
	}
	return out
}

// SetVolume chooses where the scripts live. Scripts on the old volume stay
// there and come back when it is chosen again.
func (m *Manager) SetVolume(volume string) error {
	if !naming.ValidVolume(volume) || !slices.Contains(m.mounted(), volume) {
		return ErrVolume
	}
	m.io.Lock()
	defer m.io.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.running) > 0 {
		return ErrRunning
	}
	if m.volume == volume {
		return nil
	}
	if err := m.store.WriteJSON(settingsFile, settings{Volume: volume}); err != nil {
		return err
	}
	m.volume, m.loaded, m.scripts, m.last, m.finished, m.problem, m.missing = volume, "", nil, map[string]Run{}, map[string]*execution{}, "", false
	return nil
}

func validName(s string, max int) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > max {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (d Draft) check() error {
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" || !validName(d.Name, 64) || !validName(d.Description, 200) {
		return fmt.Errorf("%w: name or description", ErrInvalid)
	}
	switch d.Trigger {
	case Manual, Start, Shutdown:
	case Scheduled:
		if d.Schedule == nil {
			return fmt.Errorf("%w: schedule missing", ErrInvalid)
		}
		if _, err := d.Schedule.Expression(); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalid, err)
		}
	default:
		return fmt.Errorf("%w: trigger", ErrInvalid)
	}
	if d.Content != nil && (len(*d.Content) > MaxContent || !utf8.ValidString(*d.Content) || strings.ContainsRune(*d.Content, 0)) {
		return fmt.Errorf("%w: content", ErrInvalid)
	}
	return nil
}

func (d Draft) apply(s *Script) {
	s.Name, s.Description, s.Trigger = strings.TrimSpace(d.Name), d.Description, d.Trigger
	s.Schedule = nil
	if d.Trigger == Scheduled {
		sched := *d.Schedule
		if sched.Every == "custom" {
			sched.Cron, _ = sched.Expression()
		} else {
			sched.Cron = ""
		}
		s.Schedule = &sched
	}
	s.Changed = time.Now().UTC().Truncate(time.Second)
}

// writable loads the scripts for a change and pins the volume. The first
// change stores the volume, so that the scripts do not move with a default
// that changes later.
func (m *Manager) writable() (*os.Root, error) {
	if err := m.ensure(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	volume, chosen := m.loaded, m.volume != ""
	m.mu.Unlock()
	if !chosen {
		if err := m.store.WriteJSON(settingsFile, settings{Volume: volume}); err != nil {
			return nil, err
		}
		m.mu.Lock()
		m.volume = volume
		m.mu.Unlock()
	}
	return m.open(volume)
}

func (m *Manager) saveIndexLocked(root *os.Root, list []Script) error {
	return volumedata.WriteJSON(root, indexFile, index{Scripts: list})
}

// Create adds a script.
func (m *Manager) Create(d Draft) (Script, error) {
	if d.Content == nil {
		empty := ""
		d.Content = &empty
	}
	if err := d.check(); err != nil {
		return Script{}, err
	}
	root, err := m.writable()
	if err != nil {
		return Script{}, err
	}
	defer root.Close()
	m.io.Lock()
	defer m.io.Unlock()
	m.mu.Lock()
	list := slices.Clone(m.scripts)
	m.mu.Unlock()
	if len(list) >= maxScripts {
		return Script{}, ErrTooMany
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return Script{}, err
	}
	s := Script{ID: hex.EncodeToString(b[:])}
	d.apply(&s)
	if err := root.Mkdir(s.ID, 0700); err != nil {
		return Script{}, err
	}
	if err := volumedata.Write(root, s.ID+"/script", []byte(*d.Content)); err != nil {
		return Script{}, err
	}
	list = append(list, s)
	if err := m.saveIndexLocked(root, list); err != nil {
		return Script{}, err
	}
	m.mu.Lock()
	m.scripts = list
	m.mu.Unlock()
	return s, nil
}

// Get returns a script with its content.
func (m *Manager) Get(id string) (Script, string, error) {
	if err := m.ensure(); err != nil {
		return Script{}, "", err
	}
	s, ok := m.find(id)
	if !ok {
		return Script{}, "", ErrNotFound
	}
	m.mu.Lock()
	volume := m.loaded
	m.mu.Unlock()
	root, err := m.open(volume)
	if err != nil {
		return Script{}, "", err
	}
	defer root.Close()
	content, err := readContent(root, id)
	return s, content, err
}

func readContent(root *os.Root, id string) (string, error) {
	st, err := root.Lstat(id + "/script")
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() || st.Size() > MaxContent {
		return "", errors.New("scripts: script file is not a plain file")
	}
	f, err := root.Open(id + "/script")
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxContent+1))
	return string(data), err
}

func (m *Manager) find(id string) (Script, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := slices.IndexFunc(m.scripts, func(s Script) bool { return s.ID == id })
	if i < 0 {
		return Script{}, false
	}
	return m.scripts[i], true
}

// Update changes a script. The content changes only when it is sent.
func (m *Manager) Update(id string, d Draft) (Script, error) {
	if err := d.check(); err != nil {
		return Script{}, err
	}
	root, err := m.writable()
	if err != nil {
		return Script{}, err
	}
	defer root.Close()
	m.io.Lock()
	defer m.io.Unlock()
	m.mu.Lock()
	list := slices.Clone(m.scripts)
	i := slices.IndexFunc(list, func(s Script) bool { return s.ID == id })
	m.mu.Unlock()
	if i < 0 {
		return Script{}, ErrNotFound
	}
	before := list[i]
	d.apply(&list[i])
	if d.Content != nil {
		current, err := readContent(root, id)
		if err != nil {
			return Script{}, err
		}
		if current != *d.Content {
			if err := volumedata.Write(root, id+"/script", []byte(*d.Content)); err != nil {
				return Script{}, err
			}
		} else if same(before, list[i]) {
			return before, nil
		}
	} else if same(before, list[i]) {
		return before, nil
	}
	if err := m.saveIndexLocked(root, list); err != nil {
		return Script{}, err
	}
	m.mu.Lock()
	m.scripts = list
	m.mu.Unlock()
	return list[i], nil
}

// same compares everything but the time of the change: nothing is written
// when nothing changed.
func same(a, b Script) bool {
	a.Changed, b.Changed = time.Time{}, time.Time{}
	if (a.Schedule == nil) != (b.Schedule == nil) {
		return false
	}
	if a.Schedule != nil && *a.Schedule != *b.Schedule {
		return false
	}
	a.Schedule, b.Schedule = nil, nil
	return a == b
}

// Delete removes a script with its content and runs.
func (m *Manager) Delete(id string) error {
	root, err := m.writable()
	if err != nil {
		return err
	}
	defer root.Close()
	m.io.Lock()
	defer m.io.Unlock()
	m.mu.Lock()
	if m.running[id] != nil {
		m.mu.Unlock()
		return ErrRunning
	}
	list := slices.Clone(m.scripts)
	m.mu.Unlock()
	i := slices.IndexFunc(list, func(s Script) bool { return s.ID == id })
	if i < 0 {
		return ErrNotFound
	}
	list = slices.Delete(list, i, i+1)
	if err := m.saveIndexLocked(root, list); err != nil {
		return err
	}
	if err := root.RemoveAll(id); err != nil {
		slog.Warn("scripts: folder of a deleted script not removed", "script", id, "err", err)
	}
	m.mu.Lock()
	m.scripts = list
	delete(m.last, id)
	delete(m.finished, id)
	m.mu.Unlock()
	return nil
}

// Runs returns the kept runs of a script, newest first.
func (m *Manager) Runs(id string) ([]Run, error) {
	if err := m.ensure(); err != nil {
		return nil, err
	}
	if _, ok := m.find(id); !ok {
		return nil, ErrNotFound
	}
	m.mu.Lock()
	volume := m.loaded
	m.mu.Unlock()
	root, err := m.open(volume)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	m.io.Lock()
	defer m.io.Unlock()
	return readRuns(root, id)
}

// interpreter reads the #! line: the program and at most one argument, as
// the kernel does. Without one the script runs with the shell.
func interpreter(content io.Reader, shell string) (string, []string) {
	line, _ := bufio.NewReader(io.LimitReader(content, 512)).ReadString('\n')
	rest, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), "#!")
	fields := strings.Fields(rest)
	if !ok || len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return shell, nil
	}
	if len(fields) > 1 {
		return fields[0], []string{strings.Join(fields[1:], " ")}
	}
	return fields[0], nil
}
