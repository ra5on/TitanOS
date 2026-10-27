// Package maintenance owns durable, serial maintenance schedules on a data volume.
package maintenance

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/naming"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

const settingsFile = "system/maintenance.json"

type Job struct {
	DependsOn         string `json:"dependsOn,omitempty"`
	DependencyEnabled *bool  `json:"dependencyEnabled,omitempty"`
	ID                string `json:"id"`
	Name              string `json:"name"`
	Kind              string `json:"kind"`
	Target            string `json:"target"`
	Enabled           bool   `json:"enabled"`
	Every             string `json:"every"`
	Weekdays          []int  `json:"weekdays"`
	MonthDay          int    `json:"monthDay"`
	Time              string `json:"time"`
	Missed            string `json:"missed"`
	ReadData          int    `json:"readData"`
	MaxBytes          uint64 `json:"maxBytes"`
	MaxFiles          int    `json:"maxFiles"`
	Keep              int    `json:"keep"`
	AllowPrune        bool   `json:"allowPrune"`
	// LockDays keeps each snapshot of a snapshot job from being deleted for
	// that many days (0 to 365); older versions ignore it.
	LockDays int `json:"lockDays,omitempty"`
}
type Settings struct {
	BackupsFirst bool   `json:"backupsFirst,omitempty"`
	Enabled      bool   `json:"enabled"`
	Volume       string `json:"volume"`
	Timezone     string `json:"timezone"`
	WindowFrom   string `json:"windowFrom"`
	WindowTo     string `json:"windowTo"`
	Jobs         []Job  `json:"jobs"`
}
type State struct {
	Running     bool       `json:"running"`
	Blocked     bool       `json:"blocked"`
	LastAttempt *time.Time `json:"lastAttempt"`
	LastOK      *time.Time `json:"lastOK"`
	Error       string     `json:"error"`
	Phase       string     `json:"phase"`
}
type Result struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Kind     string    `json:"kind"`
	Target   string    `json:"target"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	Error    string    `json:"error"`
}
type persisted struct {
	States  map[string]State `json:"states"`
	History []Result         `json:"history"`
}
type View struct {
	Wait  string     `json:"wait,omitempty"`
	Job   Job        `json:"job"`
	State State      `json:"state"`
	Next  *time.Time `json:"next"`
}
type Status struct {
	Settings  Settings `json:"settings"`
	Jobs      []View   `json:"jobs"`
	History   []Result `json:"history"`
	Available bool     `json:"available"`
	Error     string   `json:"error"`
}
type Manager struct {
	store        *config.Store
	MountRoot    string
	Mounted      func() []string
	Execute      func(context.Context, Job) error
	ExternalBusy func(Job) bool
	Conflict     func(Job) string
	OnResult     func(Result)
	Disabled     bool
	mu           sync.Mutex
	ioMu         sync.Mutex
	admission    sync.RWMutex
	settings     Settings
	state        persisted
	loadedVolume string
	problem      string
	cancel       context.CancelFunc
}

func New(store *config.Store) *Manager {
	return &Manager{store: store, MountRoot: "/mnt", settings: Settings{Timezone: "Europe/Berlin", WindowFrom: "03:00", WindowTo: "08:00", Jobs: []Job{}}, state: persisted{States: map[string]State{}, History: []Result{}}}
}
func (m *Manager) Load() error {
	var s Settings
	if err := m.store.ReadJSON(settingsFile, &s); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	for i := range s.Jobs {
		if value := s.Jobs[i].DependencyEnabled; value != nil {
			s.Jobs[i].Enabled = *value
			s.Jobs[i].DependencyEnabled = nil
		}
	}
	if err := validate(s); err != nil {
		return err
	}
	m.mu.Lock()
	m.settings = s
	m.mu.Unlock()
	return nil
}
func clock(value string) (int, bool) {
	t, err := time.Parse("15:04", value)
	return t.Hour()*60 + t.Minute(), err == nil && len(value) == 5
}
func validate(s Settings) error {
	if ((s.Enabled || len(s.Jobs) > 0) && !naming.ValidVolume(s.Volume)) || len(s.Jobs) > 100 {
		return errors.New("invalid maintenance settings")
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return err
	}
	if _, ok := clock(s.WindowFrom); !ok {
		return errors.New("invalid window start")
	}
	if _, ok := clock(s.WindowTo); !ok {
		return errors.New("invalid window end")
	}
	ids := map[string]bool{}
	for _, j := range s.Jobs {
		if !regexp.MustCompile(`^[A-Za-z0-9-]{1,32}$`).MatchString(j.ID) || ids[j.ID] || len(j.Name) < 1 || len(j.Name) > 100 || len(j.Target) < 1 || len(j.Target) > 128 || strings.ContainsAny(j.Target, "\x00\r\n") {
			return errors.New("invalid maintenance job")
		}
		ids[j.ID] = true
		if !slices.Contains([]string{"raid-check", "smart-long", "backup-check", "restore-probe", "snapshot"}, j.Kind) || !slices.Contains([]string{"daily", "weekly", "monthly"}, j.Every) || !slices.Contains([]string{"window", "skip"}, j.Missed) {
			return errors.New("invalid maintenance schedule")
		}
		if _, ok := clock(j.Time); !ok {
			return errors.New("invalid job time")
		}
		if j.Every == "weekly" && (len(j.Weekdays) == 0 || len(j.Weekdays) > 7) {
			return errors.New("choose weekdays")
		}
		for _, d := range j.Weekdays {
			if d < 0 || d > 6 {
				return errors.New("invalid weekday")
			}
		}
		if j.Every == "monthly" && (j.MonthDay < 1 || j.MonthDay > 28) {
			return errors.New("invalid month day")
		}
		if j.ReadData < 0 || j.ReadData > 100 || j.MaxBytes > 1<<30 || j.MaxFiles < 0 || j.MaxFiles > 200 || j.Keep < 0 || j.Keep > 100 {
			return errors.New("invalid job limits")
		}
		if j.Kind == "restore-probe" && (j.MaxBytes == 0 || j.MaxFiles == 0) {
			return errors.New("probe limits required")
		}
		if j.LockDays < 0 || j.LockDays > 365 || j.LockDays > 0 && j.Kind != "snapshot" {
			return errors.New("invalid snapshot lock")
		}
		if j.Kind == "snapshot" && (j.Keep < 1 || !j.AllowPrune) {
			return errors.New("snapshot retention must be confirmed")
		}
	}
	for _, j := range s.Jobs {
		seen := map[string]bool{j.ID: true}
		for parent := j.DependsOn; parent != ""; {
			if seen[parent] {
				return errors.New("maintenance dependency cycle")
			}
			seen[parent] = true
			i := slices.IndexFunc(s.Jobs, func(candidate Job) bool { return candidate.ID == parent })
			if i < 0 {
				return errors.New("maintenance dependency is missing")
			}
			parent = s.Jobs[i].DependsOn
		}
	}
	return nil
}
func (m *Manager) Set(s Settings) error {
	for i := range s.Jobs {
		if s.Jobs[i].ID == "" {
			var b [8]byte
			if _, err := rand.Read(b[:]); err != nil {
				return err
			}
			s.Jobs[i].ID = hex.EncodeToString(b[:])
		}
	}
	if err := validate(s); err != nil {
		return err
	}
	previous := m.Status().Settings
	if previous.Volume != "" {
		if err := m.loadState(); err != nil {
			disabled := previous
			disabled.Enabled = false
			if s.Enabled || !reflect.DeepEqual(s, disabled) {
				return err
			}
		}
	}
	m.ioMu.Lock()
	defer m.ioMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, st := range m.state.States {
		if st.Blocked {
			old := slices.IndexFunc(m.settings.Jobs, func(j Job) bool { return j.ID == id })
			next := slices.IndexFunc(s.Jobs, func(j Job) bool { return j.ID == id })
			if old >= 0 && (next < 0 || m.settings.Jobs[old].Target != s.Jobs[next].Target || m.settings.Jobs[old].Kind != s.Jobs[next].Kind || m.settings.Volume != s.Volume) {
				return errors.New("review the interrupted job before removing or retargeting it")
			}
		}
		if st.Running {
			return errors.New("maintenance is running")
		}
	}
	persistedSettings := s
	persistedSettings.Jobs = slices.Clone(s.Jobs)
	for i := range persistedSettings.Jobs {
		j := &persistedSettings.Jobs[i]
		if j.DependsOn != "" {
			enabled := j.Enabled
			j.DependencyEnabled = &enabled
			j.Enabled = false
		}
	}
	if err := m.store.WriteJSON(settingsFile, persistedSettings); err != nil {
		return err
	}
	if m.settings.Volume != s.Volume {
		m.loadedVolume = ""
		m.state = persisted{States: map[string]State{}, History: []Result{}}
	}
	for id := range m.state.States {
		if !slices.ContainsFunc(s.Jobs, func(j Job) bool { return j.ID == id }) {
			delete(m.state.States, id)
		}
	}
	m.settings = s
	return nil
}
func slots(j Job, now time.Time, loc *time.Location) (previous, next *time.Time) {
	local := now.In(loc)
	minute, _ := clock(j.Time)
	for offset := -32; offset <= 62; offset++ {
		day := local.AddDate(0, 0, offset)
		if j.Every == "weekly" && !slices.Contains(j.Weekdays, int(day.Weekday())) {
			continue
		}
		if j.Every == "monthly" && day.Day() != j.MonthDay {
			continue
		}
		at := time.Date(day.Year(), day.Month(), day.Day(), minute/60, minute%60, 0, 0, loc)
		if !at.After(now) {
			stamp := at.UTC()
			previous = &stamp
		} else if next == nil {
			stamp := at.UTC()
			next = &stamp
		}
	}
	return
}
func (m *Manager) Status() Status {
	m.mu.Lock()
	loc, err := time.LoadLocation(m.settings.Timezone)
	if err != nil {
		loc = time.UTC
	}
	out := Status{Settings: m.settings, Jobs: []View{}, History: slices.Clone(m.state.History), Available: m.loadedVolume != "", Error: m.problem}
	out.Settings.Jobs = slices.Clone(m.settings.Jobs)
	for _, j := range m.settings.Jobs {
		_, next := slots(j, time.Now(), loc)
		wait := m.dependencyWaitLocked(j, time.Now())
		if !m.settings.Enabled || !j.Enabled {
			wait = "disabled"
		} else if m.state.States[j.ID].Blocked {
			wait = "review"
		} else if m.loadedVolume == "" {
			wait = "volume"
		} else if !inWindow(m.settings, time.Now()) {
			wait = "window"
		}
		out.Jobs = append(out.Jobs, View{Job: j, State: m.state.States[j.ID], Next: next, Wait: wait})
	}
	m.mu.Unlock()
	externalBusy := out.Settings.Enabled && len(out.Jobs) > 0 && m.ExternalBusy != nil && m.ExternalBusy(out.Jobs[0].Job)
	for i := range out.Jobs {
		v := &out.Jobs[i]
		if v.Wait != "" || v.State.Running {
			continue
		}
		if out.Settings.BackupsFirst && m.Conflict != nil {
			v.Wait = m.Conflict(v.Job)
		}
		if v.Wait == "" && externalBusy {
			v.Wait = "other-work"
		}
	}
	return out
}
func (m *Manager) Busy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.state.States {
		if s.Running {
			return true
		}
	}
	return false
}
func (m *Manager) loadState() error {
	m.ioMu.Lock()
	defer m.ioMu.Unlock()
	m.mu.Lock()
	s := m.settings
	loaded := m.loadedVolume
	active := m.cancel != nil
	m.mu.Unlock()
	if s.Volume == "" || m.Mounted == nil {
		return volumedata.ErrUnavailable
	}
	mounted := m.Mounted()
	if !slices.Contains(mounted, s.Volume) {
		m.mu.Lock()
		m.loadedVolume = ""
		if m.cancel != nil {
			m.cancel()
		}
		m.mu.Unlock()
		return volumedata.ErrUnavailable
	}
	if active {
		return nil
	}
	if loaded == s.Volume {
		return nil
	}
	root, err := volumedata.Open(m.MountRoot, s.Volume, "maintenance", mounted)
	if err != nil {
		return err
	}
	defer root.Close()
	data := persisted{States: map[string]State{}, History: []Result{}}
	if err := volumedata.ReadJSON(root, "state.json", &data, 2<<20); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if data.States == nil {
		data.States = map[string]State{}
	}
	for id, st := range data.States {
		if st.Running {
			st.Running = false
			st.Blocked = true
			st.Phase = "interrupted"
			st.Error = "Previous execution outcome is unknown; inspect the target and acknowledge before another run."
			data.States[id] = st
		}
	}
	if err := volumedata.WriteJSON(root, "state.json", data); err != nil {
		return err
	}
	m.mu.Lock()
	m.state = data
	m.loadedVolume = s.Volume
	m.problem = ""
	m.mu.Unlock()
	return nil
}
func (m *Manager) saveLocked() error {
	if m.Mounted == nil {
		return volumedata.ErrUnavailable
	}
	root, err := volumedata.Open(m.MountRoot, m.settings.Volume, "maintenance", m.Mounted())
	if err != nil {
		return err
	}
	defer root.Close()
	return volumedata.WriteJSON(root, "state.json", m.state)
}
func (m *Manager) Start(id string) error {
	m.admission.Lock()
	defer m.admission.Unlock()
	if m.Disabled {
		return errors.New("maintenance is disabled in development mode")
	}
	if m.Execute == nil {
		return errors.New("maintenance unavailable")
	}
	if err := m.loadState(); err != nil {
		return err
	}
	m.ioMu.Lock()
	defer m.ioMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	i := slices.IndexFunc(m.settings.Jobs, func(j Job) bool { return j.ID == id })
	if i < 0 {
		return fs.ErrNotExist
	}
	if m.loadedVolume != m.settings.Volume {
		return errors.New("maintenance volume changed; retry after loading its history")
	}
	job := m.settings.Jobs[i]
	for _, s := range m.state.States {
		if s.Running {
			return errors.New("another maintenance job is running")
		}
	}
	st := m.state.States[id]
	if st.Blocked {
		return errors.New(st.Error)
	}
	if reason := m.dependencyWaitLocked(job, time.Now()); reason != "" {
		return fmt.Errorf("waiting: %s", reason)
	}
	if m.settings.BackupsFirst && m.Conflict != nil {
		if reason := m.Conflict(job); reason != "" {
			return fmt.Errorf("waiting: %s", reason)
		}
	}
	if m.ExternalBusy != nil && m.ExternalBusy(job) {
		return errors.New("waiting for other NAS work")
	}
	now := time.Now().UTC()
	st.Running = true
	st.LastAttempt = &now
	st.Phase = "running"
	st.Error = ""
	m.state.States[id] = st
	if err := m.saveLocked(); err != nil {
		st.Running = false
		st.Blocked = true
		st.Error = err.Error()
		m.state.States[id] = st
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go func() {
		err := m.Execute(ctx, job)
		cancel()
		m.ioMu.Lock()
		m.mu.Lock()
		st := m.state.States[id]
		st.Running = false
		st.Phase = "done"
		result := Result{job.ID, job.Name, job.Kind, job.Target, now, time.Now().UTC(), ""}
		if err != nil {
			st.Error = err.Error()
			st.Phase = "failed"
			result.Error = err.Error()
		} else {
			stamp := result.Finished
			st.LastOK = &stamp
		}
		m.state.States[id] = st
		m.state.History = append([]Result{result}, m.state.History...)
		if len(m.state.History) > 100 {
			m.state.History = m.state.History[:100]
		}
		if e := m.saveLocked(); e != nil {
			st.Blocked = true
			st.Phase = "unknown"
			st.Error = "Could not persist execution outcome: " + e.Error()
			m.state.States[id] = st
			m.problem = st.Error
			result.Error = st.Error
			m.state.History[0] = result
		}
		m.cancel = nil
		m.mu.Unlock()
		m.ioMu.Unlock()
		if m.OnResult != nil {
			m.OnResult(result)
		}
	}()
	return nil
}
func (m *Manager) Acknowledge(id, confirm string) error {
	if id != confirm {
		return errors.New("repeat job ID to acknowledge")
	}
	if err := m.loadState(); err != nil {
		return err
	}
	m.ioMu.Lock()
	defer m.ioMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.state.States[id]
	if !ok || st.Running {
		return errors.New("job is not recoverable")
	}
	before := st
	st.Blocked = false
	st.Phase = "reviewed"
	m.state.States[id] = st
	if err := m.saveLocked(); err != nil {
		m.state.States[id] = before
		return err
	}
	return nil
}
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.state.States[id].Running || m.cancel == nil {
		return fs.ErrNotExist
	}
	m.cancel()
	return nil
}
func inWindow(s Settings, now time.Time) bool {
	loc, _ := time.LoadLocation(s.Timezone)
	if loc == nil {
		return false
	}
	local := now.In(loc)
	v := local.Hour()*60 + local.Minute()
	from, _ := clock(s.WindowFrom)
	to, _ := clock(s.WindowTo)
	if from == to {
		return true
	}
	if from < to {
		return v >= from && v < to
	}
	return v >= from || v < to
}
func (m *Manager) Tick(now time.Time) error {
	if m.Disabled || !m.Status().Settings.Enabled {
		return nil
	}
	if err := m.loadState(); err != nil {
		return err
	}
	status := m.Status()
	if !status.Settings.Enabled || !inWindow(status.Settings, now) || m.Busy() {
		return nil
	}
	loc, _ := time.LoadLocation(status.Settings.Timezone)
	for _, v := range status.Jobs {
		j := v.Job
		if !j.Enabled || v.State.Blocked {
			continue
		}
		due, _ := slots(j, now, loc)
		if due == nil || (v.State.LastAttempt != nil && !v.State.LastAttempt.Before(*due)) {
			continue
		}
		if j.Missed == "skip" && now.Sub(*due) > time.Minute {
			continue
		}
		if v.Wait != "" || (status.Settings.BackupsFirst && m.Conflict != nil && m.Conflict(j) != "") || (m.ExternalBusy != nil && m.ExternalBusy(j)) {
			continue
		}
		if err := m.Start(j.ID); err != nil {
			return err
		}
		break
	}
	return nil
}
func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.mu.Lock()
			if m.cancel != nil {
				m.cancel()
			}
			m.mu.Unlock()
			return
		case now := <-ticker.C:
			if err := m.Tick(now); err != nil {
				m.mu.Lock()
				m.problem = fmt.Sprint(err)
				m.mu.Unlock()
			}
		}
	}
}

// Enter serializes admission with the scheduler's final conflict check. The
// caller publishes the new backend-owned job before releasing the admission.
func (m *Manager) Enter() (func(), error) {
	m.admission.RLock()
	if m.Busy() {
		m.admission.RUnlock()
		return nil, errors.New("maintenance is running")
	}
	return m.admission.RUnlock, nil
}

// Caller holds mu. A dependency must have succeeded in its current schedule slot,
// and no newer failed/running attempt may be hidden by an older success.
func (m *Manager) dependencyWaitLocked(job Job, now time.Time) string {
	if job.DependsOn == "" {
		return ""
	}
	i := slices.IndexFunc(m.settings.Jobs, func(j Job) bool { return j.ID == job.DependsOn })
	if i < 0 {
		return "dependency"
	}
	parent := m.settings.Jobs[i]
	st := m.state.States[parent.ID]
	loc, _ := time.LoadLocation(m.settings.Timezone)
	if loc == nil {
		return "dependency"
	}
	due, _ := slots(parent, now, loc)
	if st.Running || st.Blocked || st.Error != "" || st.LastOK == nil || (due != nil && st.LastOK.Before(*due)) || (st.LastAttempt != nil && st.LastOK.Before(*st.LastAttempt)) {
		return "dependency"
	}
	return ""
}
