package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/system"
)

type systemResponse struct {
	system.Info
	Version      string                 `json:"version"`
	Accelerators []hardware.Accelerator `json:"accelerators"`
	// NetworkPendingSeconds is how long a network change on trial has
	// left to be confirmed; 0 if there is none. Every page shows it.
	NetworkPendingSeconds int `json:"networkPendingSeconds"`
	// NotificationsLatest is the number of the newest message, for owners
	// and administrators; the web UI fetches the list when it changes.
	NotificationsLatest int `json:"notificationsLatest"`
}

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	res := systemResponse{Info: s.opts.Monitor.Info(), Version: s.opts.Version, NetworkPendingSeconds: s.opts.Network.PendingSeconds()}
	res.Accelerators = []hardware.Accelerator{}
	if s.opts.Accelerators != nil {
		res.Accelerators = s.opts.Accelerators.Snapshot()
	}
	if currentUser(r).Role.CanAdminister() {
		res.NotificationsLatest = s.opts.Notify.Latest()
	}
	writeJSON(w, http.StatusOK, res)
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
