package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/configbackup"
)

func writeBackupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, configbackup.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_backup_settings", err.Error())
	case errors.Is(err, configbackup.ErrNotLoaded):
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
	case errors.Is(err, configbackup.ErrDisabled):
		writeError(w, http.StatusConflict, "read_only", err.Error())
	case errors.Is(err, configbackup.ErrNoVolume):
		writeError(w, http.StatusConflict, "backup_no_volume", err.Error())
	case errors.Is(err, configbackup.ErrNotFound):
		writeError(w, http.StatusNotFound, "backup_not_found", err.Error())
	case errors.Is(err, configbackup.ErrNotABackup):
		writeError(w, http.StatusBadRequest, "not_a_backup", err.Error())
	case errors.Is(err, configbackup.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", err.Error())
	default:
		slog.Error("backup of the configuration failed", "err", err)
		writeError(w, http.StatusInternalServerError, "backup_failed", err.Error())
	}
}

func (s *Server) handleConfigBackup(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"backup":  s.opts.ConfigBackup.State(s.opts.Storage.Mounted()),
		"volumes": s.opts.Storage.Names(),
	})
}

func (s *Server) handleConfigBackupSettings(w http.ResponseWriter, r *http.Request) {
	var settings configbackup.Settings
	if !readJSON(w, r, &settings) {
		return
	}
	if err := s.opts.ConfigBackup.SetSettings(settings, s.opts.Storage.Names()); err != nil {
		writeBackupError(w, err)
		return
	}
	slog.Info("backup settings changed", "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleConfigBackup(w, r)
}

func (s *Server) handleConfigBackupRun(w http.ResponseWriter, r *http.Request) {
	if _, err := s.opts.ConfigBackup.Create(s.opts.Storage.Mounted()); err != nil {
		writeBackupError(w, err)
		return
	}
	s.handleConfigBackup(w, r)
}

// handleConfigBackupDownload hands out a backup, to keep a copy away from
// the NAS. It holds password hashes and every secret of the NAS, so the
// password has to be entered again first, as for a console.
func (s *Server) handleConfigBackupDownload(w http.ResponseWriter, r *http.Request) {
	if !s.confirmed(r) {
		writeError(w, http.StatusForbidden, "confirm_required", "enter your password again to download a backup")
		return
	}
	name := r.URL.Query().Get("name")
	file, err := s.opts.ConfigBackup.Open(s.opts.Storage.Mounted(), r.URL.Query().Get("volume"), name)
	if err != nil {
		writeBackupError(w, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeBackupError(w, err)
		return
	}
	slog.Info("backup of the configuration downloaded", "backup", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="titanos-`+name+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", time.Time{}, file)
}

// newStick reports whether this is a stick without accounts. Only then
// may a backup be put back, and it needs no login: there is nobody yet who
// could log in. A restore gives nobody access they did not have, it brings
// back the accounts of the backup.
func (s *Server) newStick(w http.ResponseWriter) bool {
	if !s.ensureUsersLoaded() {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "/config is not available, is the stick plugged in?")
		return false
	}
	if s.opts.Users.Count() > 0 {
		writeError(w, http.StatusConflict, "setup_done", "this NAS is set up already")
		return false
	}
	return true
}

// handleSetupBackups lists the backups found on the disks of this machine.
func (s *Server) handleSetupBackups(w http.ResponseWriter, r *http.Request) {
	if !s.newStick(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backups": s.opts.ConfigBackup.Find(r.Context())})
}

// restored finishes a restore: the accounts are there now, and the NAS
// starts again to take everything else from the stick.
func (s *Server) restored(w http.ResponseWriter, r *http.Request, source string) {
	// From here on the setup is closed, even before the restart.
	if err := s.opts.Users.Load(); err != nil {
		slog.Warn("accounts not readable after the restore", "err", err)
	}
	slog.Info("configuration restored, restarting", "from", source, "ip", remoteIP(r))
	w.WriteHeader(http.StatusAccepted)
	go func() {
		time.Sleep(time.Second)
		if err := s.opts.Power.Reboot(); err != nil {
			slog.Error("restart after the restore failed", "err", err)
		}
	}()
}

func (s *Server) handleSetupRestore(w http.ResponseWriter, r *http.Request) {
	var req struct{ Device, Name string }
	if !readJSON(w, r, &req) || !s.newStick(w) {
		return
	}
	if _, err := s.opts.ConfigBackup.RestoreFound(r.Context(), req.Device, req.Name); err != nil {
		writeBackupError(w, err)
		return
	}
	s.restored(w, r, req.Device+"/"+req.Name)
}

// handleSetupRestoreUpload takes a backup file as the body of the request.
func (s *Server) handleSetupRestoreUpload(w http.ResponseWriter, r *http.Request) {
	if !s.newStick(w) {
		return
	}
	if _, err := s.opts.ConfigBackup.RestoreUpload(r.Body); err != nil {
		writeBackupError(w, err)
		return
	}
	s.restored(w, r, "upload")
}
