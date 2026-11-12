package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/appbackup"
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/vm"
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

// backupsArea is the backups of shares; an area with narrow dependencies
// (PLAN 8a, see fansArea). storage names the volumes; docker, vms and shares
// say what a backup should cover. applications reads the coordinator of
// application backups when it is asked (tests replace it on a running
// server; nil: none). admitOperation admits a restore unless maintenance
// runs; sessionConfirmed says whether the password was entered again.
type backupsArea struct {
	backup           *backup.Manager
	storage          *storage.Manager
	docker           *docker.Manager
	vms              *vm.Manager
	shares           *shares.Manager
	applications     func() *appbackup.Manager
	admitOperation   admitFunc
	sessionConfirmed confirmedFunc
}

// backupsArea is the area of the server.
func (s *Server) backupsArea() backupsArea {
	return backupsArea{backup: s.opts.Backup, storage: s.opts.Storage, docker: s.opts.Docker, vms: s.opts.VMs, shares: s.opts.Shares,
		applications:   func() *appbackup.Manager { return s.opts.ApplicationBackups },
		admitOperation: s.admitOperation, sessionConfirmed: s.sessionConfirmed}
}

// registerOperations registers the routes that belong to the workflows of
// operationsRoutes, with its log.
func (a backupsArea) registerOperations(admin, heavy func(route string, fn http.HandlerFunc)) {
	heavy("POST /api/v1/backups/{id}/probe", a.handleBackupProbe)
	admin("GET /api/v1/backups/coverage", a.handleBackupCoverage)
	heavy("POST /api/v1/backups/{id}/applications/{step}", a.handleApplicationRestore)
	admin("GET /api/v1/backups/application-recovery", a.handleApplicationRecovery)
	admin("POST /api/v1/backups/application-recovery", a.handleApplicationRecover)
}

func (a backupsArea) register(mux *routeMux, g guards) {
	mux.HandleFunc("POST /api/v1/backups/smb/discover", g.admin(a.handleSMBDiscover))
	mux.HandleFunc("PUT /api/v1/backups/smb", g.admin(a.backupJobOr("smb", a.handleSMBSave)))
	mux.HandleFunc("DELETE /api/v1/backups/smb/{target}", g.admin(a.handleSMBDelete))
	mux.HandleFunc("GET /api/v1/backups/progress", g.admin(a.handleUSBBackupProgress))
	mux.HandleFunc("GET /api/v1/backups", g.admin(a.handleBackups))
	mux.HandleFunc("POST /api/v1/backups", g.admin(a.handleBackupCreate))
	mux.HandleFunc("GET /api/v1/backups/folders", g.admin(a.handleBackupFolders))
	mux.HandleFunc("GET /api/v1/backups/vm-disks", g.admin(a.handleBackupVMDisks))
	mux.HandleFunc("POST /api/v1/backups/ssh-key", g.admin(a.handleBackupKey))
	mux.HandleFunc("PUT /api/v1/backups/{id}", g.admin(a.handleBackupUpdate))
	mux.HandleFunc("DELETE /api/v1/backups/{id}", g.admin(a.handleBackupDelete))
	mux.HandleFunc("GET /api/v1/backups/{id}/snapshots", g.admin(a.handleBackupSnapshots))
	mux.HandleFunc("GET /api/v1/backups/{id}/password", g.admin(a.handleBackupPassword))
	mux.HandleFunc("POST /api/v1/backups/{id}/restore", g.heavyAdmin(a.handleBackupRestore))
	mux.HandleFunc("POST /api/v1/backups/{id}/check", g.heavyAdmin(a.handleBackupCheck))
	mux.HandleFunc("GET /api/v1/backups/{id}/snapshots/{snapshot}/files", g.admin(a.handleBackupFiles))
	mux.HandleFunc("POST /api/v1/backups/{id}/{action}", g.admin(a.handleBackupAction))
}

func (a backupsArea) handleBackups(w http.ResponseWriter, r *http.Request) {
	shares := a.backup.Sources()
	if shares == nil {
		shares = []backup.Source{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"jobs":       a.backup.List(),
		"smbTargets": a.backup.SMBTargets(),
		"shares":     shares,
		"volumes":    a.storage.Mounted(),
	})
}

