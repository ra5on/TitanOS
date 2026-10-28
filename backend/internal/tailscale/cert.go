package tailscale

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// The certificate of the NAS's own name in the tailnet
// (https://<name>.<tailnet>.ts.net). Tailscale gets it from Let's Encrypt
// once "HTTPS Certificates" is on in the admin console; titand serves it
// for that name only (tlscert.Manager.SetTailnet), so phones and browsers
// in the tailnet see a trusted certificate: no warning, and passkeys and
// web push work. Nothing to switch on in TitanOS: turning on HTTPS in the
// tailnet is the owner's choice (it puts the name into the public
// certificate logs). tailscaled keeps and renews the certificate in its
// state on the data volume; titand asks for it at the start and twice a
// day and holds it in RAM only.

// HTTPSStatus tells the settings page about the certificate.
type HTTPSStatus struct {
	// Name is the name of the NAS in the tailnet, if known.
	Name string `json:"name,omitempty"`
	// State: "inactive" (plugin off or not connected), "disabled" (HTTPS
	// is off in the tailnet), "ok" or "error".
	State    string     `json:"state"`
	NotAfter *time.Time `json:"notAfter,omitempty"`
	Error    string     `json:"error,omitempty"`
}

type certState struct {
	name     string
	state    string
	notAfter time.Time
	err      string
}

func (c certState) status() HTTPSStatus {
	s := HTTPSStatus{Name: c.name, State: c.state, Error: c.err}
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
)

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
		return nil, errors.New("Tailscale handed out an unusable certificate")
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
	next := certState{name: name}
	var cert *tls.Certificate
	switch {
	case errors.Is(err, ErrUnavailable):
		next.state = "inactive"
	case errors.Is(err, errNoHTTPS):
		next.state = "disabled"
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
	m.cert = next
	m.mu.Unlock()
	return cert, next.state == "ok"
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
