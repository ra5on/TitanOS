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

// ForNames returns the certificate for an app's host name serverName. One
// from Let's Encrypt is used when it covers the name, then the
// administrator's own certificate is used when it covers the name (an
// imported wildcard certificate); else the one TitanOS makes for apps,
// which must name every wildcard in names.
func (m *Manager) ForNames(serverName string, names []string) (*tls.Certificate, error) {
	// One from Let's Encrypt for "*.<domain>" serves the names under it.
	if acme := m.acme.Load(); acme != nil && covers(acme, serverName) {
		return acme, nil
	}
	if current := m.current.Load(); m.kind.Load() == KindCustom && covers(current, serverName) {
		return current, nil
	}
	// Without a real one, the TitanOS authority issues it.
	if leaf, err := m.caLeaf(serverName, names); err == nil {
		return leaf, nil
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

// AddressCertificate is the certificate for the HTTPS ports of apps
// (PLAN 62, https://192.168.10.5:20001): the browser asks for the NAS's
// address or its own name there, not for an app's name. Let's Encrypt,
// Tailscale and an own certificate serve the names they cover (an own one
// also the address it names). Otherwise the TitanOS authority's leaf of
// the web UI serves: it names the NAS's name, its .local name and its
// private addresses (the authority's name constraints permit the private,
// CGNAT, ULA, link-local and loopback ranges, see permittedRanges), also
// when Let's Encrypt or an own certificate is in use for the web UI.
// Without the authority, or for a public address it may not vouch for,
// the certificate in use (self-signed or own) is served, with a warning
// in the browser.
func (m *Manager) AddressCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	serverName := ""
	if hello != nil {
		serverName = hello.ServerName
	}
	if serverName != "" {
		if acme := m.acme.Load(); acme != nil && covers(acme, serverName) {
			return acme, nil
		}
		if tailnet := m.tailnet.Load(); tailnet != nil && covers(tailnet, serverName) {
			return tailnet, nil
		}
	}
	current := m.current.Load()
	if m.kind.Load() == KindCustom {
		if serverName != "" && covers(current, serverName) {
			return current, nil
		}
		if ip := localIP(hello); serverName == "" && ip != nil && covers(current, ip.String()) {
			return current, nil
		}
	}
	if leaf, err := m.uiLeaf(serverName, localIP(hello)); err == nil {
		return leaf, nil
	}
	return current, nil
}
