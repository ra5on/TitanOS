package notify

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/storage"
)

func newManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	m := NewManager(config.Open(dir))
	m.retryPause = time.Millisecond
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m, dir
}

func TestDismissKeepsNewArrivalsAndRefreshesOtherBrowsers(t *testing.T) {
	m, _ := newManager(t)
	m.Notify(Info, "test", "first")
	first := m.Messages()[0].ID
	m.Notify(Warning, "test", "second")
	revision := m.Latest()
	m.Dismiss([]int{first, first, 999})
	if messages := m.Messages(); len(messages) != 1 || messages[0].Subject != "second" {
		t.Fatalf("new arrival lost: %+v", messages)
	}
	if m.Latest() <= revision {
		t.Fatal("other browsers will not refresh")
	}
	m.Dismiss([]int{first})
	if m.Latest() != revision+1 {
		t.Fatal("no-op changed revision")
	}
	m.Dismiss([]int{m.Messages()[0].ID})
	if len(m.Messages()) != 0 {
		t.Fatal("last message retained")
	}
	m.Notify(Info, "test", "third")
	if m.Messages()[0].ID <= revision {
		t.Fatal("deleted IDs were reused")
	}
}

// sink records what the push services would have received.
type sink struct {
	mu       sync.Mutex
	requests []string
	headers  []http.Header
	fail     int
}

func (s *sink) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail > 0 {
		s.fail--
		http.Error(w, "try again", http.StatusBadGateway)
		return
	}
	s.requests = append(s.requests, r.URL.Path+" "+string(body))
	s.headers = append(s.headers, r.Header.Clone())
}

func (s *sink) wait(t *testing.T, n int) []string {
	t.Helper()
	for range 400 {
		s.mu.Lock()
		got := append([]string(nil), s.requests...)
		s.mu.Unlock()
		if len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("fewer than %d requests arrived", n)
	return nil
}

func TestSecretsStayOnTheNAS(t *testing.T) {
	m, dir := newManager(t)
	settings := Settings{Language: "de", MinSeverity: Warning,
		Ntfy:     Ntfy{Enabled: true, URL: "https://ntfy.example/topic", Token: "tk_secret"},
		Telegram: Telegram{Enabled: true, Token: "123:abc", ChatID: "42"},
		Discord:  Discord{Enabled: true, Webhook: "https://discord.example/api/webhooks/1/secret"},
		Email:    Email{Enabled: true, Host: "mail.example", Port: 587, Security: "starttls", Username: "nas", Password: "pw", From: "nas@example.org", To: "me@example.org"},
	}
	if err := m.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
	shown, set := m.Settings()
	out, _ := json.Marshal(shown)
	for _, secret := range []string{"tk_secret", "123:abc", "webhooks/1/secret", `"pw"`} {
		if strings.Contains(string(out), secret) {
			t.Errorf("the settings give away %q: %s", secret, out)
		}
	}
	for _, channel := range []string{"email", "ntfy", "telegram", "discord"} {
		if !set[channel] {
			t.Errorf("%s: secret not reported as set", channel)
		}
	}

	// Saving what the web UI got back keeps the secrets.
	shown.Ntfy.URL = "https://ntfy.example/other"
	if err := m.SetSettings(shown); err != nil {
		t.Fatal(err)
	}
	again := NewManager(config.Open(dir))
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if again.settings.Ntfy.Token != "tk_secret" || again.settings.Email.Password != "pw" || again.settings.Ntfy.URL != "https://ntfy.example/other" {
		t.Errorf("after saving again: %+v", again.settings)
	}
	// A secret goes away only when that is asked for.
	if err := m.SetSettings(shown, "ntfy"); err != nil {
		t.Fatal(err)
	}
	if _, set := m.Settings(); set["ntfy"] || !set["email"] {
		t.Errorf("after dropping the ntfy token: %v", set)
	}
	if info, err := os.Stat(filepath.Join(dir, "notify", "settings.json")); err != nil || info.Size() == 0 {
		t.Errorf("settings not written: %v", err)
	}
}

func TestNothingIsWrittenWithoutTheStick(t *testing.T) {
	m := NewManager(config.Open(t.TempDir()))
	if err := m.SetSettings(Settings{Language: "en", MinSeverity: Warning}); !errors.Is(err, ErrNotLoaded) {
		t.Errorf("settings stored without having been loaded: %v", err)
	}
}

func TestInvalidSettings(t *testing.T) {
	m, _ := newManager(t)
	base := Settings{Language: "en", MinSeverity: Warning}
	cases := map[string]func(*Settings){
		"language":        func(s *Settings) { s.Language = "fr" },
		"severity":        func(s *Settings) { s.MinSeverity = "loud" },
		"ntfy address":    func(s *Settings) { s.Ntfy = Ntfy{Enabled: true, URL: "ftp://x/topic"} },
		"telegram token":  func(s *Settings) { s.Telegram = Telegram{Enabled: true, Token: "a/b", ChatID: "1"} },
		"telegram chat":   func(s *Settings) { s.Telegram = Telegram{Enabled: true, Token: "ab"} },
		"discord webhook": func(s *Settings) { s.Discord = Discord{Enabled: true} },
		"mail port": func(s *Settings) {
			s.Email = Email{Enabled: true, Host: "h", Security: "tls", From: "a@b.c", To: "a@b.c"}
		},
		"mail security": func(s *Settings) {
			s.Email = Email{Enabled: true, Host: "h", Port: 25, Security: "x", From: "a@b.c", To: "a@b.c"}
		},
		"mail address": func(s *Settings) {
			s.Email = Email{Enabled: true, Host: "h", Port: 25, Security: "none", From: "a@b.c", To: "nobody"}
		},
	}
	for name, change := range cases {
		settings := base
		change(&settings)
		if err := m.SetSettings(settings); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: accepted (%v)", name, err)
		}
	}
	// What is turned off need not be filled in.
	if err := m.SetSettings(base); err != nil {
		t.Errorf("empty settings: %v", err)
	}
}

