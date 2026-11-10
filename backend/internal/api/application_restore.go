package api

import (
	"net/http"
)

func (a backupsArea) handleApplicationRestore(w http.ResponseWriter, r *http.Request) {
	if a.backup == nil {
		featureUnavailable(w)
		return
	}
	var req struct{ Snapshot, Volume string }
	if !readJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	if r.PathValue("step") == "preview" {
		plan, err := a.backup.PreviewApplicationRestore(r.Context(), id, req.Snapshot, req.Volume)
		if err != nil {
			writeBackupJobError(w, err)
			return
		}
		writeJSON(w, 200, plan)
		return
	}
	if r.PathValue("step") != "restore" {
		writeError(w, 404, "not_found", "no such restore step")
		return
	}
	target, err := a.backup.RestoreApplication(r.Context(), id, req.Snapshot, req.Volume)
	if err != nil {
		writeBackupJobError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"target": target})
}
