package api

import (
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/maintenance"
	"github.com/ra5on/TitanOS/backend/internal/monitoring"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/vm"
)

func (s *Server) operationsRoutes(mux *http.ServeMux) {
	register := func(route string, fn http.HandlerFunc, guard func(http.HandlerFunc) http.HandlerFunc) {
		mux.HandleFunc(route, guard(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				slog.Info("workflow change requested", "method", r.Method, "path", r.URL.Path, "user", currentUser(r).Name, "ip", remoteIP(r))
			}
			fn(w, r)
		}))
	}
	admin := func(route string, fn http.HandlerFunc) { register(route, fn, s.requireAdmin) }
	heavy := func(route string, fn http.HandlerFunc) { register(route, fn, s.requireHeavyAdmin) }
	admin("GET /api/v1/operations", s.handleOperations)
	admin("GET /api/v1/maintenance", s.handleMaintenance)
	admin("PUT /api/v1/maintenance", s.handleMaintenanceSet)
	admin("POST /api/v1/maintenance/{id}/{action}", s.handleMaintenanceAction)
	admin("GET /api/v1/monitoring", s.handleMonitoring)
	admin("PUT /api/v1/monitoring", s.handleMonitoringSet)
	admin("GET /api/v1/monitoring/history", s.handleMonitoringHistory)
	admin("GET /api/v1/monitoring/export", s.handleMonitoringExport)
	admin("GET /api/v1/monitoring/trends", s.handleCapacityTrends)
	admin("GET /api/v1/storage/import", s.handleImportCandidates)
	heavy("POST /api/v1/storage/import/preview", s.handleImportPreview)
	heavy("POST /api/v1/storage/import", s.handleImport)
	admin("GET /api/v1/storage/growth", s.handleGrowth)
	heavy("POST /api/v1/storage/growth/recover", s.handleGrowthRecover)
	heavy("POST /api/v1/storage/growth/cancel", s.handleGrowthCancel)
	heavy("POST /api/v1/storage/volumes/{name}/growth/preview", s.handleGrowthPreview)
	heavy("POST /api/v1/storage/volumes/{name}/growth", s.handleGrowthStart)
	admin("GET /api/v1/storage/volumes/{name}/usage", s.handleVolumeUsage)
	admin("GET /api/v1/storage/volumes/{name}/snapshots/{snapshot}/files", s.handleSnapshotFiles)
	heavy("POST /api/v1/storage/volumes/{name}/snapshots/{snapshot}/files/restore", s.handleSnapshotFilesRestore)
	heavy("POST /api/v1/storage/volumes/{name}/snapshots/{snapshot}/files/preview", s.handleSnapshotRestorePreview)
	heavy("POST /api/v1/backups/{id}/probe", s.handleBackupProbe)
	admin("GET /api/v1/backups/coverage", s.handleBackupCoverage)
	heavy("POST /api/v1/backups/{id}/applications/{step}", s.handleApplicationRestore)
	admin("GET /api/v1/backups/application-recovery", s.handleApplicationRecovery)
	admin("POST /api/v1/backups/application-recovery", s.handleApplicationRecover)
	heavy("POST /api/v1/vms/{name}/clone", s.handleVMClone)
	admin("GET /api/v1/vm-clones/{volume}", s.handleVMClones)
	heavy("POST /api/v1/vm-clones/{volume}/{id}", s.handleVMCloneRecover)
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
		if !req.Enabled {
			continue
		}
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
	if op == nil && !active && s.opts.ApplicationBackups.Busy() {
		featureError(w, errors.New("application recovery state could not be loaded; inspect the system log"))
		return
	}
	message := ""
	if r.URL.Query().Get("details") == "1" {
		review, _, err := s.opts.ApplicationBackups.Review()
		if err != nil {
			message = err.Error()
		} else {
			op = review
		}
	}
	writeJSON(w, 200, map[string]any{"operation": op, "active": active, "error": message})
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
	slog.Info("forget transfer requested", "id", r.PathValue("id"), "user", currentUser(r).Name, "ip", remoteIP(r))
	if err := s.opts.Files.ForgetTransfer(currentUser(r).Name, r.PathValue("id"), req.Confirm); err != nil {
		writeFileError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) handleOperations(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	out := map[string]any{"transfers": s.opts.Files.Jobs(user.Name), "uploads": s.opts.Files.UploadTasks(user.Name)}
	problems := []string{}
	if s.chats != nil {
		list, err := s.chats.List(user.Name, "")
		if err == nil {
			out["assistant"] = list
		} else {
			problems = append(problems, "assistant")
		}
	}
	if s.automations != nil && s.chats != nil && s.chats.Persistent() {
		list, err := s.automations.List(user.Name)
		if err == nil {
			type automationSummary struct {
				ID      string     `json:"id"`
				Name    string     `json:"name"`
				Enabled bool       `json:"enabled"`
				Pending bool       `json:"pending"`
				Detail  string     `json:"detail,omitempty"`
				Kind    string     `json:"kind"`
				Next    *time.Time `json:"next,omitempty"`
			}
			summaries := []automationSummary{}
			for _, a := range list {
				item := automationSummary{ID: a.ID, Name: a.Name, Enabled: a.Enabled, Pending: a.Pending, Detail: a.Detail, Kind: a.Trigger.Kind}
				if !a.Next.IsZero() {
					next := a.Next
					item.Next = &next
				}
				summaries = append(summaries, item)
			}
			out["automations"] = summaries
		} else {
			problems = append(problems, "automations")
		}
	}
	if s.opts.ApplicationBackups != nil {
		op, active := s.opts.ApplicationBackups.Status()
		out["applicationRecovery"] = map[string]any{"operation": op, "active": active}
		if op == nil && s.opts.ApplicationBackups.Busy() && !active {
			problems = append(problems, "application-recovery")
		}
	}
	if s.opts.VMs != nil && s.opts.Storage != nil {
		clones := []vm.CloneOperation{}
		mounted := s.opts.Storage.Mounted()
		for _, volume := range mounted {
			ops, err := s.opts.VMs.CloneOperations(volume, mounted)
			if err != nil {
				problems = append(problems, "vm-clones")
			} else {
				clones = append(clones, ops...)
			}
		}
		out["clones"] = clones
	}
	if s.opts.Maintenance != nil {
		out["maintenance"] = s.opts.Maintenance.Status()
	}
	if s.opts.Backup != nil {
		out["backups"] = s.opts.Backup.CachedViews()
	}
	if s.opts.Docker != nil {
		status := s.opts.Docker.Status(r.Context())
		out["docker"] = status.Jobs
		if status.Settings.Enabled && !status.Running {
			problems = append(problems, "docker")
		}
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
		out["storageActive"] = s.opts.Storage.ActionsRunning()
	}
	slices.Sort(problems)
	out["unavailable"] = slices.Compact(problems)
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

func (s *Server) handleSnapshotRestorePreview(w http.ResponseWriter, r *http.Request) {
	var req storage.SnapshotRestore
	if !readJSON(w, r, &req) {
		return
	}
	out, err := s.opts.Storage.PreviewSnapshotRestore(r.Context(), r.PathValue("name"), r.PathValue("snapshot"), req)
	if err != nil {
		writeStorageError(w, r, "snapshot restore preview", err)
		return
	}
	writeJSON(w, 200, out)
}
