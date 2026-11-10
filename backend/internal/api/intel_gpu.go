package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/update"
)

// intelGucArea is the GuC/HuC setting of Intel graphics; an area with
// narrow dependencies (PLAN 8a, see fansArea).
type intelGucArea struct {
	guc *hardware.IntelGuc
}

func (a intelGucArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/hardware/intel-gpu", admin(a.handleIntelGuc))
	mux.HandleFunc("PUT /api/v1/hardware/intel-gpu", admin(a.handleIntelGucSet))
}

// handleIntelGuc shows the GuC/HuC setting of Intel graphics with what the
// kernel loaded. The page asks once when it opens; it is not polled.
func (a intelGucArea) handleIntelGuc(w http.ResponseWriter, r *http.Request) {
	if a.guc == nil {
		writeError(w, http.StatusServiceUnavailable, "not_available", "Intel GPU settings unavailable")
		return
	}
	writeJSON(w, http.StatusOK, a.guc.Status(r.Context()))
}

func (a intelGucArea) handleIntelGucSet(w http.ResponseWriter, r *http.Request) {
	if a.guc == nil {
		writeError(w, http.StatusServiceUnavailable, "not_available", "Intel GPU settings unavailable")
		return
	}
	var req hardware.IntelGucSettings
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Mode)
	if err := a.guc.Set(req.Mode); err != nil {
		switch {
		case errors.Is(err, hardware.ErrGucMode):
			writeError(w, http.StatusBadRequest, "invalid", err.Error())
		case errors.Is(err, hardware.ErrNoIntelGen9):
			writeError(w, http.StatusConflict, "intel_guc_not_applicable", err.Error())
		case errors.Is(err, hardware.ErrBootUnavailable):
			writeError(w, http.StatusConflict, "intel_guc_unavailable", err.Error())
		case errors.Is(err, update.ErrBusy):
			writeError(w, http.StatusConflict, "update_busy", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "intel_guc_failed", err.Error())
		}
		return
	}
	slog.Info("Intel GuC/HuC setting changed, effective after a restart", "mode", req.Mode, "user", currentUser(r).Name)
	a.handleIntelGuc(w, r)
}

// uasQuirksArea is usb-storage instead of uas for USB bridges; an area
// with narrow dependencies (PLAN 8a, see fansArea).
type uasQuirksArea struct {
	quirks *hardware.UASQuirks
}

func (a uasQuirksArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/hardware/uas-quirks", admin(a.handleUASQuirks))
	mux.HandleFunc("PUT /api/v1/hardware/uas-quirks", admin(a.handleUASQuirksSet))
}

// handleUASQuirks shows which USB bridges are driven with usb-storage
// instead of uas. The hardware page asks once when it opens.
func (a uasQuirksArea) handleUASQuirks(w http.ResponseWriter, r *http.Request) {
	if a.quirks == nil {
		writeError(w, http.StatusServiceUnavailable, "not_available", "USB quirks unavailable")
		return
	}
	writeJSON(w, http.StatusOK, a.quirks.Status())
}

type uasQuirkRequest struct {
	Device string `json:"device"`
	On     bool   `json:"on"`
}

// handleUASQuirksSet switches usb-storage for one device on or off; it
// takes effect after a restart. Only the owner's click on the hardware
// page gets here, TitanOS never sets it on its own.
func (a uasQuirksArea) handleUASQuirksSet(w http.ResponseWriter, r *http.Request) {
	if a.quirks == nil {
		writeError(w, http.StatusServiceUnavailable, "not_available", "USB quirks unavailable")
		return
	}
	var req uasQuirkRequest
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Device)
	if err := a.quirks.Set(req.Device, req.On); err != nil {
		switch {
		case errors.Is(err, hardware.ErrUASDevice), errors.Is(err, hardware.ErrUASFull):
			writeError(w, http.StatusBadRequest, "invalid", err.Error())
		case errors.Is(err, hardware.ErrBootUnavailable):
			writeError(w, http.StatusConflict, "uas_quirks_unavailable", err.Error())
		case errors.Is(err, update.ErrBusy):
			writeError(w, http.StatusConflict, "update_busy", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "uas_quirks_failed", err.Error())
		}
		return
	}
	slog.Info("USB quirk changed, effective after a restart", "device", req.Device, "usb_storage", req.On, "user", currentUser(r).Name)
	a.handleUASQuirks(w, r)
}
