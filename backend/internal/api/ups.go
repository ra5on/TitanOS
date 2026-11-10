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

// upsArea is the uninterruptible power supply; an area with narrow
// dependencies (PLAN 8a, see fansArea).
type upsArea struct {
	ups *ups.Manager
}

func (a upsArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/ups", admin(a.handleUPS))
	mux.HandleFunc("PUT /api/v1/ups", admin(a.handleUPSChange))
	mux.HandleFunc("POST /api/v1/ups/scan", admin(a.handleUPSScan))
}

func (a upsArea) handleUPS(w http.ResponseWriter, r *http.Request) {
	settings, passwordSet := a.ups.Settings()
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings, "passwordSet": passwordSet, "status": a.ups.Status(), "drivers": ups.Drivers})
}

func (a upsArea) handleUPSChange(w http.ResponseWriter, r *http.Request) {
	var settings ups.Settings
	if !readJSON(w, r, &settings) {
		return
	}
	if err := a.ups.SetSettings(settings); err != nil {
		writeUPSError(w, err)
		return
	}
	slog.Info("UPS settings changed", "mode", settings.Mode, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleUPS(w, r)
}

// handleUPSScan looks for UPS on USB.
func (a upsArea) handleUPSScan(w http.ResponseWriter, r *http.Request) {
	found, err := a.ups.Scan(r.Context())
	if err != nil {
		writeUPSError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": found})
}
