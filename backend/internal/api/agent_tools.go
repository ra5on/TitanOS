package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// The tools of the agent are the tools of the fixed catalog and the public
// routes, exactly as MCP gets them (docs/rules/API.md: never a list of its
// own), plus a few of the agent itself (questions, memory, skills, web,
// locations). What runs is decided by the agent's policy, not by the
// switches of the MCP page: a tool set to "Verboten" is not offered and
// refused if called anyway, "Nachfragen" pauses the run until the person
// clicks, and the model never sets confirm itself.

// agentCallKey marks requests a tool of the agent makes, for the activity
// log (Herkunft "KI-Agent").
type agentCallKey struct{}

type agentTool struct {
	name, area, kind string
	untrusted        bool
	description      string
	params           map[string]any
	read             func(context.Context, json.RawMessage) (toolcatalog.Result, error)
	plan             func(context.Context, json.RawMessage) (*toolcatalog.Action, error)
	route            *publicRoute
	own              func(context.Context, *agent.Chat, json.RawMessage) (agent.Outcome, error)
	// kindOf decides the kind from the arguments (note_write: new or not).
	kindOf func(json.RawMessage) string
}

type agentToolbox struct {
	s        *Server
	user     auth.User
	settings agentSettings
	tools    map[string]*agentTool
	ordered  []*agentTool
	session  *mcpSession
	web      *agent.Web
	version  string
	dirs     map[string]string
	// areas maps Home Assistant entities to their room, for area: rules.
	areas map[string]string
}

// newAgentToolbox collects the tools of an account.
func (s *Server) newAgentToolbox(user auth.User, settings agentSettings) *agentToolbox {
	env := s.agentEnv()
	b := &agentToolbox{s: s, user: user, settings: settings, tools: map[string]*agentTool{}, version: env.version}
	// Requests of the tools come from titand itself.
	r, _ := http.NewRequest(http.MethodPost, "/agent", nil)
	r.RemoteAddr = "127.0.0.1:0"
	b.session = &mcpSession{user: user, token: auth.APIToken{Name: "KI-Agent"}, version: mcpVersions[0], r: r}
	on := env.servicesOn()
	off := func(area string) bool {
		return area == toolcatalog.AreaDocker && !on["docker"] || area == toolcatalog.AreaVMs && !on["vms"]
	}
	add := func(t *agentTool) {
		if _, dup := b.tools[t.name]; dup {
			return
		}
		b.tools[t.name] = t
		b.ordered = append(b.ordered, t)
	}
	for _, tool := range s.catalogTools(user) {
		if mcpSkip[tool.Name] || off(tool.Area) {
			continue
		}
		params := tool.Parameters
		if tool.Plan != nil {
			// The reason is filled in by the agent; confirm never exists.
			params = withoutProps(params, "reason", "confirm")
		}
		t := &agentTool{name: tool.Name, area: tool.Area, untrusted: tool.Untrusted, description: tool.Description, params: params, read: tool.Read, plan: tool.Plan}
		if t.area == "" {
			t.area = toolcatalog.AreaSystem
		}
		t.kind = agent.KindOf(tool.Name, tool.Read != nil)
		if t.params == nil {
			t.params = schema(nil)
		}
		add(t)
	}
	allowed := func(scope string) bool { return !auth.AdminScope(scope) || user.Role.CanAdminister() }
	for i := range publicRoutes {
		route := &publicRoutes[i]
		if route.Tool == "" || !allowed(route.Scope) || off(routeArea(route.Scope)) {
			continue
		}
		mt := s.routeTool(*route)
		params := withoutProps(mt.Schema, "confirm")
		area := routeArea(route.Scope)
		add(&agentTool{name: route.Tool, area: area, kind: agent.KindOf(route.Tool, route.Method == http.MethodGet), untrusted: route.Method == http.MethodGet, description: mt.Description, params: params, route: route})
	}
	for _, t := range b.ownTools() {
		add(t)
	}
	for _, t := range b.haTools() {
		add(t)
	}
	for _, t := range b.mapTools() {
		add(t)
	}
	for _, t := range b.clusterTools() {
		add(t)
	}
	add(b.attachmentTool())
	add(b.imageTool())
	if t := b.codeTool(); t != nil {
		add(t)
	}
	for _, t := range b.noteTools() {
		add(t)
	}
	for _, t := range b.featureTools() {
		add(t)
	}
	return b
}

