package backup

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
)

// frozenSources hands a plain copy the files that change while they are
// read (disks of running VMs, Manager.LiveFiles) from a read-only snapshot
// of their volume (Manager.SnapshotSource), made the first time such a file
// of a volume comes up and removed when the run ends. All disks of a volume
// therefore come from the same moment.
//
// Only these files are read from the snapshot. A Btrfs snapshot does not
// reach into other subvolumes below the volume (LXC containers, Docker's
// layers, older snapshots); copying everything from it would silently leave
// those out. A live file the snapshot does not hold is read as it is.
type frozenSources struct {
	m   *Manager
	ctx context.Context
	job Job
	// lives are the files running guests write to, with the name of their
	// VM ("" if unknown).
	lives map[string]string
	// disks are the image files of all VMs, running or not, with their VM:
	// asked afresh before such a file is opened (a VM may have been started
	// since the run began).
	disks map[string]string
	// roots holds the snapshot of each volume asked for; nil if none
	// could be made.
	roots    map[string]*os.Root
	releases []func()
}

func (m *Manager) frozenSources(ctx context.Context, job Job) *frozenSources {
	f := &frozenSources{m: m, ctx: ctx, job: job, lives: map[string]string{}, roots: map[string]*os.Root{}}
	if m.LiveFiles != nil {
		for _, file := range m.LiveFiles() {
			f.lives[filepath.Clean(file)] = ""
		}
	}
	if m.LiveDisks != nil {
		for file, vm := range m.LiveDisks() {
			f.lives[filepath.Clean(file)] = vm
		}
	}
	if m.VMDiskFiles != nil && m.VMRunning != nil {
		f.disks = map[string]string{}
		for file, vm := range m.VMDiskFiles() {
			f.disks[filepath.Clean(file)] = vm
		}
	}
	return f
}

// live tells whether a file of a source is one a running guest writes to.
//
// Not only what ran when the run began: the disk of a VM that was started
// since is live too, and is asked about right before it would be opened.
func (f *frozenSources) live(source Source, rel string) bool {
	file := filepath.Join(source.Path, filepath.FromSlash(rel))
	if _, live := f.lives[file]; live {
		return true
	}
	if vm, disk := f.disks[file]; disk && f.m.VMRunning(vm) {
		f.lives[file] = vm
		return true
	}
	return false
}

// vm is the VM a live file belongs to, "" if that is not known.
func (f *frozenSources) vm(source Source, rel string) string {
	return f.lives[filepath.Join(source.Path, filepath.FromSlash(rel))]
}

// file returns the snapshot of the source's volume and the path of the
// file in it; false if there is no snapshot or the file is not in it.
func (f *frozenSources) file(source Source, rel string) (*os.Root, string, bool) {
	if f.m.SnapshotSource == nil || source.Volume == "" {
		return nil, "", false
	}
	within, err := filepath.Rel(filepath.Join(f.m.mountRoot, source.Volume), filepath.Join(source.Path, filepath.FromSlash(rel)))
	if err != nil || !filepath.IsLocal(within) {
		return nil, "", false
	}
	root, asked := f.roots[source.Volume]
	if !asked {
		dir, release, err := f.m.SnapshotSource(f.ctx, source.Volume)
		if err != nil {
			slog.Info("backup: no snapshot for the disks of running VMs, they are read as they are", "job", f.job.ID, "volume", source.Volume, "reason", err)
		} else if root, err = os.OpenRoot(dir); err != nil {
			slog.Warn("backup: snapshot for the disks of running VMs not usable", "job", f.job.ID, "volume", source.Volume, "err", err)
			release()
			root = nil
		} else {
			slog.Info("backup: disks of running VMs are read from a snapshot", "job", f.job.ID, "volume", source.Volume)
			f.releases = append(f.releases, release)
		}
		f.roots[source.Volume] = root
	}
	if root == nil {
		return nil, "", false
	}
	if info, err := root.Lstat(within); err != nil || !info.Mode().IsRegular() {
		return nil, "", false
	}
	return root, filepath.ToSlash(within), true
}

// release closes and removes the snapshots of the run.
func (f *frozenSources) release() {
	for _, root := range f.roots {
		if root != nil {
			root.Close()
		}
	}
	for _, release := range f.releases {
		release()
	}
	f.roots, f.releases = map[string]*os.Root{}, nil
}
