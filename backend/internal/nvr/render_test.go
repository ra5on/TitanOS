package nvr

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

func testCamera(id, name string, edit func(*Camera)) Camera {
	c := NewCamera()
	c.ID, c.Name = id, name
	c.Source = Source{Kind: "rtsp", User: "viewer", Password: "geheim", MainURL: "rtsp://192.168.1.61:554/Streaming/Channels/101", SubURL: "rtsp://192.168.1.61:554/Streaming/Channels/102"}
	if edit != nil {
		edit(&c)
	}
	valid, err := ValidateCamera(c)
	if err != nil {
		panic(err)
	}
	return valid
}

// renderCases are the golden files: one per detector, with the camera
// options spread over them.
func renderCases() map[string]struct {
	settings Settings
	cameras  []Camera
	hw       RenderHardware
} {
	type tc = struct {
		settings Settings
		cameras  []Camera
		hw       RenderHardware
	}
	return map[string]tc{
		// NPU, iGPU decodes: two cameras, one with sound, talking back and PTZ.
		"npu": {Defaults(), []Camera{
			testCamera("haustuer", "Haustür", func(c *Camera) {
				c.Order = 1
				c.Audio = Audio{Enabled: true, TwoWay: true}
				c.Detect.Objects = []string{"dog", "person"}
			}),
			testCamera("garten", "Garten", func(c *Camera) {
				c.Order = 2
				c.Source = Source{Kind: "onvif", Host: "192.168.1.63", ONVIFPort: 8000, User: "admin", Password: "p@ss/w:rd {x}",
					MainURL: "rtsp://192.168.1.63:554/main", SubURL: "rtsp://192.168.1.63:554/sub"}
				c.PTZ = true
				c.Record = Record{Mode: RecordContinuous, RetainDays: 7, PreSeconds: 10, PostSeconds: 20}
			}),
		}, RenderHardware{Detector: DetectorNPU, VAAPI: true, Candidates: []string{"192.168.1.10:8555"}}},
		// iGPU with plates and faces; the restream is published.
		"igpu": {Settings{Detector: DetectorSetting{Kind: DetectorIGPU}, RTSPOut: true, RTSPPassword: "abcdefgh234567"}, []Camera{
			testCamera("einfahrt", "Einfahrt", func(c *Camera) {
				c.Detect.Plates = true
				c.Detect.Objects = []string{"person"}
				c.Record.Mode = RecordMotion
			}),
			testCamera("flur", "Flur", func(c *Camera) {
				c.Order = 5
				c.Detect.Faces = true
				c.Detect.Objects = []string{"cat"}
				c.Source.SubURL = ""
			}),
		}, RenderHardware{Detector: DetectorIGPU, VAAPI: true}},
		// Coral, no GPU: a camera without login, one switched off, one that does not record.
		"coral": {Defaults(), []Camera{
			testCamera("hof", "Hof", func(c *Camera) { c.Source.User, c.Source.Password = "", "" }),
			testCamera("keller", "Keller", func(c *Camera) { c.Enabled = false }),
			testCamera("carport", "Carport", func(c *Camera) { c.Record.Mode = RecordOff; c.Detect.Enabled = false }),
		}, RenderHardware{Detector: DetectorCoral}},
		// CPU and no camera yet: what Frigate first starts with.
		"cpu":   {Defaults(), nil, RenderHardware{Detector: DetectorCPU}},
		"hailo": {Defaults(), []Camera{testCamera("tor", "Tor", nil)}, RenderHardware{Detector: DetectorHailo, VAAPI: true}},
	}
}

func TestRenderGolden(t *testing.T) {
	for name, tc := range renderCases() {
		t.Run(name, func(t *testing.T) {
			got, err := Render(tc.settings, tc.cameras, tc.hw)
			if err != nil {
				t.Fatal(err)
			}
			// The file must be YAML that reads back.
			var parsed map[string]any
			if err := yaml.Unmarshal(got, &parsed); err != nil {
				t.Fatalf("not YAML: %v\n%s", err, got)
			}
			golden := filepath.Join("testdata", name+".yml")
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("config differs from %s (go test ./internal/nvr -run TestRenderGolden -update rewrites it):\n%s", golden, got)
			}
		})
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	tc := renderCases()["npu"]
	first, err := Render(tc.settings, tc.cameras, tc.hw)
	if err != nil {
		t.Fatal(err)
	}
	reversed := slices.Clone(tc.cameras)
	slices.Reverse(reversed)
	for range 5 {
		again, err := Render(tc.settings, reversed, tc.hw)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, again) {
			t.Fatal("the same cameras in another order give another file")
		}
	}
}

func parse(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	return parsed
}

func dig(t *testing.T, v any, path ...string) any {
	t.Helper()
	for _, key := range path {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("%v: not a mapping at %q", path, key)
		}
		v = m[key]
	}
	return v
}

