package notify

import (
	"log/slog"
	"os"
	"slices"
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
	// Link and Note go into the Message.
	Link, Note string
}

// About is what a message of NotifyTo is about: Ref for the system log,
// Link and Note for the Message (see Ways).
type About struct {
	Ref, Link, Note string
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
	text = withNote(settings.Language, ways.Note, text)
	message := Message{Time: time.Now(), Severity: severity, Kind: kind, Subject: subject, Title: title, Text: text, Link: ways.Link, Note: ways.Note}
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

// Target is one account a message of NotifyTo goes to, with its ways.
// The caller has checked that the account may get it now.
type Target struct {
	User string
	// Bell puts the message into this account's bell; only owners and
	// administrators have one (the caller leaves it out for others).
	Bell bool
	// Push sends it to the account's own devices (PushTo).
	Push bool
	// Email sends it to the account's own address (its notification
	// settings) through the mail server of the NAS; without an address or
	// a mail server nothing is sent.
	Email bool
}

// NotifyTo records and sends an event to some accounts, each through its
// own ways (PLAN 82-3: the recipients of a camera). Like NotifyVia it
// neither looks at the least severity nor collects for the summary.
//
//   - Bell: one message for the bell whose audience (Message.To) are the
//     accounts with Bell; only their bells and their personal channels
//     (tellPeople) get it. Without any such account nothing goes into the
//     bell: a message without an audience would be shown to everyone.
//   - Push: PushTo, to that account's devices only.
//   - Email: to the account's own address, in its language.
//   - agent: the KI-Agent (Observer), as for NotifyVia.
//
// The log names kind, ref and how many accounts got which way, never the
// subject (it may hold a recognised person's name or a plate).
func (m *Manager) NotifyTo(severity, kind, subject string, targets []Target, agent bool, about About) {
	ref := about.Ref
	var bell []string
	pushes, mails := 0, 0
	for _, t := range targets {
		if t.Bell && !slices.Contains(bell, t.User) {
			bell = append(bell, t.User)
		}
		if t.Push {
			pushes++
		}
		if t.Email {
			mails++
		}
	}
	slices.Sort(bell)
	m.mu.Lock()
	settings := m.settings
	title, text := wording(settings.Language, kind, subject)
	text = withNote(settings.Language, about.Note, text)
	message := Message{Time: time.Now(), Severity: severity, Kind: kind, Subject: subject, Title: title, Text: text, To: bell, Link: about.Link, Note: about.Note}
	if len(bell) > 0 {
		message.ID = m.nextID
		m.nextID++
		m.messages = append(m.messages, message)
		if len(m.messages) > keep {
			m.messages = m.messages[len(m.messages)-keep:]
		}
	}
	m.mu.Unlock()
	slog.Info("notification", "severity", severity, "kind", kind, "ref", ref, "bell", len(bell), "push", pushes, "email", mails, "agent", agent)
	if len(bell) > 0 {
		go m.tellPeople(message)
		m.goKeepBell()
	}
	if agent && m.Observer != nil {
		go m.Observer(message)
	}
	hostname, _ := os.Hostname()
	for _, t := range targets {
		if !t.Push && !t.Email {
			continue
		}
		// Read without m.mu: the personal settings take it themselves.
		address, language, content := m.personalMail(t.User)
		ownTitle, ownText := wording(language, kind, subject)
		ownText = withNote(language, about.Note, ownText)
		if t.Push && m.PushTo != nil {
			own := message
			own.Title, own.Text = ownTitle, ownText
			m.PushTo(t.User, ref, own, "["+hostname+"] "+ownTitle)
		}
		if !t.Email || address == "" || settings.Email.Host == "" {
			continue
		}
		// The bell forwards this message to the account's own e-mail
		// already (tellPeople): one mail, not two.
		if t.Bell && slices.Contains(bell, t.User) && m.Audience != nil && slices.Contains(m.Audience(), t.User) && m.forwardsMail(t.User, severity, kind) {
			continue
		}
		server := settings.Email
		server.To = address
		body := ownText
		if content == "none" {
			body = ownTitle
		}
		user, subjectLine := t.User, "["+hostname+"] "+ownTitle
		go func() {
			if !m.deliverPersonal(user, "email", func() error { return m.mail(server, subjectLine, body) }) {
				slog.Warn("notification not delivered", "channel", "email", "kind", kind, "ref", ref, "user", user)
			}
		}()
	}
}
