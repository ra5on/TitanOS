// Package replication copies a Btrfs volume to another TitanOS, snapshot by
// snapshot, with btrfs send and receive.
//
// The receiving NAS is the same server as for backups of other TitanOS
// (package backuptarget): it listens only on its Tailscale addresses and
// knows each sending NAS by name and token from a pairing code. Below the
// source's name it answers /_replica/: GET lists the snapshots it holds,
// PUT /_replica/<snapshot> takes a send stream into btrfs receive.
//
// The sending NAS takes a read-only snapshot of the whole volume, finds the
// newest snapshot both sides have and sends only the difference to it. It
// keeps the last two of its own snapshots: the newest is the parent of the
// next run. Nested subvolumes (Docker's, for example) are not part of a
// Btrfs snapshot and so not of the replica.
package replication

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/naming"
	"github.com/ra5on/TitanOS/backend/internal/run"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

const (
	settingsFile = "backup/replication.json"
	// codePrefix is the start of a pairing code of package backuptarget.
	codePrefix = "titanos-backup:"
	// Sender snapshots, relative to the volume.
	localDir = ".titanos/replicated"
	// Received snapshots, relative to the receiving volume, one folder per source.
	receivedDir = ".titanos/replicas"
	maxJobs     = 16
)

var (
	ErrNotLoaded = errors.New("replication: settings not loaded from /config")
	ErrInvalid   = errors.New("replication: invalid setting")
	ErrNotFound  = errors.New("replication: no such job")
	ErrBusy      = errors.New("replication: a run is in progress")
	ErrDisabled  = errors.New("replication: not available in development mode")
	ErrNotBtrfs  = errors.New("replication: the volume is not Btrfs")

	snapshotForm = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z$`)
)

// Job sends one volume to one other TitanOS.
type Job struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Volume  string `json:"volume"`
	// URL, User and Token come from the pairing code of the other NAS.
	URL   string `json:"url"`
	User  string `json:"user"`
	Token string `json:"token"`
	// EveryHours is the time between two runs (1 to 168).
	EveryHours int `json:"everyHours"`
	// Keep is how many snapshots the other NAS keeps (2 to 365).
	Keep int `json:"keep"`
	// LimitMiB caps the transfer in MiB/s; 0 is no limit.
	LimitMiB int `json:"limitMiB,omitempty"`
}

// State is what is known about the runs of a job; it lives in RAM.
type State struct {
	Running   bool       `json:"running"`
	Bytes     int64      `json:"bytes"`
	LastRun   *time.Time `json:"lastRun"`
	LastOK    *time.Time `json:"lastOK"`
	LastError string     `json:"lastError"`
	Snapshot  string     `json:"snapshot"`
	Next      *time.Time `json:"next"`
}

// JobView is a job as the web UI sees it: without the token.
type JobView struct {
	Job
	Token string `json:"token,omitempty"`
	State State  `json:"state"`
}

type settings struct {
	Jobs []Job `json:"jobs"`
}

// Manager keeps the jobs and runs them.
type Manager struct {
	store *config.Store
	// Disabled changes nothing on disks: development mode.
	Disabled bool
	// Mounted returns the mounted volumes; Filesystem the file system of one.
	Mounted    func() []string
	Filesystem func(volume string) string
	// OnResult is told when a run ends (err nil on success).
	OnResult func(job Job, err error)
	// Client sends the streams; tests replace it.
	Client    *http.Client
	runner    run.Runner
	mountRoot string
	now       func() time.Time
	// command starts btrfs with a pipe; tests replace it.
	command func(ctx context.Context, args ...string) *exec.Cmd
	// stall gives up a stream that moved no byte for so long; answerWait
	// is how long the other side may take to answer once all is sent
	// (btrfs receive finishing). Tests shorten them.
	stall, answerWait time.Duration

	mu       sync.Mutex
	loaded   bool
	settings settings
	states   map[string]*State
}

// NewManager returns a manager for the real system.
func NewManager(store *config.Store) *Manager {
	return &Manager{
		store: store, runner: run.Exec{}, mountRoot: "/mnt", now: time.Now, states: map[string]*State{},
		// No overall timeout: a first copy at a bandwidth limit may run for
		// days. stream gives up on a connection that stands still instead.
		Client: &http.Client{Timeout: 0},
		stall:  5 * time.Minute, answerWait: 30 * time.Minute,
		command: func(ctx context.Context, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "btrfs", args...)
		},
	}
}

// Load reads the jobs from /config.
func (m *Manager) Load() error {
	var s settings
	if err := m.store.ReadJSON(settingsFile, &s); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings, m.loaded = s, true
	return nil
}

// Jobs lists the jobs with their state.
func (m *Manager) Jobs() []JobView {
	m.mu.Lock()
	defer m.mu.Unlock()
	views := []JobView{}
	for _, job := range m.settings.Jobs {
		view := JobView{Job: job}
		view.Job.Token = ""
		if state := m.states[job.ID]; state != nil {
			view.State = *state
		}
		view.State.Next = m.next(job, view.State)
		views = append(views, view)
	}
	return views
}

// Request is a job as the web UI sends it. Code is the pairing code of the
// other NAS; when editing, an empty code keeps the stored address.
type Request struct {
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Volume     string `json:"volume"`
	Code       string `json:"code"`
	EveryHours int    `json:"everyHours"`
	Keep       int    `json:"keep"`
	LimitMiB   int    `json:"limitMiB"`
}

// Save adds a job (id empty) or changes one.
func (m *Manager) Save(id string, req Request) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return "", ErrNotLoaded
	}
	if !naming.ValidBackup(req.Name) || !naming.ValidVolume(req.Volume) {
		return "", fmt.Errorf("%w: name or volume", ErrInvalid)
	}
	if req.EveryHours < 1 || req.EveryHours > 168 || req.Keep < 2 || req.Keep > 365 || req.LimitMiB < 0 || req.LimitMiB > 10000 {
		return "", fmt.Errorf("%w: schedule, keep or limit", ErrInvalid)
	}
	if m.Filesystem != nil && m.Filesystem(req.Volume) != "btrfs" {
		return "", ErrNotBtrfs
	}
	job := Job{Name: req.Name, Enabled: req.Enabled, Volume: req.Volume, EveryHours: req.EveryHours, Keep: req.Keep, LimitMiB: req.LimitMiB}
	jobs := slices.Clone(m.settings.Jobs)
	index := slices.IndexFunc(jobs, func(j Job) bool { return j.ID == id })
	switch {
	case id == "":
		if len(jobs) >= maxJobs {
			return "", fmt.Errorf("%w: too many jobs", ErrInvalid)
		}
		raw := make([]byte, 6)
		rand.Read(raw)
		job.ID = hex.EncodeToString(raw)
	case index < 0:
		return "", ErrNotFound
	default:
		if state := m.states[id]; state != nil && state.Running {
			return "", ErrBusy
		}
		job.ID = id
		job.URL, job.User, job.Token = jobs[index].URL, jobs[index].User, jobs[index].Token
	}
	if req.Code != "" || id == "" {
		target, user, token, err := parseCode(req.Code)
		if err != nil {
			return "", err
		}
		job.URL, job.User, job.Token = target, user, token
	}
	if slices.ContainsFunc(jobs, func(j Job) bool { return j.ID != job.ID && strings.EqualFold(j.Name, job.Name) }) {
		return "", fmt.Errorf("%w: name taken", ErrInvalid)
	}
	if index < 0 {
		jobs = append(jobs, job)
	} else {
		jobs[index] = job
	}
	if err := m.store.WriteJSON(settingsFile, settings{Jobs: jobs}); err != nil {
		return "", err
	}
	m.settings.Jobs = jobs
	return job.ID, nil
}

// Remove forgets a job. Snapshots on both sides stay; the local ones of
// the job are removed on the next look at the volume by hand only.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	if state := m.states[id]; state != nil && state.Running {
		return ErrBusy
	}
	jobs := slices.DeleteFunc(slices.Clone(m.settings.Jobs), func(j Job) bool { return j.ID == id })
	if len(jobs) == len(m.settings.Jobs) {
		return ErrNotFound
	}
	if err := m.store.WriteJSON(settingsFile, settings{Jobs: jobs}); err != nil {
		return err
	}
	m.settings.Jobs = jobs
	delete(m.states, id)
	return nil
}

// parseCode reads a pairing code of package backuptarget.
func parseCode(code string) (target, user, token string, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(strings.TrimSpace(code), codePrefix))
	if !strings.HasPrefix(strings.TrimSpace(code), codePrefix) || err != nil {
		return "", "", "", fmt.Errorf("%w: pairing code", ErrInvalid)
	}
	var payload struct {
		URL      string `json:"url"`
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return "", "", "", fmt.Errorf("%w: pairing code", ErrInvalid)
	}
	parsed, err := url.Parse(payload.URL)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || payload.User == "" || payload.Password == "" {
		return "", "", "", fmt.Errorf("%w: pairing code", ErrInvalid)
	}
	return strings.TrimRight(payload.URL, "/"), payload.User, payload.Password, nil
}

func (m *Manager) next(job Job, state State) *time.Time {
	if !job.Enabled {
		return nil
	}
	next := m.now()
	if state.LastRun != nil {
		next = state.LastRun.Add(time.Duration(job.EveryHours) * time.Hour)
		// A failed run is tried again after half an hour.
		if state.LastError != "" && (state.LastOK == nil || state.LastOK.Before(*state.LastRun)) {
			if retry := state.LastRun.Add(30 * time.Minute); retry.Before(next) {
				next = retry
			}
		}
	}
	return &next
}

// Busy reports whether a run is in progress: app updates wait for it.
func (m *Manager) Busy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, state := range m.states {
		if state.Running {
			return true
		}
	}
	return false
}

// Run starts due jobs every minute until ctx is done.
func (m *Manager) Run(ctx context.Context) {
	if m.Disabled {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		if !m.loaded {
			m.mu.Unlock()
			m.Load()
			m.mu.Lock()
		}
		var due []string
		for _, job := range m.settings.Jobs {
			state := State{}
			if s := m.states[job.ID]; s != nil {
				state = *s
			}
			if next := m.next(job, state); next != nil && !state.Running && !next.After(m.now()) && m.mounted(job.Volume) {
				due = append(due, job.ID)
			}
		}
		m.mu.Unlock()
		for _, id := range due {
			m.Start(ctx, id)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) mounted(volume string) bool {
	return m.Mounted == nil || slices.Contains(m.Mounted(), volume)
}

// Start runs a job in the background now.
func (m *Manager) Start(ctx context.Context, id string) error {
	if m.Disabled {
		return ErrDisabled
	}
	m.mu.Lock()
	index := slices.IndexFunc(m.settings.Jobs, func(j Job) bool { return j.ID == id })
	if index < 0 {
		m.mu.Unlock()
		return ErrNotFound
	}
	job := m.settings.Jobs[index]
	state := m.states[id]
	if state == nil {
		state = &State{}
		m.states[id] = state
	}
	if state.Running {
		m.mu.Unlock()
		return ErrBusy
	}
	started := m.now().UTC()
	state.Running, state.Bytes, state.LastRun = true, 0, &started
	m.mu.Unlock()
	go func() {
		snapshot, err := m.send(context.WithoutCancel(ctx), job, func(n int64) {
			m.mu.Lock()
			state.Bytes = n
			m.mu.Unlock()
		})
		m.mu.Lock()
		state.Running = false
		if err != nil {
			state.LastError = err.Error()
			slog.Warn("replication failed", "job", job.Name, "err", err)
		} else {
			done := m.now().UTC()
			state.LastOK, state.LastError, state.Snapshot = &done, "", snapshot
			slog.Info("replication done", "job", job.Name, "snapshot", snapshot, "bytes", state.Bytes)
		}
		m.mu.Unlock()
		if m.OnResult != nil {
			m.OnResult(job, err)
		}
	}()
	return nil
}

// send takes a snapshot of the volume and sends it, as difference to the
// newest snapshot the other side has.
func (m *Manager) send(ctx context.Context, job Job, progress func(int64)) (string, error) {
	if !m.mounted(job.Volume) {
		return "", errors.New("volume not mounted")
	}
	root, err := volumedata.Open(m.mountRoot, job.Volume, "replicated", m.mountedList())
	if err != nil {
		return "", err
	}
	root.Close()
	point := filepath.Join(m.mountRoot, job.Volume)
	dir := filepath.Join(point, localDir, job.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	remote, err := m.remoteSnapshots(ctx, job)
	if err != nil {
		return "", fmt.Errorf("other NAS: %w", err)
	}
	local := localSnapshots(dir)
	name := m.now().UTC().Format("20060102T150405Z")
	if slices.Contains(local, name) {
		return "", ErrBusy
	}
	if _, err := btrfs(ctx, m.runner, "subvolume", "snapshot", "-r", point, filepath.Join(dir, name)); err != nil {
		return "", err
	}
	parent := ""
	for i := len(local) - 1; i >= 0; i-- {
		if slices.Contains(remote, local[i]) {
			parent = local[i]
			break
		}
	}
	args := []string{"send", "-q"}
	if parent != "" {
		args = append(args, "-p", filepath.Join(dir, parent))
	}
	args = append(args, filepath.Join(dir, name))
	if err := m.stream(ctx, job, name, args, progress); err != nil {
		// The new snapshot is useless without its copy on the other side.
		btrfs(context.WithoutCancel(ctx), m.runner, "subvolume", "delete", filepath.Join(dir, name))
		return "", err
	}
	// Keep the snapshot just sent (the next parent) and the one before.
	local = append(local, name)
	for _, old := range local[:max(0, len(local)-2)] {
		if _, err := btrfs(ctx, m.runner, "subvolume", "delete", filepath.Join(dir, old)); err != nil {
			slog.Warn("old replication snapshot not deleted", "snapshot", old, "err", err)
		}
	}
	return name, nil
}

func (m *Manager) mountedList() []string {
	if m.Mounted == nil {
		return nil
	}
	return m.Mounted()
}

func localSnapshots(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && snapshotForm.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

func (m *Manager) remoteSnapshots(ctx context.Context, job Job) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, job.URL+"/_replica/", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(job.User, job.Token)
	client := *m.Client
	client.Timeout = time.Minute
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, fmt.Errorf("%s: %s", res.Status, strings.TrimSpace(string(body)))
	}
	var list struct {
		Snapshots []string `json:"snapshots"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&list); err != nil {
		return nil, err
	}
	return list.Snapshots, nil
}

