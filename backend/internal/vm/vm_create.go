package vm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Create makes a new VM with its disks. It does not start it.
func (m *Manager) Create(ctx context.Context, vm VM, mounted []string) error {
	if m.NetworkLease != nil {
		release, err := m.NetworkLease()
		if err != nil {
			return err
		}
		defer release()
	}
	if !m.createMu.TryLock() {
		return ErrSnapshotBusy
	}
	defer m.createMu.Unlock()
	if m.Disabled {
		return ErrDisabled
	}
	if vm.Network == "bridge" && m.BridgeReady != nil && !m.BridgeReady() {
		return ErrNoBridge
	}
	if err := m.prepare(ctx, &vm, mounted); err != nil {
		return err
	}
	m.mu.Lock()
	_, exists := m.vms[vm.Name]
	loaded := m.loaded
	m.mu.Unlock()
	if !loaded {
		return fmt.Errorf("%w: /config is not readable", ErrInvalid)
	}
	if exists {
		return ErrExists
	}
	if err := m.checkDirectory(vm); err != nil {
		return err
	}
	vm.UUID, vm.MAC, vm.Created = newUUID(), newMAC(), time.Now().UTC().Truncate(time.Second)

	// Whatever is made here is taken back if a later step fails.
	var created []string
	fail := func(err error) error {
		for _, path := range created {
			os.Remove(path)
		}
		for _, volume := range vm.volumes() {
			os.Remove(m.dir(volume, vm.folder())) // only if empty
		}
		return err
	}
	for _, volume := range vm.volumes() {
		if _, err := m.makeDir(volume, vm.folder()); err != nil {
			return fail(err)
		}
	}
	copies := false
	for _, disk := range vm.Disks {
		if disk.Path != "" {
			continue
		}
		path := m.diskPath(vm, disk)
		if _, err := os.Lstat(path); err == nil {
			// Never write over a disk that is there, e.g. of a VM removed earlier.
			return fail(fmt.Errorf("%w: %s exists already", ErrExists, path))
		}
		free, err := freeBytes(filepath.Dir(path))
		if err != nil {
			return fail(err)
		}
		// A raw disk takes its full size at once; a qcow2 one starts small.
		if needed := map[bool]int64{true: disk.SizeBytes, false: gib}[disk.Format == "raw"]; free < needed {
			return fail(ErrNoSpace)
		}
		if disk.Source != "" {
			// Copied in the background once the VM is stored.
			copies = true
			continue
		}
		args := []string{"create", "-q", "-f", disk.Format}
		if disk.Format == "raw" {
			args = append(args, "-o", "preallocation=falloc")
		}
		created = append(created, path)
		if _, err := m.runner.Run(ctx, "", "qemu-img", append(args, path, strconv.FormatInt(disk.SizeBytes, 10))...); err != nil {
			return fail(err)
		}
		if err := m.share(path, 0o660); err != nil {
			return fail(err)
		}
	}
	if err := m.persistVM(vm); err != nil {
		return fail(err)
	}
	m.mu.Lock()
	m.vms[vm.Name] = vm
	if copies {
		m.jobs[vm.Name] = &Job{State: "running"}
	}
	m.mu.Unlock()
	if vm.PowerSchedule.Enabled {
		m.resetPower(vm.Name, time.Now())
	}
	if copies {
		go m.copyDisks(context.WithoutCancel(ctx), vm)
	}
	return nil
}

// Change is what can be changed on a VM that exists.
type Change struct {
	DisplayName *string  `json:"displayName"`
	Description string   `json:"description"`
	MemoryMiB   int      `json:"memoryMiB"`
	VCPUs       int      `json:"vcpus"`
	ISO         string   `json:"iso"`
	VirtIOISO   string   `json:"virtioISO"`
	Autostart   bool     `json:"autostart"`
	CPUPin      []int    `json:"cpuPin"`
	Exclusive   bool     `json:"exclusive"`
	Folders     []Folder `json:"folders"`
	PCI         []string `json:"pci"`
	AutoGPU     bool     `json:"autoGPU"`
	USB         []string `json:"usb"`
	Network     string   `json:"network"`
	// PowerSchedule replaces the schedule; without it the schedule stays.
	PowerSchedule *PowerSchedule `json:"powerSchedule"`
}

// Update changes a VM. Everything but the display name, description and autostart
// belongs to the virtual hardware and only changes while the VM is off.
func (m *Manager) Update(ctx context.Context, name string, change Change, mounted []string) error {
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
	changed := vm
	if change.DisplayName != nil {
		changed.DisplayName = strings.TrimSpace(*change.DisplayName)
		if !validDisplayName(changed.DisplayName) {
			return ErrInvalidDisplayName
		}
	}
	changed.Description, changed.Autostart = change.Description, change.Autostart
	changed.MemoryMiB, changed.VCPUs, changed.ISO = change.MemoryMiB, change.VCPUs, change.ISO
	changed.VirtIOISO = change.VirtIOISO
	changed.CPUPin, changed.Exclusive, changed.Folders, changed.PCI, changed.USB = change.CPUPin, change.Exclusive, change.Folders, change.PCI, change.USB
	changed.Network = change.Network
	changed.AutoGPU = change.AutoGPU
	if change.PowerSchedule != nil {
		changed.PowerSchedule = *change.PowerSchedule
	}
	if err := m.prepare(ctx, &changed, mounted); err != nil {
		return err
	}
	before, after := vm, changed
	before.DisplayName, after.DisplayName = "", ""
	before.Description, before.Autostart, after.Description, after.Autostart = "", false, "", false
	before.PowerSchedule, after.PowerSchedule = PowerSchedule{}, PowerSchedule{}
	if !reflect.DeepEqual(normal(before), normal(after)) && m.pid(name) != 0 {
		return ErrRunning
	}
	if err := m.persistVM(changed); err != nil {
		return err
	}
	m.mu.Lock()
	m.vms[name] = changed
	m.mu.Unlock()
	if !reflect.DeepEqual(vm.PowerSchedule, changed.PowerSchedule) {
		m.resetPower(name, time.Now())
	}
	return nil
}

// normal makes VMs comparable: no entries and an empty list are the same.
func normal(vm VM) VM {
	if len(vm.CPUPin) == 0 {
		vm.CPUPin = nil
	}
	if len(vm.Folders) == 0 {
		vm.Folders = nil
	}
	if len(vm.PCI) == 0 {
		vm.PCI = nil
	}
	if len(vm.USB) == 0 {
		vm.USB = nil
	}
	return vm
}

// XML returns what libvirt is told about a VM.
func (m *Manager) XML(name string) (string, error) {
	vm, err := m.get(name)
	if err != nil {
		return "", err
	}
	return m.domainXML(vm)
}
