package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/notify"
)

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"messages": s.opts.Notify.Messages()})
}

func (s *Server) handleDismissNotifications(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int `json:"ids"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.IDs) > 100 {
		writeError(w, http.StatusBadRequest, "invalid_request", "too many notification IDs")
		return
	}
	for _, id := range req.IDs {
		if id <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid notification ID")
			return
		}
	}
	s.opts.Notify.Dismiss(req.IDs)
	s.handleNotifications(w, r)
}

// handleNotifySettings shows where messages go. Passwords and tokens are
// left out; secretSet says which of them are stored.
func (s *Server) handleNotifySettings(w http.ResponseWriter, r *http.Request) {
	settings, set := s.opts.Notify.Settings()
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings, "secretSet": set})
}

func (s *Server) handleNotifySettingsChange(w http.ResponseWriter, r *http.Request) {
	// ClearSecrets names the channels whose stored password or token is
	// to be dropped; an empty one in the settings keeps what is stored.
	var req struct {
		notify.Settings
		ClearSecrets []string `json:"clearSecrets"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Notify.SetSettings(req.Settings, req.ClearSecrets...); err != nil {
		switch {
		case errors.Is(err, notify.ErrInvalid):
			writeError(w, http.StatusBadRequest, "invalid_notify_settings", err.Error())
		case errors.Is(err, notify.ErrNotLoaded):
			writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
		}
		return
	}
	slog.Info("notification settings changed", "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleNotifySettings(w, r)
}

// handleNotifyTest sends a test message through one channel with the
// stored settings and says what the other side answered.
func (s *Server) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	var req struct{ Channel string }
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Notify.Test(req.Channel); err != nil {
		if errors.Is(err, notify.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "invalid_notify_settings", err.Error())
			return
		}
		writeError(w, http.StatusBadGateway, "notify_test_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