// withoutProps returns a schema without some properties.
func withoutProps(params map[string]any, names ...string) map[string]any {
	if params == nil {
		return schema(nil)
	}
	props, _ := params["properties"].(map[string]any)
	clone := map[string]any{}
	for k, v := range props {
		if !slices.Contains(names, k) {
			clone[k] = v
		}
	}
	var required []string
	switch r := params["required"].(type) {
	case []string:
		required = r
	case []any:
		for _, v := range r {
			if s, ok := v.(string); ok {
				required = append(required, s)
			}
		}
	}
	required = slices.DeleteFunc(slices.Clone(required), func(n string) bool { return slices.Contains(names, n) })
	return schema(clone, required...)
}

func (b *agentToolbox) mode(t *agentTool, kind string) string {
	return b.settings.Policy.Mode(t.name, t.area, kind)
}

// Specs are the tools the model is offered: all but those set to
// "Verboten".
func (b *agentToolbox) Specs(chat *agent.Chat) []agent.ToolSpec {
	specs := []agent.ToolSpec{}
	for _, t := range b.ordered {
		if b.mode(t, t.kind) == agent.ModeDeny {
			continue
		}
		var spec agent.ToolSpec
		spec.Type = "function"
		spec.Function.Name = t.name
		spec.Function.Description = strings.TrimSpace(strings.Split(t.description, "\n")[0])
		spec.Function.Parameters = t.params
		spec.Area, spec.Kind = t.area, t.kind
		specs = append(specs, spec)
	}
	return specs
}

// Prepare checks a call against the policy and plans it.
func (b *agentToolbox) Prepare(ctx context.Context, chat *agent.Chat, name string, args json.RawMessage) (agent.Prepared, error) {
	t, ok := b.tools[name]
	if !ok {
		return agent.Prepared{}, fmt.Errorf("there is no tool %q", name)
	}
	if b.mode(t, t.kind) == agent.ModeDeny && t.kind != agent.KindCreate && !(t.name == "file_write") {
		return agent.Prepared{}, fmt.Errorf("the person has not allowed %s for the agent (Einstellungen › KI-Agent)", name)
	}
	if t.own == nil {
		if err := toolcatalog.ValidateArguments(t.params, args); err != nil {
			return agent.Prepared{}, err
		}
	}
	p := agent.Prepared{Tool: name, Args: args, Area: t.area, Untrusted: t.untrusted}
	ctx = b.haContext(ctx, t)
	kind := t.kind
	if t.kindOf != nil {
		kind = t.kindOf(args)
	}
	if t.plan != nil {
		planned := args
		var fields map[string]json.RawMessage
		if json.Unmarshal(args, &fields) == nil {
			delete(fields, "confirm")
			if len(fields["reason"]) == 0 {
				fields["reason"] = json.RawMessage(`"KI-Agent"`)
			}
			planned, _ = json.Marshal(fields)
		}
		a, err := t.plan(ctx, planned)
		if err != nil {
			return agent.Prepared{}, err
		}
		p.Level = a.Level
		p.Summary = actionSummary(a)
		p.Plan, _ = json.Marshal(a)
		// A new file only adds something; replacing one changes it.
		if a.Key == "file.write" || a.Key == "folder.create" {
			kind = agent.KindCreate
		} else if a.Key == "file.replace" || a.Key == "file.edit" || a.Key == "files.moveTo" {
			kind = agent.KindChange
		}
	} else if t.route != nil && t.route.Method != http.MethodGet {
		p.Level = toolcatalog.LevelNormal
		if mcpNeedsConfirm(*t.route) {
			p.Level = toolcatalog.LevelDestroy
		}
		p.Summary = t.name + " " + compactArgs(args)
	}
	mode := b.mode(t, kind)
	if mode == agent.ModeDeny {
		return agent.Prepared{}, fmt.Errorf("the person has not allowed %s for the agent (Einstellungen › KI-Agent)", name)
	}
	if fm := b.folderMode(args); fm != "" {
		if fm == agent.ModeDeny {
			return agent.Prepared{}, errors.New("this folder is locked for the agent / dieser Ordner ist für den Agenten gesperrt")
		}
		mode = agent.Stricter(mode, fm)
	}
	if err := b.initiativeCheck(ctx, chat, name, args, &mode); err != nil {
		return agent.Prepared{}, err
	}
	// After outside content was read, nothing goes out to the web without
	// the person: a page or file could have told the model to send data.
	if t.area == agent.AreaWeb && chat != nil && (chat.Tainted || chat.OutsideSkill) {
		mode = agent.Stricter(mode, agent.ModeAsk)
	}
	if t.area == toolcatalog.AreaHome {
		b.loadAreas(ctx)
		mode = agent.Stricter(mode, b.haMode(name, args, mode))
		if mode == agent.ModeDeny {
			return agent.Prepared{}, errors.New("this device is not allowed for the agent")
		}
	}
	if t.name == "code_run" {
		if _, err := parseCode(args); err != nil {
			return agent.Prepared{}, err
		}
		mode = b.codeMode(mode)
		p.Summary = codeSummary(args)
	}
	if p.Summary == "" && t.kind != agent.KindRead && t.kind != agent.KindAgent {
		p.Summary = t.name + " " + compactArgs(args)
	}
	p.Mode, p.Kind = mode, kind
	return p, nil
}

