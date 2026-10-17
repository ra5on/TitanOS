package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/backuptarget"
)

var backupTargetErrors = []struct {
	err    error
	status int
	code   string
}{
	{backuptarget.ErrDisabled, http.StatusConflict, "read_only"},
	{backuptarget.ErrNotLoaded, http.StatusServiceUnavailable, "config_unavailable"},
	{backuptarget.ErrInvalid, http.StatusBadRequest, "invalid_backup_target"},
	{backuptarget.ErrExists, http.StatusConflict, "backup_source_exists"},
	{backuptarget.ErrNotFound, http.StatusNotFound, "backup_source_not_found"},
	{backuptarget.ErrTooMany, http.StatusConflict, "too_many_backup_sources"},
	{backuptarget.ErrNoNetwork, http.StatusConflict, "backup_target_no_tailscale"},
}

func writeBackupTargetError(w http.ResponseWriter, err error) {
	for _, known := range backupTargetErrors {
		if errors.Is(err, known.err) {
			writeError(w, known.status, known.code, known.err.Error())
			return
		}
	}
	writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
}

func (s *Server) handleBackupTarget(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"target": s.opts.BackupTarget.View(), "volumes": s.opts.Storage.Mounted()})
}

func (s *Server) handleBackupTargetSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool
		Volume  string
	}
	if !readJSON(w, r, &req) {
		return
	}
	if s.opts.BackupTarget.Disabled {
		writeBackupTargetError(w, backuptarget.ErrDisabled)
		return
	}
	if err := s.opts.BackupTarget.Configure(req.Enabled, req.Volume, s.opts.Storage.Mounted()); err != nil {
		writeBackupTargetError(w, err)
		return
	}
	slog.Info("backup target switched", "enabled", req.Enabled, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleBackupTarget(w, r)
}

// handleBackupTargetPair adds a sending NAS and answers with the code to
// enter there. The code is not stored and cannot be shown again.
func (s *Server) handleBackupTargetPair(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name string }
	if !readJSON(w, r, &req) {
		return
	}
	if s.opts.BackupTarget.Disabled {
		writeBackupTargetError(w, backuptarget.ErrDisabled)
		return
	}
	code, err := s.opts.BackupTarget.Pair(req.Name)
	if err != nil {
		writeBackupTargetError(w, err)
		return
	}
	slog.Info("backup source paired", "name", req.Name, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusCreated, map[string]string{"code": code})
}

func (s *Server) handleBackupTargetRemove(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.BackupTarget.Remove(r.PathValue("name"), r.URL.Query().Get("deleteData") == "true"); err != nil {
		writeBackupTargetError(w, err)
		return
	}
	slog.Info("backup source removed", "name", r.PathValue("name"), "data", r.URL.Query().Get("deleteData") == "true", "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleBackupTarget(w, r)
}
