package api

import (
	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"log/slog"
	"net/http"
)

func (s *Server) handleNvidia(w http.ResponseWriter, r *http.Request) {
	if s.opts.Nvidia == nil {
		writeError(w, 503, "not_available", "NVIDIA settings unavailable")
		return
	}
	writeJSON(w, 200, s.opts.Nvidia.Status())
}
func (s *Server) handleNvidiaSet(w http.ResponseWriter, r *http.Request) {
	if s.opts.Nvidia == nil {
		writeError(w, 503, "not_available", "NVIDIA settings unavailable")
		return
	}
	var req hardware.NvidiaSettings
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Nvidia.Set(r.Context(), r.PathValue("address"), req); err != nil {
		writeError(w, 400, "hardware_settings", err.Error())
		return
	}
	slog.Info("NVIDIA power limit changed", "address", r.PathValue("address"), "percent", req.PowerPercent, "user", currentUser(r).Name)
	s.handleNvidia(w, r)
}

func (s *Server) handleContainerDevices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.opts.Docker.ContainerDevices(r.Context()))
}
