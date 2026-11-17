package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/office"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// Further parts of the KI-Agent (PLAN 44, Runde 5): the other nodes of the
// cluster, attachments, project files, export, undo, model preload and
// download, and the weekly report.

// ---- Cluster -------------------------------------------------------------

// The agent reaches the other nodes through the proxy of the cluster as
// its account, like the web interface does; the other node checks the
// account itself. Only a few fixed reads and starting/stopping containers,
// VMs and system containers are offered (decision of the owner 5.11.2026:
// "Cluster: alle Knoten").

var clusterReads = map[string]string{
	"status":        "system",
	"storage":       "storage/volumes",
	"docker":        "docker/containers",
	"vms":           "vms",
	"lxc":           "lxc",
	"notifications": "notifications",
	"backups":       "backups",
}

func (b *agentToolbox) clusterTools() []*agentTool {
	env := b.s.agentEnv()
	c := env.cluster
	if c == nil || !c.InCluster() || !c.Manages() || !b.user.Role.CanAdminister() {
		return nil
	}
	return []*agentTool{
		{name: "cluster_nodes", area: "system", kind: agent.KindRead,
			description: "The nodes of the cluster with name, id, online, version, CPU and memory.",
			params:      schema(nil), own: b.clusterNodes},
		{name: "cluster_read", area: "system", kind: agent.KindRead, untrusted: true,
			description: "Read the state of another node of the cluster: status, storage, docker, vms, lxc, notifications or backups.",
			params:      schema(map[string]any{"node": str("Node id from cluster_nodes."), "what": enum("What to read.", "status", "storage", "docker", "vms", "lxc", "notifications", "backups")}, "node", "what"),
			own:         b.clusterRead},
		b.clusterActionTool(),
	}
}

// clusterActionTool starts, stops or restarts something on another node.
func (b *agentToolbox) clusterActionTool() *agentTool {
	return &agentTool{name: "cluster_action", area: toolcatalog.AreaVMs, kind: agent.KindChange,
		// Each kind under the area of the same action on this node
		// (A-41, 1.03): containers under Docker, VMs and system
		// containers under VMs (where lxc_action is). With Docker
		// forbidden and VMs without asking, a container on another node
		// stays forbidden.
		areas: []string{toolcatalog.AreaDocker}, areaOf: clusterActionArea,
		description: "Start, stop or restart a container, VM or system container on another node of the cluster.",
		params: schema(map[string]any{"node": str("Node id from cluster_nodes."), "kind": enum("What it is.", "container", "vm", "lxc"),
			"name": str("Name (for containers the id or name from cluster_read docker)."), "action": enum("What to do.", "start", "stop", "restart")}, "node", "kind", "name", "action"),
		own: b.clusterAction}
}

// clusterActionArea is the area of the rights a cluster_action falls
// under: the one of the same action on this node (docker_container_*,
// vm_action, lxc_action).
func clusterActionArea(args json.RawMessage) string {
	var req struct{ Kind string }
	_ = json.Unmarshal(args, &req)
	return map[string]string{"container": toolcatalog.AreaDocker, "vm": toolcatalog.AreaVMs, "lxc": toolcatalog.AreaVMs}[req.Kind]
}

