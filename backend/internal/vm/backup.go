package vm

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type BackupLease struct {
	manager *Manager
	machine VM
	lock    *sync.Mutex
	Running bool
}

func (m *Manager) BackupLease(name string, mounted []string) (*BackupLease, error) {
	if m.Disabled {
		return nil, ErrDisabled
	}
	lock, err := m.operation(name)
	if err != nil {
		return nil, err
	}
	v, err := m.get(name)
	if err != nil {
		lock.Unlock()
		return nil, err
	}
	if !m.available(v, mounted) || m.pendingSnapshot(v) || m.snapshotReason(v) != "" {
		lock.Unlock()
		return nil, ErrSnapshotUnsupported
	}
	for _, d := range v.Disks {
		if d.Path != "" || d.Source != "" {
			lock.Unlock()
			return nil, ErrSnapshotUnsupported
		}
	}
	return &BackupLease{manager: m, machine: v, lock: lock, Running: m.pid(name) != 0}, nil
}

// RunningDiskFiles returns the image files running (or starting) VMs write
// to, as absolute paths: their disks that are files. Whole disks of the NAS
// handed to a VM are left out. A backup that copies such a file while the
// VM runs reads it from a snapshot where it can (backup.Manager.LiveFiles).
func (m *Manager) RunningDiskFiles() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var files []string
	if m.Disabled {
		return files
	}
	for name, vm := range m.vms {
		if !m.starting[name] && m.pid(name) == 0 {
			continue
		}
		for _, disk := range vm.Disks {
			if !m.isBlock(disk) {
				files = append(files, filepath.Clean(m.diskPath(vm, disk)))
			}
		}
	}
	sort.Strings(files)
	return files
}
func (l *BackupLease) Identity() string { return l.machine.UUID }
func (l *BackupLease) Paths() []string {
	out := []string{}
	for _, vol := range l.machine.volumes() {
		out = append(out, l.manager.dir(vol, l.machine.folder()))
	}
	return out
}
func (l *BackupLease) Stop(ctx context.Context) error {
	if !l.Running {
		return nil
	}
	m := l.manager
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if _, err := m.runner.Run(stopCtx, "", "virsh", "--quiet", "shutdown", l.machine.Name); err != nil {
		return err
	}
	for m.pid(l.machine.Name) != 0 {
		select {
		case <-stopCtx.Done():
			return errors.New("guest did not shut down cleanly within five minutes")
		case <-time.After(time.Second):
		}
	}
	return nil
}
func (l *BackupLease) Close() {
	if l.lock != nil {
		l.lock.Unlock()
		l.lock = nil
	}
}
func (l *BackupLease) Restart(ctx context.Context, mounted []string) error {
	l.Close()
	if !l.Running {
		return nil
	}
	v, err := l.manager.get(l.machine.Name)
	if err != nil || v.UUID != l.machine.UUID {
		return ErrNotFound
	}
	if l.manager.pid(v.Name) != 0 {
		return nil
	}
	return l.manager.Start(ctx, v.Name, mounted)
}
func (m *Manager) BackupIdentity(name string) string {
	v, err := m.get(name)
	if err != nil {
		return ""
	}
	return v.UUID
}
