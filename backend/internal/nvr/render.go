package nvr

import (
	"bytes"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// RenderHardware is what the config depends on besides settings and
// cameras: the device the detection runs on now and what the container
// was given.
type RenderHardware struct {
	// Detector is the device in use: npu, igpu, coral, hailo or cpu
	// (never auto: the manager resolved it).
	Detector string
	// VAAPI: a render node of an Intel or AMD GPU is passed to the
	// container, so ffmpeg decodes on it.
	VAAPI bool
	// Candidates are the addresses browsers reach WebRTC at
	// ("192.168.1.10:8555"), for talking back through a camera.
	Candidates []string
}

// frigateConfigVersion is CURRENT_CONFIG_VERSION of Frigate 0.18
// (frigate/util/config.py); it changes with FrigateVersion.
const frigateConfigVersion = "0.18-0"

// The header of the file: whoever opens it should know it is overwritten.
const renderHeader = "# Written by TitanOS (NVR). Do not edit: every change of the settings\n# or cameras in TitanOS replaces this file.\n"

// ymap is a YAML mapping that keeps its order.
type ymap []yentry

type yentry struct {
	key   string
	value any
}

func (m ymap) MarshalYAML() (any, error) {
	node := &yaml.Node{Kind: yaml.MappingNode}
	for _, e := range m {
		var key, value yaml.Node
		if err := key.Encode(e.key); err != nil {
			return nil, err
		}
		if err := value.Encode(e.value); err != nil {
			return nil, err
		}
		node.Content = append(node.Content, &key, &value)
	}
	return node, nil
}

// Render makes Frigate's config.yml (version 0.18) from the settings and
// cameras. The same input always gives the same bytes: cameras are sorted
// (Order, ID) and every mapping has a fixed order, so the file is only
// rewritten (and Frigate only restarted) when something changed. The file
// holds the cameras' passwords: it is written 0600 into the private folder
// of the NVR and never logged.
func Render(s Settings, cameras []Camera, hw RenderHardware) ([]byte, error) {
	detectors, model, err := detectorBlocks(hw.Detector)
	if err != nil {
		return nil, err
	}
	cameras = slices.Clone(cameras)
	sortCameras(cameras)

	hwaccel := ""
	if hw.VAAPI {
		// preset-vaapi finds the codec itself; the QSV presets Frigate
		// suggests for newer Intel chips are per codec (h264/h265), which
		// TitanOS does not know before the camera answers.
		hwaccel = "preset-vaapi"
	}
	anyPlates, anyFaces := false, false
	for _, c := range cameras {
		anyPlates = anyPlates || (c.Enabled && c.Detect.Enabled && c.Detect.Plates)
		anyFaces = anyFaces || (c.Enabled && c.Detect.Enabled && c.Detect.Faces)
	}

	streams := ymap{}
	cams := ymap{}
	for _, c := range cameras {
		if _, err := ValidateCamera(c); err != nil {
			return nil, fmt.Errorf("camera %s: %w", c.ID, err)
		}
		entries, err := cameraStreams(c)
		if err != nil {
			return nil, fmt.Errorf("camera %s: %w", c.ID, err)
		}
		streams = append(streams, entries...)
		cams = append(cams, yentry{c.ID, cameraBlock(c, anyPlates, anyFaces)})
	}

	go2rtc := ymap{}
	if s.RTSPOut && s.RTSPPassword != "" {
		// Only for connections from outside the container; Frigate's own
		// ffmpeg reads the restream from 127.0.0.1 without a login.
		go2rtc = append(go2rtc, yentry{"rtsp", ymap{{"username", RTSPUser}, {"password", s.RTSPPassword}}})
	}
	// Always named: without the key Frigate adds a public STUN server.
	candidates := slices.Clone(hw.Candidates)
	if candidates == nil {
		candidates = []string{}
	}
	go2rtc = append(go2rtc, yentry{"webrtc", ymap{{"candidates", candidates}}}, yentry{"streams", streams})

	root := ymap{
		// The version of the config format. Without it Frigate takes the
		// file for one of 0.13, "migrates" it and writes it back.
		{"version", frigateConfigVersion},
		{"mqtt", ymap{{"enabled", false}}},
		// titand is the only client and uses port 5000, which never asks
		// for a login. Port 8971 (TLS, login) is bound to the container's
		// loopback so that nothing reaches it at all.
		{"auth", ymap{{"enabled", false}}},
		{"tls", ymap{{"enabled", false}}},
		{"networking", ymap{{"listen", ymap{{"external", "127.0.0.1:8971"}}}}},
		{"telemetry", ymap{{"version_check", false}}},
		{"detectors", detectors},
	}
	if model != nil {
		root = append(root, yentry{"model", model})
	}
	root = append(root,
		yentry{"ffmpeg", ymap{{"hwaccel_args", hwaccel}}},
		yentry{"birdseye", ymap{{"enabled", false}}},
		yentry{"lpr", ymap{{"enabled", anyPlates}}},
		yentry{"face_recognition", ymap{{"enabled", anyFaces}}},
		yentry{"go2rtc", go2rtc},
		yentry{"cameras", cams},
	)

	var out bytes.Buffer
	out.WriteString(renderHeader)
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(root); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// detectorBlocks are `detectors` and `model` for a device, as Frigate's
// documentation gives them (configuration/object_detectors.md, 0.18.2).
func detectorBlocks(device string) (detectors, model ymap, err error) {
	// The OpenVINO model in the image: SSDLite MobileNet v2, 300×300, COCO.
	openvino := ymap{
		{"width", 300}, {"height", 300},
		{"input_tensor", "nhwc"}, {"input_pixel_format", "bgr"},
		{"path", "/openvino-model/ssdlite_mobilenet_v2.xml"},
		{"labelmap_path", "/openvino-model/coco_91cl_bkgr.txt"},
	}
	switch device {
	case DetectorNPU:
		return ymap{{"ov", ymap{{"type", "openvino"}, {"device", "NPU"}}}}, openvino, nil
	case DetectorIGPU:
		return ymap{{"ov", ymap{{"type", "openvino"}, {"device", "GPU"}}}}, openvino, nil
	case DetectorCPU:
		return ymap{{"ov", ymap{{"type", "openvino"}, {"device", "CPU"}}}}, openvino, nil
	case DetectorCoral:
		// The image brings /edgetpu_model.tflite (MobileDet), the default.
		return ymap{{"coral", ymap{{"type", "edgetpu"}, {"device", "usb"}}}}, nil, nil
	case DetectorHailo:
		// The model (YOLOv6n) is fetched once on the first start.
		return ymap{{"hailo", ymap{{"type", "hailo8l"}, {"device", "PCIe"}}}}, ymap{
			{"width", 320}, {"height", 320},
			{"input_tensor", "nhwc"}, {"input_pixel_format", "rgb"}, {"input_dtype", "int"},
			{"model_type", "yolo-generic"},
			{"labelmap_path", "/labelmap/coco-80.txt"},
		}, nil
	}
	return nil, nil, fmt.Errorf("nvr: no detector %q", device)
}

// streamNames are the names of a camera's streams in go2rtc.
func mainStream(id string) string   { return id }
func subStream(id string) string    { return id + "_sub" }
func twoWayStream(id string) string { return id + "_twoway" }

// withCredentials puts user and password into a stream address,
// URL-escaped (a "@" or "/" in a password would otherwise end the host).
func withCredentials(raw, user, password string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	switch {
	case user != "" && password != "":
		u.User = url.UserPassword(user, password)
	case user != "":
		u.User = url.User(user)
	}
	out := u.String()
	// Frigate runs str.format() over every stream: a brace would be read
	// as a placeholder. ValidateCamera refuses them in addresses and Go
	// escapes them in credentials; this is the last check.
	if strings.ContainsAny(out, "{}") {
		return "", fmt.Errorf("%w: braces in a stream address", ErrInvalid)
	}
	return out, nil
}

// cameraStreams are the go2rtc streams of a camera: the main stream, the
// sub stream if the camera has one, and for talking back a third
// connection without any option (go2rtc opens the camera's audio output
// only on such a source).
func cameraStreams(c Camera) (ymap, error) {
	// "#backchannel=0" keeps go2rtc from taking the camera's audio output
	// for the streams that only watch; "#media=video" leaves the sound out.
	options := "#backchannel=0"
	if !c.Audio.Enabled {
		options = "#media=video" + options
	}
	main, err := withCredentials(c.Source.MainURL, c.Source.User, c.Source.Password)
	if err != nil {
		return nil, err
	}
	sources := []string{main + options}
	if c.Audio.TwoWay {
		// WebRTC carries no AAC: an Opus copy of the sound for it.
		sources = append(sources, "ffmpeg:"+mainStream(c.ID)+"#audio=opus")
	}
	out := ymap{{mainStream(c.ID), sources}}
	if c.Source.SubURL != "" {
		sub, err := withCredentials(c.Source.SubURL, c.Source.User, c.Source.Password)
		if err != nil {
			return nil, err
		}
		out = append(out, yentry{subStream(c.ID), []string{sub + options}})
	}
	if c.Audio.TwoWay {
		out = append(out, yentry{twoWayStream(c.ID), []string{main}})
	}
	return out, nil
}

// LiveStream is the go2rtc stream a live view of a camera uses: the sub
// stream for small tiles if the camera has one, else the main stream.
func LiveStream(c Camera, sub bool) string {
	if sub && c.Source.SubURL != "" {
		return subStream(c.ID)
	}
	return mainStream(c.ID)
}

// frigateBraces escapes braces for the fields Frigate substitutes
// {FRIGATE_…} variables in ("{{" is a literal brace there).
func frigateBraces(s string) string {
	return strings.NewReplacer("{", "{{", "}", "}}").Replace(s)
}

func cameraBlock(c Camera, anyPlates, anyFaces bool) ymap {
	const restream = "rtsp://127.0.0.1:8554/"
	input := func(stream string, roles ...string) ymap {
		return ymap{{"path", restream + stream}, {"input_args", "preset-rtsp-restream"}, {"roles", roles}}
	}
	// Detection runs on the small stream when there is one; the main
	// stream is what gets recorded.
	inputs := []ymap{input(mainStream(c.ID), "record", "detect")}
	if c.Source.SubURL != "" {
		inputs = []ymap{input(mainStream(c.ID), "record"), input(subStream(c.ID), "detect")}
	}
	recordArgs := "preset-record-generic"
	if c.Audio.Enabled {
		recordArgs = "preset-record-generic-audio-copy"
	}

	// Plates are read on vehicles and faces on persons: those must be
	// tracked for the camera, chosen or not.
	track := slices.Clone(c.Detect.Objects)
	if c.Detect.Plates && !slices.Contains(track, "car") {
		track = append(track, "car")
	}
	if c.Detect.Faces && !slices.Contains(track, "person") {
		track = append(track, "person")
	}
	track, _ = cleanList(track, allowedObjects) // validated before; this only orders them

	block := ymap{
		{"enabled", c.Enabled},
		{"friendly_name", c.Name},
		{"ffmpeg", ymap{{"output_args", ymap{{"record", recordArgs}}}, {"inputs", inputs}}},
		{"detect", ymap{{"enabled", c.Detect.Enabled}, {"fps", 5}}},
		{"objects", ymap{{"track", track}}},
		{"record", recordBlock(c.Record)},
		{"snapshots", ymap{{"enabled", c.Detect.Enabled}, {"retain", ymap{{"default", c.Record.RetainDays}}}}},
		// Frigate's own listening for sounds (barking, alarms) stays off;
		// Audio.Enabled is about the sound of the picture.
		{"audio", ymap{{"enabled", false}}},
	}
	if anyPlates {
		block = append(block, yentry{"lpr", ymap{{"enabled", c.Detect.Enabled && c.Detect.Plates}}})
	}
	if anyFaces {
		block = append(block, yentry{"face_recognition", ymap{{"enabled", c.Detect.Enabled && c.Detect.Faces}}})
	}
	if c.PTZ && c.Source.Kind == "onvif" {
		onvif := ymap{{"host", c.Source.Host}, {"port", c.Source.ONVIFPort}}
		if c.Source.User != "" {
			onvif = append(onvif, yentry{"user", frigateBraces(c.Source.User)}, yentry{"password", frigateBraces(c.Source.Password)})
		}
		block = append(block, yentry{"onvif", onvif})
	}
	return block
}

// recordBlock maps the mode of a camera to Frigate's retention:
//
//	continuous: everything is kept for RetainDays.
//	motion:     only segments with motion are kept.
//	events:     only segments in which a tracked object was active.
//
// Alerts and detections (Frigate's two classes of events) get the same
// days and the seconds before and after.
func recordBlock(r Record) ymap {
	if r.Mode == RecordOff {
		return ymap{{"enabled", false}}
	}
	continuous, motion, mode := 0, 0, "active_objects"
	switch r.Mode {
	case RecordContinuous:
		continuous, motion, mode = r.RetainDays, r.RetainDays, "all"
	case RecordMotion:
		motion, mode = r.RetainDays, "motion"
	}
	events := ymap{
		{"pre_capture", r.PreSeconds}, {"post_capture", r.PostSeconds},
		{"retain", ymap{{"days", r.RetainDays}, {"mode", mode}}},
	}
	return ymap{
		{"enabled", true},
		{"continuous", ymap{{"days", continuous}}},
		{"motion", ymap{{"days", motion}}},
		{"alerts", events},
		{"detections", events},
	}
}
