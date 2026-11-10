package api

import (
	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"log/slog"
	"net/http"
)

// nvidiaArea is the power limit of NVIDIA cards; an area with narrow
// dependencies (PLAN 8a, see fansArea).
type nvidiaArea struct {
	nvidia *hardware.NvidiaControl
}

func (a nvidiaArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/hardware/nvidia", admin(a.handleNvidia))
	mux.HandleFunc("PUT /api/v1/hardware/nvidia/{address}", admin(a.handleNvidiaSet))
}

func (a nvidiaArea) handleNvidia(w http.ResponseWriter, r *http.Request) {
	if a.nvidia == nil {
		writeError(w, 503, "not_available", "NVIDIA settings unavailable")
		return
	}
	writeJSON(w, 200, a.nvidia.Status())
}
func (a nvidiaArea) handleNvidiaSet(w http.ResponseWriter, r *http.Request) {
	if a.nvidia == nil {
		writeError(w, 503, "not_available", "NVIDIA settings unavailable")
		return
	}
	var req hardware.NvidiaSettings
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.nvidia.Set(r.Context(), r.PathValue("address"), req); err != nil {
		writeError(w, 400, "hardware_settings", err.Error())
		return
	}
	slog.Info("NVIDIA power limit changed", "address", r.PathValue("address"), "percent", req.PowerPercent, "user", currentUser(r).Name)
	a.handleNvidia(w, r)
}

func (s *Server) handleContainerDevices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.opts.Docker.ContainerDevices(r.Context()))
}
