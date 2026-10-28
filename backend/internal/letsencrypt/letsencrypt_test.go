package letsencrypt

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
)

// fakeDNS stands in for a DNS provider and remembers the TXT records.
type fakeDNS struct {
	mu       sync.Mutex
	multiple bool
	records  map[string][]string
	most     int
	sets     int
	fail     error
}

func (f *fakeDNS) SetTXT(_ context.Context, fqdn string, values []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	if !f.multiple && len(values) > 1 {
		return errors.New("one record only")
	}
	f.records[fqdn] = slices.Clone(values)
	f.most = max(f.most, len(values))
	f.sets++
	return nil
}

func (f *fakeDNS) ClearTXT(_ context.Context, fqdn string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.records, fqdn)
	return nil
}

func (f *fakeDNS) MultipleTXT() bool { return f.multiple }

func (f *fakeDNS) has(fqdn, value string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.records[fqdn], value)
}

type events struct {
	mu   sync.Mutex
	list []string
}

func (e *events) add(s string) {
	e.mu.Lock()
	e.list = append(e.list, s)
	e.mu.Unlock()
}

func (e *events) all() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.list)
}

type harness struct {
	m         *Manager
	ca        *fakeCA
	dns       *fakeDNS
	store     *config.Store
	events    *events
	installed chan *tls.Certificate
}

func newHarness(t *testing.T, multiple bool) *harness {
	h := &harness{ca: newFakeCA(t), dns: &fakeDNS{multiple: multiple, records: map[string][]string{}}, events: &events{}, installed: make(chan *tls.Certificate, 10)}
	h.store = config.Open(t.TempDir())
	h.m = New(h.store)
	h.m.newChallenger = func(dnsprovider.Config) (dnsprovider.Challenger, error) { return h.dns, nil }
	h.m.sleep = func(context.Context, time.Duration) error { return nil }
	// Public resolvers see the records once the fake DNS has them.
	h.m.lookupTXT = func(_ context.Context, _, name string) ([]string, error) {
		h.dns.mu.Lock()
		defer h.dns.mu.Unlock()
		return slices.Clone(h.dns.records[name]), nil
	}
	h.m.AppDomain = func() string { return "home.example.org" }
	h.m.Install = func(c *tls.Certificate) { h.installed <- c }
	h.m.OnEvent = func(_, kind, subject string) { h.events.add(kind + " " + subject) }
	h.m.Activity = func(action, target string, ok bool) { h.events.add(action) }
	h.ca.validate = func(kind, name, token, keyAuth string) bool {
		if kind != "dns-01" {
			return false
		}
		sum := sha256.Sum256([]byte(keyAuth))
		return h.dns.has("_acme-challenge."+name, base64.RawURLEncoding.EncodeToString(sum[:]))
	}
	if err := h.m.Load(); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) settings() Settings {
	return Settings{
		Enabled: true, Email: "admin@example.org", Domains: []string{"nas.home.example.org"}, Apps: true,
		Challenge: ChallengeDNS, Directory: h.ca.url("/dir"),
		Provider: dnsprovider.Config{Type: dnsprovider.Cloudflare, Token: "cf-secret"},
	}
}

