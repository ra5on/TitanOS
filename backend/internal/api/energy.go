package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/system"
)

func (s *Server) handleEnergy(w http.ResponseWriter, r *http.Request) {
	if s.opts.Energy == nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "energy settings unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s.opts.Energy.Status())
}

func (s *Server) handleEnergyChange(w http.ResponseWriter, r *http.Request) {
	if s.opts.Energy == nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "energy settings unavailable")
		return
	}
	if s.opts.Energy.Disabled {
		writeError(w, http.StatusConflict, "read_only", "energy profiles are read-only in development mode")
		return
	}
	var settings system.EnergySettings
	if !readJSON(w, r, &settings) {
		return
	}
	if err := s.opts.Energy.Set(settings); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, system.ErrEnergyChoice) {
			status = http.StatusBadRequest
		}
		writeError(w, status, "energy_failed", err.Error())
		return
	}
	slog.Info("energy profiles changed", "by", currentUser(r).Name)
	s.handleEnergy(w, r)
}
