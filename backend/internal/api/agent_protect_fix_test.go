package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/protect"
	"github.com/ra5on/TitanOS/backend/internal/shares"
)

func (e *protectEnv) stoppedAlert(t *testing.T, mode string) guardAlert {
	t.Helper()
	e.features(t, map[string]any{"guard": map[string]any{"enabled": true, "mode": mode}})
	e.ts.server.agentProtectTick(e.clock())
	e.attack(t, 40, true)
	a := e.waitAlert(t)
	return a
}

func waitState(t *testing.T, ts *testServer, id, state string) guardAlert {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a, _ := ts.server.guardAlert(id); a.State == state {
			return a
		}
		time.Sleep(10 * time.Millisecond)
	}
	a, _ := ts.server.guardAlert(id)
	t.Fatalf("state %s, want %s", a.State, state)
	return a
}

// A restore runs once, only from "stopped"; a reopen while it runs stays;
// a false alarm waits; a second restore of a closed alert is refused.
func TestGuardRestoreOnce(t *testing.T) {
	env := protectServer(t)
	ts := env.ts
	a := env.stoppedAlert(t, "lock")
	time.Sleep(100 * time.Millisecond)
	env.set(env.clock().Add(5 * time.Minute))
	ts.server.agentProtectTick(env.clock())
	waitState(t, ts, a.ID, "stopped")

	hold, entered := make(chan struct{}), make(chan struct{}, 1)
	guardRestoreHook = func(string) { entered <- struct{}{}; <-hold }
	t.Cleanup(func() { guardRestoreHook = nil })
	result := make(chan error, 1)
	go func() { _, err := ts.server.guardRestore(a.ID, "alex"); result <- err }()
	<-entered
	if _, err := ts.server.guardRestore(a.ID, "auto"); !errors.Is(err, errGuardNotStopped) {
		t.Fatalf("second restore at once: %v", err)
	}
	if res, _ := ts.request("POST", "/api/v1/agent/protect/guard/"+a.ID+"/dismiss", nil); res.StatusCode != http.StatusConflict {
		t.Fatal("false alarm during the restore")
	}
	if res, body := ts.request("POST", "/api/v1/agent/protect/guard/"+a.ID+"/reopen", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("reopen during the restore: %d %s", res.StatusCode, body)
	}
	close(hold)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	got, _ := ts.server.guardAlert(a.ID)
	if got.State != "restored" || got.Locked || got.Restored == nil || got.Restored.Restored != 40 {
		t.Fatalf("after restore: state %s locked %v %+v", got.State, got.Locked, got.Restored)
	}
	if env.access("Documents")["alex"] != shares.ReadWrite {
		t.Fatal("restore undid the reopen")
	}
	guardRestoreHook = nil
	if res, _ := ts.request("POST", "/api/v1/agent/protect/guard/"+a.ID+"/restore", nil); res.StatusCode != http.StatusConflict {
		t.Fatal("restored twice")
	}
}

