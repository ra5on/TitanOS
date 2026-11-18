package agent

import (
	"encoding/json"
	"os"
	"testing"
)

// 1.13: the action rights decide like the table that web/tests/agent.test.mjs
// reads too (preset × dataRights × actionRights × own × read).
func TestActionRightsMatrix(t *testing.T) {
	data, err := os.ReadFile("testdata/data-rights.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Actions struct {
			Keys  []string
			Cases []struct {
				Name   string
				Policy Policy
				Own    map[string]string
				Read   bool
				Modes  map[string]string
			}
		}
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Actions.Keys) != len(ActionKeys) || len(m.Actions.Cases) < 12 {
		t.Fatalf("matrix too small: %d keys, %d cases", len(m.Actions.Keys), len(m.Actions.Cases))
	}
	for i, k := range ActionKeys {
		if m.Actions.Keys[i] != k {
			t.Fatalf("keys differ at %d: %s", i, m.Actions.Keys[i])
		}
	}
	for _, c := range m.Actions.Cases {
		if err := c.Policy.Check(); err != nil {
			t.Errorf("%s: Check: %v", c.Name, err)
		}
		for _, k := range ActionKeys {
			if got := EffectiveAction(c.Policy, c.Own, k, c.Read); got != c.Modes[k] {
				t.Errorf("%s: %s = %s, want %s", c.Name, k, got, c.Modes[k])
			}
		}
	}
}

