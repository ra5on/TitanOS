package docker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"go.yaml.in/yaml/v3"
)

// Resolve selections just before starting Compose. The stored file contains
// stable PCI IDs; transient render/accel node numbers stay on /run only.
func resolveHardware(text string, devices []hardware.ContainerDevice, blocked map[string]bool) (string, error) {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return "", err
	}
	services, _ := doc["services"].(map[string]any)
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)
	meta, _ := doc["x-titanos"].(map[string]any)
	for at, name := range names {
		service, _ := services[name].(map[string]any)
		if service == nil {
			continue
		}
		selected, _ := service["x-titanos-hardware"].([]any)
		if at == 0 && selected == nil {
			selected, _ = meta["hardware"].([]any)
		}
		// Raw mappings cannot be safely remapped while any GPU is lent out.
		if len(blocked) > 0 && usesRawGPU(service) {
			return "", fmt.Errorf("%w: GPU resources are reserved by a VM", ErrInvalid)
		}
		ids := []string{}
		selectedNvidia := false
		for _, value := range selected {
			id, ok := value.(string)
			if !ok {
				return "", fmt.Errorf("%w: invalid hardware selection", ErrInvalid)
			}
			match := selectDevice(devices, id)
			if match < 0 || !devices[match].Available || blocked[devices[match].ID] {
				return "", fmt.Errorf("%w: hardware %s is missing or reserved by a VM", ErrInvalid, id)
			}
			device := devices[match]
			if device.Kind == "nvidia" && !selectedNvidia {
				selectedNvidia = true
				// Explicit selection replaces inherited all-GPU CDI mappings.
				previous, _ := service["devices"].([]any)
				kept := []any{}
				for _, mapping := range previous {
					text, _ := mapping.(string)
					if !strings.HasPrefix(text, "nvidia.com/gpu=") {
						kept = append(kept, mapping)
					}
				}
				service["devices"] = kept
			}
			ids = append(ids, device.ID)
			for _, volume := range device.Volumes {
				service["volumes"] = appendUniqueValue(service["volumes"], volume)
			}
			for _, rule := range device.Rules {
				service["device_cgroup_rules"] = appendUniqueValue(service["device_cgroup_rules"], rule)
			}
			for _, mapping := range device.Mappings {
				service["devices"] = appendUniqueValue(service["devices"], mapping)
			}
			for _, group := range device.Groups {
				service["group_add"] = appendUniqueValue(service["group_add"], group)
			}
		}
		normalizeCDI(service, selectedNvidia)
		if len(ids) > 0 {
			labels := labelsMap(service["labels"])
			labels["titanos.hardware"] = strings.Join(ids, ",")
			service["labels"] = labels
		}
	}
	out, err := yaml.Marshal(doc)
	return string(out), err
}

func selectDevice(devices []hardware.ContainerDevice, id string) int {
	return slices.IndexFunc(devices, func(device hardware.ContainerDevice) bool {
		return device.ID == id || (device.Available && (id == "gpu-nvidia" && device.Kind == "nvidia" || id == "gpu-intel" && (device.Kind == "igpu" || device.Kind == "gpu") && device.Vendor == "8086" || id == "npu-intel" && device.Kind == "npu" && device.Vendor == "8086"))
	})
}

// composeUse is the hardware a running compose up may start containers on:
// the selected device IDs, or any device if a service maps devices itself.
type composeUse struct {
	ids map[string]bool
	raw bool
}

func (u composeUse) uses(id string) bool { return u.raw || u.ids[id] }

func composeHardware(text string, devices []hardware.ContainerDevice) composeUse {
	use := composeUse{ids: map[string]bool{}}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		use.raw = true
		return use
	}
	services, _ := doc["services"].(map[string]any)
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)
	meta, _ := doc["x-titanos"].(map[string]any)
	for at, name := range names {
		service, _ := services[name].(map[string]any)
		if service == nil {
			continue
		}
		use.raw = use.raw || usesRawGPU(service)
		selected, _ := service["x-titanos-hardware"].([]any)
		if at == 0 && selected == nil {
			selected, _ = meta["hardware"].([]any)
		}
		for _, value := range selected {
			id, _ := value.(string)
			if match := selectDevice(devices, id); match >= 0 {
				use.ids[devices[match].ID] = true
			} else {
				use.ids[id] = true
			}
		}
	}
	return use
}

func appendUniqueValue(value any, item string) []any {
	list, _ := value.([]any)
	for _, entry := range list {
		if text, ok := entry.(string); ok && text == item {
			return list
		}
	}
	return append(list, item)
}

func labelsMap(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	result := map[string]any{}
	if list, ok := value.([]any); ok {
		for _, entry := range list {
			if text, ok := entry.(string); ok {
				key, value, _ := strings.Cut(text, "=")
				result[key] = value
			}
		}
	}
	return result
}

func usesRawGPU(service map[string]any) bool {
	if service["privileged"] == true || service["runtime"] == "nvidia" || service["gpus"] != nil {
		return true
	}
	for _, field := range []string{"devices", "device_cgroup_rules", "deploy", "environment", "volumes"} {
		text := fmt.Sprint(service[field])
		if strings.Contains(text, "nvidia") || strings.Contains(text, "NVIDIA_VISIBLE_DEVICES") || strings.Contains(text, "/dev/dri") || strings.Contains(text, "/dev/accel") || strings.Contains(text, "/dev/bus/usb") || strings.Contains(text, "capabilities:[gpu]") {
			return true
		}
	}
	return false
}

