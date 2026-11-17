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
	// areaOf decides the area from the arguments, for a tool that works on
	// things of several areas (cluster_action: a container is Docker, a VM
	// VMs, a system container LXC; A-41, 1.03). areas are all it can give;
	// area stays the one for arguments that name none of them.
	areaOf func(json.RawMessage) string
	areas  []string
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
	// The profile of Home Assistant is no account: its tools ask titand in
	// the name of the acting account (PLAN 91).
	acting := user
	if user.Name == agent.AssistProfile {
		acting, _ = s.assistActor()
	}
	b.session = &mcpSession{user: acting, token: auth.APIToken{Name: "KI-Agent"}, version: mcpVersions[0], r: r}
	on := env.servicesOn()
	// A switched-off service has no tools; agent_access and tools_load say
	// why (1.10). System containers have a switch of their own.
	off := func(name, area string) bool {
		if strings.HasPrefix(name, "lxc_") {
			return !on["lxc"]
		}
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
		if mcpSkip[tool.Name] || off(tool.Name, tool.Area) {
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
		// V12 (owner 17 Nov 2026): looking for updates changes nothing on
		// the NAS; installing still asks.
		switch tool.Name {
		case "docker_check_updates":
			t.kind = agent.KindRead
		case "update_action":
			t.kindOf = func(args json.RawMessage) string {
				if argString(args, "action") == "check" {
					return agent.KindRead
				}
				return agent.KindChange
			}
		}
		if t.params == nil {
			t.params = schema(nil)
		}
		add(t)
	}
	allowed := func(scope string) bool { return !auth.AdminScope(scope) || user.Role.CanAdminister() }
	for i := range publicRoutes {
		route := &publicRoutes[i]
		if route.Tool == "" || !allowed(route.Scope) || off(route.Tool, routeArea(route.Scope)) || agentRetired[route.Tool].survivor != "" {
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
	for _, t := range b.remoteTools() {
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
	if t := b.photosTool(); t != nil {
		add(t)
	}
	// 1.10: what the agent may see, and reading what happened.
	add(b.accessTool())
	add(b.scheduleDeleteTool())
	for _, t := range b.insightTools() {
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
	return b.modeIn(t, t.area, kind)
}

// modeIn is the mode of a tool for a call in an area (see areaOf).
func (b *agentToolbox) modeIn(t *agentTool, area, kind string) string {
	mode := b.settings.Policy.Mode(t.name, area, kind)
	// PLAN 91: a question from Home Assistant may use less, never more.
	if b.user.Name == agent.AssistProfile {
		return b.settings.HA.assistMode(t.name, area, kind, mode)
	}
	return mode
}

// areaFor is the area a call of the tool falls under.
func (t *agentTool) areaFor(args json.RawMessage) string {
	if t.areaOf != nil {
		if a := t.areaOf(args); a != "" {
			return a
		}
	}
	return t.area
}

// offered reports whether the model gets the tool: not when it is
// forbidden in every area it can act in.
func (b *agentToolbox) offered(t *agentTool) bool {
	for _, area := range append([]string{t.area}, t.areas...) {
		if b.modeIn(t, area, t.kind) != agent.ModeDeny {
			return true
		}
		// A tool that also reads (update_action check) is offered where
		// reading is allowed; Prepare decides per call.
		if t.kindOf != nil && t.kind != agent.KindAgent && b.modeIn(t, area, agent.KindRead) != agent.ModeDeny {
			return true
		}
	}
	return false
}

// agentRetired are tools of the public routes the agent no longer gets
// (1.10, V10, owner 17 Nov 2026: one tool for each task; MCP keeps them),
// with the tool that does the same and for which of its calls. A rule the
// person set for an old name still holds for those calls.
var agentRetired = map[string]struct {
	survivor string
	covers   func(args json.RawMessage) bool
}{
	"files_read_text":  {"file_read", func(json.RawMessage) bool { return true }},
	"files_write_text": {"file_write", func(args json.RawMessage) bool { return argBool(args, "replace") }},
	"files_create":     {"file_write", func(args json.RawMessage) bool { return !argBool(args, "replace") }},
	"files_copy":       {"file_copy", func(json.RawMessage) bool { return true }},
	"vm_start":         {"vm_action", func(args json.RawMessage) bool { return argString(args, "action") == "start" }},
	"vm_shutdown":      {"vm_action", func(args json.RawMessage) bool { return argString(args, "action") == "shutdown" }},
	"vm_reboot":        {"vm_action", func(args json.RawMessage) bool { return argString(args, "action") == "reboot" }},
}

// callMode is the mode of one call: modeIn, with the person's rules for
// retired tools that did the same.
func (b *agentToolbox) callMode(t *agentTool, area, kind string, args json.RawMessage) string {
	p := b.settings.Policy
	mode := p.Mode(t.name, area, kind)
	old := ""
	for name, r := range agentRetired {
		if r.survivor != t.name || !agent.ValidMode(p.Tools[name]) || !r.covers(args) {
			continue
		}
		if old == "" {
			old = p.Tools[name]
		} else {
			old = agent.Stricter(old, p.Tools[name])
		}
	}
	if old != "" {
		if agent.ValidMode(p.Tools[t.name]) {
			mode = agent.Stricter(mode, old)
		} else {
			mode = old
		}
	}
	if b.user.Name == agent.AssistProfile {
		return b.settings.HA.assistMode(t.name, area, kind, mode)
	}
	return mode
}

// argBool is a yes/no argument of a call, false when it is missing.
func argBool(args json.RawMessage, key string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(args, &fields) != nil {
		return false
	}
	var v bool
	_ = json.Unmarshal(fields[key], &v)
	return v
}

// Specs are the tools the model is offered: all but those set to
// "Verboten".
func (b *agentToolbox) Specs(chat *agent.Chat) []agent.ToolSpec {
	specs := []agent.ToolSpec{}
	for _, t := range b.ordered {
		if !b.offered(t) {
			continue
		}
		var spec agent.ToolSpec
		spec.Type = "function"
		spec.Function.Name = t.name
		spec.Function.Description = specDescription(t.description)
		spec.Function.Parameters = t.params
		spec.Area, spec.Kind = t.area, t.kind
		specs = append(specs, spec)
	}
	return specs
}

// specDescription is the first line of a description, without the "Propose
// to" of the MCP catalog: for the agent the call does it (TitanOS asks the
// person where the rights say so), it does not propose it (1.10, V10).
func specDescription(d string) string {
	d = strings.TrimSpace(strings.Split(d, "\n")[0])
	if rest, ok := strings.CutPrefix(d, "Propose to "); ok && rest != "" {
		d = strings.ToUpper(rest[:1]) + rest[1:]
	}
	return d
}

// Paged reports whether a tool takes an offset for its next page (the note
// of a cut list names one only then).
func (b *agentToolbox) Paged(name string) bool {
	t, ok := b.tools[name]
	if !ok {
		return false
	}
	props, _ := t.params["properties"].(map[string]any)
	_, ok = props["offset"]
	return ok
}

// Prepare checks a call against the policy and plans it.
func (b *agentToolbox) Prepare(ctx context.Context, chat *agent.Chat, name string, args json.RawMessage) (agent.Prepared, error) {
	t, ok := b.tools[name]
	if !ok {
		return agent.Prepared{}, fmt.Errorf("there is no tool %q", name)
	}
	area := t.areaFor(args)
	kind := t.kind
	if t.kindOf != nil {
		kind = t.kindOf(args)
	}
	if b.callMode(t, area, kind, args) == agent.ModeDeny && kind != agent.KindCreate && !(t.name == "file_write") {
		return agent.Prepared{}, fmt.Errorf("the person has not allowed %s for the agent (Einstellungen › KI-Agent)", name)
	}
	if t.own == nil {
		if err := toolcatalog.ValidateArguments(t.params, args); err != nil {
			return agent.Prepared{}, err
		}
	}
	p := agent.Prepared{Tool: name, Args: args, Area: area, Untrusted: t.untrusted}
	ctx = b.haContext(ctx, t)
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
	mode := b.callMode(t, area, kind, args)
	if mode == agent.ModeDeny {
		return agent.Prepared{}, fmt.Errorf("the person has not allowed %s for the agent (Einstellungen › KI-Agent)", name)
	}
	// The person's rules for devices come first: they may also switch
	// without asking; everything after this only tightens.
	if t.area == toolcatalog.AreaHome {
		b.loadAreas(ctx)
		if mode = b.haMode(name, kind, args, mode); mode == agent.ModeDeny {
			return agent.Prepared{}, errors.New("the person has not allowed this device for the agent (Einstellungen › KI-Agent › Home Assistant)")
		}
	}
	// Looking at a picture on the NAS reads a file: what the person set for
	// reading files holds, not the free pass of the agent's own tools (0.99).
	if t.name == "image_view" && argString(args, "root") != "" {
		read := b.settings.Policy.Mode("file_read", toolcatalog.AreaFiles, agent.KindRead)
		if ft := b.tools["file_read"]; ft != nil {
			read = b.mode(ft, agent.KindRead)
		}
		if read == agent.ModeDeny {
			return agent.Prepared{}, errors.New("the person has not allowed reading files for the agent (Einstellungen › KI-Agent)")
		}
		mode = agent.Stricter(mode, read)
	}
	if fm := b.folderMode(args, kind); fm != "" {
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
	// A chat that has only read the web so far holds nothing of the NAS a
	// page could ask for: it may search on, and read a page whose address
	// was already there (0.94, owner 15 Nov 2026: research asked at every
	// step although the web was allowed without asking).
	// The same holds for the tools of connected MCP servers: they send
	// what the model gives them to a server outside TitanOS (0.99).
	if (t.area == agent.AreaWeb || t.area == agent.AreaMCP) && chat != nil && (chat.Tainted || chat.OutsideSkill) && !b.webStaysFree(chat, name, args) {
		mode = agent.Stricter(mode, agent.ModeAsk)
	}
	if t.area == agent.AreaNVR {
		mode = b.nvrMode(t.name, mode)
	}
	// Deleting planned tasks cannot be undone: after outside content (a
	// file could say "delete all reminders") the person decides (M4).
	if t.name == "schedule_delete" && chat != nil && (chat.Tainted || chat.OutsideSkill) {
		mode = agent.Stricter(mode, agent.ModeAsk)
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
	// "Jeden Schritt fragen" in this chat: every change waits for the
	// person, whatever the rights allow without asking.
	if chat != nil && chat.Autonomy == agent.AutonomyAsk && mode == agent.ModeAllow && kind != agent.KindRead && kind != agent.KindAgent {
		mode = agent.ModeAsk
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

// argString is a text argument of a call, "" when it is missing.
func argString(args json.RawMessage, key string) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(args, &fields) != nil {
		return ""
	}
	var v string
	_ = json.Unmarshal(fields[key], &v)
	return v
}

// folderMode is the strictest folder rule any path of the call touches.
// What a call copies, moves, renames or deletes as a whole also counts
// when a rule's folder lies inside it: copying "Finanzen" must not carry
// the locked "Finanzen/Privat" to an open place (0.99).
func (b *agentToolbox) folderMode(args json.RawMessage, kind string) string {
	if len(b.settings.Policy.Folders) == 0 {
		return ""
	}
	var fields map[string]any
	if json.Unmarshal(args, &fields) != nil {
		return ""
	}
	str := func(k string) string { v, _ := fields[k].(string); return v }
	type target struct {
		root, path string
		// whole: the call takes this path with everything below it.
		whole bool
	}
	whole := kind != agent.KindRead
	var targets []target
	root, toRoot := str("root"), str("toRoot")
	if toRoot == "" {
		toRoot = root
	}
	for _, k := range []string{"path", "from", "name", "oldPath"} {
		if v := str(k); v != "" || k == "path" {
			// "path" of a tool that creates is the folder it creates in;
			// "name" is the new name there.
			taken := whole && v != "" && k != "name" && (k != "path" || kind == agent.KindChange)
			targets = append(targets, target{root, v, taken})
		}
	}
	for _, k := range []string{"to", "toDir", "toPath", "newPath", "destination"} {
		if v := str(k); v != "" {
			targets = append(targets, target{toRoot, v, false})
		}
	}
	if list, ok := fields["paths"].([]any); ok {
		for _, v := range list {
			if s, ok := v.(string); ok {
				targets = append(targets, target{root, s, whole && s != ""})
			}
		}
	}
	if moves, ok := fields["moves"].([]any); ok {
		for _, m := range moves {
			if mm, ok := m.(map[string]any); ok {
				from, _ := mm["from"].(string)
				to, _ := mm["to"].(string)
				targets = append(targets, target{root, from, whole && from != ""}, target{root, to, false})
			}
		}
	}
	mode := ""
	for _, t := range targets {
		if t.root == "" {
			continue
		}
		if m := b.realFolderMode(t.root, t.path, t.whole); m != "" {
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
func (b *agentToolbox) realFolderMode(root, p string, whole bool) string {
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
		inside := whole && f.Mode != agent.ModeAllow && strings.HasPrefix(rule, target+string(filepath.Separator))
		if target == rule || strings.HasPrefix(target, rule+string(filepath.Separator)) || inside {
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
	if b.hasDenyRules() {
		ctx = context.WithValue(ctx, agentFolderLockKey{}, agentFolderLock(func(root, p string) bool {
			return b.realFolderMode(root, p, false) == agent.ModeDeny
		}))
	}
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
		return agent.Outcome{Text: b.filterHidden(t.name, res.Text)}, nil
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
		return agent.Outcome{Text: b.filterHidden(t.name, text)}, nil
	}
	return agent.Outcome{}, errors.New("tool cannot run")
}

// agentFolderLockKey carries the folder rules of the agent into a search
// of the catalog, so that what lies in a locked folder is left out before
// it is counted and paged (security review 1.10, N4).
type agentFolderLockKey struct{}

// agentFolderLock reports whether a path of a root lies in a locked folder.
type agentFolderLock func(root, p string) bool

// filterHidden drops entries of listings and searches that lie in folders
// locked for the agent, and counts them in hidden. A search never counts:
// with a name or a size it would tell what lies there (N4); its locked
// matches are left out inside the search (agentFolderLockKey).
func (b *agentToolbox) filterHidden(tool, text string) string {
	if !b.hasDenyRules() || !strings.Contains(text, `"entries"`) {
		return text
	}
	var out map[string]any
	if json.Unmarshal([]byte(text), &out) != nil {
		return text
	}
	if tool == "files_search" {
		out["hidden"] = true
		out["why"] = "Searches never show folders the person locked for the agent, and do not say whether anything there matched. Do not call a file missing because of it; folders can be opened for the agent under " + whereFolders + "."
		if data, err := json.Marshal(out); err == nil {
			return string(data)
		}
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
		if b.realFolderMode(root, p, false) != agent.ModeDeny {
			kept = append(kept, e)
		}
	}
	out["entries"] = kept
	// 1.10 (V1): say that something was left out, not what. Without it an
	// empty folder and a locked one read the same.
	if n := len(entries) - len(kept); n > 0 {
		out["hidden"] = n
		out["why"] = fmt.Sprintf("%d entries of this page lie in folders the person locked for the agent and are not shown (total counts them). Do not call the folder empty or a file missing because of it; they can be opened for the agent under %s.", n, whereFolders)
	}
	data, err := json.Marshal(out)
	if err != nil {
		return text
	}
	return string(data)
}

// haMode applies the Home Assistant rules of the agent to a call: every
// entity it names anywhere in its arguments (ha_call's entities, the
// targets inside an automation) and the domain of a service.
//
// Owner 15.11.2026: the rules are about switching. Reading a device never
// asks because of them (asking for the state of the alarm panel wanted an
// approval); only a rule that forbids a device keeps it from being read.
// For a change the person's rule for a device decides: "allow" switches
// without asking even where the area asks, "ask" and "deny" tighten. A
// device without a rule follows the area; locks and alarm systems then ask
// at least. Several devices: the strictest. Saving an automation or script
// whose devices cannot be told asks. mode is what the general rights give
// the tool and is never "deny" here.
func (b *agentToolbox) haMode(name, kind string, args json.RawMessage, mode string) string {
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
	// ruleFor is the person's rule for a device or a domain, "" without one.
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
		return ""
	}
	read := kind == agent.KindRead
	out := ""
	add := func(entity, domain string) {
		m := ruleFor(entity, domain)
		switch {
		case read:
			if m != agent.ModeDeny {
				m = mode
			}
		case m == "":
			m = mode
			if haAsksFirst(domain) {
				m = agent.Stricter(m, agent.ModeAsk)
			}
		}
		if out == "" {
			out = m
		} else {
			out = agent.Stricter(out, m)
		}
	}
	for _, id := range ids {
		domain, _, _ := strings.Cut(id, ".")
		add(id, domain)
	}
	// The call's own domain counts where it names no device of that domain
	// (e.g. "all lights"); with named devices their rules decide, so an
	// exception for one device works inside a denied domain (PLAN 59).
	for _, d := range domains {
		if !slices.ContainsFunc(ids, func(id string) bool { return strings.HasPrefix(id, d+".") }) {
			add("", d)
		}
	}
	if out == "" {
		out = mode
	}
	if (name == "ha_automation_save" || name == "ha_script_save") && len(ids) == 0 {
		out = agent.Stricter(out, agent.ModeAsk)
	}
	return out
}

// haAsksFirst are the kinds whose switching asks first unless the person
// set a rule for them: locks and alarm systems, and since 1.09 (owner,
// 17 Nov 2026) automations. Scripts and scenes switch as before, so that
// "Szene Kino an" works at once from quick commands and Assist.
func haAsksFirst(domain string) bool {
	switch domain {
	case "lock", "alarm_control_panel", "automation":
		return true
	}
	return false
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

// webStaysFree reports whether a web step in a chat that has read outside
// content needs no extra approval: the chat has read nothing but the web,
// no outside skill is in the prompt, and the step is a search or reads an
// address the model did not write itself – one that stands in a message of
// the person or in what an earlier search or page of this chat returned.
// Such an address existed before, so it cannot carry data out. Results of
// other tools do not count: they may repeat what the model gave them.
//
// With "Internet auch nach gelesenen Inhalten ohne Nachfrage" of the rights
// (Policy.WebFree) the person has taken that risk for every chat. That
// holds also with a skill from outside or a marked memory in the prompt
// (owner 16 Nov 2026: the switch was on and the agent still asked before
// every search, because one memory had been saved after a web search).
var jsonPlain = strings.NewReplacer(`\u0026`, "&", `\u003c`, "<", `\u003e`, ">")

func (b *agentToolbox) webStaysFree(chat *agent.Chat, name string, args json.RawMessage) bool {
	if b.settings.Policy.WebFree {
		return true
	}
	if chat.OutsideSkill {
		return false
	}
	if !chat.WebOnly {
		return false
	}
	switch name {
	case "web_search":
		return true
	case "web_read":
	default:
		return false
	}
	var req struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(args, &req) != nil {
		return false
	}
	address := strings.TrimSpace(req.URL)
	if len(address) < 12 {
		return false
	}
	for _, e := range chat.Entries {
		text := ""
		switch {
		case e.Role == "user" && !chat.Helper:
			text = e.Content
		case e.Role == "tool" && (e.Tool == "web_search" || e.Tool == "web_read") && !strings.HasPrefix(e.Result, "Error:"):
			// Results are kept as JSON, which writes "&" as \u0026: an
			// address with two parameters was never found again (0.99).
			text = jsonPlain.Replace(e.Result)
		}
		if text != "" && strings.Contains(text, address) {
			return true
		}
	}
	return false
}
