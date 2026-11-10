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

func (a storageArea) handleSnapshotFolderPreview(w http.ResponseWriter, r *http.Request) {
	var req storage.SnapshotFolderRequest
	if !readJSON(w, r, &req) {
		return
	}
	out, err := a.storage.PreviewSnapshotFolder(r.Context(), r.PathValue("name"), r.PathValue("snapshot"), req)
	if err != nil {
		writeSnapshotFolderError(w, r, "snapshot folder preview", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a storageArea) handleSnapshotFolderRestore(w http.ResponseWriter, r *http.Request) {
	var req storage.SnapshotFolderRequest
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Path, req.Mode, req.Conflict)
	user := currentUser(r).Name
	job, err := a.storage.StartSnapshotFolderRestore(context.WithoutCancel(r.Context()), user, r.PathValue("name"), r.PathValue("snapshot"), req)
	if err != nil {
		writeSnapshotFolderError(w, r, "snapshot folder restore", err)
		return
	}
	slog.Info("snapshot folder restore requested", "id", job.ID, "volume", job.Volume, "snapshot", job.Snapshot, "path", job.Path, "mode", job.Mode, "user", user, "ip", remoteIP(r))
	writeJSON(w, http.StatusAccepted, job)
}

func (a storageArea) handleSnapshotRestores(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"restores": a.storage.SnapshotRestores()})
}

func (a storageArea) handleSnapshotRestoreCancel(w http.ResponseWriter, r *http.Request) {
	if err := a.storage.CancelSnapshotRestore(r.PathValue("id")); err != nil {
		writeStorageError(w, r, "cancel snapshot restore", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a storageArea) handleSnapshotRestoreForget(w http.ResponseWriter, r *http.Request) {
	if err := a.storage.ForgetSnapshotRestore(r.PathValue("id")); err != nil {
		writeStorageError(w, r, "forget snapshot restore", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
