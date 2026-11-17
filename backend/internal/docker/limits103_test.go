package docker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// hangingRunner lets a docker compose command containing hang wait until
// its context ends, as a stalled registry or build does.
type hangingRunner struct {
	*fakeRunner
	hang string
}

func (r *hangingRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if strings.Contains(strings.Join(args, " "), r.hang) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return r.fakeRunner.Run(ctx, stdin, name, args...)
}

// A pull that hangs ends the job of the app at its limit, with a message
// that says so; the app is free for the next job.
func TestHangingPullEndsAtItsLimit(t *testing.T) {
	pull := composePullLimit
	composePullLimit = 50 * time.Millisecond
	t.Cleanup(func() { composePullLimit = pull })
	m, base, _ := newTestManager(t)
	m.applied = "/somewhere"
	m.SetSettings(Settings{Enabled: true, Volume: "data"}, []string{"data"})
	m.store.Write("apps/whoami/compose.yaml", []byte("services:\n  web:\n    image: traefik/whoami\n"))
	m.runner = &hangingRunner{fakeRunner: base, hang: "pull --quiet"}
	ctx := context.Background()
	if err := m.UpdateApp(ctx, "whoami"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var job Job
	for time.Now().Before(deadline) {
		if job = m.Status(ctx).Jobs["whoami"]; job.State != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job.State != "failed" || job.Code != JobCodeTimeout || !strings.Contains(job.Output, "docker compose pull") || !strings.Contains(job.Output, "time limit") {
		t.Fatalf("job after a hanging pull: %+v", job)
	}
	m.runner = base
	if err := m.UpdateApp(ctx, "whoami"); errors.Is(err, ErrBusy) {
		t.Fatal("the app is still busy after the limit")
	}
	for time.Now().Before(deadline) && m.Status(ctx).Jobs["whoami"].State == "running" {
		time.Sleep(10 * time.Millisecond)
	}
}

// A hanging "down" ends RemoveApp at its limit with ErrComposeTimeout, and
// the app is kept.
func TestHangingDownEndsAtItsLimit(t *testing.T) {
	down := composeDownLimit
	composeDownLimit = 50 * time.Millisecond
	t.Cleanup(func() { composeDownLimit = down })
	m, base, _ := newTestManager(t)
	m.applied = "/somewhere"
	m.SetSettings(Settings{Enabled: true, Volume: "data"}, []string{"data"})
	m.store.Write("apps/whoami/compose.yaml", []byte("services:\n  web:\n    image: traefik/whoami\n"))
	m.runner = &hangingRunner{fakeRunner: base, hang: "down"}
	done := make(chan error, 1)
	go func() { done <- m.RemoveApp(context.Background(), "whoami", false) }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrComposeTimeout) {
			t.Fatalf("hanging down: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a hanging down is waited for without end")
	}
	if _, err := m.Compose("whoami"); err != nil {
		t.Fatalf("the app was forgotten although it was not taken down: %v", err)
	}
}
