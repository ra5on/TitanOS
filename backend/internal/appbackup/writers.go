package appbackup

import (
	"context"
	"errors"
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/vm"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func writerOverlaps(writer string, paths []string) bool {
	for _, p := range paths {
		if within(writer, p) || within(p, writer) {
			return true
		}
	}
	return false
}
func vmWriterPaths(root string, machine vm.Status) []string {
	folder := machine.Directory
	if folder == "" {
		folder = machine.Name
	}
	paths := []string{filepath.Join(root, machine.Volume, "vms", folder)}
	for _, disk := range machine.Disks {
		p := disk.Path
		if p == "" {
			volume := disk.Volume
			if volume == "" {
				volume = machine.Volume
			}
			p = filepath.Join(root, volume, "vms", folder, disk.File)
		}
		paths = append(paths, p)
	}
	for _, folder := range machine.Folders {
		if !folder.ReadOnly {
			paths = append(paths, folder.Path)
		}
	}
	return paths
}
func (m *Manager) checkVMWriters(leases map[string]*vm.BackupLease, paths []string) error {
	for _, machine := range m.VMs.List(m.Mounted()) {
		if machine.State != "running" || leases[machine.Name] != nil {
			continue
		}
		for _, writer := range vmWriterPaths(m.MountRoot, machine) {
			if writerOverlaps(writer, paths) {
				return errors.New("another running VM writes a selected backup path")
			}
		}
	}
	return nil
}
func (m *Manager) validateApplicationPaths(job backup.Job, paths []string) error {
	for _, p := range paths {
		rel, err := filepath.Rel(m.MountRoot, p)
		parts := strings.Split(rel, string(filepath.Separator))
		if err != nil || len(parts) < 2 || strings.HasPrefix(rel, "..") || !slices.Contains(m.Mounted(), parts[0]) || (job.Destination.Type == "local" && parts[0] == job.Destination.Volume) {
			return errors.New("application data must be on a mounted source volume separate from the backup destination")
		}
		current := m.MountRoot
		for _, part := range parts {
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("application data path changed or is a symlink")
			}
		}
	}
	return nil
}

// The lease is held even for VM-only jobs: a disabled engine may still be stopping.
func (m *Manager) prepareDocker(ctx context.Context, applications []backup.Application, protected []string) (lease *docker.BackupLease, members []Member, paths []string, result error) {
	if m.Docker == nil {
		return nil, nil, nil, errors.New("Docker writer inventory unavailable")
	}
	lease, err := m.Docker.BackupLease(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	defer func() {
		if result != nil {
			lease.Close()
		}
	}()
	containers, err := lease.Containers(ctx)
	if err != nil {
		return lease, nil, nil, err
	}
	selected := map[string]bool{}
	for _, app := range applications {
		if app.Kind != "docker" {
			continue
		}
		i := slices.IndexFunc(containers, func(c docker.Container) bool { return c.Name == app.Name })
		if i < 0 {
			return lease, nil, nil, errors.New("selected container is unavailable")
		}
		c := containers[i]
		if c.State != "running" && c.State != "exited" && c.State != "created" {
			return lease, nil, nil, errors.New("container is not in a stable state")
		}
		if app.Method == "postgres-export" && c.State != "running" {
			return lease, nil, nil, errors.New("PostgreSQL export requires a running container")
		}
		selected[c.ID] = app.Method == "stop"
		members = append(members, Member{Kind: app.Kind, Name: app.Name, Identity: c.ID, Running: c.State == "running", Method: app.Method, Phase: "planned"})
		if app.Method == "stop" {
			mounts, err := lease.Mounts(ctx, c.ID)
			if err != nil {
				return lease, nil, nil, err
			}
			for _, mount := range mounts {
				if mount.Writable {
					paths = append(paths, mount.Source)
				}
			}
		}
	}
	protected = append(slices.Clone(protected), paths...)
	for _, c := range containers {
		if c.State != "running" || selected[c.ID] {
			continue
		}
		mounts, err := lease.Mounts(ctx, c.ID)
		if err != nil {
			return lease, nil, nil, err
		}
		for _, mount := range mounts {
			if mount.Writable && writerOverlaps(mount.Source, protected) {
				return lease, nil, nil, errors.New("another running container writes a selected backup path")
			}
		}
	}
	return lease, members, paths, nil
}
