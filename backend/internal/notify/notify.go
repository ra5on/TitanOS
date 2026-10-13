// Package notify tells people what happens on the NAS: in the web UI and,
// where set up, by e-mail and push message (ntfy, Telegram, Discord).
//
// Messages are kept in memory for the web UI. Where they are sent to is
// stored in /config/notify/settings.json; passwords and tokens in it never
// leave the NAS through the API.
package notify

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const (
	settingsFile = "notify/settings.json"
	// keep is how many messages the web UI can look back on.
	keep = 100
)

var (
	// ErrInvalid means a setting is not acceptable; the message says which.
	ErrInvalid = errors.New("notify: invalid setting")
	// ErrNotLoaded means /config could not be read, so nothing is written
	// to it either: that would replace what is stored there.
	ErrNotLoaded = errors.New("notify: settings not loaded from /config")
)

// Severities of a message.
const (
	Info    = "info"
	Warning = "warning"
	Error   = "error"
)

// Message is something worth telling.
type Message struct {
	ID   int       `json:"id"`
	Time time.Time `json:"time"`
	// Severity is info, warning or error.
	Severity string `json:"severity"`
	// Kind names the event (volume_degraded, …); Subject what it is about.
	// The web UI makes its own text from them, in the viewer's language.
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
	// Title and Text are the message in the language of the settings, as
	// it is sent out.
	Title string `json:"title"`
	Text  string `json:"text"`
}

// Email is where and how mail is sent.
type Email struct {
	Enabled bool   `json:"enabled"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	// Security is starttls (usually port 587), tls (465) or none.
	Security string `json:"security"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	From     string `json:"from"`
	To       string `json:"to"`
}

// Ntfy posts to a topic of an ntfy server.
type Ntfy struct {
	Enabled bool `json:"enabled"`
	// URL is the address of the topic, e.g. https://ntfy.sh/my-nas-topic.
	URL   string `json:"url"`
	Token string `json:"token,omitempty"`
}

// Telegram sends through a bot.
type Telegram struct {
	Enabled bool   `json:"enabled"`
	Token   string `json:"token,omitempty"`
	ChatID  string `json:"chatID"`
}

// Discord posts to a channel through its webhook.
type Discord struct {
	Enabled bool   `json:"enabled"`
	Webhook string `json:"webhook,omitempty"`
}

// Settings is /config/notify/settings.json.
type Settings struct {
	// Language of what is sent out: de or en.
	Language string `json:"language"`
	// MinSeverity is the least severe kind of message that is sent out;
	// the web UI shows all of them.
	MinSeverity string   `json:"minSeverity"`
	Email       Email    `json:"email"`
	Ntfy        Ntfy     `json:"ntfy"`
	Telegram    Telegram `json:"telegram"`
	Discord     Discord  `json:"discord"`
}

// Manager keeps the messages and sends them out.
type Manager struct {
	store *config.Store
	http  *http.Client
	// telegramAPI is where Telegram's bots are reached.
	telegramAPI string
	// sendMail is how a mail gets out; tests replace it.
	sendMail   func(Email, []byte) error
	retryPause time.Duration

	mu       sync.Mutex
	loaded   bool
	settings Settings
	messages []Message
	nextID   int
}

// NewManager returns a manager without any channel set up.
func NewManager(store *config.Store) *Manager {
	return &Manager{
		store: store, http: &http.Client{Timeout: 20 * time.Second}, telegramAPI: "https://api.telegram.org",
		sendMail: sendMail, retryPause: 10 * time.Second, settings: Settings{Language: "en", MinSeverity: Warning}, nextID: 1,
	}
}

// Load reads the settings from /config.
func (m *Manager) Load() error {
	settings := Settings{Language: "en", MinSeverity: Warning}
	err := m.store.ReadJSON(settingsFile, &settings)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings, m.loaded = settings, true
	return nil
}

