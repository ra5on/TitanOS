package api

// What the agent may do and see, for the agent itself (1.10, V2): the tool
// agent_access and the reasons why an area has no tools right now. An
// empty answer must not read as "there is none" when the agent simply may
// not look (export of 17 Nov 2026: "0 Automationen"). Only reads; shows
// the account's own settings, never paths of folder rules or names of
// hidden things.

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"sort"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// Where the person changes what the agent may do.
const (
	whereRights   = "Einstellungen › KI-Agent › Rechte"
	whereFolders  = "Einstellungen › KI-Agent › Rechte › Ordner-Regeln"
	whereHA       = "Einstellungen › KI-Agent › Home Assistant"
	whereServices = "Einstellungen › Dienste"
	wherePhotos   = "KI-Agent › Einstellungen › Fotos (Immich)"
	whereAccounts = "Einstellungen › KI-Agent › Konten (role: Einstellungen › Konten)"
)

// Missing says, per area that has no tools for this account now, why –
// a service switched off, no Immich key, no Home Assistant, the role.
// Areas the person forbade say so too. The runner shows it with tools_load.
func (b *agentToolbox) Missing() map[string]string {
	out := map[string]string{}
	have := map[string]bool{}
	for _, t := range b.ordered {
		if b.offered(t) {
			have[t.area] = true
		}
		for _, a := range t.areas {
			if b.offered(t) {
				have[a] = true
			}
		}
	}
	admin := b.user.Role.CanAdminister()
	on := b.s.agentEnv().servicesOn()
	for _, area := range agent.AgentAreas {
		if have[area] {
			continue
		}
		why := ""
		// Calendar and contacts are every account's own (1.11).
		userArea := area == agent.AreaWeb || area == agent.AreaPhotos || area == agent.AreaCode || area == agent.AreaMCP || area == toolcatalog.AreaFiles || area == toolcatalog.AreaSystem || agent.IsDataArea(area)
		switch {
		case b.settings.Policy.AreaMode(area) == agent.ModeDeny:
			why = "forbidden for the agent by the person (" + whereRights + ")"
		case agent.IsDataArea(area) && b.user.Name == agent.AssistProfile && slices.Contains(b.assistWaiting, area):
			why = "waits for the consent of the account " + b.assistActing + ": only it can let questions from Home Assistant read its " + area + ", in its own session (" + whereHA + " › Assist)"
		case agent.IsDataArea(area) && b.user.Name == agent.AssistProfile && b.settings.HA.assistLevel(area) == assistDeny:
			why = "forbidden for questions from Home Assistant (" + whereHA + " › Assist)"
		case agent.IsDataArea(area):
			why = "the data volume is not mounted (Kalender and Kontakte live on it)"
		case !admin && !userArea:
			why = "only for owners and administrators; this account is a user (" + whereAccounts + ")"
		case area == toolcatalog.AreaDocker && !on["docker"]:
			why = "Docker is switched off on this NAS (" + whereServices + ")"
		case area == toolcatalog.AreaVMs && !on["vms"] && !on["lxc"]:
			why = "virtual machines and system containers are switched off on this NAS (" + whereServices + ")"
		case area == toolcatalog.AreaHome:
			why = "Home Assistant is not connected (Einstellungen › Home Assistant)"
		case area == agent.AreaPhotos:
			why = "photos need the person's own Immich key and the app Immich running on this NAS (" + wherePhotos + ")"
		case area == agent.AreaNVR:
			why = "the NVR (cameras) is not set up on this NAS (Programme › NVR)"
		case area == agent.AreaCode:
			why = "running code is switched off (Einstellungen › KI-Agent › Verbindung und Modelle › Code ausführen; needs Docker)"
		case area == agent.AreaMCP:
			why = "no MCP server is connected (Einstellungen › KI-Agent › Verbindungen)"
		case area == agent.AreaWeb:
			why = "web search is switched off (" + whereRights + ")"
		}
		if why != "" {
			out[area] = why
		}
	}
	if admin && on["vms"] && !on["lxc"] {
		out["lxc"] = "system containers (LXC) are switched off on this NAS (" + whereServices + ")"
	}
	if admin && !on["vms"] && on["lxc"] {
		out["vms"] = "virtual machines are switched off on this NAS (" + whereServices + "); system containers are in the area vms"
	}
	return out
}

