// Package letsencrypt fetches certificates from Let's Encrypt (or another
// CA speaking ACME, RFC 8555) for the web UI and, with a wildcard, for the
// HTTPS names of apps, and renews them by itself.
//
// The DNS challenge is the main way: it needs no open port and is the only
// one that gives wildcards. HTTP-01 is there for those who forward port 80
// to the NAS. The ACME client is golang.org/x/crypto/acme; DNS records are
// set through package dnsprovider.
//
// Settings, the account key and the certificate live in /config/tls/acme/
// (hidden from the file manager and the diagnostics bundle). They are read
// once at start; the renewal is planned from the certificate in RAM, so
// the stick is only touched when something is written.
package letsencrypt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
	"github.com/ra5on/TitanOS/backend/internal/tlscert"
)

const (
	settingsFile = "tls/acme/settings.json"
	accountFile  = "tls/acme/account-key.pem"
	// certFile holds chain, key and a block naming the CA in one file, so
	// that one atomic write replaces all of it.
	certFile = "tls/acme/certificate.pem"

	// Production and Staging are the directories of Let's Encrypt. Staging
	// certificates are not trusted by browsers but have generous limits.
	Production = "https://acme-v02.api.letsencrypt.org/directory"
	Staging    = "https://acme-staging-v02.api.letsencrypt.org/directory"

	ChallengeDNS  = "dns-01"
	ChallengeHTTP = "http-01"

	maxNames = 20
	// expiryWarning is when a certificate that was not renewed is reported.
	expiryWarning = 14 * 24 * time.Hour
	// maxBackoff bounds the wait between failed attempts.
	maxBackoff = 24 * time.Hour
	// metaBlock is the PEM block that remembers the directory.
	metaBlock = "TITANOS ACME"
)

var (
	ErrInvalid    = errors.New("letsencrypt: invalid settings")
	ErrNotLoaded  = errors.New("letsencrypt: settings not loaded from /config")
	ErrNotEnabled = errors.New("letsencrypt: not switched on")
	ErrBusy       = errors.New("letsencrypt: a certificate is being fetched")
)

// Settings say which names to fetch a certificate for and how to prove
// they belong to the NAS.
type Settings struct {
	Enabled bool `json:"enabled"`
	// Email is optional; Let's Encrypt no longer sends expiry mails, but
	// other CAs may.
	Email   string `json:"email,omitempty"`
	Staging bool   `json:"staging,omitempty"`
	// Domains are the names of the web UI; "*.example.org" needs DNS-01.
	Domains []string `json:"domains"`
	// Apps adds "*.<domain of the app HTTPS names>" (DNS-01 only).
	Apps      bool               `json:"apps,omitempty"`
	Challenge string             `json:"challenge"`
	Provider  dnsprovider.Config `json:"provider"`
	// Propagation is how many seconds to wait after setting the TXT
	// records; 0 takes the default of the provider.
	Propagation int `json:"propagation,omitempty"`
	// Directory is another ACME CA instead of Let's Encrypt (step-ca in the
	// home network, …). Empty is Let's Encrypt.
	Directory string `json:"directory,omitempty"`
}

// Status is what the settings page shows.
type Status struct {
	Settings Settings        `json:"settings"`
	Secrets  map[string]bool `json:"secrets"`
	// Names are the names the certificate must cover now.
	Names       []string      `json:"names"`
	AppDomain   string        `json:"appDomain"`
	Certificate *tlscert.Info `json:"certificate,omitempty"`
	// CertificateStaging tells whether it came from the staging CA.
	CertificateStaging bool       `json:"certificateStaging,omitempty"`
	Running            bool       `json:"running"`
	Phase              string     `json:"phase,omitempty"`
	LastAttempt        *time.Time `json:"lastAttempt,omitempty"`
	LastSuccess        *time.Time `json:"lastSuccess,omitempty"`
	LastError          string     `json:"lastError,omitempty"`
	Failures           int        `json:"failures"`
	NextAttempt        *time.Time `json:"nextAttempt,omitempty"`
	RenewAt            *time.Time `json:"renewAt,omitempty"`
	// HTTPPort says whether titand answers on port 80 (needed for HTTP-01).
	HTTPPort bool `json:"httpPort"`
	// Disabled is true in development mode: nothing is fetched.
	Disabled bool `json:"disabled,omitempty"`
}

