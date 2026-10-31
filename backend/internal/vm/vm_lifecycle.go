package vm

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Start boots a VM.
func (m *Manager) Start(ctx context.Context, name string, mounted []string) error {
	if m.off.Load() {
		return ErrServiceOff
	}
	if m.NetworkLease != nil {
		release, err := m.NetworkLease()
		if err != nil {
			return err
		}
		defer release()
	}
	lock, lockErr := m.operation(name)
	if lockErr != nil {
		return lockErr
	}
	defer lock.Unlock()
	if v, e := m.get(name); e == nil && m.pendingSnapshot(v) {
		return ErrSnapshotRecovery
	}
	if m.Disabled {
		return ErrDisabled
	}
	vm, err := m.get(name)
	if err != nil {
		return err
	}
	if !m.HasKVM() {
		return ErrNoKVM
	}
	if m.pid(name) != 0 {
		return ErrRunning
	}
	if !m.available(vm, mounted) {
		return ErrUnknownVolume
	}
	m.mu.Lock()
	job := m.jobs[name]
	m.mu.Unlock()
	if job != nil {
		return ErrPreparing
	}
	if vm.Network == "bridge" {
		if m.BridgeReady != nil && !m.BridgeReady() {
			return ErrNoBridge
		}
		if _, err := os.Stat(filepath.Join(m.sysDir, "class/net", bridge)); err != nil {
			return ErrNoBridge
		}
	}
	if strings.HasPrefix(vm.Network, "network:") {
		dev, ok := m.namedBridge(vm.Network)
		if !ok {
			return ErrNoBridge
		}
		if _, err := os.Stat(filepath.Join(m.sysDir, "class/net", dev)); err != nil {
			return ErrNoBridge
		}
	}
	if vm.ISO != "" {
		if _, err := m.checkISO(vm.ISO); err != nil {
			return err
		}
	}
	if vm.VirtIOISO != "" {
		if _, err := m.checkISO(vm.VirtIOISO); err != nil {
			return err
		}
	}
	// Everything the VM was given may have changed since: a folder
	// removed, a disk put into a volume.
	if err := m.checkFolders(vm.Folders); err != nil {
		return err
	}
	for i := range vm.Disks {
		if vm.Disks[i].Path != "" {
			disk := vm.Disks[i]
			if err := m.checkExisting(ctx, &disk, mounted); err != nil {
				return err
			}
		}
	}

	// The firmware keeps its settings (boot order, …) in a file of its
	// own. QEMU must be able to write it, so it is made here, not by libvirt.
	dir, err := m.makeDir(vm.Volume, vm.folder())
	if err != nil {
		return err
	}
	if err := m.prepareNVRAM(vm); err != nil {
		return err
	}
	if err := os.MkdirAll(m.runDir, 0o770); err != nil {
		return err
	}
	if err := os.Chown(m.runDir, -1, m.runGID); err != nil {
		return err
	}
	if err := os.Chmod(m.runDir, 0o770); err != nil {
		return err
	}
	os.Remove(m.VNCSocket(name))
	os.Remove(m.SerialSocket(name))
	m.mu.Lock()
	m.starting[name] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.starting, name)
		m.mu.Unlock()
	}()
	started := false
	if m.BeforeStart != nil {
		finish, err := m.BeforeStart(ctx, vm.Name, vm.PCI, vm.USB, vm.AutoGPU)
		if err != nil {
			return err
		}
		defer func() { finish(started) }()
	}
	if err := m.lend(vm); err != nil {
		return err
	}
	if vm.TPM {
		if err := m.prepareTPM(dir); err != nil {
			return err
		}
	}

	description, err := m.domainXML(vm)
	if err != nil {
		return err
	}
	file := filepath.Join(m.runDir, name+".xml")
	if err := os.WriteFile(file, []byte(description), 0o600); err != nil {
		return err
	}
	if _, err := m.runner.Run(ctx, "", "virsh", "--quiet", "create", "--file", file); err != nil {
		m.mu.Lock()
		delete(m.starting, name)
		m.release()
		m.mu.Unlock()
		return err
	}
	started = true
	slog.Info("VM started", "vm", name)
	m.reserve(ctx)
	return nil
}

