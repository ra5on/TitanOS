package api

import (
	"net/http"
)

func (s *Server) handleApplicationRestore(w http.ResponseWriter, r *http.Request) {
	if s.opts.Backup == nil {
		featureUnavailable(w)
		return
	}
	var req struct{ Snapshot, Volume string }
	if !readJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	if r.PathValue("step") == "preview" {
		plan, err := s.opts.Backup.PreviewApplicationRestore(r.Context(), id, req.Snapshot, req.Volume)
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
	target, err := s.opts.Backup.RestoreApplication(r.Context(), id, req.Snapshot, req.Volume)
	if err != nil {
		writeBackupJobError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"target": target})
}
