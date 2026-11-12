// Package nvr is the network video recorder of TitanOS (PLAN 79-7,
// docs/NVR.md). Frigate does the work in one container that TitanOS
// manages and never shows as an app; this package owns everything around
// it: the owner's settings and cameras on the stick, Frigate's config.yml
// made from them, the container and its image, the watch that moves the
// detection to another device when Frigate keeps ending, the search for
// cameras in the home network, and which account may see which camera.
//
// The routes are in api/nvr*.go, the calls on the engine in docker/nvr.go.
package nvr

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/naming"
)

// The files on the stick; older titand ignore them. Both are written only
// when the owner changes something. What changes by itself (the device the
// detection runs on after a failover) is state.json on the NVR's volume.
const (
	settingsFile = "nvr/settings.json"
	camerasFile  = "nvr/cameras.json"
	accessFile   = "nvr/camera-access.json"
)

// The devices the object detection can run on. Auto lets TitanOS choose.
const (
	DetectorAuto  = "auto"
	DetectorNPU   = "npu"
	DetectorIGPU  = "igpu"
	DetectorCoral = "coral"
	DetectorHailo = "hailo"
	DetectorCPU   = "cpu"
)

var detectorKinds = []string{DetectorAuto, DetectorNPU, DetectorIGPU, DetectorCoral, DetectorHailo, DetectorCPU}

// Settings are the owner's choices for the NVR (nvr/settings.json).
type Settings struct {
	Enabled bool `json:"enabled"`
	// Volume holds Frigate's database and the recordings.
	Volume   string          `json:"volume"`
	Detector DetectorSetting `json:"detector"`
	Storage  StorageSetting  `json:"storage"`
	// PrivacyNoticeDismissed: the owner chose "do not show again".
	PrivacyNoticeDismissed bool `json:"privacyNoticeDismissed"`
	// RTSPOut publishes the restream on port 8554 (for Home Assistant),
	// protected by RTSPUser and RTSPPassword.
	RTSPOut bool `json:"rtspOut"`
	// Plates are the known license plates: a name and the plates (or
	// patterns) that belong to it. Frigate puts the name on vehicles whose
	// plate matches.
	Plates []KnownPlate `json:"plates,omitempty"`
	// RTSPPassword is made when RTSPOut is first switched on; only
	// administrators ever see it (they type it into Home Assistant).
	RTSPPassword string `json:"rtspPassword,omitempty"`
}

// KnownPlate is one entry of the known license plates.
type KnownPlate struct {
	Name string `json:"name"`
	// Plates are plates or patterns as Frigate reads them: "*" stands
	// for any characters, regular expressions are allowed.
	Plates []string `json:"plates"`
}

// MaxKnownPlates bounds the list of names, maxPlatePatterns the plates
// of one name.
const (
	MaxKnownPlates   = 100
	maxPlatePatterns = 20
)

// cleanPlates checks the known plates and returns them without doubles.
func cleanPlates(list []KnownPlate) ([]KnownPlate, error) {
	if len(list) > MaxKnownPlates {
		return nil, invalid("plates: at most %d names", MaxKnownPlates)
	}
	out := []KnownPlate{}
	names := map[string]bool{}
	for _, entry := range list {
		if !validName(entry.Name) || utf8.RuneCountInString(entry.Name) > 40 || names[entry.Name] {
			return nil, invalid("plates: name %q", entry.Name)
		}
		names[entry.Name] = true
		plates := []string{}
		for _, plate := range entry.Plates {
			// A pattern must compile as the regular expression Frigate
			// makes of it, and hold nothing that could leave its YAML line.
			if plate == "" || len(plate) > 64 || strings.ContainsAny(plate, "\r\n\x00\"'`") || strings.TrimSpace(plate) != plate {
				return nil, invalid("plates: plate of %q", entry.Name)
			}
			if _, err := regexp.Compile(strings.ReplaceAll(plate, "*", ".*")); err != nil {
				return nil, invalid("plates: pattern of %q", entry.Name)
			}
			if !slices.Contains(plates, plate) {
				plates = append(plates, plate)
			}
		}
		if len(plates) == 0 || len(plates) > maxPlatePatterns {
			return nil, invalid("plates: 1 to %d plates for %q", maxPlatePatterns, entry.Name)
		}
		out = append(out, KnownPlate{Name: entry.Name, Plates: plates})
	}
	return out, nil
}

