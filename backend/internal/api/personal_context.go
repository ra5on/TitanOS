package api

import (
	"crypto/rand"
	"net/http"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
)

// Context grants nothing. An app hands a selection to the Agent window;
// only an opaque identifier is passed through the desktop and browser history.
type personalContext struct {
	Sources []pa.Source `json:"sources"`
	Tool    string      `json:"tool,omitempty"`
	Prompt  string      `json:"prompt,omitempty"`
}

type personalSelection struct {
	context personalContext
	name    string
	created time.Time
	expires time.Time
}
type personalContextStore struct {
	mu      sync.Mutex
	entries map[string]personalSelection
}

func (s *personalContextStore) put(user auth.User, data personalContext, now time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = make(map[string]personalSelection)
	}
	own := 0
	for id, row := range s.entries {
		if !now.Before(row.expires) {
			delete(s.entries, id)
			continue
		}
		if row.name == user.Name {
			own++
		}
	}
	if own >= 8 || len(s.entries) >= 256 {
		return ""
	}
	id := rand.Text()
	s.entries[id] = personalSelection{data, user.Name, user.Created, now.Add(5 * time.Minute)}
	time.AfterFunc(5*time.Minute, func() { s.mu.Lock(); delete(s.entries, id); s.mu.Unlock() })
	return id
}
func (s *personalContextStore) take(user auth.User, id string, now time.Time) (personalContext, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, found := s.entries[id]
	if !found || row.name != user.Name || !row.created.Equal(user.Created) || !now.Before(row.expires) {
		return personalContext{}, false
	}
	delete(s.entries, id)
	return row.context, true
}

func (s *Server) registerPersonalContextRoutes(mux *routeMux) {
	mux.HandleFunc("POST /api/v1/personal-agent/voice", s.personalBrowser(s.personalVoice))
	mux.HandleFunc("POST /api/v1/personal-agent/contexts", s.personalBrowser(func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.authArea().authenticate(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "sign in required")
			return
		}
		var data personalContext
		if !readJSON(w, r, &data) {
			return
		}
		if len(data.Sources) > 32 || len(data.Prompt) > 8192 || len(data.Tool) > 160 {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid selection")
			return
		}
		for _, source := range data.Sources {
			if source.Kind == "" || source.ID == "" || len(source.Kind) > 64 || len(source.ID) > 4096 || len(source.Label) > 512 || len(source.Ref) > 24 {
				writeError(w, http.StatusBadRequest, "invalid_request", "invalid source")
				return
			}
		}
		id := s.personalContexts.put(user, data, time.Now())
		if id == "" {
			writeError(w, http.StatusTooManyRequests, "too_many_requests", "too many pending selections")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"id": id})
	}))
	mux.HandleFunc("POST /api/v1/personal-agent/contexts/{id}/consume", s.personalBrowser(func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.authArea().authenticate(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "sign in required")
			return
		}
		data, found := s.personalContexts.take(user, r.PathValue("id"), time.Now())
		if !found {
			writeError(w, http.StatusNotFound, "not_found", "selection has expired")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, data)
	}))
}
