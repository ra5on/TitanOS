package agent

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// TECHNIK 6.4: preset × dataRights × tool → mode. The same table drives
// web/tests/agent.test.mjs, so page and backend decide alike.
func TestDataRightsMatrix(t *testing.T) {
	data, err := os.ReadFile("testdata/data-rights.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Tools []struct{ Name, Area, Kind string }
		Cases []struct {
			Name   string
			Policy Policy
			Modes  map[string]string
		}
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Cases) < 20 || len(m.Tools) != 9 {
		t.Fatalf("matrix too small: %d cases, %d tools", len(m.Cases), len(m.Tools))
	}
	for _, c := range m.Cases {
		if err := c.Policy.Check(); err != nil {
			t.Errorf("%s: Check: %v", c.Name, err)
		}
		for _, tool := range m.Tools {
			if got := c.Policy.Mode(tool.Name, tool.Area, tool.Kind); got != c.Modes[tool.Name] {
				t.Errorf("%s: %s = %s, want %s", c.Name, tool.Name, got, c.Modes[tool.Name])
			}
		}
	}
}

// The standard preset itself is unchanged for every other area: creating
// stays free there (presetMode), only calendar and contacts ask.
func TestDataRightsLeavePresetModeAlone(t *testing.T) {
	p := Policy{Preset: PresetStandard}
	if got := p.Mode("folder_create", "files", KindCreate); got != ModeAllow {
		t.Errorf("folder_create: %s", got)
	}
	if got := p.Mode("calendar_create", AreaCalendar, KindCreate); got != ModeAsk {
		t.Errorf("calendar create kind: %s", got)
	}
	// A tool setting never opens these areas (the page shows no single tools).
	p.Tools = map[string]string{"calendar_delete": ModeAllow}
	if got := p.Mode("calendar_delete", AreaCalendar, KindChange); got != ModeAsk {
		t.Errorf("tool setting counted: %s", got)
	}
}

func TestDataRightsCheck(t *testing.T) {
	bad := []Policy{
		{Preset: PresetStandard, Areas: map[string]string{AreaCalendar: ModeAllow}},
		{Preset: PresetStandard, Areas: map[string]string{AreaContacts: ModeDeny}},
		{DataRights: map[string]DataRight{"files": {Read: ModeAllow, Write: ModeAllow}}},
		{DataRights: map[string]DataRight{AreaCalendar: {Read: ModeAsk, Write: ModeAllow}}},
		{DataRights: map[string]DataRight{AreaCalendar: {Read: ModeAllow, Write: "maybe"}}},
		{DataRights: map[string]DataRight{AreaCalendar: {Read: ModeAllow}}},
		{DataRights: map[string]DataRight{AreaContacts: {Write: ModeAsk}}},
	}
	for i, p := range bad {
		if p.Check() == nil {
			t.Errorf("case %d accepted: %+v", i, p)
		}
	}
	good := Policy{Preset: PresetRead, DataRights: map[string]DataRight{
		AreaCalendar: {Read: ModeAllow, Write: ModeAsk}, AreaContacts: {Read: ModeDeny, Write: ModeDeny}}}
	if err := good.Check(); err != nil {
		t.Error(err)
	}
}

func TestDataAreasPlaceAndAreaMode(t *testing.T) {
	i := slices.Index(AgentAreas, "files")
	if i < 0 || len(AgentAreas) < i+3 || AgentAreas[i+1] != AreaCalendar || AgentAreas[i+2] != AreaContacts {
		t.Fatalf("calendar and contacts must follow files: %v", AgentAreas)
	}
	if slices.Contains(newAreas, AreaCalendar) || slices.Contains(newAreas, AreaContacts) {
		t.Fatal("calendar and contacts are no extraAreas")
	}
	closed := Policy{DataRights: map[string]DataRight{AreaCalendar: {Read: ModeDeny, Write: ModeDeny}}}
	if got := closed.AreaMode(AreaCalendar); got != ModeDeny {
		t.Errorf("both denied: %q", got)
	}
	if got := closed.AreaMode(AreaContacts); got != "" {
		t.Errorf("contacts from the preset: %q", got)
	}
	half := Policy{DataRights: map[string]DataRight{AreaCalendar: {Read: ModeDeny, Write: ModeAsk}}}
	if got := half.AreaMode(AreaCalendar); got != "" {
		t.Errorf("writing open: %q", got)
	}
	if got := (Policy{Preset: PresetRead}).AreaMode(AreaContacts); got != "" {
		t.Errorf("read preset still reads: %q", got)
	}
}

