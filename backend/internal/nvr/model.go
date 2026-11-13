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
	// ProxySecret is what titand shows Frigate on every request (header
	// X-Proxy-Secret, Frigate's proxy.auth_secret): Frigate's port 8971
	// answers nothing without it. Made when the NVR is first switched on,
	// kept like the cameras' passwords; never in an answer of the API, a
	// log or the diagnosis.
	ProxySecret string `json:"proxySecret,omitempty"`
	// NotifyOff is the main switch of the cameras' messages (PLAN 82-3):
	// true silences every camera, each keeps its own choices. Named "off"
	// so that a file of 0.78, which has no such field, keeps its messages.
	NotifyOff bool `json:"notifyOff,omitempty"`
	// Decoder is the GPU that decodes the camera streams (PLAN 82-4):
	// "" or DecoderAuto picks an iGPU, else the first free GPU, else the
	// CPU; DecoderCPU decodes on the CPU; anything else is the PCI address
	// of a GPU (hardware.ContainerDevice.ID, never a device node: their
	// numbers change between boots). A chosen GPU that is gone or with a
	// VM is not replaced by another: the CPU decodes and the page says why.
	Decoder string `json:"decoder,omitempty"`
}

// The choices of Settings.Decoder besides a PCI address.
const (
	DecoderAuto = "auto"
	DecoderCPU  = "cpu"
)

// pciAddressRE is a PCI address as the hardware manager names devices.
var pciAddressRE = regexp.MustCompile(`^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`)

