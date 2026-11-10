package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/backuptarget"
	"github.com/ra5on/TitanOS/backend/internal/storage"
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

// backupTargetArea takes in the backups of other TitanOS; an area with
// narrow dependencies (PLAN 8a, see fansArea). storage names the volumes.
type backupTargetArea struct {
	target  *backuptarget.Manager
	storage *storage.Manager
}

// backupTargetArea is the area of the server.
func (s *Server) backupTargetArea() backupTargetArea {
	return backupTargetArea{target: s.opts.BackupTarget, storage: s.opts.Storage}
}

// register takes backupJobOr of the backups: a job named "target" made
// before the name was reserved is still changed through this route.
func (a backupTargetArea) register(mux *routeMux, g guards, backupJobOr func(id string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/backups/target", g.admin(a.handleBackupTarget))
	mux.HandleFunc("PUT /api/v1/backups/target", g.admin(backupJobOr("target", a.handleBackupTargetSet)))
	mux.HandleFunc("POST /api/v1/backups/target/sources", g.admin(a.handleBackupTargetPair))
	mux.HandleFunc("DELETE /api/v1/backups/target/sources/{name}", g.admin(a.handleBackupTargetRemove))
}

func (a backupTargetArea) handleBackupTarget(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"target": a.target.View(), "volumes": a.storage.Mounted()})
}

func (a backupTargetArea) handleBackupTargetSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool
		Volume  string
	}
	if !readJSON(w, r, &req) {
		return
	}
	if a.target.Disabled {
		writeBackupTargetError(w, backuptarget.ErrDisabled)
		return
	}
	if err := a.target.Configure(req.Enabled, req.Volume, a.storage.Mounted()); err != nil {
		writeBackupTargetError(w, err)
		return
	}
	slog.Info("backup target switched", "enabled", req.Enabled, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleBackupTarget(w, r)
}

// handleBackupTargetPair adds a sending NAS and answers with the code to
// enter there. The code is not stored and cannot be shown again.
func (a backupTargetArea) handleBackupTargetPair(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name string }
	if !readJSON(w, r, &req) {
		return
	}
	if a.target.Disabled {
		writeBackupTargetError(w, backuptarget.ErrDisabled)
		return
	}
	code, err := a.target.Pair(req.Name)
	if err != nil {
		writeBackupTargetError(w, err)
		return
	}
	slog.Info("backup source paired", "name", req.Name, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusCreated, map[string]string{"code": code})
}

func (a backupTargetArea) handleBackupTargetRemove(w http.ResponseWriter, r *http.Request) {
	if err := a.target.Remove(r.PathValue("name"), r.URL.Query().Get("deleteData") == "true"); err != nil {
		writeBackupTargetError(w, err)
		return
	}
	slog.Info("backup source removed", "name", r.PathValue("name"), "data", r.URL.Query().Get("deleteData") == "true", "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleBackupTarget(w, r)
}
