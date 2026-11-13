package nvr

import (
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		name, log, device string
		accel             bool
		reason, inDetail  string
	}{
		{"old NPU firmware", "2026-11-12 info start\nRuntimeError: Exception from src/inference/src/cpp/core.cpp:\n[ GENERAL_ERROR ] ZE_RESULT_ERROR: MAPPED_INFERENCE_VERSION is NOT compatible with the ELF Expected: 7.0 received: 11.0\nexit",
			DetectorNPU, true, ReasonNPUFirmware, "MAPPED_INFERENCE_VERSION"},
		{"Arrow Lake NPU", "frigate.detectors.plugins.openvino ERROR : Unsupported configuration key: NPU_TURBO\n", DetectorNPU, true, ReasonNPUTurbo, "NPU_TURBO"},
		{"no Coral", "ValueError: Failed to load delegate from libedgetpu.so.1.0\nfrigate.detectors.plugins.edgetpu_tfl ERROR   : No EdgeTPU was detected. If you do not have a Coral device yet, you must configure CPU detectors.",
			DetectorCoral, false, ReasonCoralMissing, "No EdgeTPU"},
		{"NPU without device node", "Process detector:ov:\nTraceback (most recent call last):\nsomething odd", DetectorNPU, false, ReasonNPUMissing, ""},
		{"NPU not registered", "RuntimeError: Device with \"NPU\" name is not registered in the OpenVINO Runtime", DetectorNPU, true, ReasonNPUMissing, "not registered"},
		{"unknown", "INFO fine\nfrigate.watchdog INFO : Detection appears to have stopped. Exiting Frigate...\nBus error (core dumped)", DetectorIGPU, true, ReasonDetectorUnknown, "Bus error"},
		{"nothing", "", DetectorCPU, true, ReasonDetectorUnknown, ""},
		{"refused config wins", "*** Your config file is not valid! ***\n***    Config Validation Errors    ***\nMessage : Extra inputs are not permitted\nUnsupported configuration key: NPU_TURBO",
			DetectorNPU, true, ReasonConfigInvalid, "Config Validation Errors"},
	} {
		reason, detail := Classify(tc.log, tc.device, tc.accel)
		if reason != tc.reason || !strings.Contains(detail, tc.inDetail) {
			t.Errorf("%s: %s %q, want %s with %q", tc.name, reason, detail, tc.reason, tc.inDetail)
		}
	}
}

func TestRedact(t *testing.T) {
	for in, want := range map[string]string{
		"ffmpeg: rtsp://admin:s3cret@192.168.1.5:554/live failed":              "ffmpeg: rtsp://***:***@192.168.1.5:554/live failed",
		"Error opening input rtsp://viewer:p%40ss@cam.lan/x and http://u:p@h/": "Error opening input rtsp://***:***@cam.lan/x and http://***:***@h/",
		"onvif password: hunter2, user admin":                                  "onvif password: ***, user admin",
		`{"password": "abc def"}`:                                              `{"password": ***}`,
		"rtsp://192.168.1.5:554/live at 12:30:01":                              "rtsp://192.168.1.5:554/live at 12:30:01",
	} {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
	// A classified line never carries a password.
	_, detail := Classify("ERROR could not open rtsp://admin:topsecret@192.168.1.9/live", DetectorCPU, true)
	if strings.Contains(detail, "topsecret") {
		t.Errorf("detail = %q", detail)
	}
}

func TestClip(t *testing.T) {
	if got := clip("häuser", 2); got != "h" {
		t.Errorf("%q", got)
	}
	if got := clip("abc", 3); got != "abc" {
		t.Errorf("%q", got)
	}
}
