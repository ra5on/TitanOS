package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/system"
)

func (s *Server) handlePowerButton(w http.ResponseWriter, r *http.Request) {
	if s.opts.PowerButton == nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "power button settings unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s.opts.PowerButton.Status())
}
func (s *Server) handlePowerButtonChange(w http.ResponseWriter, r *http.Request) {
	if s.opts.PowerButton == nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "power button settings unavailable")
		return
	}
	if s.opts.PowerButton.Disabled {
		writeError(w, http.StatusConflict, "read_only", "development mode")
		return
	}
	var settings system.PowerButtonSettings
	if !readJSON(w, r, &settings) {
		return
	}
	if err := s.opts.PowerButton.Set(settings); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, system.ErrPowerButtonChoice) {
			status = http.StatusBadRequest
		}
		writeError(w, status, "power_button_failed", err.Error())
		return
	}
	slog.Info("power button action changed", "by", currentUser(r).Name, "action", settings.Action)
	s.handlePowerButton(w, r)
}
