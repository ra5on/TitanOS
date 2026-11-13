package notify

import (
	"bytes"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// The subject of a camera's message holds a recognised person's name or
// a plate ("Anna · Haustür"): it goes to the ways chosen, never into the
// system log, which ends up in the diagnosis bundle and on a volume.
func TestNotifyViaLogsNoSubject(t *testing.T) {
	m, _ := newManager(t)
	var log bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	m.NotifyVia(Info, "nvr_event_face", "Anna Beispiel · Haustür", Ways{Bell: true, Ref: "haustuer"})
	m.NotifyVia(Info, "nvr_event_plate", "M-AB 1234 · Einfahrt", Ways{})
	text := log.String()
	for _, secret := range []string{"Anna", "M-AB 1234", "Haustür", "Einfahrt"} {
		if strings.Contains(text, secret) {
			t.Errorf("the log names %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, "nvr_event_face") || !strings.Contains(text, "haustuer") {
		t.Errorf("kind and camera are not in the log: %s", text)
	}
	if got := m.Messages(); len(got) == 0 || got[len(got)-1].Subject != "Anna Beispiel · Haustür" {
		t.Errorf("the bell lost the subject: %+v", got)
	}
}

// A camera's bell message goes to the bells of its recipients only: the
// list and dismissing show it to them, a dismissal takes it out of that
// account's bell, and the personal channels of others never hear of it
// (PLAN 82-3).
func TestNotifyToBellAudience(t *testing.T) {
	m, _ := newManager(t)
	s := personalSink(t, m, "bob", Quiet{})
	m.Audience = func() []string { return []string{"alex", "bob"} }
	m.Notify(Warning, "volume_degraded", "tank")
	m.NotifyTo(Warning, "nvr_camera_offline", "Haustür", []Target{{User: "alex", Bell: true}, {User: "carl", Bell: true}}, false, About{Ref: "haustuer"})
	shown := func(user string) []string {
		var kinds []string
		for _, message := range m.MessagesFor(user) {
			kinds = append(kinds, message.Kind)
		}
		return kinds
	}
	if got := shown("alex"); len(got) != 2 || got[0] != "nvr_camera_offline" {
		t.Fatalf("alex sees %v", got)
	}
	if got := shown("bob"); len(got) != 1 || got[0] != "volume_degraded" {
		t.Fatalf("bob sees %v; the camera's message is not his", got)
	}
	if n := len(m.Messages()); n != 2 {
		t.Fatalf("all messages: %d", n)
	}
	camera := m.MessagesFor("alex")[0]
	if !slices.Equal(camera.To, []string{"alex", "carl"}) {
		t.Fatalf("audience %v", camera.To)
	}
	// bob's personal channel heard of the volume, never of the camera.
	got := s.wait(t, 1)
	time.Sleep(100 * time.Millisecond)
	if strings.Contains(strings.Join(got, "\n"), "Haustür") || len(got) != 1 {
		t.Fatalf("bob's channel got %v", got)
	}
	// bob cannot dismiss what he does not see.
	m.Dismiss("bob", []int{camera.ID})
	if n := len(m.MessagesFor("alex")); n != 2 {
		t.Fatal("bob dismissed alex's camera message")
	}
	// alex dismisses it: gone from his bell, still in carl's.
	m.Dismiss("alex", []int{camera.ID})
	if got := shown("alex"); len(got) != 1 {
		t.Fatalf("alex still sees %v", got)
	}
	if got := shown("carl"); len(got) != 2 {
		t.Fatalf("carl lost it: %v", got)
	}
	// carl dismisses it too: gone, never shown to everyone.
	m.Dismiss("carl", []int{camera.ID})
	for _, user := range []string{"alex", "bob", "carl", "dora"} {
		for _, message := range m.MessagesFor(user) {
			if message.Kind == "nvr_camera_offline" {
				t.Fatalf("%s sees the dismissed camera message", user)
			}
		}
	}
	// Without a bell recipient nothing goes into the bell at all.
	m.NotifyTo(Info, "nvr_event_person", "Garten", []Target{{User: "alex", Push: true}}, false, About{Ref: "garten"})
	if n := len(m.Messages()); n != 1 {
		t.Fatalf("a message without bell recipients went into the bell: %d", n)
	}
}

// Push goes to the devices of each recipient, e-mail to the recipient's
// own address through the NAS's mail server, the agent when asked; the
// log names no subject.
func TestNotifyToPushAndOwnEmail(t *testing.T) {
	m, _ := newManager(t)
	var log bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	var mu sync.Mutex
	var pushed, mailed []string
	m.PushTo = func(user, ref string, message Message, title string) {
		mu.Lock()
		defer mu.Unlock()
		pushed = append(pushed, user+" "+ref+" "+title)
	}
	m.sendMail = func(cfg Email, message []byte) error {
		mu.Lock()
		defer mu.Unlock()
		mailed = append(mailed, cfg.To+" "+string(message))
		return nil
	}
	observed := make(chan Message, 1)
	m.Observer = func(message Message) { observed <- message }
	p := defaultPersonal("de")
	p.Email = PersonalEmail{Enabled: false, To: "sam@example.org"}
	if err := m.SetPersonal("sam", p); err != nil {
		t.Fatal(err)
	}
	// alex has no address of his own; without a mail server nobody gets mail.
	targets := []Target{{User: "sam", Push: true, Email: true}, {User: "alex", Email: true}}
	m.NotifyTo(Info, "nvr_event_face_known", "Anna Beispiel · Haustür", targets, true, About{Ref: "haustuer"})
	if !m.PersonalMail("sam") || m.PersonalMail("alex") || m.MailServer() {
		t.Fatal("PersonalMail or MailServer")
	}
	m.settings.Email = Email{Host: "mail.example.org", Port: 587, Security: "starttls", From: "nas@example.org", To: "nas-admin@example.org"}
	m.NotifyTo(Info, "nvr_event_face_known", "Anna Beispiel · Haustür", targets, false, About{Ref: "haustuer"})
	if !m.MailServer() {
		t.Fatal("MailServer")
	}
	select {
	case message := <-observed:
		if message.Subject != "Anna Beispiel · Haustür" {
			t.Errorf("agent got %+v", message)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the agent was not told")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(mailed)
		mu.Unlock()
		if n >= 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(pushed) != 2 || !strings.HasPrefix(pushed[0], "sam haustuer [") || !strings.Contains(pushed[0], "Erkannt: Anna Beispiel") {
		t.Errorf("pushed %v (in sam's language)", pushed)
	}
	if len(mailed) != 1 || !strings.HasPrefix(mailed[0], "sam@example.org ") || strings.Contains(mailed[0], "nas-admin@") {
		t.Errorf("mailed %v: only to sam's own address", mailed)
	}
	if len(m.Messages()) != 0 {
		t.Error("no bell was chosen")
	}
	for _, secret := range []string{"Anna", "Haustür", "sam@example.org"} {
		if strings.Contains(log.String(), secret) {
			t.Errorf("the log names %q: %s", secret, log.String())
		}
	}
}

// PLAN 82-5/83: a camera's message carries the page of its event and, if
// the condition on Home Assistant could not be checked, a sentence saying
// so: in the push (both ways) and in the bell.
func TestNotifyCarriesLinkAndNote(t *testing.T) {
	m, _ := newManager(t)
	var mu sync.Mutex
	var pushed []Message
	m.PushTo = func(user, ref string, message Message, title string) {
		mu.Lock()
		defer mu.Unlock()
		pushed = append(pushed, message)
	}
	m.Push = func(message Message, title string) {
		mu.Lock()
		defer mu.Unlock()
		pushed = append(pushed, message)
	}
	link := "/nvr/events?event=1731412345.1-abc"
	m.NotifyTo(Info, "nvr_event_person", "Haustür", []Target{{User: "alex", Bell: true, Push: true}}, false, About{Ref: "haustuer", Link: link, Note: NoteHAUnknown})
	m.NotifyVia(Info, "nvr_event_person", "Garten", Ways{Push: true, Ref: "garten", Link: link})
	mu.Lock()
	defer mu.Unlock()
	if len(pushed) != 2 || pushed[0].Link != link || pushed[1].Link != link {
		t.Fatalf("pushed %+v", pushed)
	}
	if !strings.Contains(pushed[0].Text, "Home Assistant") || pushed[0].Note != NoteHAUnknown {
		t.Fatalf("note missing in the push: %q", pushed[0].Text)
	}
	if strings.Contains(pushed[1].Text, "Home Assistant") {
		t.Fatalf("a note without cause: %q", pushed[1].Text)
	}
	bell := m.MessagesFor("alex")
	if len(bell) != 1 || bell[0].Link != link || bell[0].Note != NoteHAUnknown {
		t.Fatalf("bell %+v", bell)
	}
}

// Review PLAN 85-4: an administrator with bell and e-mail for a camera got
// "Kamera nicht erreichbar" twice – directly and through the bell's
// forwarding to the own channels. One mail now.
func TestNotifyToOneMailWithBellAndEmail(t *testing.T) {
	m, _ := newManager(t)
	var mu sync.Mutex
	var mailed []string
	m.sendMail = func(cfg Email, message []byte) error {
		mu.Lock()
		defer mu.Unlock()
		mailed = append(mailed, cfg.To)
		return nil
	}
	m.Audience = func() []string { return []string{"alex"} }
	m.settings.Email = Email{Host: "mail.example.org", Port: 587, Security: "starttls", From: "nas@example.org", To: "nas-admin@example.org"}
	p := defaultPersonal("de")
	p.Email = PersonalEmail{Enabled: true, To: "alex@example.org"}
	for event := range p.Events {
		p.Events[event] = true
	}
	if err := m.SetPersonal("alex", p); err != nil {
		t.Fatal(err)
	}
	m.NotifyTo(Warning, "nvr_camera_offline", "Haustür", []Target{{User: "alex", Bell: true, Email: true}}, false, About{Ref: "haustuer"})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(mailed)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(mailed) != 1 || mailed[0] != "alex@example.org" {
		t.Errorf("mailed %v, want one mail to alex", mailed)
	}
}
