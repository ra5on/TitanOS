package shutdown

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// Shutdown scripts run first, while the guests still run, and show up as
// items; a failed script does not keep the NAS from going down.
func TestShutdownScriptsRunBeforeTheGuests(t *testing.T) {
	f := newFake()
	w := f.workloads()
	calls := 0
	w.Scripts = func(ctx context.Context, progress func(string, string, error)) {
		calls++
		f.mu.Lock()
		vmUp := f.vms["haos"]
		f.mu.Unlock()
		if !vmUp {
			t.Error("the guests ended before the scripts")
		}
		progress("sichern", "running", nil)
		progress("sichern", "stopped", nil)
		progress("kaputt", "running", nil)
		progress("kaputt", "failed", errors.New("timeout"))
	}
	m := New(w)
	m.Wait, m.Poll = time.Minute, 5*time.Millisecond
	if err := m.Begin("poweroff", "alex", false, f.power("poweroff")); err != nil {
		t.Fatal(err)
	}
	if got := powered(t, f, 2*time.Second); got != "poweroff" {
		t.Fatalf("powered %q", got)
	}
	var scripts []string
	for _, it := range m.Status().Items {
		if it.Kind == "script" {
			scripts = append(scripts, it.Name+":"+it.State+":"+it.Error)
		}
	}
	if !slices.Equal(scripts, []string{"sichern:stopped:", "kaputt:failed:timeout"}) {
		t.Fatalf("items %+v", m.Status().Items)
	}
	if calls != 1 {
		t.Fatalf("scripts ran %d times", calls)
	}
}

// "Now" ends the running script and goes on.
func TestNowEndsTheShutdownScripts(t *testing.T) {
	f := newFake()
	f.vms, f.lxc, f.dockerOn = map[string]bool{}, map[string]bool{}, false
	w := f.workloads()
	w.Scripts = func(ctx context.Context, progress func(string, string, error)) {
		progress("lang", "running", nil)
		<-ctx.Done()
		progress("lang", "failed", ctx.Err())
	}
	m := New(w)
	m.Wait, m.Poll = time.Minute, 5*time.Millisecond
	if err := m.Begin("reboot", "alex", false, f.power("reboot")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := m.Now(); err != nil {
		t.Fatal(err)
	}
	if got := powered(t, f, 2*time.Second); got != "reboot" {
		t.Fatalf("powered %q", got)
	}
}