func actionSummary(a *toolcatalog.Action) string {
	keys := make([]string, 0, len(a.Params))
	for k := range a.Params {
		if k != "preview" && k != "content" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := []string{}
	for _, k := range keys {
		v := a.Params[k]
		if r := []rune(v); len(r) > 80 {
			v = string(r[:80]) + "…"
		}
		parts = append(parts, k+"="+v)
	}
	if len(parts) == 0 {
		return a.Key
	}
	return a.Key + " (" + strings.Join(parts, ", ") + ")"
}

func compactArgs(args json.RawMessage) string {
	s := string(args)
	if r := []rune(s); len(r) > 160 {
		s = string(r[:160]) + "…"
	}
	return s
}

// folderMode is the strictest folder rule any path of the call touches.
func (b *agentToolbox) folderMode(args json.RawMessage) string {
	if len(b.settings.Policy.Folders) == 0 {
		return ""
	}
	var fields map[string]any
	if json.Unmarshal(args, &fields) != nil {
		return ""
	}
	str := func(k string) string { v, _ := fields[k].(string); return v }
	type target struct{ root, path string }
	var targets []target
	root, toRoot := str("root"), str("toRoot")
	if toRoot == "" {
		toRoot = root
	}
	for _, k := range []string{"path", "from", "name", "oldPath"} {
		if v := str(k); v != "" || k == "path" {
			targets = append(targets, target{root, v})
		}
	}
	for _, k := range []string{"to", "toDir", "toPath", "newPath", "destination"} {
		if v := str(k); v != "" {
			targets = append(targets, target{toRoot, v})
		}
	}
	if list, ok := fields["paths"].([]any); ok {
		for _, v := range list {
			if s, ok := v.(string); ok {
				targets = append(targets, target{root, s})
			}
		}
	}
	if moves, ok := fields["moves"].([]any); ok {
		for _, m := range moves {
			if mm, ok := m.(map[string]any); ok {
				from, _ := mm["from"].(string)
				to, _ := mm["to"].(string)
				targets = append(targets, target{root, from}, target{root, to})
			}
		}
	}
	mode := ""
	for _, t := range targets {
		if t.root == "" {
			continue
		}
		if m := b.realFolderMode(t.root, t.path); m != "" {
			if mode == "" {
				mode = m
			} else {
				mode = agent.Stricter(mode, m)
			}
		}
	}
	return mode
}

// rootDir is the directory of a root of the file manager. Rules may name
// roots this account does not see (an owner set them up); they resolve
// through the roots of the owner.
func (b *agentToolbox) rootDir(id string) string {
	if b.dirs == nil {
		env := b.s.agentEnv()
		b.dirs = map[string]string{}
		for _, r := range env.fileRoots(b.user) {
			b.dirs[r.ID] = r.Dir
		}
		if owner, ok := env.users.Get(b.s.firstOwner()); ok {
			for _, r := range env.fileRoots(owner) {
				if _, seen := b.dirs[r.ID]; !seen {
					b.dirs[r.ID] = r.Dir
				}
			}
		}
	}
	return b.dirs[id]
}

// realFolderMode applies the folder rules to the real directory a path
// lies in, so that a rule on share/Documents also holds when the same
// folder is reached through volume/tank/Documents.
func (b *agentToolbox) realFolderMode(root, p string) string {
	dir := b.rootDir(root)
	if dir == "" {
		return b.settings.Policy.FolderMode(root, p)
	}
	// Links are resolved on both sides: a link in an allowed folder that
	// leads into a locked one (made over SMB, say) must not open it.
	target := resolveLinks(filepath.Join(dir, filepath.FromSlash(path.Clean("/"+p))))
	mode := ""
	for _, f := range b.settings.Policy.Folders {
		base := b.rootDir(f.Root)
		if base == "" {
			continue
		}
		rule := resolveLinks(filepath.Join(base, filepath.FromSlash(path.Clean("/"+f.Path))))
		if target == rule || strings.HasPrefix(target, rule+string(filepath.Separator)) {
			if mode == "" {
				mode = f.Mode
			} else {
				mode = agent.Stricter(mode, f.Mode)
			}
		}
	}
	return mode
}

// resolveLinks returns p with the symbolic links of its existing part
// resolved; what does not exist yet (the name of a new file) stays as it
// is behind the deepest folder that does.
func resolveLinks(p string) string {
	rest := ""
	for cur := p; ; {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest, cur = filepath.Join(filepath.Base(cur), rest), parent
	}
}

// Execute runs a prepared call as the account.
func (b *agentToolbox) Execute(ctx context.Context, chat *agent.Chat, p agent.Prepared) (agent.Outcome, error) {
	t, ok := b.tools[p.Tool]
	if !ok {
		return agent.Outcome{}, fmt.Errorf("there is no tool %q", p.Tool)
	}
	chatID := ""
	if chat != nil {
		chatID = chat.ID
	}
	ctx = context.WithValue(ctx, agentCallKey{}, chatID)
	ctx = b.haContext(ctx, t)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	switch {
	case t.own != nil:
		return t.own(ctx, chat, p.Args)
	case t.read != nil:
		res, err := t.read(ctx, p.Args)
		if err != nil {
			return agent.Outcome{}, err
		}
		return agent.Outcome{Text: b.filterHidden(res.Text)}, nil
	case t.plan != nil:
		var a toolcatalog.Action
		if err := json.Unmarshal(p.Plan, &a); err != nil {
			return agent.Outcome{}, errors.New("the planned step is unreadable")
		}
		snap := b.agentSnapshotBefore(ctx, p.Tool, p.Args)
		text, _, err := b.s.callAction(ctx, b.session, &a)
		if err != nil {
			return agent.Outcome{}, err
		}
		out := agent.Outcome{Text: text}
		if snap != "" {
			out.Note, out.NoteText = "snapshot", snap
		}
		if a.Key == "file.write" || a.Key == "file.replace" || a.Key == "file.edit" {
			out.Files = []agent.FileRef{{Root: a.Params["root"], Path: a.Params["path"]}}
		}
		return out, nil
	case t.route != nil:
		values := map[string]any{}
		if err := json.Unmarshal(p.Args, &values); err != nil {
			return agent.Outcome{}, toolcatalog.Errorf("invalid arguments")
		}
		if mcpNeedsConfirm(*t.route) {
			values["confirm"] = true
		}
		text, _, err := b.s.callRoute(ctx, b.session, *t.route, values)
		if err != nil {
			return agent.Outcome{}, err
		}
		return agent.Outcome{Text: b.filterHidden(text)}, nil
	}
	return agent.Outcome{}, errors.New("tool cannot run")
}

// filterHidden drops entries of listings and searches that lie in folders
// locked for the agent.
func (b *agentToolbox) filterHidden(text string) string {
	if len(b.settings.Policy.Folders) == 0 || !strings.Contains(text, `"entries"`) {
		return text
	}
	var out map[string]any
	if json.Unmarshal([]byte(text), &out) != nil {
		return text
	}
	root, _ := out["root"].(string)
	entries, ok := out["entries"].([]any)
	if !ok || root == "" {
		return text
	}
	base, _ := out["path"].(string)
	kept := []any{}
	for _, e := range entries {
		m, _ := e.(map[string]any)
		p, _ := m["path"].(string)
		if p == "" {
			name, _ := m["name"].(string)
			p = strings.Trim(base+"/"+name, "/")
		}
		if b.realFolderMode(root, p) != agent.ModeDeny {
			kept = append(kept, e)
		}
	}
	out["entries"] = kept
	data, err := json.Marshal(out)
	if err != nil {
		return text
	}
	return string(data)
}

// haMode applies the Home Assistant rules of the agent to a call: every
// entity it names anywhere in its arguments (ha_call's entities, the
// targets inside an automation) and the domain of a service. Locks and
// alarm systems ask unless the person decided otherwise; saving an
// automation or script whose devices cannot be told asks as well.
func (b *agentToolbox) haMode(name string, args json.RawMessage, mode string) string {
	rules := b.settings.HA.Entities
	var fields any
	if json.Unmarshal(args, &fields) != nil {
		return mode
	}
	var ids, domains []string
	var walk func(key string, v any)
	walk = func(key string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, vv := range x {
				walk(k, vv)
			}
		case []any:
			for _, vv := range x {
				walk(key, vv)
			}
		case string:
			switch key {
			case "entity_id", "entity", "entities", "entityId":
				for _, id := range strings.Split(x, ",") {
					if id = strings.TrimSpace(id); id != "" {
						ids = append(ids, id)
					}
				}
			case "domain":
				domains = append(domains, x)
			case "service", "action":
				if d, _, ok := strings.Cut(x, "."); ok {
					domains = append(domains, d)
				}
			}
		}
	}
	walk("", fields)
	ruleFor := func(entity, domain string) string {
		if entity != "" {
			if v := rules["entity:"+entity]; agent.ValidMode(v) {
				return v
			}
			if area := b.areas[entity]; area != "" {
				for key, v := range rules {
					if name, ok := strings.CutPrefix(key, "area:"); ok && agent.ValidMode(v) && sameArea(name, area) {
						return v
					}
				}
			}
		}
		if v := rules["domain:"+domain]; agent.ValidMode(v) {
			return v
		}
		if domain == "lock" || domain == "alarm_control_panel" {
			return agent.ModeAsk
		}
		return ""
	}
	out := ""
	stricter := func(m string) {
		if m == "" {
			return
		}
		if out == "" {
			out = m
		} else {
			out = agent.Stricter(out, m)
		}
	}
	for _, id := range ids {
		domain, _, _ := strings.Cut(id, ".")
		stricter(ruleFor(id, domain))
	}
	// The call's own domain counts where it names no device of that domain
	// (e.g. "all lights"); with named devices their rules decide, so an
	// exception for one device works inside a denied domain (PLAN 59).
	for _, d := range domains {
		if !slices.ContainsFunc(ids, func(id string) bool { return strings.HasPrefix(id, d+".") }) {
			stricter(ruleFor("", d))
		}
	}
	if (name == "ha_automation_save" || name == "ha_script_save") && len(ids) == 0 {
		stricter(agent.ModeAsk)
	}
	if out == "" {
		return mode
	}
	return out
}