func (b *agentToolbox) clusterCall(ctx context.Context, node, method, p string, body any) (string, error) {
	env := b.s.agentEnv()
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = strings.NewReader(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, method, "/api/v1/cluster/nodes/"+url.PathEscape(node)+"/api/"+p, reader)
	if err != nil {
		return "", err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	env.cluster.Proxy(rec, req, node, b.user.Name, p)
	data, _ := io.ReadAll(io.LimitReader(rec.Result().Body, 2<<20))
	if rec.Code >= 300 {
		return "", fmt.Errorf("node answered HTTP %d: %s", rec.Code, strings.TrimSpace(string(data)))
	}
	return string(data), nil
}

func (b *agentToolbox) clusterNodes(ctx context.Context, _ *agent.Chat, _ json.RawMessage) (agent.Outcome, error) {
	env := b.s.agentEnv()
	type node struct {
		ID, Name, Version string
		Self, Online      bool
		CPU               *float64 `json:"cpuPercent,omitempty"`
		Error             string   `json:",omitempty"`
	}
	var out []node
	for _, n := range env.cluster.Overview(ctx) {
		v := node{ID: n.ID, Name: n.Name, Self: n.Self, Online: n.Online, Error: n.Error}
		if n.Status != nil {
			v.Version, v.CPU = n.Status.Version, n.Status.CPU
		}
		out = append(out, v)
	}
	res, err := toolcatalog.JSON(map[string]any{"nodes": out})
	return agent.Outcome{Text: res.Text}, err
}

func (b *agentToolbox) clusterRead(ctx context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	var req struct{ Node, What string }
	if err := json.Unmarshal(args, &req); err != nil {
		return agent.Outcome{}, toolcatalog.Errorf("invalid arguments")
	}
	p, ok := clusterReads[req.What]
	if !ok || req.Node == "" {
		return agent.Outcome{}, toolcatalog.Errorf("give a node and what to read")
	}
	text, err := b.clusterCall(ctx, req.Node, http.MethodGet, p, nil)
	if err != nil {
		return agent.Outcome{}, err
	}
	if r := []rune(text); len(r) > agent.MaxResultText {
		text = string(r[:agent.MaxResultText])
	}
	return agent.Outcome{Text: text}, nil
}

func (b *agentToolbox) clusterAction(ctx context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	env := b.s.agentEnv()
	var req struct{ Node, Kind, Name, Action string }
	if err := json.Unmarshal(args, &req); err != nil {
		return agent.Outcome{}, toolcatalog.Errorf("invalid arguments")
	}
	if req.Node == "" || req.Name == "" || strings.ContainsAny(req.Name, "/?#") {
		return agent.Outcome{}, toolcatalog.Errorf("give node, kind, name and action")
	}
	var p string
	switch req.Kind + "/" + req.Action {
	case "container/start", "container/stop", "container/restart":
		p = "docker/containers/" + url.PathEscape(req.Name) + "/" + req.Action
	case "vm/start", "vm/restart":
		p = "vms/" + url.PathEscape(req.Name) + "/" + strings.Replace(req.Action, "restart", "reboot", 1)
	case "vm/stop":
		p = "vms/" + url.PathEscape(req.Name) + "/shutdown"
	case "lxc/start", "lxc/stop", "lxc/restart":
		p = "lxc/" + url.PathEscape(req.Name) + "/" + req.Action
	default:
		return agent.Outcome{}, toolcatalog.Errorf("unknown kind or action")
	}
	text, err := b.clusterCall(ctx, req.Node, http.MethodPost, p, nil)
	if err != nil {
		return agent.Outcome{}, err
	}
	if env.activity != nil {
		env.activity.Record(activity.Entry{User: b.user.Name, Via: activity.ViaAgent, Action: "cluster_" + req.Kind + "_" + req.Action, Target: req.Node + ", " + req.Name, Status: 200, OK: true})
	}
	if strings.TrimSpace(text) == "" {
		text = `{"ok":true}`
	}
	return agent.Outcome{Text: text}, nil
}

// ---- Attachments ---------------------------------------------------------

const maxAttachment = 20 << 20

// handleAgentAttach keeps a file uploaded into a chat; the agent reads it
// with attachment_read. Files of the NAS are attached by reference.
func (s *Server) handleAgentAttach(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id := r.PathValue("id")
	if !agent.ValidID(id) {
		agentStoreError(w, agent.ErrNotFound)
		return
	}
	if _, err := s.agentStore().Chat(user.Name, id); err != nil {
		agentStoreError(w, err)
		return
	}
	name := path.Base(strings.ReplaceAll(r.URL.Query().Get("name"), "\\", "/"))
	if name == "" || name == "." || name == "/" || len(name) > 200 || !utf8.ValidString(name) || strings.HasPrefix(name, ".") {
		writeError(w, http.StatusBadRequest, "invalid_name", "invalid file name")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxAttachment+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "upload_failed", err.Error())
		return
	}
	if len(data) > maxAttachment {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "at most 20 MB")
		return
	}
	// The name it is kept under: another one with this name stays (1.01).
	name, err = s.agentStore().SaveAttachment(user.Name, id, name, data)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"name": name, "size": len(data)})
}