func TestIssueWithWildcardForApps(t *testing.T) {
	h := newHarness(t, true)
	s := h.settings()
	s.Domains = []string{"home.example.org"}
	if err := h.m.Set(s, nil); err != nil {
		t.Fatal(err)
	}
	h.m.attempt(context.Background())
	st := h.m.Status()
	if st.LastError != "" || st.Certificate == nil {
		t.Fatalf("status %+v", st)
	}
	want := []string{"home.example.org", "*.home.example.org"}
	if !slices.Equal(st.Names, want) || !slices.Equal(st.Certificate.Names, want) {
		t.Fatalf("names %v, certificate %v", st.Names, st.Certificate.Names)
	}
	// Both values sat on one name at once, and are gone again.
	if h.dns.most != 2 || len(h.dns.records) != 0 {
		t.Fatalf("records %v, most %d", h.dns.records, h.dns.most)
	}
	var cert *tls.Certificate
	for cert == nil || cert.Leaf == nil {
		select {
		case cert = <-h.installed:
		default:
			t.Fatal("nothing installed")
		}
	}
	if cert.Leaf.VerifyHostname("immich.home.example.org") != nil {
		t.Fatal("wildcard missing")
	}
	data, err := h.store.Read(certFile)
	if err != nil || !strings.Contains(string(data), "PRIVATE KEY") {
		t.Fatal("certificate not stored", err)
	}
	if got := h.events.all(); !slices.Contains(got, "letsencrypt_renewed home.example.org") || !slices.Contains(got, "letsencrypt_issued") {
		t.Fatal(got)
	}
	// The secret never comes back through the status.
	if st.Settings.Provider.Token != "" || !st.Secrets["token"] {
		t.Fatal("token in status")
	}

	// A new start reads and serves the stored certificate without asking the CA.
	again := New(h.store)
	var served *tls.Certificate
	again.Install = func(c *tls.Certificate) { served = c }
	if err := again.Load(); err != nil || served == nil {
		t.Fatal("stored certificate not served", err)
	}
	again.AppDomain = h.m.AppDomain
	again.mu.Lock()
	due, _ := again.plan(time.Now(), false)
	again.mu.Unlock()
	if due {
		t.Fatal("a fresh certificate must not be fetched again")
	}
}

func TestOneTXTRecordPerName(t *testing.T) {
	h := newHarness(t, false)
	s := h.settings()
	s.Domains = []string{"nas.duckdns.org", "*.nas.duckdns.org"}
	s.Apps = false
	if err := h.m.Set(s, nil); err != nil {
		t.Fatal(err)
	}
	h.m.attempt(context.Background())
	if st := h.m.Status(); st.LastError != "" || st.Certificate == nil {
		t.Fatalf("%+v", st)
	}
	if h.dns.most != 1 || h.dns.sets != 2 {
		t.Fatalf("most %d, sets %d", h.dns.most, h.dns.sets)
	}
}

func TestHTTPChallenge(t *testing.T) {
	h := newHarness(t, true)
	h.ca.validate = func(kind, name, token, keyAuth string) bool {
		if kind != "http-01" {
			return false
		}
		// The CA would call port 80 of the NAS; here the handler directly.
		rec := httptest.NewRecorder()
		h.m.HTTPHandler(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+name+"/.well-known/acme-challenge/"+token, nil))
		return rec.Code == http.StatusOK && rec.Body.String() == keyAuth
	}
	s := h.settings()
	s.Challenge, s.Apps, s.Provider = ChallengeHTTP, false, dnsprovider.Config{}
	if err := h.m.Set(s, nil); err != nil {
		t.Fatal(err)
	}
	// The fake CA takes its lock while judging; the handler takes the one
	// of the manager, which attempt does not hold meanwhile.
	h.m.attempt(context.Background())
	if st := h.m.Status(); st.LastError != "" || st.Certificate == nil {
		t.Fatalf("%+v", st)
	}
	if len(h.m.http01) != 0 {
		t.Fatal("challenge answers left behind")
	}
	// Other requests on port 80 go on to the redirect.
	rec := httptest.NewRecorder()
	h.m.HTTPHandler(http.RedirectHandler("https://nas/", http.StatusPermanentRedirect)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusPermanentRedirect {
		t.Fatal(rec.Code)
	}
}