// RTSPUser is the fixed user name of the published restream.
const RTSPUser = "titanos"

// DetectorSetting is the chosen device.
type DetectorSetting struct {
	// Kind is one of detectorKinds.
	Kind string `json:"kind"`
}

// StorageSetting bounds the recordings.
type StorageSetting struct {
	// LimitGB is the most the recordings may take; 0 is no cap of TitanOS
	// (Frigate still frees space when the volume is nearly full).
	LimitGB int `json:"limitGB"`
}

// Defaults are the settings without a file.
func Defaults() Settings {
	return Settings{Detector: DetectorSetting{Kind: DetectorAuto}}
}

// normalize fills fields an older or hand-edited file left empty.
func normalize(s Settings) Settings {
	if s.Detector.Kind == "" {
		s.Detector.Kind = DetectorAuto
	}
	return s
}

// ErrInvalid is a setting or camera outside the allowed values; the
// message names the field.
var ErrInvalid = errors.New("nvr: invalid")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// MaxLimitGB bounds the storage cap (1 PB).
const MaxLimitGB = 1 << 20

// ValidateSettings checks settings against the mounted volumes. An
// enabled NVR needs a volume; a volume that is named must be mounted.
func ValidateSettings(s Settings, mounted []string) error {
	if !slices.Contains(detectorKinds, s.Detector.Kind) {
		return invalid("detector.kind")
	}
	if s.Storage.LimitGB < 0 || s.Storage.LimitGB > MaxLimitGB {
		return invalid("storage.limitGB")
	}
	if _, err := cleanPlates(s.Plates); err != nil {
		return err
	}
	if s.Volume == "" {
		if s.Enabled {
			return invalid("volume")
		}
		return nil
	}
	if !naming.ValidVolume(s.Volume) || !slices.Contains(mounted, s.Volume) {
		return invalid("volume")
	}
	return nil
}

// Camera is one camera (nvr/cameras.json).
type Camera struct {
	// ID is the stable name in Frigate, in the folders of the recordings
	// and in the rights; it never changes after the camera was made.
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Source  Source `json:"source"`
	Detect  Detect `json:"detect"`
	Record  Record `json:"record"`
	Audio   Audio  `json:"audio"`
	// PTZ: the camera pans and tilts over ONVIF.
	PTZ    bool   `json:"ptz"`
	Notify Notify `json:"notify"`
	// Order sorts the cameras in lists; equal values sort by ID.
	Order int `json:"order"`
}

// Source is where the pictures come from.
type Source struct {
	// Kind is "onvif" (found by the search; PTZ possible) or "rtsp" (an
	// address typed in).
	Kind string `json:"kind"`
	// Host and Port are the camera's ONVIF address for kind "onvif"; for
	// "rtsp" they are taken from MainURL.
	Host string `json:"host"`
	Port int    `json:"port"`
	User string `json:"user"`
	// Password never leaves titand: the API answers hasPassword instead.
	Password string `json:"password,omitempty"`
	// MainURL and SubURL are the streams without credentials. The main
	// stream is recorded, the sub stream (if any) is used for detection.
	MainURL string `json:"mainURL"`
	SubURL  string `json:"subURL"`
	// ONVIFPort is the port of the ONVIF service (kind "onvif"; 0 for rtsp).
	ONVIFPort int `json:"onvifPort"`
	// AllowPublic: the administrator confirmed an address outside the home
	// network.
	AllowPublic bool `json:"allowPublic,omitempty"`
}

// Detect is what Frigate looks for.
type Detect struct {
	Enabled bool `json:"enabled"`
	// Objects are labels of allowedObjects.
	Objects []string `json:"objects"`
	Plates  bool     `json:"plates"`
	Faces   bool     `json:"faces"`
}