func TestRenderFixedParts(t *testing.T) {
	tc := renderCases()["npu"]
	data, err := Render(tc.settings, tc.cameras, tc.hw)
	if err != nil {
		t.Fatal(err)
	}
	cfg := parse(t, data)
	for _, check := range []struct {
		path []string
		want any
	}{
		{[]string{"mqtt", "enabled"}, false},
		{[]string{"auth", "enabled"}, false},
		{[]string{"tls", "enabled"}, false},
		{[]string{"telemetry", "version_check"}, false},
		// Frigate treats a file without this as one of 0.13 and rewrites it.
		{[]string{"version"}, "0.18-0"},
		// 8971 only inside the container.
		{[]string{"networking", "listen", "external"}, "127.0.0.1:8971"},
		{[]string{"detectors", "ov", "type"}, "openvino"},
		{[]string{"detectors", "ov", "device"}, "NPU"},
		{[]string{"model", "path"}, "/openvino-model/ssdlite_mobilenet_v2.xml"},
		{[]string{"ffmpeg", "hwaccel_args"}, "preset-vaapi"},
		{[]string{"lpr", "enabled"}, false},
		{[]string{"face_recognition", "enabled"}, false},
		{[]string{"cameras", "haustuer", "ffmpeg", "output_args", "record"}, "preset-record-generic-audio-copy"},
		{[]string{"cameras", "garten", "ffmpeg", "output_args", "record"}, "preset-record-generic"},
		{[]string{"cameras", "garten", "onvif", "port"}, 8000},
		// Braces are doubled where Frigate substitutes {FRIGATE_…}.
		{[]string{"cameras", "garten", "onvif", "password"}, "p@ss/w:rd {{x}}"},
		{[]string{"cameras", "garten", "record", "continuous", "days"}, 7},
		{[]string{"cameras", "garten", "record", "alerts", "retain", "mode"}, "all"},
		{[]string{"cameras", "garten", "record", "alerts", "pre_capture"}, 10},
		{[]string{"cameras", "garten", "record", "detections", "post_capture"}, 20},
		{[]string{"cameras", "haustuer", "record", "continuous", "days"}, 0},
		{[]string{"cameras", "haustuer", "record", "motion", "days"}, 0},
		{[]string{"cameras", "haustuer", "record", "alerts", "retain", "mode"}, "active_objects"},
		{[]string{"cameras", "haustuer", "detect", "fps"}, 5},
	} {
		if got := dig(t, cfg, check.path...); got != check.want {
			t.Errorf("%v = %#v, want %#v", check.path, got, check.want)
		}
	}
	streams := dig(t, cfg, "go2rtc", "streams").(map[string]any)
	// The password is URL-escaped; the watching streams give the audio
	// output back, the one for talking back carries no option at all.
	wantStreams := map[string][]any{
		"haustuer":        {"rtsp://viewer:geheim@192.168.1.61:554/Streaming/Channels/101#backchannel=0", "ffmpeg:haustuer#audio=opus"},
		"haustuer_sub":    {"rtsp://viewer:geheim@192.168.1.61:554/Streaming/Channels/102#backchannel=0"},
		"haustuer_twoway": {"rtsp://viewer:geheim@192.168.1.61:554/Streaming/Channels/101"},
		"garten":          {"rtsp://admin:p%40ss%2Fw%3Ard%20%7Bx%7D@192.168.1.63:554/main#media=video#backchannel=0"},
		"garten_sub":      {"rtsp://admin:p%40ss%2Fw%3Ard%20%7Bx%7D@192.168.1.63:554/sub#media=video#backchannel=0"},
	}
	if len(streams) != len(wantStreams) {
		t.Errorf("streams: %v", streams)
	}
	for name, want := range wantStreams {
		if got, _ := streams[name].([]any); !slices.Equal(got, want) {
			t.Errorf("stream %s = %v, want %v", name, got, want)
		}
	}
	// Frigate runs str.format() over the streams: no brace may be left.
	for name, sources := range streams {
		for _, source := range sources.([]any) {
			if strings.ContainsAny(source.(string), "{}") {
				t.Errorf("stream %s holds a brace: %v", name, source)
			}
		}
	}
	if got := dig(t, cfg, "go2rtc", "webrtc", "candidates").([]any); len(got) != 1 || got[0] != "192.168.1.10:8555" {
		t.Errorf("candidates = %v", got)
	}
	inputs := dig(t, cfg, "cameras", "haustuer", "ffmpeg", "inputs").([]any)
	if len(inputs) != 2 || dig(t, inputs[0], "path") != "rtsp://127.0.0.1:8554/haustuer" || dig(t, inputs[1], "path") != "rtsp://127.0.0.1:8554/haustuer_sub" ||
		dig(t, inputs[1], "input_args") != "preset-rtsp-restream" || !slices.Equal(dig(t, inputs[0], "roles").([]any), []any{"record"}) || !slices.Equal(dig(t, inputs[1], "roles").([]any), []any{"detect"}) {
		t.Errorf("inputs = %v", inputs)
	}
}

