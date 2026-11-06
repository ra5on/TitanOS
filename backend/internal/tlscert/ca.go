package tlscert

// The certificate authority of TitanOS (PLAN 61-9): app windows are
// frames, and a frame shows no warning one could click away, so a
// self-signed certificate keeps an app from loading. Without a real
// certificate (Let's Encrypt, Tailscale) TitanOS issues the certificates
// of its own names and of the apps' HTTPS names from an authority of its
// own, which one installs once per device.
//
// The authority is made on first need: ECDSA P-256, about ten years. Its
// private key stays in /config/tls (hidden in the stick file manager,
// not in the diagnostic bundle, never sent by the API or MCP). Only its
// certificate can be downloaded. The leaf certificates are short-lived
// and kept in RAM only: a new start makes new ones without writing to the
// stick.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math/big"
	"net"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	caCertFile = "tls/ca-cert.pem"
	caKeyFile  = "tls/ca-key.pem"

	caValidity = 10 * 365 * 24 * time.Hour
	// leafValidity is short: leaves live in RAM and are made anew.
	leafValidity = 30 * 24 * time.Hour
	// leafRenew makes a new leaf when the old one has less left.
	leafRenew = 10 * 24 * time.Hour
	// addressCheck is how often the NAS's addresses are looked at again
	// for the certificate of the web UI.
	addressCheck = time.Minute
)

// KindCA is a certificate issued by the TitanOS authority. Browsers trust
// it once the authority's certificate is installed on the device.
const KindCA = "titanos-ca"

// ErrNoCA means the authority could not be made or read.
var ErrNoCA = errors.New("tlscert: no certificate authority")

type authority struct {
	mu     sync.Mutex
	cert   *x509.Certificate
	key    *ecdsa.PrivateKey
	failed bool
	leaves map[string]*tls.Certificate

	// The web UI's own names and addresses, looked at every minute.
	uiAt    time.Time
	uiIPs   []net.IP
	uiNames []string
}

// caLoaded returns the authority, making it on first need. A stick that
// cannot be read never gets a new one written over the old.
func (m *Manager) caLoaded() (*x509.Certificate, *ecdsa.PrivateKey, error) {
	a := &m.ca
	if a.cert != nil {
		return a.cert, a.key, nil
	}
	if a.failed {
		return nil, nil, ErrNoCA
	}
	certPEM, certErr := m.store.Read(caCertFile)
	keyPEM, keyErr := m.store.Read(caKeyFile)
	if certErr == nil && keyErr == nil {
		cert, key, err := parseCA(certPEM, keyPEM)
		if err != nil {
			a.failed = true
			slog.Error("certificate authority of TitanOS not usable", "err", err)
			return nil, nil, fmt.Errorf("%w: %v", ErrNoCA, err)
		}
		a.cert, a.key = cert, key
		return cert, key, nil
	}
	if !errors.Is(certErr, fs.ErrNotExist) || !errors.Is(keyErr, fs.ErrNotExist) {
		// One of both is there, or the stick cannot be read: keep it.
		a.failed = true
		return nil, nil, fmt.Errorf("%w: %v", ErrNoCA, errors.Join(certErr, keyErr))
	}
	cert, key, certPEM, keyPEM, err := newCA(m.hostname, m.now())
	if err != nil {
		a.failed = true
		return nil, nil, fmt.Errorf("%w: %v", ErrNoCA, err)
	}
	// The key first: a certificate without its key would be useless.
	if err := m.store.Write(caKeyFile, keyPEM); err != nil {
		a.failed = true
		return nil, nil, fmt.Errorf("%w: not saved: %v", ErrNoCA, err)
	}
	if err := m.store.Write(caCertFile, certPEM); err != nil {
		_ = m.store.Remove(caKeyFile) // best effort: the next start tries again from scratch
		a.failed = true
		return nil, nil, fmt.Errorf("%w: not saved: %v", ErrNoCA, err)
	}
	slog.Info("certificate authority of TitanOS created", "name", cert.Subject.CommonName)
	a.cert, a.key = cert, key
	return cert, key, nil
}

