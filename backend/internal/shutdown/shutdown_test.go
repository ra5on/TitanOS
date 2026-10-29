package shutdown

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

// fake is a NAS with VMs, LXC containers and Docker apps that end when
// asked, unless they are stubborn.
type fake struct {
	mu        sync.Mutex
	vms       map[string]bool // running
	stubborn  map[string]bool // ignores the request to shut down
	lxc       map[string]bool
	docker    []string
	dockerOn  bool
	dockerErr error
	started   []string
	powered   chan string
}

func newFake() *fake {
	return &fake{
		vms: map[string]bool{"haos": true, "win": true}, stubborn: map[string]bool{},
		lxc: map[string]bool{"pihole": true}, docker: []string{"immich-server", "immich-db"}, dockerOn: true,
		powered: make(chan string, 1),
	}
}

func (f *fake) workloads() Workloads {
	running := func(set map[string]bool) func() []string {
		return func() []string {
			f.mu.Lock()
			defer f.mu.Unlock()
			var names []string
			for name, on := range set {
				if on {
					names = append(names, name)
				}
			}
			return names
		}
	}
	return Workloads{
		VMs: running(f.vms),
		StopVM: func(_ context.Context, name string) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			if !f.stubborn[name] {
				f.vms[name] = false
			}
			return nil
		},
		StartVM: func(_ context.Context, name string) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.vms[name] = true
			f.started = append(f.started, "vm:"+name)
			return nil
		},
		LXC: running(f.lxc),
		StopLXC: func(_ context.Context, name string) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.lxc[name] = false
			return nil
		},
		StartLXC: func(_ context.Context, name string) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.lxc[name] = true
			f.started = append(f.started, "lxc:"+name)
			return nil
		},
		Containers: func(context.Context) []string {
			f.mu.Lock()
			defer f.mu.Unlock()
			if !f.dockerOn {
				return nil
			}
			return slices.Clone(f.docker)
		},
		StopDocker: func(context.Context) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.dockerErr != nil {
				return f.dockerErr
			}
			f.dockerOn = false
			return nil
		},
		StartDocker: func(context.Context) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.dockerOn = true
			f.started = append(f.started, "docker")
			return nil
		},
	}
}

func (f *fake) power(action string) func() error {
	return func() error { f.powered <- action; return nil }
}

func testManager(f *fake, wait time.Duration) *Manager {
	m := New(f.workloads())
	m.Wait, m.Poll = wait, 5*time.Millisecond
	return m
}

func powered(t *testing.T, f *fake, within time.Duration) string {
	t.Helper()
	select {
	case action := <-f.powered:
		return action
	case <-time.After(within):
		return ""
	}
}

func TestEverythingEndsThenTheNASGoesDown(t *testing.T) {
	f := newFake()
	m := testManager(f, time.Minute)
	if err := m.Begin("reboot", "alex", false, f.power("reboot")); err != nil {
		t.Fatal(err)
	}
	if err := m.Begin("poweroff", "alex", false, f.power("poweroff")); !errors.Is(err, ErrBusy) {
		t.Fatalf("second: %v", err)
	}
	if got := powered(t, f, 2*time.Second); got != "reboot" {
		t.Fatalf("powered %q", got)
	}
	s := m.Status()
	if s.Phase != "going" || len(s.Items) != 5 {
		t.Fatalf("status %+v", s)
	}
	for _, it := range s.Items {
		if it.State != "stopped" {
			t.Errorf("%+v", it)
		}
	}
}

func TestNothingRunningGoesDownAtOnce(t *testing.T) {
	f := newFake()
	f.vms, f.lxc, f.dockerOn = map[string]bool{}, map[string]bool{}, false
	m := testManager(f, time.Minute)
	m.Begin("poweroff", "alex", false, f.power("poweroff"))
	if got := powered(t, f, time.Second); got != "poweroff" {
		t.Fatalf("powered %q", got)
	}
}

// A VM that ignores the request: the person decides, an update does not wait.
func TestAStubbornGuest(t *testing.T) {
	f := newFake()
	f.stubborn["win"] = true
	m := testManager(f, 50*time.Millisecond)
	m.Begin("reboot", "alex", false, f.power("reboot"))
	if got := powered(t, f, 300*time.Millisecond); got != "" {
		t.Fatal("went down while a VM still ran and the person watches")
	}
	s := m.Status()
	if s.Phase != "waiting" {
		t.Fatalf("phase %q", s.Phase)
	}
	for _, it := range s.Items {
		if (it.Name == "win") != (it.State == "stopping") {
			t.Errorf("%+v", it)
		}
	}
	// "Go down anyway".
	if err := m.Now(); err != nil {
		t.Fatal(err)
	}
	if got := powered(t, f, time.Second); got != "reboot" {
		t.Fatalf("powered %q", got)
	}

	// Unattended (an update): after the time it goes on by itself.
	f = newFake()
	f.stubborn["win"] = true
	m = testManager(f, 50*time.Millisecond)
	m.Begin("reboot", "update", true, f.power("reboot"))
	if got := powered(t, f, time.Second); got != "reboot" {
		t.Fatalf("unattended: powered %q", got)
	}
}

func TestADockerEngineThatFailsToStopIsShown(t *testing.T) {
	f := newFake()
	f.dockerErr = errors.New("timeout")
	m := testManager(f, 30*time.Millisecond)
	m.Begin("poweroff", "alex", false, f.power("poweroff"))
	time.Sleep(150 * time.Millisecond)
	s := m.Status()
	failed := 0
	for _, it := range s.Items {
		if it.Kind == "docker" && it.State == "failed" && it.Error == "timeout" {
			failed++
		}
	}
	if s.Phase != "waiting" || failed != 2 {
		t.Fatalf("status %+v", s)
	}
}

// Calling it off starts again exactly what was ended.
func TestCancelStartsTheGuestsAgain(t *testing.T) {
	f := newFake()
	f.stubborn["win"] = true
	m := testManager(f, time.Minute)
	m.Begin("reboot", "alex", false, f.power("reboot"))
	time.Sleep(50 * time.Millisecond)
	if err := m.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.Status() != nil {
		t.Fatal("status after cancel")
	}
	if got := powered(t, f, 100*time.Millisecond); got != "" {
		t.Fatal("went down after cancel")
	}
	f.mu.Lock()
	started := slices.Clone(f.started)
	f.mu.Unlock()
	slices.Sort(started)
	if !slices.Equal(started, []string{"docker", "lxc:pihole", "vm:haos", "vm:win"}) {
		t.Fatalf("started %v", started)
	}
	if err := m.Cancel(context.Background()); !errors.Is(err, ErrNone) {
		t.Fatalf("second cancel: %v", err)
	}
	// A new start is possible again.
	if err := m.Begin("reboot", "alex", true, f.power("reboot")); err != nil {
		t.Fatal(err)
	}
}

func TestAFailedRestartCanBeTriedAgain(t *testing.T) {
	f := newFake()
	f.vms, f.lxc, f.dockerOn = map[string]bool{}, map[string]bool{}, false
	m := testManager(f, time.Minute)
	m.Begin("reboot", "alex", false, func() error { return errors.New("systemctl failed") })
	deadline := time.Now().Add(time.Second)
	for m.Status().Phase != "failed" {
		if time.Now().After(deadline) {
			t.Fatalf("status %+v", m.Status())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := m.Begin("reboot", "alex", false, f.power("reboot")); err != nil {
		t.Fatal(err)
	}
	if got := powered(t, f, time.Second); got != "reboot" {
		t.Fatalf("powered %q", got)
	}
}
