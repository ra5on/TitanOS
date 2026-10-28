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
