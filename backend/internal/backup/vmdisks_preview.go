package backup

import (
	"context"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// The ways a readable copy saves an image file of a VM (VMDiskPreview.Way).
const (
	VMDiskWayCopy     = "copy"             // the VM is shut off: copied like any file
	VMDiskWayBtrfs    = "btrfs_snapshot"   // read from a read-only snapshot of the volume
	VMDiskWaySnapshot = "libvirt_snapshot" // read while the guest writes to an overlay
	// VMDiskWayPause (see backup.go): read while the VM is paused.
	VMDiskWayAsIs = "as_is" // read while it changes; warned if it changed, the earlier copy then stays
	VMDiskWaySkip = "skip"  // left out on purpose by the job
	VMDiskWayNone = "none"  // not saved by a run now; Reason says why
)

// VMDiskInfo is an image file of a VM as the VM manager knows it.
type VMDiskInfo struct {
	VM        string
	File      string
	Format    string
	SizeBytes int64
	Running   bool
}

// VMDiskPlan is how the disks of a running VM could be held still now
// (vm.DiskPlan): Way is VMDiskWaySnapshot, VMDiskWayPause or "".
type VMDiskPlan struct {
	Way       string
	Agent     bool
	Reason    string
	NeedBytes uint64
	FreeBytes uint64
}

// VMDiskPreview is one image file of a VM in the sources of a job, with
// the way a run would save it right now. Codes and values only; the page
// has the words.
type VMDiskPreview struct {
	VM string `json:"vm"`
	// Path is the name in the copy ("<share or Volume-name>/<path>"), File
	// the file on the NAS.
	Path      string `json:"path"`
	File      string `json:"file"`
	Format    string `json:"format"`
	SizeBytes int64  `json:"sizeBytes"`
	// State is "running" or "stopped".
	State string `json:"state"`
	Way   string `json:"way"`
	// Agent: with VMDiskWaySnapshot, the guest agent answers and the
	// guest's file systems are frozen for the snapshot; without, the copy
	// is like the disk after a power cut.
	Agent bool `json:"agent"`
	// Reason is why no better way is used: a problem code (vm_disk_…).
	Reason string `json:"reason,omitempty"`
	// NeedBytes and FreeBytes belong to the reason vm_disk_no_space.
	NeedBytes uint64 `json:"needBytes,omitempty"`
	FreeBytes uint64 `json:"freeBytes,omitempty"`
}

// copyName tells whether a readable copy of the job holds file, and under
// which name; volume is the volume the file lies on. Nothing is read: the
// answer comes from the paths alone.
func (m *Manager) copyName(job Job, file string) (name, volume string, ok bool) {
	sources, names, folderOf, _ := m.copyEntries(job)
	for _, top := range names {
		source := sources[top]
		rel, err := filepath.Rel(source.Path, file)
		if err != nil || !filepath.IsLocal(rel) {
			continue
		}
		rel = filepath.ToSlash(rel)
		if folders := folderOf[top]; len(folders) > 0 && !slices.ContainsFunc(folders, func(folder string) bool {
			return folder == "." || rel == folder || strings.HasPrefix(rel, folder+"/")
		}) {
			continue
		}
		full := top + "/" + rel
		excluded := false
		for _, skip := range job.Skip {
			excluded = excluded || full == skip || strings.HasPrefix(full, skip+"/")
		}
		for _, part := range strings.Split(rel, "/") {
			for _, pattern := range job.Patterns {
				match, _ := path.Match(pattern, part)
				excluded = excluded || match
			}
			excluded = excluded || part == ".recycle" || part == ".titanos" || part == ".snapshots"
		}
		if excluded {
			continue
		}
		return full, source.Volume, true
	}
	return "", "", false
}

// PreviewVMDisks lists the image files of VMs a readable copy of job would
// meet, with the way a run would save each right now under the job's
// setting (job.VMDisks; nil: no choice yet). btrfs tells whether a volume
// has snapshots; plan is asked once per running VM and only where no Btrfs
// snapshot does. No file of a source is opened here.
func (m *Manager) PreviewVMDisks(ctx context.Context, job Job, disks []VMDiskInfo, btrfs func(volume string) bool, plan func(ctx context.Context, vm string, opts VMDiskOptions) VMDiskPlan) []VMDiskPreview {
	list := []VMDiskPreview{}
	plans := map[string]VMDiskPlan{}
	for _, disk := range disks {
		name, volume, ok := m.copyName(job, disk.File)
		if !ok {
			continue
		}
		entry := VMDiskPreview{VM: disk.VM, Path: name, File: disk.File, Format: disk.Format, SizeBytes: disk.SizeBytes, State: "stopped", Way: VMDiskWayCopy}
		if disk.Running {
			entry.State = "running"
			switch {
			case job.VMDisks != nil && job.VMDisks.Mode == VMDisksSkip:
				entry.Way = VMDiskWaySkip
			case volume != "" && btrfs != nil && btrfs(volume):
				entry.Way = VMDiskWayBtrfs
			case job.VMDisks == nil || plan == nil:
				// No choice yet: nothing that touches the VM.
				entry.Way = VMDiskWayAsIs
			default:
				p, asked := plans[disk.VM]
				if !asked {
					p = plan(ctx, disk.VM, job.VMDisks.options())
					plans[disk.VM] = p
				}
				entry.Agent, entry.Reason, entry.NeedBytes, entry.FreeBytes = p.Agent, p.Reason, p.NeedBytes, p.FreeBytes
				switch {
				case p.Way != "":
					entry.Way = p.Way
				case p.Reason == ProblemVMDiskRawImage || p.Reason == ProblemVMDiskSnapshotUnproven:
					// No snapshot and no leave to pause: left out, not read.
					entry.Way = VMDiskWaySkip
				default:
					entry.Way = VMDiskWayNone
				}
			}
		}
		list = append(list, entry)
	}
	slices.SortFunc(list, func(a, b VMDiskPreview) int { return strings.Compare(a.Path, b.Path) })
	return list
}
