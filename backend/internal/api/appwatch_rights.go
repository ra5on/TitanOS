package api

// Least rights for the session of a watch (PLAN 116 package G, security
// review 22.11.2026): a watch is a session of the account like a phone app,
// but it only needs a few routes. Every other route answers 403
// watch_forbidden for it, among them the app routes for VPN, contacts and
// calendars, photos and files, the account, security and all settings.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"slices"

	"github.com/ra5on/TitanOS/backend/internal/appdevices"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// watchRoutes are the routes a watch may use (app-watch).
var watchRoutes = map[string]bool{
	"GET /api/v1/session":                          true,
	"GET /api/v1/system":                           true,
	"GET /api/v1/storage":                          true,
	"GET /api/v1/services/state":                   true,
	"GET /api/v1/app/notifications":                true,
	"GET /api/v1/nvr/cameras":                      true,
	"GET /api/v1/nvr/cameras/{id}/snapshot.jpg":    true,
	"GET /api/v1/nvr/events":                       true,
	"GET /api/v1/nvr/events/{id}/thumbnail.jpg":    true,
	"GET /api/v1/nvr/events/{id}/snapshot.jpg":     true,
	"GET /api/v1/agent/status":                     true,
	"GET /api/v1/agent/chats":                      true,
	"POST /api/v1/agent/chats":                     true,
	"GET /api/v1/agent/chats/{id}":                 true,
	"POST /api/v1/agent/chats/{id}/messages":       true,
	"POST /api/v1/agent/chats/{id}/decide":         true,
	"POST /api/v1/agent/transcribe":                true,
	"POST /api/v1/agent/speech/say":                true,
	"POST /api/v1/agent/speech/hear":               true,
	"GET /api/v1/homeassistant/entities":           true,
	"POST /api/v1/homeassistant/call":              true,
	"GET /api/v1/docker":                           true,
	"POST /api/v1/docker/containers/{id}/{action}": true,
	"GET /api/v1/vms":                              true,
	"POST /api/v1/vms/{name}/{action}":             true,
}

// Within those routes, the watch only starts and stops; in Home Assistant
// it only switches lights, switches, fans and helpers and runs scenes and
// scripts.
var (
	watchContainerActions = []string{"start", "stop", "restart"}
	watchVMActions        = []string{"start", "shutdown"}
	watchHADomains        = []string{"light", "switch", "fan", "input_boolean", "scene", "script"}
	watchHAServices       = []string{"turn_on", "turn_off", "toggle"}
)

// watchSession tells whether the request's session belongs to a paired
// watch of the account.
func (s *Server) watchSession(r *http.Request, user auth.User) bool {
	if s.appDevices == nil {
		return false
	}
	session, ok := s.sessionStore().Get(s.authArea().sessionToken(r))
	if !ok || session.Device == "" || session.User != user.Name {
		return false
	}
	d, err := s.appDevices.Device(user, session.Device)
	// An app the NAS no longer knows gets no more than a watch.
	return err != nil || d.App == appdevices.WatchApp
}

// watchAllows checks a request of a watch against watchRoutes.
func watchAllows(r *http.Request) bool {
	if !watchRoutes[r.Pattern] {
		return false
	}
	switch r.Pattern {
	case "POST /api/v1/docker/containers/{id}/{action}":
		return slices.Contains(watchContainerActions, r.PathValue("action"))
	case "POST /api/v1/vms/{name}/{action}":
		return slices.Contains(watchVMActions, r.PathValue("action"))
	case "POST /api/v1/homeassistant/call":
		body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBody+1))
		r.Body = io.NopCloser(bytes.NewReader(body))
		if err != nil || len(body) > maxJSONBody {
			return false
		}
		var call struct {
			Domain  string `json:"domain"`
			Service string `json:"service"`
		}
		return json.Unmarshal(body, &call) == nil && slices.Contains(watchHADomains, call.Domain) && slices.Contains(watchHAServices, call.Service)
	}
	return true
}
