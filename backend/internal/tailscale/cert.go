package tailscale

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/naming"
)

// The certificate of the NAS's own name in the tailnet
// (https://<name>.<tailnet>.ts.net). Tailscale gets it from Let's Encrypt
// once "HTTPS Certificates" (and MagicDNS) is on in the admin console;
// titand serves it for that name only (tlscert.Manager.SetTailnet), so
// phones and browsers in the tailnet see a trusted certificate: no
// warning, and passkeys and web push work. IP addresses and .local keep
// the NAS's own certificate.
//
// Since 0.41.0 (decided 31.10.2026, PLAN 12b) it is a switch in the
// Tailscale settings, off by default, because getting the certificate puts
// the name into the public Certificate Transparency logs. Settings stored
// by an older version have no switch; there it stays on if tailscaled
// already holds a certificate for the name (an earlier version got one, the
// name is public already), else off. While HTTPS is off in the tailnet, the
// plugin hands out the one-click link of Tailscale to turn it on (LocalAPI
// query-feature, the same link `tailscale serve` shows). tailscaled keeps
// and renews the certificate in its state on the data volume; titand asks
// for it at the start and twice a day and holds it in RAM only, nothing
// lands on the boot stick.

// HTTPSStatus tells the settings page about the certificate.
type HTTPSStatus struct {
	// Name is the name of the NAS in the tailnet, if known.
	Name string `json:"name,omitempty"`
	// State: "off" (switched off in TitanOS), "inactive" (plugin off or not
	// connected), "disabled" (HTTPS is off in the tailnet), "ok" or "error".
	State    string     `json:"state"`
	// Enabled is the switch in TitanOS (Settings.HTTPSCertificate, or what
	// a missing switch means).
	Enabled bool `json:"enabled"`
	NotAfter *time.Time `json:"notAfter,omitempty"`
	Error    string     `json:"error,omitempty"`
	// EnableURL turns HTTPS on in the tailnet with one click at Tailscale
	// (state "disabled" only).
	EnableURL string `json:"enableUrl,omitempty"`
}

type certState struct {
	enabled   bool
	name      string
	state     string
	notAfter  time.Time
	err       string
	enableURL string
	tried     time.Time
	// kick asks RunCertificate to look again now.
	kick chan struct{}
}

func (c certState) status() HTTPSStatus {
	s := HTTPSStatus{Name: c.name, State: c.state, Error: c.err, EnableURL: c.enableURL, Enabled: c.enabled}
	if s.State == "" {
		s.State = "inactive"
	}
	if !c.notAfter.IsZero() {
		t := c.notAfter
		s.NotAfter = &t
	}
	return s
}

var (
	// errNoHTTPS means HTTPS certificates are off in the tailnet.
	errNoHTTPS = errors.New("HTTPS certificates are off in the tailnet")
	certName   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
)

const (
	certEvery = 12 * time.Hour
	// Without a certificate titand looks again soon: the plugin may just
	// have been switched on or HTTPS turned on in the tailnet.
	certRetry = 5 * time.Minute
	// While the settings page is open and HTTPS is still off, titand looks
	// this often, so the certificate follows the click at Tailscale.
	certWatch   = 20 * time.Second
	localSocket = "/run/tailscale/tailscaled.sock"
)

// enableLink asks tailscaled for the link that turns HTTPS on in the
// tailnet. Only a link to the Tailscale login service is passed on.
func (m *Manager) enableLink(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := m.queryFeature(ctx, "serve")
	if err != nil {
		return ""
	}
	var answer struct {
		Complete bool
		URL      string
	}
	if json.Unmarshal(raw, &answer) != nil || answer.Complete {
		return ""
	}
	return loginURL(answer.URL)
}

// localClient reaches the LocalAPI of the plugin's tailscaled; one for all
// calls, so idle connections do not pile up.
var localClient = &http.Client{Transport: &http.Transport{
	DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", localSocket)
	},
	IdleConnTimeout: 30 * time.Second,
}}

// queryFeature is POST /localapi/v0/query-feature on the socket of the
// plugin's tailscaled.
func queryFeature(ctx context.Context, feature string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://local-tailscaled.sock/localapi/v0/query-feature?feature="+url.QueryEscape(feature), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Sec-Tailscale", "localapi")
	resp, err := localClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("tailscaled answered " + resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<10))
}

// certificateName asks tailscaled for the name to get a certificate for:
// the NAS's own name if the tailnet offers certificates for it.
func (m *Manager) certificateName() (string, error) {
	raw, err := m.command("status", "--json")
	if err != nil {
		return "", ErrUnavailable
	}
	var status struct {
		BackendState string
		CertDomains  []string
		Self         struct{ DNSName string }
	}
	if json.Unmarshal([]byte(raw), &status) != nil {
		return "", errors.New("invalid Tailscale status")
	}
	if status.BackendState != "Running" {
		return "", ErrUnavailable
	}
	self := strings.ToLower(strings.TrimSuffix(status.Self.DNSName, "."))
	if len(status.CertDomains) == 0 {
		return self, errNoHTTPS
	}
	name := strings.ToLower(strings.TrimSuffix(status.CertDomains[0], "."))
	if slices.ContainsFunc(status.CertDomains, func(d string) bool { return strings.EqualFold(strings.TrimSuffix(d, "."), self) }) {
		name = self
	}
	if !certName.MatchString(name) {
		return self, errors.New("unexpected name in the Tailscale status")
	}
	return name, nil
}

