package api

import (
	"errors"
	"log/slog"
	"net/http"

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

// handleReplication lists the jobs of this NAS and what it received from others.
func (s *Server) handleReplication(w http.ResponseWriter, r *http.Request) {
	received := []replication.Replica{}
	if volume := s.opts.BackupTarget.View().Volume; volume != "" && s.opts.ReplicaReceiver != nil {
		received = s.opts.ReplicaReceiver.List(volume)
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": s.opts.Replication.Jobs(), "received": received, "receiveVolume": s.opts.BackupTarget.View().Volume})
}

func (s *Server) handleReplicationSave(w http.ResponseWriter, r *http.Request) {
	var req replication.Request
	if !readJSON(w, r, &req) {
		return
	}
	id, err := s.opts.Replication.Save(r.PathValue("id"), req)
	if err != nil {
		writeReplicationError(w, err)
		return
	}
	noteTarget(r, req.Name)
	slog.Info("replication job saved", "id", id, "name", req.Name, "volume", req.Volume, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

func (s *Server) handleReplicationRemove(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Replication.Remove(r.PathValue("id")); err != nil {
		writeReplicationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReplicationRun(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Replication.Start(r.Context(), r.PathValue("id")); err != nil {
		writeReplicationError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// handleReplicaRestore copies a received snapshot into a new folder.
func (s *Server) handleReplicaRestore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source   string `json:"source"`
		Snapshot string `json:"snapshot"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	volume := s.opts.BackupTarget.View().Volume
	if volume == "" || s.opts.ReplicaReceiver == nil {
		writeReplicationError(w, replication.ErrNotFound)
		return
	}
	path, err := s.opts.ReplicaReceiver.Restore(r.Context(), volume, req.Source, req.Snapshot)
	if err != nil {
		writeReplicationError(w, err)
		return
	}
	noteTarget(r, path)
	writeJSON(w, http.StatusCreated, map[string]string{"path": path})
}
