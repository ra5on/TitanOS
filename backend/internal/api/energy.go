package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

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

func (s *Server) handlePowerSchedule(w http.ResponseWriter, r *http.Request) {
	if s.opts.PowerSchedule == nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "power schedule unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s.opts.PowerSchedule.Status())
}

func (s *Server) handlePowerScheduleChange(w http.ResponseWriter, r *http.Request) {
	if s.opts.PowerSchedule == nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "power schedule unavailable")
		return
	}
	var settings system.PowerSchedule
	if !readJSON(w, r, &settings) {
		return
	}
	if err := s.opts.PowerSchedule.Set(settings); err != nil {
		if errors.Is(err, system.ErrPowerSchedule) {
			writeError(w, http.StatusBadRequest, "invalid_power_schedule", err.Error())
			return
		}
		// Stored, but the hardware did not take all of it: the status says what.
		slog.Warn("power schedule applied with problems", "err", err)
	}
	slog.Info("power schedule changed", "by", currentUser(r).Name)
	s.handlePowerSchedule(w, r)
}

// PowerBusy says whether work is running that a scheduled shutdown would
// break off: copies, backups, disk tests or a rebuild.
func (s *Server) PowerBusy() (bool, string) {
	if s.opts.Update != nil && s.opts.Update.Working != nil && s.opts.Update.Working() {
		return true, "copies, backups or disk tests"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if s.storageBusy(ctx) {
		return true, "volume rebuild or check"
	}
	return false, ""
}
