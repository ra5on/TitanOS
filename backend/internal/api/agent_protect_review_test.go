package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/protect"
)

// restartProtect stands in for a restart of titand: the guard's RAM
// (detector, incidents) is gone, its file on the volume stays.
func (e *protectEnv) restartProtect(t *testing.T) {
	t.Helper()
	e.ts.server.guardStop()
	protectStates.Delete(e.ts.server)
	ps := e.ts.server.protect()
	ps.source, ps.runner, ps.now = e.src, e.runner, e.clock
	t.Cleanup(func() { e.ts.server.guardStop() })
}

// Review 1.02: an alert saved as "active" stayed active after a restart of
// titand (the incident lived only in RAM): restore and reopen were refused
// for good.
func TestGuardActiveAlertAfterRestart(t *testing.T) {
	env := protectServer(t)
	ts := env.ts
	a := env.stoppedAlert(t, "lock")
	if a.State != "active" {
		t.Fatalf("state %s", a.State)
	}
	time.Sleep(100 * time.Millisecond)
	env.restartProtect(t)
	env.set(env.clock().Add(10 * time.Minute))
	ts.server.agentProtectTick(env.clock())
	if res, body := ts.request("POST", "/api/v1/agent/protect/guard/"+a.ID+"/restore", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("restore after a restart: %d %s", res.StatusCode, body)
	}
}

// A restore that a restart broke off can run again.
func TestGuardRestoringAlertAfterRestart(t *testing.T) {
	env := protectServer(t)
	ts := env.ts
	a := env.stoppedAlert(t, "lock")
	time.Sleep(100 * time.Millisecond)
	env.set(env.clock().Add(5 * time.Minute))
	ts.server.agentProtectTick(env.clock())
	waitState(t, ts, a.ID, "stopped")
	if _, err := ts.server.guardSetState(a.ID, "stopped", "restoring"); err != nil {
		t.Fatal(err)
	}
	env.restartProtect(t)
	if res, body := ts.request("POST", "/api/v1/agent/protect/guard/"+a.ID+"/restore", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("restore after a broken-off one: %d %s", res.StatusCode, body)
	}
}

// An alert whose incident ended before it was written (a slow lock while
// the tick ran) does not stay active either.
func TestGuardAlertWithoutIncidentStops(t *testing.T) {
	env := protectServer(t)
	ts := env.ts
	a := env.stoppedAlert(t, "lock")
	time.Sleep(100 * time.Millisecond)
	ps := ts.server.protect()
	ps.mu.Lock()
	det := ps.detector
	ps.mu.Unlock()
	det.Forget(a.Share)
	ts.server.agentProtectTick(env.clock().Add(time.Minute))
	waitState(t, ts, a.ID, "stopped")
}

// Trimming the list to 20 alerts never drops an open or locked one: the
// saved access of a locked share would be gone.
func TestGuardTrimKeepsLockedAlerts(t *testing.T) {
	env := protectServer(t)
	ps := env.ts.server.protect()
	ps.mu.Lock()
	ps.loaded = true
	ps.alerts = []guardAlert{{ID: "locked", Share: "A", State: "restored", Locked: true, Saved: map[string]string{"lena": "rw"}}, {ID: "open", Share: "B", State: "stopped"}}
	for i := range maxGuardAlerts {
		ps.alerts = append(ps.alerts, guardAlert{ID: fmt.Sprint("x", i), Share: "C", State: "dismissed", Files: []protect.Affected{}})
	}
	env.ts.server.guardSaveLocked(ps)
	ids := map[string]bool{}
	for _, a := range ps.alerts {
		ids[a.ID] = true
	}
	ps.mu.Unlock()
	if !ids["locked"] || !ids["open"] || len(ids) != maxGuardAlerts {
		t.Fatalf("kept %d: %v", len(ids), ids)
	}
}
