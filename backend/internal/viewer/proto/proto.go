// Package proto holds the wire protocol of the TitanOS remote view
// ("Fernansicht", docs/FERNANSICHT.md) shared by the viewer agent
// (cmd/titanos-viewer) and titand, which relays messages between the
// browser and the agent and validates the direction towards the agent.
package proto

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"
)

// Port is the TCP port the agent listens on inside the viewer container.
const Port = 7300

// Binary message types (first byte).
const (
	TypeVideo  byte = 0x01 // agent → browser: flags, ts µs, H.264 access unit
	TypeAudio  byte = 0x02 // agent → browser: ts µs, Opus packet
	TypeUpload byte = 0x03 // browser → agent: upload id (uint32 BE), data
)

// Limits.
const (
	MaxUploadChunk   = 256 << 10 // payload bytes per 0x03 message
	MaxUploadSize    = 2 << 30   // bytes per uploaded file
	MaxClipboard     = 1 << 20   // bytes of clipboard text
	MaxText          = 10000     // runes per "text" message
	MaxTextMessage   = 6 << 20   // bytes of one JSON text message towards the agent (1 MiB clipboard, escaped)
	MaxBinaryMessage = 5 + MaxUploadChunk
	MaxNameLen       = 255     // bytes of an upload name as sent by the browser
	ProfileReserve   = 1 << 30 // bytes that must stay free on the profile volume
	ProfileCap       = 4 << 30 // bytes of uploads + downloads per profile
	MinWidth         = 320
	MaxWidth         = 3840
	MinHeight        = 240
	MaxHeight        = 2160
	MinScale         = 1.0
	MaxScale         = 3.0
)

// VideoFlagKey marks an access unit that contains an IDR picture
// (preceded by SPS and PPS).
const VideoFlagKey byte = 0x01

// Mods are the modifier states of a key message.
type Mods struct {
	Shift bool `json:"shift"`
	Ctrl  bool `json:"ctrl"`
	Alt   bool `json:"alt"`
	Meta  bool `json:"meta"`
}

// ClientMsg is any JSON message browser → agent. Only the fields of the
// respective type are meaningful.
type ClientMsg struct {
	T       string  `json:"t"`
	Width   int     `json:"width,omitempty"`
	Height  int     `json:"height,omitempty"`
	Scale   float64 `json:"scale,omitempty"`
	X       int     `json:"x,omitempty"`
	Y       int     `json:"y,omitempty"`
	Buttons int     `json:"buttons,omitempty"`
	DX      float64 `json:"dx,omitempty"`
	DY      float64 `json:"dy,omitempty"`
	Code    string  `json:"code,omitempty"`
	Key     string  `json:"key,omitempty"`
	Down    bool    `json:"down,omitempty"`
	Mods    Mods    `json:"mods"`
	Text    string  `json:"text,omitempty"`
	Mode    string  `json:"mode,omitempty"`
	FPS     int     `json:"fps,omitempty"`
	ID      uint32  `json:"id,omitempty"`
	Name    string  `json:"name,omitempty"`
	Size    int64   `json:"size,omitempty"`
}

// Client message types.
const (
	MsgSize      = "size"
	MsgMouse     = "mouse"
	MsgWheel     = "wheel"
	MsgKey       = "key"
	MsgText      = "text"
	MsgClipboard = "clipboard"
	MsgKeyframe  = "keyframe"
	MsgQuality   = "quality"
	MsgUpload    = "upload"
)

// Quality modes.
const (
	QualityAuto     = "auto"
	QualityHigh     = "high"
	QualityBalanced = "balanced"
	QualityLow      = "low"
)

// ErrInvalid is wrapped by all validation errors.
var ErrInvalid = errors.New("invalid viewer message")

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// ParseClient decodes and validates one JSON text message from the
// browser. titand may use it to check messages before relaying them.
func ParseClient(data []byte) (ClientMsg, error) {
	var m ClientMsg
	if len(data) > MaxTextMessage {
		return m, invalid("message too large")
	}
	if !utf8.Valid(data) {
		return m, invalid("not utf-8")
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, invalid("json: %v", err)
	}
	switch m.T {
	case MsgSize:
		if m.Width <= 0 || m.Height <= 0 || m.Width > 16384 || m.Height > 16384 {
			return m, invalid("size out of range")
		}
		if m.Scale == 0 {
			m.Scale = 1
		}
		if math.IsNaN(m.Scale) || m.Scale < 0.25 || m.Scale > 8 {
			return m, invalid("scale out of range")
		}
	case MsgMouse:
		if m.X < -1 || m.Y < -1 || m.X > 16384 || m.Y > 16384 || m.Buttons < 0 || m.Buttons > 31 {
			return m, invalid("mouse out of range")
		}
	case MsgWheel:
		if math.IsNaN(m.DX) || math.IsNaN(m.DY) || math.Abs(m.DX) > 100000 || math.Abs(m.DY) > 100000 {
			return m, invalid("wheel out of range")
		}
	case MsgKey:
		if m.Code == "" && m.Key == "" {
			return m, invalid("key without code and key")
		}
		if len(m.Code) > 64 || len(m.Key) > 64 {
			return m, invalid("key too long")
		}
	case MsgText:
		if utf8.RuneCountInString(m.Text) > MaxText {
			return m, invalid("text too long")
		}
	case MsgClipboard:
		if len(m.Text) > MaxClipboard {
			return m, invalid("clipboard too large")
		}
	case MsgKeyframe:
	case MsgQuality:
		switch m.Mode {
		case "", QualityAuto, QualityHigh, QualityBalanced, QualityLow:
		default:
			return m, invalid("unknown quality mode")
		}
		if m.FPS != 0 && (m.FPS < 5 || m.FPS > 60) {
			return m, invalid("fps out of range")
		}
	case MsgUpload:
		if m.ID == 0 {
			return m, invalid("upload id 0")
		}
		if m.Name == "" || len(m.Name) > MaxNameLen {
			return m, invalid("upload name")
		}
		if m.Size < 0 || m.Size > 1<<50 { // the agent answers too_large above MaxUploadSize
			return m, invalid("upload size")
		}
	default:
		return m, invalid("unknown type %q", m.T)
	}
	return m, nil
}

