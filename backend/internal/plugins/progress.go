package plugins

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"sync"
	"time"
)

// Steps of a plugin installation, update or removal, in the order they run.
// The page and the jobs list show them with a mark each; they live only in RAM.
const (
	StepSignature = "signature" // signed release inventory and manifest, or a local signature
	StepDownload  = "download"  // the image from GitHub onto the data volume
	StepChecksum  = "checksum"  // size and SHA-256 of the image
	StepWrite     = "write"     // the plugin folder on the data volume, the setting on the stick
	StepStop      = "stop"      // services stopped and modules unloaded
	StepSysext    = "sysext"    // systemd-sysext refresh
	StepModules   = "modules"   // module index and modprobe
	StepServices  = "services"  // the plugin's services
	StepHealth    = "health"    // the plugin is active afterwards
	StepRemove    = "remove"    // plugin files deleted
	StepRollback  = "rollback"  // a failed update brought the previous version back
)

// Step states.
const (
	StepPending = "pending"
	StepRunning = "running"
	StepDone    = "done"
	StepFailed  = "failed"
	StepSkipped = "skipped"
)

// Step is one part of a plugin operation.
type Step struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

// Progress is a running or finished plugin operation.
type Progress struct {
	ID     string `json:"id"`
	Plugin string `json:"plugin"`
	Title  string `json:"title,omitempty"`
	// Action is install, update or remove.
	Action string `json:"action"`
	// State is running, done or failed.
	State string `json:"state"`
	Steps []Step `json:"steps"`
	// Bytes and Total belong to the download step; Total 0 means unknown.
	Bytes               int64      `json:"bytes"`
	Total               int64      `json:"total"`
	SpeedBytesPerSecond int64      `json:"speedBytesPerSecond"`
	Error               string     `json:"error,omitempty"`
	Started             time.Time  `json:"started"`
	Finished            *time.Time `json:"finished,omitempty"`
}

// keptProgress bounds the finished operations kept for the page and the jobs list.
const keptProgress = 10

// maxStepError bounds the error text of a step; command output can be long.
const maxStepError = 4000

type progressLog struct {
	mu   sync.Mutex
	list []*Progress
}

// tracker reports the steps of one operation. A nil tracker does nothing, so
// background reconciliation and tests without one run unchanged.
type tracker struct {
	log      *progressLog
	progress *Progress
	// speed sampling
	sampleAt    time.Time
	sampleBytes int64
}

type trackerKey struct{}

func withTracker(ctx context.Context, t *tracker) context.Context {
	return context.WithValue(ctx, trackerKey{}, t)
}

func trackerOf(ctx context.Context) *tracker {
	t, _ := ctx.Value(trackerKey{}).(*tracker)
	return t
}

// track starts an operation with the given steps, all pending.
func (m *Manager) track(plugin, action string, steps ...string) *tracker {
	id := make([]byte, 8)
	rand.Read(id)
	p := &Progress{ID: hex.EncodeToString(id), Plugin: plugin, Action: action, State: "running", Steps: []Step{}, Started: time.Now()}
	for _, step := range steps {
		p.Steps = append(p.Steps, Step{ID: step, State: StepPending})
	}
	m.progress.mu.Lock()
	defer m.progress.mu.Unlock()
	m.progress.list = append(m.progress.list, p)
	return &tracker{log: &m.progress, progress: p}
}

// prune drops the oldest finished operations; running ones always stay.
// Called with mu held.
func (l *progressLog) prune() {
	finished := 0
	for _, p := range l.list {
		if p.State != "running" {
			finished++
		}
	}
	for ; finished > keptProgress; finished-- {
		index := slices.IndexFunc(l.list, func(p *Progress) bool { return p.State != "running" })
		l.list = slices.Delete(l.list, index, index+1)
	}
}

// Progress returns copies of the known plugin operations, oldest first.
// It never waits for a running plugin action.
func (m *Manager) Progress() []Progress {
	m.progress.mu.Lock()
	defer m.progress.mu.Unlock()
	out := make([]Progress, 0, len(m.progress.list))
	for _, p := range m.progress.list {
		copied := *p
		copied.Steps = slices.Clone(p.Steps)
		out = append(out, copied)
	}
	return out
}

// ForgetProgress removes a finished operation. Running ones stay.
func (m *Manager) ForgetProgress(id string) error {
	m.progress.mu.Lock()
	defer m.progress.mu.Unlock()
	index := slices.IndexFunc(m.progress.list, func(p *Progress) bool { return p.ID == id })
	if index < 0 {
		return ErrNotFound
	}
	if m.progress.list[index].State == "running" {
		return ErrBusy
	}
	m.progress.list = slices.Delete(m.progress.list, index, index+1)
	return nil
}