func (b *agentToolbox) attachmentTool() *agentTool {
	return &agentTool{name: "attachment_read", area: agent.AreaAgent, kind: agent.KindAgent, untrusted: true,
		description: "Read a file the person attached to this chat (text, Markdown, CSV, JSON, Word, Excel, PDF).",
		params:      schema(map[string]any{"name": str("File name as attached.")}, "name"),
		own: func(ctx context.Context, chat *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
			var req struct{ Name string }
			if err := json.Unmarshal(args, &req); err != nil || chat == nil {
				return agent.Outcome{}, toolcatalog.Errorf("invalid arguments")
			}
			data, err := b.s.agentStore().Attachment(b.user.Name, chat.ID, req.Name)
			if err != nil {
				return agent.Outcome{}, toolcatalog.Errorf("no attachment %q in this chat", req.Name)
			}
			text, err := attachmentText(ctx, req.Name, data)
			if err != nil {
				return agent.Outcome{}, err
			}
			if r := []rune(text); len(r) > agent.MaxResultText*3 {
				text = string(r[:agent.MaxResultText*3]) + "\n[… cut]"
			}
			return agent.Outcome{Text: text}, nil
		}}
}

func attachmentText(ctx context.Context, name string, data []byte) (string, error) {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".pdf"):
		r, err := office.ReadPDF(ctx, data, 1, 20)
		if err != nil {
			return "", toolcatalog.Errorf("the PDF could not be read: %v", err)
		}
		out, _ := json.Marshal(r)
		return string(out), nil
	case strings.HasSuffix(lower, ".docx"), strings.HasSuffix(lower, ".xlsx"):
		return office.Text(name, data)
	}
	if !utf8.Valid(data) {
		return "", toolcatalog.Errorf("this file is no text, PDF, Word or Excel document (for pictures use image_view)")
	}
	return string(data), nil
}

// ---- Project files ---------------------------------------------------------

