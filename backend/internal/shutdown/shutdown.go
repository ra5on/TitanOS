// Package shutdown ends the VMs, LXC containers and Docker apps of the NAS
// before it restarts or shuts down (owner request of 29 Oct 2026). systemd
// would end them too, but in a hurry (Docker: ten seconds per container)
// and without anyone seeing what hangs. Here the guests get their time,
// the web UI shows each of them, and a person can go on at any time or
// call the whole thing off, which starts again what was ended.
//
// A restart nobody watches (an update, the power schedule, Home Assistant)
// goes on by itself once the time is up; one asked for in the web UI waits
// for the person then.
package shutdown

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// Workloads are the hooks into the managers of VMs, LXC and Docker.
type Workloads struct {
	// VMs lists the VMs that run.
	VMs      func() []string
	StopVM   func(ctx context.Context, name string) error
	StartVM  func(ctx context.Context, name string) error
	LXC      func() []string
	StopLXC  func(ctx context.Context, name string) error
	StartLXC func(ctx context.Context, name string) error
	// Containers lists the Docker containers that run, by name.
	Containers func(ctx context.Context) []string
	// StopDocker ends the engine with all its containers. Containers are
	// not stopped one by one: those with "unless-stopped" would then count
	// as stopped on purpose and not come back after the restart.
	StopDocker  func(ctx context.Context) error
	StartDocker func(ctx context.Context) error
}

// Item is one guest on the way down.
type Item struct {
	Kind  string `json:"kind"` // vm, lxc, docker
	Name  string `json:"name"`
	State string `json:"state"` // stopping, stopped, failed
	Error string `json:"error,omitempty"`
}

// Status is a preparation under way.
type Status struct {
	Action string `json:"action"` // reboot or poweroff
	By     string `json:"by"`
	// Phase: stopping; waiting (the time is up, the person decides);
	// going (the NAS goes down now); failed (the restart itself failed).
	Phase      string    `json:"phase"`
	Unattended bool      `json:"unattended"`
	Started    time.Time `json:"started"`
	Deadline   time.Time `json:"deadline"`
	Items      []Item    `json:"items"`
	Error      string    `json:"error,omitempty"`
}

var (
	// ErrBusy: a preparation runs already.
	ErrBusy = errors.New("shutdown: the NAS is already being prepared for going down")
	// ErrNone: nothing is being prepared.
	ErrNone = errors.New("shutdown: nothing is being prepared")
)

// Manager runs one preparation at a time.
type Manager struct {
	Workloads Workloads
	// Wait is how long the guests get (5 minutes, owner decision).
	Wait time.Duration
	// Poll is how often the state is looked at.
	Poll time.Duration

	mu      sync.Mutex
	status  *Status
	now     chan struct{}
	cancel  context.CancelFunc
	stopped stopped
}

// stopped is what this preparation ended, to start it again on cancel.
type stopped struct {
	vms, lxc []string
	docker   bool
}

// New returns a manager with the default times.
func New(w Workloads) *Manager {
	return &Manager{Workloads: w, Wait: 5 * time.Minute, Poll: 2 * time.Second}
}

// Status returns the preparation under way, or nil.
func (m *Manager) Status() *Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status == nil {
		return nil
	}
	s := *m.status
	s.Items = append([]Item{}, m.status.Items...)
	return &s
}

// Begin ends the guests and then runs power (systemctl reboot or poweroff).
// It returns at once; Status shows the progress.
func (m *Manager) Begin(action, by string, unattended bool, power func() error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status != nil && m.status.Phase != "failed" {
		return ErrBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now()
	m.status = &Status{Action: action, By: by, Phase: "stopping", Unattended: unattended, Started: now, Deadline: now.Add(m.Wait), Items: []Item{}}
	m.now, m.cancel, m.stopped = make(chan struct{}), cancel, stopped{}
	slog.Info("preparing to go down", "action", action, "by", by, "unattended", unattended)
	go m.run(ctx, m.now, power)
	return nil
}

// Now goes down without waiting any longer for the guests.
func (m *Manager) Now() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status == nil || m.status.Phase == "going" {
		return ErrNone
	}
	select {
	case <-m.now:
	default:
		close(m.now)
	}
	slog.Info("going down without waiting for the guests", "action", m.status.Action)
	return nil
}

