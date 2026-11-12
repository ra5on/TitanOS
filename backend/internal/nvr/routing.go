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

// Notice is one message about a camera, with the ways the owner chose for
// that camera (Camera.Notify).
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
	// Titan, Push, Email and Agent are the ways; Home Assistant gets its
	// state from HACameras.
	Titan, Push, Email, Agent bool
}

// NoticePause is the least time between two messages of one kind for one
// camera.
var NoticePause = 60 * time.Second

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
			m.report(c, kind, "nvr_event_"+kind, "info", c.Name, now)
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
			m.report(c, KindPlate, "nvr_event_plate", "info", text+" · "+c.Name, now)
		}
	}
	if e.Label == "person" && e.SubLabel != "" {
		m.noteSeen(c.ID, KindFace, e.SubLabel, now)
		if slices.Contains(c.Notify.Kinds, KindFace) && m.once(e.ID, KindFace, now) {
			m.report(c, KindFace, "nvr_event_face_known", "info", e.SubLabel+" · "+c.Name, now)
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

// report hands a message to Route unless the same kind was reported for
// this camera within NoticePause, or the camera wants no way at all.
func (m *Manager) report(c Camera, kind, message, severity, subject string, now time.Time) {
	n := c.Notify
	if !n.Titan && !n.Push && !n.Email && !n.Agent {
		return
	}
	m.mu.Lock()
	state := &m.rt.notices
	if state.last == nil {
		state.last = map[string]time.Time{}
	}
	key := c.ID + "/" + kind
	if last, ok := state.last[key]; ok && now.Sub(last) < NoticePause {
		m.mu.Unlock()
		return
	}
	state.last[key] = now
	m.mu.Unlock()
	if m.Route != nil {
		m.Route(Notice{Kind: kind, Message: message, Severity: severity, Subject: subject, Camera: c.ID,
			Titan: n.Titan, Push: n.Push, Email: n.Email, Agent: n.Agent})
	}
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