// The recording modes.
const (
	RecordOff        = "off"
	RecordEvents     = "events"
	RecordMotion     = "motion"
	RecordContinuous = "continuous"
)

// Record is when the camera records and how long recordings are kept.
type Record struct {
	Mode        string `json:"mode"`
	RetainDays  int    `json:"retainDays"`
	PreSeconds  int    `json:"preSeconds"`
	PostSeconds int    `json:"postSeconds"`
}

// Audio is the sound of the camera.
type Audio struct {
	// Enabled keeps the sound in live view and recordings (off: video only).
	Enabled bool `json:"enabled"`
	// TwoWay offers talking back through the camera (WebRTC).
	TwoWay bool `json:"twoWay"`
}

// Notify is where messages about this camera go. Phase 1 raises only the
// TitanOS message; the other ways are kept for the routing of phase 2.
type Notify struct {
	Titan         bool `json:"titan"`
	Push          bool `json:"push"`
	Email         bool `json:"email"`
	HomeAssistant bool `json:"homeAssistant"`
	Agent         bool `json:"agent"`
	// Kinds are what is reported: labels of allowedObjects, "plate",
	// "face" and "offline".
	Kinds []string `json:"kinds"`
}

// allowedObjects are the labels offered for detection: the ones the
// bundled models of all detectors know (COCO) and a home needs.
var allowedObjects = []string{"person", "bicycle", "car", "motorcycle", "bus", "bird", "cat", "dog", "horse", "sheep", "cow", "bear"}

// AllowedObjects lists the labels a camera may detect.
func AllowedObjects() []string { return slices.Clone(allowedObjects) }

var notifyKinds = append(slices.Clone(allowedObjects), "plate", "face", "offline")

// NewCamera is a camera with the defaults of a new one: detection of
// persons on, recording on detected objects for 14 days, sound off.
func NewCamera() Camera {
	return Camera{
		Enabled: true,
		Source:  Source{Kind: "rtsp"},
		Detect:  Detect{Enabled: true, Objects: []string{"person"}},
		Record:  Record{Mode: RecordEvents, RetainDays: 14, PreSeconds: 5, PostSeconds: 5},
		Notify:  Notify{Titan: true, Kinds: []string{"person", "offline"}},
	}
}

var (
	// cameraIDRE: Frigate allows [a-zA-Z0-9_-]; TitanOS keeps to lower
	// case, digits and "_" so the ID is also a safe folder and stream name.
	cameraIDRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	// reservedIDs would collide with routes or with Frigate's own names.
	reservedIDs = []string{"discover", "probe", "birdseye", "all", "default"}
)

// MaxCameras bounds the list (the NVR is built and tested for 4; more
// work, the UI warns on weak hardware).
const MaxCameras = 64

// ValidCameraID tells whether id can name a camera.
func ValidCameraID(id string) bool {
	if !cameraIDRE.MatchString(id) || slices.Contains(reservedIDs, id) {
		return false
	}
	// "<id>_sub" and "<id>_twoway" are the names of the other streams of
	// a camera in go2rtc.
	return !strings.HasSuffix(id, "_sub") && !strings.HasSuffix(id, "_twoway")
}

// SlugID makes a camera ID from a display name: "Haustür vorn" → "haustuer_vorn".
// taken are the IDs in use; a number is appended until the ID is free.
func SlugID(name string, taken []string) string {
	var b strings.Builder
	replace := map[rune]string{'ä': "ae", 'ö': "oe", 'ü': "ue", 'ß': "ss"}
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case replace[r] != "":
			b.WriteString(replace[r])
		default:
			if s := b.String(); s != "" && !strings.HasSuffix(s, "_") {
				b.WriteByte('_')
			}
		}
	}
	base := strings.Trim(b.String(), "_")
	if base == "" || base[0] < 'a' || base[0] > 'z' {
		base = "cam_" + base
	}
	base = strings.TrimRight(base, "_")
	if len(base) > 24 {
		base = strings.TrimRight(base[:24], "_")
	}
	id := base
	for n := 2; !ValidCameraID(id) || slices.Contains(taken, id); n++ {
		id = fmt.Sprintf("%s_%d", base, n)
	}
	return id
}

