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
func (hass haArea) haConnection(id string) *homeassistant.Manager {
	if hub := hass.hub(); hub != nil {
		return hub.Get(id)
	}
	return nil
}

// haDefault is the connection used when none is named: the first one, or
// the only one in use while the first is empty.
func (hass haArea) haDefault() *homeassistant.Manager {
	hub := hass.hub()
	if hub == nil {
		return nil
	}
	if primary := hub.Primary(); primary.Active() {
		return primary
	}
	if active := hub.Active(); len(active) == 1 {
		return active[0]
	}
	return hub.Primary()
}

// haOf is the connection a request is about.
func (hass haArea) haOf(r *http.Request) *homeassistant.Manager {
	return hass.haConnection(r.URL.Query().Get("connection"))
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

func (hass haArea) haConnectionList() []haConnectionInfo {
	list := []haConnectionInfo{}
	hub := hass.hub()
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

func (hass haArea) handleHAConnections(w http.ResponseWriter, r *http.Request) {
	if !hass.haReady(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": hass.haConnectionList(), "max": homeassistant.MaxConnections})
}

func (hass haArea) handleHAConnectionAdd(w http.ResponseWriter, r *http.Request) {
	if !hass.haReady(w) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	m, err := hass.hub().Add(req.Name)
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
	writeJSON(w, http.StatusCreated, map[string]any{"id": m.ID(), "connections": hass.haConnectionList()})
}

// handleHAConnectionRemove takes a further connection out, with its
// storage entries on this NAS. What it set up in Home Assistant stays
// there; the first connection cannot be removed, only emptied.
func (hass haArea) handleHAConnectionRemove(w http.ResponseWriter, r *http.Request) {
	if !hass.haReady(w) {
		return
	}
	id := r.PathValue("id")
	noteTarget(r, id)
	if err := hass.hub().Remove(id); err != nil {
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
	hass.forgetHAStorage(id)
	slog.Info("Home Assistant connection removed", "connection", id, "by", currentUser(r).Name)
	writeJSON(w, http.StatusOK, map[string]any{"connections": hass.haConnectionList()})
}

// haConnectionArg splits the optional argument "connection" off the
// arguments of a Home Assistant tool; the rest is decoded as before.
func (hass haArea) haConnectionArg(args json.RawMessage) (*homeassistant.Manager, json.RawMessage, error) {
	if len(args) == 0 {
		return hass.haDefault(), args, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil || fields == nil {
		return hass.haDefault(), args, nil // the tool's own decoding says what is wrong
	}
	raw, ok := fields["connection"]
	if !ok {
		return hass.haDefault(), args, nil
	}
	var id string
	if err := json.Unmarshal(raw, &id); err != nil {
		return nil, nil, homeassistant.ErrNoConnection
	}
	delete(fields, "connection")
	m := hass.haConnection(id)
	if m == nil || !m.Active() {
		return nil, nil, homeassistant.ErrNoConnection
	}
	rest, err := json.Marshal(fields)
	return m, rest, err
}

// haConnectionProperty is the schema of the argument "connection", with
// the connections there are.
func (hass haArea) haConnectionProperty() map[string]any {
	ids := []string{}
	desc := "Home Assistant connection by ID; leave out for the first one in use."
	for _, m := range hass.hub().Active() {
		ids = append(ids, m.ID())
		if name := haLabel(m); name != "" {
			desc += " " + m.ID() + " = " + name + "."
		}
	}
	return map[string]any{"type": "string", "enum": ids, "description": desc}
}

// haFromContext is the connection a button or a publish is for, the first
// one if the context does not say.
func (hass haArea) haFromContext(ctx context.Context) *homeassistant.Manager {
	if m := homeassistant.ConnectionFrom(ctx); m != nil {
		return m
	}
	return hass.home
}

// handleHAConnectionNames lists the connections in use by name, for every
// account: choosing one for a phone or a trigger. Nothing else of them.
func (hass haArea) handleHAConnectionNames(w http.ResponseWriter, r *http.Request) {
	type choice struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	choices := []choice{}
	if hub := hass.hub(); hub != nil {
		for _, m := range hub.Active() {
			choices = append(choices, choice{m.ID(), m.Name()})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": choices})
}