// stream pipes btrfs send into a PUT to the other NAS.
//
// Whatever ends the request early (an error, an answer before the stream
// was taken in full, a connection that stands still) cancels the context:
// that kills btrfs send, which would otherwise block for ever on a pipe
// nobody reads, and cmd.Wait with it.
func (m *Manager) stream(ctx context.Context, job Job, name string, args []string, progress func(int64)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stalled := false
	var stallMu sync.Mutex
	watchdog := time.AfterFunc(m.stall, func() {
		stallMu.Lock()
		stalled = true
		stallMu.Unlock()
		cancel()
	})
	defer watchdog.Stop()
	cmd := m.command(ctx, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	// finish ends btrfs send after an early end of the request.
	finish := func(err error) error {
		cancel()
		stdout.Close()
		cmd.Wait()
		stallMu.Lock()
		defer stallMu.Unlock()
		if stalled {
			return fmt.Errorf("the transfer stood still for %v: %w", m.stall, err)
		}
		return err
	}
	body := &counter{r: stdout, closer: stdout, limit: int64(job.LimitMiB) << 20, start: time.Now(), progress: progress,
		activity: func(sent bool) {
			if sent {
				watchdog.Reset(m.answerWait)
			} else {
				watchdog.Reset(m.stall)
			}
		}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("%s/_replica/%s?keep=%d", job.URL, name, job.Keep), body)
	if err != nil {
		return finish(err)
	}
	req.SetBasicAuth(job.User, job.Token)
	req.Header.Set("Content-Type", "application/octet-stream")
	res, err := m.Client.Do(req)
	if err != nil {
		return finish(err)
	}
	defer res.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(res.Body, 512))
	if res.StatusCode != http.StatusCreated {
		// Answered before or without taking the stream: btrfs send may
		// still be writing into the pipe.
		return finish(fmt.Errorf("other NAS: %s: %s", res.Status, strings.TrimSpace(string(answer))))
	}
	if waitErr := cmd.Wait(); waitErr != nil {
		return fmt.Errorf("btrfs send: %v: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// btrfsTimeout bounds snapshot and delete commands: they take seconds,
// and one hanging on a broken disk must not keep a job running for ever.
const btrfsTimeout = 10 * time.Minute

func btrfs(ctx context.Context, runner run.Runner, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, btrfsTimeout)
	defer cancel()
	return runner.Run(ctx, "", "btrfs", args...)
}

// counter counts what is read and slows down to the limit (bytes per
// second). activity is told of every read, with true once all was read;
// Close (from the HTTP client, when it stops sending) closes closer.
type counter struct {
	r        io.Reader
	closer   io.Closer
	n        int64
	limit    int64
	start    time.Time
	progress func(int64)
	activity func(sent bool)
}

func (c *counter) Close() error {
	if c.closer == nil {
		return nil
	}
	return c.closer.Close()
}

func (c *counter) Read(p []byte) (int, error) {
	if c.limit > 0 && int64(len(p)) > c.limit/4 {
		p = p[:max(1, c.limit/4)]
	}
	n, err := c.r.Read(p)
	c.n += int64(n)
	if c.progress != nil {
		c.progress(c.n)
	}
	if c.activity != nil && (n > 0 || err != nil) {
		c.activity(err == io.EOF)
	}
	if c.limit > 0 {
		if ahead := time.Duration(float64(c.n)/float64(c.limit)*float64(time.Second)) - time.Since(c.start); ahead > 0 {
			time.Sleep(ahead)
		}
	}
	return n, err
}

// --- receiving -------------------------------------------------------------

// Receiver answers /_replica/ for a source of package backuptarget.
type Receiver struct {
	MountRoot  string
	Mounted    func() []string
	Filesystem func(volume string) string
	runner     run.Runner
	command    func(ctx context.Context, args ...string) *exec.Cmd
	mu         sync.Mutex
	busy       map[string]bool
}

// NewReceiver returns a receiver for the real system.
func NewReceiver() *Receiver {
	return &Receiver{MountRoot: "/mnt", runner: run.Exec{}, busy: map[string]bool{},
		command: func(ctx context.Context, args ...string) *exec.Cmd { return exec.CommandContext(ctx, "btrfs", args...) }}
}

// Serve handles rest (the path after /_replica/) for source on volume.
func (r *Receiver) Serve(w http.ResponseWriter, req *http.Request, source, volume, rest string) {
	if r.Filesystem != nil && r.Filesystem(volume) != "btrfs" {
		http.Error(w, "the receiving volume is not Btrfs", http.StatusConflict)
		return
	}
	var mounted []string
	if r.Mounted != nil {
		mounted = r.Mounted()
	}
	root, err := volumedata.Open(r.MountRoot, volume, "replicas", mounted)
	if err != nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	root.Close()
	dir := filepath.Join(r.MountRoot, volume, receivedDir, source)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	switch {
	case rest == "" && req.Method == http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"snapshots": received(dir)})
	case snapshotForm.MatchString(rest) && req.Method == http.MethodPut:
		r.receive(w, req, source, dir, rest)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// received lists the complete snapshots of a source, oldest first.
func received(dir string) []string {
	names := localSnapshots(dir)
	if names == nil {
		names = []string{}
	}
	return names
}

func (r *Receiver) receive(w http.ResponseWriter, req *http.Request, source, dir, name string) {
	r.mu.Lock()
	if r.busy[source] {
		r.mu.Unlock()
		http.Error(w, "a transfer of this NAS is running", http.StatusConflict)
		return
	}
	r.busy[source] = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.busy, source)
		r.mu.Unlock()
	}()
	if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
		http.Error(w, "snapshot exists", http.StatusConflict)
		return
	}
	// btrfs receive names the new subvolume after the sent one. It lands in
	// a folder of its own first, so that a broken transfer never looks like
	// a complete snapshot.
	incoming := filepath.Join(dir, ".incoming")
	if leftovers, _ := os.ReadDir(incoming); len(leftovers) > 0 {
		for _, entry := range leftovers {
			btrfs(req.Context(), r.runner, "subvolume", "delete", filepath.Join(incoming, entry.Name()))
		}
	}
	if err := os.MkdirAll(incoming, 0o700); err != nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	cmd := r.command(req.Context(), "receive", "-e", incoming)
	cmd.Stdin = req.Body
	output, err := cmd.CombinedOutput()
	if err != nil {
		btrfs(context.WithoutCancel(req.Context()), r.runner, "subvolume", "delete", filepath.Join(incoming, name))
		slog.Warn("replica not received", "source", source, "snapshot", name, "err", err, "output", strings.TrimSpace(string(output)))
		http.Error(w, "receive failed", http.StatusInternalServerError)
		return
	}
	if err := os.Rename(filepath.Join(incoming, name), filepath.Join(dir, name)); err != nil {
		http.Error(w, "receive failed", http.StatusInternalServerError)
		return
	}
	slog.Info("replica received", "source", source, "snapshot", name)
	// Old snapshots beyond keep go; the newest stays the parent of the next.
	keep := 7
	fmt.Sscan(req.URL.Query().Get("keep"), &keep)
	keep = min(max(keep, 2), 365)
	all := received(dir)
	for _, old := range all[:max(0, len(all)-keep)] {
		btrfs(context.WithoutCancel(req.Context()), r.runner, "subvolume", "delete", filepath.Join(dir, old))
	}
	w.WriteHeader(http.StatusCreated)
}

