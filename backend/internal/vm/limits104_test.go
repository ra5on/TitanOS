package vm

import (
	"context"
	"runtime"
	"slices"
	"testing"
	"time"
)

// A failed systemctl does not count as a reservation: the loop of the VMs
// tries it again, and only a reservation that went through is kept.
func TestFailedCPUReservationIsTriedAgain(t *testing.T) {
	if runtime.NumCPU() < 2 {
		t.Skip("one CPU only")
	}
	m, runner := newTestManager(t)
	m.mu.Lock()
	m.vms["solo"] = VM{Name: "solo", Template: "linux", Exclusive: true, CPUPin: []int{0}}
	m.mu.Unlock()
	running(t, m, "solo")
	runner.fail = "AllowedCPUs"
	m.reserve(context.Background())
	if m.reserved != "" {
		t.Fatalf("a failed reservation counts as done: %q", m.reserved)
	}
	runner.fail = ""
	m.cpuRetryAt = m.cpuRetryAt.Add(-cpuRetryWait)
	m.sample()
	want := m.exclusiveCPUs()
	if m.reserved != want || !slices.Contains(runner.commands, "systemctl set-property --runtime init.scope AllowedCPUs="+want) {
		t.Fatalf("not tried again: reserved %q, commands %v", m.reserved, runner.commands)
	}
}

// hangingSystemctl holds every systemctl until its context ends.
type hangingSystemctl struct{ *fakeRunner }

func (r hangingSystemctl) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if name == "systemctl" {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return r.fakeRunner.Run(ctx, stdin, name, args...)
}

// A hanging systemctl does not hold the lock of the VMs for good.
func TestHangingSystemctlEndsAfterItsLimit(t *testing.T) {
	if runtime.NumCPU() < 2 {
		t.Skip("one CPU only")
	}
	limit := systemctlLimit
	systemctlLimit = 50 * time.Millisecond
	t.Cleanup(func() { systemctlLimit = limit })
	m, base := newTestManager(t)
	m.runner = hangingSystemctl{base}
	m.mu.Lock()
	m.vms["solo"] = VM{Name: "solo", Template: "linux", Exclusive: true, CPUPin: []int{0}}
	m.mu.Unlock()
	running(t, m, "solo")
	done := make(chan struct{})
	go func() { m.reserve(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a hanging systemctl holds the VMs for good")
	}
	if m.reserved != "" {
		t.Errorf("a reservation cut off counts as done: %q", m.reserved)
	}
}
