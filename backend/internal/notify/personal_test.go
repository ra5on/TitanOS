package notify

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func personalSink(t *testing.T, m *Manager, user string, quiet Quiet) *sink {
	t.Helper()
	s := &sink{}
	srv := httptest.NewServer(s)
	// The test server is on loopback, which a plain user may not reach.
	m.Confined = func(string) bool { return false }
	t.Cleanup(srv.Close)
	server := srv.URL
	p := defaultPersonal("en")
	p.Ntfy = Ntfy{Enabled: true, URL: server + "/me", Token: "secret-token"}
	p.Events[EventDone] = true
	p.Quiet = quiet
	p.Origin = "https://nas.local/"
	if err := m.SetPersonal(user, p); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPersonalSecretsStayOnTheNAS(t *testing.T) {
	m, dir := newManager(t)
	personalSink(t, m, "alex", Quiet{})
	got, set, err := m.PersonalSettings("alex")
	if err != nil {
		t.Fatal(err)
	}
	if got.Ntfy.Token != "" || !set["ntfy"] {
		t.Fatalf("token leaked or not marked: %+v %v", got.Ntfy, set)
	}
	// An empty token keeps the stored one, clearSecrets drops it.
	got.Ntfy.Token = ""
	if err := m.SetPersonal("alex", got); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "users/alex/notify.json"))
	if !strings.Contains(string(data), "secret-token") {
		t.Fatalf("stored token lost: %s", data)
	}
	if err := m.SetPersonal("alex", got, "ntfy"); err != nil {
		t.Fatal(err)
	}
	if _, set, _ := m.PersonalSettings("alex"); set["ntfy"] {
		t.Fatal("token not cleared")
	}
	// Other accounts know nothing of it.
	if other, set, _ := m.PersonalSettings("sam"); other.Ntfy.Enabled || set["ntfy"] {
		t.Fatal("settings shared between accounts")
	}
}

func TestPersonalOncePerKeyAndWithLink(t *testing.T) {
	m, _ := newManager(t)
	s := personalSink(t, m, "alex", Quiet{})
	msg := PersonalMessage{Event: EventDone, Severity: Info, Title: "Done: report", Text: "All volumes fine.", Path: "/assistant?chat=c1"}
	for range 2 {
		if err := m.SendPersonal("alex", "c1/done/3", msg); err != nil {
			t.Fatal(err)
		}
	}
	got := s.wait(t, 1)
	time.Sleep(20 * time.Millisecond)
	if len(s.requests) != 1 || !strings.Contains(got[0], "All volumes fine.") {
		t.Fatalf("sent %v", s.requests)
	}
	if click := s.headers[0].Get("Click"); click != "https://nas.local/assistant?chat=c1" {
		t.Fatalf("click %q", click)
	}
	if s.headers[0].Get("Authorization") != "Bearer secret-token" {
		t.Fatal("token not used")
	}
	// An event the account switched off is not sent.
	p, _, _ := m.PersonalSettings("alex")
	p.Events[EventDone] = false
	m.SetPersonal("alex", p)
	if err := m.SendPersonal("alex", "c1/done/5", msg); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("switched-off event: %v", err)
	}
}

func TestPersonalAgentRateLimit(t *testing.T) {
	m, _ := newManager(t)
	// Deliveries run in the background and read the clock too.
	var clock sync.Mutex
	current := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { clock.Lock(); defer clock.Unlock(); return current }
	advance := func(d time.Duration) { clock.Lock(); current = current.Add(d); clock.Unlock() }
	personalSink(t, m, "alex", Quiet{})
	msg := PersonalMessage{Event: EventAgent, Severity: Info, Title: "Hi", Text: "x"}
	if err := m.SendPersonal("alex", "", msg); err != nil {
		t.Fatal(err)
	}
	if err := m.SendPersonal("alex", "", msg); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("second within a minute: %v", err)
	}
	for i := range 19 {
		advance(time.Minute + time.Second)
		if err := m.SendPersonal("alex", "", msg); err != nil {
			t.Fatalf("message %d: %v", i+2, err)
		}
	}
	advance(2 * time.Minute)
	if err := m.SendPersonal("alex", "", msg); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("21st within an hour: %v", err)
	}
}