// KnownPlate is one entry of the known license plates.
type KnownPlate struct {
	Name string `json:"name"`
	// Plates are plates or patterns: letters, digits, spaces and "-",
	// "*" stands for any characters (nothing else: Frigate reads each as
	// a regular expression and one that does not compile stops the
	// matching of all of them).
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
			if !validPlate(plate) {
				return nil, invalid("plates: plate of %q", entry.Name)
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

// validPlate: 1 to 64 characters, letters, digits, spaces and "-", "*"
// as the wildcard; not starting or ending with a space. None of these
// means anything in a regular expression except "*", which Render turns
// into ".*": a plate never fails as a pattern in Frigate, and a plate
// without "*" reaches Frigate unchanged (it also compares the plates
// letter by letter, with match_distance).
func validPlate(plate string) bool {
	if plate == "" || utf8.RuneCountInString(plate) > 64 || strings.TrimSpace(plate) != plate || strings.Trim(plate, "*") == "" {
		return false
	}
	for _, r := range plate {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != ' ' && r != '-' && r != '*' {
			return false
		}
	}
	return true
}

// platePattern is a plate as Frigate's regular expression.
func platePattern(plate string) string { return strings.ReplaceAll(plate, "*", ".*") }

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
	if s.Decoder != "" && s.Decoder != DecoderAuto && s.Decoder != DecoderCPU && !pciAddressRE.MatchString(s.Decoder) {
		return invalid("decoder")
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
	// Streams is which stream serves what (PLAN 82-2) and whether go2rtc
	// keeps them open (82-1).
	Streams StreamUse `json:"streams"`
	Detect  Detect    `json:"detect"`
	Record  Record    `json:"record"`
	Audio   Audio     `json:"audio"`
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
	// MainURL and SubURL are the streams without credentials: the main
	// stream in high resolution, the sub stream (optional) in low. What
	// each serves is Camera.Streams. In go2rtc they are always "<id>" and
	// "<id>_sub" (Home Assistant reads them under these names).
	MainURL string `json:"mainURL"`
	SubURL  string `json:"subURL"`
	// MainWidth … SubHeight are the pictures' sizes as the last check of
	// the camera found them; set by titand like Audio.Codec, 0 while
	// unknown.
	MainWidth  int `json:"mainWidth,omitempty"`
	MainHeight int `json:"mainHeight,omitempty"`
	SubWidth   int `json:"subWidth,omitempty"`
	SubHeight  int `json:"subHeight,omitempty"`
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
//
// Since 0.81 the days are two (PLAN 88): RecordDays for what is recorded
// without an event (continuous and motion recordings) and EventDays for
// the recordings of events (Frigate's alerts and detections). A camera of
// 0.80 has RetainDays only: both are it. Both equal is stored as 0.80
// stores it (RetainDays alone), so 0.80 reads and edits such a camera as
// before. Apart, RetainDays is the larger (ValidateCamera): 0.80 reads
// only RetainDays, and a way back to it then keeps everything longer
// instead of deleting anything.
type Record struct {
	Mode       string `json:"mode"`
	RetainDays int    `json:"retainDays"`
	// RecordDays and EventDays are 0 when they equal RetainDays.
	RecordDays  int `json:"recordDays,omitempty"`
	EventDays   int `json:"eventDays,omitempty"`
	PreSeconds  int `json:"preSeconds"`
	PostSeconds int `json:"postSeconds"`
}

// recordDays is how long recordings without an event are kept
// (continuous and motion; no meaning for RecordEvents).
func (r Record) recordDays() int {
	if r.RecordDays > 0 {
		return r.RecordDays
	}
	return r.RetainDays
}

// eventDays is how long the recordings of events are kept.
func (r Record) eventDays() int {
	if r.EventDays > 0 {
		return r.EventDays
	}
	return r.RetainDays
}

// usesRecordDays tells whether the mode keeps recordings without an event
// (off keeps what was recorded, continuous and motion record such).
func usesRecordDays(mode string) bool { return mode != RecordEvents }

// WithDays is the record block as the pages read it: both days named.
func (r Record) WithDays() Record {
	r.RecordDays, r.EventDays = r.recordDays(), r.eventDays()
	return r
}

// maxRetainDays bounds every number of days.
const maxRetainDays = 365

// normalizeDays checks the days and stores them: both equal as RetainDays
// alone (as 0.80 does), else both and RetainDays the larger. An events
// camera keeps nothing but events: its record days are its event days.
func normalizeDays(r Record) (Record, error) {
	if r.RecordDays < 0 || r.EventDays < 0 {
		return r, invalid("record: days")
	}
	rd, ed := r.recordDays(), r.eventDays()
	if r.Mode == RecordEvents {
		rd = ed
	}
	for _, days := range []int{rd, ed} {
		if days < 1 || days > maxRetainDays {
			return r, invalid("record.retainDays")
		}
	}
	if rd == ed {
		r.RetainDays, r.RecordDays, r.EventDays = rd, 0, 0
	} else {
		r.RetainDays, r.RecordDays, r.EventDays = max(rd, ed), rd, ed
	}
	return r, nil
}

// Audio is the sound of the camera.
type Audio struct {
	// Enabled keeps the sound in live view and recordings (off: video only).
	Enabled bool `json:"enabled"`
	// TwoWay offers talking back through the camera (WebRTC).
	TwoWay bool `json:"twoWay"`
	// Codec is the sound's codec in the main stream as the last check of
	// the camera found it (RTSP name: "MPEG4-GENERIC" is AAC, "PCMA" and
	// "PCMU" are G.711); "" while unknown. Set by titand, not by the page:
	// only AAC is recorded as it is, anything else is turned into AAC
	// (an MP4 file takes no G.711, and Frigate would record nothing).
	Codec string `json:"codec,omitempty"`
	// SubCodec is the same for the sub stream: the recording follows the
	// codec of the stream that is recorded (Streams.Record).
	SubCodec string `json:"subCodec,omitempty"`
}

// The streams of a camera as Streams names them.
const (
	StreamMain = "main"
	StreamSub  = "sub"
	StreamAuto = "auto"
)

// StreamUse is which stream serves what. "" is the default of each field,
// so a camera of 0.78 (no field at all) works as before: the main stream
// is recorded, the sub stream (if any) detects, the live view takes the
// sub stream for small tiles and the main stream for a large picture, and
// go2rtc keeps the streams open.
type StreamUse struct {
	// Record is StreamMain (default) or StreamSub.
	Record string `json:"record,omitempty"`
	// Detect is StreamSub (default; the main stream when there is no sub
	// stream) or StreamMain.
	Detect string `json:"detect,omitempty"`
	// Live is StreamAuto (default), StreamMain or StreamSub.
	Live string `json:"live,omitempty"`
	// NoPreload: go2rtc opens the streams only while something reads them.
	// Off (default): it keeps them open, so that the live view stands at
	// once even when Frigate does not read a stream. Named "no" so that a
	// camera of 0.78 keeps them open.
	NoPreload bool `json:"noPreload,omitempty"`
}

// recordStream is the stream that is recorded: StreamMain or StreamSub.
func (c Camera) recordStream() string {
	if c.Streams.Record == StreamSub && c.Source.SubURL != "" {
		return StreamSub
	}
	return StreamMain
}

// detectStream is the stream the detection reads: StreamMain or StreamSub.
func (c Camera) detectStream() string {
	if c.Streams.Detect != StreamMain && c.Source.SubURL != "" {
		return StreamSub
	}
	return StreamMain
}

// liveStream is the stream a live view shows; small: a small tile.
func (c Camera) liveStream(small bool) string {
	if c.Source.SubURL == "" {
		return StreamMain
	}
	switch c.Streams.Live {
	case StreamMain:
		return StreamMain
	case StreamSub:
		return StreamSub
	}
	if small {
		return StreamSub
	}
	return StreamMain
}

// recordCodec is the sound's codec of the stream that is recorded.
func (c Camera) recordCodec() string {
	if c.recordStream() == StreamSub {
		return c.Audio.SubCodec
	}
	return c.Audio.Codec
}

// codecAAC is AAC as RTSP names it.
const codecAAC = "MPEG4-GENERIC"

// Notify is whom and how this camera tells (routing.go).
type Notify struct {
	// Titan, Push and Email are the ways of 0.78: the bell, the push to
	// the administrators' devices and the e-mail address of the NAS. They
	// count only while Recipients is nil; with a list they are kept as
	// "some recipient has this way", for a way back to 0.78.
	Titan bool `json:"titan"`
	Push  bool `json:"push"`
	Email bool `json:"email"`
	// HomeAssistant and Agent are ways of the camera, not of an account.
	HomeAssistant bool `json:"homeAssistant"`
	Agent         bool `json:"agent"`
	// Kinds are what is reported: labels of allowedObjects, "plate",
	// "face" and "offline".
	Kinds []string `json:"kinds"`
	// Recipients are the accounts that get the messages, each with its own
	// ways (PLAN 82-3, chosen by an administrator per camera). nil is a
	// camera of 0.78: the ways above, to the administrators. An empty
	// list (not nil) is nobody. Rights are checked again for every
	// message: an account that may no longer see the camera, is disabled
	// or was made anew under the same name gets nothing.
	Recipients []Recipient `json:"recipients"`
	// PauseSeconds is the least time between two messages of one kind of
	// this camera: one of pauseChoices; nil (a camera of 0.78) is 60.
	PauseSeconds *int `json:"pauseSeconds,omitempty"`
	// Window limits the messages about events to some hours.
	Window Window `json:"window"`
	// HA lets messages about events through only while entities of Home
	// Assistant are in chosen states (PLAN 83), e.g. only while the alarm
	// is armed. nil is no condition. A request without it keeps the stored
	// one (the API decodes a change over the stored camera, so an older web
	// UI keeps it); null or an empty list of rules removes it.
	HA *HACondition `json:"ha,omitempty"`
}

// HACondition is the condition of a camera's messages on Home Assistant.
// "Kamera offline" is reported whatever it says, like the window. A state
// that cannot be read (Home Assistant not reachable, entity unknown or
// unavailable) never holds a message back: it goes out with a note.
type HACondition struct {
	// Mode is "all" (every rule holds) or "any" (one is enough).
	Mode  string   `json:"mode"`
	Rules []HARule `json:"rules"`
}

// HARule is one entity and the states in which it lets messages through.
type HARule struct {
	// Connection is the ID of the Home Assistant connection; "" the first.
	Connection string   `json:"connection"`
	Entity     string   `json:"entity"`
	States     []string `json:"states"`
}

// Limits of a condition.
const (
	MaxHARules  = 10
	maxHAStates = 20
)

// Modes of a condition.
const (
	HAModeAll = "all"
	HAModeAny = "any"
)

var (
	haEntityRE     = regexp.MustCompile(`^[a-z_]+\.[a-z0-9_]+$`)
	haConnectionRE = regexp.MustCompile(`^[a-z0-9_-]{0,64}$`)
)

// validateHA checks a condition and returns it in its stored form; no
// rules is no condition (nil).
func validateHA(c *HACondition) (*HACondition, error) {
	if c == nil || len(c.Rules) == 0 {
		return nil, nil
	}
	out := HACondition{Mode: c.Mode, Rules: make([]HARule, 0, len(c.Rules))}
	if out.Mode == "" {
		out.Mode = HAModeAll
	}
	if out.Mode != HAModeAll && out.Mode != HAModeAny {
		return nil, invalid("notify.ha.mode")
	}
	if len(c.Rules) > MaxHARules {
		return nil, invalid("notify.ha.rules: at most %d", MaxHARules)
	}
	for i, r := range c.Rules {
		if !haConnectionRE.MatchString(r.Connection) {
			return nil, invalid("notify.ha.rules[%d].connection", i)
		}
		if len(r.Entity) > 255 || !haEntityRE.MatchString(r.Entity) {
			return nil, invalid("notify.ha.rules[%d].entity", i)
		}
		states := []string{}
		for _, s := range r.States {
			s = strings.TrimSpace(s)
			if s == "" || len(s) > 255 || strings.ContainsFunc(s, unicode.IsControl) {
				return nil, invalid("notify.ha.rules[%d].states", i)
			}
			if !slices.Contains(states, s) {
				states = append(states, s)
			}
		}
		if len(states) == 0 || len(states) > maxHAStates {
			return nil, invalid("notify.ha.rules[%d].states: 1 to %d", i, maxHAStates)
		}
		out.Rules = append(out.Rules, HARule{Connection: r.Connection, Entity: r.Entity, States: states})
	}
	return &out, nil
}

// Recipient is one account a camera tells, with its ways.
type Recipient struct {
	User string `json:"user"`
	// Bell is the bell of TitanOS; only administrators have one.
	Bell bool `json:"bell"`
	// Push goes to the account's own subscribed browsers and phones.
	Push bool `json:"push"`
	// Email goes to the account's own address (its notification
	// settings), through the mail server of the NAS.
	Email bool `json:"email"`
	// Added is when an administrator chose this account here, set by
	// titand (never taken from a request): an account made later under the
	// same name, administrator or not, gets nothing of this entry.
	Added time.Time `json:"added,omitzero"`
}

// MaxRecipients bounds the accounts of one camera.
const MaxRecipients = 50

// pauseChoices are the pauses offered: none, 1, 5, 15, 30 and 60 minutes.
var pauseChoices = []int{0, 60, 300, 900, 1800, 3600}

// defaultPause is the pause of a camera that names none (0.78: fixed).
const defaultPause = 60

// pause is the camera's least time between two messages of one kind.
func (n Notify) pause() time.Duration {
	if n.PauseSeconds == nil {
		return defaultPause * time.Second
	}
	return time.Duration(*n.PauseSeconds) * time.Second
}

// Window is when a camera reports events (persons, vehicles, animals,
// plates, faces). "Kamera offline" is reported at any time: a camera that
// goes dark at night is what the window must not hide.
type Window struct {
	// On: only within the window; off: at any time.
	On bool `json:"on"`
	// Days are the days the window starts on: 1 Monday … 7 Sunday.
	Days []int `json:"days"`
	// From and To are HH:MM in the time zone of the NAS. To before From
	// runs across midnight into the next day; From equal to To is the
	// whole day.
	From string `json:"from"`
	To   string `json:"to"`
}

// accountRE is an account name as auth allows it (auth/users.go).
var accountRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

var clockRE = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// minutes is HH:MM as minutes after midnight (valid by clockRE).
func minutes(hhmm string) int { return atoi(hhmm[:2])*60 + atoi(hhmm[3:]) }

// Open tells whether the window lets an event at t (local time) through.
func (w Window) Open(t time.Time) bool {
	if !w.On {
		return true
	}
	day := int(t.Weekday())
	if day == 0 {
		day = 7
	}
	from, to, now := minutes(w.From), minutes(w.To), t.Hour()*60+t.Minute()
	switch {
	case from == to:
		return slices.Contains(w.Days, day)
	case from < to:
		return slices.Contains(w.Days, day) && now >= from && now < to
	}
	// Across midnight: the part after From belongs to today, the part
	// before To to the window that started yesterday.
	yesterday := day - 1
	if yesterday == 0 {
		yesterday = 7
	}
	return (slices.Contains(w.Days, day) && now >= from) || (slices.Contains(w.Days, yesterday) && now < to)
}

// legacyWays tells whether the camera still uses the ways of 0.78.
func (n Notify) legacyWays() bool { return n.Recipients == nil }

// validateNotify checks the ways, recipients, pause and window and returns
// them in their stored form.
func validateNotify(n Notify) (Notify, error) {
	var err error
	if n.Kinds, err = cleanList(n.Kinds, notifyKinds); err != nil {
		return n, invalid("notify.kinds: %v", err)
	}
	if n.Recipients != nil {
		if len(n.Recipients) > MaxRecipients {
			return n, invalid("notify.recipients: at most %d", MaxRecipients)
		}
		seen := map[string]bool{}
		list := make([]Recipient, 0, len(n.Recipients))
		for _, r := range n.Recipients {
			if !accountRE.MatchString(r.User) || seen[r.User] {
				return n, invalid("notify.recipients: account %q", r.User)
			}
			seen[r.User] = true
			list = append(list, r)
		}
		slices.SortFunc(list, func(a, b Recipient) int { return strings.Compare(a.User, b.User) })
		n.Recipients = list
		// Kept for a way back to 0.78, which reads only these.
		n.Titan, n.Push, n.Email = false, false, false
		for _, r := range list {
			n.Titan, n.Push, n.Email = n.Titan || r.Bell, n.Push || r.Push, n.Email || r.Email
		}
	}
	if n.PauseSeconds != nil && !slices.Contains(pauseChoices, *n.PauseSeconds) {
		return n, invalid("notify.pauseSeconds")
	}
	w := &n.Window
	if w.On || len(w.Days) > 0 || w.From != "" || w.To != "" {
		days := []int{}
		for d := 1; d <= 7; d++ {
			if slices.Contains(w.Days, d) {
				days = append(days, d)
			}
		}
		for _, d := range w.Days {
			if d < 1 || d > 7 {
				return n, invalid("notify.window.days")
			}
		}
		w.Days = days
		if !clockRE.MatchString(w.From) || !clockRE.MatchString(w.To) {
			return n, invalid("notify.window.from or notify.window.to")
		}
		if w.On && len(w.Days) == 0 {
			return n, invalid("notify.window.days: none chosen")
		}
	}
	if w.Days == nil {
		w.Days = []int{}
	}
	var err2 error
	if n.HA, err2 = validateHA(n.HA); err2 != nil {
		return n, err2
	}
	return n, nil
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
func validName(name string) bool { return validText(name, 64) }

// validText is a name of at most most characters: not empty, no spaces
// around it, no control characters.
func validText(name string, most int) bool {
	if name == "" || utf8.RuneCountInString(name) > most || strings.TrimSpace(name) != name || !utf8.ValidString(name) {
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
	subHost := ""
	if s.SubURL != "" {
		if subHost, err = streamURL(s.SubURL); err != nil {
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
		for _, host := range []string{s.Host, mainHost, subHost} {
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
	if r, err = normalizeDays(r); err != nil {
		return c, err
	}
	c.Record = r
	// Frigate takes at most 60 seconds before an event.
	if r.PreSeconds < 0 || r.PreSeconds > 60 || r.PostSeconds < 0 || r.PostSeconds > 60 {
		return c, invalid("record.preSeconds or record.postSeconds")
	}
	if c.Audio.TwoWay && !c.Audio.Enabled {
		return c, invalid("audio.twoWay needs audio.enabled")
	}
	for _, codec := range []string{c.Audio.Codec, c.Audio.SubCodec} {
		if len(codec) > 32 || strings.ContainsAny(codec, "\r\n\x00") {
			return c, invalid("audio.codec")
		}
	}
	for _, size := range []int{s.MainWidth, s.MainHeight, s.SubWidth, s.SubHeight} {
		if size < 0 || size > 16384 {
			return c, invalid("source: picture size")
		}
	}
	st := c.Streams
	if !slices.Contains([]string{"", StreamMain, StreamSub}, st.Record) {
		return c, invalid("streams.record")
	}
	if !slices.Contains([]string{"", StreamMain, StreamSub}, st.Detect) {
		return c, invalid("streams.detect")
	}
	if !slices.Contains([]string{"", StreamAuto, StreamMain, StreamSub}, st.Live) {
		return c, invalid("streams.live")
	}
	if s.SubURL == "" && (st.Record == StreamSub || st.Live == StreamSub) {
		return c, invalid("streams: the camera has no second stream")
	}
	if c.Notify, err = validateNotify(c.Notify); err != nil {
		return c, err
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