// handleAgentProjectFiles lists the files the agent made in the chats of a
// project, newest first.
func (s *Server) handleAgentProjectFiles(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r).Name
	id := r.PathValue("id")
	store := s.agentStore()
	list, err := store.Chats(user)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	type file struct {
		agent.FileRef
		Chat string    `json:"chat"`
		At   time.Time `json:"at"`
	}
	out := []file{}
	seen := map[string]bool{}
	for _, info := range list {
		if info.Project != id {
			continue
		}
		chat, err := store.Chat(user, info.ID)
		if err != nil {
			continue
		}
		for i := len(chat.Entries) - 1; i >= 0; i-- {
			for _, f := range chat.Entries[i].Files {
				if key := f.Root + "/" + f.Path; !seen[key] {
					seen[key] = true
					out = append(out, file{f, chat.ID, chat.Entries[i].At})
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": out})
}

// ---- Export ------------------------------------------------------------------

// exportStepWords are the steps in plain words, as the chat shows them
// (web: STEP_WORDS in lib/agent.ts, texts agent.step.<word> of the
// translation files; TestExportStepWordsMatchThePage keeps them alike).
var exportStepWords = map[string]string{
	"files_list": "files", "files_search": "search", "file_read": "read", "document_read": "read", "files_read_text": "read",
	"file_write": "write", "files_write_text": "write", "files_move": "move", "file_copy": "copy", "files_copy": "copy",
	"files_mkdir": "folder", "files_delete": "delete", "docker_list": "docker", "container_logs": "logs", "logs_search": "logs",
	"vm_list": "vms", "storage_overview": "storage", "shares_list": "shares", "my_shares": "shares", "system_status": "system",
	"hardware_status": "hardware", "backup_status": "backups", "notifications": "notifications", "web_search": "web",
	"web_read": "page", "memory_save": "memory", "skill_save": "skill", "ask_user": "ask", "ha_entities": "home",
	"ha_state": "home", "ha_call": "homeCall", "ha_history": "home", "ha_diagnostics": "home", "plan_update": "plan",
	"tools_load": "toolsLoad", "task_delegate": "delegate", "code_run": "code",
}

// exportTexts are the words of an export in German and English.
var exportTexts = map[string]map[string]string{
	"de": {"you": "Du", "agent": "KI-Agent",
		"files": "Ordner gelesen", "search": "Dateien gesucht", "read": "Datei gelesen", "write": "Datei geschrieben",
		"move": "Verschoben", "copy": "Kopiert", "folder": "Ordner angelegt", "delete": "Gelöscht", "docker": "Docker-Apps gelesen",
		"logs": "Protokolle durchsucht", "vms": "VMs gelesen", "storage": "Speicher gelesen", "shares": "Freigaben gelesen",
		"system": "System gelesen", "hardware": "Hardware gelesen", "backups": "Sicherungen gelesen", "notifications": "Meldungen gelesen",
		"web": "Im Web gesucht", "page": "Webseite gelesen", "memory": "Gemerkt", "skill": "Skill gespeichert", "ask": "Rückfrage",
		"home": "Home Assistant gelesen", "homeCall": "In Home Assistant geschaltet", "plan": "Plan aktualisiert",
		"toolsLoad": "Werkzeuge geladen", "delegate": "Helfer beauftragt", "code": "Code ausgeführt"},
	"en": {"you": "You", "agent": "AI agent",
		"files": "Folder read", "search": "Files searched", "read": "File read", "write": "File written",
		"move": "Moved", "copy": "Copied", "folder": "Folder created", "delete": "Deleted", "docker": "Docker apps read",
		"logs": "Logs searched", "vms": "VMs read", "storage": "Storage read", "shares": "Shares read",
		"system": "System read", "hardware": "Hardware read", "backups": "Backups read", "notifications": "Notifications read",
		"web": "Searched the web", "page": "Web page read", "memory": "Remembered", "skill": "Skill saved", "ask": "Question",
		"home": "Home Assistant read", "homeCall": "Switched in Home Assistant", "plan": "Plan updated",
		"toolsLoad": "Tools loaded", "delegate": "Helper asked", "code": "Code run"},
}

// exportStepDetail is what a step was about, like the chat shows it
// (stepDetail in lib/agent.ts): place, query, address, name, device.
func exportStepDetail(args json.RawMessage) string {
	var a map[string]any
	if json.Unmarshal(args, &a) != nil {
		return ""
	}
	str := func(k string) string { v, _ := a[k].(string); return v }
	var where []string
	for _, v := range []string{str("root"), str("path")} {
		if v != "" {
			where = append(where, v)
		}
	}
	parts := []string{strings.TrimPrefix(strings.Join(where, " › "), "share/")}
	for _, k := range []string{"query", "url", "name", "entity_id", "domain", "vm", "container"} {
		parts = append(parts, str(k))
	}
	parts = slices.DeleteFunc(parts, func(p string) bool { return p == "" })
	return strings.Join(parts, " · ")
}

// chatMarkdown writes a chat as Markdown, with the steps as a short list
// in words, as the chat shows them (1.09: "• ? ha_entities" before).
func chatMarkdown(chat *agent.Chat, lang string) string {
	words, ok := exportTexts[lang]
	if !ok {
		words = exportTexts["de"]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", chat.ShownTitle())
	fmt.Fprintf(&b, "_%s_\n\n", chat.Created.Local().Format("02.01.2006 15:04"))
	for _, e := range chat.Entries {
		switch e.Role {
		case "user":
			fmt.Fprintf(&b, "**%s:** %s\n\n", words["you"], e.Content)
		case "assistant":
			if strings.TrimSpace(e.Content) != "" {
				fmt.Fprintf(&b, "**%s:**\n\n%s\n\n", words["agent"], e.Content)
			}
		case "tool":
			mark := "✓"
			switch {
			case strings.HasPrefix(e.Result, "Error:"):
				mark = "✗"
			case !e.OK:
				mark = "–" // not run (waiting, stopped, a repeat)
			}
			step := "`" + e.Tool + "`"
			if word := words[exportStepWords[e.Tool]]; word != "" {
				step = word
			}
			if detail := exportStepDetail(e.Args); detail != "" {
				step += " · " + detail
			}
			fmt.Fprintf(&b, "- %s %s\n", mark, step)
		case "note":
			if e.Content != "" {
				fmt.Fprintf(&b, "> %s: %s\n\n", e.Note, e.Content)
			}
		}
	}
	return b.String()
}

func (s *Server) handleAgentExport(w http.ResponseWriter, r *http.Request) {
	chat, err := s.agentStore().Chat(currentUser(r).Name, r.PathValue("id"))
	if err != nil {
		agentStoreError(w, err)
		return
	}
	lang := "de"
	if r.URL.Query().Get("lang") == "en" {
		lang = "en"
	}
	md := chatMarkdown(chat, lang)
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 {
			return '_'
		}
		return r
	}, chat.ShownTitle())
	switch r.URL.Query().Get("format") {
	case "pdf":
		data, err := office.PDF(md, chat.ShownTitle())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "export_failed", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s.pdf", url.PathEscape(name)))
		w.Write(data)
	default:
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s.md", url.PathEscape(name)))
		io.WriteString(w, md)
	}
}

// ---- Undo ----------------------------------------------------------------------

// handleAgentUndo takes back one step where TitanOS can: a replaced file
// gets its previous version (.prev) back, deleted files come back from the
// recycle bin, a switch in Home Assistant goes back with its receipt. It
// runs as the account, through the normal routes.
func (s *Server) handleAgentUndo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Step string `json:"step"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	store := s.agentStore()
	chat, err := store.Chat(user.Name, r.PathValue("id"))
	if err != nil {
		agentStoreError(w, err)
		return
	}
	var step *agent.Entry
	for i := range chat.Entries {
		if chat.Entries[i].ID == req.Step && chat.Entries[i].Role == "tool" {
			step = &chat.Entries[i]
		}
	}
	if step == nil || !step.OK {
		writeError(w, http.StatusNotFound, "not_found", "no such step")
		return
	}
	actions, err := s.undoActions(r.Context(), user, step)
	if err != nil {
		writeError(w, http.StatusBadRequest, "undo_not_possible", err.Error())
		return
	}
	box := s.newAgentToolbox(user, s.agentSettings())
	ctx := context.WithValue(r.Context(), agentCallKey{}, chat.ID)
	text := ""
	for _, action := range actions {
		if text, _, err = s.callAction(ctx, box.session, action); err != nil {
			writeError(w, http.StatusBadGateway, "undo_failed", err.Error())
			return
		}
	}
	note := agent.Entry{ID: agent.NewID(), At: time.Now(), Role: "note", Note: "undone", Tool: step.Tool, Content: step.Summary}
	chat.Entries = append(chat.Entries, note)
	if err := store.SaveChat(user.Name, chat); err != nil {
		slog.Warn("undo not noted in the chat", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": json.RawMessage(nonEmptyJSON(text))})
}

func nonEmptyJSON(s string) string {
	if json.Valid([]byte(s)) && strings.TrimSpace(s) != "" {
		return s
	}
	return `{}`
}

// undoActions are the web API calls that take a step back.
func (s *Server) undoActions(ctx context.Context, user auth.User, step *agent.Entry) ([]*toolcatalog.Action, error) {
	env := s.agentEnv()
	var args map[string]any
	_ = json.Unmarshal(step.Args, &args)
	str := func(k string) string { v, _ := args[k].(string); return v }
	switch step.Tool {
	case "file_write":
		p := strings.TrimPrefix(path.Clean("/"+str("path")), "/")
		del := card("files.delete", nil, "POST", "/files/delete", map[string]any{"root": str("root"), "paths": []string{p}})
		if replace, _ := args["replace"].(bool); replace {
			// The new file goes to the recycle bin, the previous version
			// (kept as <name>.prev) takes its place.
			return []*toolcatalog.Action{del, card("file.rename", nil, "POST", "/files/rename", map[string]any{"root": str("root"), "from": p + ".prev", "to": p})}, nil
		}
		return []*toolcatalog.Action{del}, nil
	case "files_delete":
		paths, _ := args["paths"].([]any)
		var list []string
		for _, p := range paths {
			if s, ok := p.(string); ok {
				list = append(list, s)
			}
		}
		if p := str("path"); p != "" {
			list = append(list, p)
		}
		// The recycle bin names files by their place in the bin; find the
		// newest entry of every deleted path.
		root, err := s.catalogRoot(user, str("root"))
		if err != nil || root.Kind != "share" {
			return nil, fmt.Errorf("only files deleted in a share come back from its recycle bin")
		}
		entries, err := env.files.TrashFlat(ctx, root, user.Name, user.Role.CanAdminister())
		if err != nil {
			return nil, err
		}
		var restore []string
		for _, want := range list {
			want = strings.TrimPrefix(path.Clean("/"+want), "/")
			best := -1
			for i, e := range entries {
				if strings.TrimPrefix(path.Clean("/"+e.Original), "/") == want && (best < 0 || e.Modified.After(entries[best].Modified)) {
					best = i
				}
			}
			if best >= 0 {
				restore = append(restore, entries[best].Path)
			}
		}
		if len(restore) == 0 {
			return nil, fmt.Errorf("the files are no longer in the recycle bin")
		}
		return []*toolcatalog.Action{card("files.restore", nil, "POST", "/files/trash/restore", map[string]any{"root": str("root"), "paths": restore})}, nil
	case "ha_call":
		var res struct {
			Receipt string `json:"undoReceipt"`
		}
		if json.Unmarshal([]byte(step.Result), &res) != nil || res.Receipt == "" {
			return nil, fmt.Errorf("this switch cannot be taken back (only on/off within 30 minutes)")
		}
		return []*toolcatalog.Action{card("ha.undo", nil, "POST", "/homeassistant/undo", map[string]string{"receipt": res.Receipt})}, nil
	}
	return nil, fmt.Errorf("this step cannot be taken back automatically")
}

// ---- Preload and download --------------------------------------------------------

// handleAgentPreload loads the model while the page opens, so the first
// answer does not wait for it.
func (s *Server) handleAgentPreload(w http.ResponseWriter, r *http.Request) {
	settings := s.agentSettings()
	client, err := s.agentOllama()
	if err != nil || !settings.Enabled {
		writeJSON(w, http.StatusOK, map[string]any{"loaded": false})
		return
	}
	var req struct {
		Model string `json:"model"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req)
	model := req.Model
	if model == "" {
		model = settings.Model
	}
	if model == "" {
		writeJSON(w, http.StatusOK, map[string]any{"loaded": false})
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(s.agentBase(), 3*time.Minute)
		defer cancel()
		if err := client.Preload(ctx, model, settings.KeepAlive, settings.Context); err != nil {
			slog.Info("model not preloaded", "model", model, "err", err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"loading": model})
}

type agentPull struct {
	mu        sync.Mutex
	Model     string    `json:"model,omitempty"`
	Status    string    `json:"status,omitempty"`
	Completed int64     `json:"completed,omitempty"`
	Total     int64     `json:"total,omitempty"`
	Error     string    `json:"error,omitempty"`
	Running   bool      `json:"running"`
	Done      time.Time `json:"done,omitzero"`
}

func (s *Server) handleAgentPullState(w http.ResponseWriter, r *http.Request) {
	p := &s.agentPull
	p.mu.Lock()
	defer p.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"model": p.Model, "status": p.Status, "completed": p.Completed, "total": p.Total, "error": p.Error, "running": p.Running})
}