func (m *Manager) hardwareCompose(ctx context.Context, app string) (string, func(), error) {
	text, err := m.Compose(app)
	if err != nil {
		return "", nil, err
	}
	if !strings.Contains(text, "x-titanos-hardware") && !strings.Contains(text, "hardware:") && !strings.Contains(text, "nvidia.com/gpu=") && len(m.gpuLeases) == 0 {
		return filepath.Join(m.configDir, composeFile(app)), func() {}, nil
	}
	var devices []hardware.ContainerDevice
	if m.HardwareDevices != nil {
		devices = m.HardwareDevices(ctx)
	}
	resolved, err := resolveHardware(text, devices, m.blockedHardware())
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(m.hardwareRunDir, 0o700); err != nil {
		return "", nil, err
	}
	file, err := os.CreateTemp(m.hardwareRunDir, app+"-*.yaml")
	if err != nil {
		return "", nil, err
	}
	path := file.Name()
	cleanup := func() { os.Remove(path) }
	if _, err := file.WriteString(resolved); err != nil {
		file.Close()
		cleanup()
		return "", nil, err
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return filepath.Clean(path), cleanup, nil
}

func (m *Manager) ContainerDevices(ctx context.Context) []hardware.ContainerDevice {
	m.hardwareMu.Lock()
	defer m.hardwareMu.Unlock()
	if m.HardwareDevices == nil {
		return []hardware.ContainerDevice{}
	}
	devices := m.HardwareDevices(ctx)
	blocked := m.blockedHardware()
	for i := range devices {
		if blocked[devices[i].ID] {
			devices[i].Available = false
			devices[i].Reason = "vm"
		}
	}
	return devices
}

// Compose 2.26 treats service.devices as filesystem paths. Express CDI through
// device reservations instead, which becomes Engine HostConfig.DeviceRequests.
// This also repairs stored Compose files produced by earlier TitanOS releases.
func normalizeCDI(service map[string]any, selectedNvidia bool) {
	ids := []any{}
	devices, _ := service["devices"].([]any)
	plain := []any{}
	for _, device := range devices {
		value, ok := device.(string)
		if ok && strings.HasPrefix(value, "nvidia.com/gpu=") && !strings.ContainsAny(value, " \t\n") {
			ids = appendUniqueValue(ids, value)
		} else {
			plain = append(plain, device)
		}
	}
	if len(ids) == 0 {
		return
	}
	if len(plain) == 0 {
		delete(service, "devices")
	} else {
		service["devices"] = plain
	}
	child := func(parent map[string]any, key string) map[string]any {
		v, _ := parent[key].(map[string]any)
		if v == nil {
			v = map[string]any{}
			parent[key] = v
		}
		return v
	}
	reservations := child(child(child(service, "deploy"), "resources"), "reservations")
	old, _ := reservations["devices"].([]any)
	requests := []any{}
	for _, request := range old {
		req, _ := request.(map[string]any)
		if req["driver"] == "nvidia" {
			continue
		}
		if req["driver"] == "cdi" {
			existing, _ := req["device_ids"].([]any)
			kept := []any{}
			for _, id := range existing {
				if value, ok := id.(string); ok && strings.HasPrefix(value, "nvidia.com/gpu=") {
					if !selectedNvidia {
						ids = appendUniqueValue(ids, value)
					}
				} else {
					kept = append(kept, id)
				}
			}
			if len(kept) == 0 {
				continue
			}
			req["device_ids"] = kept
		}
		requests = append(requests, request)
	}
	requests = append(requests, map[string]any{"driver": "cdi", "device_ids": ids, "capabilities": []any{"gpu"}})
	reservations["devices"] = requests
	delete(service, "gpus")
	if service["runtime"] == "nvidia" {
		delete(service, "runtime")
	}
}

// A pre-update failed container still has the old HostConfig after upgrading
// TitanOS. Recreate only that service from its saved Compose file on Start.
// Named/anonymous volumes and bind mounts are retained; no image is pulled.
func (m *Manager) recreateLegacyCDI(ctx context.Context, id string) (bool, error) {
	info, err := m.inspect(ctx, id)
	if err != nil || info.State.Running {
		return false, nil
	}
	legacy := slices.ContainsFunc(info.HostConfig.Devices, func(d struct{ PathOnHost string }) bool { return strings.HasPrefix(d.PathOnHost, "nvidia.com/gpu=") })
	if !legacy {
		return false, nil
	}
	app, service := info.Config.Labels["com.docker.compose.project"], info.Config.Labels["com.docker.compose.service"]
	if !appNameRE.MatchString(app) || service == "" || strings.HasPrefix(service, "-") {
		return false, nil
	}
	text, err := m.Compose(app)
	if err != nil {
		return false, nil
	}
	var doc map[string]any
	if yaml.Unmarshal([]byte(text), &doc) != nil {
		return false, nil
	}
	services, _ := doc["services"].(map[string]any)
	if services[service] == nil {
		return false, nil
	}
	job, volume, err := m.begin(app)
	if err != nil {
		return true, err
	}
	path, cleanup, err := m.hardwareCompose(ctx, app)
	var output string
	if err == nil {
		args := m.compose(volume, app, "up", "--detach", "--no-deps", "--force-recreate", "--pull", "never", service)
		args[4] = path
		output, err = m.runner.Run(ctx, "", "docker", args...)
		cleanup()
	}
	m.mu.Lock()
	job.State, job.Output = "done", tail(output, 4000)
	if err != nil {
		job.State = "failed"
		if job.Output == "" {
			job.Output = err.Error()
		}
	}
	m.mu.Unlock()
	return true, err
}
