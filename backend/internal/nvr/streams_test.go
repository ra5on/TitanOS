package nvr

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
)

// renderedCamera is the part of config.yml the tests of PLAN 82 read.
type renderedConfig struct {
	FFmpeg struct {
		HWAccel any `yaml:"hwaccel_args"`
	} `yaml:"ffmpeg"`
	Go2RTC struct {
		Streams map[string][]string `yaml:"streams"`
		Preload yaml.Node           `yaml:"preload"`
	} `yaml:"go2rtc"`
	Cameras map[string]struct {
		FFmpeg struct {
			OutputArgs struct {
				Record string `yaml:"record"`
			} `yaml:"output_args"`
			Inputs []struct {
				Path  string   `yaml:"path"`
				Roles []string `yaml:"roles"`
			} `yaml:"inputs"`
		} `yaml:"ffmpeg"`
		Detect struct {
			Width  *int `yaml:"width"`
			Height *int `yaml:"height"`
		} `yaml:"detect"`
	} `yaml:"cameras"`
}

func render(t *testing.T, cameras []Camera, deleted []DeletedCamera, hw RenderHardware) renderedConfig {
	t.Helper()
	if hw.Detector == "" {
		hw.Detector = DetectorCPU
	}
	out, err := Render(secretSettings(), cameras, deleted, hw)
	if err != nil {
		t.Fatal(err)
	}
	var cfg renderedConfig
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// preloadKeys are the names under go2rtc.preload, in their order.
func (c renderedConfig) preloadKeys() []string {
	out := []string{}
	for i := 0; i+1 < len(c.Go2RTC.Preload.Content); i += 2 {
		out = append(out, c.Go2RTC.Preload.Content[i].Value)
	}
	return out
}

// inputs are "<path> <roles>" of a camera.
func (c renderedConfig) inputs(id string) []string {
	out := []string{}
	for _, in := range c.Cameras[id].FFmpeg.Inputs {
		out = append(out, strings.TrimPrefix(in.Path, "rtsp://127.0.0.1:8554/")+" "+strings.Join(in.Roles, ","))
	}
	return out
}

// TestRenderStreamRoles: the recorded stream gets record, the detect
// stream detect; one stream that does both is one input (PLAN 82-2).
func TestRenderStreamRoles(t *testing.T) {
	for _, tc := range []struct {
		name string
		use  StreamUse
		sub  bool
		want []string
	}{
		{"default", StreamUse{}, true, []string{"cam record", "cam_sub detect"}},
		{"swapped", StreamUse{Record: StreamSub, Detect: StreamMain}, true, []string{"cam detect", "cam_sub record"}},
		{"both on main", StreamUse{Detect: StreamMain}, true, []string{"cam record,detect"}},
		{"both on sub", StreamUse{Record: StreamSub}, true, []string{"cam_sub record,detect"}},
		{"one stream", StreamUse{}, false, []string{"cam record,detect"}},
		{"one stream, detect sub asked", StreamUse{Detect: StreamSub}, false, []string{"cam record,detect"}},
	} {
		c := testCamera("cam", "Cam", func(c *Camera) {
			c.Streams = tc.use
			if !tc.sub {
				c.Source.SubURL = ""
			}
		})
		if got := render(t, []Camera{c}, nil, RenderHardware{}).inputs("cam"); !slices.Equal(got, tc.want) {
			t.Errorf("%s: inputs %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestRenderRecordCodec: the sound of the recording follows the codec of
// the stream that is recorded.
func TestRenderRecordCodec(t *testing.T) {
	for _, tc := range []struct {
		record     string
		main, sub  string
		audio      bool
		recordArgs string
	}{
		{StreamMain, codecAAC, "PCMA", true, "preset-record-generic-audio-copy"},
		{StreamSub, codecAAC, "PCMA", true, "preset-record-generic-audio-aac"},
		{StreamSub, "PCMA", codecAAC, true, "preset-record-generic-audio-copy"},
		{StreamSub, "PCMA", "", true, "preset-record-generic-audio-aac"},
		{StreamSub, codecAAC, codecAAC, false, "preset-record-generic"},
	} {
		c := testCamera("cam", "Cam", func(c *Camera) {
			c.Streams.Record = tc.record
			c.Audio = Audio{Enabled: tc.audio, Codec: tc.main, SubCodec: tc.sub}
		})
		if got := render(t, []Camera{c}, nil, RenderHardware{}).Cameras["cam"].FFmpeg.OutputArgs.Record; got != tc.recordArgs {
			t.Errorf("record %s, main %q, sub %q: %s, want %s", tc.record, tc.main, tc.sub, got, tc.recordArgs)
		}
	}
}

// TestDetectSize: detecting on a main stream wider than 1280 works at
// 1280 wide keeping the aspect (even height); unknown or small sizes and
// the sub stream are left to Frigate.
func TestDetectSize(t *testing.T) {
	for _, tc := range []struct {
		w, h   int
		detect string
		sub    bool
		want   [2]int // 0, 0: not set
	}{
		{3840, 2160, StreamMain, true, [2]int{1280, 720}},
		{2560, 1440, StreamMain, false, [2]int{1280, 720}},
		{2592, 1944, StreamMain, true, [2]int{1280, 960}},
		{2688, 1520, StreamMain, true, [2]int{1280, 724}}, // 723.8 → 724
		{3072, 1728, StreamMain, true, [2]int{1280, 720}},
		{1280, 720, StreamMain, true, [2]int{}},
		{1920, 1080, StreamSub, true, [2]int{}},
		{0, 0, StreamMain, true, [2]int{}},
		{3840, 0, StreamMain, true, [2]int{}},
	} {
		c := testCamera("cam", "Cam", func(c *Camera) {
			c.Streams.Detect = tc.detect
			c.Source.MainWidth, c.Source.MainHeight = tc.w, tc.h
			if !tc.sub {
				c.Source.SubURL = ""
			}
		})
		d := render(t, []Camera{c}, nil, RenderHardware{}).Cameras["cam"].Detect
		got := [2]int{}
		if d.Width != nil && d.Height != nil {
			got = [2]int{*d.Width, *d.Height}
		} else if d.Width != nil || d.Height != nil {
			t.Errorf("%d×%d: only one of width and height", tc.w, tc.h)
		}
		if got != tc.want {
			t.Errorf("%d×%d on %s: detect %v, want %v", tc.w, tc.h, tc.detect, got, tc.want)
		}
	}
}

// TestPreloadList: go2rtc keeps the main and sub stream of every enabled
// camera open unless it says otherwise; never the stream for talking
// back, a switched-off camera or a deleted one (PLAN 82-1).
func TestPreloadList(t *testing.T) {
	cameras := []Camera{
		testCamera("tuer", "Tür", func(c *Camera) { c.Order = 1; c.Audio = Audio{Enabled: true, TwoWay: true} }),
		testCamera("hof", "Hof", func(c *Camera) { c.Order = 2; c.Source.SubURL = "" }),
		testCamera("aus", "Aus", func(c *Camera) { c.Order = 3; c.Enabled = false }),
		testCamera("zu", "Zu", func(c *Camera) { c.Order = 4; c.Streams.NoPreload = true }),
	}
	deleted := []DeletedCamera{{ID: "alt", Name: "Alt", RetainDays: 10, DeletedAt: time.Now()}}
	cfg := render(t, cameras, deleted, RenderHardware{})
	if got, want := cfg.preloadKeys(), []string{"tuer", "tuer_sub", "hof"}; !slices.Equal(got, want) {
		t.Errorf("preload %v, want %v", got, want)
	}
	if _, ok := cfg.Go2RTC.Streams["tuer_twoway"]; !ok {
		t.Error("the stream for talking back is still there, only not kept open")
	}
	for i := 1; i < len(cfg.Go2RTC.Preload.Content); i += 2 {
		if v := cfg.Go2RTC.Preload.Content[i]; v.Value != "" || v.Tag != "!!str" {
			t.Errorf("a preload is %q (%s), want \"\"", v.Value, v.Tag)
		}
	}
	// Nothing to keep open: no key (go2rtc gets what it had before).
	if out, _ := Render(secretSettings(), []Camera{cameras[2]}, nil, RenderHardware{Detector: DetectorCPU}); strings.Contains(string(out), "preload") {
		t.Errorf("preload without a camera to keep open:\n%s", out)
	}
	if got := preloadStreams(nil); got == nil || len(got) != 0 {
		t.Errorf("no cameras: %#v", got)
	}
}

func TestLiveStreamFollowsTheChoice(t *testing.T) {
	c := testCamera("cam", "Cam", nil)
	if LiveStream(c, true) != "cam_sub" || LiveStream(c, false) != "cam" {
		t.Error("auto: sub for small tiles, main for a large picture")
	}
	c.Streams.Live = StreamMain
	if LiveStream(c, true) != "cam" {
		t.Error("live on main: also for small tiles")
	}
	c.Streams.Live = StreamSub
	if LiveStream(c, false) != "cam_sub" {
		t.Error("live on sub: also for a large picture")
	}
	c.Source.SubURL, c.Streams.Live = "", ""
	if LiveStream(c, true) != "cam" {
		t.Error("one stream: always main")
	}
}

func TestHWAccelArgs(t *testing.T) {
	for _, tc := range []struct {
		decode Decode
		want   any
	}{
		{Decode{}, ""},
		{Decode{Kind: DecodeVAAPI}, "preset-vaapi"},
		{Decode{Kind: DecodeNVIDIA}, "preset-nvidia"},
		{Decode{Kind: DecodeVAAPI, Node: "/dev/dri/renderD129"}, []any{"-hwaccel_flags", "allow_profile_mismatch", "-hwaccel", "vaapi", "-hwaccel_device", "/dev/dri/renderD129"}},
	} {
		got := render(t, []Camera{testCamera("cam", "Cam", nil)}, nil, RenderHardware{Decode: tc.decode}).FFmpeg.HWAccel
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(tc.want)
		if string(a) != string(b) {
			t.Errorf("%+v: hwaccel_args %s, want %s", tc.decode, a, b)
		}
		// A custom list never asks for frames to stay on the GPU: Frigate
		// then scales in software.
		if strings.Contains(string(a), "hwaccel_output_format") {
			t.Errorf("%+v: %s", tc.decode, a)
		}
	}
}

var (
	devIGPU2 = hardware.ContainerDevice{ID: "0000:04:00.0", Vendor: "8086", Kind: "igpu", Title: "Intel · 0000:04:00.0", Available: true, Mappings: []string{"/dev/dri/renderD130:/dev/dri/renderD130"}}
	devNV2   = hardware.ContainerDevice{ID: "0000:02:00.0", Vendor: "10de", Kind: "nvidia", Title: "NVIDIA · 0000:02:00.0", Available: true, UUID: "GPU-2", Mappings: []string{"nvidia.com/gpu=GPU-2"}}
)

// TestDecoderChoice: auto takes an iGPU, else another Intel/AMD GPU,
// else NVIDIA, else the CPU; a chosen GPU that is gone, with a VM or
// without driver is never replaced by another GPU (PLAN 82-4).
func TestDecoderChoice(t *testing.T) {
	nvNoDriver := devNV
	nvNoDriver.Available, nvNoDriver.Mappings, nvNoDriver.Reason = false, []string{}, "driver"
	for _, tc := range []struct {
		name    string
		devices []hardware.ContainerDevice
		leased  map[string]bool
		setting string
		active  string
		reason  string
	}{
		{"auto takes the iGPU before an AMD GPU and NVIDIA", []hardware.ContainerDevice{devNV, devAMD, devIGPU}, nil, DecoderAuto, devIGPU.ID, ""},
		{"empty is auto", []hardware.ContainerDevice{devNV, devAMD, devIGPU}, nil, "", devIGPU.ID, ""},
		{"auto without iGPU: the AMD GPU", []hardware.ContainerDevice{devNV, devAMD}, nil, DecoderAuto, devAMD.ID, ""},
		{"auto: the iGPU with a VM is passed over", []hardware.ContainerDevice{devNV, devAMD, devIGPU}, map[string]bool{devIGPU.ID: true}, DecoderAuto, devAMD.ID, ""},
		{"auto never takes NVIDIA", []hardware.ContainerDevice{devNV}, nil, DecoderAuto, DecoderCPU, ""},
		{"auto: NVIDIA beside a VM's iGPU is not taken either", []hardware.ContainerDevice{devNV, devIGPU}, map[string]bool{devIGPU.ID: true}, DecoderAuto, DecoderCPU, ""},
		{"auto: the first of two iGPUs", []hardware.ContainerDevice{devIGPU, devIGPU2}, nil, DecoderAuto, devIGPU.ID, ""},
		{"auto: nothing", []hardware.ContainerDevice{devNPU, nvNoDriver}, nil, DecoderAuto, DecoderCPU, ""},
		{"cpu", []hardware.ContainerDevice{devIGPU}, nil, DecoderCPU, DecoderCPU, ""},
		{"named NVIDIA", []hardware.ContainerDevice{devIGPU, devNV}, nil, devNV.ID, devNV.ID, ""},
		{"named AMD beside the iGPU", []hardware.ContainerDevice{devIGPU, devAMD}, nil, devAMD.ID, devAMD.ID, ""},
		{"named, with a VM: the CPU, not the iGPU", []hardware.ContainerDevice{devIGPU, devNV}, map[string]bool{devNV.ID: true}, devNV.ID, DecoderCPU, "vm"},
		{"named, no driver", []hardware.ContainerDevice{devIGPU, nvNoDriver}, nil, devNV.ID, DecoderCPU, "driver"},
		{"named, gone", []hardware.ContainerDevice{devIGPU}, nil, devNV.ID, DecoderCPU, "gone"},
	} {
		h := probeHardware(tc.devices, tc.leased, fullHost)
		dec, st := h.decoder(tc.setting)
		if st.Active != tc.active || st.Reason != tc.reason || (dec == nil) != (tc.active == DecoderCPU) {
			t.Errorf("%s: %+v (decoder %v), want %s reason %q", tc.name, st, dec != nil, tc.active, tc.reason)
		}
		if dec != nil && (dec.ID != st.Active || dec.Kind != st.Kind) {
			t.Errorf("%s: decoder %+v, status %+v", tc.name, dec, st)
		}
		if tc.setting == "" && st.Setting != DecoderAuto {
			t.Errorf("%s: setting %q", tc.name, st.Setting)
		}
	}
	// The list names every GPU with its state, NPU and Coral are none.
	h := probeHardware([]hardware.ContainerDevice{devIGPU, devNPU, devCoral, devAMD, nvNoDriver}, map[string]bool{devAMD.ID: true}, fullHost)
	got := []string{}
	for _, d := range h.Decoders {
		got = append(got, d.ID+" "+d.Kind+" "+d.Reason)
	}
	if want := []string{"0000:00:02.0 igpu ", "0000:03:00.0 gpu vm", "0000:01:00.0 nvidia driver"}; !slices.Equal(got, want) {
		t.Errorf("decoders %q, want %q", got, want)
	}
	if data, _ := json.Marshal(probeHardware(nil, nil, fullHost)); !strings.Contains(string(data), `"decoders":[]`) {
		t.Errorf("no GPU: %s", data)
	}
}

// TestDecoderDevices: the container gets the decoder and the detector's
// own nodes, nothing else; two render nodes name the decoder's.
func TestDecoderDevices(t *testing.T) {
	all := []hardware.ContainerDevice{devIGPU, devNPU, devAMD, devNV}
	for _, tc := range []struct {
		name     string
		setting  string
		detector string
		nodes    []string
		cdi      []string
		decode   Decode
	}{
		{"iGPU decodes and detects", DecoderAuto, DetectorIGPU, []string{"/dev/dri/renderD128"}, nil, Decode{Kind: DecodeVAAPI}},
		{"AMD decodes, iGPU detects: the node is named", devAMD.ID, DetectorIGPU, []string{"/dev/dri/renderD128", "/dev/dri/renderD129"}, nil, Decode{Kind: DecodeVAAPI, Node: "/dev/dri/renderD129"}},
		{"AMD decodes, NPU detects", devAMD.ID, DetectorNPU, []string{"/dev/accel/accel0", "/dev/dri/renderD129"}, nil, Decode{Kind: DecodeVAAPI}},
		{"NVIDIA decodes, iGPU detects", devNV.ID, DetectorIGPU, []string{"/dev/dri/renderD128"}, []string{"nvidia.com/gpu=GPU-1"}, Decode{Kind: DecodeNVIDIA}},
		{"NVIDIA decodes, CPU detects", devNV.ID, DetectorCPU, []string{}, []string{"nvidia.com/gpu=GPU-1"}, Decode{Kind: DecodeNVIDIA}},
		{"CPU decodes, iGPU detects", DecoderCPU, DetectorIGPU, []string{"/dev/dri/renderD128"}, nil, Decode{}},
		{"CPU decodes and detects", DecoderCPU, DetectorCPU, []string{}, nil, Decode{}},
	} {
		h := probeHardware(all, nil, fullHost)
		dec, _ := h.decoder(tc.setting)
		nodes, cdi, _, decode := h.containerDevices(tc.detector, dec)
		if !slices.Equal(nodes, tc.nodes) || !slices.Equal(cdi, tc.cdi) || decode != tc.decode {
			t.Errorf("%s: nodes %v cdi %v decode %+v; want %v %v %+v", tc.name, nodes, cdi, decode, tc.nodes, tc.cdi, tc.decode)
		}
	}
}

// TestPlanDecoder: the plan passes the decoder's CDI device; the hash of
// the container changes with it and stays as it was without one.
func TestPlanDecoder(t *testing.T) {
	m := New(config.Open(t.TempDir()))
	devices := []hardware.ContainerDevice{devIGPU, devNV}
	s := Settings{Enabled: true, Volume: "tank", Detector: DetectorSetting{Kind: DetectorCPU}, ProxySecret: testSecret}
	cameras := []Camera{testCamera("cam", "Cam", nil)}
	plan := func(s Settings, leased map[string]bool) plan {
		t.Helper()
		p, err := m.makePlan(s, cameras, nil, probeHardware(devices, leased, fullHost), "sha256:x", "/x", nil)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	auto := plan(s, nil)
	if !slices.Equal(auto.spec.Devices, []string{"/dev/dri/renderD128"}) || len(auto.spec.CDIDevices) != 0 || auto.decoder.Active != devIGPU.ID {
		t.Errorf("auto: %+v %+v", auto.spec, auto.decoder)
	}
	if !strings.Contains(string(auto.config), "hwaccel_args: preset-vaapi") {
		t.Error("auto: preset-vaapi")
	}
	if described, _ := json.Marshal(auto.spec); strings.Contains(string(described), "CDIDevices") {
		t.Errorf("an empty CDI list changes the hash of every container made before it: %s", described)
	}
	if !slices.Equal(auto.preload, []string{"cam", "cam_sub"}) {
		t.Errorf("preload %v", auto.preload)
	}
	s.Decoder = devNV.ID
	nv := plan(s, nil)
	if len(nv.spec.Devices) != 0 || !slices.Equal(nv.spec.CDIDevices, []string{"nvidia.com/gpu=GPU-1"}) || nv.spec.Hash == auto.spec.Hash {
		t.Errorf("nvidia: %+v", nv.spec)
	}
	if !strings.Contains(string(nv.config), "hwaccel_args: preset-nvidia") {
		t.Errorf("nvidia: %s", nv.config)
	}
	// The NVIDIA GPU goes to a VM: the CPU decodes, never the iGPU.
	vm := plan(s, map[string]bool{devNV.ID: true})
	if len(vm.spec.Devices) != 0 || len(vm.spec.CDIDevices) != 0 || vm.decoder.Reason != "vm" || !strings.Contains(string(vm.config), `hwaccel_args: ""`) {
		t.Errorf("nvidia with a VM: %+v %+v", vm.spec, vm.decoder)
	}
}

// configOn reads config.yml of a volumeRig's volume.
func (r *rig) configOn(volume string) string {
	r.t.Helper()
	return volumeConfig(r.t, r.m.MountRoot, volume)
}

// TestProbeMemory: what a check found per stream goes into the camera
// that is saved after it – codecs and sizes of both streams – never what
// the request says; without a fresh check a stream keeps what it had,
// also when main and sub swap places; an old check counts no more.
func TestProbeMemory(t *testing.T) {
	r := newRig(t, true)
	m := r.m
	ctx := context.Background()
	res, err := m.Probe(ctx, ProbeRequest{Kind: "onvif", Host: "192.168.1.70", User: "admin", Password: "x"})
	if err != nil || res.Outcome != OutcomeOK {
		t.Fatalf("%+v %v", res, err)
	}
	// Four profiles, three streams: each address once.
	urls := []string{}
	for _, s := range res.Streams {
		urls = append(urls, s.Role+" "+strings.TrimPrefix(s.URL, "rtsp://192.168.1.70:554/"))
	}
	if want := []string{"main main", "sub sub", "other third"}; !slices.Equal(urls, want) {
		t.Errorf("streams %v, want %v", urls, want)
	}
	c := NewCamera()
	c.Name = "Neu"
	c.Source = Source{Kind: "onvif", Host: "192.168.1.70", User: "admin", Password: "x", MainURL: res.MainURL, SubURL: res.SubURL}
	// What the page might send is never taken.
	c.Audio.Codec, c.Audio.SubCodec, c.Source.MainWidth, c.Source.SubHeight = "PCMU", "PCMU", 99, 99
	added, err := m.AddCamera(ctx, c, CameraOptions{})
	if err != nil {
		t.Fatal(err)
	}
	s := added.Source
	if added.Audio.Codec != codecAAC || added.Audio.SubCodec != "" || s.MainWidth != 3840 || s.MainHeight != 2160 || s.SubWidth != 640 || s.SubHeight != 360 {
		t.Errorf("added: audio %+v, source %+v", added.Audio, s)
	}
	// An hour later the check counts no more; the addresses swap places:
	// each keeps what it had.
	r.advance(2 * time.Hour)
	added.Source.MainURL, added.Source.SubURL = added.Source.SubURL, added.Source.MainURL
	added.Source.Password = ""
	swapped, err := m.UpdateCamera(ctx, added.ID, added, CameraOptions{})
	if err != nil {
		t.Fatal(err)
	}
	s = swapped.Source
	if swapped.Audio.Codec != "" || swapped.Audio.SubCodec != codecAAC || s.MainWidth != 640 || s.SubWidth != 3840 || s.SubHeight != 2160 {
		t.Errorf("swapped: audio %+v, source %+v", swapped.Audio, s)
	}
	// Another address without a check: unknown.
	swapped.Source.SubURL = "rtsp://192.168.1.70:554/third"
	other, err := m.UpdateCamera(ctx, added.ID, swapped, CameraOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if other.Audio.SubCodec != "" || other.Source.SubWidth != 0 || other.Source.SubHeight != 0 || other.Source.MainWidth != 640 {
		t.Errorf("unchecked address: audio %+v, source %+v", other.Audio, other.Source)
	}
	// A fresh check of it: its sound (G.711) and size.
	if _, err := m.Probe(ctx, ProbeRequest{Kind: "onvif", Host: "192.168.1.70", Camera: added.ID}); err != nil {
		t.Fatal(err)
	}
	checked, err := m.UpdateCamera(ctx, added.ID, other, CameraOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if checked.Audio.SubCodec != "PCMA" || checked.Source.SubWidth != 1280 || checked.Source.SubHeight != 720 {
		t.Errorf("checked: audio %+v, source %+v", checked.Audio, checked.Source)
	}
}

// TestProbeKeepsAStoredSize: a check whose stream description names no
// size leaves the size stored for the same address.
func TestProbeKeepsAStoredSize(t *testing.T) {
	m := New(config.Open(t.TempDir()))
	was := testCamera("cam", "Cam", func(c *Camera) { c.Source.MainWidth, c.Source.MainHeight, c.Audio.Codec = 1920, 1080, "PCMA" })
	m.noteProbe(was.Source.MainURL, StreamInfo{Outcome: OutcomeOK, Codec: "H264", Audio: codecAAC})
	m.noteProbe(was.Source.SubURL, StreamInfo{Outcome: OutcomeUnauthorized})
	c := was
	m.mu.Lock()
	m.streamFactsLocked(&c, &was)
	m.mu.Unlock()
	if c.Audio.Codec != codecAAC || c.Source.MainWidth != 1920 || c.Source.MainHeight != 1080 {
		t.Errorf("%+v %+v", c.Audio, c.Source)
	}
	if _, ok := m.probed[was.Source.SubURL]; ok {
		t.Error("a refused check is noted")
	}
}

func TestProbeTargets(t *testing.T) {
	profiles := []Profile{
		{Name: "a", URL: "rtsp://h/1", Width: 1920, Height: 1080, Codec: "H264"},
		{Name: "b", URL: "rtsp://h/2"},
		{Name: "a2", URL: "rtsp://h/1"},
	}
	for i := 3; i < 10; i++ {
		profiles = append(profiles, Profile{Name: "p", URL: "rtsp://h/" + string(rune('0'+i))})
	}
	got := probeTargets("rtsp://h/2", "rtsp://h/9", profiles)
	list := []string{}
	for _, target := range got {
		list = append(list, target.Role+" "+target.URL)
	}
	want := []string{"main rtsp://h/2", "sub rtsp://h/9", "other rtsp://h/1", "other rtsp://h/3", "other rtsp://h/4", "other rtsp://h/5"}
	if !slices.Equal(list, want) {
		t.Errorf("targets %v, want %v", list, want)
	}
	if got[2].Profile != "a" || got[2].profile.Width != 1920 {
		t.Errorf("profile of %s: %+v", got[2].URL, got[2])
	}
	// The size and codec of the profile fill what the description left out.
	info := withProfile(StreamInfo{Outcome: OutcomeOK, Audio: "PCMA"}, got[2].profile)
	if info.Width != 1920 || info.Height != 1080 || info.Codec != "H264" || info.Audio != "PCMA" {
		t.Errorf("%+v", info)
	}
	if info := withProfile(StreamInfo{Outcome: OutcomeOK, Width: 640, Height: 360, Codec: "H265"}, got[2].profile); info.Width != 640 || info.Codec != "H265" {
		t.Errorf("the description wins: %+v", info)
	}
	if info := withProfile(StreamInfo{Outcome: OutcomeUnauthorized}, got[2].profile); info.Width != 0 {
		t.Errorf("a refused stream: %+v", info)
	}
}

// waitPreload waits until a round of the preload watch ended.
func (r *rig) waitPreload() {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.m.mu.Lock()
		busy := r.m.rt.preload.busy
		r.m.mu.Unlock()
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatal("the preload round did not end")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// puts are the streams the watch asked go2rtc to open again.
func puts(execs [][]string) []string {
	out := []string{}
	for _, cmd := range execs {
		if slices.Contains(cmd, "PUT") {
			_, src, _ := strings.Cut(cmd[len(cmd)-1], "?src=")
			out = append(out, src)
		}
	}
	return out
}

// TestPreloadRearm: while Frigate runs the watch asks go2rtc every two
// minutes which streams it keeps open and asks it again for a missing
// one, at most every five minutes, never for one that is open or not to
// be kept open; the answers are never logged (PLAN 82-1).
func TestPreloadRearm(t *testing.T) {
	r := newRig(t, true)
	r.up()
	// The demo cameras: garage is switched off, haustuer talks back.
	if got, want := r.engine.Preloads(), []string{"einfahrt", "einfahrt_sub", "garten", "garten_sub", "haustuer", "haustuer_sub"}; !slices.Equal(got, want) {
		t.Fatalf("go2rtc keeps %v open, want %v", got, want)
	}
	// The camera of "einfahrt" was away when go2rtc connected.
	r.engine.Away("einfahrt", "einfahrt_sub")
	// Not in the first two minutes after the start: go2rtc still connects.
	r.advance(time.Minute)
	r.look()
	r.waitPreload()
	if n := len(r.engine.Execs()); n != 0 {
		t.Fatalf("asked go2rtc %d times within a minute of the start", n)
	}
	step := func() []string {
		t.Helper()
		before := len(r.engine.Execs())
		r.advance(preloadEvery)
		r.look()
		r.waitPreload()
		return puts(r.engine.Execs()[before:])
	}
	if got := step(); !slices.Equal(got, []string{"einfahrt", "einfahrt_sub"}) {
		t.Errorf("first round asked again for %v", got)
	}
	// The camera still does not answer: not again within five minutes.
	if got := step(); len(got) != 0 {
		t.Errorf("asked again after 2 minutes: %v", got)
	}
	if got := step(); len(got) != 0 {
		t.Errorf("asked again after 4 minutes: %v", got)
	}
	if got := step(); !slices.Equal(got, []string{"einfahrt", "einfahrt_sub"}) {
		t.Errorf("after 6 minutes: %v", got)
	}
	// It answers again: the next ask opens it, and then it is left alone.
	r.engine.Back("einfahrt", "einfahrt_sub")
	step()
	step()
	step()
	if got := r.engine.Preloads(); !slices.Contains(got, "einfahrt") || !slices.Contains(got, "einfahrt_sub") {
		t.Errorf("go2rtc keeps %v open", got)
	}
	if got := step(); len(got) != 0 {
		t.Errorf("an open stream asked for again: %v", got)
	}
	for _, cmd := range r.engine.Execs() {
		if cmd[0] != "curl" || !strings.HasPrefix(cmd[len(cmd)-1], "http://127.0.0.1:1984/api/preload") {
			t.Errorf("command %q", cmd)
		}
		for _, arg := range cmd {
			if strings.Contains(arg, "twoway") || strings.Contains(arg, "garage") || strings.Contains(arg, "rtsp") {
				t.Errorf("command %q", cmd)
			}
		}
	}
	// A camera that is no longer kept open is forgotten.
	r.engine.Away("garten")
	c, _ := r.m.Camera("garten")
	c.Streams.NoPreload = true
	c.Source.Password = ""
	if _, err := r.m.UpdateCamera(context.Background(), "garten", c, CameraOptions{}); err != nil {
		t.Fatal(err)
	}
	r.look() // the config changes, Frigate restarts
	r.look()
	r.advance(preloadEvery)
	r.look()
	r.waitPreload()
	if got := step(); slices.Contains(got, "garten") {
		t.Errorf("a stream not to be kept open asked for: %v", got)
	}
}

// TestPreloadRoundRateLimit: the round itself, with a broken answer of
// go2rtc and a failing engine.
func TestPreloadRoundRateLimit(t *testing.T) {
	r := newRig(t, true)
	r.up()
	ctx := context.Background()
	r.engine.Away("haustuer")
	r.m.preloadRound(ctx, []string{"haustuer", "haustuer_sub"})
	r.m.preloadRound(ctx, []string{"haustuer", "haustuer_sub"})
	if got := puts(r.engine.Execs()); !slices.Equal(got, []string{"haustuer"}) {
		t.Errorf("two rounds at once asked for %v", got)
	}
	r.advance(rearmEvery)
	r.m.preloadRound(ctx, []string{"haustuer", "haustuer_sub"})
	if got := puts(r.engine.Execs()); !slices.Equal(got, []string{"haustuer", "haustuer"}) {
		t.Errorf("after five minutes: %v", got)
	}
	// The container went: nothing is asked, nothing breaks.
	r.engine.Exit(0)
	before := len(r.engine.Execs())
	r.advance(rearmEvery)
	r.m.preloadRound(ctx, []string{"haustuer"})
	if len(r.engine.Execs()) != before {
		t.Error("asked a container that does not run")
	}
}