// Manager fetches and renews the certificate.
type Manager struct {
	store *config.Store
	// Disabled saves settings but fetches nothing (development mode).
	Disabled bool
	// HTTPPort tells whether titand serves port 80 (see HTTPHandler).
	HTTPPort bool
	// AppDomain returns the domain of the app HTTPS names, "" without one.
	AppDomain func() string
	// AppNames returns the full domains of single app routes (such as
	// photos.nas.ipv64.de); with "names for apps" they join the
	// certificate where the DNS challenge can prove them (see names).
	AppNames func() []string
	// Install serves a certificate from now on; nil stops serving one.
	Install func(*tls.Certificate)
	// OnEvent reports to the notifications; Activity to the activity log.
	OnEvent  func(severity, kind, subject string)
	Activity func(action, target string, ok bool)

	// For tests.
	httpClient    *http.Client
	newChallenger func(dnsprovider.Config) (dnsprovider.Challenger, error)
	now           func() time.Time
	sleep         func(context.Context, time.Duration) error
	lookupTXT     func(ctx context.Context, server, name string) ([]string, error)

	mu          sync.Mutex
	loaded      bool
	settings    Settings
	cert        *tls.Certificate
	certDir     string
	running     bool
	phase       string
	lastAttempt time.Time
	lastSuccess time.Time
	lastError   string
	failures    int
	nextAttempt time.Time
	notified    time.Time
	warned      string
	http01      map[string]string
	kick        chan bool
}

// New returns a manager for the configuration in store.
func New(store *config.Store) *Manager {
	return &Manager{
		store: store,
		newChallenger: func(c dnsprovider.Config) (dnsprovider.Challenger, error) {
			return dnsprovider.NewChallenger(c, dnsprovider.Options{})
		},
		now:       time.Now,
		sleep:     sleep,
		lookupTXT: lookupTXT,
		http01:    map[string]string{},
		kick:      make(chan bool, 1),
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Load reads the settings and the certificate and serves it if it is
// still valid.
func (m *Manager) Load() error {
	settings := Settings{Challenge: ChallengeDNS, Domains: []string{}}
	err := m.store.ReadJSON(settingsFile, &settings)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if settings.Domains == nil {
		settings.Domains = []string{}
	}
	var cert *tls.Certificate
	dir := ""
	if data, readErr := m.store.Read(certFile); readErr == nil {
		if parsed, d, parseErr := parseCertificate(data); parseErr == nil && m.now().Before(parsed.Leaf.NotAfter) {
			cert, dir = parsed, d
		} else if parseErr != nil {
			slog.Warn("certificate from Let's Encrypt not usable", "err", parseErr)
		}
	}
	m.mu.Lock()
	m.settings, m.loaded = settings, true
	m.cert, m.certDir = cert, dir
	m.mu.Unlock()
	if settings.Enabled && cert != nil {
		m.install(cert)
	}
	return nil
}

func (m *Manager) install(cert *tls.Certificate) {
	if m.Install != nil {
		m.Install(cert)
	}
}

func (m *Manager) event(severity, kind, subject string) {
	if m.OnEvent != nil {
		m.OnEvent(severity, kind, subject)
	}
}

func (m *Manager) activity(action, target string, ok bool) {
	if m.Activity != nil {
		m.Activity(action, target, ok)
	}
}

// parseCertificate reads the stored file: chain, key and the CA it is from.
func parseCertificate(data []byte) (*tls.Certificate, string, error) {
	cert, err := tls.X509KeyPair(data, data)
	if err != nil {
		return nil, "", err
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, "", err
	}
	cert.Leaf = leaf
	dir := ""
	for rest := data; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == metaBlock {
			dir = block.Headers["Directory"]
		}
	}
	return &cert, dir, nil
}

// directory returns the ACME directory the settings ask for.
func (s Settings) directory() string {
	switch {
	case s.Directory != "":
		return s.Directory
	case s.Staging:
		return Staging
	}
	return Production
}

// normalize cleans the settings up and checks them.
func normalize(s Settings) (Settings, error) {
	s.Email = strings.TrimSpace(s.Email)
	if s.Email != "" {
		addr, err := mail.ParseAddress(s.Email)
		if err != nil || strings.ContainsAny(addr.Address, " ,") || addr.Address != s.Email {
			return s, invalid("e-mail address")
		}
	}
	if s.Challenge == "" {
		s.Challenge = ChallengeDNS
	}
	if s.Challenge != ChallengeDNS && s.Challenge != ChallengeHTTP {
		return s, invalid("challenge")
	}
	seen := map[string]bool{}
	domains := []string{}
	for _, d := range s.Domains {
		d = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(d)), ".")
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		host, wildcard := strings.CutPrefix(d, "*.")
		if !dnsprovider.ValidHost(host) || strings.Contains(host, "*") || strings.HasSuffix(host, ".local") || strings.Contains(host, "_") {
			return s, invalid("name %q", d)
		}
		if wildcard && s.Challenge != ChallengeDNS {
			return s, invalid("wildcard names need the DNS challenge")
		}
		domains = append(domains, d)
	}
	if len(domains) > maxNames {
		return s, invalid("at most %d names", maxNames)
	}
	s.Domains = domains
	if s.Enabled && len(domains) == 0 {
		return s, invalid("no name")
	}
	if s.Apps && s.Challenge != ChallengeDNS {
		return s, invalid("names for apps need the DNS challenge")
	}
	if s.Propagation < 0 || s.Propagation > 3600 {
		return s, invalid("waiting time")
	}
	if s.Directory != "" {
		parsed, err := url.Parse(s.Directory)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || len(s.Directory) > 500 {
			return s, invalid("ACME directory")
		}
	}
	s.Provider = s.Provider.Normalize()
	if s.Challenge == ChallengeDNS || s.Provider.Type != "" {
		if err := s.Provider.Validate(dnsprovider.ForChallenge); err != nil {
			if s.Challenge == ChallengeDNS {
				return s, fmt.Errorf("%w: %v", ErrInvalid, err)
			}
			s.Provider = dnsprovider.Config{}
		}
	}
	return s, nil
}