func TestPushChannels(t *testing.T) {
	m, _ := newManager(t)
	received := &sink{fail: 2}
	server := httptest.NewServer(received)
	defer server.Close()
	m.telegramAPI = server.URL
	err := m.SetSettings(Settings{Language: "de", MinSeverity: Warning,
		Ntfy:     Ntfy{Enabled: true, URL: server.URL + "/topic", Token: "tk"},
		Telegram: Telegram{Enabled: true, Token: "123:abc", ChatID: "42"},
		Discord:  Discord{Enabled: true, Webhook: server.URL + "/hook"},
	})
	if err != nil {
		t.Fatal(err)
	}

	m.Notify(Info, "volume_ok", "tank") // below the threshold: only for the web UI
	m.Notify(Error, "volume_degraded", "tank")
	got := strings.Join(received.wait(t, 3), "\n")
	for _, want := range []string{"/topic Das Volume tank läuft ohne Redundanz", "/bot123:abc/sendMessage", `"chat_id":"42"`, "/hook ", "eine Platte fehlt"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "wieder in Ordnung") {
		t.Error("an info message was sent although only warnings are wanted")
	}
	received.mu.Lock()
	for _, header := range received.headers {
		if title := header.Get("Title"); title != "" {
			if header.Get("Priority") != "urgent" || header.Get("Authorization") != "Bearer tk" {
				t.Errorf("ntfy headers: %v", header)
			}
		}
	}
	received.mu.Unlock()

	messages := m.Messages()
	if len(messages) != 2 || messages[0].Kind != "volume_degraded" || messages[1].Kind != "volume_ok" {
		t.Errorf("messages for the web UI: %+v", messages)
	}
}