func TestActionRightsCheck(t *testing.T) {
	for _, bad := range []map[string]string{
		{"mail.read": "ask"}, {"mail.read": "suggest"}, {"event.delete": "allow"}, {"event.delete": "suggest"},
		{"mail.send": "deny"}, {"mail.print": "allow"}, {"event.create": "maybe"},
	} {
		if err := (Policy{Preset: PresetStandard, ActionRights: bad}).Check(); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	ok := map[string]string{"mail.read": "deny", "mail.send": "allow", "event.delete": "ask", "task.complete": "suggest"}
	if err := (Policy{Preset: PresetStandard, ActionRights: ok}).Check(); err != nil {
		t.Error(err)
	}
	// Mail never under areas (1.12 refuses unknown areas when it saves).
	if err := (Policy{Areas: map[string]string{AreaMail: ModeAllow}}).Check(); err == nil {
		t.Error("areas.mail accepted")
	}
	if !ValidArea(AreaMail) {
		t.Error("mail is no area for tools_load and agent_access")
	}
}

// StricterAction never lets "suggest" or an unknown value fall to allow.
func TestStricterAction(t *testing.T) {
	cases := [][3]string{
		{ModeAllow, ModeSuggest, ModeSuggest}, {ModeAsk, ModeSuggest, ModeSuggest}, {ModeSuggest, ModeDeny, ModeDeny},
		{ModeAllow, ModeAsk, ModeAsk}, {ModeAllow, "weird", ModeDeny}, {"", ModeAllow, ModeDeny},
	}
	for _, c := range cases {
		if got := StricterAction(c[0], c[1]); got != c[2] {
			t.Errorf("%s/%s = %s", c[0], c[1], got)
		}
		if got := StricterAction(c[1], c[0]); got != c[2] {
			t.Errorf("%s/%s = %s", c[1], c[0], got)
		}
	}
}

// The gate of the mail area for offering tools.
func TestMailAreaMode(t *testing.T) {
	p := Policy{Preset: PresetStandard}
	if p.Mode("mail_read", AreaMail, KindRead) != ModeAllow || p.Mode("mail_send", AreaMail, KindChange) != ModeAllow {
		t.Error("standard closes mail")
	}
	p.ActionRights = map[string]string{ActMailRead: ModeDeny}
	if p.Mode("mail_read", AreaMail, KindRead) != ModeDeny || p.Mode("mail_send", AreaMail, KindChange) != ModeDeny || p.AreaMode(AreaMail) != ModeDeny {
		t.Error("mail.read deny leaves mail open")
	}
	p = Policy{Preset: PresetRead}
	if p.Mode("mail_read", AreaMail, KindRead) != ModeAllow || p.Mode("mail_send", AreaMail, KindChange) != ModeDeny {
		t.Error("read only")
	}
	// A tool setting never opens mail (the page shows actions, not tools).
	p.Tools = map[string]string{"mail_send": ModeAllow}
	if p.Mode("mail_send", AreaMail, KindChange) != ModeDeny {
		t.Error("tool setting counted for mail")
	}
}

// ---- Rollback 1.13 → 1.12 ----

// policy112 is the Policy of 1.12 with its JSON methods and Check, copied
// from feat/v1.13 before actionRights (b0deaa20) for this test only.
type policy112 struct {
	Preset     string
	Areas      map[string]string
	Tools      map[string]string
	Folders    []FolderRule
	WebFree    bool
	DataRights map[string]DataRight
}

var agentAreas112 = []string{"system", "files", AreaCalendar, AreaContacts, "docker", "vms", "backups", "updates", "storage", "shares", "network", "accounts", "plugins", "home", AreaHistory, AreaNVR, AreaPhotos, AreaWeb, AreaCode, AreaMCP}

type policyJSON112 struct {
	Preset     string               `json:"preset"`
	Areas      map[string]string    `json:"areas,omitempty"`
	ExtraAreas map[string]string    `json:"extraAreas,omitempty"`
	Tools      map[string]string    `json:"tools,omitempty"`
	Folders    []FolderRule         `json:"folders,omitempty"`
	WebFree    bool                 `json:"webFree,omitempty"`
	DataRights map[string]DataRight `json:"dataRights,omitempty"`
}

func (p policy112) MarshalJSON() ([]byte, error) {
	out := policyJSON112{Preset: p.Preset, Tools: p.Tools, Folders: p.Folders, WebFree: p.WebFree, DataRights: p.DataRights}
	for a, m := range p.Areas {
		target := &out.Areas
		if a == AreaHistory || a == AreaNVR {
			target = &out.ExtraAreas
		}
		if *target == nil {
			*target = map[string]string{}
		}
		(*target)[a] = m
	}
	return json.Marshal(out)
}

func (p *policy112) UnmarshalJSON(data []byte) error {
	var in policyJSON112
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	*p = policy112{Preset: in.Preset, Areas: in.Areas, Tools: in.Tools, Folders: in.Folders, WebFree: in.WebFree, DataRights: in.DataRights}
	for a, m := range in.ExtraAreas {
		if p.Areas == nil {
			p.Areas = map[string]string{}
		}
		if _, set := p.Areas[a]; !set {
			p.Areas[a] = m
		}
	}
	return nil
}

// check112 is Check of 1.12 plus the area test of its settings handler.
func (p policy112) check112() error {
	q := Policy{Preset: p.Preset, Areas: p.Areas, Tools: p.Tools, Folders: p.Folders, WebFree: p.WebFree, DataRights: p.DataRights}
	if err := q.Check(); err != nil {
		return err
	}
	for a := range p.Areas {
		found := false
		for _, known := range agentAreas112 {
			found = found || known == a
		}
		if !found {
			return os.ErrInvalid
		}
	}
	return nil
}

func TestActionRightsRollbackTo112(t *testing.T) {
	saved := Policy{Preset: PresetStandard, Areas: map[string]string{"files": ModeAsk, AreaNVR: ModeAllow},
		DataRights:   map[string]DataRight{AreaCalendar: {Read: ModeAllow, Write: ModeAsk}},
		ActionRights: map[string]string{ActMailRead: ModeDeny, ActEventCreate: ModeAllow, ActContactCreate: ModeSuggest}}
	if err := saved.Check(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	// 1.12 reads the file and the person saves the rights there.
	var old policy112
	if err := json.Unmarshal(data, &old); err != nil {
		t.Fatal(err)
	}
	if err := old.check112(); err != nil {
		t.Fatalf("1.12 refuses to save: %v (%s)", err, data)
	}
	if old.DataRights[AreaCalendar].Write != ModeAsk || old.Areas["files"] != ModeAsk {
		t.Fatalf("1.12 lost what it knows: %+v", old)
	}
	again, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	// Back on 1.13: actionRights are gone, the defaults hold again.
	var back Policy
	if err := json.Unmarshal(again, &back); err != nil {
		t.Fatal(err)
	}
	if back.ActionRights != nil {
		t.Fatalf("actionRights survived 1.12: %s", again)
	}
	if got := EffectiveAction(back, nil, ActEventCreate, false); got != ModeAsk {
		t.Errorf("event.create after rollback: %s (the ask chosen under dataRights)", got)
	}
	if got := EffectiveAction(back, nil, ActContactCreate, false); got != ModeSuggest {
		t.Errorf("contact.create after rollback: %s", got)
	}
	if got := EffectiveAction(back, nil, ActMailRead, false); got != ModeAllow {
		t.Errorf("mail.read after rollback: %s (1.12 has no mail tools; on 1.13 it is open again)", got)
	}
	// 1.13 itself keeps the field.
	var kept Policy
	if err := json.Unmarshal(data, &kept); err != nil || kept.ActionRights[ActMailRead] != ModeDeny {
		t.Fatalf("1.13 lost the field: %v %+v", err, kept)
	}
}
