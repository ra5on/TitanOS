package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// The NAS map of the KI-Agent (PLAN 44, Runde 6): where things are on this
// NAS – shares with their top folders, apps with address, VMs, system
// containers, backups and the rooms of Home Assistant – plus descriptions
// the owner (or the agent) wrote. It is built at night and on request from
// the reading tools of the catalog and kept on the data volume, so a run
// reads it without touching every disk. A short form goes into every
// prompt; the tool nas_map has the details. Users only see their shares.

const (
	mapFile          = "nas-map.json"
	descriptionsFile = "descriptions.json"
	mapPromptLimit   = 3500
)

type nasMapShare struct {
	Name    string   `json:"name"`
	Root    string   `json:"root"`
	Volume  string   `json:"volume,omitempty"`
	Folders []string `json:"folders,omitempty"`
}

type nasMapItem struct {
	Name    string `json:"name"`
	Kind    string `json:"kind,omitempty"`
	State   string `json:"state,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Address string `json:"address,omitempty"`
}

type nasMap struct {
	Built   time.Time     `json:"built"`
	Host    string        `json:"host,omitempty"`
	Volumes []nasMapItem  `json:"volumes,omitempty"`
	Shares  []nasMapShare `json:"shares,omitempty"`
	Apps    []nasMapItem  `json:"apps,omitempty"`
	VMs     []nasMapItem  `json:"vms,omitempty"`
	LXC     []nasMapItem  `json:"lxc,omitempty"`
	Backups []nasMapItem  `json:"backups,omitempty"`
	Rooms   []string      `json:"rooms,omitempty"`
}

// descriptions: key → text. Keys: share:<name>, folder:<root>/<path>,
// app:<name>, vm:<name>, lxc:<name>, backup:<id>, room:<name>.
type agentDescription struct {
	Text string    `json:"text"`
	By   string    `json:"by,omitempty"` // "agent" when the agent wrote it
	At   time.Time `json:"at"`
}

var descriptionKey = regexp.MustCompile(`^(share|folder|app|vm|lxc|backup|room):[^\x00-\x1f]{1,300}$`)

// buildNASMap reads the NAS through the catalog as the first owner.
func (s *Server) buildNASMap(ctx context.Context) (nasMap, error) {
	m := nasMap{Built: time.Now().UTC()}
	if s.opts.Monitor != nil {
		m.Host = s.opts.Monitor.Info().Hostname
	}
	owner, ok := s.opts.Users.Get(s.firstOwner())
	if !ok {
		return m, fmt.Errorf("no owner account")
	}
	settings := s.agentSettings()
	settings.Policy = agent.Policy{Preset: agent.PresetRead}
	box := s.newAgentToolbox(owner, settings)
	read := func(name string, args string) map[string]any {
		t, ok := box.tools[name]
		if !ok || t.read == nil {
			return nil
		}
		rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		res, err := t.read(rctx, json.RawMessage(args))
		if err != nil {
			return nil
		}
		var out map[string]any
		if json.Unmarshal([]byte(res.Text), &out) != nil {
			return nil
		}
		return out
	}
	list := func(v any) []map[string]any {
		items, _ := v.([]any)
		var out []map[string]any
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	str := func(m map[string]any, k string) string { v, _ := m[k].(string); return v }
	if st := read("storage_overview", "{}"); st != nil {
		for _, v := range list(st["volumes"]) {
			detail := str(v, "level") + " " + str(v, "filesystem")
			if size, ok := v["sizeBytes"].(float64); ok && size > 0 {
				used, _ := v["usedBytes"].(float64)
				detail += fmt.Sprintf(", %.0f of %.0f GB used", used/1e9, size/1e9)
			}
			m.Volumes = append(m.Volumes, nasMapItem{Name: str(v, "name"), State: str(v, "state"), Detail: strings.TrimSpace(detail)})
		}
	}
	if sh := read("shares_list", "{}"); sh != nil {
		for _, v := range list(sh["shares"]) {
			share := nasMapShare{Name: str(v, "name"), Root: str(v, "root"), Volume: str(v, "volume")}
			if share.Root == "" {
				share.Root = "share/" + share.Name
			}
			if fl := read("files_list", fmt.Sprintf(`{"root":%q}`, share.Root)); fl != nil {
				for _, e := range list(fl["entries"]) {
					if dir, _ := e["dir"].(bool); dir || str(e, "kind") == "dir" || str(e, "type") == "dir" {
						share.Folders = append(share.Folders, str(e, "name"))
					}
					if len(share.Folders) >= 30 {
						break
					}
				}
			}
			m.Shares = append(m.Shares, share)
		}
	}
	if d := read("docker_list", "{}"); d != nil {
		for _, c := range list(d["containers"]) {
			item := nasMapItem{Name: str(c, "name"), Kind: str(c, "app"), State: str(c, "state"), Detail: str(c, "image")}
			var ports []string
			for _, p := range list(c["ports"]) {
				if pub, ok := p["public"].(float64); ok && pub > 0 {
					ports = append(ports, fmt.Sprintf("%.0f", pub))
				} else if pub, ok := p["host"].(float64); ok && pub > 0 {
					ports = append(ports, fmt.Sprintf("%.0f", pub))
				}
			}
			if len(ports) > 0 {
				item.Address = "port " + strings.Join(ports, ", ")
			}
			m.Apps = append(m.Apps, item)
		}
	}
	if v := read("vm_list", "{}"); v != nil {
		for _, vm := range list(v["vms"]) {
			addrs, _ := vm["addresses"].([]any)
			var a []string
			for _, x := range addrs {
				if s, ok := x.(string); ok {
					a = append(a, s)
				}
			}
			m.VMs = append(m.VMs, nasMapItem{Name: str(vm, "name"), State: str(vm, "state"), Detail: str(vm, "description"), Address: strings.Join(a, ", ")})
		}
	}
	if l := read("lxc_list", "{}"); l != nil {
		for _, c := range list(l["containers"]) {
			m.LXC = append(m.LXC, nasMapItem{Name: str(c, "name"), State: str(c, "state"), Detail: strings.TrimSpace(str(c, "distribution") + " " + str(c, "release"))})
		}
	}
	if b := read("backup_status", "{}"); b != nil {
		for _, j := range list(b["jobs"]) {
			name := str(j, "name")
			if name == "" {
				name = str(j, "id")
			}
			m.Backups = append(m.Backups, nasMapItem{Name: name, Kind: str(j, "id"), Detail: str(j, "target")})
		}
	}
	if ha := s.opts.HomeAssistant; ha != nil && ha.Active() {
		if areas, err := ha.Areas(ctx); err == nil {
			seen := map[string]bool{}
			for _, a := range areas {
				if !seen[a] {
					seen[a] = true
					m.Rooms = append(m.Rooms, a)
				}
			}
			sort.Strings(m.Rooms)
		}
	}
	return m, nil
}

// refreshNASMap builds the map and keeps it.
func (s *Server) refreshNASMap(ctx context.Context) (nasMap, error) {
	m, err := s.buildNASMap(ctx)
	if err != nil {
		return m, err
	}
	return m, s.agentStore().WriteShared(mapFile, m)
}

func (s *Server) agentMap() (nasMap, bool) {
	var m nasMap
	if err := s.agentStore().ReadShared(mapFile, &m); err != nil {
		return nasMap{}, false
	}
	return m, true
}

func (s *Server) agentDescriptions() map[string]agentDescription {
	d := map[string]agentDescription{}
	if err := s.agentStore().ReadShared(descriptionsFile, &d); err != nil {
		return map[string]agentDescription{}
	}
	return d
}

// mapFor is the map as an account may see it: users only their shares.
func (s *Server) mapFor(user auth.User, m nasMap) nasMap {
	if user.Role.CanAdminister() {
		return m
	}
	roots := map[string]bool{}
	for _, r := range s.fileRoots(user) {
		roots[r.ID] = true
	}
	out := nasMap{Built: m.Built, Host: m.Host}
	for _, sh := range m.Shares {
		if roots[sh.Root] {
			out.Shares = append(out.Shares, sh)
		}
	}
	return out
}

// mapPrompt is the short map for the system prompt.
func (s *Server) mapPrompt(user auth.User) string {
	m, ok := s.agentMap()
	if !ok {
		return ""
	}
	m = s.mapFor(user, m)
	desc := s.agentDescriptions()
	note := func(key string) string {
		if d, ok := desc[key]; ok && d.Text != "" {
			return " – " + d.Text
		}
		return ""
	}
	var b strings.Builder
	for _, sh := range m.Shares {
		fmt.Fprintf(&b, "Share %s (root %s)%s", sh.Name, sh.Root, note("share:"+sh.Name))
		if len(sh.Folders) > 0 {
			var folders []string
			for _, f := range sh.Folders {
				folders = append(folders, f+note("folder:"+sh.Root+"/"+f))
			}
			fmt.Fprintf(&b, ": %s", strings.Join(folders, ", "))
		}
		b.WriteString("\n")
	}
	items := func(label, prefix string, list []nasMapItem) {
		for _, it := range list {
			fmt.Fprintf(&b, "%s %s", label, it.Name)
			if it.State != "" {
				fmt.Fprintf(&b, " (%s)", it.State)
			}
			if it.Address != "" {
				fmt.Fprintf(&b, " %s", it.Address)
			}
			b.WriteString(note(prefix + it.Name))
			b.WriteString("\n")
		}
	}
	items("App", "app:", m.Apps)
	items("VM", "vm:", m.VMs)
	items("LXC", "lxc:", m.LXC)
	items("Backup job", "backup:", m.Backups)
	if len(m.Rooms) > 0 {
		fmt.Fprintf(&b, "Rooms in Home Assistant: %s\n", strings.Join(m.Rooms, ", "))
	}
	out := b.String()
	if r := []rune(out); len(r) > mapPromptLimit {
		out = string(r[:mapPromptLimit]) + "\n… (more in nas_map)"
	}
	return out
}

// handleAgentMap returns the map with descriptions for the page.
func (s *Server) handleAgentMap(w http.ResponseWriter, r *http.Request) {
	m, ok := s.agentMap()
	user := currentUser(r)
	desc := s.agentDescriptions()
	if !user.Role.CanAdminister() {
		visible := map[string]agentDescription{}
		for _, sh := range s.mapFor(user, m).Shares {
			for k, v := range desc {
				if k == "share:"+sh.Name || strings.HasPrefix(k, "folder:"+sh.Root+"/") {
					visible[k] = v
				}
			}
		}
		desc = visible
	}
	writeJSON(w, http.StatusOK, map[string]any{"map": s.mapFor(user, m), "built": ok, "descriptions": desc})
}

func (s *Server) handleAgentMapRefresh(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	m, err := s.refreshNASMap(ctx)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"map": m, "built": true, "descriptions": s.agentDescriptions()})
}

// saveDescription sets or removes (empty text) a description.
func (s *Server) saveDescription(key, text, by string) error {
	key = strings.TrimSpace(key)
	text = strings.Join(strings.Fields(text), " ")
	if !descriptionKey.MatchString(key) || len(text) > 300 {
		return toolcatalog.Errorf("a key like share:<name>, folder:<root>/<path>, app:<name>, vm:<name>, lxc:<name>, backup:<id> or room:<name>, and at most 300 characters")
	}
	if secretish.MatchString(text) {
		return toolcatalog.Errorf("descriptions never hold passwords, keys or secrets")
	}
	d := s.agentDescriptions()
	if text == "" {
		delete(d, key)
	} else {
		d[key] = agentDescription{Text: text, By: by, At: time.Now()}
	}
	if len(d) > 2000 {
		return toolcatalog.Errorf("too many descriptions")
	}
	return s.agentStore().WriteShared(descriptionsFile, d)
}

func (s *Server) handleAgentDescription(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key  string `json:"key"`
		Text string `json:"text"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.saveDescription(req.Key, req.Text, currentUser(r).Name); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_description", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"descriptions": s.agentDescriptions()})
}

