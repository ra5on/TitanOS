package viewer

import (
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/hardware"
)

// GPU is a graphics device the remote view can render and encode with.
type GPU struct {
	// ID is the render node (/dev/dri/renderD128) for VA-API, the PCI
	// address for NVIDIA; the setting "gpu" names it.
	ID string `json:"id"`
	// Kind is the encoder: "vaapi" or "nvenc".
	Kind string `json:"kind"`
	// Vendor is the PCI vendor ID (8086 Intel, 1002 AMD, 10de NVIDIA).
	Vendor string `json:"vendor"`
	Title  string `json:"title"`
	// Available is false while the device is with a VM or has no driver;
	// Reason says which ("vm", "driver").
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`

	node   string
	cdi    string
	groups []string
}

// gpusFrom turns the devices the hardware manager offers to containers
// into GPUs for the viewer. A device on vfio-pci or leased to a VM is not
// available. NVIDIA needs the driver plugin: without it nvidia-smi finds
// nothing and the device has no CDI name.
func gpusFrom(devices []hardware.ContainerDevice, leased map[string]bool) []GPU {
	out := []GPU{}
	for _, d := range devices {
		switch d.Kind {
		case "nvidia":
			g := GPU{ID: d.ID, Kind: "nvenc", Vendor: d.Vendor, Title: d.Title, Available: d.Available, Reason: d.Reason}
			for _, m := range d.Mappings {
				if strings.HasPrefix(m, "nvidia.com/gpu=") {
					g.cdi = m
				}
			}
			if g.cdi == "" && g.Available {
				g.Available, g.Reason = false, "driver"
			}
			if leased[d.ID] {
				g.Available, g.Reason = false, "vm"
			}
			out = append(out, g)
		case "gpu", "igpu":
			if len(d.Mappings) == 0 {
				out = append(out, GPU{ID: d.ID, Kind: "vaapi", Vendor: d.Vendor, Title: d.Title, Reason: nonEmpty(d.Reason, "driver")})
				continue
			}
			for i, m := range d.Mappings {
				node, _, _ := strings.Cut(m, ":")
				if !strings.HasPrefix(node, "/dev/dri/renderD") {
					continue
				}
				g := GPU{ID: node, Kind: "vaapi", Vendor: d.Vendor, Title: d.Title, Available: d.Available, Reason: d.Reason, node: node}
				if i < len(d.Groups) {
					g.groups = []string{d.Groups[i]}
				}
				if leased[d.ID] {
					g.Available, g.Reason = false, "vm"
				}
				out = append(out, g)
			}
		}
	}
	return out
}

func nonEmpty(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

// chooseGPU picks the GPU for a new view: none for "off", the named one if
// it is available, otherwise (and for "auto") the first available VA-API
// device, then NVIDIA. nil means the CPU encodes.
func chooseGPU(setting string, gpus []GPU) *GPU {
	if setting == "off" {
		return nil
	}
	for i := range gpus {
		if gpus[i].ID == setting && gpus[i].Available {
			return &gpus[i]
		}
	}
	if setting != "auto" {
		// The chosen device is gone or with a VM: the CPU takes over
		// rather than another device the owner did not choose.
		return nil
	}
	for _, kind := range []string{"vaapi", "nvenc"} {
		for i := range gpus {
			if gpus[i].Kind == kind && gpus[i].Available {
				return &gpus[i]
			}
		}
	}
	return nil
}
