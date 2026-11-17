package lxc

import (
	"context"
	"slices"
	"testing"
	"time"
)

// hangingCreate holds lxc-create until its context ends.
type hangingCreate struct{ *fakeRunner }

func (r hangingCreate) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if name == "env" && slices.Contains(args, "lxc-create") {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return r.fakeRunner.Run(ctx, stdin, name, args...)
}

// A download that hangs ends at its limit: the container counts as failed
// (code timeout), and nothing waits on it for good (an app update waits
// while a creation runs).
func TestHangingCreateEndsAtItsLimit(t *testing.T) {
	limit := createLimit
	createLimit = 50 * time.Millisecond
	t.Cleanup(func() { createLimit = limit })
	env := newTestManager(t)
	env.m.runner = hangingCreate{env.runner}
	if err := env.m.Create(context.Background(), Container{Name: "web", Volume: "data", Distribution: "debian", Release: "trixie"}, mounted); err != nil {
		t.Fatal(err)
	}
	job := env.waitJob(t, "web")
	if job == nil || job.State != "failed" || job.Code != "timeout" {
		t.Fatalf("job: %+v", job)
	}
	if env.m.Busy() {
		t.Fatal("still busy")
	}
}