func (a backupsArea) handleBackupCreate(w http.ResponseWriter, r *http.Request) {
	var job backup.Job
	if !readJSON(w, r, &job) {
		return
	}
	noteTarget(r, job.ID)
	if err := a.backup.Create(job, a.storage.Names()); err != nil {
		writeBackupJobError(w, err)
		return
	}
	slog.Info("backup job created", "job", job.ID, "kind", job.Destination.Type, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleBackups(w, r)
}

func (a backupsArea) handleBackupUpdate(w http.ResponseWriter, r *http.Request) {
	var job backup.Job
	if !readJSON(w, r, &job) {
		return
	}
	if err := a.backup.Update(r.PathValue("id"), job, a.storage.Names()); err != nil {
		writeBackupJobError(w, err)
		return
	}
	slog.Info("backup job changed", "job", r.PathValue("id"), "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleBackups(w, r)
}

// backupJobOr sends a PUT that changes a job to handleBackupUpdate. The fixed
// routes /backups/smb and /backups/target win over /backups/{id}; a job made
// with such a name before the names were reserved could otherwise not be
// changed, and its form would be taken for the settings of that route. A job
// always has a destination; neither of those bodies has one.
func (a backupsArea) backupJobOr(id string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body too large")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var probe map[string]json.RawMessage
		if json.Unmarshal(body, &probe) == nil {
			if _, ok := probe["destination"]; ok {
				r.SetPathValue("id", id)
				a.handleBackupUpdate(w, r)
				return
			}
		}
		next(w, r)
	}
}

// handleBackupDelete forgets a job. The backups it made stay where they are.
func (a backupsArea) handleBackupDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.backup.Delete(r.PathValue("id")); err != nil {
		writeBackupJobError(w, err)
		return
	}
	slog.Info("backup job removed", "job", r.PathValue("id"), "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleBackups(w, r)
}

func (a backupsArea) handleBackupAction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var err error
	switch r.PathValue("action") {
	case "run":
		leave, ok := a.admitOperation(w)
		if !ok {
			return
		}
		defer leave()
		err = a.backup.Start(id)
	case "stop":
		err = a.backup.Stop(id)
	default:
		writeError(w, http.StatusNotFound, "not_found", "no such action")
		return
	}
	if err != nil {
		writeBackupJobError(w, err)
		return
	}
	a.handleBackups(w, r)
}

func (a backupsArea) handleBackupSnapshots(w http.ResponseWriter, r *http.Request) {
	snapshots, err := a.backup.Snapshots(r.Context(), r.PathValue("id"))
	if err != nil {
		writeBackupJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": snapshots})
}

// handleBackupRestore copies a backup back into a new folder on a volume.
func (a backupsArea) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Snapshot, Volume string
		// Paths restores only these files and folders of the saved state.
		Paths []string
	}
	if !readJSON(w, r, &req) {
		return
	}
	var target string
	var err error
	if len(req.Paths) > 0 {
		target, err = a.backup.RestoreFiles(r.PathValue("id"), req.Snapshot, req.Volume, req.Paths)
	} else {
		target, err = a.backup.Restore(r.PathValue("id"), req.Snapshot, req.Volume)
	}
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
func (a backupsArea) handleBackupPassword(w http.ResponseWriter, r *http.Request) {
	if !a.sessionConfirmed(r) {
		writeError(w, http.StatusForbidden, "confirm_required", "enter your password again to see the password of the backup")
		return
	}
	password, err := a.backup.Password(r.PathValue("id"))
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
func (a backupsArea) handleBackupKey(w http.ResponseWriter, r *http.Request) {
	key, err := a.backup.PublicKey()
	if err != nil {
		writeBackupJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"publicKey": key})
}

// handleBackupFolders lists the folders in a folder of a share, or of a
// volume (?volume=) for folders outside the shares, for choosing what a
// backup saves.
func (a backupsArea) handleBackupFolders(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	var folders []string
	var err error
	if volume := query.Get("volume"); volume != "" {
		folders, err = a.backup.VolumeFolders(volume, query.Get("dir"))
	} else {
		folders, err = a.backup.Folders(query.Get("share"), query.Get("dir"))
	}
	if err != nil {
		writeBackupJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": folders})
}

func (a backupsArea) handleSMBDiscover(w http.ResponseWriter, r *http.Request) {
	var target backup.SMBTarget
	if !readJSON(w, r, &target) {
		return
	}
	shares, err := a.backup.SMBDiscover(r.Context(), target)
	if err != nil {
		writeBackupJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shares": shares})
}
func (a backupsArea) handleSMBSave(w http.ResponseWriter, r *http.Request) {
	var target backup.SMBTarget
	if !readJSON(w, r, &target) {
		return
	}
	if err := a.backup.SaveSMBTarget(r.Context(), target); err != nil {
		writeBackupJobError(w, err)
		return
	}
	a.handleBackups(w, r)
}
func (a backupsArea) handleSMBDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.backup.DeleteSMBTarget(r.PathValue("target")); err != nil {
		writeBackupJobError(w, err)
		return
	}
	a.handleBackups(w, r)
}

func (a backupsArea) handleUSBBackupProgress(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.backup.USBProgress())
}

// handleBackupCheck lets restic check a job's repository in the background.
func (a backupsArea) handleBackupCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ReadData int `json:"readData"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.backup.Check(r.PathValue("id"), req.ReadData); err != nil {
		writeBackupJobError(w, err)
		return
	}
	slog.Info("backup check started", "job", r.PathValue("id"), "readData", req.ReadData, "by", currentUser(r).Name)
	w.WriteHeader(http.StatusAccepted)
}

// handleBackupFiles lists one folder of a saved state.
func (a backupsArea) handleBackupFiles(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	entries, total, cut, err := a.backup.Files(ctx, r.PathValue("id"), r.PathValue("snapshot"), r.URL.Query().Get("path"), offset)
	if err != nil {
		writeBackupJobError(w, err)
		return
	}
	if entries == nil {
		entries = []backup.Entry{}
	}
	res := map[string]any{"entries": entries, "total": total, "cut": cut}
	if next := offset + len(entries); next < total {
		res["nextOffset"] = next
	}
	writeJSON(w, http.StatusOK, res)
}