func TestTestReportsFailure(t *testing.T) {
	m, _ := newManager(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such topic", http.StatusNotFound)
	}))
	defer server.Close()
	if err := m.SetSettings(Settings{Language: "en", MinSeverity: Warning, Ntfy: Ntfy{URL: server.URL + "/secret-topic"}}); err != nil {
		t.Fatal(err)
	}
	err := m.Test("ntfy")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("failure not reported: %v", err)
	}
	if err := m.Test("pigeon"); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown channel: %v", err)
	}
	// An address that cannot be reached must not show up in the error:
	// with Telegram and Discord it holds the secret.
	server.Close()
	if err := m.Test("ntfy"); err == nil || strings.Contains(err.Error(), "secret-topic") {
		t.Errorf("error carries the address: %v", err)
	}
	if len(m.Messages()) != 0 {
		t.Error("a test message was kept for the web UI")
	}
}

func TestKeepsOnlyTheLatestMessages(t *testing.T) {
	m, _ := newManager(t)
	for i := range keep + 20 {
		m.Notify(Info, "volume_ok", strconv.Itoa(i))
	}
	messages := m.Messages()
	if len(messages) != keep || messages[0].Subject != strconv.Itoa(keep+19) || messages[0].ID != keep+20 {
		t.Errorf("%d messages, newest %+v", len(messages), messages[0])
	}
}

// fakeSMTP speaks just enough SMTP to take one mail.
func fakeSMTP(t *testing.T) (string, int, chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	got := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		say := func(line string) { io.WriteString(conn, line+"\r\n") }
		say("220 fake")
		var session strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			session.WriteString(line)
			switch command := strings.ToUpper(strings.TrimSpace(line)); {
			case strings.HasPrefix(command, "EHLO"):
				say("250-fake")
				say("250 AUTH PLAIN")
			case strings.HasPrefix(command, "AUTH"):
				say("235 ok")
			case command == "DATA":
				say("354 go on")
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					if line == ".\r\n" {
						break
					}
					session.WriteString(line)
				}
				say("250 taken")
			case command == "QUIT":
				say("221 bye")
				got <- session.String()
				return
			default:
				say("250 ok")
			}
		}
	}()
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	number, _ := strconv.Atoi(port)
	return host, number, got
}

