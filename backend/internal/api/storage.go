package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

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

func (s *Server) handleStorage(w http.ResponseWriter, r *http.Request) {
	status, err := s.opts.Storage.Status(r.Context())
	if err != nil {
		writeStorageError(w, r, "status", err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// handleDiskSleep returns the settings with volumes and disks and their
// effective values. Listing the disks (lsblk) wakes none of them.
func (s *Server) handleDiskSleep(w http.ResponseWriter, r *http.Request) {
	status, err := s.opts.Storage.DiskSleepStatus(r.Context())
	if err != nil {
		// Without the disk list only the settings from RAM; clients then
		// must not send overrides (they would replace the stored ones).
		slog.Warn("disk sleep: listing disks failed", "err", err)
		status = s.opts.Storage.DiskSleep()
	}
	writeJSON(w, http.StatusOK, status)
}

// handleSetDiskSleep sets the default and, if given, replaces the overrides
// per volume and per disk. A missing "volumes" or "disks" keeps the stored
// ones, so clients that only know the default change nothing else.
func (s *Server) handleSetDiskSleep(w http.ResponseWriter, r *http.Request) {
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
	settings, err := s.opts.Storage.UpdateDiskSleep(r.Context(), *req.IdleMinutes, req.Volumes, req.Disks)
	if err != nil {
		writeStorageError(w, r, "set disk sleep", err)
		return
	}
	slog.Info("disk sleep changed", "idleMinutes", settings.IdleMinutes, "volumes", len(settings.Volumes), "disks", len(settings.Disks), "user", currentUser(r).Name)
	s.handleDiskSleep(w, r)
}

// handleStorageRescan reads the SMART values again and returns the fresh state.
func (s *Server) handleStorageRescan(w http.ResponseWriter, r *http.Request) {
	s.opts.Storage.RefreshSmart(r.Context())
	s.handleStorage(w, r)
}

// handleSmart lists the SMART state of all disks from the RAM cache. It runs
// no smartctl and so never wakes a disk.
func (s *Server) handleSmart(w http.ResponseWriter, r *http.Request) {
	reports, err := s.opts.Storage.SmartReports(r.Context(), false)
	if err != nil {
		writeStorageError(w, r, "smart", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"disks": reports})
}

// handleSmartRefresh reads SMART of the awake disks again (at most every 30
// seconds); disks in standby are left asleep.
func (s *Server) handleSmartRefresh(w http.ResponseWriter, r *http.Request) {
	s.opts.Storage.RefreshSmartSoon(r.Context())
	s.handleSmart(w, r)
}

// handleSmartDisk returns one disk with its full attribute table.
func (s *Server) handleSmartDisk(w http.ResponseWriter, r *http.Request) {
	report, err := s.opts.Storage.SmartReport(r.Context(), r.PathValue("disk"))
	if err != nil {
		writeStorageError(w, r, "smart disk", err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleCreateVolume(w http.ResponseWriter, r *http.Request) {
	var body struct {
		storage.CreateRequest
		Checks volumeChecks `json:"checks"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	req := body.CreateRequest
	noteTarget(r, req.Name)
	if req.Encrypt && !s.requireWebSession(w, r) {
		return
	}
	slog.Info("create volume requested", "name", req.Name, "level", req.Level, "disks", req.Disks, "encrypted", req.Encrypt, "user", currentUser(r).Name, "ip", remoteIP(r))
	if err := s.opts.Storage.Create(context.WithoutCancel(r.Context()), req); err != nil {
		writeStorageError(w, r, "create volume", err)
		return
	}
	s.addVolumeChecks(context.WithoutCancel(r.Context()), r, req, body.Checks)
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleDeleteVolume(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm string `json:"confirm"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	if (s.opts.Files != nil && s.opts.Files.Busy()) || (s.opts.Backup != nil && s.opts.Backup.Busy()) || (s.opts.VMs != nil && s.opts.VMs.Busy()) || (s.opts.Media != nil && s.opts.Media.Working()) {
		writeError(w, 409, "volume_work_running", "wait for file, backup and VM operations")
		return
	}
	if s.opts.Monitoring != nil {
		cfg := s.opts.Monitoring.Status().Settings
		if cfg.Enabled && cfg.Volume == name {
			writeError(w, 409, "volume_has_monitoring", "disable monitoring on this volume first")
			return
		}
	}
	if s.opts.Maintenance != nil {
		cfg := s.opts.Maintenance.Status().Settings
		if cfg.Enabled && cfg.Volume == name {
			writeError(w, 409, "volume_has_maintenance", "disable maintenance on this volume first")
			return
		}
	}
	if s.opts.ApplicationBackups != nil {
		op, _ := s.opts.ApplicationBackups.Status()
		if op != nil && op.Volume == name {
			writeError(w, 409, "volume_has_recovery", "review the pending application backup first")
			return
		}
	}
	for _, share := range s.opts.Shares.List() {
		if share.Volume == name {
			writeError(w, http.StatusConflict, "volume_has_shares", "the volume still holds the share "+share.Name)
			return
		}
	}
	if docker := s.opts.Docker.Current(); docker.Enabled && docker.Volume == name {
		writeError(w, http.StatusConflict, "volume_has_docker", "Docker keeps its data on this volume")
		return
	}
	// Disks of VMs and installed plugins would go with the volume.
	for _, machine := range s.opts.VMs.List(nil) {
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
	if s.opts.Plugins.Volume() == name {
		writeError(w, http.StatusConflict, "volume_has_plugins", "the plugins are installed on this volume")
		return
	}
	slog.Info("delete volume requested", "name", name, "user", currentUser(r).Name, "ip", remoteIP(r))
	if err := s.opts.Storage.Delete(context.WithoutCancel(r.Context()), name, req.Confirm); err != nil {
		writeStorageError(w, r, "delete volume", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAddVolumeDisk(w http.ResponseWriter, r *http.Request) {
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
	if len(password) > 0 && !s.requireWebSession(w, r) {
		return
	}
	name := r.PathValue("name")
	slog.Info("add disk requested", "volume", name, "disk", req.Disk, "profile", req.Profile, "user", currentUser(r).Name, "ip", remoteIP(r))
	ctx := context.WithoutCancel(r.Context())
	var err error
	if req.Profile != "" {
		err = s.opts.Storage.AddBtrfsDiskWithPassword(ctx, name, req.Disk, req.Profile, req.Identity, req.Erase, password)
	} else {
		err = s.opts.Storage.AddDiskWithPassword(ctx, name, req.Disk, req.Erase, req.Identity, password)
	}
	if err != nil {
		writeCryptError(w, r, "add disk", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveVolumeDisk(w http.ResponseWriter, r *http.Request) {
	name, disk := r.PathValue("name"), r.PathValue("disk")
	slog.Info("remove disk requested", "volume", name, "disk", disk, "user", currentUser(r).Name, "ip", remoteIP(r))
	if err := s.opts.Storage.RemoveDisk(context.WithoutCancel(r.Context()), name, disk); err != nil {
		writeStorageError(w, r, "remove disk", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCheckVolume(w http.ResponseWriter, r *http.Request) {
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
	if err := s.opts.Storage.Check(r.Context(), r.PathValue("name"), req.Action == "start"); err != nil {
		writeStorageError(w, r, "check volume", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDiskTests lists the tests of disks that run, have finished or wait
// to be continued after a restart, and the checkpoints that were dropped.
func (s *Server) handleDiskTests(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tests": s.opts.Storage.Tests(), "notices": s.opts.Storage.TestNotices()})
}

// handleDiskTestStart begins the test of a disk. Overwriting it needs its
// name once more. autoResume lets a read test continue by itself after a
// restart.
func (s *Server) handleDiskTestStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Disk, Mode, Confirm string
		AutoResume          bool `json:"autoResume"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Storage.StartTest(r.Context(), req.Disk, req.Mode, req.Confirm, req.AutoResume); err != nil {
		writeStorageError(w, r, "test disk", err)
		return
	}
	slog.Info("disk test requested", "disk", req.Disk, "mode", req.Mode, "autoResume", req.AutoResume, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleDiskTests(w, r)
}

// handleDiskTestResume continues a test that a restart stopped. A test
// that overwrites needs the name of the disk once more.
func (s *Server) handleDiskTestResume(w http.ResponseWriter, r *http.Request) {
	var req struct{ Confirm string }
	if !readJSON(w, r, &req) {
		return
	}
	disk := r.PathValue("disk")
	if err := s.opts.Storage.ResumeTest(r.Context(), disk, req.Confirm); err != nil {
		writeStorageError(w, r, "continue disk test", err)
		return
	}
	slog.Info("disk test continued", "disk", disk, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleDiskTests(w, r)
}

// handleDiskTestNotices forgets the notices about dropped checkpoints.
func (s *Server) handleDiskTestNotices(w http.ResponseWriter, r *http.Request) {
	s.opts.Storage.DismissTestNotices()
	s.handleDiskTests(w, r)
}

// handleDiskTestStop cancels a running test or dismisses a finished one.
func (s *Server) handleDiskTestStop(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Storage.StopTest(r.PathValue("disk")); err != nil {
		writeStorageError(w, r, "stop disk test", err)
		return
	}
	s.handleDiskTests(w, r)
}

func (s *Server) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	snapshots, err := s.opts.Storage.Snapshots(r.PathValue("name"))
	if err != nil {
		writeStorageError(w, r, "list snapshots", err)
		return
	}
	writeJSON(w, http.StatusOK, snapshots)
}
func (s *Server) handleSnapshotAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Action  string `json:"action"`
		Confirm string `json:"confirm"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Storage.SnapshotAction(r.Context(), r.PathValue("name"), req.Name, req.Action, req.Confirm); err != nil {
		writeStorageError(w, r, "snapshot", err)
		return
	}
	slog.Info("Btrfs snapshot action", "volume", r.PathValue("name"), "snapshot", req.Name, "action", req.Action, "user", currentUser(r).Name)
	w.WriteHeader(http.StatusNoContent)
}

// handleSetShareQuotas switches limits for the shares of a volume on or off.
// They take effect at the next start of the NAS.
func (s *Server) handleSetShareQuotas(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Storage.SetShareQuotas(r.PathValue("name"), req.Enabled); err != nil {
		writeStorageError(w, r, "share quotas", err)
		return
	}
	slog.Info("share quotas changed", "volume", r.PathValue("name"), "enabled", req.Enabled, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.opts.Shares.Kick()
	w.WriteHeader(http.StatusNoContent)
}

// handleSetSyncSpeed sets how fast the RAID array of a volume rebuilds:
// gentle, normal or fast (storage/syncspeed.go).
func (s *Server) handleSetSyncSpeed(w http.ResponseWriter, r *http.Request) {
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
	if err := s.opts.Storage.SetSyncSpeed(r.Context(), r.PathValue("name"), req.Speed); err != nil {
		writeStorageError(w, r, "rebuild speed", err)
		return
	}
	slog.Info("rebuild speed changed", "volume", r.PathValue("name"), "speed", req.Speed, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// handleBtrfsProfile changes the profile of a Btrfs volume in place.
func (s *Server) handleBtrfsProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Profile string `json:"profile"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	slog.Info("Btrfs profile change requested", "volume", name, "profile", req.Profile, "user", currentUser(r).Name, "ip", remoteIP(r))
	if err := s.opts.Storage.ConvertBtrfs(context.WithoutCancel(r.Context()), name, req.Profile); err != nil {
		writeStorageError(w, r, "change Btrfs profile", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleVolumeCache switches the SSD cache of a volume on (disk, erase) or
// changes its mode (mode only, when it has one).
func (s *Server) handleVolumeCache(w http.ResponseWriter, r *http.Request) {
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
		err = s.opts.Storage.EnableCache(ctx, name, req.Disk, req.Mode, req.Identity, req.Erase)
	} else {
		err = s.opts.Storage.SetCacheMode(ctx, name, req.Mode)
	}
	if err != nil {
		writeStorageError(w, r, "SSD cache", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleVolumeCacheRemove(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	slog.Info("SSD cache removal requested", "volume", name, "user", currentUser(r).Name, "ip", remoteIP(r))
	if err := s.opts.Storage.DisableCache(context.WithoutCancel(r.Context()), name); err != nil {
		writeStorageError(w, r, "SSD cache", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleReplaceVolumeDisk swaps a working disk of an mdadm volume for a new
// one without losing redundancy (assistant for swapping a disk).
func (s *Server) handleReplaceVolumeDisk(w http.ResponseWriter, r *http.Request) {
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
	if err := s.opts.Storage.ReplaceDisk(context.WithoutCancel(r.Context()), name, req.Old, req.New, req.Identity, req.Erase); err != nil {
		writeStorageError(w, r, "replace disk", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleIdentifyDisk makes the activity light of a disk flicker for a while.
func (s *Server) handleIdentifyDisk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Seconds int `json:"seconds"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Seconds == 0 {
		req.Seconds = 30
	}
	if err := s.opts.Storage.IdentifyDisk(r.Context(), r.PathValue("disk"), req.Seconds); err != nil {
		writeStorageError(w, r, "identify disk", err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