// handleAgentPull downloads a model into Ollama in the background.
func (s *Server) handleAgentPull(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if s.agentSettings().Source == agentSourceCluster {
		writeError(w, http.StatusConflict, "agent_cluster", "models are downloaded on the admin node")
		return
	}
	req.Model = strings.TrimSpace(req.Model)
	if req.Model == "" || len(req.Model) > 200 || strings.ContainsAny(req.Model, " \t\n") {
		writeError(w, http.StatusBadRequest, "invalid_model", "a model name like gpt-oss:20b")
		return
	}
	client, err := s.agentOllama()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "agent_unavailable", err.Error())
		return
	}
	p := &s.agentPull
	p.mu.Lock()
	if p.Running {
		p.mu.Unlock()
		writeError(w, http.StatusConflict, "pull_running", "a model is already being downloaded")
		return
	}
	p.Model, p.Status, p.Completed, p.Total, p.Error, p.Running = req.Model, "starting", 0, 0, "", true
	p.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(s.agentBase(), 6*time.Hour)
		defer cancel()
		err := client.Pull(ctx, req.Model, func(pp agent.PullProgress) {
			p.mu.Lock()
			p.Status, p.Completed, p.Total = pp.Status, pp.Completed, pp.Total
			p.mu.Unlock()
		})
		p.mu.Lock()
		p.Running, p.Done = false, time.Now()
		if err != nil {
			p.Error = err.Error()
		}
		p.mu.Unlock()
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"model": req.Model})
}

