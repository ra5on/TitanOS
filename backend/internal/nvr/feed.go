package nvr

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// FeedMessage is one message of the live feed (GET /api/v1/nvr/feed).
type FeedMessage struct {
	// T is "hello", "event" or "activity".
	T string `json:"t"`
	// Running is set for "hello".
	Running *bool `json:"running,omitempty"`
	// Phase ("new", "update", "end") and Event are set for "event".
	Phase string `json:"phase,omitempty"`
	Event *Event `json:"event,omitempty"`
	// Camera, Motion and Objects are set for "activity": what is in the
	// picture right now.
	Camera  string    `json:"camera,omitempty"`
	Motion  *bool     `json:"motion,omitempty"`
	Objects *[]string `json:"objects,omitempty"`
}

// subscriber is one window listening to the feed.
type subscriber struct {
	// sees tells whether the window may get messages of a camera now.
	sees func(camera string) bool
	out  chan FeedMessage
}

// cameraActivity is what Frigate last said of one camera.
type cameraActivity struct {
	motion bool
	counts map[string]int
}

// feed keeps the one connection to Frigate's WebSocket (/ws) and hands
// what it says to the windows. Commands to Frigate (PTZ) go out over the
// same connection.
type feed struct {
	mu       sync.Mutex
	subs     map[*subscriber]struct{}
	conn     *websocket.Conn
	running  bool
	activity map[string]*cameraActivity
	// dial wakes runFeed: Frigate just came up.
	dial chan struct{}
}

func newFeed() *feed {
	return &feed{subs: map[*subscriber]struct{}{}, activity: map[string]*cameraActivity{}, dial: make(chan struct{}, 1)}
}

// wake has the connection to Frigate tried now.
func (f *feed) wake() {
	select {
	case f.dial <- struct{}{}:
	default:
	}
}

// feedRetry is the pause before Frigate's WebSocket is dialled again.
var feedRetry = 5 * time.Second

// Subscribe adds a window to the feed. sees is asked for every message
// (rights change while a window is open). The channel gets a "hello" and
// the current activity first; it drops messages when the window does not
// keep up. cancel removes the window.
func (m *Manager) Subscribe(sees func(camera string) bool) (<-chan FeedMessage, func()) {
	f := m.feed
	s := &subscriber{sees: sees, out: make(chan FeedMessage, 128)}
	f.mu.Lock()
	running := f.running
	s.out <- FeedMessage{T: "hello", Running: &running}
	for camera, a := range f.activity {
		if sees(camera) {
			s.out <- activityMessage(camera, a)
		}
	}
	f.subs[s] = struct{}{}
	f.mu.Unlock()
	return s.out, func() {
		f.mu.Lock()
		delete(f.subs, s)
		f.mu.Unlock()
	}
}

func activityMessage(camera string, a *cameraActivity) FeedMessage {
	motion := a.motion
	objects := []string{}
	for label, count := range a.counts {
		if count > 0 {
			objects = append(objects, label)
		}
	}
	slices.Sort(objects)
	return FeedMessage{T: "activity", Camera: camera, Motion: &motion, Objects: &objects}
}

// broadcast hands a message to the windows that may see its camera
// (camera "": all).
func (f *feed) broadcast(camera string, message FeedMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for s := range f.subs {
		if camera != "" && !s.sees(camera) {
			continue
		}
		select {
		case s.out <- message:
		default: // a window that does not read misses messages, the others do not wait
		}
	}
}

func (f *feed) setRunning(running bool) {
	f.mu.Lock()
	changed := f.running != running
	f.running = running
	if !running {
		f.activity = map[string]*cameraActivity{}
	}
	f.mu.Unlock()
	if changed {
		f.broadcast("", FeedMessage{T: "hello", Running: &running})
	}
}

// send writes a command to Frigate ({"topic", "payload"}).
func (f *feed) send(ctx context.Context, topic, payload string) error {
	f.mu.Lock()
	conn := f.conn
	f.mu.Unlock()
	if conn == nil {
		return ErrNotRunning
	}
	data, err := json.Marshal(map[string]any{"topic": topic, "payload": payload, "retain": false})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, data)
}

// runFeed keeps the connection to Frigate's WebSocket while the NVR
// runs, until ctx is done.
func (m *Manager) runFeed(ctx context.Context) {
	for ctx.Err() == nil {
		if base, err := m.base(); err == nil {
			m.feedSession(ctx, base)
		}
		m.feed.setRunning(false)
		select {
		case <-ctx.Done():
		case <-time.After(feedRetry):
		case <-m.feed.dial:
		}
	}
}

// feedSession is one connection: it ends when Frigate closes it or the
// NVR stops running.
func (m *Manager) feedSession(ctx context.Context, base string) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	dctx, done := context.WithTimeout(ctx, 10*time.Second)
	conn, _, err := websocket.Dial(dctx, "ws"+strings.TrimPrefix(base, "http")+"/ws", nil)
	done()
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(8 << 20)
	f := m.feed
	f.mu.Lock()
	f.conn = conn
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.conn = nil
		f.mu.Unlock()
	}()
	// Frigate answers "onConnect" with the state of every camera.
	if err := f.send(ctx, "onConnect", ""); err != nil {
		return
	}
	f.setRunning(true)
	// The connection ends with the NVR (a restart of Frigate closes it
	// by itself; this catches a container that was removed).
	go func() {
		ticker := time.NewTicker(feedRetry)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if current, err := m.base(); err != nil || current != base {
					cancel()
					return
				}
			}
		}
	}()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var message struct {
			Topic   string          `json:"topic"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(data, &message) != nil {
			continue
		}
		m.feedMessage(message.Topic, message.Payload)
	}
}

// payloadText is the payload of a message: Frigate sends a JSON string
// (often with JSON in it), a number or plain JSON.
func payloadText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return string(raw)
}

// feedMessage handles one message of Frigate. Only "events" and the
// per-camera topics "<camera>/motion" and "<camera>/<label>" are used;
// everything else Frigate says stays in titand.
func (m *Manager) feedMessage(topic string, payload json.RawMessage) {
	text := payloadText(payload)
	if topic == "events" {
		var change struct {
			Type  string       `json:"type"`
			After frigateEvent `json:"after"`
		}
		if json.Unmarshal([]byte(text), &change) != nil || !ValidEventID(change.After.ID) {
			return
		}
		if _, known := m.Camera(change.After.Camera); !known {
			return
		}
		phase := change.Type
		if phase != "new" && phase != "end" {
			phase = "update"
		}
		event := change.After.event()
		m.feed.broadcast(event.Camera, FeedMessage{T: "event", Phase: phase, Event: &event})
		m.routeEvent(event)
		return
	}
	camera, rest, ok := strings.Cut(topic, "/")
	if !ok || strings.Contains(rest, "/") {
		return
	}
	if _, known := m.Camera(camera); !known {
		return
	}
	f := m.feed
	f.mu.Lock()
	a := f.activity[camera]
	if a == nil {
		a = &cameraActivity{counts: map[string]int{}}
		f.activity[camera] = a
	}
	changed := false
	switch {
	case rest == "motion":
		motion := text == "ON"
		changed, a.motion = a.motion != motion, motion
	case slices.Contains(allowedObjects, rest):
		count := atoi(text)
		if count < 0 {
			count = 0
		}
		changed = (a.counts[rest] > 0) != (count > 0)
		a.counts[rest] = count
	}
	message := activityMessage(camera, a)
	f.mu.Unlock()
	if changed {
		f.broadcast(camera, message)
	}
}
