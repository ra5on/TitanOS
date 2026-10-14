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
		for _, value := range selected {
			id, ok := value.(string)
			if !ok {
				return "", fmt.Errorf("%w: invalid hardware selection", ErrInvalid)
			}
			match := slices.IndexFunc(devices, func(device hardware.ContainerDevice) bool {
				return device.ID == id || (device.Available && (id == "gpu-nvidia" && device.Kind == "nvidia" || id == "gpu-intel" && (device.Kind == "igpu" || device.Kind == "gpu") && device.Vendor == "8086" || id == "npu-intel" && device.Kind == "npu" && device.Vendor == "8086"))
			})
			if match < 0 || !devices[match].Available || blocked[devices[match].ID] {
				return "", fmt.Errorf("%w: hardware %s is missing or reserved by a VM", ErrInvalid, id)
			}
			device := devices[match]
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
		if len(ids) > 0 {
			labels := labelsMap(service["labels"])
			labels["titanos.hardware"] = strings.Join(ids, ",")
			service["labels"] = labels
		}
	}
	out, err := yaml.Marshal(doc)
	return string(out), err
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
	if !strings.Contains(text, "x-titanos-hardware") && !strings.Contains(text, "hardware:") && len(m.gpuLeases) == 0 {
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
