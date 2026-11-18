package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// S-24 (B-4): the answer of an unattended run goes to the notify services
// of Home Assistant in full only for the account that chose them (or,
// without a record, the first owner) and the profile of Home Assistant –
// and never when the run read the calendar or the contacts. Everyone
// else's runs and reminders send a fixed text without title or answer.
func TestAgentHANotifyOnlyOwnRunsInFull(t *testing.T) {
	fakeHA := &fakeHAServer{}
	ha := fakeHA.start(t)
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	if res, body := ts.request("PUT", "/api/v1/homeassistant/settings", map[string]any{"enabled": true, "url": ha.URL, "token": "ha-token"}); res.StatusCode != http.StatusOK {
		t.Fatalf("ha: %d %s", res.StatusCode, body)
	}
	ada := ts.signIn("ada", auth.RoleAdmin)
	ts.signIn("bea", auth.RoleUser)
	// ada chooses the services; a notifyBy in the request counts for nothing.
	put := func(client *http.Client, change func(map[string]any)) {
		t.Helper()
		_, body := ts.do(client, "GET", "/api/v1/agent/settings", nil)
		var settings map[string]any
		json.Unmarshal(body, &settings)
		change(settings["homeassistant"].(map[string]any))
		if res, body := ts.do(client, "PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusOK {
			t.Fatalf("settings: %d %s", res.StatusCode, body)
		}
	}
	put(ada, func(h map[string]any) { h["notify"] = []string{"notify.mobile_app_phone"}; h["notifyBy"] = "alex" })
	if by := ts.server.agentSettings().HA.NotifyBy; by != "ada" {
		t.Fatalf("notifyBy %q", by)
	}
	// alex saves the page without changing the list: still ada's.
	put(ts.client, func(h map[string]any) { h["notifyBy"] = "alex" })
	if by := ts.server.agentSettings().HA.NotifyBy; by != "ada" {
		t.Fatalf("notifyBy after a save of alex %q", by)
	}

	last := func() string {
		t.Helper()
		fakeHA.mu.Lock()
		defer fakeHA.mu.Unlock()
		if len(fakeHA.notify) == 0 {
			t.Fatal("nothing sent")
		}
		out := fakeHA.notify[len(fakeHA.notify)-1]
		fakeHA.notify = nil
		return out
	}
	run := func(title, answer string, entries ...agent.Entry) *agent.Chat {
		return &agent.Chat{ID: agent.NewID(), Title: title, State: agent.StateIdle, Origin: "schedule",
			Entries: append(entries, agent.Entry{Role: "assistant", Content: answer})}
	}
	secret := "Zahnarzt Dr. Meier"
	cases := []struct {
		name, user string
		chat       *agent.Chat
		full       bool
	}{
		{"ada's own run", "ada", run("Morgenbericht", "Alles gut, "+secret), true},
		{"alex's run", "alex", run("Arzt "+secret, secret), false},
		{"bea's run", "bea", run("Arzt "+secret, secret), false},
		{"bea's reminder", "bea", &agent.Chat{ID: agent.NewID(), Title: "Arzt " + secret, State: agent.StateIdle, Origin: "reminder",
			Entries: []agent.Entry{{Role: "assistant", Content: "⏰ **Erinnerung:** " + secret}}}, false},
		{"ada's run that read the calendar", "ada", run("Morgenbericht", secret, agent.Entry{Role: "tool", Tool: "calendar_search", OK: true}), false},
		{"ada's run marked pimRead", "ada", func() *agent.Chat { c := run("Morgenbericht", secret); c.PIMRead = true; return c }(), false},
		{"the profile of Home Assistant", agent.AssistProfile, run("Licht", "Licht ist an, "+secret), true},
	}
	for _, c := range cases {
		ts.server.agentHANotify(c.user, c.chat)
		got := last()
		if c.full != strings.Contains(got, secret) {
			t.Errorf("%s: full %v, sent %s", c.name, c.full, got)
		}
		if !c.full && strings.Contains(got, "Arzt") {
			t.Errorf("%s: title of another account's task sent: %s", c.name, got)
		}
	}

	// Without a record (lists from before S-24 or saved by 1.10): the first owner.
	ts.server.agentConf.mu.Lock()
	ts.server.agentConf.settings.HA.NotifyBy, ts.server.agentConf.settings.HA.NotifyByCreated = "", time.Time{}
	ts.server.agentConf.mu.Unlock()
	ts.server.agentHANotify("alex", run("Bericht", secret))
	if got := last(); !strings.Contains(got, secret) {
		t.Errorf("first owner without a record: %s", got)
	}
	ts.server.agentHANotify("ada", run("Bericht", secret))
	if got := last(); strings.Contains(got, secret) {
		t.Errorf("ada without a record: %s", got)
	}
	// A new account of the name is not the one that chose them.
	ts.server.agentConf.mu.Lock()
	ts.server.agentConf.settings.HA.NotifyBy, ts.server.agentConf.settings.HA.NotifyByCreated = "ada", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	ts.server.agentConf.mu.Unlock()
	ts.server.agentHANotify("ada", run("Bericht", secret))
	if got := last(); strings.Contains(got, secret) {
		t.Errorf("other ada: %s", got)
	}
}