// accessTool is agent_access: what the agent may do and see here.
func (b *agentToolbox) accessTool() *agentTool {
	return &agentTool{name: "agent_access", area: agent.AreaAgent, kind: agent.KindAgent,
		description: "Your own rights and what you cannot see here: role, rights per area, Home Assistant release, folder rules (count), services off, Immich, and where the person changes each. Call it before you say \"none\" or \"I cannot\", and when the person says your answer is wrong.",
		params:      schema(nil),
		own: func(ctx context.Context, _ *agent.Chat, _ json.RawMessage) (agent.Outcome, error) {
			data, err := json.Marshal(b.access(ctx))
			return agent.Outcome{Text: string(data)}, err
		}}
}

type accessArea struct {
	Area string `json:"area"`
	// Read and Change are the modes for reading and for changes (create
	// counts as change here) as they apply now; "deny" also when the area
	// has no tools for this account.
	Read   string `json:"read"`
	Change string `json:"change,omitempty"`
	Why    string `json:"why,omitempty"`
}

func (b *agentToolbox) access(ctx context.Context) map[string]any {
	p := b.settings.Policy
	admin := b.user.Role.CanAdminister()
	role := "user (only own shares, no administration, no Home Assistant)"
	switch {
	case b.user.Name == agent.AssistProfile:
		role = "Home Assistant Assist (asks of Home Assistant; rights narrowed under " + whereHA + " › Assist)"
	case b.user.Role == auth.RoleOwner:
		role = "owner"
	case admin:
		role = "administrator"
	}
	missing := b.Missing()
	areas := []accessArea{}
	for _, area := range agent.AgentAreas {
		if area == agent.AreaAgent {
			continue
		}
		a := accessArea{Area: area}
		var list []*agentTool
		for _, t := range b.ordered {
			if t.area == area || slices.Contains(t.areas, area) {
				list = append(list, t)
			}
		}
		if len(list) == 0 {
			// Calendar and contacts are always named, with the reason.
			if agent.IsDataArea(area) {
				areas = append(areas, accessArea{Area: area, Read: agent.ModeDeny, Change: agent.ModeDeny, Why: missing[area]})
			}
			continue
		}
		read, change := "", ""
		for _, t := range list {
			kind := t.kind
			m := b.modeIn(t, area, kind)
			if t.name == "code_run" {
				m = b.codeMode(m)
			}
			if area == agent.AreaNVR {
				m = b.nvrMode(t.name, m)
			}
			if kind == agent.KindRead {
				read = looser(read, m)
			} else {
				change = looser(change, m)
			}
		}
		a.Read, a.Change = read, change
		if agent.IsDataArea(area) {
			// No single tools here: the two axes, as the person set them.
			read, change = agent.ModeDeny, agent.ModeDeny
			for _, t := range list {
				if t.kind == agent.KindRead {
					read = b.modeIn(t, area, agent.KindRead)
				} else {
					change = b.modeIn(t, area, agent.KindChange)
				}
			}
			a.Read, a.Change = read, change
			kind := pimstore.Calendar
			if area == agent.AreaContacts {
				kind = pimstore.Contacts
			}
			switch {
			case !b.pimMounted(kind):
				a.Why = "the data volume is not mounted right now; searches answer hidden/why instead of results"
			case area == agent.AreaCalendar && change != agent.ModeDeny:
				a.Why = "deleting a whole series or changing its repetition always asks the person, and so does deleting after outside content was read"
			case area == agent.AreaContacts && change != agent.ModeDeny:
				a.Why = "deleting a contact after outside content was read always asks the person"
			}
		}
		if area == agent.AreaNVR && read == agent.ModeAsk {
			a.Why = "camera events ask first unless the person allows them expressly (" + whereRights + " › NVR)"
		}
		areas = append(areas, a)
	}
	own := map[string]string{}
	for name, m := range p.Tools {
		if _, ok := b.tools[name]; ok {
			own[name] = m
		}
	}
	denied, ask := 0, 0
	for _, f := range p.Folders {
		switch f.Mode {
		case agent.ModeDeny:
			denied++
		case agent.ModeAsk:
			ask++
		}
	}
	on := b.s.agentEnv().servicesOn()
	out := map[string]any{
		"account": b.user.Name, "role": role,
		"preset": p.Preset, "presetMeans": presetMeaning(p.Preset),
		"areas": areas, "toolSettings": own,
		"folderRules": map[string]any{"count": len(p.Folders), "forbidden": denied, "ask": ask,
			"note": "Forbidden folders are left out of lists and searches (hidden counts them); the paths are not shown here.", "where": whereFolders},
		"services":         map[string]any{"docker": on["docker"], "vms": on["vms"], "lxc": on["lxc"], "where": whereServices},
		"mail":             "Mail (the e-mail program) is closed to the agent by design: no tool reads or sends mail. Say so; do not search files for mail.",
		"tmailAddressBook": "Suggestions learned from sent mail are Mail's and closed to the agent by design.",
		"where":            map[string]string{"rights": whereRights, "homeAssistant": whereHA, "photos": wherePhotos, "accounts": whereAccounts},
	}
	if b.user.Name != agent.AssistProfile {
		st, _ := b.s.photosStored(b.user.Name)
		_, has := b.tools["photos_search"]
		out["photos"] = map[string]any{"linked": st != nil, "available": has, "where": wherePhotos}
	}
	out["homeAssistant"] = b.haAccess(ctx, admin)
	if admin {
		out["nvr"] = map[string]any{"names": b.settings.NVRNames, "pictures": false,
			"note": "Events (who or what, when, which camera) only, never pictures. Names of faces and plates only when the person switched them on.", "where": whereRights + " › NVR"}
	}
	if len(missing) > 0 {
		// One line per reason: a user would get the same sentence ten times.
		type gone struct {
			Areas []string `json:"areas"`
			Why   string   `json:"why"`
		}
		var list []gone
		for _, area := range slices.Sorted(maps.Keys(missing)) {
			i := slices.IndexFunc(list, func(g gone) bool { return g.Why == missing[area] })
			if i < 0 {
				list = append(list, gone{Why: missing[area]})
				i = len(list) - 1
			}
			list[i].Areas = append(list[i].Areas, area)
		}
		out["notAvailable"] = list
	}
	return out
}

