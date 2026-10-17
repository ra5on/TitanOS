package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/backup"
)

var backupErrors = []struct {
	err    error
	status int
	code   string
}{
	{backup.ErrDisabled, http.StatusConflict, "read_only"},
	{backup.ErrNotLoaded, http.StatusServiceUnavailable, "config_unavailable"},
	{backup.ErrInvalid, http.StatusBadRequest, "invalid_backup_job"},
	{backup.ErrExists, http.StatusConflict, "backup_job_exists"},
	{backup.ErrNotFound, http.StatusNotFound, "backup_job_not_found"},
	{backup.ErrRunning, http.StatusConflict, "backup_job_running"},
	{backup.ErrFolder, http.StatusConflict, "folder_unreadable"},
	{backup.ErrSources, http.StatusConflict, "backup_no_sources"},
}

func writeBackupJobError(w http.ResponseWriter, err error) {
	for _, known := range backupErrors {
		if errors.Is(err, known.err) {
			writeError(w, known.status, known.code, err.Error())
			return
		}
	}
	// What restic said about the place the backup goes to.
	writeError(w, http.StatusBadGateway, "backup_job_failed", err.Error())
}

func (s *Server) handleBackups(w http.ResponseWriter, r *http.Request) {
	shares := s.opts.Backup.Sources()
	if shares == nil {
		shares = []backup.Source{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"jobs":    s.opts.Backup.List(),
		"shares":  shares,
		"volumes": s.opts.Storage.Mounted(),
	})
}

func (s *Server) handleBackupCreate(w http.ResponseWriter, r *http.Request) {
	var job backup.Job
	if !readJSON(w, r, &job) {
		return
	}
	if err := s.opts.Backup.Create(job, s.opts.Storage.Names()); err != nil {
		writeBackupJobError(w, err)
		return
	}
	slog.Info("backup job created", "job", job.ID, "kind", job.Destination.Type, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleBackups(w, r)
}

func (s *Server) handleBackupUpdate(w http.ResponseWriter, r *http.Request) {
	var job backup.Job
	if !readJSON(w, r, &job) {
		return
	}
	if err := s.opts.Backup.Update(r.PathValue("id"), job, s.opts.Storage.Names()); err != nil {
		writeBackupJobError(w, err)
		return
	}
	slog.Info("backup job changed", "job", r.PathValue("id"), "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleBackups(w, r)
}

// handleBackupDelete forgets a job. The backups it made stay where they are.
func (s *Server) handleBackupDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Backup.Delete(r.PathValue("id")); err != nil {
		writeBackupJobError(w, err)
		return
	}
	slog.Info("backup job removed", "job", r.PathValue("id"), "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleBackups(w, r)
}

func (s *Server) handleBackupAction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var err error
	switch r.PathValue("action") {
	case "run":
		err = s.opts.Backup.Start(id)
	case "stop":
		err = s.opts.Backup.Stop(id)
	default:
		writeError(w, http.StatusNotFound, "not_found", "no such action")
		return
	}
	if err != nil {
		writeBackupJobError(w, err)
		return
	}
	s.handleBackups(w, r)
}

func (s *Server) handleBackupSnapshots(w http.ResponseWriter, r *http.Request) {
	snapshots, err := s.opts.Backup.Snapshots(r.Context(), r.PathValue("id"))
	if err != nil {
		writeBackupJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": snapshots})
}

// handleBackupRestore copies a backup back into a new folder on a volume.
func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	var req struct{ Snapshot, Volume string }
	if !readJSON(w, r, &req) {
		return
	}
	target, err := s.opts.Backup.Restore(r.PathValue("id"), req.Snapshot, req.Volume)
	if err != nil {
		writeBackupJobError(w, err)
		return
	}
	slog.Info("restore started", "job", r.PathValue("id"), "snapshot", req.Snapshot, "to", target, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusAccepted, map[string]string{"target": target})
}

// handleBackupPassword shows the password a job's backups are encrypted
// with: whoever wants to read them without this NAS needs it. Like other
// secrets it takes the account's password once more.
func (s *Server) handleBackupPassword(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(s.cookieName())
	if err != nil || !s.sessions.Confirmed(cookie.Value) {
		writeError(w, http.StatusForbidden, "confirm_required", "enter your password again to see the password of the backup")
		return
	}
	password, err := s.opts.Backup.Password(r.PathValue("id"))
	if err != nil {
		writeBackupJobError(w, err)
		return
	}
	slog.Info("backup password shown", "job", r.PathValue("id"), "by", currentUser(r).Name, "ip", remoteIP(r))
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"password": password})
}

// handleBackupKey returns the SSH key this NAS shows to SFTP servers,
// making it first if there is none.
func (s *Server) handleBackupKey(w http.ResponseWriter, r *http.Request) {
	key, err := s.opts.Backup.PublicKey()
	if err != nil {
		writeBackupJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"publicKey": key})
}

// handleBackupFolders lists the folders in a folder of a share, for choosing
// what a backup saves.
func (s *Server) handleBackupFolders(w http.ResponseWriter, r *http.Request) {
	folders, err := s.opts.Backup.Folders(r.URL.Query().Get("share"), r.URL.Query().Get("dir"))
	if err != nil {
		writeBackupJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": folders})
}
