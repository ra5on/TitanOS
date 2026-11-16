package nvr

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// A container Docker refuses to start (a port of Frigate taken by an app)
// never ran: that is no crash of Frigate and must not move the detection
// to the next device, and Docker's reason stays on the page.
func TestRefusedStartIsNoCrash(t *testing.T) {
	r := newRig(t, true)
	r.up()
	before := r.m.Status(context.Background()).Detector
	refused := errors.New("docker: driver failed programming external connectivity: Bind for 0.0.0.0:8555 failed: port is already allocated (HTTP 500)")
	r.engine.mu.Lock()
	r.engine.StartError = func() error { return refused }
	r.engine.mu.Unlock()
	// The restream changes the container: it is made anew, and its start
	// is refused, at once and on every later look.
	s := r.m.Settings()
	s.RTSPOut = true
	if err := r.m.SetSettings(context.Background(), s, SetOptions{}); err != nil {
		t.Fatal(err)
	}
	for range r.m.CrashLimit + 3 {
		r.look()
		r.advance(errorBackoff + time.Second)
	}
	st := r.m.Status(context.Background())
	if st.Detector.Failover != nil || st.Detector.Active != before.Active {
		t.Fatalf("a refused start moved the detection: %+v (was %+v)", st.Detector, before)
	}
	if st.Error == nil || !strings.Contains(st.Error.Detail, "port is already allocated") {
		t.Fatalf("Docker's reason is not shown: %+v", st.Error)
	}
	for _, note := range r.noted() {
		if strings.Contains(note, "nvr_failover") || strings.Contains(note, "nvr_stopped") {
			t.Errorf("message about a failover: %s", note)
		}
	}
	// Once the port is free, Frigate starts and runs.
	r.engine.mu.Lock()
	r.engine.StartError = nil
	r.engine.mu.Unlock()
	r.look()
	r.advance(errorBackoff + time.Second)
	r.look()
	if got := r.phase(); got != PhaseRunning {
		t.Fatalf("phase %q after the port was free", got)
	}
}
