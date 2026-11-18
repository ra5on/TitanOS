package agent

import (
	"encoding/json"
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

// AreaCode is the area of running code in the sandbox (PLAN 55).
const AreaCode = "code"

// AreaMCP is the area of the tools of connected MCP servers (0.93).
const AreaMCP = "mcp"

// AreaHistory holds what happened on the NAS: the activity log, the
// monitoring history and the capacity forecast (1.10). It only reads.
const AreaHistory = "history"

// AreaNVR holds the events of the cameras (1.10), without pictures. It
// only reads, and asks first unless the person allowed it expressly.
const AreaNVR = "nvr"

// AreaPhotos is the area of the account's own photos in Immich (1.07). It
// only reads; the tool exists only with the account's own Immich key.
const AreaPhotos = "photos"

// AreaCalendar and AreaContacts are T-Kalender and Kontakte (1.11). Unlike
// the other areas they have two axes, reading and writing, set under
// Policy.DataRights (never under areas: 1.10 refuses an unknown area when
// it saves, also under extraAreas, TECHNIK 6.2).
const (
	AreaCalendar = "calendar"
	AreaContacts = "contacts"
)

// DataAreas are the areas with two axes.
var DataAreas = []string{AreaCalendar, AreaContacts}

// IsDataArea reports whether a is calendar or contacts.
func IsDataArea(a string) bool { return slices.Contains(DataAreas, a) }

// DataRight is what the agent may do with the calendar or the contacts:
// Read is allow or deny, Write (create, change and delete) allow, ask or
// deny.
type DataRight struct {
	Read  string `json:"read"`
	Write string `json:"write"`
}

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
	// WebFree: the web tools keep the mode of their rights also after the
	// chat has read content from outside (files, devices, pages, a notice
	// of the NAS). Off from the factory: then they ask, because such
	// content could tell the model to send data out. The person switches
	// it on knowingly (0.94, owner 15 Nov 2026).
	WebFree bool `json:"webFree,omitempty"`
	// DataRights holds the two axes of calendar and contacts (1.11). 1.10
	// drops the field when it reads the settings; saved there, it is gone,
	// and back on 1.11 the preset holds again (the careful side).
	DataRights map[string]DataRight `json:"dataRights,omitempty"`
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
	for a, m := range p.Areas {
		if !ValidMode(m) {
			return errors.New("unknown mode")
		}
		if IsDataArea(a) {
			return errors.New("the area " + a + " is set under dataRights")
		}
	}
	for a, r := range p.DataRights {
		if !IsDataArea(a) {
			return errors.New("unknown data area " + a)
		}
		if r.Read != ModeAllow && r.Read != ModeDeny || !ValidMode(r.Write) {
			return errors.New("invalid rights for " + a)
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

// presetDataMode is the mode a preset gives calendar and contacts: reading
// is allowed in every preset, writing (create, change, delete) asks in the
// standard one (owner, PLAN 108 question 7: "Eintragen soll nachfragen").
func presetDataMode(preset, kind string) string {
	if kind == KindRead {
		return ModeAllow
	}
	switch preset {
	case PresetAll:
		return ModeAllow
	case PresetRead:
		return ModeDeny
	}
	return ModeAsk
}

// DataMode is the mode of calendar or contacts for a kind: the person's
// two axes, else the preset. Single tool settings do not count here (the
// page shows no single tools for these areas).
func (p Policy) DataMode(area, kind string) string {
	if r, ok := p.DataRights[area]; ok {
		m := r.Write
		if kind == KindRead {
			m = r.Read
		}
		if ValidMode(m) {
			return m
		}
	}
	return presetDataMode(p.Preset, kind)
}

// Mode is how a tool is switched: a single setting first, then its area,
// then the preset. Calendar and contacts follow DataMode.
func (p Policy) Mode(name, area, kind string) string {
	if kind == KindAgent {
		return ModeAllow
	}
	if IsDataArea(area) {
		return p.DataMode(area, kind)
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
	if IsDataArea(area) {
		// Forbidden only when both axes are (Missing says why).
		if p.DataMode(area, KindRead) == ModeDeny && p.DataMode(area, KindChange) == ModeDeny {
			return ModeDeny
		}
		return ""
	}
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
var AgentAreas = []string{"system", "files", AreaCalendar, AreaContacts, "docker", "vms", "backups", "updates", "storage", "shares", "network", "accounts", "plugins", "home", AreaHistory, AreaNVR, AreaPhotos, AreaWeb, AreaCode, AreaMCP}

// newAreas are the areas since 1.10. On the stick their modes stand under
// extraAreas: 1.09 refuses an unknown area when it saves the settings, so
// it must not find them under areas (rollback, like extraDomains of the
// Home Assistant release). The running app takes both together.
var newAreas = []string{AreaHistory, AreaNVR}

type policyJSON struct {
	Preset     string            `json:"preset"`
	Areas      map[string]string `json:"areas,omitempty"`
	ExtraAreas map[string]string `json:"extraAreas,omitempty"`
	Tools      map[string]string `json:"tools,omitempty"`
	Folders    []FolderRule      `json:"folders,omitempty"`
	WebFree    bool              `json:"webFree,omitempty"`
	// DataRights: 1.10 does not know the field and drops it (TECHNIK 6.2).
	DataRights map[string]DataRight `json:"dataRights,omitempty"`
}

// MarshalJSON writes the modes of areas 1.09 does not know under extraAreas.
func (p Policy) MarshalJSON() ([]byte, error) {
	out := policyJSON{Preset: p.Preset, Tools: p.Tools, Folders: p.Folders, WebFree: p.WebFree, DataRights: p.DataRights}
	for a, m := range p.Areas {
		target := &out.Areas
		if slices.Contains(newAreas, a) {
			target = &out.ExtraAreas
		}
		if *target == nil {
			*target = map[string]string{}
		}
		(*target)[a] = m
	}
	return json.Marshal(out)
}

// UnmarshalJSON takes areas and extraAreas together.
func (p *Policy) UnmarshalJSON(data []byte) error {
	var in policyJSON
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	*p = Policy{Preset: in.Preset, Areas: in.Areas, Tools: in.Tools, Folders: in.Folders, WebFree: in.WebFree, DataRights: in.DataRights}
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

// ValidArea reports whether a is one of the areas.
func ValidArea(a string) bool { return slices.Contains(AgentAreas, a) }