func TestFailureBacksOffAndNotifiesOnce(t *testing.T) {
	h := newHarness(t, true)
	h.ca.validate = func(string, string, string, string) bool { return false }
	if err := h.m.Set(h.settings(), nil); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	h.m.now = func() time.Time { return now }
	h.m.attempt(context.Background())
	st := h.m.Status()
	if st.Failures != 1 || st.LastError == "" || st.NextAttempt == nil || !st.NextAttempt.Equal(now.Add(time.Hour)) {
		t.Fatalf("%+v", st)
	}
	if strings.Contains(st.LastError, "cf-secret") {
		t.Fatal("secret in error")
	}
	if len(h.dns.records) != 0 {
		t.Fatal("TXT records left after a failure", h.dns.records)
	}
	h.m.mu.Lock()
	due, next := h.m.plan(now.Add(30*time.Minute), false)
	h.m.mu.Unlock()
	if due || !next.Equal(now.Add(time.Hour)) {
		t.Fatal("must wait for the backoff", due, next)
	}
	h.m.mu.Lock()
	due, _ = h.m.plan(now.Add(30*time.Minute), true)
	h.m.mu.Unlock()
	if !due {
		t.Fatal("renew now ignores the backoff")
	}
	h.m.attempt(context.Background())
	failed := 0
	for _, e := range h.events.all() {
		if strings.HasPrefix(e, "letsencrypt_failed ") {
			failed++
		}
	}
	if failed != 1 || h.m.Status().Failures != 2 {
		t.Fatal("one message for failures in a row", h.events.all())
	}
	if !h.m.Status().NextAttempt.Equal(now.Add(2 * time.Hour)) {
		t.Fatal("second backoff", h.m.Status().NextAttempt)
	}
	// The provider failing is reported without its token.
	h.dns.fail = errors.New("Cloudflare: HTTP 403: Invalid access token")
	h.m.attempt(context.Background())
	if !strings.Contains(h.m.Status().LastError, "Invalid access token") {
		t.Fatal(h.m.Status().LastError)
	}
}

func TestBackoff(t *testing.T) {
	want := []time.Duration{time.Hour, 2 * time.Hour, 4 * time.Hour, 8 * time.Hour, 16 * time.Hour, 24 * time.Hour, 24 * time.Hour}
	for i, w := range want {
		if got := backoff(i + 1); got != w {
			t.Errorf("backoff(%d) = %v, want %v", i+1, got, w)
		}
	}
}

func TestRenewalPlan(t *testing.T) {
	h := newHarness(t, true)
	if err := h.m.Set(h.settings(), nil); err != nil {
		t.Fatal(err)
	}
	h.m.attempt(context.Background())
	leaf := h.m.cert.Leaf
	at := renewAt(leaf)
	if got := leaf.NotAfter.Sub(at); got != 30*24*time.Hour {
		t.Fatal("90-day certificates renew 30 days before the end", got)
	}
	plan := func(now time.Time) (bool, time.Time) {
		h.m.mu.Lock()
		defer h.m.mu.Unlock()
		return h.m.plan(now, false)
	}
	if due, next := plan(time.Now()); due || next.After(time.Now().Add(12*time.Hour+time.Minute)) {
		t.Fatal("fresh certificate", due, next)
	}
	if due, _ := plan(at.Add(time.Minute)); !due {
		t.Fatal("due 30 days before the end")
	}
	// Other names (the domain of the apps changed) need a new certificate.
	h.m.AppDomain = func() string { return "apps.example.net" }
	if due, _ := plan(time.Now()); !due {
		t.Fatal("names changed")
	}
	h.m.AppDomain = func() string { return "home.example.org" }
	// Switching to the staging CA needs a new one as well.
	s := h.settings()
	s.Directory, s.Staging = "", true
	if err := h.m.Set(s, nil); err != nil {
		t.Fatal(err)
	}
	if due, _ := plan(time.Now()); !due {
		t.Fatal("other CA")
	}
	// Short lifetimes renew after two thirds.
	short := &x509.Certificate{NotBefore: time.Unix(0, 0), NotAfter: time.Unix(0, 0).Add(6 * 24 * time.Hour)}
	if got := renewAt(short); !got.Equal(time.Unix(0, 0).Add(4 * 24 * time.Hour)) {
		t.Fatal(got)
	}
	// Switched off: nothing is fetched and the certificate is no longer served.
	s.Enabled = false
	if err := h.m.Set(s, nil); err != nil {
		t.Fatal(err)
	}
	if due, _ := plan(time.Now().Add(365 * 24 * time.Hour)); due {
		t.Fatal("off")
	}
	found := false
	for len(h.installed) > 0 {
		found = <-h.installed == nil
	}
	if !found {
		t.Fatal("switching off must stop serving the certificate")
	}
}

