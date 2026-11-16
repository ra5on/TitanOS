package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// Projects, memory, skills and recurring tasks of an account, and the
// system prompt with the short state report of the NAS. Every change of a
// list is one step under the store's lock (Update…, 1.01): read, change and
// write, so two requests do not write over each other's change.

// errAgentListFull: a list of the account is at its limit.
var errAgentListFull = errors.New("the list is full")

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
	p.ID, p.Created = agent.NewID(), time.Now()
	err := s.agentStore().UpdateProjects(user, func(list []agent.Project) ([]agent.Project, error) {
		if len(list) >= maxProjects {
			return nil, errAgentListFull
		}
		return append(list, p), nil
	})
	if errors.Is(err, errAgentListFull) {
		writeError(w, http.StatusBadRequest, "too_many_projects", "at most 100 projects")
		return
	}
	if err != nil {
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
	err := s.agentStore().UpdateProjects(user, func(list []agent.Project) ([]agent.Project, error) {
		for i := range list {
			if list[i].ID == r.PathValue("id") {
				p.ID, p.Created = list[i].ID, list[i].Created
				list[i] = p
				return list, nil
			}
		}
		return nil, agent.ErrNotFound
	})
	if err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handleAgentProjectDelete removes a project; its chats stay, without one.
func (s *Server) handleAgentProjectDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r).Name
	id := r.PathValue("id")
	store := s.agentStore()
	err := store.UpdateProjects(user, func(list []agent.Project) ([]agent.Project, error) {
		n := len(list)
		list = slices.DeleteFunc(list, func(p agent.Project) bool { return p.ID == id })
		if len(list) == n {
			return nil, agent.ErrNotFound
		}
		return list, nil
	})
	if err != nil {
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
	// PLAN 90-4: pinned and topic can change on their own; the text is then
	// left out.
	var req struct {
		Text   *string `json:"text"`
		Pinned *bool   `json:"pinned"`
		Topic  *string `json:"topic"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	text, topic := "", ""
	if req.Text != nil {
		text = strings.Join(strings.Fields(*req.Text), " ")
	}
	if req.Topic != nil {
		topic = strings.Join(strings.Fields(*req.Topic), " ")
	}
	isNew := r.PathValue("id") == "new"
	if (req.Text != nil || isNew) && (text == "" || len(text) > maxMemory) {
		writeError(w, http.StatusBadRequest, "invalid_memory", "1 to 500 characters")
		return
	}
	if len(topic) > maxMemoryTopic || !utf8.ValidString(topic) {
		writeError(w, http.StatusBadRequest, "invalid_memory", "the topic is too long")
		return
	}
	if req.Text == nil && req.Pinned == nil && req.Topic == nil {
		writeError(w, http.StatusBadRequest, "invalid_memory", "nothing to change")
		return
	}
	user := currentUser(r).Name
	id := r.PathValue("id")
	var list []agent.Memory
	err := updateMemories(s.agentStore(), user, func(now []agent.Memory) ([]agent.Memory, error) {
		found := false
		for i := range now {
			if now[i].ID == id {
				found = true
				if req.Text != nil && now[i].Text != text {
					// Written by the person now: no longer from outside content.
					now[i].Text, now[i].Untrusted, now[i].At = text, false, time.Now()
				}
				if req.Pinned != nil {
					now[i].Pinned = *req.Pinned
				}
				if req.Topic != nil {
					now[i].Topic = topic
				}
			}
		}
		if isNew {
			if len(now) >= maxMemories {
				return nil, errAgentListFull
			}
			now, found = append(now, agent.Memory{ID: agent.NewID(), Text: text, At: time.Now(), Topic: topic, Pinned: req.Pinned != nil && *req.Pinned}), true
		}
		if !found {
			return nil, agent.ErrNotFound
		}
		list = now
		return now, nil
	})
	if errors.Is(err, errAgentListFull) {
		writeError(w, http.StatusBadRequest, "too_many", "memory is full")
		return
	}
	if err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"memory": list})
}

// updateMemories is Store.UpdateMemories with a change that may fail: an
// error leaves the file as it is and comes back.
func updateMemories(store *agent.Store, user string, change func([]agent.Memory) ([]agent.Memory, error)) error {
	var failed error
	err := store.UpdateMemories(user, func(list []agent.Memory) ([]agent.Memory, bool) {
		next, err := change(list)
		if err != nil {
			failed = err
			return nil, false
		}
		return next, true
	})
	if err != nil {
		return err
	}
	if errors.Is(failed, agent.ErrUnchanged) {
		return nil
	}
	return failed
}

func (s *Server) handleAgentMemoryDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r).Name
	id := r.PathValue("id")
	err := updateMemories(s.agentStore(), user, func(list []agent.Memory) ([]agent.Memory, error) {
		if id == "all" {
			return []agent.Memory{}, nil
		}
		n := len(list)
		list = slices.DeleteFunc(list, func(m agent.Memory) bool { return m.ID == id })
		if len(list) == n {
			return nil, agent.ErrNotFound
		}
		return list, nil
	})
	if err != nil {
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
	id := r.PathValue("id")
	builtIn := strings.HasPrefix(id, "builtin-")
	if builtIn && !slices.ContainsFunc(agent.BuiltInSkills, func(b agent.Skill) bool { return b.ID == id }) {
		agentStoreError(w, agent.ErrNotFound)
		return
	}
	if !builtIn {
		req.Name, req.When, req.Steps = strings.TrimSpace(req.Name), strings.TrimSpace(req.When), strings.TrimSpace(req.Steps)
		if req.Name == "" || len(req.Name) > 80 || req.Steps == "" || len(req.Steps)+len(req.When) > maxSkillText {
			writeError(w, http.StatusBadRequest, "invalid_skill", "name, when and steps")
			return
		}
	}
	err := s.agentStore().UpdateSkills(user, func(own []agent.Skill) ([]agent.Skill, error) {
		if builtIn {
			own = slices.DeleteFunc(own, func(sk agent.Skill) bool { return sk.ID == id })
			if req.Disabled {
				own = append(own, agent.Skill{ID: id, Disabled: true, BuiltIn: true})
			}
			return own, nil
		}
		found := false
		for i := range own {
			if own[i].ID == id {
				own[i].Name, own[i].When, own[i].Steps, own[i].Disabled, own[i].Untrusted, own[i].Updated, found = req.Name, req.When, req.Steps, req.Disabled, false, time.Now(), true
			}
		}
		if id == "new" {
			if len(own) >= maxSkills {
				return nil, errAgentListFull
			}
			own, found = append(own, agent.Skill{ID: agent.NewID(), Name: req.Name, When: req.When, Steps: req.Steps, Updated: time.Now()}), true
		}
		if !found {
			return nil, agent.ErrNotFound
		}
		return own, nil
	})
	if errors.Is(err, errAgentListFull) {
		writeError(w, http.StatusBadRequest, "too_many", "too many skills")
		return
	}
	if err != nil {
		agentStoreError(w, err)
		return
	}
	list, _ := s.agentSkills(user)
	writeJSON(w, http.StatusOK, map[string]any{"skills": list})
}

func (s *Server) handleAgentSkillDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r).Name
	id := r.PathValue("id")
	err := s.agentStore().UpdateSkills(user, func(own []agent.Skill) ([]agent.Skill, error) {
		n := len(own)
		own = slices.DeleteFunc(own, func(sk agent.Skill) bool { return sk.ID == id && !strings.HasPrefix(id, "builtin-") })
		if len(own) == n {
			return nil, agent.ErrNotFound
		}
		return own, nil
	})
	if err != nil {
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
	id := r.PathValue("id")
	var list []agent.Schedule
	err := s.agentStore().UpdateSchedules(user, func(now []agent.Schedule) ([]agent.Schedule, error) {
		sc := req
		found := false
		for i := range now {
			if now[i].ID == id {
				// What the NAS keeps comes from the list as it is now, not from
				// what the page read (a run stamped meanwhile stays stamped).
				sc.ID, sc.Created, sc.LastRun, sc.LastOK, sc.Chat = now[i].ID, now[i].Created, now[i].LastRun, now[i].LastOK, now[i].Chat
				// A one-off task with a new time runs again.
				if sc.Every == "once" && (sc.Date != now[i].Date || sc.At != now[i].At) {
					sc.LastRun = time.Time{}
				}
				// Switched on again or given another time: it counts from now.
				old := now[i]
				sc.Since = old.Since
				if (sc.Enabled && !old.Enabled) || sc.Every != old.Every || sc.At != old.At || sc.Date != old.Date ||
					sc.Interval != old.Interval || sc.Cron != old.Cron || sc.Weekday != old.Weekday || sc.Day != old.Day || !slices.Equal(sc.Weekdays, old.Weekdays) {
					sc.Since = time.Now()
				}
				now[i], found = sc, true
			}
		}
		if id == "new" {
			if len(now) >= maxSchedules {
				return nil, errAgentListFull
			}
			if sc.Kind == "morning" && slices.ContainsFunc(now, func(x agent.Schedule) bool { return x.Kind == "morning" }) {
				return nil, errMorningExists
			}
			sc.ID, sc.Created = agent.NewID(), time.Now()
			now, found = append(now, sc), true
		}
		if !found {
			return nil, agent.ErrNotFound
		}
		list = now
		return now, nil
	})
	switch {
	case errors.Is(err, errAgentListFull):
		writeError(w, http.StatusBadRequest, "too_many", "too many recurring tasks")
		return
	case errors.Is(err, errMorningExists):
		writeError(w, http.StatusConflict, "morning_exists", "there is a morning report already")
		return
	case err != nil:
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": s.agentScheduleViews(list)})
}

// errMorningExists: an account has one morning report.
var errMorningExists = errors.New("there is a morning report already")

func (s *Server) handleAgentScheduleDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r).Name
	err := s.agentStore().UpdateSchedules(user, func(list []agent.Schedule) ([]agent.Schedule, error) {
		n := len(list)
		list = slices.DeleteFunc(list, func(sc agent.Schedule) bool { return sc.ID == r.PathValue("id") })
		if len(list) == n {
			return nil, agent.ErrNotFound
		}
		return list, nil
	})
	if err != nil {
		agentStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAgentScheduleRun runs a recurring task now.
func (s *Server) handleAgentScheduleRun(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	chat, err := s.agentRunSchedule(user, r.PathValue("id"), time.Now(), false, true)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "agent_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"chat": chat})
}
