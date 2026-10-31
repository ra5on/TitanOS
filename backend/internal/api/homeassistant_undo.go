package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
	"net/http"
	"strings"
	"sync"
	"time"
)

type haUndoReceipt struct {
	User, URL, Entity, Before, After string
	Changed                          time.Time
	Expires                          time.Time
}
type haUndoStore struct {
	sync.Mutex
	Entries map[string]haUndoReceipt
}

// Undo is deliberately limited to one switch/input_boolean, with an observed
// transition. No scenes, toggle, doors, locks or implicit physical rollback.
func (s *Server) beforeHAUndo(ctx context.Context, q haCall) *homeassistant.State {
	if len(q.Entities) != 1 || len(q.Data) > 0 || (q.Domain != "switch" && q.Domain != "input_boolean") || (q.Service != "turn_on" && q.Service != "turn_off") {
		return nil
	}
	st, err := s.opts.HomeAssistant.State(ctx, q.Entities[0])
	if err != nil || (st.State != "on" && st.State != "off") {
		return nil
	}
	return &st
}
func (s *Server) afterHAUndo(ctx context.Context, u auth.User, q haCall, before *homeassistant.State) string {
	if before == nil {
		return ""
	}
	expected := "on"
	if q.Service == "turn_off" {
		expected = "off"
	}
	if before.State == expected {
		return ""
	}
	after, err := s.opts.HomeAssistant.State(ctx, q.Entities[0])
	if err != nil || after.State != expected || after.LastChanged.IsZero() {
		return ""
	}
	settings, _ := s.opts.HomeAssistant.Settings()
	var key [24]byte
	if _, err = rand.Read(key[:]); err != nil {
		return ""
	}
	id := hex.EncodeToString(key[:])
	s.haUndo.Lock()
	defer s.haUndo.Unlock()
	if s.haUndo.Entries == nil {
		s.haUndo.Entries = map[string]haUndoReceipt{}
	}
	for k, v := range s.haUndo.Entries {
		if time.Now().After(v.Expires) {
			delete(s.haUndo.Entries, k)
		}
	}
	if len(s.haUndo.Entries) >= 200 {
		return ""
	}
	s.haUndo.Entries[id] = haUndoReceipt{u.Name, settings.URL, q.Entities[0], before.State, after.State, after.LastChanged, time.Now().Add(30 * time.Minute)}
	return id
}
func (s *Server) undoReceipt(user, id string) (haUndoReceipt, error) {
	s.haUndo.Lock()
	defer s.haUndo.Unlock()
	r, ok := s.haUndo.Entries[id]
	settings, _ := s.opts.HomeAssistant.Settings()
	if !ok || r.User != user || time.Now().After(r.Expires) || settings.URL != r.URL {
		return r, toolcatalog.ErrInvalid
	}
	service := "turn_" + r.Before
	if !s.opts.HomeAssistant.CanCall(r.Entity, service) {
		return r, homeassistant.ErrNotAllowed
	}
	return r, nil
}
func (s *Server) haUndoTool(u auth.User) toolcatalog.Tool {
	return actionTool("ha_undo", "Undo a previously verified single switch action ONLY using its undoReceipt returned by ha_call. Expires after 30 minutes or a service restart. Rejects intervening state changes and current access restrictions. Never undo scenes, doors or arbitrary actions.", map[string]any{"receipt": str("Exact undoReceipt from ha_call")}, []string{"receipt"}, func(ctx context.Context, args json.RawMessage) (*toolcatalog.Action, error) {
		var q struct {
			Receipt string `json:"receipt"`
		}
		if err := toolcatalog.DecodeArgs(args, &q); err != nil {
			return nil, err
		}
		receipt, err := s.undoReceipt(u.Name, q.Receipt)
		if err != nil {
			return nil, err
		}
		return card("api.change", map[string]string{"summary": receipt.Entity + " → " + receipt.Before}, "POST", "/homeassistant/undo", q), nil
	})
}
func (s *Server) handleHAUndo(w http.ResponseWriter, r *http.Request) {
	if !s.haReady(w) {
		return
	}
	var q struct {
		Receipt string `json:"receipt"`
	}
	if !readJSON(w, r, &q) {
		return
	}
	saved, err := s.undoReceipt(currentUser(r).Name, q.Receipt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_receipt", "the undo receipt is not valid or has expired")
		return
	}
	current, err := s.opts.HomeAssistant.State(r.Context(), saved.Entity)
	if err != nil {
		writeHAError(w, err)
		return
	}
	if current.State != saved.After || !current.LastChanged.Equal(saved.Changed) {
		writeError(w, 409, "ha_changed", "The switch changed since the original action; undo is no longer safe.")
		return
	}
	// Consume before mutation, including a timeout with an unknown outcome.
	s.haUndo.Lock()
	_, exists := s.haUndo.Entries[q.Receipt]
	delete(s.haUndo.Entries, q.Receipt)
	s.haUndo.Unlock()
	if !exists {
		writeError(w, http.StatusBadRequest, "invalid_receipt", "the undo receipt is not valid or has expired")
		return
	}
	service := "turn_" + saved.Before
	_, err = s.opts.HomeAssistant.Call(r.Context(), domainOfEntity(saved.Entity), service, []string{saved.Entity}, nil)
	if err != nil {
		writeHAError(w, err)
		return
	}
	observations := s.opts.HomeAssistant.Verify(r.Context(), domainOfEntity(saved.Entity), service, []string{saved.Entity}, nil, 5*time.Second)
	writeJSON(w, 200, map[string]any{"entities": observations, "notice": "HA cannot atomically compare and set; another controller may act between observation and call."})
}

func domainOfEntity(entity string) string { domain, _, _ := strings.Cut(entity, "."); return domain }
