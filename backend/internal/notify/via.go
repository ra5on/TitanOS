package notify

import (
	"log/slog"
	"os"
	"time"
)

// Ways are the ways one message goes. Notify takes all that are set up;
// NotifyVia only the ones named (the NVR lets the owner choose per camera).
type Ways struct {
	// Bell puts the message into the bell of TitanOS (and tells the
	// accounts that want its kind, as Notify does).
	Bell bool
	// Push sends it to subscribed browsers and phones.
	Push bool
	// Email sends it by e-mail, if e-mail is switched on in the settings.
	Email bool
	// Agent hands it to the KI-Agent (Observer).
	Agent bool
	// Ref names what the message is about in the system log (a camera's
	// ID): the subject may hold a recognised person's name or a plate and
	// is never logged.
	Ref string
}

// NotifyVia records and sends an event through the given ways only. kind
// and subject pick the wording as for Notify. Unlike Notify it neither
// looks at the least severity of the settings nor collects for the
// summary: whoever calls it has asked the owner per event. The log names
// kind and ways.Ref, never the subject.
func (m *Manager) NotifyVia(severity, kind, subject string, ways Ways) {
	m.mu.Lock()
	settings := m.settings
	title, text := wording(settings.Language, kind, subject)
	message := Message{Time: time.Now(), Severity: severity, Kind: kind, Subject: subject, Title: title, Text: text}
	if ways.Bell {
		message.ID = m.nextID
		m.nextID++
		m.messages = append(m.messages, message)
		if len(m.messages) > keep {
			m.messages = m.messages[len(m.messages)-keep:]
		}
	}
	m.mu.Unlock()
	slog.Info("notification", "severity", severity, "kind", kind, "ref", ways.Ref, "bell", ways.Bell, "push", ways.Push, "email", ways.Email, "agent", ways.Agent)
	if ways.Bell {
		go m.tellPeople(message)
		m.goKeepBell()
	}
	if ways.Agent && m.Observer != nil {
		go m.Observer(message)
	}
	hostname, _ := os.Hostname()
	if ways.Email && settings.Email.Enabled {
		m.dispatch("email", func() error { return m.mail(settings.Email, "["+hostname+"] "+title, text) })
	}
	if ways.Push && m.Push != nil {
		m.Push(message, "["+hostname+"] "+title)
	}
}
