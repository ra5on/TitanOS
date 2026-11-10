package api

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/network"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/shutdown"
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

// systemArea is the overview of the system; an area with narrow
// dependencies (PLAN 8a, see fansArea). accelerators and notify are left out
// where they are nil.
type systemArea struct {
	monitor      *system.Monitor
	version      string
	network      *network.Manager
	accelerators *hardware.AcceleratorMonitor
	notify       *notify.Manager
}

func (a systemArea) register(mux *routeMux, user func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/system", user(a.handleSystem))
}

func (a systemArea) handleSystem(w http.ResponseWriter, r *http.Request) {
	res := systemResponse{Info: a.monitor.Info(), Version: a.version, NetworkPendingSeconds: a.network.PendingSeconds()}
	res.Accelerators = []hardware.Accelerator{}
	if a.accelerators != nil {
		res.Accelerators = a.accelerators.Snapshot()
	}
	if currentUser(r).Role.CanAdminister() {
		res.NotificationsLatest = a.notify.Latest()
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

// powerArea is restart and shutdown; an area with narrow dependencies
// (PLAN 8a, see fansArea). shutdown reads what ends the guests first when
// it is asked (nil: nothing; tests replace it on a running server), power
// switches the machine. mu guards pending, the restart or shutdown that
// waits for a rebuild (state of the server). ctx is the root context of the
// server, rebuildingNow says whether an array is rebuilt or checked, and
// goJob starts the wait that outlives its request.
type powerArea struct {
	shutdown      func() *shutdown.Manager
	power         system.Power
	mu            *sync.Mutex
	pending       *pendingPower
	ctx           context.Context
	rebuildingNow func(ctx context.Context) bool
	goJob         jobFunc
}

// powerArea is the area of the server.
func (s *Server) powerArea() powerArea {
	return powerArea{shutdown: func() *shutdown.Manager { return s.opts.Shutdown }, power: s.opts.Power, mu: &s.powerMu, pending: &s.power, ctx: s.ctx, rebuildingNow: s.rebuildingNow, goJob: s.goJob}
}

func (a powerArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("POST /api/v1/system/power", admin(a.handlePower))
	mux.HandleFunc("GET /api/v1/system/power", admin(a.handlePowerStatus))
	mux.HandleFunc("DELETE /api/v1/system/power", admin(a.handlePowerCancel))
	mux.HandleFunc("POST /api/v1/system/power/now", admin(a.handlePowerNow))
}

func (a powerArea) handlePowerStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	res := map[string]any{"pending": nil, "rebuilding": a.rebuildingNow(ctx), "preparing": nil}
	if a.shutdown() != nil {
		if status := a.shutdown().Status(); status != nil {
			res["preparing"] = status
		}
	}
	a.mu.Lock()
	if a.pending.action != "" {
		res["pending"] = powerView{Action: a.pending.action, By: a.pending.by, Since: a.pending.since}
	}
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, res)
}

// handlePowerCancel drops a restart or shutdown that is still waiting, for
// a rebuild or for the guests; guests already ended are started again.
func (a powerArea) handlePowerCancel(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	action := a.pending.action
	if a.pending.cancel != nil {
		a.pending.cancel()
	}
	*a.pending = pendingPower{}
	a.mu.Unlock()
	if action != "" {
		slog.Info("waiting power action cancelled", "action", action, "user", currentUser(r).Name, "ip", remoteIP(r))
	}
	if a.shutdown() != nil {
		// Starting the guests again is no matter of the browser's request.
		if err := a.shutdown().Cancel(context.WithoutCancel(r.Context())); err == nil {
			slog.Info("going down called off", "user", currentUser(r).Name, "ip", remoteIP(r))
		}
	}
	a.handlePowerStatus(w, r)
}

func (a powerArea) shutdownStatus() *shutdown.Status {
	if a.shutdown() == nil {
		return nil
	}
	return a.shutdown().Status()
}

// handlePowerNow goes down without waiting any longer for the guests.
func (a powerArea) handlePowerNow(w http.ResponseWriter, r *http.Request) {
	if a.shutdown() == nil || a.shutdown().Now() != nil {
		writeError(w, http.StatusConflict, "power_not_preparing", "the NAS is not being prepared for going down")
		return
	}
	slog.Info("going down without waiting for the guests", "user", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusAccepted)
}

func (a powerArea) handlePower(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
		// AfterRebuild waits until no volume is rebuilt or checked.
		AfterRebuild bool `json:"afterRebuild"`
		// Wait: someone watches in the web UI and decides when guests are
		// still running after the waiting time. Without it (Home Assistant,
		// MCP, API) the NAS goes on by itself then.
		Wait bool `json:"wait"`
		// Now skips ending the guests first (systemd still ends them).
		Now bool `json:"now"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	var action func() error
	switch req.Action {
	case "reboot":
		action = a.power.Reboot
	case "poweroff":
		action = a.power.Poweroff
	default:
		writeError(w, http.StatusBadRequest, "bad_request", `action must be "reboot" or "poweroff"`)
		return
	}
	by := currentUser(r).Name
	run := func() {
		if a.shutdown() != nil && !req.Now {
			if err := a.shutdown().Begin(req.Action, by, !req.Wait, action); err != nil {
				slog.Error("power action not started", "action", req.Action, "err", err)
			}
			return
		}
		if err := action(); err != nil {
			slog.Error("power action failed", "action", req.Action, "err", err)
		}
	}
	if st := a.shutdownStatus(); st != nil && !req.Now && st.Phase != "failed" {
		writeError(w, http.StatusConflict, "power_preparing", "the NAS is already being prepared for going down")
		return
	}

	// A new request replaces one that is still waiting.
	a.mu.Lock()
	if a.pending.cancel != nil {
		a.pending.cancel()
	}
	*a.pending = pendingPower{}
	if req.AfterRebuild {
		check, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		busy := a.rebuildingNow(check)
		cancel()
		if busy {
			// The wait outlives the request; it ends when it is replaced,
			// cancelled or the server closes.
			ctx, cancel := context.WithCancel(a.ctx)
			a.pending.action, a.pending.by, a.pending.since, a.pending.cancel = req.Action, currentUser(r).Name, time.Now().UTC(), cancel
			a.mu.Unlock()
			slog.Info("power action waits for the rebuild", "action", req.Action, "user", currentUser(r).Name, "ip", remoteIP(r))
			a.goJob(func(context.Context) { a.waitForRebuild(ctx, req.Action, run) })
			writeJSON(w, http.StatusAccepted, map[string]any{"pending": true})
			return
		}
	}
	a.mu.Unlock()

	slog.Info("power action requested", "action", req.Action, "user", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusAccepted)
	// Give the response time to reach the browser before the system goes down.
	// The action runs even while the server closes: it was asked for.
	a.goJob(func(context.Context) {
		time.Sleep(time.Second)
		run()
	})
}

// waitForRebuild runs the action once no volume is rebuilt or checked, unless
// it is cancelled or replaced first.
func (a powerArea) waitForRebuild(ctx context.Context, name string, run func()) {
	ticker := time.NewTicker(powerPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		check, cancel := context.WithTimeout(ctx, 10*time.Second)
		busy := a.rebuildingNow(check)
		cancel()
		if busy {
			continue
		}
		a.mu.Lock()
		if ctx.Err() != nil {
			a.mu.Unlock()
			return
		}
		*a.pending = pendingPower{}
		a.mu.Unlock()
		slog.Info("rebuild finished, running the waiting power action", "action", name)
		run()
		return
	}
}
