package api

import (
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/vm"
)

func (a vmsArea) handleVMSnapshots(w http.ResponseWriter, r *http.Request) {
	result, err := a.vms.SnapshotStatus(r.PathValue("name"), a.storage.Mounted())
	if err != nil {
		writeVMError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (a vmsArea) handleVMSnapshotCreate(w http.ResponseWriter, r *http.Request) {
	var req vm.SnapshotRequest
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	if err := a.vms.CreateSnapshot(r.Context(), name, req, a.storage.Mounted()); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM snapshot requested", "vm", name, "by", currentUser(r).Name)
	w.WriteHeader(http.StatusAccepted)
}
func (a vmsArea) handleVMSnapshotRestore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm string `json:"confirm"`
		Memory  bool   `json:"memory"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	if err := a.vms.RestoreSnapshot(r.Context(), name, r.PathValue("snapshot"), req.Confirm, req.Memory, a.storage.Mounted()); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM snapshot restore requested", "vm", name, "snapshot", r.PathValue("snapshot"), "by", currentUser(r).Name)
	w.WriteHeader(http.StatusAccepted)
}
func (a vmsArea) handleVMSnapshotDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm string `json:"confirm"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.vms.DeleteSnapshot(r.PathValue("name"), r.PathValue("snapshot"), req.Confirm, a.storage.Mounted()); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM snapshot deleted", "vm", r.PathValue("name"), "snapshot", r.PathValue("snapshot"), "by", currentUser(r).Name)
	w.WriteHeader(http.StatusNoContent)
}
func (a vmsArea) handleVMSnapshotSchedule(w http.ResponseWriter, r *http.Request) {
	var req vm.SnapshotSchedule
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.vms.SetSnapshotSchedule(r.PathValue("name"), req); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM snapshot schedule changed", "vm", r.PathValue("name"), "by", currentUser(r).Name)
	w.WriteHeader(http.StatusNoContent)
}
func (a vmsArea) handleVMSnapshotRecover(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm       string `json:"confirm"`
		DiscardMemory bool   `json:"discardMemory"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.vms.RecoverSnapshot(r.Context(), r.PathValue("name"), req.Confirm, req.DiscardMemory, a.storage.Mounted()); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM snapshot operation recovered", "vm", r.PathValue("name"), "discardMemory", req.DiscardMemory, "by", currentUser(r).Name)
	w.WriteHeader(http.StatusNoContent)
}

// handleVMBackupRecover is the way out for a VM that is held because the
// record of an interrupted backup of its running disks cannot be used
// (error vm_backup_recovery): the overlays a backup left next to its disks
// are committed back, each after a check, and the record is set aside
// (vm.RepairDiskBackup). Owners and administrators.
func (a vmsArea) handleVMBackupRecover(w http.ResponseWriter, r *http.Request) {
	if err := a.vms.RepairDiskBackup(r.Context(), r.PathValue("name")); err != nil {
		writeVMError(w, err)
		return
	}
	slog.Info("VM disks repaired after an interrupted backup", "vm", r.PathValue("name"), "by", currentUser(r).Name)
	w.WriteHeader(http.StatusNoContent)
}
