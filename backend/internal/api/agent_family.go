package api

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// Shared projects (PLAN 51, "Familien-Projekte"): the owner of a project
// names other accounts of the agent as members. Members see the project,
// its instructions and the chats in it, and start their own chats there.
// A chat stays with the account that wrote it: others read it, only its
// author continues it, and each chat runs with the rights of its author.
// For a member the project's id is "<owner>:<id>".

const maxMembers = 20

// agentAccounts are the accounts that may use the agent.
func (s *Server) agentAccounts() []string {
	settings := s.agentSettings()
	var names []string
	for _, u := range s.opts.Users.List() {
		if !u.Disabled && (u.Role == auth.RoleOwner || slices.Contains(settings.Users, u.Name)) {
			names = append(names, u.Name)
		}
	}
	return names
}

func (s *Server) checkMembers(owner string, p *agent.Project) error {
	if len(p.Members) > maxMembers {
		return errors.New("at most 20 members")
	}
	accounts := s.agentAccounts()
	var out []string
	for _, m := range p.Members {
		if m == owner || slices.Contains(out, m) {
			continue
		}
		if !slices.Contains(accounts, m) {
			return errors.New("member " + m + " may not use the agent")
		}
		out = append(out, m)
	}
	p.Members, p.Owner = out, ""
	return nil
}

// agentSharedProject resolves "<owner>:<id>" for a member.
func (s *Server) agentSharedProject(user, id string) (*agent.Project, error) {
	owner, pid, ok := strings.Cut(id, ":")
	if !ok || owner == user || !slices.Contains(s.agentAccounts(), owner) {
		return nil, agent.ErrNotFound
	}
	list, err := s.agentStore().Projects(owner)
	if err != nil {
		return nil, agent.ErrNotFound
	}
	for _, p := range list {
		if p.ID == pid && slices.Contains(p.Members, user) {
			p.ID, p.Owner = id, owner
			return &p, nil
		}
	}
	return nil, agent.ErrNotFound
}

// agentSharedProjects are the projects other accounts share with user.
func (s *Server) agentSharedProjects(user string) []agent.Project {
	var out []agent.Project
	store := s.agentStore()
	for _, owner := range s.agentAccounts() {
		if owner == user {
			continue
		}
		list, err := store.Projects(owner)
		if err != nil {
			continue
		}
		for _, p := range list {
			if slices.Contains(p.Members, user) {
				p.Owner = owner
				p.ID = owner + ":" + p.ID
				out = append(out, p)
			}
		}
	}
	return out
}

// agentSharedChats are the chats of other accounts in projects shared
// with user, with the project id as user knows it.
func (s *Server) agentSharedChats(user string) []agent.ChatInfo {
	store := s.agentStore()
	type want struct {
		author, stored, shown string
	}
	var wants []want
	own, _ := store.Projects(user)
	for _, p := range own {
		for _, m := range p.Members {
			wants = append(wants, want{m, user + ":" + p.ID, p.ID})
		}
	}
	for _, p := range s.agentSharedProjects(user) {
		owner, pid, _ := strings.Cut(p.ID, ":")
		wants = append(wants, want{owner, pid, p.ID})
		for _, m := range p.Members {
			if m != user {
				wants = append(wants, want{m, p.ID, p.ID})
			}
		}
	}
	var out []agent.ChatInfo
	lists := map[string][]agent.ChatInfo{}
	for _, w := range wants {
		list, ok := lists[w.author]
		if !ok {
			list, _ = store.Chats(w.author)
			lists[w.author] = list
		}
		for _, c := range list {
			if c.Project == w.stored {
				c.Project, c.Author, c.Unread = w.shown, w.author, false
				out = append(out, c)
			}
		}
	}
	return out
}

// agentReadableChat returns a chat of another account if it lies in a
// project shared with user.
func (s *Server) agentReadableChat(user, author, id string) (*agent.Chat, error) {
	for _, c := range s.agentSharedChats(user) {
		if c.ID == id && c.Author == author {
			chat, err := s.agentStore().Chat(author, id)
			if err != nil {
				return nil, err
			}
			chat.Project = c.Project
			return foreignChat(chat), nil
		}
	}
	return nil, agent.ErrNotFound
}

// foreignChat is what another member reads of a chat: the questions and
// the answers, not the steps. Tool results, arguments, thoughts, files and
// waiting approvals came from the author's rights (files, logs, admin
// output) and stay with the author.
func foreignChat(c *agent.Chat) *agent.Chat {
	out := *c
	out.Pending = nil
	out.Entries = make([]agent.Entry, 0, len(c.Entries))
	for _, e := range c.Entries {
		switch e.Role {
		case "user", "assistant":
			e.Thinking, e.Calls, e.Files, e.Sources, e.Stats = "", nil, nil, nil, nil
			if e.Role == "assistant" && strings.TrimSpace(e.Content) == "" {
				continue
			}
		case "tool":
			// Only that there was a step, for the "n Schritte" box.
			e = agent.Entry{ID: e.ID, At: e.At, Role: "tool", Tool: e.Tool, OK: e.OK}
		case "note":
			// Summaries and memory notes are made from the steps' results
			// (PLAN 59): they stay the author's like the results.
			if e.Note == "edited" || e.Note == "snapshot" || e.Note == "error" || e.Note == "summary" || e.Note == "memory" {
				continue
			}
			e.Args, e.Result, e.Summary = nil, "", ""
		default:
			continue
		}
		out.Entries = append(out.Entries, e)
	}
	return &out
}

// handleAgentAccounts lists the other accounts of the agent, for members.
func (s *Server) handleAgentAccounts(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r).Name
	names := slices.DeleteFunc(s.agentAccounts(), func(n string) bool { return n == me })
	if names == nil {
		names = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": names})
}
