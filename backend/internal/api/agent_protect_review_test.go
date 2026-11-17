package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/protect"
	"github.com/ra5on/TitanOS/backend/internal/shares"
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

// Review 1.02: a waiting request and a running grant kept the shares of
// the moment of asking. Taking a share out of the person's entry gave it
// anyway after the waiting time, and a running grant kept it 30 days.
func TestEmergencyFollowsNarrowedShares(t *testing.T) {
	env := protectServer(t)
	ts := env.ts
	both := map[string]any{"enabled": true, "persons": []map[string]any{{"account": "lena", "waitDays": 3, "shares": []string{"Documents", "Fotos"}}}}
	only := map[string]any{"enabled": true, "persons": []map[string]any{{"account": "lena", "waitDays": 3, "shares": []string{"Documents"}}}}
	env.features(t, map[string]any{"emergency": both})
	lena := env.login(t, "lena")
	ask := func() protect.Request {
		res, body := ts.do(lena, "POST", "/api/v1/agent/protect/emergency/request", map[string]any{"reason": "Notfall"})
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("request: %d %s", res.StatusCode, body)
		}
		var req protect.Request
		json.Unmarshal(body, &req)
		return req
	}
	// Waiting: narrowed before the waiting time is over.
	ask()
	env.features(t, map[string]any{"emergency": only})
	env.set(env.clock().Add(72 * time.Hour))
	ts.server.emergencyTick(ts.server.agentSettings().Features.Emergency, env.clock())
	if _, has := env.access("Fotos")["lena"]; has {
		t.Fatalf("a share taken out was granted: %v", env.access("Fotos"))
	}
	got, _ := ts.server.emergencyLatest("lena")
	_ = ts.server.emergencyEnd(got, "alex", "test", env.clock())

	// Granted: narrowed while it runs.
	env.features(t, map[string]any{"emergency": both})
	req := ask()
	if res, body := ts.request("POST", "/api/v1/agent/protect/emergency/"+req.ID+"/grant", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("grant now: %d %s", res.StatusCode, body)
	}
	if env.access("Fotos")["lena"] != shares.ReadOnly {
		t.Fatal("not granted")
	}
	env.features(t, map[string]any{"emergency": only})
	ts.server.emergencyTick(ts.server.agentSettings().Features.Emergency, env.clock())
	if _, has := env.access("Fotos")["lena"]; has {
		t.Fatalf("a share taken out stayed granted: %v", env.access("Fotos"))
	}
	if got, _ := ts.server.emergencyLatest("lena"); got.State != protect.StateGranted {
		t.Fatalf("the rest of the grant ended: %+v", got)
	}
}

// Review 1.02: the handbook kept contacts after the owner switched them off
// (or changed a number) until it was made again at night; a person with
// the emergency access read them in the meantime.
func TestHandbookFollowsContactSettings(t *testing.T) {
	env := protectServer(t)
	ts := env.ts
	env.features(t, map[string]any{"handbook": map[string]any{"enabled": true, "withContacts": true, "contacts": []map[string]any{{"name": "Onkel Peter", "phone": "0171 123456"}}}})
	if _, body := ts.request("GET", "/api/v1/agent/protect/handbook", nil); !strings.Contains(string(body), "0171 123456") {
		t.Fatalf("no contact: %s", body)
	}
	env.features(t, map[string]any{"handbook": map[string]any{"enabled": true, "withContacts": true, "contacts": []map[string]any{{"name": "Onkel Peter", "phone": "0171 999999"}}}})
	if _, body := ts.request("GET", "/api/v1/agent/protect/handbook", nil); strings.Contains(string(body), "0171 123456") || !strings.Contains(string(body), "0171 999999") {
		t.Fatalf("old number: %s", body)
	}
	env.features(t, map[string]any{"handbook": map[string]any{"enabled": true, "withContacts": false, "contacts": []map[string]any{{"name": "Onkel Peter", "phone": "0171 999999"}}}})
	for _, p := range []string{"/api/v1/agent/protect/handbook", "/api/v1/agent/protect/handbook/download?format=md"} {
		if _, body := ts.request("GET", p, nil); strings.Contains(string(body), "Onkel Peter") {
			t.Fatalf("%s: contact after switching off: %s", p, body)
		}
	}
}