// haContext gives Home Assistant calls the agent's own release list.
func (b *agentToolbox) haContext(ctx context.Context, t *agentTool) context.Context {
	if t.area != toolcatalog.AreaHome {
		return ctx
	}
	releases := map[string]homeassistant.Release{homeassistant.PrimaryID: b.settings.HA.Release}
	for id, r := range b.settings.HA.Releases {
		if id != homeassistant.PrimaryID {
			releases[id] = r
		}
	}
	return homeassistant.WithReleases(ctx, releases)
}

// loadAreas reads the rooms of the entities once per run, only when a rule
// names an area.
func (b *agentToolbox) loadAreas(ctx context.Context) {
	if b.areas != nil || b.s.haHub() == nil {
		return
	}
	b.areas = map[string]string{}
	for key := range b.settings.HA.Entities {
		if strings.HasPrefix(key, "area:") {
			// Entity IDs of the connections, with their rooms.
			for _, ha := range b.s.haHub().Active() {
				if areas, err := ha.Areas(ctx); err == nil {
					for entity, area := range areas {
						if _, known := b.areas[entity]; !known {
							b.areas[entity] = area
						}
					}
				}
			}
			return
		}
	}
}

// sameArea compares an area of a rule with the name of a room loosely:
// "kueche", "Küche" and "küche" are the same.
func sameArea(rule, area string) bool {
	norm := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		return strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss", " ", "_", "-", "_").Replace(s)
	}
	return norm(rule) == norm(area)
}
