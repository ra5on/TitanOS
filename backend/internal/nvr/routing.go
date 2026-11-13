package nvr

import (
	"slices"
	"time"
)

// The kinds of events a camera reports.
const (
	KindPerson  = "person"
	KindVehicle = "vehicle"
	KindAnimal  = "animal"
	KindPlate   = "plate"
	KindFace    = "face"
	KindOffline = "offline"
)

// kindOf is the kind of a label: vehicles and animals are groups.
func kindOf(label string) string {
	switch label {
	case "person":
		return KindPerson
	case "car", "motorcycle", "bus", "bicycle":
		return KindVehicle
	case "bird", "cat", "dog", "horse", "sheep", "cow", "bear":
		return KindAnimal
	}
	return ""
}

// Notice is one message about a camera, with whom and how it goes
// (Camera.Notify, checked when it is sent).
type Notice struct {
	// Kind is one of the kinds above; Message the kind of the text
	// (notify/texts.go: nvr_event_person, nvr_camera_offline, …).
	Kind, Message string
	// Severity is "info" or "warning".
	Severity string
	// Subject fills the text: the camera's name, for plates and faces
	// "<plate or name> · <camera>".
	Subject string
	Camera  string
	// Legacy is a camera of 0.78 without a list of recipients: Titan,
	// Push and Email are its ways, to the administrators as in 0.78.
	Legacy bool
	// Titan, Push and Email are the ways of a Legacy camera.
	Titan, Push, Email bool
	// Agent hands the message to the KI-Agent; Home Assistant gets its
	// state from HACameras.
	Agent bool
	// Targets are the recipients that may get the message now, each with
	// its ways (not Legacy). Empty is nobody.
	Targets []Target
	// Event is the ID of the event the message is about ("" for "Kamera
	// offline"): a tap on the push opens it (PLAN 82-5).
	Event string
	// HAUnknown says the camera's condition on Home Assistant could not be
	// checked (not reachable, state unknown): the message goes out anyway
	// and says so (PLAN 83).
	HAUnknown bool
}

// Target is one account a Notice goes to.
type Target struct {
	User string
	// Bell only for owners and administrators.
	Bell, Push, Email bool
}

// noticeState is what the routing remembers (guarded by Manager.mu).
type noticeState struct {
	// last is when a kind was last reported per camera: "<camera>/<kind>".
	last map[string]time.Time
	// told are plates and faces already reported per event:
	// "<event>/<kind>" → when.
	told map[string]time.Time
	// seen is when a kind last happened per camera, reported or not (for
	// Home Assistant), and what it was.
	seen     map[string]time.Time
	lastText map[string]string
}

// routeEvent decides what an event of the feed is worth telling: its
// object when the event begins, a plate or a recognised face once per
// event when Frigate has read it.
func (m *Manager) routeEvent(e Event) {
	c, ok := m.Camera(e.Camera)
	if !ok {
		return
	}
	now := m.now()
	if kind := kindOf(e.Label); kind != "" {
		m.noteSeen(c.ID, kind, e.Label, now)
		if slices.Contains(c.Notify.Kinds, e.Label) && m.once(e.ID, kind, now) {
			m.report(c, kind, "nvr_event_"+kind, "info", c.Name, e.ID, now)
		}
	}
	if e.Plate != "" {
		m.noteSeen(c.ID, KindPlate, e.Plate, now)
		if slices.Contains(c.Notify.Kinds, KindPlate) && m.once(e.ID, KindPlate, now) {
			text := e.Plate
			// A known plate carries its name as the sub label of the vehicle.
			if e.SubLabel != "" && e.Label != "person" {
				text = e.SubLabel + " (" + e.Plate + ")"
			}
			m.report(c, KindPlate, "nvr_event_plate", "info", text+" · "+c.Name, e.ID, now)
		}
	}
	if e.Label == "person" && e.SubLabel != "" {
		m.noteSeen(c.ID, KindFace, e.SubLabel, now)
		if slices.Contains(c.Notify.Kinds, KindFace) && m.once(e.ID, KindFace, now) {
			m.report(c, KindFace, "nvr_event_face_known", "info", e.SubLabel+" · "+c.Name, e.ID, now)
		}
	}
}

// once tells whether a kind was not yet handled for an event (events are
// updated many times while they last).
func (m *Manager) once(event, kind string, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := &m.rt.notices
	if n.told == nil {
		n.told = map[string]time.Time{}
	}
	for key, at := range n.told {
		if now.Sub(at) > 6*time.Hour {
			delete(n.told, key)
		}
	}
	key := event + "/" + kind
	if _, done := n.told[key]; done {
		return false
	}
	n.told[key] = now
	return true
}

