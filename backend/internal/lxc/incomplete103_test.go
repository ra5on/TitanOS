package lxc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// halfRunner unpacks part of the root filesystem and then fails, as a
// download that breaks off does.
type halfRunner struct{ *fakeRunner }

func (r halfRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if name == "env" && slices.Contains(args, "lxc-create") {
		path, container := args[slices.Index(args, "-P")+1], args[slices.Index(args, "-n")+1]
		os.MkdirAll(filepath.Join(path, container, "rootfs", "etc"), 0o755)
		return "", errors.New("lxc-create: download broke off")
	}
	return r.fakeRunner.Run(ctx, stdin, name, args...)
}

// A container whose creation failed halfway is shown as failed, cannot be
// started and can be removed; also after a restart of titand.
func TestHalfCreatedContainerDoesNotStart(t *testing.T) {
	env := newTestManager(t)
	m, ctx := env.m, context.Background()
	m.runner = halfRunner{env.runner}
	if err := m.Create(ctx, Container{Name: "box", Volume: "data", Distribution: "alpine", Release: "3.22"}, mounted); err != nil {
		t.Fatal(err)
	}
	if job := env.waitJob(t, "box"); job == nil || job.State != "failed" {
		t.Fatalf("job %+v", job)
	}
	if err := m.Start(ctx, "box", mounted); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("a half created container: %v", err)
	}
	// After a restart of titand it is still incomplete.
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.jobs = map[string]*Job{}
	m.mu.Unlock()
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx, "box", mounted); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("after a restart: %v", err)
	}
	if err := m.Delete(ctx, "box", true, "box"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.mountRoot, "data", "lxc", "box")); !os.IsNotExist(err) {
		t.Fatalf("folder kept: %v", err)
	}
}

// titand stopped while the container was being made: after the start it
// is listed as failed (interrupted) instead of as ready.
func TestInterruptedCreationIsFailedAfterTheStart(t *testing.T) {
	env := newTestManager(t)
	m, ctx := env.m, context.Background()
	block := make(chan struct{})
	env.runner.block = block
	if err := m.Create(ctx, Container{Name: "web", Volume: "data", Distribution: "debian", Release: "trixie"}, mounted); err != nil {
		t.Fatal(err)
	}
	// What the stick holds while lxc-create runs; then titand stops.
	data, err := m.store.Read("lxc/web.json")
	if err != nil {
		t.Fatal(err)
	}
	close(block)
	env.waitJob(t, "web")
	other := newTestManager(t)
	other.m.store.Write("lxc/web.json", data)
	os.MkdirAll(filepath.Join(other.m.mountRoot, "data", "lxc", "web", "rootfs"), 0o755)
	if err := other.m.Load(); err != nil {
		t.Fatal(err)
	}
	list := other.m.List(mounted)
	if len(list) != 1 || list[0].Job == nil || list[0].Job.State != "failed" || list[0].Job.Code != "interrupted" {
		t.Fatalf("after the restart: %+v", list)
	}
	if err := other.m.Start(ctx, "web", mounted); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("started: %v", err)
	}
	other.m.autostart(ctx, mounted)
	for _, line := range other.runner.lines() {
		if strings.Contains(line, "systemctl start") {
			t.Fatalf("started: %s", line)
		}
	}
	// The finished container of the first manager is ready and keeps
	// the stick file readable for 1.02 (no other change of shape).
	if err := m.Start(ctx, "web", mounted); err != nil {
		t.Fatalf("finished container: %v", err)
	}
	stored, _ := m.store.Read("lxc/web.json")
	var old struct {
		Name    string    `json:"name"`
		Volume  string    `json:"volume"`
		Created time.Time `json:"created"`
	}
	if err := json.Unmarshal(stored, &old); err != nil || old.Name != "web" || strings.Contains(string(stored), "incomplete") {
		t.Fatalf("stored after creation: %s %v", stored, err)
	}
}
