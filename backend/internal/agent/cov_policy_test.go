package agent

import (
	"fmt"
	"strings"
	"testing"
)

// Policy table: a single tool setting beats its area, the area beats the
// preset, and the agent's own tools are always allowed.
func TestCovPolicyModePrecedence(t *testing.T) {
	cases := []struct {
		name   string
		p      Policy
		tool   string
		area   string
		kind   string
		expect string
	}{
		// Presets alone.
		{"read preset read", Policy{Preset: PresetRead}, "files_list", "files", KindRead, ModeAllow},
		{"read preset create", Policy{Preset: PresetRead}, "folder_create", "files", KindCreate, ModeDeny},
		{"read preset change", Policy{Preset: PresetRead}, "files_delete", "files", KindChange, ModeDeny},
		{"standard read", Policy{Preset: PresetStandard}, "files_list", "files", KindRead, ModeAllow},
		{"standard create", Policy{Preset: PresetStandard}, "folder_create", "files", KindCreate, ModeAllow},
		{"standard change", Policy{Preset: PresetStandard}, "files_delete", "files", KindChange, ModeAsk},
		{"empty preset is standard", Policy{}, "files_delete", "files", KindChange, ModeAsk},
		{"all change", Policy{Preset: PresetAll}, "power_action", "updates", KindChange, ModeAllow},
		// Areas beat the preset, in both directions.
		{"area deny beats all", Policy{Preset: PresetAll, Areas: map[string]string{"docker": ModeDeny}}, "docker_list", "docker", KindRead, ModeDeny},
		{"area allow beats read", Policy{Preset: PresetRead, Areas: map[string]string{"docker": ModeAllow}}, "container_action", "docker", KindChange, ModeAllow},
		{"area of another tool", Policy{Preset: PresetRead, Areas: map[string]string{"docker": ModeAllow}}, "vm_action", "vms", KindChange, ModeDeny},
		{"invalid area mode ignored", Policy{Preset: PresetRead, Areas: map[string]string{"docker": "maybe"}}, "container_action", "docker", KindChange, ModeDeny},
		// Tools beat areas.
		{"tool ask beats area allow", Policy{Areas: map[string]string{"files": ModeAllow}, Tools: map[string]string{"files_delete": ModeAsk}}, "files_delete", "files", KindChange, ModeAsk},
		{"tool allow beats area deny", Policy{Areas: map[string]string{"files": ModeDeny}, Tools: map[string]string{"files_list": ModeAllow}}, "files_list", "files", KindRead, ModeAllow},
		{"tool deny beats preset all", Policy{Preset: PresetAll, Tools: map[string]string{"power_action": ModeDeny}}, "power_action", "updates", KindChange, ModeDeny},
		// The agent's own tools: always allowed, whatever is set.
		{"agent kind under read", Policy{Preset: PresetRead}, "memory_save", AreaAgent, KindAgent, ModeAllow},
		{"agent kind tool deny", Policy{Tools: map[string]string{"memory_save": ModeDeny}}, "memory_save", AreaAgent, KindAgent, ModeAllow},
		{"agent kind area deny", Policy{Areas: map[string]string{AreaAgent: ModeDeny}}, "skill_save", AreaAgent, KindAgent, ModeAllow},
	}
	for _, c := range cases {
		if got := c.p.Mode(c.tool, c.area, c.kind); got != c.expect {
			t.Errorf("%s: Mode(%s, %s, %s) = %s, want %s", c.name, c.tool, c.area, c.kind, got, c.expect)
		}
	}
}