// Replica is a received snapshot as the receiving NAS shows it.
type Replica struct {
	Source   string `json:"source"`
	Snapshot string `json:"snapshot"`
}

// List shows the snapshots received on volume, newest first.
func (r *Receiver) List(volume string) []Replica {
	base := filepath.Join(r.MountRoot, volume, receivedDir)
	sources, _ := os.ReadDir(base)
	list := []Replica{}
	for _, source := range sources {
		if !source.IsDir() || !naming.ValidBackup(source.Name()) {
			continue
		}
		for _, name := range received(filepath.Join(base, source.Name())) {
			list = append(list, Replica{Source: source.Name(), Snapshot: name})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Snapshot > list[j].Snapshot })
	return list
}

// Restore makes a writable copy of a received snapshot in a new folder
// /mnt/<volume>/restored/<source>-<snapshot> and returns its path. The
// replica itself stays as it is.
func (r *Receiver) Restore(ctx context.Context, volume, source, snapshot string) (string, error) {
	if !naming.ValidBackup(source) || !snapshotForm.MatchString(snapshot) {
		return "", ErrInvalid
	}
	var mounted []string
	if r.Mounted != nil {
		mounted = r.Mounted()
	}
	root, err := volumedata.OpenVolume(r.MountRoot, volume, mounted)
	if err != nil {
		return "", err
	}
	root.Close()
	from := filepath.Join(r.MountRoot, volume, receivedDir, source, snapshot)
	if _, err := os.Lstat(from); err != nil {
		return "", ErrNotFound
	}
	restored := filepath.Join(r.MountRoot, volume, "restored")
	if err := os.MkdirAll(restored, 0o700); err != nil {
		return "", err
	}
	target := filepath.Join(restored, source+"-"+snapshot)
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("%w: %s exists", ErrInvalid, target)
	}
	if _, err := btrfs(ctx, r.runner, "subvolume", "snapshot", from, target); err != nil {
		return "", err
	}
	return target, nil
}