// Settings returns the settings with the secrets left out; for each one
// the second value says whether it is set.
func (m *Manager) Settings() (Settings, map[string]bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	settings := m.settings
	set := map[string]bool{
		"email": settings.Email.Password != "", "ntfy": settings.Ntfy.Token != "",
		"telegram": settings.Telegram.Token != "", "discord": settings.Discord.Webhook != "",
	}
	settings.Email.Password, settings.Ntfy.Token, settings.Telegram.Token, settings.Discord.Webhook = "", "", "", ""
	return settings, set
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func webAddress(address string) bool {
	parsed, err := url.Parse(address)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" && len(address) <= 500
}

// SetSettings stores new settings. A secret that is left empty keeps its
// stored value: the web UI never gets the old one to send back. To drop
// one, its channel is named in clear.
func (m *Manager) SetSettings(settings Settings, clear ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	old := m.settings
	for _, channel := range clear {
		switch channel {
		case "email":
			old.Email.Password = ""
		case "ntfy":
			old.Ntfy.Token = ""
		case "telegram":
			old.Telegram.Token = ""
		case "discord":
			old.Discord.Webhook = ""
		}
	}
	keepOld := func(new *string, stored string) {
		if *new == "" {
			*new = stored
		}
	}
	keepOld(&settings.Email.Password, old.Email.Password)
	keepOld(&settings.Ntfy.Token, old.Ntfy.Token)
	keepOld(&settings.Telegram.Token, old.Telegram.Token)
	keepOld(&settings.Discord.Webhook, old.Discord.Webhook)

	if settings.Language != "de" && settings.Language != "en" {
		return invalid("language")
	}
	if settings.MinSeverity != Info && settings.MinSeverity != Warning && settings.MinSeverity != Error {
		return invalid("severity")
	}
	if e := settings.Email; e.Enabled {
		if e.Host == "" || strings.ContainsAny(e.Host, " /\r\n") || e.Port < 1 || e.Port > 65535 {
			return invalid("mail server")
		}
		if e.Security != "starttls" && e.Security != "tls" && e.Security != "none" {
			return invalid("mail security")
		}
		for _, address := range []string{e.From, e.To} {
			if _, err := mail.ParseAddress(address); err != nil {
				return invalid("mail address %q", address)
			}
		}
	}
	if settings.Ntfy.Enabled && !webAddress(settings.Ntfy.URL) {
		return invalid("ntfy address")
	}
	if t := settings.Telegram; t.Enabled && (t.Token == "" || t.ChatID == "" || strings.ContainsAny(t.Token, "/ \r\n")) {
		return invalid("Telegram bot token or chat")
	}
	if settings.Discord.Enabled && !webAddress(settings.Discord.Webhook) {
		return invalid("Discord webhook")
	}
	if err := m.store.WriteJSON(settingsFile, settings); err != nil {
		return err
	}
	m.settings = settings
	return nil
}

// Messages returns the messages kept for the web UI, the newest first.
func (m *Manager) Messages() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := make([]Message, len(m.messages))
	for i, message := range m.messages {
		list[len(list)-1-i] = message
	}
	return list
}

// Latest returns the number of the newest message, 0 if there is none.
// The web UI asks for the list again when it changes.
func (m *Manager) Latest() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nextID - 1
}

// Loaded says whether the settings were read from /config.
func (m *Manager) Loaded() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loaded
}

var rank = map[string]int{Info: 0, Warning: 1, Error: 2}

// Notify records an event and sends it out where that is set up. kind and
// subject pick the wording; see texts.go.
func (m *Manager) Notify(severity, kind, subject string) {
	m.mu.Lock()
	settings := m.settings
	title, text := wording(settings.Language, kind, subject)
	message := Message{ID: m.nextID, Time: time.Now(), Severity: severity, Kind: kind, Subject: subject, Title: title, Text: text}
	m.nextID++
	m.messages = append(m.messages, message)
	if len(m.messages) > keep {
		m.messages = m.messages[len(m.messages)-keep:]
	}
	m.mu.Unlock()
	slog.Info("notification", "severity", severity, "kind", kind, "subject", subject)
	if rank[severity] >= rank[settings.MinSeverity] {
		m.deliver(settings, message, nil)
	}
}

// Test sends a test message through one channel and reports what went wrong.
func (m *Manager) Test(channel string) error {
	m.mu.Lock()
	settings := m.settings
	m.mu.Unlock()
	title, text := wording(settings.Language, "test", "")
	return m.deliver(settings, Message{Time: time.Now(), Severity: Info, Kind: "test", Title: title, Text: text}, &channel)
}

