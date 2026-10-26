package api

import (
	"context"
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

// pendingPower is a restart or shutdown that waits until no volume is
// rebuilt or checked any more. It lives in RAM: a restart of titand drops it.
type pendingPower struct {
	action string
	by     string
	since  time.Time
	cancel context.CancelFunc
}

type powerView struct {
	Action string    `json:"action"`
	By     string    `json:"by"`
	Since  time.Time `json:"since"`
}

// rebuildingNow says whether a volume is rebuilt or checked; tests replace
// the answer with s.rebuilding.
func (s *Server) rebuildingNow(ctx context.Context) bool {
	if s.rebuilding != nil {
		return s.rebuilding()
	}
	return s.storageBusy(ctx)
}

// powerPollInterval is how often a waiting restart looks at the volumes.
var powerPollInterval = 30 * time.Second

func (s *Server) handlePowerStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	res := map[string]any{"pending": nil, "rebuilding": s.rebuildingNow(ctx)}
	s.powerMu.Lock()
	if s.power.action != "" {
		res["pending"] = powerView{Action: s.power.action, By: s.power.by, Since: s.power.since}
	}
	s.powerMu.Unlock()
	writeJSON(w, http.StatusOK, res)
}

// handlePowerCancel drops a restart or shutdown that is still waiting.
func (s *Server) handlePowerCancel(w http.ResponseWriter, r *http.Request) {
	s.powerMu.Lock()
	action := s.power.action
	if s.power.cancel != nil {
		s.power.cancel()
	}
	s.power = pendingPower{}
	s.powerMu.Unlock()
	if action != "" {
		slog.Info("waiting power action cancelled", "action", action, "user", currentUser(r).Name, "ip", remoteIP(r))
	}
	s.handlePowerStatus(w, r)
}

func (s *Server) handlePower(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
		// AfterRebuild waits until no volume is rebuilt or checked.
		AfterRebuild bool `json:"afterRebuild"`
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
	run := func() {
		if err := action(); err != nil {
			slog.Error("power action failed", "action", req.Action, "err", err)
		}
	}

	// A new request replaces one that is still waiting.
	s.powerMu.Lock()
	if s.power.cancel != nil {
		s.power.cancel()
	}
	s.power = pendingPower{}
	if req.AfterRebuild {
		check, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		busy := s.rebuildingNow(check)
		cancel()
		if busy {
			ctx, cancel := context.WithCancel(context.Background())
			s.power.action, s.power.by, s.power.since, s.power.cancel = req.Action, currentUser(r).Name, time.Now().UTC(), cancel
			s.powerMu.Unlock()
			slog.Info("power action waits for the rebuild", "action", req.Action, "user", currentUser(r).Name, "ip", remoteIP(r))
			go s.waitForRebuild(ctx, req.Action, run)
			writeJSON(w, http.StatusAccepted, map[string]any{"pending": true})
			return
		}
	}
	s.powerMu.Unlock()

	slog.Info("power action requested", "action", req.Action, "user", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusAccepted)
	// Give the response time to reach the browser before the system goes down.
	go func() {
		time.Sleep(time.Second)
		run()
	}()
}

// waitForRebuild runs the action once no volume is rebuilt or checked, unless
// it is cancelled or replaced first.
func (s *Server) waitForRebuild(ctx context.Context, name string, run func()) {
	ticker := time.NewTicker(powerPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		check, cancel := context.WithTimeout(ctx, 10*time.Second)
		busy := s.rebuildingNow(check)
		cancel()
		if busy {
			continue
		}
		s.powerMu.Lock()
		if ctx.Err() != nil {
			s.powerMu.Unlock()
			return
		}
		s.power = pendingPower{}
		s.powerMu.Unlock()
		slog.Info("rebuild finished, running the waiting power action", "action", name)
		run()
		return
	}
}
