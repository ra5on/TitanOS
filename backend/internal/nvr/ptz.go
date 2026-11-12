package nvr

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// ErrPTZUnsupported: the camera does not pan, tilt or zoom.
var ErrPTZUnsupported = errors.New("nvr: the camera cannot be moved")

// ErrRateLimited: too many PTZ commands of one account.
var ErrRateLimited = errors.New("nvr: too many commands")

// PTZPerSecond is how many commands one account may send per second.
const PTZPerSecond = 10

// ptzAllowed counts a command of an account; false above PTZPerSecond.
func (m *Manager) ptzAllowed(user string) bool {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rt.ptzSent == nil {
		m.rt.ptzSent = map[string][]time.Time{}
	}
	recent := slices.DeleteFunc(m.rt.ptzSent[user], func(t time.Time) bool { return now.Sub(t) >= time.Second })
	if len(recent) >= PTZPerSecond {
		m.rt.ptzSent[user] = recent
		return false
	}
	m.rt.ptzSent[user] = append(recent, now)
	return true
}

// The PTZ actions.
var ptzMoves = []string{"move_up", "move_down", "move_left", "move_right", "zoom_in", "zoom_out"}

// PTZInfo is what a camera can do.
type PTZInfo struct {
	// Supported is false for a camera that is not set up to move or that
	// Frigate cannot steer.
	Supported bool `json:"supported"`
	// Features are Frigate's: "pt", "zoom", "pt-r", "zoom-r", …
	Features []string `json:"features"`
	Presets  []string `json:"presets"`
}

// PTZStopAfter is how long a movement goes on without a new request
// before titand stops it.
var PTZStopAfter = 5 * time.Second

// presetRE: the names of presets TitanOS passes on. Frigate reads the
// command as "preset_<name>" in lower case.
var presetRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,39}$`)

// PTZ asks Frigate what a camera can do.
func (m *Manager) PTZ(ctx context.Context, camera string) (PTZInfo, error) {
	info := PTZInfo{Features: []string{}, Presets: []string{}}
	c, ok := m.Camera(camera)
	if !ok {
		return info, ErrNotFound
	}
	if !c.PTZ || !c.Enabled {
		return info, nil
	}
	base, err := m.base()
	if err != nil {
		return info, err
	}
	var raw struct {
		Features []string `json:"features"`
		Presets  []string `json:"presets"`
	}
	if err := m.frigate.call(ctx, http.MethodGet, base, "/api/"+url.PathEscape(camera)+"/ptz/info", nil, nil, &raw); err != nil {
		return info, err
	}
	// An empty answer: Frigate did not reach the camera's ONVIF service.
	if raw.Features == nil && raw.Presets == nil {
		return info, nil
	}
	info.Supported = true
	for _, feature := range raw.Features {
		info.Features = append(info.Features, clip(feature, 40))
	}
	for _, preset := range raw.Presets {
		if presetRE.MatchString(preset) {
			info.Presets = append(info.Presets, preset)
		}
	}
	return info, nil
}

// PTZCommand moves a camera: action is one of ptzMoves, "stop" or
// "preset" (with the preset's name). A movement goes on until "stop";
// titand sends the stop itself PTZStopAfter after the last request for
// the camera, so that a closed window or a lost connection never leaves
// a camera turning.
func (m *Manager) PTZCommand(ctx context.Context, user, camera, action, preset string) error {
	c, ok := m.Camera(camera)
	if !ok {
		return ErrNotFound
	}
	if !m.ptzAllowed(user) {
		return ErrRateLimited
	}
	if !c.PTZ || !c.Enabled {
		return ErrPTZUnsupported
	}
	payload := ""
	switch {
	case slices.Contains(ptzMoves, action), action == "stop":
		payload = strings.ToUpper(action)
	case action == "preset":
		if !presetRE.MatchString(preset) {
			return invalid("preset")
		}
		payload = "preset_" + preset
	default:
		return invalid("action")
	}
	if err := m.feed.send(ctx, camera+"/ptz", payload); err != nil {
		return ErrNotRunning
	}
	m.mu.Lock()
	if timer := m.rt.ptzStops[camera]; timer != nil {
		timer.Stop()
		delete(m.rt.ptzStops, camera)
	}
	if slices.Contains(ptzMoves, action) {
		if m.rt.ptzStops == nil {
			m.rt.ptzStops = map[string]*time.Timer{}
		}
		m.rt.ptzStops[camera] = time.AfterFunc(PTZStopAfter, func() {
			sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = m.feed.send(sctx, camera+"/ptz", "STOP") // Frigate is gone: so is the movement's sender
		})
	}
	m.mu.Unlock()
	return nil
}

// Talk returns the address of go2rtc's WebRTC signalling for talking back
// through a camera (the stream without options, see render.go).
func (m *Manager) Talk(id string) (string, error) {
	c, ok := m.Camera(id)
	if !ok {
		return "", ErrNotFound
	}
	if !c.Enabled || !c.Audio.TwoWay {
		return "", ErrNotRunning
	}
	base, err := m.base()
	if err != nil {
		return "", err
	}
	return "ws" + strings.TrimPrefix(base, "http") + "/live/webrtc/api/ws?src=" + twoWayStream(id), nil
}
