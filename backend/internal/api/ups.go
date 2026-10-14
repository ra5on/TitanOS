package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/ups"
)

func writeUPSError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ups.ErrDisabled):
		writeError(w, http.StatusConflict, "read_only", err.Error())
	case errors.Is(err, ups.ErrNotLoaded):
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
	case errors.Is(err, ups.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_ups_settings", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
	}
}

func (s *Server) handleUPS(w http.ResponseWriter, r *http.Request) {
	settings, passwordSet := s.opts.UPS.Settings()
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings, "passwordSet": passwordSet, "status": s.opts.UPS.Status(), "drivers": ups.Drivers})
}

func (s *Server) handleUPSChange(w http.ResponseWriter, r *http.Request) {
	var settings ups.Settings
	if !readJSON(w, r, &settings) {
		return
	}
	if err := s.opts.UPS.SetSettings(settings); err != nil {
		writeUPSError(w, err)
		return
	}
	slog.Info("UPS settings changed", "mode", settings.Mode, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleUPS(w, r)
}

// handleUPSScan looks for UPS on USB.
func (s *Server) handleUPSScan(w http.ResponseWriter, r *http.Request) {
	found, err := s.opts.UPS.Scan(r.Context())
	if err != nil {
		writeUPSError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": found})
}
