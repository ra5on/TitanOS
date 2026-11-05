package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/update"
)

// handleIntelGuc shows the GuC/HuC setting of Intel graphics with what the
// kernel loaded. The page asks once when it opens; it is not polled.
func (s *Server) handleIntelGuc(w http.ResponseWriter, r *http.Request) {
	if s.opts.IntelGuc == nil {
		writeError(w, http.StatusServiceUnavailable, "not_available", "Intel GPU settings unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s.opts.IntelGuc.Status(r.Context()))
}

func (s *Server) handleIntelGucSet(w http.ResponseWriter, r *http.Request) {
	if s.opts.IntelGuc == nil {
		writeError(w, http.StatusServiceUnavailable, "not_available", "Intel GPU settings unavailable")
		return
	}
	var req hardware.IntelGucSettings
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Mode)
	if err := s.opts.IntelGuc.Set(req.Mode); err != nil {
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
	s.handleIntelGuc(w, r)
}

// handleUASQuirks shows which USB bridges are driven with usb-storage
// instead of uas. The hardware page asks once when it opens.
func (s *Server) handleUASQuirks(w http.ResponseWriter, r *http.Request) {
	if s.opts.UASQuirks == nil {
		writeError(w, http.StatusServiceUnavailable, "not_available", "USB quirks unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s.opts.UASQuirks.Status())
}

type uasQuirkRequest struct {
	Device string `json:"device"`
	On     bool   `json:"on"`
}

// handleUASQuirksSet switches usb-storage for one device on or off; it
// takes effect after a restart. Only the owner's click on the hardware
// page gets here, TitanOS never sets it on its own.
func (s *Server) handleUASQuirksSet(w http.ResponseWriter, r *http.Request) {
	if s.opts.UASQuirks == nil {
		writeError(w, http.StatusServiceUnavailable, "not_available", "USB quirks unavailable")
		return
	}
	var req uasQuirkRequest
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Device)
	if err := s.opts.UASQuirks.Set(req.Device, req.On); err != nil {
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
	s.handleUASQuirks(w, r)
}
