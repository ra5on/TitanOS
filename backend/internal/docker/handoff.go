package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/hardware"
)

type gpuLease struct {
	Devices []hardware.ContainerDevice `json:"devices"`
	Stopped []string                   `json:"stopped"`
}

// hardwareMu serializes lease changes, hardware resolution for compose up and
// container starts; it is never held across compose up or a backup. Lease records
// live in /run so a titand restart can still return the stopped containers.
func (m *Manager) blockedHardware() map[string]bool {
	result := map[string]bool{}
	for _, lease := range m.gpuLeases {
		for _, device := range lease.Devices {
			result[device.ID] = true
		}
	}
	return result
}

func (m *Manager) saveLeases() error {
	if err := os.MkdirAll(m.hardwareRunDir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(m.gpuLeases)
	if err != nil {
		return err
	}
	path := filepath.Join(m.hardwareRunDir, "leases.json")
	if err := os.WriteFile(path+".tmp", data, 0o600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

func (m *Manager) LoadHardwareLeases() error {
	m.hardwareMu.Lock()
	defer m.hardwareMu.Unlock()
	data, err := os.ReadFile(filepath.Join(m.hardwareRunDir, "leases.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &m.gpuLeases)
}

func (m *Manager) engineRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.applied != ""
}

func usesDevice(info apiInspect, device hardware.ContainerDevice) bool {
	for _, id := range strings.Split(info.Config.Labels["titanos.hardware"], ",") {
		if device.ID != "" && id == device.ID {
			return true
		}
	}
	if info.HostConfig.Privileged {
		return true
	}
	for _, mapping := range info.HostConfig.Devices {
		for _, wanted := range device.Mappings {
			if mapping.PathOnHost == strings.Split(wanted, ":")[0] {
				return true
			}
		}
		if device.Kind == "nvidia" {
			path := mapping.PathOnHost
			if path == "nvidia.com/gpu=all" || path == "nvidia.com/gpu="+device.UUID || path == "nvidia.com/gpu="+device.Index || path == "/dev/nvidia"+device.Index || path == "/dev/nvidiactl" || strings.HasPrefix(path, "/dev/nvidia-uvm") {
				return true
			}
		}
	}
	for _, mount := range info.Mounts {
		if mount.Source == "/dev" || device.Kind == "coral" && strings.HasPrefix(mount.Source, "/dev/bus/usb") || device.Kind != "coral" && (mount.Source == "/dev/dri" || mount.Source == "/dev/accel" || strings.HasPrefix(mount.Source, "/dev/nvidia")) {
			return true
		}
	}
	if device.Kind != "nvidia" {
		return false
	}
	for _, req := range info.HostConfig.DeviceRequests {
		if req.Driver != "" && req.Driver != "nvidia" && req.Driver != "cdi" {
			continue
		}
		gpu := false
		for _, caps := range req.Capabilities {
			gpu = gpu || slices.Contains(caps, "gpu")
		}
		if !gpu && req.Driver != "nvidia" && req.Driver != "cdi" {
			continue
		}
		if req.Count != 0 || len(req.DeviceIDs) == 0 {
			return true
		}
		for _, id := range req.DeviceIDs {
			if id == device.UUID || id == device.Index || id == "nvidia.com/gpu="+device.UUID || id == "nvidia.com/gpu="+device.Index || id == "nvidia.com/gpu=all" || id == "all" || strings.HasSuffix(id, "="+device.UUID) {
				return true
			}
		}
	}
	// The image's NVIDIA_VISIBLE_DEVICES default has no effect on native
	// CDI with runc. Only the legacy NVIDIA runtime interprets this variable.
	if info.HostConfig.Runtime == "nvidia" {
		for _, value := range info.Config.Env {
			if ids, ok := strings.CutPrefix(value, "NVIDIA_VISIBLE_DEVICES="); ok {
				for _, id := range strings.Split(ids, ",") {
					if id == "all" || id == device.UUID || id == device.Index {
						return true
					}
				}
				return false
			}
		}
	}
	return info.HostConfig.Runtime == "nvidia"
}

func (m *Manager) inspect(ctx context.Context, id string) (apiInspect, error) {
	var result apiInspect
	err := m.api.getJSON(ctx, "/containers/"+id+"/json", nil, &result)
	return result, err
}

// BeginVM reserves devices until the VM exits. The returned function MUST be
// called once after libvirt create; false restores containers on start failure.
func (m *Manager) BeginVM(ctx context.Context, name string, pci, usb []string, automatic bool) (func(bool), error) {
	m.hardwareMu.Lock()
	finish := func() { m.hardwareMu.Unlock() }
	if _, exists := m.gpuLeases[name]; exists {
		finish()
		return nil, fmt.Errorf("%w: VM hardware already reserved", ErrInvalid)
	}
	lease := gpuLease{Devices: []hardware.ContainerDevice{}, Stopped: []string{}}
	if m.HardwareDevices != nil {
		for _, device := range m.HardwareDevices(ctx) {
			if !slices.Contains(pci, device.ID) && !(device.Kind == "coral" && (slices.Contains(usb, "1a6e:089a") || slices.Contains(usb, "18d1:9302"))) {
				continue
			}
			if m.blockedHardware()[device.ID] {
				finish()
				return nil, fmt.Errorf("%w: hardware %s is already reserved", ErrInvalid, device.ID)
			}
			lease.Devices = append(lease.Devices, device)
		}
	}
	if len(lease.Devices) == 0 {
		finish()
		return func(bool) {}, nil
	}
	// A compose up resolved its hardware before this lease existed; its
	// containers may still start on these devices.
	for app, use := range m.composing {
		for _, device := range lease.Devices {
			if use.uses(device.ID) {
				finish()
				return nil, fmt.Errorf("%w: Docker is starting app %s on %s", ErrHardwareBusy, app, device.ID)
			}
		}
	}
	// Containers an app backup stopped are started again when it ends.
	if err := m.backupNeeds(ctx, lease.Devices); err != nil {
		finish()
		return nil, err
	}
	var affected []string
	if m.current().Enabled && !m.engineRunning() {
		finish()
		return nil, fmt.Errorf("%w: wait for Docker startup", ErrInvalid)
	}
	if m.engineRunning() {
		list, err := m.api.containers(ctx)
		if err != nil {
			finish()
			return nil, err
		}
		for _, container := range list {
			if container.State != "running" && container.State != "paused" {
				continue
			}
			info, err := m.inspect(ctx, container.ID)
			if err != nil {
				finish()
				return nil, err
			}
			for _, device := range lease.Devices {
				if !usesDevice(info, device) {
					continue
				}
				if !automatic || device.Kind != "nvidia" || container.State == "paused" {
					finish()
					return nil, fmt.Errorf("%w: container %s is using %s", ErrInvalid, strings.Join(container.Names, ","), device.ID)
				}
				affected = append(affected, container.ID)
				break
			}
		}
	}
	if len(affected) > 0 && m.backupActive() {
		finish()
		return nil, fmt.Errorf("%w: %w", ErrHardwareBusy, errBackupRunning)
	}
	if m.gpuLeases == nil {
		m.gpuLeases = map[string]gpuLease{}
	}
	m.gpuLeases[name] = lease
	if err := m.saveLeases(); err != nil {
		delete(m.gpuLeases, name)
		finish()
		return nil, err
	}
	for _, id := range affected {
		// Record intent before stopping; after a daemon crash recovery checks
		// the actual state and starts only an exited container.
		lease.Stopped = append(lease.Stopped, id)
		m.gpuLeases[name] = lease
		if err := m.saveLeases(); err != nil {
			_ = m.returnVM(ctx, name) // the save error is what is reported; giving the GPU back is best effort
			finish()
			return nil, err
		}
		if err := m.api.post(ctx, "/containers/"+id+"/stop", url.Values{"t": {"30"}}); err != nil {
			restoreErr := m.returnVM(context.WithoutCancel(ctx), name)
			finish()
			return nil, errors.Join(err, restoreErr)
		}
	}
	return func(started bool) {
		defer finish()
		if !started {
			recovery, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			if err := m.returnVM(recovery, name); err != nil {
				slog.Warn("return GPU after failed VM start", "vm", name, "err", err)
			}
		}
	}, nil
}

func (m *Manager) returnVM(ctx context.Context, name string) error {
	lease, exists := m.gpuLeases[name]
	if !exists {
		return nil
	}
	// Wait for libvirt to reattach every GPU before restarting its containers.
	if len(lease.Stopped) > 0 && m.HardwareDevices != nil {
		current := m.HardwareDevices(ctx)
		for _, wanted := range lease.Devices {
			if !slices.ContainsFunc(current, func(d hardware.ContainerDevice) bool { return d.ID == wanted.ID && d.Available }) {
				return fmt.Errorf("GPU %s has not returned from VM %s", wanted.ID, name)
			}
		}
	}
	if len(lease.Stopped) > 0 && m.RefreshCDI != nil {
		if err := m.RefreshCDI(ctx); err != nil {
			return err
		}
	}
	for len(lease.Stopped) > 0 {
		id := lease.Stopped[0]
		info, err := m.inspect(ctx, id)
		var apiErr *apiError
		if errors.As(err, &apiErr) && apiErr.Status == 404 {
			err = nil
		} else if err == nil && !info.State.Running {
			// A container can use more than one GPU; another VM may still
			// hold its second card even though this VM has stopped.
			for other, held := range m.gpuLeases {
				if other == name {
					continue
				}
				for _, device := range held.Devices {
					if usesDevice(info, device) {
						return fmt.Errorf("container %s still needs GPU reserved by VM %s", id, other)
					}
				}
			}
			err = m.api.post(ctx, "/containers/"+id+"/start", nil)
		}
		if err != nil {
			return err
		}
		lease.Stopped = lease.Stopped[1:]
		m.gpuLeases[name] = lease
		if err := m.saveLeases(); err != nil {
			return err
		}
	}
	delete(m.gpuLeases, name)
	return m.saveLeases()
}

// Called with names of running OR starting VMs, including after titand restarts.
func (m *Manager) ReturnVMHardware(ctx context.Context, running map[string]bool) error {
	m.hardwareMu.Lock()
	defer m.hardwareMu.Unlock()
	var failures []error
	backup := m.backupActive()
	for name, lease := range m.gpuLeases {
		if !running[name] && backup && len(lease.Stopped) > 0 {
			// Restarting containers would add writers to a running backup;
			// the next poll returns them.
			failures = append(failures, fmt.Errorf("VM %s: %w", name, errBackupRunning))
			continue
		}
		if !running[name] {
			if err := m.returnVM(ctx, name); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}

// backupNeeds refuses devices a container stopped by the open app backup
// uses. The caller holds hardwareMu.
func (m *Manager) backupNeeds(ctx context.Context, devices []hardware.ContainerDevice) error {
	m.mu.Lock()
	stopped := make([]string, 0, len(m.backupStopped))
	for id := range m.backupStopped {
		stopped = append(stopped, id)
	}
	m.mu.Unlock()
	for _, id := range stopped {
		info, err := m.inspect(ctx, url.PathEscape(id))
		var apiErr *apiError
		if errors.As(err, &apiErr) && apiErr.Status == 404 {
			continue
		}
		if err != nil {
			return err
		}
		for _, device := range devices {
			if usesDevice(info, device) {
				return fmt.Errorf("%w: an app backup will restart a container using %s", ErrHardwareBusy, device.ID)
			}
		}
	}
	return nil
}

func (m *Manager) canStartContainer(ctx context.Context, id string) error {
	if len(m.gpuLeases) == 0 {
		return nil
	}
	info, err := m.inspect(ctx, id)
	if err != nil {
		return err
	}
	for name, lease := range m.gpuLeases {
		for _, device := range lease.Devices {
			if usesDevice(info, device) {
				return fmt.Errorf("%w: GPU is reserved by VM %s", ErrInvalid, name)
			}
		}
	}
	return nil
}

// StartupReady prevents VM autostart from racing Docker's restart policies.
func (m *Manager) StartupReady() bool {
	settings := m.current()
	return !settings.Enabled || m.engineRunning()
}
