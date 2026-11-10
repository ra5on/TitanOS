package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/lxc"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/smarthistory"
	"github.com/ra5on/TitanOS/backend/internal/storage"
)

// storageErrors maps what the storage manager refuses to do onto API errors.
var storageErrors = []struct {
	err    error
	status int
	code   string
}{
	{storage.ErrNotLoaded, http.StatusServiceUnavailable, "config_unavailable"},
	{storage.ErrInvalidName, http.StatusBadRequest, "invalid_volume_name"},
	{storage.ErrExists, http.StatusConflict, "volume_exists"},
	{storage.ErrNotFound, http.StatusNotFound, "volume_not_found"},
	{storage.ErrInvalidLevel, http.StatusBadRequest, "invalid_level"},
	{storage.ErrInvalidFS, http.StatusBadRequest, "invalid_filesystem"},
	{storage.ErrInvalidIdleTime, http.StatusBadRequest, "invalid_disk_idle_time"},
	{storage.ErrUnknownSleepTarget, http.StatusBadRequest, "unknown_disk_sleep_target"},
	{storage.ErrTooFewDisks, http.StatusBadRequest, "too_few_disks"},
	{storage.ErrDiskUnavailable, http.StatusConflict, "disk_unavailable"},
	{storage.ErrTestRunning, http.StatusConflict, "disk_test_running"},
	{storage.ErrNoTest, http.StatusNotFound, "disk_test_not_found"},
	{storage.ErrNoRestore, http.StatusNotFound, "snapshot_restore_not_found"},
	{storage.ErrDiskTooSmall, http.StatusBadRequest, "disk_too_small"},
	{storage.ErrNotConfirmed, http.StatusBadRequest, "not_confirmed"},
	{storage.ErrBusy, http.StatusConflict, "volume_busy"},
	{storage.ErrLocked, http.StatusConflict, "snapshot_locked"},
	{storage.ErrCacheConfigured, http.StatusConflict, "volume_cache_configured"},
	{storage.ErrCacheRestart, http.StatusConflict, "volume_cache_restart"},
	{storage.ErrNotPossible, http.StatusConflict, "not_possible"},
	{storage.ErrReadOnly, http.StatusConflict, "read_only"},
	{storage.ErrNoDisk, http.StatusNotFound, "disk_not_found"},
	{storage.ErrVolumeLocked, http.StatusConflict, "volume_locked"},
	{storage.ErrWrongPassword, http.StatusBadRequest, "volume_wrong_password"},
	{storage.ErrTooManyAttempts, http.StatusTooManyRequests, "volume_password_attempts"},
	{storage.ErrWeakPassword, http.StatusBadRequest, "volume_weak_password"},
	{storage.ErrPasswordRequired, http.StatusBadRequest, "volume_password_required"},
	{storage.ErrLastPassword, http.StatusConflict, "volume_last_password"},
	{storage.ErrNotEncrypted, http.StatusConflict, "volume_not_encrypted"},
	{storage.ErrEncryptionMembers, http.StatusConflict, "volume_encryption_members"},
}

func writeStorageError(w http.ResponseWriter, r *http.Request, action string, err error) {
	for _, known := range storageErrors {
		if errors.Is(err, known.err) {
			writeError(w, known.status, known.code, err.Error())
			return
		}
	}
	// A system command failed half-way. Tell the administrator what it said;
	// they need it to judge the state of the disks.
	slog.Error("storage action failed", "action", action, "user", currentUser(r).Name, "err", err)
	writeError(w, http.StatusInternalServerError, "storage_failed", err.Error())
}

// storageArea is the volumes, disks, SMART, disk tests and snapshots; an
// area with narrow dependencies (PLAN 8a, see fansArea). smartHistory keeps
// the daily SMART values (nil: none), shares the quotas of a volume.
// webSession lets only a person at a web session through; addVolumeChecks
// adds the checks chosen for a new volume as maintenance jobs. Removing a
// volume (handleDeleteVolume) asks every part of the NAS that may still use
// it and belongs to volumeUseArea.
type storageArea struct {
	storage         *storage.Manager
	smartHistory    *smarthistory.Recorder
	shares          *shares.Manager
	webSession      webSessionFunc
	addVolumeChecks func(ctx context.Context, r *http.Request, req storage.CreateRequest, checks volumeChecks)
}

