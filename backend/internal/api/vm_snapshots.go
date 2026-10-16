package api

import (
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/vm"
)

func (s *Server) handleVMSnapshots(w http.ResponseWriter, r *http.Request) {
	result, err := s.opts.VMs.SnapshotStatus(r.PathValue("name"), s.opts.Storage.Mounted())
	if err != nil {
		writeVMError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (s *Server) handleVMSnapshotCreate(w http.ResponseWriter, r *http.Request) {
	var req vm.SnapshotRequest
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	if err := s.opts.VMs.CreateSnapshot(r.Context(), name, req, s.opts.Storage.Mounted()); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM snapshot requested", "vm", name, "by", currentUser(r).Name)
	w.WriteHeader(http.StatusAccepted)
}
func (s *Server) handleVMSnapshotRestore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm string `json:"confirm"`
		Memory  bool   `json:"memory"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	if err := s.opts.VMs.RestoreSnapshot(r.Context(), name, r.PathValue("snapshot"), req.Confirm, req.Memory, s.opts.Storage.Mounted()); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM snapshot restore requested", "vm", name, "snapshot", r.PathValue("snapshot"), "by", currentUser(r).Name)
	w.WriteHeader(http.StatusAccepted)
}
func (s *Server) handleVMSnapshotDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm string `json:"confirm"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.VMs.DeleteSnapshot(r.PathValue("name"), r.PathValue("snapshot"), req.Confirm, s.opts.Storage.Mounted()); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM snapshot deleted", "vm", r.PathValue("name"), "snapshot", r.PathValue("snapshot"), "by", currentUser(r).Name)
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) handleVMSnapshotSchedule(w http.ResponseWriter, r *http.Request) {
	var req vm.SnapshotSchedule
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.VMs.SetSnapshotSchedule(r.PathValue("name"), req); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM snapshot schedule changed", "vm", r.PathValue("name"), "by", currentUser(r).Name)
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) handleVMSnapshotRecover(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm       string `json:"confirm"`
		DiscardMemory bool   `json:"discardMemory"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.VMs.RecoverSnapshot(r.Context(), r.PathValue("name"), req.Confirm, req.DiscardMemory, s.opts.Storage.Mounted()); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM snapshot operation recovered", "vm", r.PathValue("name"), "discardMemory", req.DiscardMemory, "by", currentUser(r).Name)
	w.WriteHeader(http.StatusNoContent)
}