// In mode auto the restore runs in the background: the tick returns at once.
func TestGuardAutoRestoreAsync(t *testing.T) {
	env := protectServer(t)
	ts := env.ts
	a := env.stoppedAlert(t, "auto")
	time.Sleep(100 * time.Millisecond)
	hold, entered := make(chan struct{}), make(chan struct{}, 1)
	guardRestoreHook = func(string) { entered <- struct{}{}; <-hold }
	t.Cleanup(func() { guardRestoreHook = nil })
	env.set(env.clock().Add(5 * time.Minute))
	done := make(chan struct{})
	go func() { ts.server.agentProtectTick(env.clock()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the tick waited for the restore")
	}
	<-entered
	waitState(t, ts, a.ID, "restoring")
	close(hold)
	waitState(t, ts, a.ID, "restored")
}

type errSource struct {
	calls atomic.Int32
	panic bool
}

func (e *errSource) Watch(context.Context, []string, chan<- protect.Event) error {
	e.calls.Add(1)
	if e.panic {
		panic("boom")
	}
	return errors.New("fanotify_mark: invalid argument")
}

// A watcher that stopped with an error is tried again after 1, 2, 4 …
// minutes; a panic is caught and counts as an error.
func TestGuardWatchRetry(t *testing.T) {
	for _, panics := range []bool{false, true} {
		env := protectServer(t)
		ts := env.ts
		src := &errSource{panic: panics}
		ts.server.protect().source = src
		env.features(t, map[string]any{"guard": map[string]any{"enabled": true}})
		t0 := env.clock()
		ts.server.agentProtectTick(t0)
		deadline := time.Now().Add(3 * time.Second)
		for ts.server.guardStatusFor(1)["error"] == "" && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if ts.server.guardStatusFor(1)["error"] == "" {
			t.Fatal("no error shown")
		}
		ts.server.agentProtectTick(t0.Add(30 * time.Second))
		time.Sleep(50 * time.Millisecond)
		if src.calls.Load() != 1 {
			t.Fatalf("retried too early: %d", src.calls.Load())
		}
		ts.server.agentProtectTick(t0.Add(61 * time.Second))
		time.Sleep(100 * time.Millisecond)
		if src.calls.Load() != 2 {
			t.Fatalf("not retried: %d", src.calls.Load())
		}
		ps := ts.server.protect()
		ps.mu.Lock()
		b := ps.watchBackoff
		ps.mu.Unlock()
		if b != 2*time.Minute {
			t.Fatalf("backoff: %v", b)
		}
	}
}

// Reading the handbook through the agent's tool is logged like the page.
func TestHandbookToolLogs(t *testing.T) {
	env := protectServer(t)
	ts := env.ts
	log := activity.New(ts.store)
	ts.server.opts.Activity = log
	env.features(t, map[string]any{
		"handbook":  map[string]any{"enabled": true},
		"emergency": map[string]any{"enabled": true, "persons": []map[string]any{{"account": "lena", "shares": []string{"Fotos"}}}},
	})
	lena := env.login(t, "lena")
	_, body := ts.do(lena, "POST", "/api/v1/agent/protect/emergency/request", map[string]any{"reason": "x"})
	id := strings.Split(strings.Split(string(body), `"id":"`)[1], `"`)[0]
	ts.request("POST", "/api/v1/agent/protect/emergency/"+id+"/grant", nil)
	user, _ := ts.users.Get("lena")
	box := ts.server.newAgentToolbox(user, ts.server.agentSettings())
	var tool *agentTool
	for _, x := range box.protectTools() {
		if x.name == "emergency_handbook" {
			tool = x
		}
	}
	if tool == nil {
		t.Fatal("no handbook tool for the granted person")
	}
	if out, err := tool.own(t.Context(), nil, nil); err != nil || !strings.Contains(out.Text, "Notfallhandbuch") {
		t.Fatalf("tool: %v", err)
	}
	entries, _, _, _ := log.Query(t.Context(), activity.Filter{Days: 1, Limit: 100})
	found := false
	for _, e := range entries {
		if e.Action == "emergency handbook read" && e.User == "lena" && strings.Contains(e.Target, "agent") {
			found = true
		}
	}
	if !found {
		t.Fatalf("not logged: %+v", entries)
	}
}

// What an administrator changed on a granted share during the access is
// theirs: the end of the access leaves it.
func TestEmergencyRevokeKeepsAdminChange(t *testing.T) {
	env := protectServer(t)
	ts := env.ts
	env.features(t, map[string]any{"emergency": map[string]any{"enabled": true, "persons": []map[string]any{{"account": "lena", "shares": []string{"Fotos"}}}}})
	lena := env.login(t, "lena")
	grant := func() string {
		_, body := ts.do(lena, "POST", "/api/v1/agent/protect/emergency/request", map[string]any{"reason": "x"})
		id := strings.Split(strings.Split(string(body), `"id":"`)[1], `"`)[0]
		if res, b := ts.request("POST", "/api/v1/agent/protect/emergency/"+id+"/grant", nil); res.StatusCode != http.StatusOK {
			t.Fatalf("grant: %d %s", res.StatusCode, b)
		}
		return id
	}
	setLena := func(acc string) {
		sh, _ := ts.server.shareByName("Fotos")
		if acc == "" {
			delete(sh.Access, "lena")
		} else {
			sh.Access["lena"] = acc
		}
		ts.server.updateShare(sh)
	}
	e := ts.server.agentSettings().Features.Emergency
	// Raised to write access by an administrator.
	id := grant()
	setLena(shares.ReadWrite)
	ts.server.emergencyTick(e, env.clock())
	ts.request("POST", "/api/v1/agent/protect/emergency/"+id+"/end", nil)
	if env.access("Fotos")["lena"] != shares.ReadWrite {
		t.Fatal("end took the administrator's write access")
	}
	// Taken away, then given read access on purpose.
	setLena("")
	id = grant()
	setLena("")
	ts.server.emergencyTick(e, env.clock())
	setLena(shares.ReadOnly)
	ts.request("POST", "/api/v1/agent/protect/emergency/"+id+"/end", nil)
	if env.access("Fotos")["lena"] != shares.ReadOnly {
		t.Fatal("end took the administrator's read access")
	}
	// Unchanged: taken back as before.
	setLena("")
	id = grant()
	ts.server.emergencyTick(e, env.clock())
	ts.request("POST", "/api/v1/agent/protect/emergency/"+id+"/end", nil)
	if _, has := env.access("Fotos")["lena"]; has {
		t.Fatal("own grant not taken back")
	}
}