func parseCA(certPEM, keyPEM []byte) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, nil, errors.New("no certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, err
	}
	block, _ = pem.Decode(keyPEM)
	if block == nil {
		return nil, nil, errors.New("no key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, nil, err
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || !key.PublicKey.Equal(cert.PublicKey) {
		return nil, nil, errors.New("key does not belong to the certificate")
	}
	if !cert.IsCA {
		return nil, nil, errors.New("not an authority")
	}
	return cert, key, nil
}

func newCA(hostname string, now time.Time) (*x509.Certificate, *ecdsa.PrivateKey, []byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	ski := sha1.Sum(pub)
	name := strings.TrimSpace(hostname)
	if name == "" {
		name = "NAS"
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		// The serial in the name keeps two authorities of the same NAS apart.
		Subject:               pkix.Name{CommonName: "TitanOS " + name + " CA", Organization: []string{"TitanOS"}, SerialNumber: fmt.Sprintf("%032x", serial)},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
		SubjectKeyId:          ski[:],
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return cert, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), nil
}

// CACertificate returns the authority's certificate (DER) for devices to
// install, making the authority on first need. Never its key.
func (m *Manager) CACertificate() ([]byte, error) {
	m.ca.mu.Lock()
	defer m.ca.mu.Unlock()
	cert, _, err := m.caLoaded()
	if err != nil {
		return nil, err
	}
	return slices.Clone(cert.Raw), nil
}

// CAInfo describes the authority, or ok false without one.
func (m *Manager) CAInfo() (Info, bool) {
	m.ca.mu.Lock()
	defer m.ca.mu.Unlock()
	cert, _, err := m.caLoaded()
	if err != nil {
		return Info{}, false
	}
	return Describe(&tls.Certificate{Certificate: [][]byte{cert.Raw}, Leaf: cert}), true
}

// caLeaf returns a certificate from the authority for these names and
// addresses, kept in RAM and made anew before it runs out.
func (m *Manager) caLeaf(names []string, ips []net.IP) (*tls.Certificate, error) {
	a := &m.ca
	a.mu.Lock()
	defer a.mu.Unlock()
	caCert, caKey, err := m.caLoaded()
	if err != nil {
		return nil, err
	}
	key := leafKey(names, ips)
	now := m.now()
	if leaf := a.leaves[key]; leaf != nil && leaf.Leaf.NotAfter.Sub(now) > leafRenew {
		return leaf, nil
	}
	leaf, err := issueLeaf(caCert, caKey, names, ips, now)
	if err != nil {
		return nil, err
	}
	if a.leaves == nil || len(a.leaves) > 32 {
		// Names come and go; old sets are not kept for ever.
		a.leaves = map[string]*tls.Certificate{}
	}
	a.leaves[key] = leaf
	return leaf, nil
}

func leafKey(names []string, ips []net.IP) string {
	parts := slices.Clone(names)
	for _, ip := range ips {
		parts = append(parts, ip.String())
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func issueLeaf(caCert *x509.Certificate, caKey *ecdsa.PrivateKey, names []string, ips []net.IP, now time.Time) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, err
	}
	cn := "TitanOS"
	if len(names) > 0 {
		cn = names[0]
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn, Organization: []string{"TitanOS"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(leafValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              names,
		IPAddresses:           ips,
		AuthorityKeyId:        caCert.SubjectKeyId,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &tls.Certificate{Certificate: [][]byte{der, caCert.Raw}, PrivateKey: key, Leaf: leaf}, nil
}

// uiLeaf is the authority's certificate for the web UI: the NAS's name,
// its .local name, localhost and its addresses.
func (m *Manager) uiLeaf() (*tls.Certificate, error) {
	a := &m.ca
	a.mu.Lock()
	if a.uiNames == nil || m.now().Sub(a.uiAt) > addressCheck {
		name := strings.ToLower(m.hostname)
		names := []string{"localhost"}
		if name != "" {
			names = []string{name, name + ".local", "localhost"}
		}
		ips := append([]net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}, m.addresses()...)
		a.uiNames, a.uiIPs, a.uiAt = names, ips, m.now()
	}
	names, ips := a.uiNames, a.uiIPs
	a.mu.Unlock()
	return m.caLeaf(names, ips)
}

func (m *Manager) addresses() []net.IP {
	if m.localAddresses != nil {
		return m.localAddresses()
	}
	return localAddresses()
}