func TestCovKindOf(t *testing.T) {
	cases := []struct {
		name  string
		reads bool
		want  string
	}{
		{"files_list", true, KindRead},
		// Reading wins over the create list.
		{"folder_create", true, KindRead},
		{"folder_create", false, KindCreate},
		{"files_mkdir", false, KindCreate},
		{"files_create", false, KindCreate},
		{"file_copy", false, KindCreate},
		{"files_copy", false, KindCreate},
		// Writing a file may replace one: a change, decided later by its plan.
		{"file_write", false, KindChange},
		{"files_delete", false, KindChange},
		{"unknown_tool", false, KindChange},
	}
	for _, c := range cases {
		if got := KindOf(c.name, c.reads); got != c.want {
			t.Errorf("KindOf(%s, %v) = %s, want %s", c.name, c.reads, got, c.want)
		}
	}
}

// Stricter never loosens: the result is at least as strict as each input,
// and the order of the inputs does not matter.
func TestCovStricterNeverLoosens(t *testing.T) {
	rank := map[string]int{ModeAllow: 0, ModeAsk: 1, ModeDeny: 2}
	modes := []string{ModeAllow, ModeAsk, ModeDeny}
	for _, a := range modes {
		for _, b := range modes {
			got := Stricter(a, b)
			if rank[got] < rank[a] || rank[got] < rank[b] {
				t.Errorf("Stricter(%s, %s) = %s loosens", a, b, got)
			}
			if got != Stricter(b, a) {
				t.Errorf("Stricter(%s, %s) = %s but Stricter(%s, %s) = %s", a, b, got, b, a, Stricter(b, a))
			}
			if got != a && got != b {
				t.Errorf("Stricter(%s, %s) = %s, neither input", a, b, got)
			}
		}
	}
}

func TestCovFolderModeOverlaps(t *testing.T) {
	p := Policy{Folders: []FolderRule{
		{Root: "share/Docs", Path: "Privat", Mode: ModeDeny},
		{Root: "share/Docs", Path: "Privat/Offen", Mode: ModeAllow},
		{Root: "share/Docs", Path: "Arbeit", Mode: ModeAsk},
		{Root: "share/Docs", Path: "Arbeit/Frei", Mode: ModeAllow},
		{Root: "volume/tank", Path: "", Mode: ModeAsk},
	}}
	cases := []struct {
		root, path, want string
	}{
		// The strictest rule wins, not the most specific one.
		{"share/Docs", "Privat/Offen/x.txt", ModeDeny},
		{"share/Docs", "Arbeit/Frei/x.txt", ModeAsk},
		{"share/Docs", "Arbeit", ModeAsk},
		// Paths are cleaned before matching.
		{"share/Docs", "Public/../Privat/x", ModeDeny},
		{"share/Docs", `Privat\geheim.txt`, ModeDeny},
		{"share/Docs", "/Privat/", ModeDeny},
		{"share/Docs", "./Privat", ModeDeny},
		// A sibling with the same prefix is not covered.
		{"share/Docs", "Privatsache/x", ""},
		{"share/Docs", "Arbeitszeit.txt", ""},
		// A parent of a rule is not covered.
		{"share/Docs", "", ""},
		// A rule with an empty path covers the whole root.
		{"volume/tank", "", ModeAsk},
		{"volume/tank", "Docs/Privat/x", ModeAsk},
		// Rules of other roots do not apply.
		{"share/Other", "Privat/x", ""},
	}
	for _, c := range cases {
		if got := p.FolderMode(c.root, c.path); got != c.want {
			t.Errorf("FolderMode(%s, %q) = %q, want %q", c.root, c.path, got, c.want)
		}
	}
	if !p.FolderHidden("share/Docs", "Privat/Offen") || p.FolderHidden("share/Docs", "Arbeit") {
		t.Fatal("FolderHidden")
	}
}