func (m *Manager) noteSeen(camera, kind, text string, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := &m.rt.notices
	if n.seen == nil {
		n.seen, n.lastText = map[string]time.Time{}, map[string]string{}
	}
	n.seen[camera+"/"+kind] = now
	n.lastText[camera] = kind + ": " + text
}

// report hands a message to Route, unless
//   - the main switch silences the cameras (Settings.NotifyOff),
//   - an event happens outside the camera's window (offline is reported at
//     any time: a camera that goes dark at night is what the window must
//     not hide),
//   - nobody may get it (no way, or no recipient may see the camera now),
//   - or the same kind was reported for this camera within its pause.
//
// The checks run in this order: a message held back by the switch, the
// window, the condition on Home Assistant or the recipients does not start
// the pause.
func (m *Manager) report(c Camera, kind, message, severity, subject, event string, now time.Time) {
	m.mu.Lock()
	off := m.settings.NotifyOff
	m.mu.Unlock()
	if off {
		return
	}
	n := c.Notify
	if kind != KindOffline && !n.Window.Open(now.In(m.zone())) {
		return
	}
	haUnknown := false
	if kind != KindOffline && n.HA != nil {
		allow, unknown := m.haAllows(*n.HA)
		if !allow {
			return
		}
		haUnknown = unknown
	}
	notice := Notice{Kind: kind, Message: message, Severity: severity, Subject: subject, Camera: c.ID, Agent: n.Agent, Event: event, HAUnknown: haUnknown}
	if n.legacyWays() {
		if !n.Titan && !n.Push && !n.Email && !n.Agent {
			return
		}
		notice.Legacy, notice.Titan, notice.Push, notice.Email = true, n.Titan, n.Push, n.Email
	} else {
		// Asked outside m.mu: the accounts live in package auth.
		notice.Targets = m.targets(c, kind)
		if len(notice.Targets) == 0 && !n.Agent {
			return
		}
	}
	m.mu.Lock()
	state := &m.rt.notices
	if state.last == nil {
		state.last = map[string]time.Time{}
	}
	key := c.ID + "/" + kind
	if last, ok := state.last[key]; ok && now.Sub(last) < n.pause() {
		m.mu.Unlock()
		return
	}
	state.last[key] = now
	m.mu.Unlock()
	if m.Route != nil {
		m.Route(notice)
	}
}

// haUnknownStates are states Home Assistant gives an entity it cannot
// tell: they count as not known, not as a state that was chosen or not.
var haUnknownStates = []string{"unavailable", "unknown"}

// haAllows checks a camera's condition on Home Assistant with the states
// the watcher knows now (never asking Home Assistant on the way of an
// event). A rule whose state is not known can neither hold a message back
// nor let it through on its own: if the known rules do not decide, the
// message goes out with unknown set (owner 13.11.2026: "trotzdem melden").
func (m *Manager) haAllows(c HACondition) (allow, unknown bool) {
	holds, fails, missing := 0, 0, 0
	for _, r := range c.Rules {
		state, known := "", false
		if m.HAState != nil {
			state, known = m.HAState(r.Connection, r.Entity)
		}
		switch {
		case !known || slices.Contains(haUnknownStates, state):
			missing++
		case slices.Contains(r.States, state):
			holds++
		default:
			fails++
		}
	}
	if c.Mode == HAModeAny {
		if holds > 0 {
			return true, false
		}
		return missing > 0, missing > 0
	}
	if fails > 0 {
		return false, false
	}
	return true, missing > 0
}

// HAHold is how long a kind counts as "on" for Home Assistant after it
// happened (states are published every 30 seconds).
const HAHold = 90 * time.Second

// HACamera is a camera for Home Assistant: one binary sensor per kind,
// one for "online" and the last event as text.
type HACamera struct {
	ID, Name string
	// Online is nil while the NVR does not know.
	Online *bool
	// Active tells per kind (person, vehicle, animal, plate, face)
	// whether it happened within HAHold.
	Active map[string]bool
	// LastEvent is "<kind>: <label, plate or name>", "" if none yet.
	LastEvent string
}

// HACameras lists the cameras whose owner chose Home Assistant.
func (m *Manager) HACameras() []HACamera {
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	states := m.cameraStatesLocked()
	out := []HACamera{}
	for _, c := range m.cameras {
		if !c.Notify.HomeAssistant || !c.Enabled {
			continue
		}
		entry := HACamera{ID: c.ID, Name: c.Name, Active: map[string]bool{}, LastEvent: m.rt.notices.lastText[c.ID]}
		if state := states[c.ID]; state.Known {
			online := state.Online
			entry.Online = &online
		}
		for _, kind := range []string{KindPerson, KindVehicle, KindAnimal, KindPlate, KindFace} {
			at, ok := m.rt.notices.seen[c.ID+"/"+kind]
			entry.Active[kind] = ok && now.Sub(at) < HAHold
		}
		out = append(out, entry)
	}
	return out
}