// Set stores new settings. Secrets left empty keep the stored ones for
// the same provider account; clear names secrets to drop.
func (m *Manager) Set(s Settings, clear []string) error {
	m.mu.Lock()
	if !m.loaded {
		m.mu.Unlock()
		return ErrNotLoaded
	}
	s.Provider = s.Provider.Normalize().KeepSecrets(m.settings.Provider, clear)
	s, err := normalize(s)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	if err := m.store.WriteJSON(settingsFile, s); err != nil {
		m.mu.Unlock()
		return err
	}
	m.settings = s
	// New settings deserve a new try right away.
	m.failures, m.nextAttempt, m.lastError = 0, time.Time{}, ""
	cert := m.cert
	m.mu.Unlock()
	if !s.Enabled {
		m.install(nil)
	} else if cert != nil {
		m.install(cert)
	}
	m.poke(false)
	return nil
}

// Renew fetches a new certificate now, even if the current one is fine.
func (m *Manager) Renew() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.settings.Enabled {
		return ErrNotEnabled
	}
	if m.running {
		return ErrBusy
	}
	m.poke(true)
	return nil
}

func (m *Manager) poke(force bool) {
	select {
	case m.kick <- force:
	default:
		if force {
			// A waiting non-forced kick becomes a forced one.
			select {
			case <-m.kick:
			default:
			}
			select {
			case m.kick <- true:
			default:
			}
		}
	}
}

// names returns the names the certificate must cover.
func (m *Manager) names(s Settings) (names []string, appDomain string) {
	names = append(names, s.Domains...)
	if m.AppDomain != nil {
		appDomain = m.AppDomain()
	}
	if s.Apps && s.Challenge == ChallengeDNS && appDomain != "" && !strings.HasSuffix(appDomain, ".local") && appDomain != "local" {
		if wildcard := "*." + appDomain; !slices.Contains(names, wildcard) {
			names = append(names, wildcard)
		}
	}
	if s.Apps && s.Challenge == ChallengeDNS && m.AppNames != nil {
		for _, name := range m.AppNames() {
			if len(names) >= maxSANs || slices.Contains(names, name) || covered(names, name) || !provable(s, appDomain, name) {
				continue
			}
			names = append(names, name)
		}
	}
	return names, appDomain
}

