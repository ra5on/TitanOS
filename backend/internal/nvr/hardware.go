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

// Decoder is a GPU that can decode the camera streams (PLAN 82-4).
type Decoder struct {
	// ID is the PCI address, what Settings.Decoder names.
	ID    string `json:"id"`
	Title string `json:"title"`
	// Kind is igpu or gpu (Intel, AMD: VA-API) or nvidia (CUDA).
	Kind string `json:"kind"`
	// Available: Frigate can be given it now; if not, Reason is "driver"
	// (no render node, no CDI device) or "vm" (with a virtual machine).
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`

	// nodes are its render nodes (VA-API), cdi its CDI names (NVIDIA).
	nodes, cdi []string
}

// DecoderStatus is the GPU that decodes as the setting resolves now.
type DecoderStatus struct {
	// Setting is DecoderAuto, DecoderCPU or a PCI address.
	Setting string `json:"setting"`
	// Active is the PCI address of the GPU that decodes, DecoderCPU for
	// the CPU; Kind is igpu, gpu, nvidia or cpu, Title the GPU's name.
	Active string `json:"active"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	// Reason says why a chosen GPU does not decode (the CPU does instead):
	// "gone" (not in the NAS any more), "vm" or "driver".
	Reason string `json:"reason,omitempty"`
}

// HardwareInfo is what the NAS has for the NVR.
type HardwareInfo struct {
	// Devices lists npu, igpu, coral, hailo (only those present) and cpu.
	Devices []Device `json:"devices"`
	// Auto is what "auto" picks now.
	Auto string `json:"auto"`
	// Decoders are the GPUs that could decode the camera streams, in the
	// order of the NAS (PCI addresses).
	Decoders []Decoder `json:"decoders"`
	// VideoDecode: a GPU decodes the camera streams; without, the CPU
	// does. Status and diagnosis set it from the decoder in use; the probe
	// alone tells whether "auto" would find one.
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
	info := HardwareInfo{AccelDevice: facts.accel, AVX2: facts.avx2, MemoryBytes: facts.memory, Decoders: []Decoder{}}
	var npu, igpu, coral *Device
	for _, d := range devices {
		nodes, cdi := []string{}, []string{}
		for _, m := range d.Mappings {
			if node, _, _ := strings.Cut(m, ":"); strings.HasPrefix(node, "/dev/") {
				nodes = append(nodes, node)
			} else if strings.HasPrefix(m, "nvidia.com/gpu=") {
				cdi = append(cdi, m)
			}
		}
		available, reason := d.Available && len(nodes) > 0, d.Reason
		if d.Kind == "nvidia" {
			available = d.Available && len(cdi) > 0
		}
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
		case d.Kind == "nvidia":
			// NVIDIA decodes over CUDA; the container gets the GPU through
			// CDI (nvidia.com/gpu=<UUID>), as Docker apps do.
			info.Decoders = append(info.Decoders, Decoder{ID: d.ID, Title: d.Title, Kind: "nvidia", Available: available, Reason: reason, cdi: cdi})
		case (d.Kind == "igpu" || d.Kind == "gpu") && (d.Vendor == "8086" || d.Vendor == "1002"):
			// Intel and AMD decode over VA-API; only Intel runs OpenVINO.
			info.Decoders = append(info.Decoders, Decoder{ID: d.ID, Title: d.Title, Kind: d.Kind, Available: available, Reason: reason, nodes: renderNodes(nodes)})
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
	_, auto := info.decoder(DecoderAuto)
	info.VideoDecode = auto.Active != DecoderCPU
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

// renderNodes are the render nodes among a GPU's device nodes.
func renderNodes(nodes []string) []string {
	out := []string{}
	for _, node := range nodes {
		if strings.HasPrefix(node, "/dev/dri/renderD") {
			out = append(out, node)
		}
	}
	return out
}

// decoder resolves the setting of the decoder (Settings.Decoder) on this
// NAS: "" or auto takes the first free iGPU, else the first free other
// Intel/AMD GPU, else the first free NVIDIA GPU, else the CPU; cpu is the
// CPU; a PCI address is that GPU while it is free, else the CPU with the
// reason – never another GPU: the owner chose this one, and a GPU picked
// behind his back might be the one he keeps for a VM or an app. The
// decoder is nil for the CPU.
func (h HardwareInfo) decoder(setting string) (*Decoder, DecoderStatus) {
	cpu := DecoderStatus{Setting: setting, Active: DecoderCPU, Kind: DecoderCPU, Title: "CPU"}
	switch setting {
	case "", DecoderAuto:
		cpu.Setting = DecoderAuto
		for _, kind := range []string{"igpu", "gpu", "nvidia"} {
			for i, d := range h.Decoders {
				if d.Kind == kind && d.Available {
					return &h.Decoders[i], DecoderStatus{Setting: DecoderAuto, Active: d.ID, Kind: d.Kind, Title: d.Title}
				}
			}
		}
		return nil, cpu
	case DecoderCPU:
		return nil, cpu
	}
	for i, d := range h.Decoders {
		if d.ID != setting {
			continue
		}
		if !d.Available {
			cpu.Reason = d.Reason
			if cpu.Reason == "" {
				cpu.Reason = "driver"
			}
			return nil, cpu
		}
		return &h.Decoders[i], DecoderStatus{Setting: setting, Active: d.ID, Kind: d.Kind, Title: d.Title}
	}
	cpu.Reason = "gone"
	return nil, cpu
}

// withBroken marks GPUs whose CDI device the engine refused as without
// driver: the decoder falls back to the CPU instead of leaving the NVR
// without a container (watch.go).
func (h HardwareInfo) withBroken(broken map[string]bool) HardwareInfo {
	if len(broken) == 0 {
		return h
	}
	h.Decoders = slices.Clone(h.Decoders)
	for i := range h.Decoders {
		if broken[h.Decoders[i].ID] && h.Decoders[i].Available {
			h.Decoders[i].Available, h.Decoders[i].Reason = false, "driver"
		}
	}
	return h
}

// containerDevices are what Frigate gets for a detector and a decoder:
// the decoder's render node or CDI device and the detector's own nodes
// (the iGPU's render node for OpenVINO, the NPU's accel node, …); never
// another GPU, so that Frigate cannot pick one by itself. usb says that
// the USB bus is passed (Coral). decode is the decoder as Render needs it.
func (h HardwareInfo) containerDevices(detector string, dec *Decoder) (nodes, cdi []string, usb bool, decode Decode) {
	nodes = []string{}
	if dec != nil {
		nodes = append(nodes, dec.nodes...)
		cdi = slices.Clone(dec.cdi)
	}
	if d, ok := h.device(detector); ok && d.Available {
		for _, node := range d.nodes {
			if !slices.Contains(nodes, node) {
				nodes = append(nodes, node)
			}
		}
	}
	slices.Sort(nodes)
	switch {
	case dec == nil:
	case dec.Kind == "nvidia":
		decode = Decode{Kind: DecodeNVIDIA}
	case len(dec.nodes) > 0:
		decode = Decode{Kind: DecodeVAAPI}
		if len(renderNodes(nodes)) > 1 {
			// The detector brought a render node of another GPU.
			decode.Node = dec.nodes[0]
		}
	}
	return nodes, cdi, detector == DetectorCoral, decode
}