// validName: a display name of 1 to 64 characters without control
// characters, not starting or ending with a space.
func validName(name string) bool {
	if name == "" || utf8.RuneCountInString(name) > 64 || strings.TrimSpace(name) != name || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

var hostRE = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)

// loopbackIsCamera lets tests reach their stand-in cameras on 127.0.0.1.
var loopbackIsCamera = false

// validHost: an IP address or a host name; never this host.
func validHost(host string) bool {
	if addr, err := netip.ParseAddr(host); err == nil {
		return (!addr.IsLoopback() || loopbackIsCamera) && !addr.IsUnspecified() && !addr.IsMulticast()
	}
	return hostRE.MatchString(host) && !strings.EqualFold(host, "localhost") && !strings.HasSuffix(strings.ToLower(host), ".localhost")
}

// streamURL checks a stream address: rtsp or rtsps, a host, no
// credentials (they are fields of their own), no fragment (go2rtc reads
// "#…" as its own options), nothing a YAML or go2rtc source could
// misread. It returns the host.
func streamURL(raw string) (string, error) {
	if len(raw) > 1024 || strings.ContainsAny(raw, " \t\r\n\"'`{}\\") {
		return "", errors.New("characters that are not allowed")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("not an address")
	}
	if u.Scheme != "rtsp" && u.Scheme != "rtsps" {
		return "", errors.New("only rtsp:// and rtsps://")
	}
	if u.User != nil {
		return "", errors.New("credentials belong in user and password")
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return "", errors.New("no # part")
	}
	if !validHost(u.Hostname()) {
		return "", errors.New("host")
	}
	if p := u.Port(); p != "" {
		if !validPort(atoi(p)) {
			return "", errors.New("port")
		}
	}
	return u.Hostname(), nil
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' || n > 1<<20 {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func validPort(p int) bool { return p >= 1 && p <= 65535 }

// SplitCredentials takes "rtsp://user:pass@host/…" apart: the address
// without credentials, and the credentials. People paste such addresses.
func SplitCredentials(raw string) (clean, user, password string) {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw, "", ""
	}
	user = u.User.Username()
	password, _ = u.User.Password()
	u.User = nil
	return u.String(), user, password
}