// fetchCertificate has tailscaled hand out the certificate for name. The
// files land in a private folder in RAM and are removed right after
// reading, so the key only lives in titand.
func (m *Manager) fetchCertificate(ctx context.Context, name string) (*tls.Certificate, error) {
	dir := filepath.Join(m.runtime.Dir(), "cert")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	defer os.Remove(certFile)
	defer os.Remove(keyFile)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if _, err := m.runner.Run(ctx, "", "tailscale", "cert", "--cert-file", certFile, "--key-file", keyFile, name); err != nil {
		return nil, errors.New("tailscale cert failed")
	}
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, errors.New("unusable certificate from Tailscale")
	}
	if cert.Leaf == nil {
		if cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0]); err != nil {
			return nil, err
		}
	}
	if cert.Leaf.VerifyHostname(name) != nil {
		return nil, errors.New("the certificate from Tailscale does not name " + name)
	}
	return &cert, nil
}

// refreshCertificate gets the certificate again and notes the outcome. It
// returns the certificate to serve: nil when there is none, the previous
// one when only this attempt failed and it is still valid.
func (m *Manager) refreshCertificate(ctx context.Context, previous *tls.Certificate) (*tls.Certificate, bool) {
	name, err := m.certificateName()
	enabled := m.httpsWanted(name)
	next := certState{name: name, enabled: enabled}
	var cert *tls.Certificate
	switch {
	case !enabled:
		next.state = "off"
	case errors.Is(err, ErrUnavailable):
		next.state = "inactive"
	case errors.Is(err, errNoHTTPS):
		next.state = "disabled"
		next.enableURL = m.enableLink(ctx)
	case err != nil:
		next.state, next.err = "error", err.Error()
	default:
		cert, err = m.fetchCertificate(ctx, name)
		if err != nil {
			next.state, next.err = "error", err.Error()
			if previous != nil && previous.Leaf != nil && time.Now().Before(previous.Leaf.NotAfter) && previous.Leaf.VerifyHostname(name) == nil {
				cert = previous
				next.notAfter = previous.Leaf.NotAfter
			}
		} else {
			next.state, next.notAfter = "ok", cert.Leaf.NotAfter
		}
	}
	m.mu.Lock()
	next.tried, next.kick = time.Now(), m.cert.kick
	m.cert = next
	m.mu.Unlock()
	return cert, next.state == "ok"
}

// httpsWanted tells whether the certificate is switched on. Without a
// stored switch (settings of an older version) it is on only if tailscaled
// already keeps a certificate for name in its state folder on the volume.
func (m *Manager) httpsWanted(name string) bool {
	m.mu.Lock()
	on, volume := m.settings.HTTPSCertificate, m.volume
	m.mu.Unlock()
	if on != nil {
		return *on
	}
	if name == "" || !certName.MatchString(name) || !naming.ValidVolume(volume) {
		return false
	}
	_, err := os.Stat(filepath.Join(m.mountRoot, volume, ".titanos/tailscale/certs", name+".crt"))
	return err == nil
}

// SetHTTPSCertificate switches the certificate on or off and stores the
// choice at once (no trial: access by IP address never depends on it).
func (m *Manager) SetHTTPSCertificate(on bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Disabled || !m.loaded {
		return ErrUnavailable
	}
	if m.pending != nil {
		return ErrPending
	}
	next := m.settings
	next.Routes = slices.Clone(m.settings.Routes)
	next.HTTPSCertificate = &on
	if err := m.store.WriteJSON(settingsFile, next); err != nil {
		return err
	}
	m.settings = next
	m.cert.enabled = on
	if !on {
		m.cert.state, m.cert.err, m.cert.enableURL, m.cert.notAfter = "off", "", "", time.Time{}
	}
	select {
	case m.cert.kick <- struct{}{}:
	default:
	}
	return nil
}

// RunCertificate keeps use supplied with the certificate of the NAS's own
// name in the tailnet (nil without one) until ctx ends.
func (m *Manager) RunCertificate(ctx context.Context, use func(*tls.Certificate)) {
	if m.Disabled {
		return
	}
	var current *tls.Certificate
	wait := 20 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		case <-m.cert.kick:
		}
		cert, ok := m.refreshCertificate(ctx, current)
		if cert != current {
			use(cert)
			current = cert
		}
		wait = certRetry
		if ok {
			wait = certEvery
		}
	}
}

// watchCertificate is called with mu held whenever the settings page reads
// the status: while HTTPS is still off in the tailnet, look again soon.
func (m *Manager) watchCertificate() {
	if m.cert.state != "disabled" || time.Since(m.cert.tried) < certWatch {
		return
	}
	select {
	case m.cert.kick <- struct{}{}:
	default:
	}
}
