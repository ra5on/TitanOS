package api

import (
	"context"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/vm"
)

// vmDisksAnswer is what GET /api/v1/backups/vm-disks returns.
type vmDisksAnswer struct {
	// Choice is the setting the answer was made for: the stored one of
	// the job, or the one asked for with mode/raw; null if there is none.
	Choice *backup.VMDisks `json:"choice"`
	// ChoiceNeeded: the sources hold disks of running VMs and no choice
	// was made yet; the page asks the person.
	ChoiceNeeded bool                   `json:"choiceNeeded"`
	Disks        []backup.VMDiskPreview `json:"disks"`
}

// handleBackupVMDisks tells the job editor which image files of VMs a
// readable copy would meet and how a run would save each right now.
//
// The sources come from a stored job (?job=<id>) or from the editor
// (?source=<share>&volume=<volume>&place=<volume/folder>, each as often as
// needed). mode (consistent | skip) and raw (skip | pause) ask "what if"
// for a setting that is not stored yet; without them the stored one of
// the job counts. It reads no file of a source: the paths come from the
// configuration of the VMs. Only for a running VM on a volume without
// snapshots, and only with mode=consistent, libvirt and the header of its
// images are asked (the VM runs, its disks are awake).
func (a backupsArea) handleBackupVMDisks(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	job := backup.Job{ID: "preview", Sources: query["source"], Volumes: query["volume"], Places: query["place"], Destination: backup.Destination{Type: "local-copy"}}
	if id := query.Get("job"); id != "" {
		stored, ok := a.backup.Job(id)
		if !ok {
			writeBackupJobError(w, backup.ErrNotFound)
			return
		}
		job = stored
	}
	if mode := query.Get("mode"); mode != "" {
		job.VMDisks = &backup.VMDisks{Mode: mode, Raw: query.Get("raw"), LibvirtSnapshot: query.Get("libvirtSnapshot") == "true"}
	}
	if v := job.VMDisks; v != nil {
		known := v.Mode == backup.VMDisksSkip && v.Raw == "" && !v.LibvirtSnapshot ||
			v.Mode == backup.VMDisksConsistent && (v.Raw == "" || v.Raw == backup.VMDisksRawSkip || v.Raw == backup.VMDisksRawPause)
		if !known {
			writeError(w, http.StatusBadRequest, "invalid_backup_job", "unknown setting for disks of running VMs")
			return
		}
	}
	var disks []backup.VMDiskInfo
	if a.vms != nil {
		for _, disk := range a.vms.FileDisks() {
			disks = append(disks, backup.VMDiskInfo{VM: disk.VM, File: disk.Path, Format: disk.Format, SizeBytes: disk.SizeBytes, Running: disk.Running})
		}
	}
	btrfs := func(volume string) bool { return a.storage != nil && a.storage.FilesystemOf(volume) == "btrfs" }
	// Asked at most every half minute per VM, and bounded: the page may
	// ask with every change in the editor.
	plan := func(ctx context.Context, name string, opts backup.VMDiskOptions) backup.VMDiskPlan {
		p := a.vms.PlanDiskBackupCached(ctx, name, vm.DiskBackupOptions{Snapshot: opts.Snapshot, Pause: opts.Pause})
		return backup.VMDiskPlan{Way: p.Way, Agent: p.Agent, Reason: p.Reason, NeedBytes: p.NeedBytes, FreeBytes: p.FreeBytes}
	}
	answer := vmDisksAnswer{Choice: job.VMDisks, Disks: a.backup.PreviewVMDisks(r.Context(), job, disks, btrfs, plan)}
	for _, disk := range answer.Disks {
		answer.ChoiceNeeded = answer.ChoiceNeeded || job.VMDisks == nil && disk.State == "running"
	}
	writeJSON(w, http.StatusOK, answer)
}
