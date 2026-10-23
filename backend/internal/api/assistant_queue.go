package api

import (
	"net/http"
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/assistant"
)

// Claim under one lock so only the oldest eligible highest-priority job enters
// the existing single execution slot. Queued jobs cannot conflict on resources.
func (s *Server) claimAgent(key string, j *agentJob) bool {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	j.mu.Lock()
	eligible, started, control, priority, queued := j.eligible, j.started, j.control, j.priority, j.queuedAt
	j.mu.Unlock()
	if !eligible || started || control != "" {
		return false
	}
	// Waiting jobs age by one priority tier each minute to avoid starvation.
	score := priority + int(time.Since(queued)/time.Minute)
	for otherKey, other := range s.agentJobs {
		if other == j {
			continue
		}
		other.mu.Lock()
		ready, running, command, p, q := other.eligible, other.started, other.control, other.priority, other.queuedAt
		other.mu.Unlock()
		otherScore := p + int(time.Since(q)/time.Minute)
		if ready && !running && command == "" && (otherScore > score || otherScore == score && (q.Before(queued) || q.Equal(queued) && otherKey < key)) {
			return false
		}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.control != "" {
		return false
	}
	select {
	case s.agentSlots <- struct{}{}:
		j.started = true
		return true
	default:
		return false
	}
}

func (s *Server) dependenciesReady(user string, c *assistant.Chat) (bool, string) {
	for _, id := range c.DependsOn {
		other, err := s.chats.Get(user, id)
		if err != nil {
			return false, "dependency_unavailable"
		}
		if other.Run == nil || other.Run.State != "done" {
			return false, "dependency_pending"
		}
		// A terminal model response is not sufficient if an action failed.
		for _, entry := range other.Messages {
			if entry.Action != nil && entry.Action.State != assistant.ActionDone && entry.Action.State != assistant.ActionRejected && entry.Action.State != assistant.ActionExpired {
				return false, "dependency_failed"
			}
		}
	}
	return true, ""
}

func (s *Server) handleAgentSchedule(w http.ResponseWriter, r *http.Request) {
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	var q struct {
		Priority  int      `json:"priority"`
		DependsOn []string `json:"dependsOn"`
	}
	if !readJSON(w, r, &q) {
		return
	}
	if q.Priority < -1 || q.Priority > 1 || len(q.DependsOn) > 10 {
		writeAssistantError(w, assistant.ErrInvalid)
		return
	}
	u := currentUser(r)
	id := r.PathValue("id")
	// Follow saved edges within this account, rejecting cycles and hidden IDs.
	visited := 0
	var visit func(string, map[string]bool) bool
	visit = func(next string, seen map[string]bool) bool {
		visited++
		if visited > 1000 || next == id || seen[next] {
			return false
		}
		seen[next] = true
		c, e := s.chats.Get(u.Name, next)
		if e != nil {
			return false
		}
		for _, d := range c.DependsOn {
			if !visit(d, seen) {
				return false
			}
		}
		delete(seen, next)
		return true
	}
	q.DependsOn = slices.Compact(slices.Sorted(slices.Values(q.DependsOn)))
	for _, dependency := range q.DependsOn {
		if !visit(dependency, map[string]bool{}) {
			writeAssistantError(w, assistant.ErrInvalid)
			return
		}
	}
	key := u.Name + "/" + id
	s.agentMu.Lock()
	j := s.agentJobs[key]
	s.agentMu.Unlock()
	if j != nil {
		j.mu.Lock()
		if j.started {
			j.mu.Unlock()
			writeAssistantError(w, assistant.ErrBusy)
			return
		}
		j.control = "requeue"
		j.mu.Unlock()
		j.cancel()
		select {
		case <-j.done:
		case <-r.Context().Done():
			return
		}
	}
	release, err := s.chats.Reserve(u.Name, id)
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	c, err := s.chats.Get(u.Name, id)
	if err != nil {
		release()
		writeAssistantError(w, err)
		return
	}
	c.Priority = q.Priority
	c.DependsOn = q.DependsOn
	if err = s.chats.Save(u.Name, c); err != nil {
		release()
		writeAssistantError(w, err)
		return
	}
	if c.Run != nil && c.Run.State == "queued" {
		s.startAgent(u, c, c.Model, lastPage(c), release)
	} else {
		release()
	}
	w.WriteHeader(http.StatusNoContent)
}
