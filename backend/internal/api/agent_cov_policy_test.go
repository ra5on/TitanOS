package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// The policy as the toolbox applies it to real calls: preset × area ×
// tool × folder rule. A denied folder beats every allow; a folder rule
// that allows never loosens a stricter tool or area.
func TestCovAgentPrepareFolderBeatsArea(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetStandard})
	dir := ts.withShare()
	os.MkdirAll(filepath.Join(dir, "Privat"), 0o755)
	os.WriteFile(filepath.Join(dir, "Privat", "geheim.txt"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(dir, "Offen"), 0o755)
	os.WriteFile(filepath.Join(dir, "Offen", "a.txt"), []byte("a"), 0o644)
	user := mustUser(t, ts, "alex")
	ctx := context.Background()

	type result struct {
		mode string
		err  bool
	}
	cases := []struct {
		name   string
		policy agent.Policy
		tool   string
		args   map[string]any
		want   result
	}{
		{"area allow, folder deny", agent.Policy{Preset: agent.PresetStandard, Areas: map[string]string{"files": agent.ModeAllow},
			Folders: []agent.FolderRule{{Root: "share/Docs", Path: "Privat", Mode: agent.ModeDeny}}},
			"file_read", map[string]any{"root": "share/Docs", "path": "Privat/geheim.txt"}, result{err: true}},
		{"tool allow, folder deny", agent.Policy{Preset: agent.PresetAll, Tools: map[string]string{"file_read": agent.ModeAllow},
			Folders: []agent.FolderRule{{Root: "share/Docs", Path: "Privat", Mode: agent.ModeDeny}}},
			"file_read", map[string]any{"root": "share/Docs", "path": "Privat/geheim.txt"}, result{err: true}},
		{"folder deny through the volume root", agent.Policy{Preset: agent.PresetAll,
			Folders: []agent.FolderRule{{Root: "share/Docs", Path: "Privat", Mode: agent.ModeDeny}}},
			"file_read", map[string]any{"root": "volume/tank", "path": "Docs/Privat/geheim.txt"}, result{err: true}},
		{"folder deny leaves siblings", agent.Policy{Preset: agent.PresetAll,
			Folders: []agent.FolderRule{{Root: "share/Docs", Path: "Privat", Mode: agent.ModeDeny}}},
			"file_read", map[string]any{"root": "share/Docs", "path": "Offen/a.txt"}, result{mode: agent.ModeAllow}},
		{"preset all, folder ask", agent.Policy{Preset: agent.PresetAll,
			Folders: []agent.FolderRule{{Root: "share/Docs", Path: "Offen", Mode: agent.ModeAsk}}},
			"file_read", map[string]any{"root": "share/Docs", "path": "Offen/a.txt"}, result{mode: agent.ModeAsk}},
		{"area deny, folder allow", agent.Policy{Preset: agent.PresetAll, Areas: map[string]string{"files": agent.ModeDeny},
			Folders: []agent.FolderRule{{Root: "share/Docs", Path: "", Mode: agent.ModeAllow}}},
			"file_read", map[string]any{"root": "share/Docs", "path": "Offen/a.txt"}, result{err: true}},
		{"tool ask, folder allow", agent.Policy{Preset: agent.PresetAll, Tools: map[string]string{"file_read": agent.ModeAsk},
			Folders: []agent.FolderRule{{Root: "share/Docs", Path: "Offen", Mode: agent.ModeAllow}}},
			"file_read", map[string]any{"root": "share/Docs", "path": "Offen/a.txt"}, result{mode: agent.ModeAsk}},
		{"read preset denies", agent.Policy{Preset: agent.PresetRead},
			"files_delete", map[string]any{"root": "share/Docs", "paths": []string{"Offen/a.txt"}}, result{err: true}},
		// Replacing a file is a change: standard asks, a folder allow does not loosen it.
		{"standard replace asks, folder allow", agent.Policy{Preset: agent.PresetStandard,
			Folders: []agent.FolderRule{{Root: "share/Docs", Path: "Offen", Mode: agent.ModeAllow}}},
			"file_write", map[string]any{"root": "share/Docs", "path": "Offen/a.txt", "content": "neu", "replace": true}, result{mode: agent.ModeAsk}},
		{"standard new file free", agent.Policy{Preset: agent.PresetStandard},
			"file_write", map[string]any{"root": "share/Docs", "path": "Offen/neu.txt", "content": "neu"}, result{mode: agent.ModeAllow}},
		{"new file in denied folder", agent.Policy{Preset: agent.PresetAll,
			Folders: []agent.FolderRule{{Root: "share/Docs", Path: "Privat", Mode: agent.ModeDeny}}},
			"file_write", map[string]any{"root": "share/Docs", "path": "Privat/neu.txt", "content": "neu"}, result{err: true}},
		// The agent's own tools stay allowed whatever is set.
		{"memory under read preset", agent.Policy{Preset: agent.PresetRead, Tools: map[string]string{"memory_save": agent.ModeDeny}},
			"memory_save", map[string]any{"text": "Der Drucker steht im Flur."}, result{mode: agent.ModeAllow}},
	}
	for _, c := range cases {
		settings := ts.server.agentSettings()
		settings.Policy = c.policy
		box := ts.server.newAgentToolbox(user, settings)
		if _, ok := box.tools[c.tool]; !ok {
			t.Errorf("%s: no tool %s", c.name, c.tool)
			continue
		}
		args, _ := json.Marshal(c.args)
		p, err := box.Prepare(ctx, &agent.Chat{ID: agent.NewID()}, c.tool, args)
		if c.want.err {
			if err == nil {
				t.Errorf("%s: allowed with mode %s", c.name, p.Mode)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if p.Mode != c.want.mode {
			t.Errorf("%s: mode %s, want %s", c.name, p.Mode, c.want.mode)
		}
	}
}

// Web tools ask once the chat has read content from outside, even when
// the area allows them.
func TestCovAgentPrepareTaintedWebAsks(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetAll, Areas: map[string]string{agent.AreaWeb: agent.ModeAllow}})
	box := ts.server.newAgentToolbox(mustUser(t, ts, "alex"), ts.server.agentSettings())
	if _, ok := box.tools["web_search"]; !ok {
		t.Skip("web tools not offered in this setup")
	}
	ctx := context.Background()
	args := json.RawMessage(`{"query":"titanos"}`)
	p, err := box.Prepare(ctx, &agent.Chat{ID: agent.NewID()}, "web_search", args)
	if err != nil || p.Mode != agent.ModeAllow {
		t.Fatalf("clean chat: %+v %v", p, err)
	}
	p, err = box.Prepare(ctx, &agent.Chat{ID: agent.NewID(), Tainted: true}, "web_search", args)
	if err != nil || p.Mode != agent.ModeAsk {
		t.Fatalf("tainted chat: %+v %v", p, err)
	}
}

