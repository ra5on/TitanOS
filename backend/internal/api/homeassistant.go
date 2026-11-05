package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// Home Assistant: the connection (owners and administrators), the notify
// services for one's own messages, and service calls for the assistant.
// The token never leaves the NAS; see package homeassistant.

func writeHAError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, homeassistant.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_ha_settings", err.Error())
	case errors.Is(err, homeassistant.ErrOff):
		writeError(w, http.StatusConflict, "ha_off", err.Error())
	case errors.Is(err, homeassistant.ErrNotAllowed):
		writeError(w, http.StatusForbidden, "ha_not_allowed", err.Error())
	case errors.Is(err, homeassistant.ErrUnreachable):
		writeError(w, http.StatusBadGateway, "ha_unreachable", err.Error())
	case errors.Is(err, homeassistant.ErrNotLoaded):
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}

// haRequest is the connection a request is about, or nil after an answer.
func (s *Server) haRequest(w http.ResponseWriter, r *http.Request) *homeassistant.Manager {
	if !s.haReady(w) {
		return nil
	}
	ha := s.haOf(r)
	if ha == nil {
		writeError(w, http.StatusNotFound, "ha_connection_missing", homeassistant.ErrNoConnection.Error())
	}
	return ha
}

func (s *Server) haReady(w http.ResponseWriter) bool {
	if s.opts.HomeAssistant == nil {
		writeError(w, http.StatusNotFound, "ha_off", "Home Assistant is not available")
		return false
	}
	return true
}

func (s *Server) handleHASettings(w http.ResponseWriter, r *http.Request) {
	ha := s.haRequest(w, r)
	if ha == nil {
		return
	}
	settings, set := ha.Settings()
	watching, watchError := ha.WatchStatus()
	publishing, mqttError := ha.MQTTStatus()
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings, "tokenSet": set["token"], "mqttPasswordSet": set["mqtt"], "domains": homeassistant.Domains(),
		"diagnostics": ha.Diagnostics(), "sensorGroups": homeassistant.SensorGroups, "commandGroups": homeassistant.CommandGroups, "mqttDevices": s.HASnapshot(r.Context()).Telemetry,
		"status": map[string]any{"watching": watching, "watchError": watchError, "mqtt": publishing, "mqttError": mqttError}})
}