func TestMail(t *testing.T) {
	m, _ := newManager(t)
	host, port, got := fakeSMTP(t)
	err := m.SetSettings(Settings{Language: "de", MinSeverity: Warning,
		Email: Email{Host: host, Port: port, Security: "none", Username: "nas", Password: "pw", From: "TitanOS <nas@example.org>", To: "me@example.org"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Test("email"); err != nil {
		t.Fatal(err)
	}
	session := <-got
	if strings.Contains(session, "AUTH") {
		t.Errorf("the password went over a connection that is not encrypted:\n%s", session)
	}
	for _, want := range []string{"MAIL FROM:<nas@example.org>", "RCPT TO:<me@example.org>", "Subject: ", "Testnachricht", "Content-Type: text/plain; charset=utf-8", "funktioniert dieser Weg für Benachrichtigungen"} {
		if !strings.Contains(session, want) {
			t.Errorf("%q missing in:\n%s", want, session)
		}
	}
}

func TestHeaderEncoding(t *testing.T) {
	if got := encodeHeader("[nas] Volume tank"); got != "[nas] Volume tank" {
		t.Errorf("plain subject changed: %q", got)
	}
	if got := encodeHeader("Prüfung"); !strings.HasPrefix(got, "=?utf-8?q?") {
		t.Errorf("subject not encoded: %q", got)
	}
}

func volume(name, state string) storage.Volume {
	return storage.Volume{Name: name, State: state, Mounted: true, SizeBytes: 1000, UsedBytes: 100}
}

func kinds(m *Manager) string {
	var list []string
	messages := m.Messages()
	for i := len(messages) - 1; i >= 0; i-- {
		list = append(list, messages[i].Kind+":"+messages[i].Subject)
	}
	return strings.Join(list, " ")
}

func TestWatcherReportsChanges(t *testing.T) {
	m, _ := newManager(t)
	w := NewWatcher(m)
	look := func(volumes ...storage.Volume) { w.Observe(storage.Status{Volumes: volumes}) }

	// While the NAS starts the array is not there yet; that is not news.
	look(volume("tank", storage.VolumeMissing))
	look(volume("tank", storage.VolumeOK))
	look(volume("tank", storage.VolumeOK))
	if got := kinds(m); got != "" {
		t.Fatalf("messages at the start: %s", got)
	}

	look(volume("tank", storage.VolumeDegraded))
	if got := kinds(m); got != "" {
		t.Fatalf("reported at the first look: %s", got)
	}
	look(volume("tank", storage.VolumeDegraded))
	look(volume("tank", storage.VolumeDegraded))
	// A check in between does not make it news again.
	look(volume("tank", storage.VolumeChecking))
	look(volume("tank", storage.VolumeDegraded))
	look(volume("tank", storage.VolumeDegraded))
	look(volume("tank", storage.VolumeRebuilding))
	look(volume("tank", storage.VolumeRebuilding))
	look(volume("tank", storage.VolumeOK))
	look(volume("tank", storage.VolumeOK))
	if got, want := kinds(m), "volume_degraded:tank volume_rebuilding:tank volume_ok:tank"; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestWatcherReportsTroubleAtTheStart(t *testing.T) {
	m, _ := newManager(t)
	w := NewWatcher(m)
	for range 3 {
		w.Observe(storage.Status{Volumes: []storage.Volume{volume("tank", storage.VolumeMissing), volume("fast", storage.VolumeOK)}})
	}
	if got := kinds(m); got != "volume_missing:tank" {
		t.Errorf("got %s", got)
	}
}

func TestWatcherWarnsOnce(t *testing.T) {
	m, _ := newManager(t)
	w := NewWatcher(m)
	state := func(used uint64, temperature int, healthy bool, checkState string, mismatches uint64) {
		v := volume("tank", checkState)
		v.UsedBytes, v.Mismatches = used, mismatches
		w.Observe(storage.Status{Volumes: []storage.Volume{v}, Disks: []storage.Disk{
			{Name: "sda", Model: "WD Red", Serial: "S1", Smart: &storage.Smart{Healthy: healthy, TemperatureC: temperature}},
			{Name: "sdb"},
		}})
	}
	state(100, 35, true, storage.VolumeOK, 0)
	state(910, 56, false, storage.VolumeOK, 0)
	state(950, 58, false, storage.VolumeChecking, 0)
	state(880, 52, false, storage.VolumeChecking, 3) // between the thresholds: nothing new
	state(910, 56, false, storage.VolumeOK, 3)
	state(700, 40, true, storage.VolumeOK, 3)
	state(920, 60, false, storage.VolumeOK, 3)
	want := "volume_full:tank disk_unhealthy:sda (WD Red, S1) disk_hot:sda (WD Red, S1) volume_mismatch:tank volume_full:tank disk_unhealthy:sda (WD Red, S1) disk_hot:sda (WD Red, S1)"
	if got := kinds(m); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// The bell survives a restart when a volume holds it.
func TestBellKeptOnTheVolume(t *testing.T) {
	dir := t.TempDir()
	first, _ := newManager(t)
	first.SetPersonalDataDir(func() (string, bool) { return dir, true })
	first.Notify(Warning, "volume_degraded", "tank")
	first.Notify(Info, "update_available", "0.41.0")
	for range 100 {
		if _, err := os.Stat(filepath.Join(dir, bellFile)); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	second, _ := newManager(t)
	second.SetPersonalDataDir(func() (string, bool) { return dir, true })
	second.Notify(Error, "disk_unhealthy", "sdb")
	list := second.Messages()
	if len(list) != 3 || list[0].Kind != "disk_unhealthy" || list[2].Kind != "volume_degraded" {
		t.Fatalf("bell after restart: %+v", list)
	}
	ids := map[int]bool{}
	for _, m := range list {
		if ids[m.ID] {
			t.Fatal("ids twice")
		}
		ids[m.ID] = true
	}
}