// Home Assistant rules on top of the tool mode: an entity rule that allows
// never loosens what the preset asks for, a deny beats preset all, a lock
// asks unless the person allowed it.
func TestCovAgentHARulesCombineWithPolicy(t *testing.T) {
	call := func(policy agent.Policy, rules map[string]string, name string, args any) string {
		box := &agentToolbox{settings: agentSettings{Policy: policy}}
		box.settings.HA.Entities = rules
		data, _ := json.Marshal(args)
		base := policy.Mode(name, "home", agent.KindChange)
		// As Prepare combines them.
		return agent.Stricter(base, box.haMode(name, data, base))
	}
	light := map[string]any{"domain": "light", "service": "turn_on", "entities": []string{"light.flur"}}
	lock := map[string]any{"domain": "lock", "service": "unlock", "entities": []string{"lock.haustuer"}}
	cases := []struct {
		name   string
		policy agent.Policy
		rules  map[string]string
		args   any
		want   string
	}{
		{"standard light asks", agent.Policy{Preset: agent.PresetStandard}, nil, light, agent.ModeAsk},
		{"entity allow does not loosen standard", agent.Policy{Preset: agent.PresetStandard}, map[string]string{"entity:light.flur": agent.ModeAllow}, light, agent.ModeAsk},
		{"all light free", agent.Policy{Preset: agent.PresetAll}, nil, light, agent.ModeAllow},
		{"entity deny beats all", agent.Policy{Preset: agent.PresetAll}, map[string]string{"entity:light.flur": agent.ModeDeny}, light, agent.ModeDeny},
		{"domain deny beats all", agent.Policy{Preset: agent.PresetAll}, map[string]string{"domain:light": agent.ModeDeny}, light, agent.ModeDeny},
		{"lock asks under all", agent.Policy{Preset: agent.PresetAll}, nil, lock, agent.ModeAsk},
		{"lock allowed by the person", agent.Policy{Preset: agent.PresetAll}, map[string]string{"domain:lock": agent.ModeAllow}, lock, agent.ModeAllow},
		{"tool allow beats area deny", agent.Policy{Preset: agent.PresetRead, Tools: map[string]string{"ha_call": agent.ModeAllow}, Areas: map[string]string{"home": agent.ModeDeny}}, nil, light, agent.ModeAllow},
		{"area deny", agent.Policy{Preset: agent.PresetAll, Areas: map[string]string{"home": agent.ModeDeny}}, nil, light, agent.ModeDeny},
		{"several entities: strictest", agent.Policy{Preset: agent.PresetAll}, map[string]string{"entity:light.a": agent.ModeAllow, "entity:light.b": agent.ModeDeny},
			map[string]any{"domain": "light", "service": "turn_on", "entities": []string{"light.a", "light.b"}}, agent.ModeDeny},
		{"comma list", agent.Policy{Preset: agent.PresetAll}, map[string]string{"entity:light.b": agent.ModeAsk},
			map[string]any{"domain": "light", "service": "turn_on", "entity_id": "light.a, light.b"}, agent.ModeAsk},
	}
	for _, c := range cases {
		if got := call(c.policy, c.rules, "ha_call", c.args); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	// Invalid rule values are ignored, not treated as allow.
	if got := call(agent.Policy{Preset: agent.PresetStandard}, map[string]string{"entity:light.flur": "sometimes"}, "ha_call", light); got != agent.ModeAsk {
		t.Fatalf("invalid rule: %s", got)
	}
}

// ruleFor in haMode prefers an entity rule over the domain rule, but the
// call's own "domain" field is checked again on its own, so a domain deny
// always wins over an entity exception.
func TestCovAgentHAEntityExceptionInDeniedDomain(t *testing.T) {
	box := &agentToolbox{settings: agentSettings{Policy: agent.Policy{Preset: agent.PresetAll}}}
	box.settings.HA.Entities = map[string]string{"domain:light": agent.ModeDeny, "entity:light.flur": agent.ModeAsk}
	data, _ := json.Marshal(map[string]any{"domain": "light", "service": "turn_on", "entities": []string{"light.flur"}})
	if got := box.haMode("ha_call", data, agent.ModeAllow); got != agent.ModeAsk {
		t.Fatalf("entity exception: %s", got)
	}
}