// Cancel calls the preparation off and starts again what it ended.
func (m *Manager) Cancel(ctx context.Context) error {
	m.mu.Lock()
	if m.status == nil || m.status.Phase == "going" {
		m.mu.Unlock()
		return ErrNone
	}
	m.cancel()
	again := m.stopped
	m.status, m.stopped = nil, stopped{}
	m.mu.Unlock()
	slog.Info("going down called off; starting the guests again", "vms", len(again.vms), "lxc", len(again.lxc), "docker", again.docker)
	w := m.Workloads
	if again.docker && w.StartDocker != nil {
		if err := w.StartDocker(ctx); err != nil {
			slog.Warn("docker engine not started again", "err", err)
		}
	}
	for _, name := range again.lxc {
		if err := w.StartLXC(ctx, name); err != nil {
			slog.Warn("lxc container not started again", "name", name, "err", err)
		}
	}
	for _, name := range again.vms {
		if err := w.StartVM(ctx, name); err != nil {
			slog.Warn("vm not started again", "name", name, "err", err)
		}
	}
	return nil
}

func (m *Manager) run(ctx context.Context, now <-chan struct{}, power func() error) {
	w := m.Workloads
	list := func(f func() []string) []string {
		if f == nil {
			return nil
		}
		names := f()
		sort.Strings(names)
		return names
	}
	vms, lxc := list(w.VMs), list(w.LXC)
	var containers []string
	if w.Containers != nil {
		containers = w.Containers(ctx)
		sort.Strings(containers)
	}
	m.mu.Lock()
	for _, name := range vms {
		m.status.Items = append(m.status.Items, Item{Kind: "vm", Name: name, State: "stopping"})
	}
	for _, name := range lxc {
		m.status.Items = append(m.status.Items, Item{Kind: "lxc", Name: name, State: "stopping"})
	}
	for _, name := range containers {
		m.status.Items = append(m.status.Items, Item{Kind: "docker", Name: name, State: "stopping"})
	}
	m.stopped = stopped{vms: vms, lxc: lxc, docker: len(containers) > 0}
	m.mu.Unlock()

	// Everything at once: they do not depend on each other.
	set := func(kind, name, state string, err error) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.status == nil {
			return
		}
		for i := range m.status.Items {
			it := &m.status.Items[i]
			if it.Kind == kind && (name == "" || it.Name == name) && it.State != "stopped" {
				it.State = state
				if err != nil {
					it.Error = err.Error()
				}
			}
		}
	}
	for _, name := range vms {
		if err := w.StopVM(ctx, name); err != nil && ctx.Err() == nil {
			set("vm", name, "failed", err)
		}
	}
	for _, name := range lxc {
		go func() {
			if err := w.StopLXC(ctx, name); err != nil {
				if ctx.Err() == nil {
					set("lxc", name, "failed", err)
				}
				return
			}
			set("lxc", name, "stopped", nil)
		}()
	}
	if len(containers) > 0 {
		go func() {
			if err := w.StopDocker(ctx); err != nil {
				if ctx.Err() == nil {
					set("docker", "", "failed", err)
				}
				return
			}
			set("docker", "", "stopped", nil)
		}()
	}

	ticker := time.NewTicker(m.Poll)
	defer ticker.Stop()
	for {
		// A VM is down once its QEMU is gone, however it got there.
		running := map[string]bool{}
		for _, name := range list(w.VMs) {
			running[name] = true
		}
		for _, name := range vms {
			if !running[name] {
				set("vm", name, "stopped", nil)
			}
		}
		m.mu.Lock()
		if m.status == nil || ctx.Err() != nil {
			m.mu.Unlock()
			return
		}
		done := true
		for _, it := range m.status.Items {
			if it.State != "stopped" {
				done = false
			}
		}
		late := time.Now().After(m.status.Deadline)
		if !done && late && m.status.Phase == "stopping" {
			if m.status.Unattended {
				done = true
				slog.Warn("guests still running after the waiting time; going down anyway", "action", m.status.Action)
			} else {
				m.status.Phase = "waiting"
				slog.Warn("guests still running after the waiting time; waiting for a decision", "action", m.status.Action)
			}
		}
		m.mu.Unlock()
		if done {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-now:
		case <-ticker.C:
			continue
		}
		break
	}

	m.mu.Lock()
	if m.status == nil || ctx.Err() != nil {
		m.mu.Unlock()
		return
	}
	m.status.Phase = "going"
	action := m.status.Action
	m.mu.Unlock()
	slog.Info("guests ended; going down", "action", action)
	if err := power(); err != nil {
		slog.Error("going down failed", "action", action, "err", err)
		m.mu.Lock()
		if m.status != nil {
			m.status.Phase, m.status.Error = "failed", err.Error()
		}
		m.mu.Unlock()
	}
}