func TestRenderEnrichmentsAndDevices(t *testing.T) {
	tc := renderCases()["igpu"]
	data, err := Render(tc.settings, tc.cameras, tc.hw)
	if err != nil {
		t.Fatal(err)
	}
	cfg := parse(t, data)
	for _, check := range []struct {
		path []string
		want any
	}{
		{[]string{"detectors", "ov", "device"}, "GPU"},
		{[]string{"lpr", "enabled"}, true},
		{[]string{"face_recognition", "enabled"}, true},
		{[]string{"cameras", "einfahrt", "lpr", "enabled"}, true},
		{[]string{"cameras", "einfahrt", "face_recognition", "enabled"}, false},
		{[]string{"cameras", "flur", "lpr", "enabled"}, false},
		{[]string{"cameras", "flur", "face_recognition", "enabled"}, true},
		{[]string{"cameras", "einfahrt", "record", "motion", "days"}, 14},
		{[]string{"cameras", "einfahrt", "record", "alerts", "retain", "mode"}, "motion"},
		{[]string{"go2rtc", "rtsp", "username"}, "titanos"},
		{[]string{"go2rtc", "rtsp", "password"}, "abcdefgh234567"},
	} {
		if got := dig(t, cfg, check.path...); got != check.want {
			t.Errorf("%v = %#v, want %#v", check.path, got, check.want)
		}
	}
	// Plates need the car, faces the person, chosen or not.
	if got := dig(t, cfg, "cameras", "einfahrt", "objects", "track").([]any); !slices.Equal(got, []any{"person", "car"}) {
		t.Errorf("einfahrt tracks %v", got)
	}
	if got := dig(t, cfg, "cameras", "flur", "objects", "track").([]any); !slices.Equal(got, []any{"person", "cat"}) {
		t.Errorf("flur tracks %v", got)
	}
	// One stream: it records and detects.
	inputs := dig(t, cfg, "cameras", "flur", "ffmpeg", "inputs").([]any)
	if len(inputs) != 1 || !slices.Equal(dig(t, inputs[0], "roles").([]any), []any{"record", "detect"}) {
		t.Errorf("inputs = %v", inputs)
	}
	// An empty list, never missing: Frigate would add a public STUN server.
	if got, ok := dig(t, cfg, "go2rtc", "webrtc", "candidates").([]any); !ok || len(got) != 0 {
		t.Errorf("candidates = %#v", dig(t, cfg, "go2rtc", "webrtc", "candidates"))
	}

	coral := renderCases()["coral"]
	data, err = Render(coral.settings, coral.cameras, coral.hw)
	if err != nil {
		t.Fatal(err)
	}
	cfg = parse(t, data)
	if dig(t, cfg, "detectors", "coral", "type") != "edgetpu" || dig(t, cfg, "detectors", "coral", "device") != "usb" {
		t.Errorf("detectors = %v", cfg["detectors"])
	}
	if _, has := cfg["model"]; has {
		t.Error("the Coral uses the model of the image: no model block")
	}
	// No GPU: explicitly none ("auto" would be Frigate's default).
	if got := dig(t, cfg, "ffmpeg", "hwaccel_args"); got != "" {
		t.Errorf("hwaccel_args = %#v", got)
	}
	if dig(t, cfg, "cameras", "keller", "enabled") != false || dig(t, cfg, "cameras", "carport", "record", "enabled") != false ||
		dig(t, cfg, "cameras", "carport", "detect", "enabled") != false || dig(t, cfg, "cameras", "carport", "snapshots", "enabled") != false {
		t.Errorf("cameras = %v", cfg["cameras"])
	}
	if got := dig(t, cfg, "go2rtc", "streams", "hof").([]any); got[0] != "rtsp://192.168.1.61:554/Streaming/Channels/101#media=video#backchannel=0" {
		t.Errorf("stream without login = %v", got)
	}

	hailo := renderCases()["hailo"]
	data, _ = Render(hailo.settings, hailo.cameras, hailo.hw)
	cfg = parse(t, data)
	if dig(t, cfg, "detectors", "hailo", "type") != "hailo8l" || dig(t, cfg, "model", "labelmap_path") != "/labelmap/coco-80.txt" || dig(t, cfg, "model", "width") != 320 {
		t.Errorf("hailo: %v %v", cfg["detectors"], cfg["model"])
	}
}

func TestRenderRefusesWhatFrigateWouldMisread(t *testing.T) {
	if _, err := Render(Defaults(), nil, RenderHardware{Detector: DetectorAuto}); err == nil {
		t.Error("auto is no device")
	}
	bad := testCamera("a", "A", nil)
	bad.Source.MainURL = "rtsp://192.168.1.2/{FRIGATE_X}"
	if _, err := Render(Defaults(), []Camera{bad}, RenderHardware{Detector: DetectorCPU}); err == nil {
		t.Error("a brace in an address must be refused")
	}
}
