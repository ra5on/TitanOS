package api

import (
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
	{storage.ErrTooFewDisks, http.StatusBadRequest, "too_few_disks"},
	{storage.ErrDiskUnavailable, http.StatusConflict, "disk_unavailable"},
	{storage.ErrDiskTooSmall, http.StatusBadRequest, "disk_too_small"},
	{storage.ErrNotConfirmed, http.StatusBadRequest, "not_confirmed"},
	{storage.ErrBusy, http.StatusConflict, "volume_busy"},
	{storage.ErrNotPossible, http.StatusConflict, "not_possible"},
	{storage.ErrReadOnly, http.StatusConflict, "read_only"},
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

// handleStorageRescan reads the SMART values again and returns the fresh state.
func (s *Server) handleStorageRescan(w http.ResponseWriter, r *http.Request) {
	s.opts.Storage.RefreshSmart(r.Context())
	s.handleStorage(w, r)
}

func (s *Server) handleCreateVolume(w http.ResponseWriter, r *http.Request) {
	var req storage.CreateRequest
	if !readJSON(w, r, &req) {
		return
	}
	slog.Info("create volume requested", "name", req.Name, "level", req.Level, "disks", req.Disks, "user", currentUser(r).Name, "ip", remoteIP(r))
	if err := s.opts.Storage.Create(r.Context(), req); err != nil {
		writeStorageError(w, r, "create volume", err)
		return
	}
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
	slog.Info("delete volume requested", "name", name, "user", currentUser(r).Name, "ip", remoteIP(r))
	if err := s.opts.Storage.Delete(r.Context(), name, req.Confirm); err != nil {
		writeStorageError(w, r, "delete volume", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAddVolumeDisk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Disk  string `json:"disk"`
		Erase bool   `json:"erase"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	slog.Info("add disk requested", "volume", name, "disk", req.Disk, "user", currentUser(r).Name, "ip", remoteIP(r))
	if err := s.opts.Storage.AddDisk(r.Context(), name, req.Disk, req.Erase); err != nil {
		writeStorageError(w, r, "add disk", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveVolumeDisk(w http.ResponseWriter, r *http.Request) {
	name, disk := r.PathValue("name"), r.PathValue("disk")
	slog.Info("remove disk requested", "volume", name, "disk", disk, "user", currentUser(r).Name, "ip", remoteIP(r))
	if err := s.opts.Storage.RemoveDisk(r.Context(), name, disk); err != nil {
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