// ---- Weekly report -----------------------------------------------------------------

// agentWeeklyReport sends on Monday morning what the agent did in the past
// week, when the owner switched it on.
func (s *Server) agentWeeklyReport(now time.Time) {
	env := s.agentEnv()
	settings := s.agentSettings()
	if !settings.WeeklyReport || now.Weekday() != time.Monday || now.Hour() != 8 || env.notify == nil || env.activity == nil {
		return
	}
	r := &s.agentReact
	r.mu.Lock()
	if now.Sub(r.reportSent) < 24*time.Hour {
		r.mu.Unlock()
		return
	}
	r.reportSent = now
	r.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(s.agentBase(), time.Minute)
		defer cancel()
		entries, _, _, err := env.activity.Query(ctx, activity.Filter{Days: 7, Via: activity.ViaAgent, Limit: 500})
		if err != nil {
			return
		}
		var ok, failed int
		count := map[string]int{}
		for _, e := range entries {
			if e.OK {
				ok++
			} else {
				failed++
			}
			count[e.Action]++
		}
		var lines []string
		for action, n := range count {
			lines = append(lines, fmt.Sprintf("%dx %s", n, action))
		}
		text := fmt.Sprintf("Der KI-Agent hat in den letzten 7 Tagen %d Änderungen gemacht (%d fehlgeschlagen).", ok+failed, failed)
		if len(lines) > 0 {
			text += "\n" + strings.Join(lines, "\n")
		}
		if err := env.notify.SendReport("Wochenbericht KI-Agent", text); err != nil {
			slog.Info("agent weekly report not sent", "err", err)
		}
	}()
}