// deliver sends a message through the enabled channels in the background,
// three attempts each; or through the one named, enabled or not, once and
// while the caller waits.
func (m *Manager) deliver(settings Settings, message Message, only *string) error {
	hostname, _ := os.Hostname()
	title := "[" + hostname + "] " + message.Title
	channels := map[string]func() error{
		"email":    func() error { return m.mail(settings.Email, title, message.Text) },
		"ntfy":     func() error { return m.ntfy(settings.Ntfy, title, message) },
		"telegram": func() error { return m.telegram(settings.Telegram, title+"\n\n"+message.Text) },
		"discord":  func() error { return m.discord(settings.Discord, "**"+title+"**\n"+message.Text) },
	}
	enabled := map[string]bool{"email": settings.Email.Enabled, "ntfy": settings.Ntfy.Enabled, "telegram": settings.Telegram.Enabled, "discord": settings.Discord.Enabled}
	if only != nil {
		send, known := channels[*only]
		if !known {
			return invalid("channel")
		}
		return send()
	}
	for name, send := range channels {
		if !enabled[name] {
			continue
		}
		// Each channel on its own: one that hangs does not hold up the others.
		go func() {
			var err error
			for attempt := range 3 {
				if err = send(); err == nil {
					return
				}
				time.Sleep(time.Duration(attempt+1) * m.retryPause)
			}
			slog.Error("notification not delivered", "channel", name, "err", err)
		}()
	}
	return nil
}

// post sends a request and treats every answer but a success as an error.
func (m *Manager) post(address string, header map[string]string, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, address, bytes.NewReader(body))
	if err != nil {
		return err
	}
	for name, value := range header {
		req.Header.Set(name, value)
	}
	res, err := m.http.Do(req)
	if err != nil {
		// The address may hold a token; do not carry it into the log.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return urlErr.Err
		}
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return fmt.Errorf("the server answered %s: %s", res.Status, strings.TrimSpace(string(detail)))
	}
	return nil
}

func (m *Manager) ntfy(cfg Ntfy, title string, message Message) error {
	header := map[string]string{"Title": title, "Priority": map[string]string{Info: "default", Warning: "high", Error: "urgent"}[message.Severity], "Tags": "floppy_disk"}
	if cfg.Token != "" {
		header["Authorization"] = "Bearer " + cfg.Token
	}
	return m.post(cfg.URL, header, []byte(message.Text))
}

func (m *Manager) telegram(cfg Telegram, text string) error {
	body, _ := json.Marshal(map[string]string{"chat_id": cfg.ChatID, "text": text})
	return m.post(m.telegramAPI+"/bot"+cfg.Token+"/sendMessage", map[string]string{"Content-Type": "application/json"}, body)
}

func (m *Manager) discord(cfg Discord, content string) error {
	body, _ := json.Marshal(map[string]string{"content": content})
	return m.post(cfg.Webhook, map[string]string{"Content-Type": "application/json"}, body)
}

func (m *Manager) mail(cfg Email, subject, text string) error {
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n",
		from.String(), cfg.To, encodeHeader(subject), time.Now().Format(time.RFC1123Z), strings.ReplaceAll(text, "\n", "\r\n"))
	return m.sendMail(cfg, []byte(b.String()))
}

// encodeHeader writes a header value with characters outside ASCII the way mail expects.
func encodeHeader(value string) string {
	return mime.QEncoding.Encode("utf-8", value)
}

// sendMail hands a message to the mail server.
func sendMail(cfg Email, message []byte) error {
	address := net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port))
	from, _ := mail.ParseAddress(cfg.From)
	to, err := mail.ParseAddress(cfg.To)
	if err != nil {
		return err
	}
	dialer := &net.Dialer{Timeout: 20 * time.Second}
	var conn net.Conn
	if cfg.Security == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", address, &tls.Config{ServerName: cfg.Host})
	} else {
		conn, err = dialer.Dial("tcp", address)
	}
	if err != nil {
		return err
	}
	conn.SetDeadline(time.Now().Add(60 * time.Second))
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer client.Close()
	if cfg.Security == "starttls" {
		if err := client.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
			return err
		}
	}
	// A password never goes over a connection that is not encrypted.
	if cfg.Username != "" && cfg.Security != "none" {
		if err := client.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return err
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return err
	}
	if err := client.Rcpt(to.Address); err != nil {
		return err
	}
	body, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := body.Write(message); err != nil {
		return err
	}
	if err := body.Close(); err != nil {
		return err
	}
	return client.Quit()
}
