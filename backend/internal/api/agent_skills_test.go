package api

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// PLAN 53: the built-in skills only name tools the agent really has, so the
// model is never told to call one that does not exist. Tools of Home
// Assistant come only with a connection; they are listed here.
func TestBuiltInSkillsNameRealTools(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{})
	u, _ := ts.users.Get("alex")
	box := ts.server.newAgentToolbox(u, ts.server.agentSettings())
	// code_run comes only with the sandbox switched on, plan_update is the
	// runner's own tool (agent/context.go).
	known := map[string]bool{"ha_state": true, "ha_call": true, "ha_diagnostics": true, "ha_statistics": true, "ha_history": true, "ha_entities": true, "ha_automation_save": true,
		"code_run": true, "plan_update": true,
		// 1.10: with Home Assistant, the NVR, Immich, the activity log and
		// the monitoring history of a real NAS.
		"ha_logbook": true, "ha_config_read": true, "nvr_events": true, "photos_search": true, "activity_log": true, "monitoring_history": true, "capacity_forecast": true,
		// 1.14: the runner's own tools_load; Mail, the calendar and the
		// contacts of an account that has them (agent_mail.go, agent_pim.go).
		"tools_load": true, "mail_search": true, "mail_list": true, "mail_read": true, "mail_send": true, "mail_draft": true,
		"calendar_list": true, "calendar_free": true, "calendar_create": true, "contacts_search": true, "contacts_create": true}
	// Words of Home Assistant in the steps, no tools.
	for _, w := range []string{"device_class", "garage_door", "alarm_control_panel"} {
		known[w] = true
	}
	for _, x := range box.ordered {
		known[x.name] = true
	}
	word := regexp.MustCompile(`\b[a-z]+_[a-z_]+\b`)
	if len(agent.BuiltInSkills) < 34 {
		t.Fatalf("only %d built-in skills", len(agent.BuiltInSkills))
	}
	seen := map[string]bool{}
	for _, s := range agent.BuiltInSkills {
		if seen[s.ID] {
			t.Errorf("skill %s twice", s.ID)
		}
		seen[s.ID] = true
		for _, name := range word.FindAllString(s.Steps, -1) {
			if !known[name] {
				t.Errorf("skill %q names unknown tool %q", s.Name, name)
			}
		}
	}
}

// 1.10 (V6): the prompt names the skills, skill_read gives the steps – not
// of a skill the person switched off; a skill from outside comes marked
// and makes the chat's web tools ask.
func TestAgentSkillRead(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	settings := ts.server.agentSettings()
	settings.SkillsOff = []string{"builtin-raid"}
	box := ts.server.newAgentToolbox(mustUser(t, ts, "alex"), settings)
	read := func(chat *agent.Chat, name string) (string, error) {
		out, err := box.tools["skill_read"].own(context.Background(), chat, json.RawMessage(`{"name":`+strconv.Quote(name)+`}`))
		return out.Text, err
	}
	text, err := read(&agent.Chat{}, "is everything closed at home?")
	if err != nil || !strings.Contains(text, "device_class window,door,garage_door") {
		t.Fatalf("by name: %q %v", text, err)
	}
	if text, err := read(&agent.Chat{}, "SMART"); err != nil || !strings.Contains(text, "storage_overview") || strings.Contains(text, "hardware_status") {
		t.Fatalf("part of a name: %q %v", text, err)
	}
	if _, err := read(&agent.Chat{}, "Explain the RAID state"); err == nil || !strings.Contains(err.Error(), "skills:") {
		t.Fatalf("a switched-off skill was read: %v", err)
	}
	if err := ts.server.agentStore().SaveSkills("alex", []agent.Skill{{ID: "outside-1", Name: "Fremder Ablauf", When: "nie", Steps: "1. web_search", Untrusted: true}}); err != nil {
		t.Fatal(err)
	}
	chat := &agent.Chat{}
	if text, err := read(chat, "Fremder Ablauf"); err != nil || !strings.HasPrefix(text, "[Outside content") || !chat.OutsideSkill {
		t.Fatalf("outside skill: %q %v %v", text, err, chat.OutsideSkill)
	}
}

// 114-2: every built-in skill in German, with the same tools and the same
// steps (lines) as in English.
func TestBuiltInSkillsGerman(t *testing.T) {
	word := regexp.MustCompile(`\b[a-z]+_[a-z_]+\b`)
	if len(agent.BuiltInSkillsDE) != len(agent.BuiltInSkills) {
		t.Fatalf("%d German, %d skills", len(agent.BuiltInSkillsDE), len(agent.BuiltInSkills))
	}
	for _, s := range agent.BuiltInSkills {
		de, ok := agent.BuiltInSkillsDE[s.ID]
		if !ok {
			t.Errorf("%s has no German text", s.ID)
			continue
		}
		if de.Name == "" || len([]rune(de.Name)) > 80 || de.Name == s.Name || strings.Contains(de.When, "\n") {
			t.Errorf("%s: name %q when %q", s.ID, de.Name, de.When)
		}
		if strings.Count(de.Steps, "\n") != strings.Count(s.Steps, "\n") {
			t.Errorf("%s: %d lines in German, %d in English", s.ID, strings.Count(de.Steps, "\n")+1, strings.Count(s.Steps, "\n")+1)
		}
		have := map[string]bool{}
		for _, w := range word.FindAllString(de.Steps, -1) {
			have[w] = true
		}
		for _, w := range word.FindAllString(s.Steps, -1) {
			if !have[w] {
				t.Errorf("%s: %s missing in German", s.ID, w)
			}
		}
	}
}

// 114-2: the page gets the German text of a built-in skill, not of an own one.
func TestAgentSkillsViewGerman(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{})
	res, body := ts.request("GET", "/api/v1/agent/skills", nil)
	var got struct {
		Skills []struct {
			ID, Name string
			BuiltIn  bool
			DE       *agent.SkillText
		}
	}
	if err := json.Unmarshal(body, &got); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	for _, s := range got.Skills {
		if s.ID == "builtin-health" && (s.Name != "Health check" || s.DE == nil || s.DE.Name != "Gesundheitscheck") {
			t.Fatalf("health: %+v", s)
		}
		if !s.BuiltIn && s.DE != nil {
			t.Fatalf("own skill with German: %+v", s)
		}
	}
}