// CheckBinaryToAgent validates a binary message browser → agent.
// Only upload chunks are allowed in that direction.
func CheckBinaryToAgent(data []byte) error {
	if len(data) < 6 {
		return invalid("binary too short")
	}
	if data[0] != TypeUpload {
		return invalid("binary type 0x%02x not allowed towards agent", data[0])
	}
	if len(data) > MaxBinaryMessage {
		return invalid("upload chunk too large")
	}
	return nil
}

// ParseUploadChunk splits a validated 0x03 message.
func ParseUploadChunk(data []byte) (id uint32, payload []byte, err error) {
	if err := CheckBinaryToAgent(data); err != nil {
		return 0, nil, err
	}
	return binary.BigEndian.Uint32(data[1:5]), data[5:], nil
}

// Screen computes the screen size in device pixels and the browser
// scale factor for a "size" request in CSS pixels. The scale is rounded
// to 0.25 steps and lowered when the screen would exceed the maximum; the
// pixel sizes are even and inside the allowed range.
func Screen(cssW, cssH int, scale float64) (w, h int, s float64) {
	if math.IsNaN(scale) || scale <= 0 {
		scale = 1
	}
	s = math.Round(scale*4) / 4
	s = math.Max(MinScale, math.Min(MaxScale, s))
	if cssW > 0 && float64(cssW)*s > MaxWidth {
		s = math.Max(MinScale, math.Floor(MaxWidth/float64(cssW)*4)/4)
	}
	if cssH > 0 && float64(cssH)*s > MaxHeight {
		s = math.Max(MinScale, math.Floor(MaxHeight/float64(cssH)*4)/4)
	}
	w = even(clampInt(int(math.Round(float64(cssW)*s)), MinWidth, MaxWidth))
	h = even(clampInt(int(math.Round(float64(cssH)*s)), MinHeight, MaxHeight))
	return w, h, s
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func even(v int) int { return v &^ 1 }

// Server messages (agent → browser).

// Hello is sent after connecting and after every encoder (re)start; a
// key frame follows. Codec is derived from the SPS (avc1.PPCCLL).
type Hello struct {
	T      string  `json:"t"`
	Codec  string  `json:"codec"`
	Width  int     `json:"width"`
	Height int     `json:"height"`
	Scale  float64 `json:"scale"`
	GPU    string  `json:"gpu"`
	Audio  bool    `json:"audio"`
}

// Stats is sent every second. LatencyMs is the agent-side latency from
// capture to send (encoder + pipeline), not the network latency.
type Stats struct {
	T         string `json:"t"`
	FPS       int    `json:"fps"`
	Kbps      int    `json:"kbps"`
	LatencyMs int    `json:"latencyMs"`
}

// Clipboard carries clipboard text in both directions.
type Clipboard struct {
	T    string `json:"t"`
	Text string `json:"text"`
}

// Download announces a finished download, fetchable at /downloads/{id}.
type Download struct {
	T    string `json:"t"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	ID   string `json:"id"`
}

// Cursor carries the CSS cursor name of the pointer in the remote browser.
type Cursor struct {
	T     string `json:"t"`
	Shape string `json:"shape"`
}

// Uploaded confirms (or rejects, Error set) an upload.
type Uploaded struct {
	T     string `json:"t"`
	ID    uint32 `json:"id"`
	Name  string `json:"name,omitempty"`
	Error string `json:"error,omitempty"`
}

// Bye is sent before the agent closes the stream.
type Bye struct {
	T      string `json:"t"`
	Reason string `json:"reason"`
	Text   string `json:"text,omitempty"`
}

// Bye reasons. "replaced" is sent when a newer client took over.
const (
	ByeIdle     = "idle"
	ByeLogout   = "logout"
	ByeStopped  = "stopped"
	ByeError    = "error"
	ByeReplaced = "replaced"
)

// Upload error codes.
const (
	UploadTooLarge = "too_large"
	UploadFailed   = "failed"
	UploadBadName  = "bad_name"
	// UploadNoSpace: the profile volume would keep less than
	// ProfileReserve free, or the profile would exceed ProfileCap.
	UploadNoSpace = "no_space"
)

// VideoHeader builds the 10-byte header of a 0x01 message.
func VideoHeader(key bool, tsMicros uint64) []byte {
	b := make([]byte, 10)
	b[0] = TypeVideo
	if key {
		b[1] = VideoFlagKey
	}
	binary.BigEndian.PutUint64(b[2:], tsMicros)
	return b
}

// AudioHeader builds the 9-byte header of a 0x02 message.
func AudioHeader(tsMicros uint64) []byte {
	b := make([]byte, 9)
	b[0] = TypeAudio
	binary.BigEndian.PutUint64(b[1:], tsMicros)
	return b
}
