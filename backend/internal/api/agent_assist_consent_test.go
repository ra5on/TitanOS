package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// S-23 (B-3): an administrator sets "Assist als" and assistData, and can
// fetch the key of Assist. Calendar and contacts hold for Assist only for
// what the acting account agreed to in its own web session: an
// administrator cannot agree for the owner, a withdrawal holds at once,
// a new account of the name has no consent, and agent.json (which 1.10
// reads and saves) does not change.
func TestAssistConsent(t *testing.T) {
	e := newCalEnv(t)
	e.seed("privat", "x.ics", pimSingle("x", "Arzt", pimTomorrow()))
	ts := e.ts
	ada := ts.signIn("ada", auth.RoleAdmin)
	agentFile := filepath.Join(ts.store.Dir(), "system", "agent.json")
	if res, body := ts.request("PUT", "/api/v1/agent/settings", map[string]any{"homeassistant": map[string]any{"assist": true, "assistData": map[string]string{"calendar": "read"}}}); res.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	settingsBefore, _ := os.ReadFile(agentFile)
	if !strings.Contains(string(settingsBefore), "assistData") {
		t.Fatalf("agent.json: %s", settingsBefore)
	}

	allowed := agentSettings{Policy: agent.Policy{Preset: agent.PresetAll},
		HA: agentHASettings{AssistData: map[string]string{"calendar": "read", "contacts": "read"}}}
	offered := func() []string {
		t.Helper()
		profile, ok := ts.server.assistProfile()
		if !ok {
			t.Fatal("no Assist profile")
		}
		var out []string
		for _, s := range ts.server.newAgentToolbox(profile, allowed).Specs(nil) {
			if slices.Contains(pimToolNames, s.Function.Name) {
				out = append(out, s.Function.Name)
			}
		}
		return out
	}
	consent := func(client *http.Client, body any) (int, string) {
		res, data := ts.do(client, "PUT", "/api/v1/agent/assist/consent", body)
		return res.StatusCode, string(data)
	}
	// "Assist als" is empty: the first owner, alex. ada allowed reading.
	if got := offered(); len(got) != 0 {
		t.Fatalf("without the consent of alex: %v", got)
	}
	profile, _ := ts.server.assistProfile()
	if _, err := ts.server.newAgentToolbox(profile, allowed).Prepare(context.Background(), nil, "calendar_search", json.RawMessage(`{}`)); err == nil {
		t.Fatal("calendar_search ran without consent")
	}
	// ada agrees: only for herself – no name in the request reaches alex.
	if code, body := consent(ada, map[string]bool{"calendar": true, "contacts": true}); code != http.StatusOK {
		t.Fatalf("ada: %d %s", code, body)
	}
	if code, _ := consent(ada, map[string]any{"calendar": true, "user": "alex"}); code != http.StatusBadRequest {
		t.Fatalf("a name in the request: %d", code)
	}
	if got := offered(); len(got) != 0 {
		t.Fatalf("ada's consent opened alex's calendar: %v", got)
	}
	// No token, no agent: only a browser session.
	token := ts.mcpToken("ada", "system:read", "system:write")
	req, _ := http.NewRequest("PUT", ts.url+"/api/v1/agent/assist/consent", strings.NewReader(`{"calendar":true}`))
	req.Header.Set("Authorization", "Bearer "+token)
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode == http.StatusOK {
		t.Fatalf("token: %v %v", res, err)
	}
	// The page shows ada who has to agree; the question of Home Assistant
	// only to alex.
	assistNote(assistSeen{At: time.Now(), From: "192.0.2.5", OK: true, Question: "Wann ist mein Arzttermin?"})
	info := func(client *http.Client) map[string]any {
		t.Helper()
		res, body := ts.do(client, "GET", "/api/v1/agent/assist", nil)
		var out map[string]any
		if res.StatusCode != http.StatusOK || json.Unmarshal(body, &out) != nil {
			t.Fatalf("assist: %d %s", res.StatusCode, body)
		}
		return out
	}
	got := info(ada)
	if got["actor"] != "alex" || got["isActor"] != false || got["consent"].(map[string]any)["calendar"] == true || got["myConsent"].(map[string]any)["calendar"] != true {
		t.Fatalf("ada sees %v", got)
	}
	if last := got["last"].(map[string]any); last["question"] != nil {
		t.Fatalf("ada sees the question: %v", last)
	}
	if last := info(e.client)["last"].(map[string]any); last["question"] != "Wann ist mein Arzttermin?" {
		t.Fatalf("alex does not see the question: %v", last)
	}

	// alex agrees to the calendar only: the contacts stay closed.
	if code, body := consent(e.client, map[string]bool{"calendar": true}); code != http.StatusOK {
		t.Fatalf("alex: %d %s", code, body)
	}
	if got := offered(); !slices.Equal(got, []string{"calendar_list", "calendar_search"}) {
		t.Fatalf("with the consent of alex: %v", got)
	}
	_, res := pimRun(t, ts.server.newAgentToolbox(profile, allowed), "calendar_search", `{}`)
	mustHave(t, "assist reads", pimText(t, res), "Arzt")
	// Withdrawn: closed at once.
	if code, body := consent(e.client, map[string]bool{}); code != http.StatusOK {
		t.Fatalf("withdraw: %d %s", code, body)
	}
	if got := offered(); len(got) != 0 {
		t.Fatalf("after withdrawing: %v", got)
	}
	if _, err := os.Stat(filepath.Join(ts.store.Dir(), "users", "alex", "assist-consent.json")); !os.IsNotExist(err) {
		t.Fatalf("consent file after withdrawing: %v", err)
	}

	// "Assist als" ada: her own consent holds for her calendar.
	setAs := func(name string) {
		ts.server.agentConf.mu.Lock()
		ts.server.agentConf.settings.HA.AssistAs = name
		ts.server.agentConf.settings.Users = []string{name}
		ts.server.agentConf.mu.Unlock()
	}
	setAs("ada")
	if got := offered(); len(got) != 3 {
		t.Fatalf("as ada with her consent: %v", got)
	}
	// A new account of the name, with the old file put back (a restore of
	// the stick): no consent.
	kept, _ := os.ReadFile(filepath.Join(ts.store.Dir(), "users", "ada", "assist-consent.json"))
	if err := ts.users.Delete("ada"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	ts.signIn("ada", auth.RoleAdmin)
	os.WriteFile(filepath.Join(ts.store.Dir(), "users", "ada", "assist-consent.json"), kept, 0o600)
	ts.server.assistConsent.mu.Lock()
	ts.server.assistConsent.read = nil // as after a restart
	ts.server.assistConsent.mu.Unlock()
	setAs("ada")
	if got := offered(); len(got) != 0 {
		t.Fatalf("a new ada has the old consent: %v", got)
	}

	// agent.json is as before: 1.10 reads and saves it unchanged.
	if after, _ := os.ReadFile(agentFile); !bytes.Equal(after, settingsBefore) {
		t.Fatalf("agent.json changed:\n%s\n%s", settingsBefore, after)
	}
}