func TestQuietHoursHoldAndDigest(t *testing.T) {
	m, _ := newManager(t)
	s := personalSink(t, m, "alex", Quiet{Enabled: true, From: "22:00", To: "07:00"})
	night := time.Now().Add(-time.Hour)
	// Pick a clock inside the quiet hours that ends in a moment.
	m.now = func() time.Time {
		return time.Date(night.Year(), night.Month(), night.Day(), 6, 59, 59, 900_000_000, time.Local)
	}
	for i, title := range []string{"first", "second"} {
		if err := m.SendPersonal("alex", title, PersonalMessage{Event: EventDone, Severity: Info, Title: title}); err != nil {
			t.Fatal(i, err)
		}
	}
	// Errors are not held back.
	if err := m.SendPersonal("alex", "err", PersonalMessage{Event: EventBlocked, Severity: Error, Title: "broken"}); err != nil {
		t.Fatal(err)
	}
	got := s.wait(t, 2)
	if !strings.Contains(strings.Join(got, "|"), "broken") || !strings.Contains(strings.Join(got, "|"), "first") || !strings.Contains(strings.Join(got, "|"), "second") {
		t.Fatalf("got %v", got)
	}
	if title := s.headers[1].Get("Title"); !strings.Contains(title+s.headers[0].Get("Title"), "2 messages") {
		t.Fatalf("no digest: %q %q", s.headers[0].Get("Title"), title)
	}
}

func TestQuietUntil(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 10, 23, h, m, 0, 0, time.UTC) }
	q := Quiet{Enabled: true, From: "22:00", To: "07:00"}
	for _, c := range []struct {
		t     time.Time
		quiet bool
		end   time.Time
	}{
		{at(23, 0), true, at(7, 0).AddDate(0, 0, 1)},
		{at(3, 0), true, at(7, 0)},
		{at(7, 0), false, time.Time{}},
		{at(12, 0), false, time.Time{}},
	} {
		end, quiet := quietUntil(q, c.t)
		if quiet != c.quiet || !end.Equal(c.end) {
			t.Errorf("%v: %v %v", c.t, quiet, end)
		}
	}
	if _, quiet := quietUntil(Quiet{Enabled: true, From: "13:00", To: "15:00"}, at(14, 0)); !quiet {
		t.Error("daytime quiet hours")
	}
}

func TestPersonalRejectsBadSettings(t *testing.T) {
	m, _ := newManager(t)
	for _, p := range []Personal{
		{Language: "fr", Content: "summary"},
		{Language: "en", Content: "everything"},
		{Language: "en", Content: "none", Quiet: Quiet{Enabled: true, From: "25:00", To: "07:00"}},
		{Language: "en", Content: "none", Events: map[string]bool{"shell": true}},
		{Language: "en", Content: "none", Email: PersonalEmail{Enabled: true, To: "not an address"}},
		{Language: "en", Content: "none", Origin: "javascript:alert(1)"},
	} {
		if err := m.SetPersonal("alex", p); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v accepted: %v", p, err)
		}
	}
}

func TestPersonalMailUsesTheSharedServerOnly(t *testing.T) {
	m, _ := newManager(t)
	p := defaultPersonal("en")
	p.Email = PersonalEmail{Enabled: true, To: "alex@example.org"}
	if err := m.SetPersonal("alex", p); err != nil {
		t.Fatal(err)
	}
	if m.Reachable("alex", EventBlocked) {
		t.Fatal("reachable by mail without a mail server")
	}
	var sent []string
	m.sendMail = func(cfg Email, message []byte) error {
		sent = append(sent, cfg.To)
		return nil
	}
	m.settings.Email = Email{Host: "mail.example.org", Port: 587, Security: "starttls", From: "nas@example.org", To: "admin@example.org"}
	if err := m.TestPersonal("alex", "email"); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0] != "alex@example.org" {
		t.Fatalf("sent to %v", sent)
	}
}

// A plain user's own channel must not reach into the NAS or the home network,
// nor read back what a server there answered.
func TestConfinedUserCannotReachPrivateAddresses(t *testing.T) {
	m, _ := newManager(t)
	reached := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		http.Error(w, "router admin secret", http.StatusForbidden)
	}))
	defer srv.Close()
	m.Confined = func(user string) bool { return user == "sam" }
	p := defaultPersonal("en")
	p.Ntfy = Ntfy{Enabled: true, URL: srv.URL + "/topic"}
	if err := m.SetPersonal("sam", p); err != nil {
		t.Fatal(err)
	}
	err := m.TestPersonal("sam", "ntfy")
	if err == nil || reached || strings.Contains(err.Error(), "secret") {
		t.Fatalf("confined request: err=%v reached=%v", err, reached)
	}
	if err := m.SetPersonal("alex", p); err != nil {
		t.Fatal(err)
	}
	if err := m.TestPersonal("alex", "ntfy"); err == nil || !reached {
		t.Fatalf("an administrator's own server on the LAN: err=%v reached=%v", err, reached)
	}
	for addr, want := range map[string]bool{"8.8.8.8": true, "10.0.0.1": false, "127.0.0.1": false, "100.100.1.1": false, "169.254.169.254": false, "::1": false, "fd00::1": false, "2001:4860:4860::8888": true} {
		if public(netip.MustParseAddr(addr)) != want {
			t.Errorf("public(%s) != %v", addr, want)
		}
	}
}