func (s *Server) handleAgentProfile(w http.ResponseWriter, r *http.Request) {
	p, err := s.agentStore().Profile(currentUser(r).Name)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleAgentProfileSet(w http.ResponseWriter, r *http.Request) {
	var p agent.Profile
	if !readJSON(w, r, &p) {
		return
	}
	if len(p.Instruction) > 4000 {
		writeError(w, http.StatusBadRequest, "invalid_profile", "at most 4000 characters")
		return
	}
	if err := s.agentStore().SaveProfile(currentUser(r).Name, p); err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// mapTools are the agent's tools for the map.
func (b *agentToolbox) mapTools() []*agentTool {
	tools := []*agentTool{{
		name: "nas_map", area: agent.AreaAgent, kind: agent.KindAgent,
		description: "The map of this NAS: shares with their top folders, apps with address, VMs, system containers, backup jobs, rooms of Home Assistant, with the descriptions people wrote. Use it to find where something belongs.",
		params:      schema(map[string]any{"section": enum("Only one part, optional.", "shares", "apps", "vms", "lxc", "backups", "rooms", "volumes")}),
		own: func(ctx context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
			var req struct{ Section string }
			_ = json.Unmarshal(args, &req)
			m, ok := b.s.agentMap()
			if !ok {
				var err error
				if m, err = b.s.refreshNASMap(ctx); err != nil {
					return agent.Outcome{}, err
				}
			}
			m = b.s.mapFor(b.user, m)
			out := map[string]any{"built": m.Built, "descriptions": b.s.agentDescriptions()}
			sections := map[string]any{"shares": m.Shares, "apps": m.Apps, "vms": m.VMs, "lxc": m.LXC, "backups": m.Backups, "rooms": m.Rooms, "volumes": m.Volumes}
			if v, ok := sections[req.Section]; ok {
				out[req.Section] = v
			} else {
				for k, v := range sections {
					out[k] = v
				}
			}
			res, err := toolcatalog.JSON(out)
			return agent.Outcome{Text: res.Text}, err
		},
	}}
	if b.user.Role.CanAdminister() {
		tools = append(tools, &agentTool{
			name: "description_save", area: agent.AreaAgent, kind: agent.KindAgent,
			description: "Save a short description of a share, folder, app, VM, system container, backup job or room, so you and others know what it is for (\"Scans from the printer\"). Empty text removes it.",
			params:      schema(map[string]any{"key": str("share:<name>, folder:<root>/<path>, app:<name>, vm:<name>, lxc:<name>, backup:<id> or room:<name>."), "text": str("One short sentence.")}, "key", "text"),
			own: func(_ context.Context, chat *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
				var req struct{ Key, Text string }
				if err := json.Unmarshal(args, &req); err != nil {
					return agent.Outcome{}, toolcatalog.Errorf("invalid arguments")
				}
				by := "agent"
				if chat != nil && chat.Tainted {
					by = "agent (outside content)"
				}
				if err := b.s.saveDescription(req.Key, req.Text, by); err != nil {
					return agent.Outcome{}, err
				}
				return agent.Outcome{Text: "Saved.", Note: "memory", NoteText: req.Key + ": " + req.Text}, nil
			},
		})
	}
	return tools
}

// nasMapDue reports whether the nightly map is due (03:00–03:59, older
// than 20 hours or missing).
func (s *Server) nasMapDue(now time.Time) bool {
	if now.Hour() != 3 {
		return false
	}
	r := &s.agentReact
	r.mu.Lock()
	defer r.mu.Unlock()
	// Once a night; the time is kept in RAM so the volume is not read
	// every minute of that hour.
	if now.Sub(r.mapTried) < 20*time.Hour {
		return false
	}
	r.mapTried = now
	return true
}

func (s *Server) nightlyNASMap(now time.Time) {
	if !s.nasMapDue(now.In(s.agentLocation())) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(s.agentBase(), 5*time.Minute)
		defer cancel()
		if _, err := s.refreshNASMap(ctx); err != nil {
			slog.Warn("NAS map not built", "err", err)
		}
	}()
}