// lookupGroup returns the number of a group, or fallback if the system has none of that name.
func lookupGroup(name string, fallback int) int {
	if group, err := user.LookupGroup(name); err == nil {
		if gid, err := strconv.Atoi(group.Gid); err == nil {
			return gid
		}
	}
	return fallback
}

// qemuUser returns the user QEMU runs as, or root where there is none.
func qemuUser() int {
	if account, err := user.Lookup("libvirt-qemu"); err == nil {
		if uid, err := strconv.Atoi(account.Uid); err == nil {
			return uid
		}
	}
	return 0
}

// qemuGroup returns the group QEMU runs in.
func qemuGroup(fallback int) int { return lookupGroup("kvm", fallback) }

func copyFile(from, to string) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o660)
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		target.Close()
		os.Remove(to)
		return err
	}
	return target.Close()
}

// Action asks a running VM to shut down or restart, or switches it off.
func (m *Manager) Action(ctx context.Context, name, action string) error {
	lock, lockErr := m.operation(name)
	if lockErr != nil {
		return lockErr
	}
	defer lock.Unlock()
	if v, e := m.get(name); e == nil && m.pendingSnapshot(v) {
		return ErrSnapshotRecovery
	}
	if m.Disabled {
		return ErrDisabled
	}
	if _, err := m.get(name); err != nil {
		return err
	}
	// shutdown and reboot ask the guest; destroy is pulling the plug.
	command, known := map[string]string{"shutdown": "shutdown", "reboot": "reboot", "poweroff": "destroy", "resume": "resume"}[action]
	if !known {
		return ErrNotFound
	}
	if m.pid(name) == 0 {
		return ErrNotRunning
	}
	_, err := m.runner.Run(ctx, "", "virsh", "--quiet", command, name)
	return err
}

// Delete forgets a VM. Its disks stay unless deleteDisks is set, which
// needs the name of the VM once more as confirmation.
func (m *Manager) Delete(name string, deleteDisks bool, confirm string) error {
	lock, lockErr := m.operation(name)
	if lockErr != nil {
		return lockErr
	}
	defer lock.Unlock()
	if v, e := m.get(name); e == nil && m.pendingSnapshot(v) {
		return ErrSnapshotRecovery
	}
	if m.Disabled {
		return ErrDisabled
	}
	vm, err := m.get(name)
	if err != nil {
		return err
	}
	entries, readErr := os.ReadDir(m.snapshotRoot(vm))
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		return readErr
	}
	for _, entry := range entries {
		if entry.IsDir() && snapshotID.MatchString(entry.Name()) {
			return ErrSnapshotExists
		}
	}
	if m.pid(name) != 0 {
		return ErrRunning
	}
	m.mu.Lock()
	job := m.jobs[name]
	m.mu.Unlock()
	if job != nil && job.State == "running" {
		return ErrPreparing
	}
	if deleteDisks && confirm != name && confirm != vm.Label() {
		return ErrNotConfirmed
	}
	if err := m.store.Remove(configDir + "/" + name + ".json"); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.vms, name)
	delete(m.autostarted, name)
	delete(m.jobs, name)
	m.mu.Unlock()
	m.resetPower(name, time.Now())
	os.Remove(filepath.Join(m.runDir, name+".xml"))

	if deleteDisks {
		// Only what TitanOS made itself; anything else in the folder stays,
		// and with it the folder.
		for _, disk := range vm.Disks {
			if disk.Path != "" {
				continue // was there before the VM and stays
			}
			if err := os.Remove(m.diskPath(vm, disk)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				slog.Warn("disk of a removed VM not deleted", "vm", name, "err", err)
			}
		}
		os.Remove(filepath.Join(m.dir(vm.Volume, vm.folder()), "nvram.fd"))
		os.RemoveAll(filepath.Join(m.dir(vm.Volume, vm.folder()), "tpm"))
		// The copy of the settings goes with the disks it describes.
		m.removeVolumeCopy(vm)
		for _, volume := range vm.volumes() {
			os.Remove(m.dir(volume, vm.folder()))
		}
	} else {
		// The disks stay: the copy says the VM was removed, and it can be
		// taken over again ("VM übernehmen").
		m.markVolumeCopyRemoved(vm, time.Now().UTC().Truncate(time.Second))
	}
	return nil
}
