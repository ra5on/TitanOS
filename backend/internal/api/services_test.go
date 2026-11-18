package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

func TestServicesSwitchVMsAndLXC(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if res, body := ts.request("PUT", "/api/v1/services/vms", map[string]any{"enabled": false}); res.StatusCode != 200 {
		t.Fatalf("switch off: %d %s", res.StatusCode, body)
	}
	_, body := ts.request("GET", "/api/v1/services/state", nil)
	var state map[string]bool
	json.Unmarshal(body, &state)
	if state["vms"] || !state["lxc"] || !state["docker"] {
		t.Fatalf("state: %s", body)
	}
	var stored serviceSettings
	if err := ts.store.ReadJSON(servicesFile, &stored); err != nil || !stored.VMsOff {
		t.Fatalf("stored: %+v %v", stored, err)
	}
	if res, _ := ts.request("POST", "/api/v1/vms/nope/start", nil); res.StatusCode != http.StatusConflict && res.StatusCode != http.StatusNotFound {
		t.Fatalf("start while off: %d", res.StatusCode)
	}
	ts.mcpOn()
	token := ts.mcpToken("alex", "mcp", "vms:read", "vms:write", "system:read")
	names := mcpToolNames(t, ts.mcp(token, "2025-06-18", rpc(1, "tools/list", nil)))
	if slices.Contains(names, "vm_list") || slices.Contains(names, "vm_start") {
		t.Fatalf("VM tools while off: %v", names)
	}
	ts.request("PUT", "/api/v1/services/vms", map[string]any{"enabled": true})
	names = mcpToolNames(t, ts.mcp(token, "2025-06-18", rpc(2, "tools/list", nil)))
	if !slices.Contains(names, "vm_list") {
		t.Fatalf("VM tools after switching on: %v", names)
	}
	if res, _ := ts.request("PUT", "/api/v1/services/printer", map[string]any{"enabled": false}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown service: %d", res.StatusCode)
	}
}

// A new NAS has Docker not set up yet; its tab stays, because that is where
// it is set up. Only switching it off here hides it.
func TestDockerHiddenOnlyWhenSwitchedOff(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if !ts.server.servicesArea().servicesOn()["docker"] {
		t.Fatal("docker hidden on a new NAS")
	}
	if res, body := ts.request("PUT", "/api/v1/services/docker", map[string]any{"enabled": false}); res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if ts.server.servicesArea().servicesOn()["docker"] {
		t.Fatal("docker still shown after switching off")
	}
}

// signIn makes an account of the role and returns a browser signed in as it.
func (ts *testServer) signIn(name string, role auth.Role) *http.Client {
	ts.t.Helper()
	if _, err := ts.users.Create(name, testPassword, role); err != nil {
		ts.t.Fatal(err)
	}
	client := ts.newClient()
	if res, body := ts.do(client, "POST", "/api/v1/login", credentials{Name: name, Password: testPassword}); res.StatusCode != http.StatusOK {
		ts.t.Fatalf("login of %s: %d %s", name, res.StatusCode, body)
	}
	return client
}

// TestServicesStateShowsCalendarAndContacts (TECHNIK 7.5): the menu shows
// T-Kalender and Kontakte when the real /services/state says so – for the
// owner, an administrator and a plain account; a locked account sees
// nothing. The same answer for T-Mail (only with its release) and the
// KI-Agent (on, and the owner or a released account), the lesson of T-Mail
// 1.05–1.08, where a missing key hid a program from everyone.
func TestServicesStateShowsCalendarAndContacts(t *testing.T) {
	ts, _, _ := newMailServer(t) // owner alex
	accounts := map[string]*http.Client{
		"alex": ts.client,
		"ada":  ts.signIn("ada", auth.RoleAdmin),
		"bea":  ts.signIn("bea", auth.RoleUser),
		"dora": ts.signIn("dora", auth.RoleUser),
	}
	state := func(name string) map[string]bool {
		t.Helper()
		res, body := ts.do(accounts[name], "GET", "/api/v1/services/state", nil)
		var out map[string]bool
		if res.StatusCode != http.StatusOK || json.Unmarshal(body, &out) != nil {
			t.Fatalf("state of %s: %d %s", name, res.StatusCode, body)
		}
		return out
	}
	expect := func(step string, want map[string][4]bool) {
		t.Helper()
		for name, w := range want {
			got := state(name)
			for i, key := range []string{"calendar", "contacts", "mail", "agent"} {
				if on, ok := got[key]; !ok || on != w[i] {
					t.Errorf("%s: %s sees %s=%v (present %v), want %v", step, name, key, on, ok, w[i])
				}
			}
		}
	}
	// From the start: calendar and contacts for everyone; T-Mail and the
	// agent for nobody (both off from the start, for the owner too).
	expect("start", map[string][4]bool{
		"alex": {true, true, false, false}, "ada": {true, true, false, false},
		"bea": {true, true, false, false}, "dora": {true, true, false, false},
	})
	// T-Mail released for ada and bea; the agent switched on for bea (the
	// owner always has it, ada was not released).
	ts.grantMail("ada")
	ts.grantMail("bea")
	if res, body := ts.request("PUT", "/api/v1/agent/settings", map[string]any{"enabled": true, "ollama": "http://127.0.0.1:1", "model": "m", "users": []string{"bea"}}); res.StatusCode != http.StatusOK {
		t.Fatalf("agent on: %d %s", res.StatusCode, body)
	}
	ts.server.shareSyncs.Wait()
	expect("released", map[string][4]bool{
		"alex": {true, true, false, true}, "ada": {true, true, true, false},
		"bea": {true, true, true, true}, "dora": {true, true, false, false},
	})
	// Release taken back: T-Mail goes, calendar and contacts stay.
	ts.request("PUT", "/api/v1/users/bea/mail", map[string]bool{"enabled": false})
	expect("mail taken back", map[string][4]bool{"bea": {true, true, false, true}})

	// Locked: the session ends, so the menu gets no answer at all …
	if res, body := ts.request("POST", "/api/v1/users/dora/lock", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("lock: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(accounts["dora"], "GET", "/api/v1/services/state", nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("locked account still asks: %d", res.StatusCode)
	}
	// … and the handler itself, asked with the locked account (a session
	// that would survive the lock), says false for every program.
	dora, _ := ts.users.Get("dora")
	if !dora.Disabled {
		t.Fatal("dora is not locked")
	}
	req := httptest.NewRequest("GET", "/api/v1/services/state", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKey{}, dora))
	rec := httptest.NewRecorder()
	ts.server.servicesArea().handleServicesState(rec, req)
	var locked map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &locked); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"calendar", "contacts", "mail", "agent"} {
		if on, ok := locked[key]; !ok || on {
			t.Errorf("locked account: %s=%v (present %v)", key, on, ok)
		}
	}
}
