package notify

import (
	"errors"
	"fmt"
	"io/fs"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Events an account can be told about by the assistant.
const (
	EventConfirmation = "confirmation" // a task waits for an approval
	EventDone         = "done"         // a task has an answer
	EventBlocked      = "blocked"      // a task cannot go on
	EventAgent        = "agent"        // the agent writes on its own (notify_me)
)

var personalEvents = []string{EventConfirmation, EventDone, EventBlocked, EventAgent}

// MaxPersonalText limits what one personal message carries.
const MaxPersonalText = 1000

// PersonalEmail sends through the mail server of the NAS to the account's own address.
type PersonalEmail struct {
	Enabled bool   `json:"enabled"`
	To      string `json:"to"`
}

// Quiet holds messages back for a part of the day, in the time zone of the NAS.
// Errors are sent anyway; what was held back follows as one message.
type Quiet struct {
	Enabled bool   `json:"enabled"`
	From    string `json:"from"` // HH:MM
	To      string `json:"to"`   // HH:MM
}

// Personal is /config/users/<name>/notify.json: where the assistant reaches
// one account. Unlike the shared channels it may carry a short summary,
// because only that account reads it.
type Personal struct {
	Language string        `json:"language"`
	Email    PersonalEmail `json:"email"`
	Ntfy     Ntfy          `json:"ntfy"`
	Telegram Telegram      `json:"telegram"`
	Discord  Discord       `json:"discord"`
	// HomeAssistant uses the shared connection with the account's own service.
	HomeAssistant HomeAssistant   `json:"homeAssistant"`
	Events        map[string]bool `json:"events"`
	// Content is summary (a short excerpt of the answer) or none.
	Content string `json:"content"`
	Quiet   Quiet  `json:"quiet"`
	// Origin is the address of the web UI the settings were saved from;
	// links in messages point there.
	Origin string `json:"origin"`
}

// PersonalMessage is one message to one account.
type PersonalMessage struct {
	Event    string
	Severity string
	Title    string
	Text     string
	// Path is a page of the web UI the message links to, e.g. /assistant?chat=…
	Path string
}

func personalFile(user string) string { return "users/" + user + "/notify.json" }

func defaultPersonal(language string) Personal {
	return Personal{Language: language, Content: "summary", Events: map[string]bool{EventConfirmation: true, EventDone: false, EventBlocked: true, EventAgent: true}}
}

var clock = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// personalState is what the manager keeps per account in memory.
type personalState struct {
	settings Personal
	sent     map[string]bool // event keys already told, see SendPersonal
	held     []PersonalMessage
	flush    *time.Timer
	recent   []time.Time // agent messages, for the rate limit
}

type personalStore struct {
	mu    sync.Mutex
	users map[string]*personalState
}

// personalLocked returns the state of an account, reading its file once.
// Called with m.people.mu held.
func (m *Manager) personalLocked(user string) (*personalState, error) {
	if m.people.users == nil {
		m.people.users = map[string]*personalState{}
	}
	if state, ok := m.people.users[user]; ok {
		return state, nil
	}
	m.mu.Lock()
	language := m.settings.Language
	m.mu.Unlock()
	settings := defaultPersonal(language)
	if err := m.store.ReadJSON(personalFile(user), &settings); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if settings.Events == nil {
		settings.Events = defaultPersonal(language).Events
	}
	state := &personalState{settings: settings, sent: map[string]bool{}}
	m.people.users[user] = state
	return state, nil
}

// PersonalSettings returns an account's settings with the secrets left out;
// the second value says for each channel whether its secret is set.
func (m *Manager) PersonalSettings(user string) (Personal, map[string]bool, error) {
	m.people.mu.Lock()
	defer m.people.mu.Unlock()
	state, err := m.personalLocked(user)
	if err != nil {
		return Personal{}, nil, err
	}
	p := state.settings
	set := map[string]bool{"ntfy": p.Ntfy.Token != "", "telegram": p.Telegram.Token != "", "discord": p.Discord.Webhook != ""}
	p.Ntfy.Token, p.Telegram.Token, p.Discord.Webhook = "", "", ""
	p.Events = maps(p.Events)
	return p, set, nil
}

func maps(in map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, e := range personalEvents {
		out[e] = in[e]
	}
	return out
}

// SetPersonal stores an account's settings. As with the shared settings, an
// empty secret keeps the stored one and clear names the channels to drop.
func (m *Manager) SetPersonal(user string, p Personal, clear ...string) error {
	m.people.mu.Lock()
	defer m.people.mu.Unlock()
	state, err := m.personalLocked(user)
	if err != nil {
		return err
	}
	old := state.settings
	for _, channel := range clear {
		switch channel {
		case "ntfy":
			old.Ntfy.Token = ""
		case "telegram":
			old.Telegram.Token = ""
		case "discord":
			old.Discord.Webhook = ""
		}
	}
	if p.Ntfy.Token == "" {
		p.Ntfy.Token = old.Ntfy.Token
	}
	if p.Telegram.Token == "" {
		p.Telegram.Token = old.Telegram.Token
	}
	if p.Discord.Webhook == "" {
		p.Discord.Webhook = old.Discord.Webhook
	}
	if p.Language != "de" && p.Language != "en" {
		return invalid("language")
	}
	if p.Content != "summary" && p.Content != "none" {
		return invalid("content")
	}
	for event := range p.Events {
		if !slices.Contains(personalEvents, event) {
			return invalid("event %q", event)
		}
	}
	p.Events = maps(p.Events)
	if p.Email.Enabled {
		if _, err := mail.ParseAddress(p.Email.To); err != nil {
			return invalid("mail address")
		}
	}
	if p.Ntfy.Enabled && !webAddress(p.Ntfy.URL) {
		return invalid("ntfy address")
	}
	if t := p.Telegram; t.Enabled && (t.Token == "" || t.ChatID == "" || strings.ContainsAny(t.Token, "/ \r\n")) {
		return invalid("Telegram bot token or chat")
	}
	if p.Discord.Enabled && !webAddress(p.Discord.Webhook) {
		return invalid("Discord webhook")
	}
	if p.HomeAssistant.Enabled && !haService.MatchString(p.HomeAssistant.Service) {
		return invalid("Home Assistant service, e.g. notify.mobile_app_phone")
	}
	if p.Quiet.Enabled && (!clock.MatchString(p.Quiet.From) || !clock.MatchString(p.Quiet.To) || p.Quiet.From == p.Quiet.To) {
		return invalid("quiet hours")
	}
	if p.Origin != "" && !webAddress(p.Origin) {
		return invalid("origin")
	}
	p.Origin = strings.TrimRight(p.Origin, "/")
	if err := m.store.WriteJSON(personalFile(user), p); err != nil {
		return err
	}
	state.settings = p
	return nil
}

// ForgetPersonal drops what is kept about a removed account; its file goes
// with the account's folder.
func (m *Manager) ForgetPersonal(user string) {
	m.people.mu.Lock()
	defer m.people.mu.Unlock()
	if state, ok := m.people.users[user]; ok && state.flush != nil {
		state.flush.Stop()
	}
	delete(m.people.users, user)
}

// Reachable says whether an account has a channel and wants an event.
func (m *Manager) Reachable(user, event string) bool {
	m.people.mu.Lock()
	defer m.people.mu.Unlock()
	state, err := m.personalLocked(user)
	if err != nil {
		return false
	}
	return state.settings.Events[event] && m.anyChannel(state.settings)
}

func (m *Manager) anyChannel(p Personal) bool {
	m.mu.Lock()
	mailServer := m.settings.Email.Host != ""
	ha := m.ha
	m.mu.Unlock()
	haReady := p.HomeAssistant.Enabled && ha != nil && ha.Active()
	return p.Email.Enabled && mailServer || p.Ntfy.Enabled || p.Telegram.Enabled || p.Discord.Enabled || haReady
}

// Summary shortens an answer for a personal message, or leaves it out.
func (p Personal) Summary(text string) string {
	if p.Content != "summary" {
		return ""
	}
	return shorten(strings.TrimSpace(text), 300)
}

func shorten(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

// ErrRateLimited means the agent wrote too often.
var ErrRateLimited = errors.New("notify: too many messages")

// ErrUnreachable means the account has no channel for this event.
var ErrUnreachable = errors.New("notify: no channel set up for this account")

// SendPersonal sends a message to one account if it wants the event. key
// makes it happen only once per task and event (empty: no such limit). The
// summary is added by the caller through Personal.Summary.
func (m *Manager) SendPersonal(user, key string, message PersonalMessage) error {
	m.people.mu.Lock()
	defer m.people.mu.Unlock()
	state, err := m.personalLocked(user)
	if err != nil {
		return err
	}
	p := state.settings
	if !p.Events[message.Event] || !m.anyChannel(p) {
		return ErrUnreachable
	}
	if key != "" {
		if state.sent[key] {
			return nil
		}
		if len(state.sent) > 2000 {
			clear(state.sent)
		}
		state.sent[key] = true
	}
	now := m.now()
	if message.Event == EventAgent {
		state.recent = slices.DeleteFunc(state.recent, func(t time.Time) bool { return now.Sub(t) > time.Hour })
		if len(state.recent) >= 20 || len(state.recent) > 0 && now.Sub(state.recent[len(state.recent)-1]) < time.Minute {
			return ErrRateLimited
		}
		state.recent = append(state.recent, now)
	}
	message.Text = shorten(message.Text, MaxPersonalText)
	if end, quiet := quietUntil(p.Quiet, now); quiet && message.Severity != Error {
		state.held = append(state.held, message)
		if len(state.held) > 50 {
			state.held = state.held[len(state.held)-50:]
		}
		if state.flush == nil {
			state.flush = time.AfterFunc(end.Sub(now), func() { m.flushHeld(user) })
		}
		return nil
	}
	m.sendTo(p, message)
	return nil
}

// TestPersonal sends a test message through one channel of an account and
// reports what went wrong.
func (m *Manager) TestPersonal(user, channel string) error {
	m.people.mu.Lock()
	state, err := m.personalLocked(user)
	m.people.mu.Unlock()
	if err != nil {
		return err
	}
	p := state.settings
	title, text := wording(p.Language, "test", "")
	send, ok := m.personalSenders(p, PersonalMessage{Severity: Info, Title: title, Text: text})[channel]
	if !ok {
		return invalid("channel")
	}
	return send()
}

func (m *Manager) flushHeld(user string) {
	m.people.mu.Lock()
	defer m.people.mu.Unlock()
	state, ok := m.people.users[user]
	if !ok {
		return
	}
	held := state.held
	state.held, state.flush = nil, nil
	if len(held) == 0 {
		return
	}
	if len(held) == 1 {
		m.sendTo(state.settings, held[0])
		return
	}
	title, _ := wording(state.settings.Language, "personal_digest", fmt.Sprint(len(held)))
	var b strings.Builder
	for _, message := range held {
		fmt.Fprintf(&b, "• %s", message.Title)
		if message.Text != "" {
			fmt.Fprintf(&b, ": %s", shorten(message.Text, 120))
		}
		b.WriteString("\n")
	}
	m.sendTo(state.settings, PersonalMessage{Severity: Info, Title: title, Text: shorten(b.String(), MaxPersonalText), Path: "/assistant"})
}

// quietUntil says whether t lies in the quiet hours and when they end.
func quietUntil(q Quiet, t time.Time) (time.Time, bool) {
	if !q.Enabled {
		return time.Time{}, false
	}
	at := func(hhmm string, day time.Time) time.Time {
		parsed, _ := time.Parse("15:04", hhmm)
		return time.Date(day.Year(), day.Month(), day.Day(), parsed.Hour(), parsed.Minute(), 0, 0, day.Location())
	}
	from, to := at(q.From, t), at(q.To, t)
	if from.Before(to) { // e.g. 13:00–15:00
		return to, !t.Before(from) && t.Before(to)
	}
	// Over midnight, e.g. 22:00–07:00.
	if !t.Before(from) {
		return to.AddDate(0, 0, 1), true
	}
	if t.Before(to) {
		return to, true
	}
	return time.Time{}, false
}

func (m *Manager) sendTo(p Personal, message PersonalMessage) {
	enabled := map[string]bool{"email": p.Email.Enabled, "ntfy": p.Ntfy.Enabled, "telegram": p.Telegram.Enabled, "discord": p.Discord.Enabled, "homeassistant": p.HomeAssistant.Enabled}
	for name, send := range m.personalSenders(p, message) {
		if enabled[name] {
			m.dispatch(name, send)
		}
	}
}

func (m *Manager) personalSenders(p Personal, message PersonalMessage) map[string]func() error {
	link := ""
	if p.Origin != "" && message.Path != "" {
		link = p.Origin + message.Path
	}
	text := message.Text
	if link != "" {
		if text != "" {
			text += "\n\n"
		}
		text += link
	}
	title := "TitanOS: " + message.Title
	return map[string]func() error{
		"email": func() error {
			m.mu.Lock()
			server := m.settings.Email
			m.mu.Unlock()
			if server.Host == "" {
				return invalid("no mail server is set up under Settings › Notifications")
			}
			server.To = p.Email.To
			return m.mail(server, title, text)
		},
		"ntfy": func() error {
			header := map[string]string{}
			if link != "" {
				header["Click"] = link
			}
			body := message.Text
			if body == "" {
				body = message.Title
			}
			return m.ntfyPost(p.Ntfy, title, message.Severity, body, header)
		},
		"telegram": func() error { return m.telegram(p.Telegram, title+"\n\n"+text) },
		"discord":  func() error { return m.discord(p.Discord, "**"+title+"**\n"+text) },
		"homeassistant": func() error {
			data := haPriority(message.Severity)
			if link != "" {
				// Opens the page when the message is tapped (Android: clickAction, iOS: url).
				data["url"], data["clickAction"] = link, link
			}
			body := message.Text
			if body == "" {
				body = message.Title
			}
			return m.haSend(p.HomeAssistant, title, body, data)
		},
	}
}