// storageArea is the area of the server.
func (s *Server) storageArea() storageArea {
	return storageArea{storage: s.opts.Storage, smartHistory: s.opts.SmartHistory, shares: s.opts.Shares,
		webSession: s.requireWebSession, addVolumeChecks: s.volumeUseArea().addVolumeChecks}
}

// registerOperations registers the workflows of the storage (import,
// growth, usage, restoring from snapshots), with the log of
// operationsRoutes.
func (a storageArea) registerOperations(admin, heavy func(route string, fn http.HandlerFunc)) {
	admin("GET /api/v1/storage/import", a.handleImportCandidates)
	heavy("POST /api/v1/storage/import/preview", a.handleImportPreview)
	heavy("POST /api/v1/storage/import", a.handleImport)
	admin("GET /api/v1/storage/growth", a.handleGrowth)
	heavy("POST /api/v1/storage/growth/recover", a.handleGrowthRecover)
	heavy("POST /api/v1/storage/growth/cancel", a.handleGrowthCancel)
	heavy("POST /api/v1/storage/volumes/{name}/growth/preview", a.handleGrowthPreview)
	heavy("POST /api/v1/storage/volumes/{name}/growth", a.handleGrowthStart)
	admin("GET /api/v1/storage/volumes/{name}/usage", a.handleVolumeUsage)
	admin("GET /api/v1/storage/volumes/{name}/snapshots/{snapshot}/files", a.handleSnapshotFiles)
	heavy("POST /api/v1/storage/volumes/{name}/snapshots/{snapshot}/files/restore", a.handleSnapshotFilesRestore)
	heavy("POST /api/v1/storage/volumes/{name}/snapshots/{snapshot}/files/preview", a.handleSnapshotRestorePreview)
	heavy("POST /api/v1/storage/volumes/{name}/snapshots/{snapshot}/folders/preview", a.handleSnapshotFolderPreview)
	heavy("POST /api/v1/storage/volumes/{name}/snapshots/{snapshot}/folders/restore", a.handleSnapshotFolderRestore)
	admin("GET /api/v1/storage/snapshot-restores", a.handleSnapshotRestores)
	admin("POST /api/v1/storage/snapshot-restores/{id}/cancel", a.handleSnapshotRestoreCancel)
	admin("DELETE /api/v1/storage/snapshot-restores/{id}", a.handleSnapshotRestoreForget)
}

func (a storageArea) register(mux *routeMux, g guards) {
	mux.HandleFunc("GET /api/v1/storage", g.user(a.handleStorage))
	mux.HandleFunc("GET /api/v1/storage/disk-sleep", g.admin(a.handleDiskSleep))
	mux.HandleFunc("PUT /api/v1/storage/disk-sleep", g.admin(a.handleSetDiskSleep))
	mux.HandleFunc("PUT /api/v1/storage/volumes/{name}/quotas", g.admin(a.handleSetShareQuotas))
	mux.HandleFunc("PUT /api/v1/storage/volumes/{name}/sync-speed", g.admin(a.handleSetSyncSpeed))
	mux.HandleFunc("POST /api/v1/storage/rescan", g.admin(a.handleStorageRescan))
	mux.HandleFunc("GET /api/v1/storage/smart", g.admin(a.handleSmart))
	mux.HandleFunc("POST /api/v1/storage/smart/refresh", g.admin(a.handleSmartRefresh))
	mux.HandleFunc("GET /api/v1/storage/smart/{disk}", g.admin(a.handleSmartDisk))
	mux.HandleFunc("GET /api/v1/storage/smart/{disk}/history", g.admin(a.handleSmartHistory))
	mux.HandleFunc("POST /api/v1/storage/volumes", g.heavyAdmin(a.handleCreateVolume))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/disks", g.heavyAdmin(a.handleAddVolumeDisk))
	mux.HandleFunc("DELETE /api/v1/storage/volumes/{name}/disks/{disk}", g.heavyAdmin(a.handleRemoveVolumeDisk))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/replace", g.heavyAdmin(a.handleReplaceVolumeDisk))
	mux.HandleFunc("POST /api/v1/storage/disks/{disk}/identify", g.admin(a.handleIdentifyDisk))
	mux.HandleFunc("PUT /api/v1/storage/volumes/{name}/profile", g.heavyAdmin(a.handleBtrfsProfile))
	mux.HandleFunc("PUT /api/v1/storage/volumes/{name}/cache", g.heavyAdmin(a.handleVolumeCache))
	mux.HandleFunc("DELETE /api/v1/storage/volumes/{name}/cache", g.heavyAdmin(a.handleVolumeCacheRemove))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/check", g.heavyAdmin(a.handleCheckVolume))
	mux.HandleFunc("GET /api/v1/storage/volumes/{name}/snapshots", g.admin(a.handleSnapshots))
	mux.HandleFunc("POST /api/v1/storage/volumes/{name}/snapshots", g.heavyAdmin(a.handleSnapshotAction))
	mux.HandleFunc("GET /api/v1/storage/tests", g.admin(a.handleDiskTests))
	mux.HandleFunc("POST /api/v1/storage/tests", g.heavyAdmin(a.handleDiskTestStart))
	mux.HandleFunc("DELETE /api/v1/storage/tests/{disk}", g.admin(a.handleDiskTestStop))
	mux.HandleFunc("POST /api/v1/storage/tests/{disk}/resume", g.heavyAdmin(a.handleDiskTestResume))
	mux.HandleFunc("DELETE /api/v1/storage/test-notices", g.admin(a.handleDiskTestNotices))
}

