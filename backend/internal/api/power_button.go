package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/system"
)

// powerButtonArea is what the power button does; an area with narrow
// dependencies (PLAN 8a, see fansArea).
type powerButtonArea struct {
	button *system.PowerButtonManager
}

func (a powerButtonArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/system/power-button", admin(a.handlePowerButton))
	mux.HandleFunc("PUT /api/v1/system/power-button", admin(a.handlePowerButtonChange))
}

func (a powerButtonArea) handlePowerButton(w http.ResponseWriter, r *http.Request) {
	if a.button == nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "power button settings unavailable")
		return
	}
	writeJSON(w, http.StatusOK, a.button.Status())
}
func (a powerButtonArea) handlePowerButtonChange(w http.ResponseWriter, r *http.Request) {
	if a.button == nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "power button settings unavailable")
		return
	}
	if a.button.Disabled {
		writeError(w, http.StatusConflict, "read_only", "development mode")
		return
	}
	var settings system.PowerButtonSettings
	if !readJSON(w, r, &settings) {
		return
	}
	if err := a.button.Set(settings); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, system.ErrPowerButtonChoice) {
			status = http.StatusBadRequest
		}
		writeError(w, status, "power_button_failed", err.Error())
		return
	}
	slog.Info("power button action changed", "by", currentUser(r).Name, "action", settings.Action)
	a.handlePowerButton(w, r)
}
