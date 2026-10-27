package api

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/storage"
)

// Whole folders from a Btrfs snapshot: a preview that writes nothing, a
// background job, and its list, cancel and dismiss. Administrators only.

func writeSnapshotFolderError(w http.ResponseWriter, r *http.Request, action string, err error) {
	if errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusNotFound, "snapshot_path_not_found", err.Error())
		return
	}
	writeStorageError(w, r, action, err)
}

func (s *Server) handleSnapshotFolderPreview(w http.ResponseWriter, r *http.Request) {
	var req storage.SnapshotFolderRequest
	if !readJSON(w, r, &req) {
		return
	}
	out, err := s.opts.Storage.PreviewSnapshotFolder(r.Context(), r.PathValue("name"), r.PathValue("snapshot"), req)
	if err != nil {
		writeSnapshotFolderError(w, r, "snapshot folder preview", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSnapshotFolderRestore(w http.ResponseWriter, r *http.Request) {
	var req storage.SnapshotFolderRequest
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Path, req.Mode, req.Conflict)
	user := currentUser(r).Name
	job, err := s.opts.Storage.StartSnapshotFolderRestore(context.WithoutCancel(r.Context()), user, r.PathValue("name"), r.PathValue("snapshot"), req)
	if err != nil {
		writeSnapshotFolderError(w, r, "snapshot folder restore", err)
		return
	}
	slog.Info("snapshot folder restore requested", "id", job.ID, "volume", job.Volume, "snapshot", job.Snapshot, "path", job.Path, "mode", job.Mode, "user", user, "ip", remoteIP(r))
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleSnapshotRestores(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"restores": s.opts.Storage.SnapshotRestores()})
}

func (s *Server) handleSnapshotRestoreCancel(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Storage.CancelSnapshotRestore(r.PathValue("id")); err != nil {
		writeStorageError(w, r, "cancel snapshot restore", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSnapshotRestoreForget(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Storage.ForgetSnapshotRestore(r.PathValue("id")); err != nil {
		writeStorageError(w, r, "forget snapshot restore", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