func (t *tracker) update(change func(p *Progress)) {
	if t == nil {
		return
	}
	t.log.mu.Lock()
	defer t.log.mu.Unlock()
	change(t.progress)
}

func stepIndex(p *Progress, id string) int {
	return slices.IndexFunc(p.Steps, func(s Step) bool { return s.ID == id })
}

func runningStep(p *Progress) int {
	return slices.IndexFunc(p.Steps, func(s Step) bool { return s.State == StepRunning })
}

// plugin is the plugin this operation is about.
func (t *tracker) plugin() string {
	if t == nil {
		return ""
	}
	t.log.mu.Lock()
	defer t.log.mu.Unlock()
	return t.progress.Plugin
}

// name sets the plugin once a local manifest has been read.
func (t *tracker) name(plugin, title string) {
	t.update(func(p *Progress) {
		if p.Plugin == "" {
			p.Plugin = plugin
		}
		p.Title = title
	})
}

// expect adds steps that are known only once the manifest is read, before the
// final health step when one is planned.
func (t *tracker) expect(steps ...string) {
	t.update(func(p *Progress) {
		for _, step := range steps {
			if stepIndex(p, step) < 0 {
				p.Steps = append(p.Steps, Step{ID: step, State: StepPending})
			}
		}
	})
}

// begin finishes the running step and starts id. Starting a step that already
// ran does nothing: a check repeated by a later stage stays one mark.
func (t *tracker) begin(id string) {
	t.update(func(p *Progress) {
		index := stepIndex(p, id)
		if index >= 0 && p.Steps[index].State != StepPending {
			return
		}
		if running := runningStep(p); running >= 0 {
			p.Steps[running].State = StepDone
		}
		// Steps that were passed over did not apply to this plugin.
		if index < 0 {
			p.Steps = append(p.Steps, Step{ID: id, State: StepRunning})
			return
		}
		for i := range index {
			if p.Steps[i].State == StepPending {
				p.Steps[i].State = StepSkipped
			}
		}
		p.Steps[index].State = StepRunning
	})
}

// skip marks a step that does not apply, such as the signature of an
// unsigned plugin confirmed by name.
func (t *tracker) skip(id string) {
	t.update(func(p *Progress) {
		if index := stepIndex(p, id); index >= 0 && p.Steps[index].State == StepPending {
			p.Steps[index].State = StepSkipped
		}
	})
}

// complete marks the running step done.
func (t *tracker) complete() {
	t.update(func(p *Progress) {
		if running := runningStep(p); running >= 0 {
			p.Steps[running].State = StepDone
		}
	})
}

// fail marks the running step failed with err, without ending the operation.
func (t *tracker) fail(err error) {
	t.update(func(p *Progress) {
		if running := runningStep(p); running >= 0 && err != nil {
			p.Steps[running].State = StepFailed
			p.Steps[running].Error = errorText(err)
		}
	})
}

// bytes reports the download; the speed is sampled at most once a second.
func (t *tracker) bytes(done, total int64) {
	if t == nil {
		return
	}
	now := time.Now()
	t.update(func(p *Progress) {
		p.Bytes, p.Total = done, total
		switch {
		case t.sampleAt.IsZero() || done < t.sampleBytes:
			t.sampleAt, t.sampleBytes = now, done
		case now.Sub(t.sampleAt) >= time.Second:
			p.SpeedBytesPerSecond = int64(float64(done-t.sampleBytes) / now.Sub(t.sampleAt).Seconds())
			t.sampleAt, t.sampleBytes = now, done
		}
	})
}

// finish ends the operation. A failure marks the running step; steps not
// reached stay pending. An operation that failed before its first step (a
// refused request) is dropped: the API answer already says why.
func (t *tracker) finish(err error) {
	if t == nil {
		return
	}
	t.log.mu.Lock()
	defer t.log.mu.Unlock()
	p := t.progress
	defer t.log.prune()
	now := time.Now()
	p.Finished = &now
	p.SpeedBytesPerSecond = 0
	if err == nil {
		p.State = "done"
		for i := range p.Steps {
			if p.Steps[i].State == StepRunning {
				p.Steps[i].State = StepDone
			} else if p.Steps[i].State == StepPending {
				p.Steps[i].State = StepSkipped
			}
		}
		return
	}
	p.State, p.Error = "failed", errorText(err)
	started := slices.ContainsFunc(p.Steps, func(s Step) bool { return s.State != StepPending && s.State != StepSkipped })
	if !started {
		t.log.list = slices.DeleteFunc(t.log.list, func(other *Progress) bool { return other == p })
		return
	}
	if running := runningStep(p); running >= 0 {
		p.Steps[running].State = StepFailed
		p.Steps[running].Error = p.Error
	}
}

func errorText(err error) string {
	text := err.Error()
	if len(text) > maxStepError {
		text = text[:maxStepError] + "…"
	}
	return text
}
