package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// Projects, memory, skills and recurring tasks of an account, and the
// system prompt with the short state report of the NAS.

func (s *Server) agentProject(user, id string) (*agent.Project, error) {
	if strings.Contains(id, ":") {
		return s.agentSharedProject(user, id)
	}
	list, err := s.agentStore().Projects(user)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == id {
			return &list[i], nil
		}
	}
	return nil, agent.ErrNotFound
}

func (s *Server) handleAgentProjects(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r).Name
	list, err := s.agentStore().Projects(user)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	list = append(list, s.agentSharedProjects(user)...)
	writeJSON(w, http.StatusOK, map[string]any{"projects": list})
}

func checkProject(p *agent.Project) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || len(p.Name) > 80 || len(p.Instructions) > 8000 || len(p.Icon) > 40 || len(p.Color) > 20 || len(p.Location) > 80 {
		return fmt.Errorf("name 1–80 characters, instructions at most 8000")
	}
	return nil
}

func (s *Server) handleAgentProjectNew(w http.ResponseWriter, r *http.Request) {
	var p agent.Project
	if !readJSON(w, r, &p) {
		return
	}
	if err := checkProject(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_project", err.Error())
		return
	}
	user := currentUser(r).Name
	if err := s.checkMembers(user, &p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_project", err.Error())
		return
	}
	store := s.agentStore()
	list, err := store.Projects(user)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	if len(list) >= maxProjects {
		writeError(w, http.StatusBadRequest, "too_many_projects", "at most 100 projects")
		return
	}
	p.ID, p.Created = agent.NewID(), time.Now()
	list = append(list, p)
	if err := store.SaveProjects(user, list); err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleAgentProjectSet(w http.ResponseWriter, r *http.Request) {
	var p agent.Project
	if !readJSON(w, r, &p) {
		return
	}
	if err := checkProject(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_project", err.Error())
		return
	}
	user := currentUser(r).Name
	if err := s.checkMembers(user, &p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_project", err.Error())
		return
	}
	store := s.agentStore()
	list, err := store.Projects(user)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	for i := range list {
		if list[i].ID == r.PathValue("id") {
			p.ID, p.Created = list[i].ID, list[i].Created
			list[i] = p
			if err := store.SaveProjects(user, list); err != nil {
				agentStoreError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, p)
			return
		}
	}
	agentStoreError(w, agent.ErrNotFound)
}

// handleAgentProjectDelete removes a project; its chats stay, without one.
func (s *Server) handleAgentProjectDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r).Name
	id := r.PathValue("id")
	store := s.agentStore()
	list, err := store.Projects(user)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	n := len(list)
	list = slices.DeleteFunc(list, func(p agent.Project) bool { return p.ID == id })
	if len(list) == n {
		agentStoreError(w, agent.ErrNotFound)
		return
	}
	if err := store.SaveProjects(user, list); err != nil {
		agentStoreError(w, err)
		return
	}
	chats, _ := store.Chats(user)
	for _, info := range chats {
		if info.Project != id || s.agentRunning(user, info.ID) {
			continue
		}
		if chat, err := store.Chat(user, info.ID); err == nil {
			chat.Project = ""
			if err := store.SaveChat(user, chat); err != nil {
				slog.Warn("chat keeps a removed project", "chat", chat.ID, "err", err)
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAgentMemory(w http.ResponseWriter, r *http.Request) {
	list, err := s.agentStore().Memories(currentUser(r).Name)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"memory": list})
}

func (s *Server) handleAgentMemorySet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	text := strings.Join(strings.Fields(req.Text), " ")
	if text == "" || len(text) > maxMemory {
		writeError(w, http.StatusBadRequest, "invalid_memory", "1 to 500 characters")
		return
	}
	user := currentUser(r).Name
	store := s.agentStore()
	list, err := store.Memories(user)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	id := r.PathValue("id")
	found := false
	for i := range list {
		if list[i].ID == id {
			// Written by the person now: no longer from outside content.
			list[i].Text, list[i].Untrusted, list[i].At, found = text, false, time.Now(), true
		}
	}
	if id == "new" {
		if len(list) >= maxMemories {
			writeError(w, http.StatusBadRequest, "too_many", "memory is full")
			return
		}
		list, found = append(list, agent.Memory{ID: agent.NewID(), Text: text, At: time.Now()}), true
	}
	if !found {
		agentStoreError(w, agent.ErrNotFound)
		return
	}
	if err := store.SaveMemories(user, list); err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"memory": list})
}

