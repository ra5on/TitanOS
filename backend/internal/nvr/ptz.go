package nvr

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/coder/websocket"
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

// A stop that does not go through is tried again every PTZStopRetry for
// PTZStopTrying; after that it is sent when the connection to Frigate
// stands again, and when titand ends.
var (
	PTZStopRetry  = 2 * time.Second
	PTZStopTrying = 60 * time.Second
)

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
	// A stop always goes through: refused, the camera would turn on until
	// the automatic stop (review PLAN 85-4).
	if action != "stop" && !m.ptzAllowed(user) {
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
	if action == "stop" {
		m.mu.Lock()
		if timer := m.rt.ptzStops[camera]; timer != nil {
			timer.Stop()
			delete(m.rt.ptzStops, camera)
		}
		m.mu.Unlock()
		if !m.stopPTZ(camera) {
			return ErrNotRunning // tried again in the background
		}
		return nil
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
			m.rt.ptzStops, m.rt.ptzMoving = map[string]*time.Timer{}, map[string]bool{}
		}
		// A new movement replaces a stop that is still being tried: its
		// own stop follows.
		if timer := m.rt.ptzRetries[camera]; timer != nil {
			timer.Stop()
			delete(m.rt.ptzRetries, camera)
		}
		delete(m.rt.ptzOwed, camera)
		m.rt.ptzMoving[camera] = true
		var timer *time.Timer
		timer = time.AfterFunc(PTZStopAfter, func() {
			// Only the timer of the last movement stops the camera.
			m.mu.Lock()
			current := m.rt.ptzStops[camera] == timer
			if current {
				delete(m.rt.ptzStops, camera)
			}
			m.mu.Unlock()
			if current {
				m.stopPTZ(camera)
			}
		})
		m.rt.ptzStops[camera] = timer
	}
	m.mu.Unlock()
	return nil
}

// stopPTZ sends STOP to a camera and reports whether it went through.
// If not, it is tried again every PTZStopRetry for PTZStopTrying, and
// owed (ptzOwed) until it goes through: the feed sends it when its
// connection stands again, and when titand ends.
func (m *Manager) stopPTZ(camera string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := m.feed.send(ctx, camera+"/ptz", "STOP")
	cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	if timer := m.rt.ptzRetries[camera]; timer != nil {
		timer.Stop()
		delete(m.rt.ptzRetries, camera)
	}
	if err == nil {
		delete(m.rt.ptzMoving, camera)
		delete(m.rt.ptzOwed, camera)
		return true
	}
	if m.rt.ptzOwed == nil {
		m.rt.ptzOwed, m.rt.ptzRetries = map[string]time.Time{}, map[string]*time.Timer{}
	}
	first, owed := m.rt.ptzOwed[camera]
	if !owed {
		first = time.Now()
		m.rt.ptzOwed[camera] = first
		slog.Warn("NVR: a camera did not get its stop; trying again", "camera", camera, "err", err)
	}
	if time.Since(first) < PTZStopTrying {
		m.rt.ptzRetries[camera] = time.AfterFunc(PTZStopRetry, func() { m.stopPTZ(camera) })
	} else {
		slog.Warn("NVR: a camera did not get its stop; it is sent when Frigate is reached again", "camera", camera)
	}
	return false
}

// owedStops are the cameras whose stop did not go through; with moving,
// also those set moving whose stop is still to come (titand ends).
func (m *Manager) owedStops(moving bool) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []string{}
	for camera := range m.rt.ptzOwed {
		out = append(out, camera)
	}
	if moving {
		for camera := range m.rt.ptzMoving {
			if !slices.Contains(out, camera) {
				out = append(out, camera)
			}
		}
	}
	slices.Sort(out)
	return out
}

// sendOwedStops sends the stops that are owed over conn (a connection to
// Frigate that just stood up, or the last one before titand ends).
func (m *Manager) sendOwedStops(conn *websocket.Conn, moving bool) {
	for _, camera := range m.owedStops(moving) {
		data, err := json.Marshal(map[string]any{"topic": camera + "/ptz", "payload": "STOP", "retain": false})
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = conn.Write(ctx, websocket.MessageText, data)
		cancel()
		if err != nil {
			slog.Warn("NVR: stop not sent", "camera", camera, "err", err)
			continue
		}
		m.mu.Lock()
		delete(m.rt.ptzOwed, camera)
		delete(m.rt.ptzMoving, camera)
		if timer := m.rt.ptzRetries[camera]; timer != nil {
			timer.Stop()
			delete(m.rt.ptzRetries, camera)
		}
		if timer := m.rt.ptzStops[camera]; timer != nil && moving {
			timer.Stop()
			delete(m.rt.ptzStops, camera)
		}
		m.mu.Unlock()
		slog.Info("NVR: owed stop sent", "camera", camera)
	}
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
