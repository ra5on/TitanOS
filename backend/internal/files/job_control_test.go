package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJobQueuePriorityPauseAndIsolation(t *testing.T) {
	m, _, _ := newRoot(t)
	a := &Job{ID: "a", user: "alex", State: JobRunning, c: &counters{}}
	b := &Job{ID: "b", user: "alex", State: JobRunning, c: &counters{}}
	other := &Job{ID: "other", user: "bob", State: JobRunning, Priority: 2, c: &counters{}}
	m.jobs.list = []*Job{a, b, other}
	if !m.jobTurn(a) || m.jobTurn(b) || !m.jobTurn(other) {
		t.Fatal("FIFO/account isolation")
	}
	if err := m.ControlJob("bob", "a", true, 0, 0); !errors.Is(err, ErrNoJob) {
		t.Fatal(err)
	}
	if err := m.ControlJob("alex", "b", false, 2, 0); err != nil {
		t.Fatal(err)
	}
	if m.jobTurn(a) || !m.jobTurn(b) {
		t.Fatal("priority did not preempt")
	}
	if err := m.ControlJob("alex", "b", true, 2, 0); err != nil {
		t.Fatal(err)
	}
	if !m.jobTurn(a) || m.jobTurn(b) {
		t.Fatal("paused job blocked queue")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := m.copyCheckpoint(ctx, b, 0); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := m.ControlJob("alex", "a", false, 0, 1<<20); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := m.copyCheckpoint(context.Background(), a, 100<<10); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 90*time.Millisecond {
		t.Fatal("rate limit ignored")
	}
	for _, rate := range []int64{-1, (1024 << 20) + 1} {
		if err := m.ControlJob("alex", "a", false, 0, rate); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
}
func TestQueuedMoveCancellationRetainsSourceAndResumePublishes(t *testing.T) {
	m, src, dst := twoRoots(t)
	blocker := &Job{ID: "blocker", user: "alex", State: JobRunning, c: &counters{}, cancel: func() {}}
	m.jobs.list = []*Job{blocker}
	job, err := m.StartCopy(src, []string{"notiz.txt"}, dst, "ziel", true, "rename", "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	if err = m.CancelJob("alex", job.ID); err != nil {
		t.Fatal(err)
	}
	if end := finish(t, m, job.ID); end.State != JobCancelled {
		t.Fatal(end)
	}
	if _, err = os.Stat(filepath.Join(src.Dir, "notiz.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dst.Dir, "ziel/notiz.txt")); !os.IsNotExist(err) {
		t.Fatal("queued move wrote target")
	}
	if err = m.ControlJob("alex", blocker.ID, true, 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = m.RetryCopy("alex", job.ID, src, dst, os.Getuid()); err != nil {
		t.Fatal(err)
	}
	if end := finish(t, m, job.ID); end.State != JobDone {
		t.Fatal(end)
	}
	if read(t, filepath.Join(dst.Dir, "ziel/notiz.txt")) != "notiz" {
		t.Fatal("copy content")
	}
	if _, err = os.Stat(filepath.Join(src.Dir, "notiz.txt")); !os.IsNotExist(err) {
		t.Fatal("move did not finish")
	}
}

func TestMoveRetainsFilesAddedWhileCopyWasInProgress(t *testing.T) {
	m, src, dst := twoRoots(t)
	m.syncFile = func(f *os.File) error {
		if err := os.WriteFile(filepath.Join(src.Dir, "urlaub/new.txt"), []byte("new data"), 0600); err != nil {
			return err
		}
		return f.Sync()
	}
	job, err := m.StartCopy(src, []string{"urlaub"}, dst, "ziel", true, "rename", "alex", os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	end := finish(t, m, job.ID)
	if end.State != JobFailed {
		t.Fatal(end)
	}
	if read(t, filepath.Join(src.Dir, "urlaub/new.txt")) != "new data" {
		t.Fatal("new source data lost")
	}
	if read(t, filepath.Join(src.Dir, "urlaub/strand.jpg")) != "strand" {
		t.Fatal("original removed")
	}
}