// maxSANs is the limit of Let's Encrypt for names in one certificate.
const maxSANs = 100

// covered tells whether a wildcard in names already holds name.
func covered(names []string, name string) bool {
	_, parent, ok := strings.Cut(name, ".")
	return ok && slices.Contains(names, "*."+parent)
}

// provable tells whether the DNS provider of the certificate can be
// expected to answer for an app's full domain: always with IPv64 (whose
// domains these are), else only under a name the certificate holds
// anyway. A name it cannot prove would break the whole certificate.
func provable(s Settings, appDomain, name string) bool {
	if s.Provider.Type == dnsprovider.IPv64Net {
		return true
	}
	under := func(base string) bool {
		base = strings.TrimPrefix(base, "*.")
		return base != "" && (name == base || strings.HasSuffix(name, "."+base))
	}
	if under(appDomain) {
		return true
	}
	for _, d := range s.Domains {
		if under(d) {
			return true
		}
	}
	return false
}

// NamesChanged looks again soon whether the certificate still covers
// every name, e.g. after the routes of apps changed.
func (m *Manager) NamesChanged() {
	m.poke(false)
}

// renewAt is when a certificate should be replaced: 30 days before the end,
// or after two thirds of a shorter lifetime.
func renewAt(leaf *x509.Certificate) time.Time {
	before := min(30*24*time.Hour, leaf.NotAfter.Sub(leaf.NotBefore)/3)
	return leaf.NotAfter.Add(-before)
}

// coversExactly tells whether a certificate names exactly these names.
func coversExactly(leaf *x509.Certificate, names []string) bool {
	have := slices.Clone(leaf.DNSNames)
	want := slices.Clone(names)
	slices.Sort(have)
	slices.Sort(want)
	return slices.Equal(have, want)
}

// backoff is the wait after the n-th failure in a row: an hour, doubling,
// at most a day.
func backoff(n int) time.Duration {
	d := time.Hour
	for i := 1; i < n && d < maxBackoff; i++ {
		d *= 2
	}
	return min(d, maxBackoff)
}

// plan says whether a certificate must be fetched now and, if not, when to
// look again. Called with mu held.
func (m *Manager) plan(now time.Time, force bool) (bool, time.Time) {
	s := m.settings
	later := now.Add(12 * time.Hour)
	if !m.loaded || !s.Enabled || m.Disabled {
		return false, later
	}
	names, _ := m.names(s)
	need := m.cert == nil || !coversExactly(m.cert.Leaf, names) || m.certDir != s.directory()
	if !need {
		at := renewAt(m.cert.Leaf)
		if now.Before(at) && !force {
			// Also wake for the expiry warning.
			return false, earliest(later, at)
		}
	}
	if force {
		return true, time.Time{}
	}
	if m.failures > 0 && now.Before(m.nextAttempt) {
		return false, earliest(later, m.nextAttempt)
	}
	return true, time.Time{}
}

