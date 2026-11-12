package nvr

import (
	"bufio"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/hardware"
)

// Device is one device the detection could run on.
type Device struct {
	// Kind is npu, igpu, coral, hailo or cpu.
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// Present: the NAS has it. Available: Frigate can be given it now.
	Present   bool `json:"present"`
	Available bool `json:"available"`
	// Reason says why a present device is not available: "driver" (no
	// device node), "vm" (handed to a virtual machine), "plugin" (the
	// Coral plugin is not installed).
	Reason string `json:"reason,omitempty"`

	// nodes are the device nodes passed to the container.
	nodes []string
}

// HardwareInfo is what the NAS has for the NVR.
type HardwareInfo struct {
	// Devices lists npu, igpu, coral, hailo (only those present) and cpu.
	Devices []Device `json:"devices"`
	// Auto is what "auto" picks now.
	Auto string `json:"auto"`
	// VideoDecode: a GPU decodes the camera streams (VA-API); without, the
	// CPU does.
	VideoDecode bool `json:"videoDecode"`
	// AccelDevice: /dev/accel exists (the NPU's driver is loaded).
	AccelDevice bool `json:"accelDevice"`
	// AVX2 and MemoryBytes decide over plates and faces.
	AVX2        bool   `json:"avx2"`
	MemoryBytes uint64 `json:"memoryBytes"`
	// EnrichmentsPossible: plates and faces can run; if not,
	// EnrichmentsReason is "avx2" or "memory".
	EnrichmentsPossible bool   `json:"enrichmentsPossible"`
	EnrichmentsReason   string `json:"enrichmentsReason,omitempty"`

	// renderNodes are the GPUs' render nodes for decoding.
	renderNodes []string
}

// enrichmentsMemory is what Frigate asks for plates and faces (4 GB).
const enrichmentsMemory = 4 << 30

// hostFacts reads what does not come from the hardware manager; tests
// replace it.
type hostFacts struct {
	// hailo: /dev/hailo0 is a device node. accel: /dev/accel exists.
	hailo, accel bool
	avx2         bool
	memory       uint64
}

func readHostFacts() hostFacts {
	var f hostFacts
	if info, err := os.Stat("/dev/hailo0"); err == nil && info.Mode()&os.ModeCharDevice != 0 {
		f.hailo = true
	}
	if info, err := os.Stat("/dev/accel"); err == nil && info.IsDir() {
		f.accel = true
	}
	if file, err := os.Open("/proc/cpuinfo"); err == nil {
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for scanner.Scan() {
			if key, value, ok := strings.Cut(scanner.Text(), ":"); ok && strings.TrimSpace(key) == "flags" {
				f.avx2 = slices.Contains(strings.Fields(value), "avx2")
				break
			}
		}
		file.Close()
	}
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == "MemTotal:" {
				kb, _ := strconv.ParseUint(fields[1], 10, 64) // a line that cannot be read leaves 0: enrichments are then not offered
				f.memory = kb << 10
			}
		}
	}
	return f
}