func (s *Server) handleAgentMemoryDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r).Name
	store := s.agentStore()
	list, err := store.Memories(user)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	id := r.PathValue("id")
	n := len(list)
	if id == "all" {
		list = []agent.Memory{}
	} else {
		list = slices.DeleteFunc(list, func(m agent.Memory) bool { return m.ID == id })
	}
	if len(list) == n && id != "all" {
		agentStoreError(w, agent.ErrNotFound)
		return
	}
	if err := store.SaveMemories(user, list); err != nil {
		agentStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// agentSkills are the built-in skills (with the account's switch) and the
// account's own ones.
func (s *Server) agentSkills(user string) ([]agent.Skill, error) {
	own, err := s.agentStore().Skills(user)
	if err != nil {
		return nil, err
	}
	off := map[string]bool{}
	var list []agent.Skill
	for _, sk := range own {
		if strings.HasPrefix(sk.ID, "builtin-") {
			off[sk.ID] = sk.Disabled
			continue
		}
		list = append(list, sk)
	}
	out := []agent.Skill{}
	for _, sk := range agent.BuiltInSkills {
		sk.Disabled = off[sk.ID]
		out = append(out, sk)
	}
	return append(out, list...), nil
}

func (s *Server) handleAgentSkills(w http.ResponseWriter, r *http.Request) {
	list, err := s.agentSkills(currentUser(r).Name)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": list})
}

// handleAgentSkillSet changes a skill or switches a built-in one on/off.
func (s *Server) handleAgentSkillSet(w http.ResponseWriter, r *http.Request) {
	var req agent.Skill
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r).Name
	store := s.agentStore()
	own, err := store.Skills(user)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	id := r.PathValue("id")
	if strings.HasPrefix(id, "builtin-") {
		if !slices.ContainsFunc(agent.BuiltInSkills, func(b agent.Skill) bool { return b.ID == id }) {
			agentStoreError(w, agent.ErrNotFound)
			return
		}
		own = slices.DeleteFunc(own, func(sk agent.Skill) bool { return sk.ID == id })
		if req.Disabled {
			own = append(own, agent.Skill{ID: id, Disabled: true, BuiltIn: true})
		}
	} else {
		req.Name, req.When, req.Steps = strings.TrimSpace(req.Name), strings.TrimSpace(req.When), strings.TrimSpace(req.Steps)
		if req.Name == "" || len(req.Name) > 80 || req.Steps == "" || len(req.Steps)+len(req.When) > maxSkillText {
			writeError(w, http.StatusBadRequest, "invalid_skill", "name, when and steps")
			return
		}
		found := false
		for i := range own {
			if own[i].ID == id {
				own[i].Name, own[i].When, own[i].Steps, own[i].Disabled, own[i].Untrusted, own[i].Updated, found = req.Name, req.When, req.Steps, req.Disabled, false, time.Now(), true
			}
		}
		if id == "new" {
			if len(own) >= maxSkills {
				writeError(w, http.StatusBadRequest, "too_many", "too many skills")
				return
			}
			own, found = append(own, agent.Skill{ID: agent.NewID(), Name: req.Name, When: req.When, Steps: req.Steps, Updated: time.Now()}), true
		}
		if !found {
			agentStoreError(w, agent.ErrNotFound)
			return
		}
	}
	if err := store.SaveSkills(user, own); err != nil {
		agentStoreError(w, err)
		return
	}
	list, _ := s.agentSkills(user)
	writeJSON(w, http.StatusOK, map[string]any{"skills": list})
}

