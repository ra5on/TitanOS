package api

import (
	"errors"
	"github.com/ra5on/TitanOS/backend/internal/plugins"
	"github.com/ra5on/TitanOS/backend/internal/tailscale"
	"log/slog"
	"net/http"
)

func (s *Server) handleTailscale(w http.ResponseWriter, r *http.Request) {
	if s.opts.Tailscale == nil {
		writeError(w, 503, "config_unavailable", "Tailscale settings unavailable")
		return
	}
	writeJSON(w, 200, s.opts.Tailscale.Status())
}
func writeTailscaleError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, tailscale.ErrInvalid) {
		status = 400
	}
	if errors.Is(err, tailscale.ErrUnavailable) || errors.Is(err, tailscale.ErrPending) || errors.Is(err, plugins.ErrDisabled) || errors.Is(err, plugins.ErrNotFound) || errors.Is(err, plugins.ErrUpdatePending) || errors.Is(err, plugins.ErrDriverPending) {
		status = 409
	}
	writeError(w, status, "tailscale_failed", err.Error())
}
func (s *Server) handleTailscaleSettings(w http.ResponseWriter, r *http.Request) {
	if s.opts.Tailscale == nil {
		writeError(w, 503, "config_unavailable", "Tailscale settings unavailable")
		return
	}
	var settings tailscale.Settings
	if !readJSON(w, r, &settings) {
		return
	}
	if err := s.tailscaleRuntimeAction(func() error { return s.opts.Tailscale.Set(settings) }); err != nil {
		writeTailscaleError(w, err)
		return
	}
	slog.Info("Tailscale configuration applied on trial", "by", currentUser(r).Name)
	s.handleTailscale(w, r)
}
func (s *Server) handleTailscaleAction(w http.ResponseWriter, r *http.Request) {
	if s.opts.Tailscale == nil {
		writeError(w, 503, "config_unavailable", "Tailscale settings unavailable")
		return
	}
	action := r.PathValue("action")
	var err error
	switch action {
	case "confirm":
		err = s.opts.Tailscale.Confirm()
	case "undo":
		err = s.opts.Tailscale.Undo()
	default:
		err = s.tailscaleRuntimeAction(func() error { return s.opts.Tailscale.Action(action) })
	}
	if err != nil {
		writeTailscaleError(w, err)
		return
	}
	slog.Info("Tailscale action", "action", action, "by", currentUser(r).Name)
	s.handleTailscale(w, r)
}

func (s *Server) tailscaleRuntimeAction(action func() error) error {
	if s.opts.Plugins != nil {
		return s.opts.Plugins.RuntimeAction("tailscale", action)
	}
	return action()
}

// handleTailscaleDevices lists the tailnet for the dashboard. Without an
// active plugin the answer is the state "inactive", not an error.
func (s *Server) handleTailscaleDevices(w http.ResponseWriter, r *http.Request) {
	if s.opts.Tailscale == nil {
		writeJSON(w, 200, tailscale.Devices{State: "inactive", Devices: []tailscale.Device{}})
		return
	}
	writeJSON(w, 200, s.opts.Tailscale.Devices())
}
