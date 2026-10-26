package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/netbird"
	"github.com/ra5on/TitanOS/backend/internal/plugins"
)

func (s *Server) handleNetbird(w http.ResponseWriter, r *http.Request) {
	if s.opts.Netbird == nil {
		writeError(w, 503, "config_unavailable", "NetBird settings unavailable")
		return
	}
	writeJSON(w, 200, s.opts.Netbird.Status())
}
func writeNetbirdError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "netbird_failed"
	if errors.Is(err, netbird.ErrInvalid) {
		status = 400
	}
	if errors.Is(err, netbird.ErrSetupKey) {
		status, code = 409, "netbird_setup_key"
	}
	if errors.Is(err, netbird.ErrUnavailable) || errors.Is(err, netbird.ErrPending) || errors.Is(err, plugins.ErrDisabled) || errors.Is(err, plugins.ErrNotFound) || errors.Is(err, plugins.ErrUpdatePending) || errors.Is(err, plugins.ErrDriverPending) {
		status = 409
	}
	writeError(w, status, code, err.Error())
}

// handleNetbirdSettings starts a trial of the routing choices. The setup key
// is not part of this body; it has its own route.
func (s *Server) handleNetbirdSettings(w http.ResponseWriter, r *http.Request) {
	if s.opts.Netbird == nil {
		writeError(w, 503, "config_unavailable", "NetBird settings unavailable")
		return
	}
	var settings netbird.Settings
	if !readJSON(w, r, &settings) {
		return
	}
	if err := s.netbirdRuntimeAction(func() error { return s.opts.Netbird.Set(settings) }); err != nil {
		writeNetbirdError(w, err)
		return
	}
	slog.Info("NetBird configuration applied on trial", "by", currentUser(r).Name)
	s.handleNetbird(w, r)
}

// handleNetbirdSetupKey stores (or with an empty value removes) the setup
// key. It is never returned; GET only says whether one is set.
func (s *Server) handleNetbirdSetupKey(w http.ResponseWriter, r *http.Request) {
	if s.opts.Netbird == nil {
		writeError(w, 503, "config_unavailable", "NetBird settings unavailable")
		return
	}
	var body struct {
		SetupKey string `json:"setupKey"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if err := s.opts.Netbird.SetKey(body.SetupKey); err != nil {
		writeNetbirdError(w, err)
		return
	}
	slog.Info("NetBird setup key changed", "set", body.SetupKey != "", "by", currentUser(r).Name)
	s.handleNetbird(w, r)
}

func (s *Server) handleNetbirdAction(w http.ResponseWriter, r *http.Request) {
	if s.opts.Netbird == nil {
		writeError(w, 503, "config_unavailable", "NetBird settings unavailable")
		return
	}
	action := r.PathValue("action")
	var err error
	switch action {
	case "confirm":
		err = s.opts.Netbird.Confirm()
	case "undo":
		err = s.opts.Netbird.Undo()
	default:
		err = s.netbirdRuntimeAction(func() error { return s.opts.Netbird.Action(action) })
	}
	if err != nil {
		writeNetbirdError(w, err)
		return
	}
	slog.Info("NetBird action", "action", action, "by", currentUser(r).Name)
	s.handleNetbird(w, r)
}

func (s *Server) netbirdRuntimeAction(action func() error) error {
	if s.opts.Plugins != nil {
		return s.opts.Plugins.RuntimeAction("netbird", action)
	}
	return action()
}

// handleNetbirdPeers lists the NetBird network for the dashboard. Without an
// active plugin the answer is the state "inactive", not an error.
func (s *Server) handleNetbirdPeers(w http.ResponseWriter, r *http.Request) {
	if s.opts.Netbird == nil {
		writeJSON(w, 200, netbird.Peers{State: "inactive", Peers: []netbird.Peer{}})
		return
	}
	writeJSON(w, 200, s.opts.Netbird.Peers())
}
