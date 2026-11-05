package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
)

// Several Home Assistant connections (PLAN §45): every route of Settings ›
// Home Assistant takes ?connection=<id>; without it, it is the first one,
// as before. Tools take an optional argument "connection".

// haHub is every connection; a hub of HomeAssistant alone when titand was
// started without one (tests).
func (s *Server) haHub() *homeassistant.Hub {
	s.haHubOnce.Do(func() {
		if s.opts.HomeAssistants == nil && s.opts.HomeAssistant != nil {
			s.opts.HomeAssistants = homeassistant.NewHub(s.opts.Store, s.opts.HomeAssistant)
		}
	})
	return s.opts.HomeAssistants
}

// haConnection is the connection by its ID ("" is the first one), nil if
// there is none.
func (s *Server) haConnection(id string) *homeassistant.Manager {
	if hub := s.haHub(); hub != nil {
		return hub.Get(id)
	}
	return nil
}

// haOf is the connection a request is about.
func (s *Server) haOf(r *http.Request) *homeassistant.Manager {
	return s.haConnection(r.URL.Query().Get("connection"))
}

// haLabel names a connection in messages: empty for the first one, so
// that texts stay as they were with one connection.
func haLabel(m *homeassistant.Manager) string {
	if m == nil || m.ID() == homeassistant.PrimaryID {
		return ""
	}
	if name := m.Name(); name != "" {
		return name
	}
	return m.ID()
}

type haConnectionInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
	Active  bool   `json:"active"`
	MQTT    bool   `json:"mqtt"`
}

func (s *Server) haConnectionList() []haConnectionInfo {
	list := []haConnectionInfo{}
	hub := s.haHub()
	if hub == nil {
		return list
	}
	for _, m := range hub.All() {
		settings, _ := m.Settings()
		publishing, _ := m.MQTTStatus()
		list = append(list, haConnectionInfo{ID: m.ID(), Name: settings.Name, URL: settings.URL, Enabled: settings.Enabled, Active: m.Active(), MQTT: publishing})
	}
	return list
}

func (s *Server) handleHAConnections(w http.ResponseWriter, r *http.Request) {
	if !s.haReady(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": s.haConnectionList(), "max": homeassistant.MaxConnections})
}

func (s *Server) handleHAConnectionAdd(w http.ResponseWriter, r *http.Request) {
	if !s.haReady(w) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	m, err := s.haHub().Add(req.Name)
	switch {
	case errors.Is(err, homeassistant.ErrTooMany):
		writeError(w, http.StatusConflict, "ha_too_many", err.Error())
		return
	case err != nil:
		writeHAError(w, err)
		return
	}
	noteTarget(r, m.ID())
	slog.Info("Home Assistant connection added", "connection", m.ID(), "by", currentUser(r).Name)
	writeJSON(w, http.StatusCreated, map[string]any{"id": m.ID(), "connections": s.haConnectionList()})
}

// handleHAConnectionRemove takes a further connection out, with its
// storage entries on this NAS. What it set up in Home Assistant stays
// there; the first connection cannot be removed, only emptied.
func (s *Server) handleHAConnectionRemove(w http.ResponseWriter, r *http.Request) {
	if !s.haReady(w) {
		return
	}
	id := r.PathValue("id")
	noteTarget(r, id)
	if err := s.haHub().Remove(id); err != nil {
		switch {
		case errors.Is(err, homeassistant.ErrPrimary):
			writeError(w, http.StatusConflict, "ha_primary", err.Error())
		case errors.Is(err, homeassistant.ErrNoConnection):
			writeError(w, http.StatusNotFound, "ha_connection_missing", err.Error())
		default:
			writeHAError(w, err)
		}
		return
	}
	s.forgetHAStorage(id)
	slog.Info("Home Assistant connection removed", "connection", id, "by", currentUser(r).Name)
	writeJSON(w, http.StatusOK, map[string]any{"connections": s.haConnectionList()})
}

// haConnectionArg splits the optional argument "connection" off the
// arguments of a Home Assistant tool; the rest is decoded as before.
func (s *Server) haConnectionArg(args json.RawMessage) (*homeassistant.Manager, json.RawMessage, error) {
	if len(args) == 0 {
		return s.haConnection(""), args, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil || fields == nil {
		return s.haConnection(""), args, nil // the tool's own decoding says what is wrong
	}
	raw, ok := fields["connection"]
	if !ok {
		return s.haConnection(""), args, nil
	}
	var id string
	if err := json.Unmarshal(raw, &id); err != nil {
		return nil, nil, homeassistant.ErrNoConnection
	}
	delete(fields, "connection")
	m := s.haConnection(id)
	if m == nil || !m.Active() {
		return nil, nil, homeassistant.ErrNoConnection
	}
	rest, err := json.Marshal(fields)
	return m, rest, err
}

// haConnectionProperty is the schema of the argument "connection", with
// the connections there are.
func (s *Server) haConnectionProperty() map[string]any {
	ids := []string{}
	desc := "Home Assistant connection by ID; leave out for the first one."
	for _, m := range s.haHub().Active() {
		ids = append(ids, m.ID())
		if name := haLabel(m); name != "" {
			desc += " " + m.ID() + " = " + name + "."
		}
	}
	return map[string]any{"type": "string", "enum": ids, "description": desc}
}

// haFromContext is the connection a button or a publish is for, the first
// one if the context does not say.
func (s *Server) haFromContext(ctx context.Context) *homeassistant.Manager {
	if m := homeassistant.ConnectionFrom(ctx); m != nil {
		return m
	}
	return s.opts.HomeAssistant
}
