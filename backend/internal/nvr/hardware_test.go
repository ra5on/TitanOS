package nvr

import (
	"slices"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/hardware"
)

var (
	devNPU   = hardware.ContainerDevice{ID: "0000:00:0b.0", Vendor: "8086", Kind: "npu", Title: "Intel · 0000:00:0b.0", Available: true, Mappings: []string{"/dev/accel/accel0:/dev/accel/accel0"}}
	devIGPU  = hardware.ContainerDevice{ID: "0000:00:02.0", Vendor: "8086", Kind: "igpu", Title: "Intel · 0000:00:02.0", Available: true, Mappings: []string{"/dev/dri/renderD128:/dev/dri/renderD128"}}
	devAMD   = hardware.ContainerDevice{ID: "0000:03:00.0", Vendor: "1002", Kind: "gpu", Available: true, Mappings: []string{"/dev/dri/renderD129:/dev/dri/renderD129"}}
	devNV    = hardware.ContainerDevice{ID: "0000:01:00.0", Vendor: "10de", Kind: "nvidia", Available: true, Mappings: []string{"nvidia.com/gpu=GPU-1"}}
	devCoral = hardware.ContainerDevice{ID: "coral-usb", Kind: "coral", Title: "Coral USB", Available: true}
	fullHost = hostFacts{accel: true, avx2: true, memory: 16 << 30}
)

func kinds(h HardwareInfo) []string {
	var out []string
	for _, d := range h.Devices {
		out = append(out, d.Kind)
	}
	return out
}

func TestProbeHardware(t *testing.T) {
	h := probeHardware([]hardware.ContainerDevice{devIGPU, devNPU, devNV}, nil, fullHost)
	if !slices.Equal(kinds(h), []string{"npu", "igpu", "cpu"}) || h.Auto != "npu" || !h.VideoDecode || !h.EnrichmentsPossible || !h.AccelDevice {
		t.Errorf("%+v", h)
	}
	// A stick comes first: whoever has one bought it for this.
	h = probeHardware([]hardware.ContainerDevice{devIGPU, devNPU, devCoral}, nil, hostFacts{hailo: true, avx2: true, memory: 16 << 30})
	if !slices.Equal(kinds(h), []string{"coral", "hailo", "npu", "igpu", "cpu"}) || h.Auto != "coral" {
		t.Errorf("%v auto %s", kinds(h), h.Auto)
	}
	// The Coral without its plugin, the iGPU with a VM, the NPU without a driver.
	coral, igpu, npu := devCoral, devIGPU, devNPU
	coral.Available = false
	npu.Available, npu.Mappings, npu.Reason = false, nil, "driver"
	h = probeHardware([]hardware.ContainerDevice{coral, igpu, npu}, map[string]bool{igpu.ID: true}, hostFacts{avx2: true, memory: 2 << 30})
	if h.Auto != "cpu" || h.VideoDecode {
		t.Errorf("auto %s, decode %v", h.Auto, h.VideoDecode)
	}
	for kind, reason := range map[string]string{"coral": "plugin", "igpu": "vm", "npu": "driver"} {
		if d, ok := h.device(kind); !ok || !d.Present || d.Available || d.Reason != reason {
			t.Errorf("%s: %+v", kind, d)
		}
	}
	if h.EnrichmentsPossible || h.EnrichmentsReason != "memory" {
		t.Errorf("enrichments with 2 GB: %+v", h)
	}
	if h := probeHardware(nil, nil, hostFacts{memory: 16 << 30}); h.EnrichmentsReason != "avx2" || !slices.Equal(kinds(h), []string{"cpu"}) {
		t.Errorf("%+v", h)
	}
	// An AMD GPU decodes but is no OpenVINO device.
	if h := probeHardware([]hardware.ContainerDevice{devAMD}, nil, fullHost); !h.VideoDecode || !slices.Equal(kinds(h), []string{"cpu"}) {
		t.Errorf("%+v", h)
	}
}

func TestChains(t *testing.T) {
	all := probeHardware([]hardware.ContainerDevice{devIGPU, devNPU, devCoral}, nil, hostFacts{hailo: true})
	plain := probeHardware([]hardware.ContainerDevice{devIGPU, devNPU}, nil, hostFacts{})
	noNPU := probeHardware([]hardware.ContainerDevice{devIGPU}, nil, hostFacts{})
	for _, tc := range []struct {
		name    string
		h       HardwareInfo
		setting string
		want    []string
	}{
		{"auto, no stick", plain, DetectorAuto, []string{"npu", "igpu", "cpu"}},
		{"auto with sticks", all, DetectorAuto, []string{"coral", "hailo", "npu", "igpu", "cpu"}},
		{"npu", all, DetectorNPU, []string{"npu", "igpu", "cpu"}},
		{"igpu never goes back to the npu", all, DetectorIGPU, []string{"igpu", "cpu"}},
		{"coral", all, DetectorCoral, []string{"coral", "npu", "igpu", "cpu"}},
		{"hailo", all, DetectorHailo, []string{"hailo", "npu", "igpu", "cpu"}},
		{"cpu", all, DetectorCPU, []string{"cpu"}},
		{"a chosen device that is missing is still tried", noNPU, DetectorNPU, []string{"npu", "igpu", "cpu"}},
		{"auto without npu", noNPU, DetectorAuto, []string{"igpu", "cpu"}},
	} {
		if got := tc.h.chain(tc.setting); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	if plain.next(DetectorAuto, "npu") != "igpu" || plain.next(DetectorAuto, "igpu") != "cpu" || plain.next(DetectorAuto, "cpu") != "" {
		t.Error("next along npu → igpu → cpu")
	}
	// The device in use was unplugged: the CPU remains.
	if got := plain.next(DetectorAuto, "coral"); got != "cpu" {
		t.Errorf("next after a vanished device = %q", got)
	}
}

func TestContainerDevices(t *testing.T) {
	h := probeHardware([]hardware.ContainerDevice{devIGPU, devNPU, devCoral, devAMD}, nil, hostFacts{hailo: true})
	for _, tc := range []struct {
		detector string
		nodes    []string
		usb      bool
	}{
		{"npu", []string{"/dev/accel/accel0", "/dev/dri/renderD128", "/dev/dri/renderD129"}, false},
		{"igpu", []string{"/dev/dri/renderD128", "/dev/dri/renderD129"}, false},
		{"cpu", []string{"/dev/dri/renderD128", "/dev/dri/renderD129"}, false},
		{"coral", []string{"/dev/dri/renderD128", "/dev/dri/renderD129"}, true},
		{"hailo", []string{"/dev/dri/renderD128", "/dev/dri/renderD129", "/dev/hailo0"}, false},
	} {
		nodes, usb := h.containerDevices(tc.detector)
		if !slices.Equal(nodes, tc.nodes) || usb != tc.usb {
			t.Errorf("%s: %v usb=%v", tc.detector, nodes, usb)
		}
	}
	if nodes, _ := probeHardware(nil, nil, hostFacts{}).containerDevices("cpu"); len(nodes) != 0 {
		t.Errorf("no GPU: %v", nodes)
	}
}

func TestShmBytes(t *testing.T) {
	for cameras, mb := range map[int]int64{0: 256, 1: 256, 2: 256, 3: 320, 4: 384, 8: 640} {
		if got := shmBytes(cameras) >> 20; got != mb {
			t.Errorf("%d cameras: %d MB, want %d", cameras, got, mb)
		}
	}
}
