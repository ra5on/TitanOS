package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/assistant"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
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

func (s *Server) haReady(w http.ResponseWriter) bool {
	if s.opts.HomeAssistant == nil {
		writeError(w, http.StatusNotFound, "ha_off", "Home Assistant is not available")
		return false
	}
	return true
}

func (s *Server) handleHASettings(w http.ResponseWriter, r *http.Request) {
	if !s.haReady(w) {
		return
	}
	settings, tokenSet := s.opts.HomeAssistant.Settings()
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings, "tokenSet": tokenSet, "domains": homeassistant.Domains()})
}

func (s *Server) handleHASettingsChange(w http.ResponseWriter, r *http.Request) {
	if !s.haReady(w) {
		return
	}
	var req struct {
		homeassistant.Settings
		ClearToken bool `json:"clearToken"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.HomeAssistant.SetSettings(req.Settings, req.ClearToken); err != nil {
		writeHAError(w, err)
		return
	}
	slog.Info("Home Assistant settings changed", "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleHASettings(w, r)
}

func (s *Server) handleHACheck(w http.ResponseWriter, r *http.Request) {
	if !s.haReady(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	status, err := s.opts.HomeAssistant.Check(ctx)
	if err != nil {
		writeHAError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// handleHAEntities lists every entity for choosing what to release.
func (s *Server) handleHAEntities(w http.ResponseWriter, r *http.Request) {
	if !s.haReady(w) {
		return
	}
	states, err := s.opts.HomeAssistant.States(r.Context(), false)
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
	if !s.haReady(w) {
		return
	}
	services, err := s.opts.HomeAssistant.NotifyServices(r.Context())
	if err != nil {
		writeHAError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": services})
}

type haCall struct {
	Domain   string         `json:"domain"`
	Service  string         `json:"service"`
	Entities []string       `json:"entities"`
	Data     map[string]any `json:"data,omitempty"`
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
	changed, err := s.opts.HomeAssistant.Call(r.Context(), req.Domain, req.Service, req.Entities, req.Data)
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
		list = append(list, entity{st.EntityID, st.State})
	}
	writeJSON(w, http.StatusOK, map[string]any{"changed": list})
}

// haTools are the assistant's tools for Home Assistant, only for owners and
// administrators and only while a server is set up.
func (s *Server) haTools(user auth.User) []assistant.Tool {
	ha := s.opts.HomeAssistant
	if ha == nil || !ha.Active() || !user.Role.CanAdminister() {
		return nil
	}
	return []assistant.Tool{
		reading("ha_entities", "Entities of Home Assistant released to TitanOS (lights, switches, sensors …) with state and area. Only released ones are visible. Pages of 100.",
			schema(map[string]any{
				"domain": str("Only this domain, e.g. light."),
				"area":   str("Only entities in an area whose name contains this text."),
				"search": str("Text in the ID or name."),
				"offset": map[string]any{"type": "integer", "minimum": 0, "description": "Next page from nextOffset."},
			}), true,
			func(ctx context.Context, args json.RawMessage) (any, error) {
				var q struct {
					Domain, Area, Search string
					Offset               int
				}
				if err := assistant.DecodeArgs(args, &q); err != nil {
					return nil, err
				}
				states, err := ha.States(ctx, true)
				if err != nil {
					return nil, err
				}
				type entity struct {
					ID    string `json:"id"`
					Name  string `json:"name"`
					State string `json:"state"`
					Area  string `json:"area,omitempty"`
					Unit  string `json:"unit,omitempty"`
				}
				var list []entity
				for _, st := range states {
					domain, _, _ := strings.Cut(st.EntityID, ".")
					search := strings.ToLower(q.Search)
					if q.Domain != "" && domain != q.Domain || q.Area != "" && !strings.Contains(strings.ToLower(st.Area), strings.ToLower(q.Area)) ||
						search != "" && !strings.Contains(strings.ToLower(st.EntityID+" "+st.Name()), search) {
						continue
					}
					unit, _ := st.Attributes["unit_of_measurement"].(string)
					list = append(list, entity{st.EntityID, st.Name(), st.State, st.Area, unit})
				}
				total := len(list)
				start := min(q.Offset, total)
				end := min(start+100, total)
				result := map[string]any{"entities": list[start:end], "total": total}
				if end < total {
					result["cut"], result["nextOffset"] = true, end
				}
				return result, nil
			}),
		reading("ha_state", "One released Home Assistant entity with all attributes (brightness, temperature, …).",
			schema(map[string]any{"entity": str("Entity ID, e.g. light.kitchen.")}, "entity"), true,
			func(ctx context.Context, args json.RawMessage) (any, error) {
				var q struct{ Entity string }
				if err := assistant.DecodeArgs(args, &q); err != nil {
					return nil, err
				}
				if !ha.Allowed(q.Entity) {
					return nil, homeassistant.ErrNotAllowed
				}
				return ha.State(ctx, q.Entity)
			}),
		actionTool("ha_call", "Propose to call a Home Assistant service for released entities, e.g. light.turn_on with data {\"brightness_pct\": 50}, switch.turn_off, scene.turn_on, climate.set_temperature. All entities must belong to the service's domain.",
			map[string]any{
				"domain":   str("Domain of the service and the entities, e.g. light."),
				"service":  str("Service without domain, e.g. turn_on."),
				"entities": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "maxItems": 50, "description": "Entity IDs from ha_entities."},
				"data":     map[string]any{"type": "object", "description": "Service data without targets, optional."},
			}, []string{"domain", "service", "entities"},
			func(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
				var q struct {
					Domain, Service, Reason string
					Entities                []string
					Data                    map[string]any
				}
				if err := assistant.DecodeArgs(args, &q); err != nil {
					return nil, err
				}
				level, ok := homeassistant.Level(q.Domain)
				if !ok || !homeassistant.ValidService(q.Service) {
					return nil, homeassistant.ErrNotAllowed
				}
				for _, e := range q.Entities {
					if !strings.HasPrefix(e, q.Domain+".") || !ha.Allowed(e) {
						return nil, homeassistant.ErrNotAllowed
					}
				}
				summary := q.Domain + "." + q.Service + " → " + strings.Join(q.Entities, ", ")
				action := card("api.change", map[string]string{"summary": summary}, "POST", "/homeassistant/call", haCall{q.Domain, q.Service, q.Entities, q.Data})
				if level == homeassistant.LevelDanger {
					return withLevel(action, assistant.LevelDanger, "", ""), nil
				}
				return withLevel(action, assistant.LevelNormal, "", ""), nil
			}),
	}
}
