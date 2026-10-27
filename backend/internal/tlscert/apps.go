package tlscert

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
)

const (
	appsCertFile = "tls/apps-cert.pem"
	appsKeyFile  = "tls/apps-key.pem"
)

// appsCert is the self-signed certificate for the HTTPS names of apps. It
// names only wildcards ("*.nas.local", "*.home.example"), so it stays the
// same when apps come and go and the browser's exception holds.
type appsCert struct {
	mu   sync.Mutex
	cert *tls.Certificate
}

// ForNames returns the certificate for an app's host name serverName. The
// administrator's own certificate is used when it covers the name (an
// imported wildcard certificate); else the one TitanOS makes for apps,
// which must name every wildcard in names.
func (m *Manager) ForNames(serverName string, names []string) (*tls.Certificate, error) {
	if current := m.current.Load(); m.kind.Load() == KindCustom && covers(current, serverName) {
		return current, nil
	}
	a := &m.apps
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cert == nil {
		if cert, err := m.loadApps(); err == nil {
			a.cert = &cert
		}
	}
	if a.cert != nil && coversAll(a.cert, names) {
		return a.cert, nil
	}
	// The set of names changed (hostname or domain): a new certificate.
	certPEM, keyPEM, err := generateFor(names[0], names, nil, m.now())
	if err != nil {
		return nil, err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	// Kept in RAM even if the stick cannot take it: the names work until
	// the next start, with a new browser exception then.
	a.cert = &cert
	err = m.store.Write(appsKeyFile, keyPEM)
	if err == nil {
		err = m.store.Write(appsCertFile, certPEM)
	}
	if err != nil {
		slog.Warn("certificate for apps not saved", "err", err)
	}
	return a.cert, nil
}

func (m *Manager) loadApps() (tls.Certificate, error) {
	certPEM, err := m.store.Read(appsCertFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM, err := m.store.Read(appsKeyFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, err
	}
	if time.Until(cert.Leaf.NotAfter) < renewBefore {
		return tls.Certificate{}, fmt.Errorf("certificate for apps expires soon")
	}
	return cert, nil
}

// covers tells whether a certificate is valid for a host name now.
func covers(cert *tls.Certificate, name string) bool {
	if cert == nil || len(cert.Certificate) == 0 {
		return false
	}
	leaf := cert.Leaf
	if leaf == nil {
		parsed, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return false
		}
		leaf = parsed
	}
	return leaf.VerifyHostname(name) == nil && time.Now().Before(leaf.NotAfter)
}

// coversAll tells whether a certificate names all these wildcards and is
// not about to expire.
func coversAll(cert *tls.Certificate, names []string) bool {
	leaf := cert.Leaf
	if leaf == nil {
		return false
	}
	for _, name := range names {
		if !slices.Contains(leaf.DNSNames, name) {
			return false
		}
	}
	return time.Until(leaf.NotAfter) > renewBefore
}
