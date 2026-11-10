package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/system"
)

// energyArea is the energy profiles; an area with narrow dependencies
// (PLAN 8a, see fansArea).
type energyArea struct {
	energy *system.EnergyManager
}

func (a energyArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/system/energy", admin(a.handleEnergy))
	mux.HandleFunc("PUT /api/v1/system/energy", admin(a.handleEnergyChange))
}

func (a energyArea) handleEnergy(w http.ResponseWriter, r *http.Request) {
	if a.energy == nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "energy settings unavailable")
		return
	}
	writeJSON(w, http.StatusOK, a.energy.Status())
}

func (a energyArea) handleEnergyChange(w http.ResponseWriter, r *http.Request) {
	if a.energy == nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "energy settings unavailable")
		return
	}
	if a.energy.Disabled {
		writeError(w, http.StatusConflict, "read_only", "energy profiles are read-only in development mode")
		return
	}
	var settings system.EnergySettings
	if !readJSON(w, r, &settings) {
		return
	}
	if err := a.energy.Set(settings); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, system.ErrEnergyChoice) {
			status = http.StatusBadRequest
		}
		writeError(w, status, "energy_failed", err.Error())
		return
	}
	slog.Info("energy profiles changed", "by", currentUser(r).Name)
	a.handleEnergy(w, r)
}

// powerScheduleArea is the power schedule; an area with narrow dependencies
// (PLAN 8a, see fansArea). schedule is nil where there is none.
type powerScheduleArea struct {
	schedule *system.PowerScheduler
}

func (a powerScheduleArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/system/power-schedule", admin(a.handlePowerSchedule))
	mux.HandleFunc("PUT /api/v1/system/power-schedule", admin(a.handlePowerScheduleChange))
}

func (a powerScheduleArea) handlePowerSchedule(w http.ResponseWriter, r *http.Request) {
	if a.schedule == nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "power schedule unavailable")
		return
	}
	writeJSON(w, http.StatusOK, a.schedule.Status())
}

func (a powerScheduleArea) handlePowerScheduleChange(w http.ResponseWriter, r *http.Request) {
	if a.schedule == nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "power schedule unavailable")
		return
	}
	var settings system.PowerSchedule
	if !readJSON(w, r, &settings) {
		return
	}
	if err := a.schedule.Set(settings); err != nil {
		if errors.Is(err, system.ErrPowerSchedule) {
			writeError(w, http.StatusBadRequest, "invalid_power_schedule", err.Error())
			return
		}
		// Stored, but the hardware did not take all of it: the status says what.
		slog.Warn("power schedule applied with problems", "err", err)
	}
	slog.Info("power schedule changed", "by", currentUser(r).Name)
	a.handlePowerSchedule(w, r)
}

// PowerBusy says whether work is running that a scheduled shutdown would
// break off: copies, backups, disk tests or a rebuild.
func (s *Server) PowerBusy() (bool, string) {
	if s.core().update != nil && s.core().update.Working != nil && s.core().update.Working() {
		return true, "copies, backups or disk tests"
	}
	// Asked by the power schedule, not by a request: the root context.
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	if s.storageBusy(ctx) {
		return true, "volume rebuild or check"
	}
	return false, ""
}