func TestRunAndRenewNow(t *testing.T) {
	h := newHarness(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { h.m.Run(ctx); close(done) }()
	if err := h.m.Renew(); !errors.Is(err, ErrNotEnabled) {
		t.Fatal(err)
	}
	if err := h.m.Set(h.settings(), nil); err != nil {
		t.Fatal(err)
	}
	wait := func() *tls.Certificate {
		t.Helper()
		for {
			select {
			case c := <-h.installed:
				if c != nil && c.Leaf != nil {
					return c
				}
			case <-time.After(10 * time.Second):
				t.Fatal("no certificate")
			}
		}
	}
	first := wait()
	if err := h.m.Renew(); err != nil {
		t.Fatal(err)
	}
	second := wait()
	if first.Leaf.SerialNumber.Cmp(second.Leaf.SerialNumber) == 0 {
		t.Fatal("renew now did not fetch a new certificate")
	}
	cancel()
	<-done
}

func TestExpiryWarning(t *testing.T) {
	h := newHarness(t, true)
	h.ca.lifetime = 10 * 24 * time.Hour
	if err := h.m.Set(h.settings(), nil); err != nil {
		t.Fatal(err)
	}
	h.m.attempt(context.Background())
	h.m.warnExpiry()
	h.m.warnExpiry()
	count := 0
	for _, e := range h.events.all() {
		if strings.HasPrefix(e, "letsencrypt_expiring") {
			count++
		}
	}
	if count != 1 {
		t.Fatal(h.events.all())
	}
}

func TestSettingsValidation(t *testing.T) {
	h := newHarness(t, true)
	base := h.settings()
	cases := map[string]func(*Settings){
		"wildcard over HTTP": func(s *Settings) { s.Challenge, s.Domains, s.Apps = ChallengeHTTP, []string{"*.example.org"}, false },
		"apps over HTTP":     func(s *Settings) { s.Challenge = ChallengeHTTP },
		"local name":         func(s *Settings) { s.Domains = []string{"nas.local"} },
		"no name":            func(s *Settings) { s.Domains = nil },
		"single label":       func(s *Settings) { s.Domains = []string{"nas"} },
		"bad mail":           func(s *Settings) { s.Email = "not mail" },
		"provider missing":   func(s *Settings) { s.Provider = dnsprovider.Config{} },
		"dyndns2 for TXT": func(s *Settings) {
			s.Provider = dnsprovider.Config{Type: dnsprovider.DynDNS2, Server: "https://x/nic/update"}
		},
		"long wait":            func(s *Settings) { s.Propagation = 4000 },
		"ftp directory":        func(s *Settings) { s.Directory = "ftp://ca" },
		"challenge of no kind": func(s *Settings) { s.Challenge = "tls-alpn-01" },
	}
	for name, change := range cases {
		s := base
		change(&s)
		if err := h.m.Set(s, nil); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	s := base
	s.Domains = []string{" NAS.Home.Example.org. ", "nas.home.example.org"}
	if err := h.m.Set(s, nil); err != nil {
		t.Fatal(err)
	}
	if got := h.m.Status().Settings.Domains; !slices.Equal(got, []string{"nas.home.example.org"}) {
		t.Fatal(got)
	}
	// An empty token keeps the stored one for the same provider only.
	s.Provider.Token = ""
	if err := h.m.Set(s, nil); err != nil {
		t.Fatal("token kept", err)
	}
	s.Provider = dnsprovider.Config{Type: dnsprovider.DeSEC}
	if err := h.m.Set(s, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("token must not move to another provider", err)
	}
	var stored Settings
	if err := h.store.ReadJSON(settingsFile, &stored); err != nil || stored.Provider.Token != "cf-secret" {
		t.Fatal("stored token", err)
	}
}