// A-7 (1.03): a changed waiting time holds for a request that is already
// waiting, shorter and longer, counted from the request.
func TestEmergencyWaitingTimeFollowsTheSetting(t *testing.T) {
	env := protectServer(t)
	ts := env.ts
	person := func(days int) map[string]any {
		return map[string]any{"enabled": true, "persons": []map[string]any{{"account": "lena", "waitDays": days, "shares": []string{"Documents"}}}}
	}
	env.features(t, map[string]any{"emergency": person(7)})
	lena := env.login(t, "lena")
	res, body := ts.do(lena, "POST", "/api/v1/agent/protect/emergency/request", map[string]any{"reason": "Notfall"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("request: %d %s", res.StatusCode, body)
	}
	var req protect.Request
	json.Unmarshal(body, &req)
	tick := func() {
		ts.server.emergencyTick(ts.server.agentSettings().Features.Emergency, env.clock())
	}
	// Longer: after 2 days of 7 the owner sets 10; day 8 still waits.
	env.features(t, map[string]any{"emergency": person(10)})
	tick()
	if got, _ := ts.server.emergencyLatest("lena"); !got.Due.Equal(req.Created.Add(10*24*time.Hour)) || got.State != protect.StateWaiting {
		t.Fatalf("longer: %+v", got)
	}
	env.set(req.Created.Add(8 * 24 * time.Hour))
	tick()
	if got, _ := ts.server.emergencyLatest("lena"); got.State != protect.StateWaiting {
		t.Fatalf("granted before the longer time: %+v", got)
	}
	// Shorter: 1 day; the time is over already, so the next tick grants.
	env.features(t, map[string]any{"emergency": person(1)})
	tick()
	tick()
	if got, _ := ts.server.emergencyLatest("lena"); got.State != protect.StateGranted {
		t.Fatalf("not granted after the shorter time: %+v", got)
	}
}

// A-9 (1.04): a share form opened before the guard locked the share does
// not lift the lock when it is saved. A save that keeps the share
// read-only goes through, and after reopening the old form saves again.
func TestGuardLockSurvivesAnOldShareForm(t *testing.T) {
	env := protectServer(t)
	ts := env.ts
	before, _ := ts.server.shareByName("Documents")
	env.features(t, map[string]any{"guard": map[string]any{"enabled": true}})
	ts.server.agentProtectTick(env.clock())
	env.attack(t, 40, true)
	a := env.waitAlert(t)
	if !a.Locked {
		t.Fatalf("not locked: %+v", a)
	}
	if res, body := ts.request("PUT", "/api/v1/shares/Documents", before); res.StatusCode != http.StatusConflict || !strings.Contains(string(body), "share_guard_locked") {
		t.Fatalf("old form saved: %d %s", res.StatusCode, body)
	}
	for user, acc := range env.access("Documents") {
		if acc != shares.ReadOnly {
			t.Fatalf("%s may write again: %v", user, env.access("Documents"))
		}
	}
	now, _ := ts.server.shareByName("Documents")
	now.Browseable = !now.Browseable
	if res, body := ts.request("PUT", "/api/v1/shares/Documents", now); res.StatusCode != http.StatusNoContent {
		t.Fatalf("read-only save refused: %d %s", res.StatusCode, body)
	}
	env.set(env.clock().Add(5 * time.Minute))
	ts.server.agentProtectTick(env.clock())
	if res, body := ts.request("POST", "/api/v1/agent/protect/guard/"+a.ID+"/reopen", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("reopen: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/shares/Documents", before); res.StatusCode != http.StatusNoContent {
		t.Fatalf("old form after reopening: %d %s", res.StatusCode, body)
	}
}

// A-9 (1.04): the sync of the share KI-Agent, every minute, kept the
// access it wants and so lifted a lock of the guard on that share.
func TestGuardLockOfTheAgentShareStays(t *testing.T) {
	ts, _ := docsServer(t)
	sh, ok := ts.server.agentShare()
	if !ok || sh.Access["alex"] != shares.ReadWrite {
		t.Fatalf("share: %v %+v", ok, sh)
	}
	ps := ts.server.protect()
	ps.accessMu.Lock()
	locked := map[string]string{}
	for name := range sh.Access {
		locked[name] = shares.ReadOnly
	}
	sh.Access = locked
	if err := ts.server.updateShare(sh); err != nil {
		t.Fatal(err)
	}
	ps.mu.Lock()
	ps.alerts, ps.loaded = append(ps.alerts, guardAlert{ID: "x", Share: sh.Name, State: "stopped", Locked: true, Saved: map[string]string{"alex": shares.ReadWrite}}), true
	ps.mu.Unlock()
	ps.accessMu.Unlock()
	ts.server.agentSyncShare()
	if now, _ := ts.server.shareByName(sh.Name); now.Access["alex"] != shares.ReadOnly {
		t.Fatalf("the sync lifted the lock: %v", now.Access)
	}
	// Reopened in the guard, the next sync gives write access back.
	ps.mu.Lock()
	ps.alerts[len(ps.alerts)-1].Locked = false
	ps.mu.Unlock()
	ts.server.agentSyncShare()
	if now, _ := ts.server.shareByName(sh.Name); now.Access["alex"] != shares.ReadWrite {
		t.Fatalf("no write access after reopening: %v", now.Access)
	}
}