func (s *Server) handleHASettingsChange(w http.ResponseWriter, r *http.Request) {
	ha := s.haRequest(w, r)
	if ha == nil {
		return
	}
	var req struct {
		homeassistant.Settings
		ClearSecrets []string `json:"clearSecrets"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	// Buttons and Assist act for an owner or administrator only.
	if req.Account != "" {
		if u, ok := s.opts.Users.Get(req.Account); !ok || !u.Role.CanAdminister() {
			writeError(w, http.StatusBadRequest, "invalid_ha_settings", "the account must be an owner or administrator")
			return
		}
	}
	if err := ha.SetSettings(req.Settings, req.ClearSecrets...); err != nil {
		writeHAError(w, err)
		return
	}
	slog.Info("Home Assistant settings changed", "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleHASettings(w, r)
}

func (s *Server) handleHACheck(w http.ResponseWriter, r *http.Request) {
	ha := s.haRequest(w, r)
	if ha == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	status, err := ha.Check(ctx)
	if err != nil {
		writeHAError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// handleHAEntities lists every entity for choosing what to release, or with
// ?released=1 only the released ones (for automations).
func (s *Server) handleHAEntities(w http.ResponseWriter, r *http.Request) {
	ha := s.haRequest(w, r)
	if ha == nil {
		return
	}
	states, err := ha.States(r.Context(), r.URL.Query().Get("released") == "1")
	if err != nil {
		writeHAError(w, err)
		return
	}
	type entity struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Area   string `json:"area,omitempty"`
		State  string `json:"state"`
		Domain string `json:"domain"`
	}
	list := make([]entity, 0, len(states))
	for _, st := range states {
		domain, _, _ := strings.Cut(st.EntityID, ".")
		list = append(list, entity{st.EntityID, st.Name(), st.Area, st.State, domain})
	}
	writeJSON(w, http.StatusOK, map[string]any{"entities": list})
}

// handleHANotifyServices lists notify services, for choosing one's own phone.
func (s *Server) handleHANotifyServices(w http.ResponseWriter, r *http.Request) {
	ha := s.haRequest(w, r)
	if ha == nil {
		return
	}
	// The connections to choose from, by name only: this route is for
	// every account (its own phone).
	type choice struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	choices := []choice{}
	for _, m := range s.haHub().Active() {
		choices = append(choices, choice{m.ID(), m.Name()})
	}
	services, err := ha.NotifyServices(r.Context())
	if err != nil && len(choices) > 0 && r.URL.Query().Get("connection") == "" {
		// The first one is empty or away; the others can still be chosen.
		writeJSON(w, http.StatusOK, map[string]any{"services": []string{}, "connections": choices})
		return
	}
	if err != nil {
		writeHAError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": services, "connections": choices})
}

type haCall struct {
	Domain   string         `json:"domain"`
	Service  string         `json:"service"`
	Entities []string       `json:"entities"`
	Data     map[string]any `json:"data,omitempty"`
	// Connection is the Home Assistant connection; empty is the first one.
	Connection string `json:"connection,omitempty"`
}

// handleHACall calls a service for released entities. The assistant's
// action ha_call ends here; the release is checked again.
func (s *Server) handleHACall(w http.ResponseWriter, r *http.Request) {
	if !s.haReady(w) {
		return
	}
	var req haCall
	if !readJSON(w, r, &req) {
		return
	}
	if q := r.URL.Query().Get("connection"); q != "" && req.Connection == "" {
		req.Connection = q
	}
	ha := s.haConnection(req.Connection)
	if ha == nil {
		writeError(w, http.StatusNotFound, "ha_connection_missing", homeassistant.ErrNoConnection.Error())
		return
	}
	before := s.beforeHAUndo(r.Context(), ha, req)
	changed, err := ha.Call(r.Context(), req.Domain, req.Service, req.Entities, req.Data)
	if err != nil {
		writeHAError(w, err)
		return
	}
	slog.Info("Home Assistant service called", "service", req.Domain+"."+req.Service, "entities", strings.Join(req.Entities, ","), "by", currentUser(r).Name, "ip", remoteIP(r))
	type entity struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	list := []entity{}
	for _, st := range changed {
		if !slices.Contains(req.Entities, st.EntityID) {
			continue
		}
		list = append(list, entity{st.EntityID, st.State})
	}
	writeJSON(w, http.StatusOK, map[string]any{"changed": list, "undoReceipt": s.afterHAUndo(r.Context(), ha, currentUser(r), req, before)})
}

// haTools are the assistant's tools for Home Assistant, only for owners and
// administrators and only while a server is set up.
func (s *Server) haTools(user auth.User) []toolcatalog.Tool {
	hub := s.haHub()
	if hub == nil || len(hub.Active()) == 0 || !user.Role.CanAdminister() {
		return nil
	}
	// With several connections every tool takes "connection"; without it,
	// it is the first one, as before.
	several := len(hub.All()) > 1
	props := func(p map[string]any) map[string]any {
		if several {
			if p == nil {
				p = map[string]any{}
			}
			p["connection"] = s.haConnectionProperty()
		}
		return p
	}
	on := func(f func(ctx context.Context, ha *homeassistant.Manager, args json.RawMessage) (any, error)) func(context.Context, json.RawMessage) (any, error) {
		return func(ctx context.Context, args json.RawMessage) (any, error) {
			ha, rest, err := s.haConnectionArg(args)
			if err != nil {
				return nil, err
			}
			if !ha.Active() {
				return nil, homeassistant.ErrOff
			}
			return f(ctx, ha, rest)
		}
	}
	return []toolcatalog.Tool{
		s.haUndoTool(user),
		reading("ha_diagnostics", "Read the HA observer connection timeline and lost-buffer counters. Gaps are not replayed; use ha_history for bounded recorder evidence and never infer a cause from correlation alone.", haDiagnosticsSchema(props(nil)), false, on(func(ctx context.Context, ha *homeassistant.Manager, args json.RawMessage) (any, error) {
			if err := toolcatalog.DecodeArgs(args, &struct{}{}); err != nil {
				return nil, err
			}
			return ha.Diagnostics(), nil
		})),
		reading("ha_capabilities", "Inspect actual service schemas, device features, units and limits of one released entity before changing it.", schema(props(map[string]any{"entity": str("Released entity ID")}), "entity"), true, on(func(ctx context.Context, ha *homeassistant.Manager, args json.RawMessage) (any, error) {
			var q struct{ Entity string }
			if err := toolcatalog.DecodeArgs(args, &q); err != nil {
				return nil, err
			}
			return ha.Capabilities(ctx, q.Entity)
		})),
		reading("ha_entities", "Entities of Home Assistant released to TitanOS (lights, switches, sensors …) with state and area. Only released ones are visible. Pages of 100.",
			schema(props(map[string]any{
				"domain": str("Only this domain, e.g. light."),
				"area":   str("Only entities in an area whose name contains this text."),
				"search": str("Text in the ID or name."),
				"offset": map[string]any{"type": "integer", "minimum": 0, "description": "Next page from nextOffset."},
			})), true,
			on(func(ctx context.Context, ha *homeassistant.Manager, args json.RawMessage) (any, error) {
				var q struct {
					Domain, Area, Search string
					Offset               int
				}
				if err := toolcatalog.DecodeArgs(args, &q); err != nil {
					return nil, err
				}
				states, err := ha.States(ctx, true)
				if err != nil {
					return nil, err
				}
				type entity struct {
					Policy      homeassistant.EntityPolicy `json:"policy"`
					Attributes  map[string]any             `json:"attributes"`
					LastChanged time.Time                  `json:"lastChanged"`
					ID          string                     `json:"id"`
					Name        string                     `json:"name"`
					State       string                     `json:"state"`
					Area        string                     `json:"area,omitempty"`
					Unit        string                     `json:"unit,omitempty"`
				}
				var list []entity
				for _, st := range states {
					domain, _, _ := strings.Cut(st.EntityID, ".")
					search := strings.ToLower(q.Search)
					if q.Domain != "" && domain != q.Domain || q.Area != "" && !strings.Contains(strings.ToLower(st.Area), strings.ToLower(q.Area)) ||
						search != "" && !strings.Contains(strings.ToLower(st.EntityID+" "+st.Name()+" "+strings.Join(ha.Policy(st.EntityID).Aliases, " ")), search) {
						continue
					}
					unit, _ := st.Attributes["unit_of_measurement"].(string)
					list = append(list, entity{ha.Policy(st.EntityID), st.Attributes, st.LastChanged, st.EntityID, st.Name(), st.State, st.Area, unit})
				}
				total := len(list)
				start := min(q.Offset, total)
				end := min(start+100, total)
				result := map[string]any{"entities": list[start:end], "total": total}
				if end < total {
					result["cut"], result["nextOffset"] = true, end
				}
				return result, nil
			})),
		reading("ha_state", "One released Home Assistant entity with all attributes (brightness, temperature, …).",
			schema(props(map[string]any{"entity": str("Entity ID, e.g. light.kitchen.")}), "entity"), true,
			on(func(ctx context.Context, ha *homeassistant.Manager, args json.RawMessage) (any, error) {
				var q struct{ Entity string }
				if err := toolcatalog.DecodeArgs(args, &q); err != nil {
					return nil, err
				}
				if !ha.AllowedCtx(ctx, q.Entity) {
					return nil, homeassistant.ErrNotAllowed
				}
				return ha.State(ctx, q.Entity)
			})),
		reading("ha_history", "Read up to 24 hours of recorded states for one released entity. Returns source and timestamps, pages of 200. Recorder retention and outages can leave gaps; absence does not prove no activity.",
			schema(props(map[string]any{"entity": str("Exact released entity ID"), "start": str("RFC3339 start"), "end": str("RFC3339 end"), "offset": map[string]any{"type": "integer", "minimum": 0, "maximum": 100000}}), "entity", "start", "end"), true,
			on(func(ctx context.Context, ha *homeassistant.Manager, args json.RawMessage) (any, error) {
				var q struct {
					Entity     string
					Start, End time.Time
					Offset     int
				}
				if err := toolcatalog.DecodeArgs(args, &q); err != nil {
					return nil, err
				}
				return ha.History(ctx, q.Entity, q.Start, q.End, q.Offset)
			})),
		actionTool("ha_call", "Propose to call a Home Assistant service for released entities, e.g. light.turn_on with data {\"brightness_pct\": 50}, switch.turn_off, scene.turn_on, climate.set_temperature. All entities must belong to the service's domain.",
			props(map[string]any{
				"domain":   str("Domain of the service and the entities, e.g. light."),
				"service":  str("Service without domain, e.g. turn_on."),
				"entities": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "maxItems": 50, "description": "Entity IDs from ha_entities."},
				"data":     map[string]any{"type": "object", "description": "Service data without targets, optional."},
			}), []string{"domain", "service", "entities"},
			func(ctx context.Context, args json.RawMessage) (*toolcatalog.Action, error) {
				ha, args, err := s.haConnectionArg(args)
				if err != nil {
					return nil, err
				}
				var q struct {
					Domain, Service, Reason string
					Entities                []string
					Data                    map[string]any
				}
				if err := toolcatalog.DecodeArgs(args, &q); err != nil {
					return nil, err
				}
				level, ok := homeassistant.Level(q.Domain)
				if !ok || !homeassistant.ValidService(q.Service) {
					return nil, homeassistant.ErrNotAllowed
				}
				for _, e := range q.Entities {
					if !strings.HasPrefix(e, q.Domain+".") || !ha.CanCallCtx(ctx, e, q.Service) {
						return nil, homeassistant.ErrNotAllowed
					}
				}
				summary := q.Domain + "." + q.Service + " → " + strings.Join(q.Entities, ", ")
				action := card("api.change", map[string]string{"summary": summary}, "POST", "/homeassistant/call", haCall{q.Domain, q.Service, q.Entities, q.Data, haConnID(ha)})
				if level == homeassistant.LevelDanger {
					return withLevel(action, toolcatalog.LevelDanger, "", ""), nil
				}
				return withLevel(action, toolcatalog.LevelNormal, "", ""), nil
			}),
	}
}

// haConnID is the ID a request names a connection by: empty for the first
// one, so bodies stay as they were with one connection.
func haConnID(m *homeassistant.Manager) string {
	if m == nil || m.ID() == homeassistant.PrimaryID {
		return ""
	}
	return m.ID()
}

// haDiagnosticsSchema is no arguments, or only the connection.
func haDiagnosticsSchema(props map[string]any) map[string]any {
	if props == nil {
		return nil
	}
	return schema(props)
}