// ---- Rollback 1.11 → 1.10 (TECHNIK 6.4) ----

// policy110 is the Policy of 1.10 with its JSON methods, copied from
// main (1b2292f4) for this test only.
type policy110 struct {
	Preset  string
	Areas   map[string]string
	Tools   map[string]string
	Folders []FolderRule
	WebFree bool
}

var agentAreas110 = []string{"system", "files", "docker", "vms", "backups", "updates", "storage", "shares", "network", "accounts", "plugins", "home", AreaHistory, AreaNVR, AreaPhotos, AreaWeb, AreaCode, AreaMCP}

type policyJSON110 struct {
	Preset     string            `json:"preset"`
	Areas      map[string]string `json:"areas,omitempty"`
	ExtraAreas map[string]string `json:"extraAreas,omitempty"`
	Tools      map[string]string `json:"tools,omitempty"`
	Folders    []FolderRule      `json:"folders,omitempty"`
	WebFree    bool              `json:"webFree,omitempty"`
}

func (p policy110) MarshalJSON() ([]byte, error) {
	out := policyJSON110{Preset: p.Preset, Tools: p.Tools, Folders: p.Folders, WebFree: p.WebFree}
	for a, m := range p.Areas {
		target := &out.Areas
		if slices.Contains([]string{AreaHistory, AreaNVR}, a) {
			target = &out.ExtraAreas
		}
		if *target == nil {
			*target = map[string]string{}
		}
		(*target)[a] = m
	}
	return json.Marshal(out)
}

func (p *policy110) UnmarshalJSON(data []byte) error {
	var in policyJSON110
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	*p = policy110{Preset: in.Preset, Areas: in.Areas, Tools: in.Tools, Folders: in.Folders, WebFree: in.WebFree}
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

// check110 is what 1.10's checkAgentSettings does with the areas.
func (p policy110) check110() error {
	for a, m := range p.Areas {
		if !ValidMode(m) || !slices.Contains(agentAreas110, a) {
			return os.ErrInvalid
		}
	}
	return nil
}

func TestDataRightsRollbackTo110(t *testing.T) {
	saved := Policy{Preset: PresetAll, Areas: map[string]string{"files": ModeAsk, AreaNVR: ModeAllow},
		DataRights: map[string]DataRight{AreaCalendar: {Read: ModeAllow, Write: ModeAllow}, AreaContacts: {Read: ModeDeny, Write: ModeDeny}}}
	if err := saved.Check(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	// 1.10 reads the file, the person saves the rights there.
	var old policy110
	if err := json.Unmarshal(data, &old); err != nil {
		t.Fatal(err)
	}
	if err := old.check110(); err != nil {
		t.Fatalf("1.10 refuses to save: %v (%s)", err, data)
	}
	again, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	// Back on 1.11: the field is gone, the preset holds again.
	var back Policy
	if err := json.Unmarshal(again, &back); err != nil {
		t.Fatal(err)
	}
	if back.DataRights != nil {
		t.Fatalf("dataRights survived 1.10: %s", again)
	}
	if back.Areas["files"] != ModeAsk || back.Areas[AreaNVR] != ModeAllow {
		t.Errorf("other areas lost: %+v", back.Areas)
	}
	if got := back.Mode("contacts_search", AreaContacts, KindRead); got != ModeAllow {
		t.Errorf("contacts read after rollback: %s", got)
	}
	if got := back.Mode("calendar_create", AreaCalendar, KindChange); got != ModeAllow {
		t.Errorf("preset all writes: %s", got)
	}
	back.Preset = PresetStandard
	if got := back.Mode("calendar_create", AreaCalendar, KindChange); got != ModeAsk {
		t.Errorf("standard asks: %s", got)
	}
	// 1.11 itself keeps the field.
	var kept Policy
	if err := json.Unmarshal(data, &kept); err != nil || kept.DataRights[AreaContacts].Read != ModeDeny {
		t.Fatalf("1.11 lost the field: %v %+v", err, kept)
	}
}
