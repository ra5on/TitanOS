package agent

import (
	"errors"
	"path"
	"slices"
	"strings"
)

// What the agent may do is decided here, in Go, for every call, never by
// the model (PLAN 44, "Was der Agent darf"). Every tool has one of three
// modes; a quick choice (preset) sets all of them, areas and single tools
// may differ. Folder rules narrow file tools further; the stricter rule
// wins. Secrets, tokens, two-factor login, consoles and the agent and MCP
// settings are not in the catalog at all, so no setting can allow them.

// Modes of a tool.
const (
	ModeAllow = "allow" // run without asking
	ModeAsk   = "ask"   // ask with a button in the chat
	ModeDeny  = "deny"  // not offered, refused if called anyway
)

// Presets of the quick choice.
const (
	PresetRead     = "read"
	PresetStandard = "standard"
	PresetAll      = "all"
)

// Kinds of tools, for the presets.
const (
	KindRead   = "read"
	KindCreate = "create"
	KindChange = "change"
	// KindAgent are the agent's own tools (memory, skills, questions):
	// always allowed, they change nothing on the NAS.
	KindAgent = "agent"
)

// AreaWeb is the area of web search and reading pages.
const AreaWeb = "web"

// AreaAgent holds the agent's own tools.
const AreaAgent = "agent"

// createTools only add something new; the standard preset runs them
// without asking ("neue Dateien legt er frei an").
var createTools = map[string]bool{
	"folder_create": true, "files_mkdir": true, "files_create": true, "file_copy": true, "files_copy": true,
}

// KindOf says what a tool does from what the catalog knows.
func KindOf(name string, reads bool) string {
	switch {
	case reads:
		return KindRead
	case createTools[name]:
		return KindCreate
	}
	return KindChange
}

// FolderRule sets a mode for a folder of a root of the file manager.
type FolderRule struct {
	Root string `json:"root"`
	Path string `json:"path"`
	Mode string `json:"mode"`
}

// Location is a named place the person uses in the chat instead of a path.
type Location struct {
	Name string `json:"name"`
	Root string `json:"root"`
	Path string `json:"path"`
	Note string `json:"note,omitempty"`
}

// Policy is the part of the settings that decides about calls.
type Policy struct {
	Preset  string            `json:"preset"`
	Areas   map[string]string `json:"areas,omitempty"`
	Tools   map[string]string `json:"tools,omitempty"`
	Folders []FolderRule      `json:"folders,omitempty"`
}

// ValidMode reports whether m is one of the three modes.
func ValidMode(m string) bool { return m == ModeAllow || m == ModeAsk || m == ModeDeny }

// Check refuses unknown values; it is called before settings are stored.
func (p Policy) Check() error {
	switch p.Preset {
	case "", PresetRead, PresetStandard, PresetAll:
	default:
		return errors.New("unknown preset")
	}
	for _, m := range p.Areas {
		if !ValidMode(m) {
			return errors.New("unknown mode")
		}
	}
	for _, m := range p.Tools {
		if !ValidMode(m) {
			return errors.New("unknown mode")
		}
	}
	if len(p.Folders) > 100 {
		return errors.New("too many folder rules")
	}
	for _, f := range p.Folders {
		if !ValidMode(f.Mode) || f.Root == "" || strings.Contains(f.Path, "..") || len(f.Path) > 1024 {
			return errors.New("invalid folder rule")
		}
	}
	return nil
}

// presetMode is the mode a preset gives a kind of tool.
func presetMode(preset, kind string) string {
	if kind == KindAgent {
		return ModeAllow
	}
	switch preset {
	case PresetAll:
		return ModeAllow
	case PresetRead:
		if kind == KindRead {
			return ModeAllow
		}
		return ModeDeny
	}
	// Standard: read and create freely, ask before changing.
	if kind == KindRead || kind == KindCreate {
		return ModeAllow
	}
	return ModeAsk
}

// Mode is how a tool is switched: a single setting first, then its area,
// then the preset.
func (p Policy) Mode(name, area, kind string) string {
	if kind == KindAgent {
		return ModeAllow
	}
	if m := p.Tools[name]; ValidMode(m) {
		return m
	}
	if m := p.Areas[area]; ValidMode(m) {
		return m
	}
	return presetMode(p.Preset, kind)
}

// AreaMode is how an area is shown: its own setting, or "mixed" when its
// tools differ, or what the preset gives most of them.
func (p Policy) AreaMode(area string) string {
	if m := p.Areas[area]; ValidMode(m) {
		return m
	}
	return ""
}

// Stricter returns the stricter of two modes.
func Stricter(a, b string) string {
	rank := map[string]int{ModeAllow: 0, ModeAsk: 1, ModeDeny: 2}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// FolderMode is the strictest folder rule that covers a path in a root;
// "" if none does. A rule covers its folder and everything below it, and a
// path that is a parent of a denied folder is not denied itself (the
// listing of the parent filters the folder out, see FolderHidden).
func (p Policy) FolderMode(root, p2 string) string {
	clean := cleanPath(p2)
	mode := ""
	for _, f := range p.Folders {
		if f.Root != root {
			continue
		}
		rule := cleanPath(f.Path)
		if rule == "" || clean == rule || strings.HasPrefix(clean, rule+"/") {
			if mode == "" {
				mode = f.Mode
			} else {
				mode = Stricter(mode, f.Mode)
			}
		}
	}
	return mode
}

// FolderHidden reports whether an entry of a listing or a search lies in
// a denied folder.
func (p Policy) FolderHidden(root, p2 string) bool {
	return p.FolderMode(root, p2) == ModeDeny
}

func cleanPath(p string) string {
	return strings.Trim(path.Clean("/"+strings.ReplaceAll(p, "\\", "/")), "/")
}

// Areas of the agent in the order the settings show them, with the tool
// areas of the catalog first.
var AgentAreas = []string{"system", "files", "docker", "vms", "backups", "updates", "storage", "shares", "network", "accounts", "plugins", "home", AreaWeb}

// ValidArea reports whether a is one of the areas.
func ValidArea(a string) bool { return slices.Contains(AgentAreas, a) }
