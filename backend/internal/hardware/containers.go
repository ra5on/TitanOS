package hardware

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/run"
)

type ContainerDevice struct {
	Volumes   []string `json:"volumes,omitempty"`
	Rules     []string `json:"rules,omitempty"`
	Warning   string   `json:"warning,omitempty"`
	Vendor    string   `json:"vendor,omitempty"`
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Kind      string   `json:"kind"`
	Mappings  []string `json:"mappings"`
	Groups    []string `json:"groups"`
	Available bool     `json:"available"`
	Reason    string   `json:"reason"`
	// UUID and Index let the handoff recognize older NVIDIA Compose files.
	UUID  string `json:"uuid,omitempty"`
	Index string `json:"index,omitempty"`
}

var gpuUUID = regexp.MustCompile(`^GPU-[a-fA-F0-9-]+$`)

func (m *Manager) ContainerDevices(ctx context.Context) []ContainerDevice {
	return m.containerDevices(ctx, run.Exec{}, "/dev")
}

func (m *Manager) containerDevices(ctx context.Context, runner run.Runner, devRoot string) []ContainerDevice {
	result := []ContainerDevice{}
	for _, pci := range m.Status().PCI {
		device := ContainerDevice{ID: pci.Address, Vendor: pci.Vendor, Title: pci.VendorName + " · " + pci.Address, Kind: pci.Kind, Mappings: []string{}, Groups: []string{}}
		if pci.Vendor == "10de" {
			device.Kind = "nvidia"
		}
		if pci.Driver == "vfio-pci" {
			device.Reason = "vm"
			result = append(result, device)
			continue
		}
		if pci.Driver == "" {
			device.Reason = "driver"
			result = append(result, device)
			continue
		}
		if pci.Vendor == "10de" {
			device.Kind = "nvidia"
			queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			out, err := runner.Run(queryCtx, "", "nvidia-smi", "--id="+pci.Address, "--query-gpu=uuid,index", "--format=csv,noheader,nounits")
			cancel()
			rows, parseErr := csv.NewReader(strings.NewReader(strings.TrimSpace(out))).ReadAll()
			if err == nil && parseErr == nil && len(rows) == 1 && len(rows[0]) == 2 && gpuUUID.MatchString(strings.TrimSpace(rows[0][0])) && regexp.MustCompile(`^[0-9]+$`).MatchString(strings.TrimSpace(rows[0][1])) {
				device.UUID, device.Index = strings.TrimSpace(rows[0][0]), strings.TrimSpace(rows[0][1])
				device.Mappings = []string{"nvidia.com/gpu=" + device.UUID}
			}
		} else {
			class, pattern, target := "drm", "renderD*", "dri"
			if pci.Kind == "npu" {
				class, pattern, target = "accel", "accel*", "accel"
			}
			nodes, _ := filepath.Glob(filepath.Join(m.sys, "class", class, pattern))
			for _, node := range nodes {
				path, err := filepath.EvalSymlinks(filepath.Join(node, "device"))
				if err != nil || filepath.Base(path) != pci.Address {
					continue
				}
				deviceNode := filepath.Join(devRoot, target, filepath.Base(node))
				info, err := os.Stat(deviceNode)
				if err != nil || info.Mode()&os.ModeCharDevice == 0 {
					continue
				}
				// The backend resolves IDs again at each Compose start. Device
				// node numbers are never the persistent selection.
				device.Mappings = append(device.Mappings, deviceNode+":"+deviceNode)
				if stat, ok := info.Sys().(*syscall.Stat_t); ok {
					device.Groups = append(device.Groups, strconv.FormatUint(uint64(stat.Gid), 10))
				}
			}
		}
		device.Available = len(device.Mappings) > 0
		if !device.Available {
			device.Reason = "driver"
		}
		result = append(result, device)
	}
	for _, usb := range m.Status().USB {
		if usb.Stick || (usb.ID != "1a6e:089a" && usb.ID != "18d1:9302") {
			continue
		}
		_, err := os.Stat("/usr/bin/titanos-coral-info")
		result = append(result, ContainerDevice{ID: "coral-usb", Title: "Coral USB", Kind: "coral", Mappings: []string{}, Groups: []string{}, Available: err == nil, Reason: "driver", Warning: "usb_bus", Volumes: []string{"/dev/bus/usb:/dev/bus/usb"}, Rules: []string{"c 189:* rmw"}})
		break
	}
	return result
}
