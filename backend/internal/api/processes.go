package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/procs"
)

// containerNameCache keeps the names of Docker containers for a few
// seconds: the task manager asks every two seconds.
type containerNameCache struct {
	mu    sync.Mutex
	at    time.Time
	names map[string]string
}

// processesArea is the task manager; an area with narrow dependencies
// (PLAN 8a, see fansArea). docker names the containers processes belong to
// (nil names none), names keeps them for a few seconds.
type processesArea struct {
	procs  *procs.Sampler
	docker *docker.Manager
	names  *containerNameCache
}

func (a processesArea) register(mux *routeMux, admin, browserOnly func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/system/processes", admin(a.handleProcesses))
	mux.HandleFunc("POST /api/v1/system/processes/{pid}/signal", admin(browserOnly(a.handleProcessSignal)))
}

// handleProcesses lists the processes of the NAS with CPU, memory and
// what they belong to (task manager, PLAN 61). Owners and administrators
// only; not in publicRoutes, so neither tokens nor MCP reach it.
func (a processesArea) handleProcesses(w http.ResponseWriter, r *http.Request) {
	list, err := a.procs.List()
	if err != nil {
		slog.Error("processes", "err", err)
		writeError(w, http.StatusInternalServerError, "processes_failed", "the processes could not be read")
		return
	}
	var names map[string]string
	if a.docker != nil {
		c := a.names
		c.mu.Lock()
		if time.Since(c.at) > 5*time.Second {
			c.names, c.at = a.docker.ContainerNames(r.Context()), time.Now()
		}
		names = c.names
		c.mu.Unlock()
	}
	for i, p := range list.Processes {
		if p.Owner.Kind == "docker" {
			list.Processes[i].Owner.Name = names[p.Owner.ID]
		}
	}
	writeJSON(w, http.StatusOK, list)
}

// handleProcessSignal ends one process: "term" asks it to (SIGTERM),
// "kill" ends it at once (SIGKILL). start is the start time from the
// list; the request fails if the pid names another process by now.
// init, kernel threads, titand and its parent are refused.
func (a processesArea) handleProcessSignal(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.Atoi(r.PathValue("pid"))
	if err != nil {
		writeError(w, http.StatusNotFound, "process_not_found", "no such process")
		return
	}
	var req struct {
		Signal string `json:"signal"`
		Start  uint64 `json:"start"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name, err := a.procs.Signal(pid, req.Start, req.Signal)
	noteTarget(r, name, req.Signal)
	switch {
	case err == nil:
		slog.Info("process signal", "pid", pid, "name", name, "signal", req.Signal, "by", currentUser(r).Name, "ip", remoteIP(r))
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, procs.ErrSignal):
		writeError(w, http.StatusBadRequest, "invalid_signal", "signal must be term or kill")
	case errors.Is(err, procs.ErrNotFound):
		writeError(w, http.StatusNotFound, "process_not_found", "no such process")
	case errors.Is(err, procs.ErrProtected):
		writeError(w, http.StatusForbidden, "process_protected", "this process cannot be ended here")
	case errors.Is(err, procs.ErrChanged):
		writeError(w, http.StatusConflict, "process_changed", "the process id now names another process")
	case errors.Is(err, procs.ErrDisabled):
		writeError(w, http.StatusConflict, "dev_mode", err.Error())
	default:
		slog.Error("process signal", "pid", pid, "err", err)
		writeError(w, http.StatusInternalServerError, "process_signal_failed", "the signal could not be sent")
	}
}
