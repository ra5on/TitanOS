package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/maintenance"
	"github.com/ra5on/TitanOS/backend/internal/monitoring"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/vm"
)

func (s *Server) operationsRoutes(mux *http.ServeMux) {
	admin := func(route string, fn http.HandlerFunc) { mux.HandleFunc(route, s.requireAdmin(fn)) }
	admin("GET /api/v1/operations", s.handleOperations)
	admin("GET /api/v1/maintenance", s.handleMaintenance)
	admin("PUT /api/v1/maintenance", s.handleMaintenanceSet)
	admin("POST /api/v1/maintenance/{id}/{action}", s.handleMaintenanceAction)
	admin("GET /api/v1/monitoring", s.handleMonitoring)
	admin("PUT /api/v1/monitoring", s.handleMonitoringSet)
	admin("GET /api/v1/monitoring/history", s.handleMonitoringHistory)
	admin("GET /api/v1/storage/import", s.handleImportCandidates)
	admin("POST /api/v1/storage/import/preview", s.handleImportPreview)
	admin("POST /api/v1/storage/import", s.handleImport)
	admin("GET /api/v1/storage/growth", s.handleGrowth)
	admin("POST /api/v1/storage/growth/recover", s.handleGrowthRecover)
	admin("POST /api/v1/storage/growth/cancel", s.handleGrowthCancel)
	admin("POST /api/v1/storage/volumes/{name}/growth/preview", s.handleGrowthPreview)
	admin("POST /api/v1/storage/volumes/{name}/growth", s.handleGrowthStart)
	admin("GET /api/v1/storage/volumes/{name}/usage", s.handleVolumeUsage)
	admin("GET /api/v1/storage/volumes/{name}/snapshots/{snapshot}/files", s.handleSnapshotFiles)
	admin("POST /api/v1/storage/volumes/{name}/snapshots/{snapshot}/files/restore", s.handleSnapshotFilesRestore)
	admin("POST /api/v1/backups/{id}/probe", s.handleBackupProbe)
	admin("GET /api/v1/backups/application-recovery", s.handleApplicationRecovery)
	admin("POST /api/v1/backups/application-recovery", s.handleApplicationRecover)
	admin("POST /api/v1/vms/{name}/clone", s.handleVMClone)
	admin("GET /api/v1/vm-clones/{volume}", s.handleVMClones)
	admin("POST /api/v1/vm-clones/{volume}/{id}", s.handleVMCloneRecover)
	mux.HandleFunc("DELETE /api/v1/files/transfers/{id}", s.requireUser(s.handleTransferForget))
}
func featureError(w http.ResponseWriter, err error) {
	writeError(w, http.StatusConflict, "operation_failed", err.Error())
}
func featureUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "unavailable", "service unavailable")
}
func (s *Server) handleMaintenance(w http.ResponseWriter, r *http.Request) {
	if s.opts.Maintenance == nil {
		featureUnavailable(w)
		return
	}
	writeJSON(w, 200, s.opts.Maintenance.Status())
}
func (s *Server) handleMaintenanceSet(w http.ResponseWriter, r *http.Request) {
	if s.opts.Maintenance == nil {
		featureUnavailable(w)
		return
	}
	var req maintenance.Settings
	if !readJSON(w, r, &req) {
		return
	}
	for _, j := range req.Jobs {
		switch j.Kind {
		case "raid-check", "snapshot":
			status, err := s.opts.Storage.Status(r.Context())
			if err != nil {
				featureError(w, err)
				return
			}
			found := false
			for _, v := range status.Volumes {
				if v.Name == j.Target && (j.Kind != "snapshot" || v.Filesystem == "btrfs") {
					found = true
				}
			}
			if !found {
				featureError(w, errors.New("choose a compatible volume"))
				return
			}
		case "smart-long":
			status, err := s.opts.Storage.Status(r.Context())
			if err != nil {
				featureError(w, err)
				return
			}
			count := 0
			for _, d := range status.Disks {
				if d.Serial != "" && d.Serial == j.Target && d.Role != storage.RoleStick {
					count++
				}
			}
			if count != 1 {
				featureError(w, errors.New("choose a unique disk serial number"))
				return
			}
		case "backup-check", "restore-probe":
			found := false
			if s.opts.Backup != nil {
				for _, v := range s.opts.Backup.CachedViews() {
					if v.Job.ID == j.Target && v.Job.Destination.Type != "usb-copy" && v.Job.Destination.Type != "smb-copy" {
						found = true
					}
				}
			}
			if !found {
				featureError(w, errors.New("choose a versioned backup job"))
				return
			}
		}
	}
	if err := s.opts.Maintenance.Set(req); err != nil {
		featureError(w, err)
		return
	}
	s.handleMaintenance(w, r)
}
func (s *Server) handleMaintenanceAction(w http.ResponseWriter, r *http.Request) {
	if s.opts.Maintenance == nil {
		featureUnavailable(w)
		return
	}
	id := r.PathValue("id")
	var err error
	switch r.PathValue("action") {
	case "run":
		err = s.opts.Maintenance.Start(id)
	case "stop":
		err = s.opts.Maintenance.Stop(id)
	case "review":
		var req struct {
			Confirm string `json:"confirm"`
		}
		if !readJSON(w, r, &req) {
			return
		}
		err = s.opts.Maintenance.Acknowledge(id, req.Confirm)
	default:
		err = errors.New("unknown maintenance action")
	}
	if err != nil {
		featureError(w, err)
		return
	}
	s.handleMaintenance(w, r)
}
func (s *Server) handleMonitoring(w http.ResponseWriter, r *http.Request) {
	if s.opts.Monitoring == nil {
		featureUnavailable(w)
		return
	}
	writeJSON(w, 200, s.opts.Monitoring.Status())
}
func (s *Server) handleMonitoringSet(w http.ResponseWriter, r *http.Request) {
	if s.opts.Monitoring == nil {
		featureUnavailable(w)
		return
	}
	var req monitoring.Settings
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Monitoring.Set(req); err != nil {
		featureError(w, err)
		return
	}
	s.handleMonitoring(w, r)
}
func (s *Server) handleMonitoringHistory(w http.ResponseWriter, r *http.Request) {
	if s.opts.Monitoring == nil {
		featureUnavailable(w)
		return
	}
	hours, err := strconv.Atoi(r.URL.Query().Get("hours"))
	if err != nil {
		featureError(w, err)
		return
	}
	out, err := s.opts.Monitoring.History(r.Context(), hours)
	if err != nil {
		featureError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleImportCandidates(w http.ResponseWriter, r *http.Request) {
	out, err := s.opts.Storage.ImportCandidates(r.Context())
	if err != nil {
		writeStorageError(w, r, "import discovery", err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	var req storage.ImportRequest
	if !readJSON(w, r, &req) {
		return
	}
	out, err := s.opts.Storage.PreviewImport(r.Context(), req)
	if err != nil {
		writeStorageError(w, r, "import preview", err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var req storage.ImportRequest
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Storage.Import(r.Context(), req); err != nil {
		writeStorageError(w, r, "import registration", err)
		return
	}
	w.WriteHeader(201)
}
func (s *Server) handleGrowth(w http.ResponseWriter, r *http.Request) {
	g, pending, err := s.opts.Storage.GrowthStatus()
	message := ""
	if err != nil {
		message = err.Error()
	}
	writeJSON(w, 200, map[string]any{"pending": pending, "active": s.opts.Storage.GrowthActive(), "operation": g, "error": message})
}
func (s *Server) handleGrowthPreview(w http.ResponseWriter, r *http.Request) {
	var req storage.GrowthRequest
	if !readJSON(w, r, &req) {
		return
	}
	out, err := s.opts.Storage.GrowthPreview(r.Context(), r.PathValue("name"), req)
	if err != nil {
		writeStorageError(w, r, "growth preview", err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleGrowthStart(w http.ResponseWriter, r *http.Request) {
	var req storage.GrowthRequest
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Storage.StartGrowth(r.Context(), r.PathValue("name"), req); err != nil {
		writeStorageError(w, r, "growth start", err)
		return
	}
	w.WriteHeader(202)
}
func (s *Server) handleGrowthRecover(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm string `json:"confirm"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Storage.RecoverGrowth(r.Context(), req.Confirm); err != nil {
		writeStorageError(w, r, "growth recovery", err)
		return
	}
	w.WriteHeader(202)
}
func (s *Server) handleVolumeUsage(w http.ResponseWriter, r *http.Request) {
	out, err := s.opts.Storage.Usage(r.Context(), r.PathValue("name"))
	if err != nil {
		writeStorageError(w, r, "usage", err)
		return
	}
	if s.opts.Shares != nil {
		for i := range out.Entries {
			for _, share := range s.opts.Shares.List() {
				if share.Volume == r.PathValue("name") && share.Name == out.Entries[i].Name && out.Entries[i].Kind == "other" {
					out.Entries[i].Kind = "share"
				}
			}
		}
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleSnapshotFiles(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	out, err := s.opts.Storage.SnapshotFiles(r.Context(), r.PathValue("name"), r.PathValue("snapshot"), r.URL.Query().Get("path"), offset)
	if err != nil {
		writeStorageError(w, r, "snapshot browse", err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleSnapshotFilesRestore(w http.ResponseWriter, r *http.Request) {
	var req storage.SnapshotRestore
	if !readJSON(w, r, &req) {
		return
	}
	out, err := s.opts.Storage.RestoreSnapshotFiles(r.Context(), r.PathValue("name"), r.PathValue("snapshot"), req)
	if err != nil {
		writeStorageError(w, r, "snapshot restore", err)
		return
	}
	writeJSON(w, 201, out)
}
func (s *Server) handleBackupProbe(w http.ResponseWriter, r *http.Request) {
	if s.opts.Backup == nil {
		featureUnavailable(w)
		return
	}
	var req backup.ProbeRequest
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Backup.Probe(r.PathValue("id"), req); err != nil {
		writeBackupError(w, err)
		return
	}
	w.WriteHeader(202)
}
func (s *Server) handleApplicationRecovery(w http.ResponseWriter, r *http.Request) {
	if s.opts.ApplicationBackups == nil {
		featureUnavailable(w)
		return
	}
	op, active := s.opts.ApplicationBackups.Status()
	writeJSON(w, 200, map[string]any{"operation": op, "active": active})
}
func (s *Server) handleApplicationRecover(w http.ResponseWriter, r *http.Request) {
	if s.opts.ApplicationBackups == nil {
		featureUnavailable(w)
		return
	}
	var req struct {
		Confirm string `json:"confirm"`
		Restart bool   `json:"restart"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.ApplicationBackups.Recover(r.Context(), req.Confirm, req.Restart); err != nil {
		featureError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) handleVMClone(w http.ResponseWriter, r *http.Request) {
	var req vm.CloneRequest
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.VMs.Clone(r.Context(), r.PathValue("name"), req, s.opts.Storage.Mounted()); err != nil {
		writeVMError(w, err)
		return
	}
	w.WriteHeader(202)
}
func (s *Server) handleVMClones(w http.ResponseWriter, r *http.Request) {
	out, err := s.opts.VMs.CloneOperations(r.PathValue("volume"), s.opts.Storage.Mounted())
	if err != nil {
		writeVMError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleVMCloneRecover(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action  string `json:"action"`
		Confirm string `json:"confirm"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.VMs.RecoverClone(r.PathValue("volume"), r.PathValue("id"), req.Action, req.Confirm, s.opts.Storage.Mounted()); err != nil {
		writeVMError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) handleTransferForget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm string `json:"confirm"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Files.ForgetTransfer(currentUser(r).Name, r.PathValue("id"), req.Confirm); err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) handleOperations(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"transfers": s.opts.Files.Jobs(currentUser(r).Name)}
	if s.opts.Maintenance != nil {
		out["maintenance"] = s.opts.Maintenance.Status()
	}
	if s.opts.Backup != nil {
		out["backups"] = s.opts.Backup.CachedViews()
	}
	if s.opts.Docker != nil {
		out["docker"] = s.opts.Docker.Status(r.Context()).Jobs
	}
	if s.opts.VMs != nil {
		out["vms"] = s.opts.VMs.List(s.opts.Storage.Mounted())
	}
	if s.opts.Storage != nil {
		g, pending, err := s.opts.Storage.GrowthStatus()
		out["growth"] = map[string]any{"pending": pending, "operation": g}
		if err != nil {
			out["growthError"] = err.Error()
		}
		out["diskTests"] = s.opts.Storage.Tests()
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleGrowthCancel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm string `json:"confirm"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Storage.CancelGrowth(r.Context(), req.Confirm); err != nil {
		writeStorageError(w, r, "cancel growth preparation", err)
		return
	}
	w.WriteHeader(204)
}

func heavyOperation(r *http.Request) bool {
	if r.Method == http.MethodGet {
		return false
	}
	p := r.URL.Path
	if strings.HasPrefix(p, "/api/v1/storage/volumes") || p == "/api/v1/storage/import" || p == "/api/v1/storage/import/preview" || p == "/api/v1/storage/growth/recover" || p == "/api/v1/storage/growth/cancel" {
		return true
	}
	if strings.HasPrefix(p, "/api/v1/vms/") && strings.HasSuffix(p, "/clone") {
		return true
	}
	if strings.HasPrefix(p, "/api/v1/backups/") && (strings.HasSuffix(p, "/run") || strings.HasSuffix(p, "/check") || strings.HasSuffix(p, "/probe") || strings.HasSuffix(p, "/restore")) {
		return true
	}
	return false
}