// haAccess is the Home Assistant part of agent_access.
func (b *agentToolbox) haAccess(ctx context.Context, admin bool) map[string]any {
	if !admin {
		return map[string]any{"available": false, "why": "only for owners and administrators"}
	}
	hub := b.s.haHub()
	if hub == nil || len(hub.Active()) == 0 {
		return map[string]any{"available": false, "why": "not connected (Einstellungen › Home Assistant)"}
	}
	r := b.settings.HA.Release
	release := "selection"
	switch {
	case r.All:
		release = "all"
	case r.Empty():
		release = "nothing"
	}
	rules := map[string]string{}
	for key, m := range b.settings.HA.Entities {
		if len(key) > 7 && key[:7] == "domain:" {
			rules[key[7:]] = m
		}
	}
	domains := slices.Clone(r.Domains)
	sort.Strings(domains)
	out := map[string]any{"available": true, "release": release, "where": whereHA,
		"note": "Reading a released kind never asks; switching follows the rights of the area home. Automations, locks and alarm panels ask before switching unless a rule says otherwise."}
	if release == "selection" {
		out["releasedKinds"] = domains
		out["singleEntities"] = len(r.Entities)
	}
	if len(rules) > 0 {
		out["switchRulesPerKind"] = rules
	}
	if len(b.settings.HA.Releases) > 0 {
		out["moreConnections"] = len(b.settings.HA.Releases)
	}
	return out
}

func presetMeaning(preset string) string {
	switch preset {
	case agent.PresetRead:
		return "only reading; every change is forbidden"
	case agent.PresetAll:
		return "everything without asking (code and camera events still ask)"
	}
	return "reading and creating new things without asking; every other change asks the person first (calendar and contacts: every entry asks)"
}

// looser is the more open of two modes ("" is none yet).
func looser(a, b string) string {
	if a == "" {
		return b
	}
	if agent.Stricter(a, b) == a {
		return b
	}
	return a
}