func earliest(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// Run renews the certificate when due until ctx ends.
func (m *Manager) Run(ctx context.Context) {
	force := false
	for {
		m.mu.Lock()
		due, next := m.plan(m.now(), force)
		m.mu.Unlock()
		force = false
		m.warnExpiry()
		if due {
			m.attempt(ctx)
			continue
		}
		wait := max(next.Sub(m.now()), time.Second)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case force = <-m.kick:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// warnExpiry reports once per certificate that it runs out soon.
func (m *Manager) warnExpiry() {
	m.mu.Lock()
	cert, enabled := m.cert, m.settings.Enabled
	if cert == nil || !enabled || m.Disabled {
		m.mu.Unlock()
		return
	}
	info := tlscert.Describe(cert)
	if cert.Leaf.NotAfter.Sub(m.now()) > expiryWarning || m.warned == info.Fingerprint {
		m.mu.Unlock()
		return
	}
	m.warned = info.Fingerprint
	m.mu.Unlock()
	m.event("warning", "letsencrypt_expiring", cert.Leaf.NotAfter.Format(time.DateOnly))
}

func (m *Manager) setPhase(phase string) {
	m.mu.Lock()
	m.phase = phase
	m.mu.Unlock()
}

// attempt fetches a certificate once and records how it went.
func (m *Manager) attempt(ctx context.Context) {
	m.mu.Lock()
	s := m.settings
	names, _ := m.names(s)
	m.running, m.phase, m.lastAttempt = true, "account", m.now()
	m.mu.Unlock()

	timeout := 15*time.Minute + time.Duration(s.Propagation)*time.Second + dnsprovider.PropagationDefault(s.Provider.Type)
	issueCtx, cancel := context.WithTimeout(ctx, timeout)
	data, err := m.issue(issueCtx, s, names)
	cancel()
	var cert *tls.Certificate
	if err == nil {
		cert, _, err = parseCertificate(data)
	}
	if err == nil {
		if writeErr := m.store.Write(certFile, data); writeErr != nil {
			// Served anyway; after a restart it is fetched again.
			slog.Warn("certificate from Let's Encrypt not saved to /config", "err", writeErr)
		}
	}
	now := m.now()
	m.mu.Lock()
	m.running, m.phase = false, ""
	if ctx.Err() != nil {
		m.mu.Unlock()
		return
	}
	notify := false
	if err == nil {
		m.cert, m.certDir = cert, s.directory()
		m.failures, m.nextAttempt, m.lastError, m.lastSuccess = 0, time.Time{}, "", now
	} else {
		m.failures++
		m.nextAttempt = now.Add(backoff(m.failures))
		m.lastError = err.Error()
		// The first failure and then at most one message a day.
		if m.failures == 1 || now.Sub(m.notified) >= 24*time.Hour {
			m.notified, notify = now, true
		}
	}
	failures := m.failures
	m.mu.Unlock()

	subject := names[0]
	if err == nil {
		slog.Info("certificate from Let's Encrypt in place", "names", names, "until", cert.Leaf.NotAfter)
		m.install(cert)
		m.event("info", "letsencrypt_renewed", subject)
		m.activity("letsencrypt_issued", strings.Join(names, ", "), true)
		return
	}
	slog.Warn("certificate from Let's Encrypt not fetched", "names", names, "failures", failures, "err", err)
	if notify {
		m.event("warning", "letsencrypt_failed", subject)
	}
	m.activity("letsencrypt_failed", strings.Join(names, ", "), false)
}

// Status tells the settings page what is going on.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.settings
	secrets := s.Provider.Secrets()
	s.Provider = s.Provider.Public()
	names, appDomain := m.names(m.settings)
	st := Status{
		Settings: s, Secrets: secrets, Names: names, AppDomain: appDomain,
		Running: m.running, Phase: m.phase, LastError: m.lastError, Failures: m.failures,
		HTTPPort: m.HTTPPort, Disabled: m.Disabled,
	}
	if st.Names == nil {
		st.Names = []string{}
	}
	stamp := func(t time.Time) *time.Time {
		if t.IsZero() {
			return nil
		}
		return &t
	}
	st.LastAttempt, st.LastSuccess = stamp(m.lastAttempt), stamp(m.lastSuccess)
	if m.failures > 0 {
		st.NextAttempt = stamp(m.nextAttempt)
	}
	if m.cert != nil {
		info := tlscert.Describe(m.cert)
		info.Kind = tlscert.KindLetsEncrypt
		st.Certificate = &info
		st.CertificateStaging = m.certDir == Staging
		st.RenewAt = stamp(renewAt(m.cert.Leaf))
	}
	return st
}

// HTTPHandler answers the HTTP-01 challenge on port 80 and hands every
// other request to next.
func (m *Manager) HTTPHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.URL.Path, "/.well-known/acme-challenge/")
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		m.mu.Lock()
		answer, found := m.http01[token]
		m.mu.Unlock()
		if !found {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(answer))
	})
}

// IPv64Key returns the API key of IPv64 stored for the DNS challenge, if any.
func (m *Manager) IPv64Key() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.settings.Provider.Type != dnsprovider.IPv64Net {
		return ""
	}
	return m.settings.Provider.Token
}
