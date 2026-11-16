package vm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// heldRunner holds a virsh command containing hold until release is
// closed or its context ends; everything else goes to the fake.
type heldRunner struct {
	*fakeRunner
	hold    string
	started chan struct{}
	release chan struct{}
}

func (r *heldRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if name == "virsh" && strings.Contains(strings.Join(args, " "), r.hold) {
		select {
		case r.started <- struct{}{}:
		default:
		}
		select {
		case <-r.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return r.fakeRunner.Run(ctx, stdin, name, args...)
}

// A page closed while libvirt creates the VM does not cut off virsh: the
// VM counts as started.
func TestStartGoesOnWhenThePageCloses(t *testing.T) {
	m, base := newTestManager(t)
	spec := VM{Name: "office", Template: "linux", MemoryMiB: 512, VCPUs: 1, Disks: []Disk{{Format: "qcow2", SizeBytes: gib, Volume: "fast"}}}
	if err := m.Create(context.Background(), spec, []string{"fast"}); err != nil {
		t.Fatal(err)
	}
	r := &heldRunner{fakeRunner: base, hold: "create", started: make(chan struct{}, 1), release: make(chan struct{})}
	m.runner = r
	request, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Start(request, "office", []string{"fast"}) }()
	select {
	case <-r.started:
	case err := <-done:
		t.Fatalf("start ended before virsh create: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("virsh create not reached")
	}
	cancel() // the page is closed
	time.Sleep(50 * time.Millisecond)
	close(r.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("start failed after the page closed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("start did not end")
	}
	if _, err := os.Stat(filepath.Join(m.pidDir, "office.pid")); err != nil {
		t.Fatalf("VM not started: %v", err)
	}
}

// A virsh command that hangs ends after its limit with a clear error, and
// the VM's lock is free again.
func TestHangingVirshEndsAfterItsLimit(t *testing.T) {
	limit := virshLimit
	virshLimit = 100 * time.Millisecond
	t.Cleanup(func() { virshLimit = limit })
	m, base := newTestManager(t)
	spec := VM{Name: "office", Template: "linux", MemoryMiB: 512, VCPUs: 1, Disks: []Disk{{Format: "qcow2", SizeBytes: gib, Volume: "fast"}}}
	if err := m.Create(context.Background(), spec, []string{"fast"}); err != nil {
		t.Fatal(err)
	}
	running(t, m, "office")
	r := &heldRunner{fakeRunner: base, hold: "shutdown", started: make(chan struct{}, 1), release: make(chan struct{})}
	m.runner = r
	t.Cleanup(func() { close(r.release) })
	done := make(chan error, 1)
	go func() { done <- m.Action(context.Background(), "office", "shutdown") }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrLibvirtTimeout) {
			t.Fatalf("hanging virsh: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a hanging virsh holds the VM for good")
	}
	lock, err := m.operation("office")
	if err != nil {
		t.Fatalf("the VM's lock is still held: %v", err)
	}
	lock.Unlock()
}