func (s *Server) handleAgentSkillDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r).Name
	store := s.agentStore()
	own, err := store.Skills(user)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	id := r.PathValue("id")
	n := len(own)
	own = slices.DeleteFunc(own, func(sk agent.Skill) bool { return sk.ID == id && !strings.HasPrefix(id, "builtin-") })
	if len(own) == n {
		agentStoreError(w, agent.ErrNotFound)
		return
	}
	if err := store.SaveSkills(user, own); err != nil {
		agentStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAgentSchedules(w http.ResponseWriter, r *http.Request) {
	list, err := s.agentStore().Schedules(currentUser(r).Name)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": s.agentScheduleViews(list)})
}

func (s *Server) handleAgentScheduleSet(w http.ResponseWriter, r *http.Request) {
	var req agent.Schedule
	if !readJSON(w, r, &req) {
		return
	}
	if err := checkSchedule(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_schedule", err.Error())
		return
	}
	user := currentUser(r).Name
	store := s.agentStore()
	list, err := store.Schedules(user)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	id := r.PathValue("id")
	found := false
	for i := range list {
		if list[i].ID == id {
			req.ID, req.Created, req.LastRun, req.LastOK, req.Chat = list[i].ID, list[i].Created, list[i].LastRun, list[i].LastOK, list[i].Chat
			// A one-off task with a new time runs again.
			if req.Every == "once" && (req.Date != list[i].Date || req.At != list[i].At) {
				req.LastRun = time.Time{}
			}
			list[i], found = req, true
		}
	}
	if id == "new" {
		if len(list) >= maxSchedules {
			writeError(w, http.StatusBadRequest, "too_many", "too many recurring tasks")
			return
		}
		if req.Kind == "morning" && slices.ContainsFunc(list, func(sc agent.Schedule) bool { return sc.Kind == "morning" }) {
			writeError(w, http.StatusConflict, "morning_exists", "there is a morning report already")
			return
		}
		req.ID, req.Created = agent.NewID(), time.Now()
		list, found = append(list, req), true
	}
	if !found {
		agentStoreError(w, agent.ErrNotFound)
		return
	}
	if err := store.SaveSchedules(user, list); err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": s.agentScheduleViews(list)})
}

func (s *Server) handleAgentScheduleDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r).Name
	store := s.agentStore()
	list, err := store.Schedules(user)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	n := len(list)
	list = slices.DeleteFunc(list, func(sc agent.Schedule) bool { return sc.ID == r.PathValue("id") })
	if len(list) == n {
		agentStoreError(w, agent.ErrNotFound)
		return
	}
	if err := store.SaveSchedules(user, list); err != nil {
		agentStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAgentScheduleRun runs a recurring task now.
func (s *Server) handleAgentScheduleRun(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	chat, err := s.agentRunSchedule(user, r.PathValue("id"), time.Now(), false)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "agent_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"chat": chat})
}

// agentPrompt builds the system prompt of a run.
func (s *Server) agentPrompt(ctx context.Context, user auth.User, settings agentSettings, chat *agent.Chat, box *agentToolbox) string {
	in := agent.PromptInput{Account: user.Name, Admin: user.Role.CanAdminister(), Version: s.opts.Version, Now: time.Now(), Locations: settings.Locations, Origin: chat.Origin}
	if s.opts.Monitor != nil {
		in.Host = s.opts.Monitor.Info().Hostname
	}
	in.Web = settings.Policy.Mode("web_search", agent.AreaWeb, agent.KindRead) != agent.ModeDeny
	if _, ok := s.agentShare(); ok {
		in.Folder, in.Notes = true, s.agentNotes(user)
	}
	store := s.agentStore()
	if chat.Project != "" {
		in.Project, _ = s.agentProject(user.Name, chat.Project)
	}
	if mems, err := store.Memories(user.Name); err == nil {
		for _, m := range mems {
			if m.Project == "" || m.Project == chat.Project {
				in.Memories = append(in.Memories, m)
			}
		}
		// The newest count most; keep the prompt small.
		if len(in.Memories) > 60 {
			in.Memories = in.Memories[len(in.Memories)-60:]
		}
		// A memory saved after reading outside content (web page, file of
		// a stranger) may carry what that content wanted remembered. It
		// steers this chat like an outside skill: web tools ask first, so
		// that nothing can be sent out on the word of an old web page.
		if chat != nil && slices.ContainsFunc(in.Memories, func(m agent.Memory) bool { return m.Untrusted }) {
			chat.OutsideSkill = true
		}
	}
	if skills, err := s.agentSkills(user.Name); err == nil {
		for _, sk := range skills {
			if !sk.Disabled && !slices.Contains(settings.SkillsOff, sk.ID) {
				in.Skills = append(in.Skills, sk)
				// A skill from outside (catalog, or saved after reading outside
				// content) steers the agent like outside content: web tools
				// ask first in this chat (PLAN 54-G, security audit).
				if sk.Untrusted && chat != nil {
					chat.OutsideSkill = true
				}
			}
		}
	}
	in.Snapshot = s.agentSnapshot(ctx, box)
	in.Instruction = settings.Instruction
	if p, err := store.Profile(user.Name); err == nil {
		in.Personal = p.Instruction
	}
	// PLAN 53: personality and memory settings of the account.
	if ini, err := s.agentInitiativeOf(user.Name); err == nil {
		in.Personal = strings.TrimSpace(in.Personal + "\n" + agentStyle(ini))
	}
	in.Map = s.mapPrompt(user)
	return agent.BuildPrompt(in)
}

// agentSnapshot is a short state report: what exists on this NAS, so the
// model knows names before it calls a tool. Only names and states.
func (s *Server) agentSnapshot(ctx context.Context, box *agentToolbox) string {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	readTool := func(name string) map[string]any {
		t, ok := box.tools[name]
		if !ok || t.read == nil || box.mode(t, t.kind) == agent.ModeDeny {
			return nil
		}
		res, err := t.read(ctx, json.RawMessage("{}"))
		if err != nil {
			return nil
		}
		var out map[string]any
		if json.Unmarshal([]byte(res.Text), &out) != nil {
			return nil
		}
		return out
	}
	names := func(v any, field, state string) []string {
		list, _ := v.([]any)
		var out []string
		for _, e := range list {
			m, _ := e.(map[string]any)
			n, _ := m[field].(string)
			if n == "" {
				continue
			}
			if st, _ := m[state].(string); st != "" {
				n += " (" + st + ")"
			}
			out = append(out, n)
			if len(out) >= 40 {
				out = append(out, "…")
				break
			}
		}
		return out
	}
	var b strings.Builder
	line := func(label string, items []string) {
		if len(items) > 0 {
			fmt.Fprintf(&b, "%s: %s\n", label, strings.Join(items, ", "))
		}
	}
	if st := readTool("storage_overview"); st != nil {
		line("Volumes", names(st["volumes"], "name", "state"))
	}
	if sh := readTool("shares_list"); sh != nil {
		line("Shares (root share/<name>)", names(sh["shares"], "name", ""))
	} else if sh := readTool("my_shares"); sh != nil {
		line("Your shares (root share/<name>)", names(sh["shares"], "name", ""))
	}
	if d := readTool("docker_list"); d != nil {
		line("Docker containers", names(d["containers"], "name", "state"))
	}
	if v := readTool("vm_list"); v != nil {
		line("VMs", names(v["vms"], "name", "state"))
	}
	if l := readTool("lxc_list"); l != nil {
		line("System containers (LXC)", names(l["containers"], "name", "state"))
	}
	if b.Len() == 0 {
		return ""
	}
	return "Right now:\n" + b.String()
}