// ValidateCamera checks one camera and returns it in its stored form
// (lists sorted and without doubles, host and port of an RTSP camera taken
// from its main stream).
func ValidateCamera(c Camera) (Camera, error) {
	if !ValidCameraID(c.ID) {
		return c, invalid("id")
	}
	if !validName(c.Name) {
		return c, invalid("name")
	}
	s := &c.Source
	mainHost, err := streamURL(s.MainURL)
	if err != nil {
		return c, invalid("source.mainURL: %v", err)
	}
	if s.SubURL != "" {
		if _, err := streamURL(s.SubURL); err != nil {
			return c, invalid("source.subURL: %v", err)
		}
	}
	if len(s.User) > 128 || len(s.Password) > 256 || strings.ContainsAny(s.User+s.Password, "\r\n\x00") {
		return c, invalid("source.user or source.password")
	}
	if s.Password != "" && s.User == "" {
		return c, invalid("source.user")
	}
	switch s.Kind {
	case "rtsp":
		u, _ := url.Parse(s.MainURL) // parsed by streamURL above
		s.Host, s.Port, s.ONVIFPort = mainHost, 554, 0
		if p := u.Port(); p != "" {
			s.Port = atoi(p)
		}
		if c.PTZ {
			return c, invalid("ptz needs an ONVIF camera")
		}
	case "onvif":
		if !validHost(s.Host) {
			return c, invalid("source.host")
		}
		if s.ONVIFPort == 0 {
			s.ONVIFPort = 80
		}
		if s.Port == 0 {
			s.Port = s.ONVIFPort
		}
		if !validPort(s.ONVIFPort) || !validPort(s.Port) {
			return c, invalid("source.onvifPort")
		}
	default:
		return c, invalid("source.kind")
	}
	if !s.AllowPublic {
		for _, host := range []string{s.Host, mainHost} {
			if addr, err := netip.ParseAddr(host); err == nil && !HomeAddr(addr) {
				return c, fmt.Errorf("%w: %s", ErrTargetNotAllowed, host)
			}
		}
	}
	if c.Detect.Objects, err = cleanList(c.Detect.Objects, allowedObjects); err != nil {
		return c, invalid("detect.objects: %v", err)
	}
	if c.Detect.Enabled && len(c.Detect.Objects) == 0 {
		return c, invalid("detect.objects: none chosen")
	}
	if (c.Detect.Plates || c.Detect.Faces) && !c.Detect.Enabled {
		return c, invalid("detect: plates and faces need the detection")
	}
	r := c.Record
	if !slices.Contains([]string{RecordOff, RecordEvents, RecordMotion, RecordContinuous}, r.Mode) {
		return c, invalid("record.mode")
	}
	if r.Mode == RecordEvents && !c.Detect.Enabled {
		return c, invalid("record.mode: events need the detection")
	}
	if r.RetainDays < 1 || r.RetainDays > 365 {
		return c, invalid("record.retainDays")
	}
	// Frigate takes at most 60 seconds before an event.
	if r.PreSeconds < 0 || r.PreSeconds > 60 || r.PostSeconds < 0 || r.PostSeconds > 60 {
		return c, invalid("record.preSeconds or record.postSeconds")
	}
	if c.Audio.TwoWay && !c.Audio.Enabled {
		return c, invalid("audio.twoWay needs audio.enabled")
	}
	if c.Notify.Kinds, err = cleanList(c.Notify.Kinds, notifyKinds); err != nil {
		return c, invalid("notify.kinds: %v", err)
	}
	if c.Order < 0 || c.Order > 100000 {
		return c, invalid("order")
	}
	return c, nil
}

// cleanList keeps the entries of list in the order of allowed, once each;
// an entry that is not allowed is an error.
func cleanList(list, allowed []string) ([]string, error) {
	out := []string{}
	for _, entry := range list {
		if !slices.Contains(allowed, entry) {
			return nil, fmt.Errorf("unknown %q", entry)
		}
	}
	for _, entry := range allowed {
		if slices.Contains(list, entry) {
			out = append(out, entry)
		}
	}
	return out, nil
}

// sortCameras orders cameras by Order, then ID.
func sortCameras(list []Camera) {
	slices.SortFunc(list, func(a, b Camera) int {
		if a.Order != b.Order {
			return a.Order - b.Order
		}
		return strings.Compare(a.ID, b.ID)
	})
}

// Failover is the last move of the detection to another device.
type Failover struct {
	From string    `json:"from"`
	To   string    `json:"to"`
	At   time.Time `json:"at"`
	// Reason is a code of classify.go (npu_firmware_old, …).
	Reason string `json:"reason"`
	// Detail is the line of Frigate's log the reason was read from.
	Detail string `json:"detail"`
}

// State is what changes without the owner (state.json in .titanos/nvr on
// the NVR's volume, never on the stick).
type State struct {
	// Active is the device the detection runs on now; "" follows the
	// setting.
	Active   string    `json:"active"`
	Failover *Failover `json:"failover"`
	// Stopped: nothing worked, the NVR waits for the owner (code and
	// detail say why).
	Stopped       bool   `json:"stopped"`
	StoppedReason string `json:"stoppedReason,omitempty"`
	StoppedDetail string `json:"stoppedDetail,omitempty"`
	// CapNoticeDay is the day (time zone of the NAS) the owner was last
	// told that the storage cap deleted recordings.
	CapNoticeDay string `json:"capNoticeDay,omitempty"`
	// Boot is the boot of the NAS the state was written in: after a
	// restart of the NAS the chosen device is tried again.
	Boot string `json:"boot"`
}
