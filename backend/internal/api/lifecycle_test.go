package api

import (
	"context"
	"testing"
	"time"
)

func TestCloseWaitsForJobs(t *testing.T) {
	ts := newTestServer(t)
	s := ts.server
	ended := make(chan struct{})
	s.goJob(func(ctx context.Context) {
		<-ctx.Done()
		time.Sleep(20 * time.Millisecond)
		close(ended)
	})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	default:
		t.Fatal("Close returned before the job ended")
	}
}

func TestStartTiesRootContext(t *testing.T) {
	ts := newTestServer(t)
	s := New(ts.server.opts)
	parent, cancel := context.WithCancel(context.Background())
	if err := s.Start(parent); err != nil {
		t.Fatal(err)
	}
	if s.ctx.Err() != nil {
		t.Fatal("root context ended before the parent")
	}
	cancel()
	select {
	case <-s.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("root context did not end with the parent")
	}
	s.Close()
}

// New must not read settings; the stand-ins are loaded by Start.
func TestNewLeavesLoadingToStart(t *testing.T) {
	ts := newTestServer(t)
	opts := ts.server.opts
	opts.WireGuard, opts.Fans, opts.HealthReport, opts.WebPush, opts.IPv64 = nil, nil, nil, nil, nil
	s := New(opts)
	if len(s.pending) != 5 {
		t.Fatalf("pending loads = %d, want 5", len(s.pending))
	}
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(s.pending) != 0 {
		t.Fatal("Start left loads pending")
	}
	s.Close()
}
