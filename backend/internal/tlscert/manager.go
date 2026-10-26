package tlscert

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const (
	customCertFile = "tls/custom-cert.pem"
	customKeyFile  = "tls/custom-key.pem"
	// maxPEM is more than any certificate chain or key needs.
	maxPEM = 64 << 10
)

// Kinds of certificate the web UI can show.
const (
	KindSelfSigned = "self-signed" // made by TitanOS; browsers warn
	KindCustom     = "custom"      // put in by an administrator
)

// ErrInvalid means a certificate or key is not usable; the message says why.
var ErrInvalid = errors.New("tlscert: certificate not usable")

// Info describes the certificate in use.
type Info struct {
	Kind string `json:"kind"`
	// Names are the host names and addresses it is valid for.
	Names     []string  `json:"names"`
	Issuer    string    `json:"issuer"`
	NotBefore time.Time `json:"notBefore"`
	NotAfter  time.Time `json:"notAfter"`
	// Fingerprint is the SHA-256 of the certificate, as browsers show it.
	Fingerprint string `json:"fingerprint"`
}

// Manager holds the certificate of the web UI and swaps it while titand runs.
type Manager struct {
	store    *config.Store
	hostname string
	now      func() time.Time

	mu      sync.Mutex
	current atomic.Pointer[tls.Certificate]
	kind    atomic.Value // string
}

// NewManager loads the certificate to use: the administrator's own if
// there is one that is still valid, else the one TitanOS makes itself.
// Like LoadOrCreate it returns a usable manager together with an error if
// a new certificate could not be stored.
func NewManager(store *config.Store, hostname string) (*Manager, error) {
	m := &Manager{store: store, hostname: hostname, now: time.Now}
	if cert, err := m.loadCustom(); err == nil {
		m.current.Store(&cert)
		m.kind.Store(KindCustom)
		return m, nil
	}
	cert, err := LoadOrCreate(store, hostname)
	if len(cert.Certificate) == 0 {
		return nil, err
	}
	m.current.Store(&cert)
	m.kind.Store(KindSelfSigned)
	return m, err
}

// GetCertificate is what the TLS server asks for every connection.
func (m *Manager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return m.current.Load(), nil
}

func (m *Manager) loadCustom() (tls.Certificate, error) {
	certPEM, err := m.store.Read(customCertFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM, err := m.store.Read(customKeyFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	return m.check(certPEM, keyPEM)
}

// check tells whether a certificate and key can serve the web UI now.
func (m *Manager) check(certPEM, keyPEM []byte) (tls.Certificate, error) {
	if len(certPEM) > maxPEM || len(keyPEM) > maxPEM {
		return tls.Certificate{}, fmt.Errorf("%w: too large", ErrInvalid)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		// The text of the error says what is wrong (no certificate found,
		// key does not match …) and holds nothing of the key.
		return tls.Certificate{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	now := m.now()
	switch {
	case now.After(leaf.NotAfter):
		return tls.Certificate{}, fmt.Errorf("%w: it expired on %s", ErrInvalid, leaf.NotAfter.Format(time.DateOnly))
	case now.Before(leaf.NotBefore):
		return tls.Certificate{}, fmt.Errorf("%w: it is not valid before %s", ErrInvalid, leaf.NotBefore.Format(time.DateOnly))
	case len(leaf.DNSNames) == 0 && len(leaf.IPAddresses) == 0:
		// Browsers look only at these names, not at the common name.
		return tls.Certificate{}, fmt.Errorf("%w: it names no host (subject alternative names)", ErrInvalid)
	case leaf.IsCA:
		return tls.Certificate{}, fmt.Errorf("%w: this is the certificate of an authority, not of a server", ErrInvalid)
	}
	cert.Leaf = leaf
	return cert, nil
}

// SetCustom takes a certificate (with the chain up to the authority, PEM)
// and its private key, stores them and serves them from now on.
func (m *Manager) SetCustom(certPEM, keyPEM []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cert, err := m.check(certPEM, keyPEM)
	if err != nil {
		return err
	}
	// Both files belong together: if the second cannot be written, the
	// first goes back to what it was, so that the next start does not find
	// a key without its certificate.
	oldKey, keyErr := m.store.Read(customKeyFile)
	if err := m.store.Write(customKeyFile, keyPEM); err != nil {
		return err
	}
	if err := m.store.Write(customCertFile, certPEM); err != nil {
		if keyErr == nil {
			m.store.Write(customKeyFile, oldKey)
		} else {
			m.store.Remove(customKeyFile)
		}
		return err
	}
	m.current.Store(&cert)
	m.kind.Store(KindCustom)
	return nil
}

// RemoveCustom goes back to the certificate TitanOS makes itself.
func (m *Manager) RemoveCustom() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.store.Remove(customCertFile); err != nil {
		return err
	}
	if err := m.store.Remove(customKeyFile); err != nil {
		return err
	}
	cert, err := LoadOrCreate(m.store, m.hostname)
	if len(cert.Certificate) == 0 {
		return err
	}
	m.current.Store(&cert)
	m.kind.Store(KindSelfSigned)
	return nil
}

// Info describes the certificate in use.
func (m *Manager) Info() Info {
	cert := m.current.Load()
	info := Info{Kind: m.kind.Load().(string), Names: []string{}}
	leaf := cert.Leaf
	if leaf == nil {
		parsed, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return info
		}
		leaf = parsed
	}
	info.Names = append(info.Names, leaf.DNSNames...)
	for _, ip := range leaf.IPAddresses {
		info.Names = append(info.Names, ip.String())
	}
	info.Issuer = leaf.Issuer.CommonName
	if info.Issuer == "" {
		info.Issuer = strings.Join(leaf.Issuer.Organization, ", ")
	}
	info.NotBefore, info.NotAfter = leaf.NotBefore, leaf.NotAfter
	sum := sha256.Sum256(leaf.Raw)
	info.Fingerprint = strings.ToUpper(hex.EncodeToString(sum[:]))
	return info
}
