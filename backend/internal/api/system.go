package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/system"
)

type systemResponse struct {
	system.Info
	Version string `json:"version"`
}

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, systemResponse{Info: s.opts.Monitor.Info(), Version: s.opts.Version})
}

func (s *Server) handlePower(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	var action func() error
	switch req.Action {
	case "reboot":
		action = s.opts.Power.Reboot
	case "poweroff":
		action = s.opts.Power.Poweroff
	default:
		writeError(w, http.StatusBadRequest, "bad_request", `action must be "reboot" or "poweroff"`)
		return
	}

	slog.Info("power action requested", "action", req.Action, "user", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusAccepted)
	// Give the response time to reach the browser before the system goes down.
	go func() {
		time.Sleep(time.Second)
		if err := action(); err != nil {
			slog.Error("power action failed", "action", req.Action, "err", err)
		}
	}()
}