// A folder rule combined with the mode of a tool through Stricter: a deny
// folder beats an area allow, an allow folder never loosens a stricter
// tool mode.
func TestCovFolderAndAreaCombined(t *testing.T) {
	for _, preset := range []string{PresetRead, PresetStandard, PresetAll} {
		for _, area := range []string{"", ModeAllow, ModeAsk, ModeDeny} {
			for _, folder := range []string{ModeAllow, ModeAsk, ModeDeny} {
				p := Policy{Preset: preset, Folders: []FolderRule{{Root: "share/Docs", Path: "X", Mode: folder}}}
				if area != "" {
					p.Areas = map[string]string{"files": area}
				}
				for _, kind := range []string{KindRead, KindCreate, KindChange} {
					tool := p.Mode("files_x", "files", kind)
					got := Stricter(tool, p.FolderMode("share/Docs", "X/y"))
					label := fmt.Sprintf("preset=%s area=%q folder=%s kind=%s", preset, area, folder, kind)
					if folder == ModeDeny && got != ModeDeny {
						t.Errorf("%s: deny folder gave %s", label, got)
					}
					if tool == ModeDeny && got != ModeDeny {
						t.Errorf("%s: denied tool loosened to %s", label, got)
					}
					if tool == ModeAsk && got == ModeAllow {
						t.Errorf("%s: asking tool loosened to allow", label)
					}
				}
			}
		}
	}
}

func TestCovPolicyCheck(t *testing.T) {
	many := make([]FolderRule, 101)
	for i := range many {
		many[i] = FolderRule{Root: "share/Docs", Path: fmt.Sprint(i), Mode: ModeDeny}
	}
	cases := []struct {
		name string
		p    Policy
		ok   bool
	}{
		{"empty", Policy{}, true},
		{"each preset", Policy{Preset: PresetRead}, true},
		{"standard", Policy{Preset: PresetStandard}, true},
		{"all", Policy{Preset: PresetAll}, true},
		{"unknown preset", Policy{Preset: "everything"}, false},
		{"area mode", Policy{Areas: map[string]string{"files": "sometimes"}}, false},
		{"tool mode", Policy{Tools: map[string]string{"files_delete": ""}}, false},
		{"folder ok", Policy{Folders: []FolderRule{{Root: "share/Docs", Path: "a/b", Mode: ModeAsk}}}, true},
		{"folder whole root", Policy{Folders: []FolderRule{{Root: "share/Docs", Path: "", Mode: ModeAsk}}}, true},
		{"folder without root", Policy{Folders: []FolderRule{{Path: "a", Mode: ModeAsk}}}, false},
		{"folder with ..", Policy{Folders: []FolderRule{{Root: "share/Docs", Path: "a/../../etc", Mode: ModeAllow}}}, false},
		{"folder mode", Policy{Folders: []FolderRule{{Root: "share/Docs", Path: "a", Mode: "open"}}}, false},
		{"folder path too long", Policy{Folders: []FolderRule{{Root: "share/Docs", Path: strings.Repeat("a", 1025), Mode: ModeAsk}}}, false},
		{"100 rules", Policy{Folders: many[:100]}, true},
		{"101 rules", Policy{Folders: many}, false},
	}
	for _, c := range cases {
		if err := c.p.Check(); (err == nil) != c.ok {
			t.Errorf("%s: Check() = %v, want ok=%v", c.name, err, c.ok)
		}
	}
}

// AreaMode reports the area's own setting only ("" otherwise); the comment
// in policy.go also promises "mixed" and a preset-derived mode, which the
// code does not do.
func TestCovAreaMode(t *testing.T) {
	p := Policy{Preset: PresetAll, Areas: map[string]string{"files": ModeAsk, "docker": "x"}}
	if got := p.AreaMode("files"); got != ModeAsk {
		t.Fatalf("own setting: %q", got)
	}
	if got := p.AreaMode("docker"); got != "" {
		t.Fatalf("invalid setting shown: %q", got)
	}
	if got := p.AreaMode("vms"); got != "" {
		t.Fatalf("unset area: %q", got)
	}
	for _, a := range []string{"files", AreaWeb, AreaCode, "home"} {
		if !ValidArea(a) {
			t.Errorf("%s not an area", a)
		}
	}
	if ValidArea(AreaAgent) || ValidArea("nowhere") {
		t.Fatal("agent's own area or unknown area offered in the settings")
	}
}
