package nvr

import (
	"regexp"
	"strings"
)

// The reasons a detection device fails, as the diagnosis shows them.
const (
	// ReasonNPUFirmware: the NPU firmware of the NAS is older than the
	// driver in Frigate's image.
	ReasonNPUFirmware = "npu_firmware_old"
	// ReasonNPUTurbo: OpenVINO sets an option this NPU does not know
	// (Frigate 0.18 on Arrow Lake; fixed in 0.19).
	ReasonNPUTurbo = "npu_turbo_unsupported"
	// ReasonNPUMissing: no /dev/accel (driver intel_vpu not loaded).
	ReasonNPUMissing = "npu_missing"
	// ReasonCoralMissing: Frigate found no Coral.
	ReasonCoralMissing = "coral_missing"
	// ReasonDetectorUnknown: the detection ended and the log does not say
	// why in a way TitanOS knows.
	ReasonDetectorUnknown = "detector_unknown"
	// ReasonConfigInvalid: Frigate refused config.yml. Not a fault of the
	// device: no failover, the NVR stops with this reason.
	ReasonConfigInvalid = "config_invalid"
	// ReasonNotHealthy: Frigate never answered after its start.
	ReasonNotHealthy = "not_healthy"
)

// knownCauses are searched in this order; the first line that matches
// names the reason.
var knownCauses = []struct {
	reason string
	match  *regexp.Regexp
}{
	{ReasonConfigInvalid, regexp.MustCompile(`Your config file is not valid|Config Validation Errors|Unable to start Frigate in safe mode`)},
	{ReasonNPUFirmware, regexp.MustCompile(`MAPPED_INFERENCE_VERSION is NOT compatible`)},
	{ReasonNPUTurbo, regexp.MustCompile(`Unsupported configuration key: NPU_TURBO`)},
	{ReasonCoralMissing, regexp.MustCompile(`No EdgeTPU was detected|Failed to load delegate from libedgetpu`)},
	{ReasonNPUMissing, regexp.MustCompile(`(?i)/dev/accel.*(No such file|not found|cannot open)|Device with "NPU" name is not registered|No available devices.*NPU|NPU.*(is not available|not found)`)},
}

// Classify reads the cause of a failed detection from Frigate's log. It
// returns a reason and the line it was read from. device is the device in
// use and accel whether /dev/accel exists: a failing NPU without its
// device node is "npu_missing" even when the log says nothing known.
func Classify(log, device string, accel bool) (reason, detail string) {
	lines := strings.Split(log, "\n")
	for _, cause := range knownCauses {
		// The last occurrence: the log holds earlier starts as well.
		for i := len(lines) - 1; i >= 0; i-- {
			if cause.match.MatchString(lines[i]) {
				return cause.reason, clip(Redact(strings.TrimSpace(lines[i])), 500)
			}
		}
	}
	if device == DetectorNPU && !accel {
		return ReasonNPUMissing, ""
	}
	// The last line that looks like an error is the best hint there is.
	for i := len(lines) - 1; i >= 0; i-- {
		if errorLine.MatchString(lines[i]) {
			return ReasonDetectorUnknown, clip(Redact(strings.TrimSpace(lines[i])), 500)
		}
	}
	return ReasonDetectorUnknown, ""
}

var errorLine = regexp.MustCompile(`(?i)\b(error|exception|traceback|fatal|critical)\b`)

// credentials in an address ("rtsp://user:secret@host") and after words
// that name a secret.
var (
	urlCredentials = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/\s:@]*:[^/\s@]*@`)
	secretWord     = regexp.MustCompile(`(?i)\b(password|passwd|pwd)(["']?\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s,;&]+)`)
)

// Redact takes passwords out of a line of Frigate's log before it is
// shown or stored: Frigate hides most itself, ffmpeg and go2rtc do not
// always.
func Redact(s string) string {
	s = urlCredentials.ReplaceAllString(s, "${1}***:***@")
	return secretWord.ReplaceAllString(s, "${1}${2}***")
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Not in the middle of a character.
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}
