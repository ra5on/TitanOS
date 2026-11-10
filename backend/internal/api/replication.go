package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/backuptarget"
	"github.com/ra5on/TitanOS/backend/internal/replication"
)

var replicationErrors = []struct {
	err    error
	status int
	code   string
}{
	{replication.ErrDisabled, http.StatusConflict, "read_only"},
	{replication.ErrNotLoaded, http.StatusServiceUnavailable, "config_unavailable"},
	{replication.ErrInvalid, http.StatusBadRequest, "invalid_replication"},
	{replication.ErrNotFound, http.StatusNotFound, "replication_not_found"},
	{replication.ErrBusy, http.StatusConflict, "replication_busy"},
	{replication.ErrNotBtrfs, http.StatusConflict, "replication_not_btrfs"},
}

func writeReplicationError(w http.ResponseWriter, err error) {
	for _, known := range replicationErrors {
		if errors.Is(err, known.err) {
			writeError(w, known.status, known.code, err.Error())
			return
		}
	}
	writeError(w, http.StatusInternalServerError, "replication_failed", err.Error())
}

// replicationArea sends Btrfs volumes to other TitanOS and holds what
// others sent here; an area with narrow dependencies (PLAN 8a, see
// fansArea). target knows the paired NAS.
type replicationArea struct {
	replication *replication.Manager
	receiver    *replication.Receiver
	target      *backuptarget.Manager
}

// replicationArea is the area of the server.
func (s *Server) replicationArea() replicationArea {
	return replicationArea{replication: s.opts.Replication, receiver: s.opts.ReplicaReceiver, target: s.opts.BackupTarget}
}

func (a replicationArea) register(mux *routeMux, g guards) {
	mux.HandleFunc("GET /api/v1/replication", g.admin(a.handleReplication))
	mux.HandleFunc("POST /api/v1/replication", g.admin(a.handleReplicationSave))
	mux.HandleFunc("PUT /api/v1/replication/{id}", g.admin(a.handleReplicationSave))
	mux.HandleFunc("DELETE /api/v1/replication/{id}", g.admin(a.handleReplicationRemove))
	mux.HandleFunc("POST /api/v1/replication/{id}/run", g.heavyAdmin(a.handleReplicationRun))
	mux.HandleFunc("POST /api/v1/replication/received/restore", g.heavyAdmin(a.handleReplicaRestore))
}

// handleReplication lists the jobs of this NAS and what it received from others.
func (a replicationArea) handleReplication(w http.ResponseWriter, r *http.Request) {
	received := []replication.Replica{}
	if volume := a.target.View().Volume; volume != "" && a.receiver != nil {
		received = a.receiver.List(volume)
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": a.replication.Jobs(), "received": received, "receiveVolume": a.target.View().Volume})
}

func (a replicationArea) handleReplicationSave(w http.ResponseWriter, r *http.Request) {
	var req replication.Request
	if !readJSON(w, r, &req) {
		return
	}
	id, err := a.replication.Save(r.PathValue("id"), req)
	if err != nil {
		writeReplicationError(w, err)
		return
	}
	noteTarget(r, req.Name)
	slog.Info("replication job saved", "id", id, "name", req.Name, "volume", req.Volume, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

func (a replicationArea) handleReplicationRemove(w http.ResponseWriter, r *http.Request) {
	if err := a.replication.Remove(r.PathValue("id")); err != nil {
		writeReplicationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a replicationArea) handleReplicationRun(w http.ResponseWriter, r *http.Request) {
	if err := a.replication.Start(r.Context(), r.PathValue("id")); err != nil {
		writeReplicationError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// handleReplicaRestore copies a received snapshot into a new folder.
func (a replicationArea) handleReplicaRestore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source   string `json:"source"`
		Snapshot string `json:"snapshot"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	volume := a.target.View().Volume
	if volume == "" || a.receiver == nil {
		writeReplicationError(w, replication.ErrNotFound)
		return
	}
	path, err := a.receiver.Restore(r.Context(), volume, req.Source, req.Snapshot)
	if err != nil {
		writeReplicationError(w, err)
		return
	}
	noteTarget(r, path)
	writeJSON(w, http.StatusCreated, map[string]string{"path": path})
}