// probeHardware turns the devices TitanOS offers to containers (the same
// list the Docker pages and the iGPU/NPU widgets use) into the devices of
// the NVR. leased are devices handed to a VM right now.
func probeHardware(devices []hardware.ContainerDevice, leased map[string]bool, facts hostFacts) HardwareInfo {
	info := HardwareInfo{AccelDevice: facts.accel, AVX2: facts.avx2, MemoryBytes: facts.memory}
	var npu, igpu, coral *Device
	for _, d := range devices {
		nodes := []string{}
		for _, m := range d.Mappings {
			if node, _, _ := strings.Cut(m, ":"); strings.HasPrefix(node, "/dev/") {
				nodes = append(nodes, node)
			}
		}
		available, reason := d.Available && len(nodes) > 0, d.Reason
		if leased[d.ID] {
			available, reason = false, "vm"
		}
		if available {
			reason = ""
		} else if reason == "" {
			reason = "driver"
		}
		switch {
		case d.Kind == "npu" && d.Vendor == "8086":
			if npu == nil || (!npu.Available && available) {
				npu = &Device{Kind: DetectorNPU, Title: d.Title, Present: true, Available: available, Reason: reason, nodes: nodes}
			}
		case (d.Kind == "igpu" || d.Kind == "gpu") && (d.Vendor == "8086" || d.Vendor == "1002"):
			// Intel and AMD decode over VA-API; only Intel runs OpenVINO.
			if available {
				info.renderNodes = append(info.renderNodes, nodes...)
			}
			if d.Vendor == "8086" && (igpu == nil || (!igpu.Available && available)) {
				igpu = &Device{Kind: DetectorIGPU, Title: d.Title, Present: true, Available: available, Reason: reason, nodes: nodes}
			}
		case d.Kind == "coral":
			// hardware offers the Coral to containers only with its plugin.
			c := Device{Kind: DetectorCoral, Title: d.Title, Present: true, Available: d.Available}
			if !c.Available {
				c.Reason = "plugin"
			}
			coral = &c
		}
	}
	// The order of the list is the order "auto" tries: a Coral or Hailo
	// first (whoever has one bought it for this), then NPU, iGPU, CPU.
	for _, d := range []*Device{coral} {
		if d != nil {
			info.Devices = append(info.Devices, *d)
		}
	}
	if facts.hailo {
		info.Devices = append(info.Devices, Device{Kind: DetectorHailo, Title: "Hailo-8", Present: true, Available: true, nodes: []string{"/dev/hailo0"}})
	}
	for _, d := range []*Device{npu, igpu} {
		if d != nil {
			info.Devices = append(info.Devices, *d)
		}
	}
	info.Devices = append(info.Devices, Device{Kind: DetectorCPU, Title: "CPU", Present: true, Available: true})
	info.VideoDecode = len(info.renderNodes) > 0
	info.Auto = info.chain(DetectorAuto)[0]
	switch {
	case !facts.avx2:
		info.EnrichmentsReason = "avx2"
	case facts.memory < enrichmentsMemory:
		info.EnrichmentsReason = "memory"
	default:
		info.EnrichmentsPossible = true
	}
	return info
}

func (h HardwareInfo) device(kind string) (Device, bool) {
	for _, d := range h.Devices {
		if d.Kind == kind {
			return d, true
		}
	}
	return Device{}, false
}

// chain is the order in which devices are tried for a setting: the chosen
// one first, then what remains of Coral/Hailo → NPU → iGPU → CPU after
// it. "auto" starts at the first available device. Devices that are not
// available are left out, except a chosen one: it is tried (and fails
// over) so that the owner sees why it does not work. The CPU always ends
// the chain.
func (h HardwareInfo) chain(setting string) []string {
	var order []string
	for _, d := range h.Devices {
		if d.Available {
			order = append(order, d.Kind)
		}
	}
	if setting == DetectorAuto || setting == "" {
		return order
	}
	// Never back up the order: a failing NPU goes on to the iGPU, a
	// failing iGPU never to the NPU or a stick.
	at := slices.Index(deviceRank, setting)
	if setting == DetectorCoral || setting == DetectorHailo {
		at = slices.Index(deviceRank, DetectorHailo) // the two sticks do not stand in for each other
	}
	out := []string{setting}
	for _, kind := range order {
		if slices.Index(deviceRank, kind) > at && kind != setting {
			out = append(out, kind)
		}
	}
	return out
}

// deviceRank is the order of the chains: the sticks, NPU, iGPU, CPU.
var deviceRank = []string{DetectorCoral, DetectorHailo, DetectorNPU, DetectorIGPU, DetectorCPU}

// next is the device after current in the chain of setting, "" at its end.
func (h HardwareInfo) next(setting, current string) string {
	chain := h.chain(setting)
	if i := slices.Index(chain, current); i >= 0 && i+1 < len(chain) {
		return chain[i+1]
	}
	if i := slices.Index(chain, current); i < 0 && current != DetectorCPU {
		// The device in use left the chain (unplugged, or with a VM).
		return h.after(setting, current)
	}
	return ""
}

// after is the first device of the chain of setting that comes after
// current in the order of the chains, the CPU if none does: never one
// before it (those failed, or were passed over).
func (h HardwareInfo) after(setting, current string) string {
	at := slices.Index(deviceRank, current)
	for _, kind := range h.chain(setting) {
		if slices.Index(deviceRank, kind) > at {
			return kind
		}
	}
	return DetectorCPU
}

// containerDevices are the device nodes Frigate gets for a detector: the
// GPUs' render nodes (decoding, and the iGPU for OpenVINO), plus the
// detector's own. usb says that the USB bus is passed (Coral).
func (h HardwareInfo) containerDevices(detector string) (nodes []string, usb bool) {
	nodes = slices.Clone(h.renderNodes)
	if d, ok := h.device(detector); ok && d.Available {
		for _, node := range d.nodes {
			if !slices.Contains(nodes, node) {
				nodes = append(nodes, node)
			}
		}
	}
	slices.Sort(nodes)
	return nodes, detector == DetectorCoral
}