func (a storageArea) handleStorage(w http.ResponseWriter, r *http.Request) {
	status, err := a.storage.Status(r.Context())
	if err != nil {
		writeStorageError(w, r, "status", err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// handleDiskSleep returns the settings with volumes and disks and their
// effective values. Listing the disks (lsblk) wakes none of them.
func (a storageArea) handleDiskSleep(w http.ResponseWriter, r *http.Request) {
	status, err := a.storage.DiskSleepStatus(r.Context())
	if err != nil {
		// Without the disk list only the settings from RAM; clients then
		// must not send overrides (they would replace the stored ones).
		slog.Warn("disk sleep: listing disks failed", "err", err)
		status = a.storage.DiskSleep()
	}
	writeJSON(w, http.StatusOK, status)
}

// handleSetDiskSleep sets the default and, if given, replaces the overrides
// per volume and per disk. A missing "volumes" or "disks" keeps the stored
// ones, so clients that only know the default change nothing else.
func (a storageArea) handleSetDiskSleep(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IdleMinutes *int            `json:"idleMinutes"`
		Volumes     *map[string]int `json:"volumes"`
		Disks       *map[string]int `json:"disks"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.IdleMinutes == nil {
		writeError(w, http.StatusBadRequest, "invalid_disk_idle_time", "idleMinutes is required")
		return
	}
	settings, err := a.storage.UpdateDiskSleep(r.Context(), *req.IdleMinutes, req.Volumes, req.Disks)
	if err != nil {
		writeStorageError(w, r, "set disk sleep", err)
		return
	}
	slog.Info("disk sleep changed", "idleMinutes", settings.IdleMinutes, "volumes", len(settings.Volumes), "disks", len(settings.Disks), "user", currentUser(r).Name)
	a.handleDiskSleep(w, r)
}

// handleStorageRescan reads the SMART values again and returns the fresh state.
func (a storageArea) handleStorageRescan(w http.ResponseWriter, r *http.Request) {
	a.storage.RefreshSmart(r.Context())
	a.handleStorage(w, r)
}

// handleSmart lists the SMART state of all disks from the RAM cache. It runs
// no smartctl and so never wakes a disk.
func (a storageArea) handleSmart(w http.ResponseWriter, r *http.Request) {
	reports, err := a.storage.SmartReports(r.Context(), false)
	if err != nil {
		writeStorageError(w, r, "smart", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"disks": reports})
}

// handleSmartRefresh reads SMART of the awake disks again (at most every 30
// seconds); disks in standby are left asleep.
func (a storageArea) handleSmartRefresh(w http.ResponseWriter, r *http.Request) {
	a.storage.RefreshSmartSoon(r.Context())
	a.handleSmart(w, r)
}

// handleSmartDisk returns one disk with its full attribute table.
func (a storageArea) handleSmartDisk(w http.ResponseWriter, r *http.Request) {
	report, err := a.storage.SmartReport(r.Context(), r.PathValue("disk"))
	if err != nil {
		writeStorageError(w, r, "smart disk", err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// handleSmartHistory returns the daily SMART values kept for one disk.
func (a storageArea) handleSmartHistory(w http.ResponseWriter, r *http.Request) {
	report, err := a.storage.SmartReport(r.Context(), r.PathValue("disk"))
	if err != nil {
		writeStorageError(w, r, "smart history", err)
		return
	}
	history := smarthistory.History{Disk: report.Disk, Samples: []smarthistory.Sample{}}
	if a.smartHistory != nil {
		if history, err = a.smartHistory.History(report); err != nil {
			slog.Warn("SMART history not readable", "disk", report.Disk, "err", err)
			writeError(w, http.StatusServiceUnavailable, "smart_history_unavailable", "the SMART history could not be read")
			return
		}
	}
	writeJSON(w, http.StatusOK, history)
}

func (a storageArea) handleCreateVolume(w http.ResponseWriter, r *http.Request) {
	var body struct {
		storage.CreateRequest
		Checks volumeChecks `json:"checks"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	req := body.CreateRequest
	noteTarget(r, req.Name)
	if req.Encrypt && !a.webSession(w, r) {
		return
	}
	slog.Info("create volume requested", "name", req.Name, "level", req.Level, "disks", req.Disks, "encrypted", req.Encrypt, "user", currentUser(r).Name, "ip", remoteIP(r))
	if err := a.storage.Create(context.WithoutCancel(r.Context()), req); err != nil {
		writeStorageError(w, r, "create volume", err)
		return
	}
	a.addVolumeChecks(context.WithoutCancel(r.Context()), r, req, body.Checks)
	w.WriteHeader(http.StatusCreated)
}

func (a volumeUseArea) handleDeleteVolume(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm string `json:"confirm"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	if (a.files != nil && a.files.Busy()) || (a.backup != nil && a.backup.Busy()) || (a.vms != nil && a.vms.Busy()) || (a.media != nil && a.media.Working()) {
		writeError(w, 409, "volume_work_running", "wait for file, backup and VM operations")
		return
	}
	if a.monitoring != nil {
		cfg := a.monitoring.Status().Settings
		if cfg.Enabled && cfg.Volume == name {
			writeError(w, 409, "volume_has_monitoring", "disable monitoring on this volume first")
			return
		}
	}
	if a.maintenance() != nil {
		cfg := a.maintenance().Status().Settings
		if cfg.Enabled && cfg.Volume == name {
			writeError(w, 409, "volume_has_maintenance", "disable maintenance on this volume first")
			return
		}
	}
	if a.applications() != nil {
		op, _ := a.applications().Status()
		if op != nil && op.Volume == name {
			writeError(w, 409, "volume_has_recovery", "review the pending application backup first")
			return
		}
	}
	for _, share := range a.shares.List() {
		if share.Volume == name {
			writeError(w, http.StatusConflict, "volume_has_shares", "the volume still holds the share "+share.Name)
			return
		}
	}
	if docker := a.docker.Current(); docker.Enabled && docker.Volume == name {
		writeError(w, http.StatusConflict, "volume_has_docker", "Docker keeps its data on this volume")
		return
	}
	// Disks of VMs and installed plugins would go with the volume.
	for _, machine := range a.vms.List(nil) {
		for _, disk := range machine.Disks {
			if machine.Volume == name || disk.Volume == name {
				writeError(w, http.StatusConflict, "volume_has_vms", "the volume holds the VM "+machine.Name)
				return
			}
		}
		if machine.Volume == name {
			writeError(w, http.StatusConflict, "volume_has_vms", "the volume holds the VM "+machine.Name)
			return
		}
	}
	if a.plugins.Volume() == name {
		writeError(w, http.StatusConflict, "volume_has_plugins", "the plugins are installed on this volume")
		return
	}
	// A system container keeps its whole root filesystem on its volume. A
	// stopped one holds nothing open, so the volume would unmount and go,
	// and the container would stay registered without its system.
	if container, ok := lxcOnVolume(a.lxc.List(nil), name); ok {
		writeError(w, http.StatusConflict, "volume_has_lxc", "the volume holds the system container "+container)
		return
	}
	slog.Info("delete volume requested", "name", name, "user", currentUser(r).Name, "ip", remoteIP(r))
	if err := a.storage.Delete(context.WithoutCancel(r.Context()), name, req.Confirm); err != nil {
		writeStorageError(w, r, "delete volume", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// lxcOnVolume names a system container whose root filesystem lies on the
// volume.
func lxcOnVolume(containers []lxc.Status, volume string) (string, bool) {
	for _, c := range containers {
		if c.Volume == volume {
			return c.Name, true
		}
	}
	return "", false
}

func (a storageArea) handleAddVolumeDisk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Disk  string `json:"disk"`
		Erase bool   `json:"erase"`
		// Identity is the disk's identity from the disk list; required,
		// the disk is erased (storage.ErrIdentityRequired).
		Identity string `json:"identity"`
		// Profile converts a Btrfs volume while the disk is added
		// (single, raid1, raid1c3); empty keeps the profile.
		Profile string `json:"profile"`
		// Password of an encrypted Btrfs volume: the new disk gets its own
		// LUKS container with it. Only from a browser session.
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	password := []byte(req.Password)
	req.Password = ""
	defer wipeBytes(password)
	if len(password) > 0 && !a.webSession(w, r) {
		return
	}
	name := r.PathValue("name")
	slog.Info("add disk requested", "volume", name, "disk", req.Disk, "profile", req.Profile, "user", currentUser(r).Name, "ip", remoteIP(r))
	ctx := context.WithoutCancel(r.Context())
	var err error
	if req.Profile != "" {
		err = a.storage.AddBtrfsDiskWithPassword(ctx, name, req.Disk, req.Profile, req.Identity, req.Erase, password)
	} else {
		err = a.storage.AddDiskWithPassword(ctx, name, req.Disk, req.Erase, req.Identity, password)
	}
	if err != nil {
		writeCryptError(w, r, "add disk", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a storageArea) handleRemoveVolumeDisk(w http.ResponseWriter, r *http.Request) {
	name, disk := r.PathValue("name"), r.PathValue("disk")
	slog.Info("remove disk requested", "volume", name, "disk", disk, "user", currentUser(r).Name, "ip", remoteIP(r))
	if err := a.storage.RemoveDisk(context.WithoutCancel(r.Context()), name, disk); err != nil {
		writeStorageError(w, r, "remove disk", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a storageArea) handleCheckVolume(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Action != "start" && req.Action != "stop" {
		writeError(w, http.StatusBadRequest, "bad_request", `action must be "start" or "stop"`)
		return
	}
	if err := a.storage.Check(context.WithoutCancel(r.Context()), r.PathValue("name"), req.Action == "start"); err != nil {
		writeStorageError(w, r, "check volume", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDiskTests lists the tests of disks that run, have finished or wait
// to be continued after a restart, and the checkpoints that were dropped.
func (a storageArea) handleDiskTests(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tests": a.storage.Tests(), "notices": a.storage.TestNotices()})
}

// handleDiskTestStart begins the test of a disk. Overwriting it needs its
// name once more. autoResume lets a read test continue by itself after a
// restart.
func (a storageArea) handleDiskTestStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Disk, Mode, Confirm string
		AutoResume          bool `json:"autoResume"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.storage.StartTest(r.Context(), req.Disk, req.Mode, req.Confirm, req.AutoResume); err != nil {
		writeStorageError(w, r, "test disk", err)
		return
	}
	slog.Info("disk test requested", "disk", req.Disk, "mode", req.Mode, "autoResume", req.AutoResume, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleDiskTests(w, r)
}

// handleDiskTestResume continues a test that a restart stopped. A test
// that overwrites needs the name of the disk once more.
func (a storageArea) handleDiskTestResume(w http.ResponseWriter, r *http.Request) {
	var req struct{ Confirm string }
	if !readJSON(w, r, &req) {
		return
	}
	disk := r.PathValue("disk")
	if err := a.storage.ResumeTest(r.Context(), disk, req.Confirm); err != nil {
		writeStorageError(w, r, "continue disk test", err)
		return
	}
	slog.Info("disk test continued", "disk", disk, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleDiskTests(w, r)
}

// handleDiskTestNotices forgets the notices about dropped checkpoints.
func (a storageArea) handleDiskTestNotices(w http.ResponseWriter, r *http.Request) {
	a.storage.DismissTestNotices()
	a.handleDiskTests(w, r)
}

// handleDiskTestStop cancels a running test or dismisses a finished one.
func (a storageArea) handleDiskTestStop(w http.ResponseWriter, r *http.Request) {
	if err := a.storage.StopTest(r.PathValue("disk")); err != nil {
		writeStorageError(w, r, "stop disk test", err)
		return
	}
	a.handleDiskTests(w, r)
}

func (a storageArea) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	snapshots, err := a.storage.Snapshots(r.PathValue("name"))
	if err != nil {
		writeStorageError(w, r, "list snapshots", err)
		return
	}
	writeJSON(w, http.StatusOK, snapshots)
}
func (a storageArea) handleSnapshotAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Action  string `json:"action"`
		Confirm string `json:"confirm"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.storage.SnapshotAction(context.WithoutCancel(r.Context()), r.PathValue("name"), req.Name, req.Action, req.Confirm); err != nil {
		writeStorageError(w, r, "snapshot", err)
		return
	}
	slog.Info("Btrfs snapshot action", "volume", r.PathValue("name"), "snapshot", req.Name, "action", req.Action, "user", currentUser(r).Name)
	w.WriteHeader(http.StatusNoContent)
}

// handleSetShareQuotas switches limits for the shares of a volume on or off.
// They take effect at the next start of the NAS.
func (a storageArea) handleSetShareQuotas(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.storage.SetShareQuotas(r.PathValue("name"), req.Enabled); err != nil {
		writeStorageError(w, r, "share quotas", err)
		return
	}
	slog.Info("share quotas changed", "volume", r.PathValue("name"), "enabled", req.Enabled, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.shares.Kick()
	w.WriteHeader(http.StatusNoContent)
}

// handleSetSyncSpeed sets how fast the RAID array of a volume rebuilds:
// gentle, normal or fast (storage/syncspeed.go).
func (a storageArea) handleSetSyncSpeed(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Speed string `json:"speed"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !storage.ValidSyncSpeed(req.Speed) {
		writeError(w, http.StatusBadRequest, "invalid_request", "unknown rebuild speed")
		return
	}
	noteTarget(r, r.PathValue("name"))
	if err := a.storage.SetSyncSpeed(context.WithoutCancel(r.Context()), r.PathValue("name"), req.Speed); err != nil {
		writeStorageError(w, r, "rebuild speed", err)
		return
	}
	slog.Info("rebuild speed changed", "volume", r.PathValue("name"), "speed", req.Speed, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// handleBtrfsProfile changes the profile of a Btrfs volume in place.
func (a storageArea) handleBtrfsProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Profile string `json:"profile"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	slog.Info("Btrfs profile change requested", "volume", name, "profile", req.Profile, "user", currentUser(r).Name, "ip", remoteIP(r))
	if err := a.storage.ConvertBtrfs(context.WithoutCancel(r.Context()), name, req.Profile); err != nil {
		writeStorageError(w, r, "change Btrfs profile", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleVolumeCache switches the SSD cache of a volume on (disk, erase) or
// changes its mode (mode only, when it has one).
func (a storageArea) handleVolumeCache(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Disk     string `json:"disk"`
		Mode     string `json:"mode"`
		Erase    bool   `json:"erase"`
		Identity string `json:"identity"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	ctx := context.WithoutCancel(r.Context())
	var err error
	if req.Disk != "" {
		slog.Info("SSD cache requested", "volume", name, "disk", req.Disk, "mode", req.Mode, "user", currentUser(r).Name, "ip", remoteIP(r))
		err = a.storage.EnableCache(ctx, name, req.Disk, req.Mode, req.Identity, req.Erase)
	} else {
		err = a.storage.SetCacheMode(ctx, name, req.Mode)
	}
	if err != nil {
		writeStorageError(w, r, "SSD cache", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a storageArea) handleVolumeCacheRemove(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	slog.Info("SSD cache removal requested", "volume", name, "user", currentUser(r).Name, "ip", remoteIP(r))
	if err := a.storage.DisableCache(context.WithoutCancel(r.Context()), name); err != nil {
		writeStorageError(w, r, "SSD cache", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleReplaceVolumeDisk swaps a working disk of an mdadm volume for a new
// one without losing redundancy (assistant for swapping a disk).
func (a storageArea) handleReplaceVolumeDisk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Old      string `json:"old"`
		New      string `json:"new"`
		Erase    bool   `json:"erase"`
		Identity string `json:"identity"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	noteTarget(r, name+": "+req.Old+" → "+req.New)
	slog.Info("replace disk requested", "volume", name, "old", req.Old, "new", req.New, "user", currentUser(r).Name, "ip", remoteIP(r))
	if err := a.storage.ReplaceDisk(context.WithoutCancel(r.Context()), name, req.Old, req.New, req.Identity, req.Erase); err != nil {
		writeStorageError(w, r, "replace disk", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleIdentifyDisk makes the activity light of a disk flicker for a while.
func (a storageArea) handleIdentifyDisk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Seconds int `json:"seconds"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Seconds == 0 {
		req.Seconds = 30
	}
	if err := a.storage.IdentifyDisk(r.Context(), r.PathValue("disk"), req.Seconds); err != nil {
		writeStorageError(w, r, "identify disk", err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
