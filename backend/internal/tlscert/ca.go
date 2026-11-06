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
// certificate can be downloaded. Two leaf certificates (web UI, apps) are
// kept in RAM and stored only when issued, so that they stay the same
// across restarts; a new one comes only when names change or it nears its
// end.

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
	"strings"
	"sync"
	"time"
)

const (
	caCertFile = "tls/ca-cert.pem"
	caKeyFile  = "tls/ca-key.pem"

	caValidity     = 10 * 365 * 24 * time.Hour
	caLeafUIFile   = "tls/ca-leaf-ui.pem"
	caLeafAppsFile = "tls/ca-leaf-apps.pem"

	// leafValidity stays under the 398 days browsers accept.
	leafValidity = 397 * 24 * time.Hour
	// leafRenew makes a new leaf when the old one has less left.
	leafRenew = 30 * 24 * time.Hour
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
	ui     leafSlot
	apps   leafSlot
	// uiRAM is the web UI's leaf for an address of the NAS the stored one
	// does not name (a new IPv6 prefix, DHCP): kept in RAM only, so that
	// a changing address never writes to the stick.
	uiRAM *tls.Certificate
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
		if constrained(cert) {
			a.cert, a.key = cert, key
			return cert, key, nil
		}
		// An authority from a development build before name constraints:
		// it could vouch for any name (a bank's), so it is replaced once.
		return m.replaceCA(keyPEM)
	}
	if !errors.Is(certErr, fs.ErrNotExist) || !errors.Is(keyErr, fs.ErrNotExist) {
		// One of both is there, or the stick cannot be read: keep it.
		a.failed = true
		return nil, nil, fmt.Errorf("%w: %v", ErrNoCA, errors.Join(certErr, keyErr))
	}
	cert, key, certPEM, keyPEM, err := newCA(m.hostname, m.permittedNames(), m.now())
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

// replaceCA makes a new authority in place of one without name
// constraints. The old key goes back if the new certificate cannot be
// written, so that the stick never holds a key without its certificate.
// The lock is held.
func (m *Manager) replaceCA(oldKey []byte) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	a := &m.ca
	cert, key, certPEM, keyPEM, err := newCA(m.hostname, m.permittedNames(), m.now())
	if err != nil {
		a.failed = true
		return nil, nil, fmt.Errorf("%w: %v", ErrNoCA, err)
	}
	if err := m.store.Write(caKeyFile, keyPEM); err != nil {
		a.failed = true
		return nil, nil, fmt.Errorf("%w: not replaced: %v", ErrNoCA, err)
	}
	if err := m.store.Write(caCertFile, certPEM); err != nil {
		_ = m.store.Write(caKeyFile, oldKey) // back to the old pair; the first error is returned
		a.failed = true
		return nil, nil, fmt.Errorf("%w: not replaced: %v", ErrNoCA, err)
	}
	slog.Warn("certificate authority of TitanOS replaced by one with name constraints; devices must install it again", "name", cert.Subject.CommonName)
	a.cert, a.key = cert, key
	return cert, key, nil
}

// constrained tells whether an authority limits the names it may vouch
// for (critical name constraints with permitted names and addresses).
func constrained(cert *x509.Certificate) bool {
	return cert.PermittedDNSDomainsCritical && len(cert.PermittedDNSDomains) > 0 && len(cert.PermittedIPRanges) > 0
}

// permittedRanges are the addresses the authority may vouch for: private,
// shared (CGNAT, Tailscale), loopback and link-local networks. A public
// address of the NAS keeps the self-signed certificate.
var permittedRanges = []string{
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
	"fc00::/7", "fe80::/10", "::1/128",
}

// permittedNames are the DNS names the authority may vouch for, fixed
// when it is made: the NAS's name, everything under .local (the apps'
// names such as immich.nas.local), localhost and the domains of the app
// proxy configured at that time. A permitted name covers itself and all
// names below it. Names added later get no certificate from it.
func (m *Manager) permittedNames() []string {
	out := []string{"local", "localhost"}
	add := func(name string) {
		name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
		name = strings.TrimPrefix(name, "*.")
		if name == "" || strings.ContainsAny(name, "*/: ") || net.ParseIP(name) != nil || slices.Contains(out, name) {
			return
		}
		// Covered by "local" already.
		if strings.HasSuffix(name, ".local") {
			return
		}
		out = append(out, name)
	}
	add(m.hostname)
	if m.CADomains != nil {
		for _, domain := range m.CADomains() {
			add(domain)
		}
	}
	return out
}

// permitsName tells whether the authority's constraints allow a DNS name
// (Go's and the browsers' rule: a permitted "local" covers "local" and
// every name ending in ".local").
func permitsName(ca *x509.Certificate, name string) bool {
	if len(ca.PermittedDNSDomains) == 0 {
		return len(ca.ExcludedDNSDomains) == 0
	}
	name = strings.TrimSuffix(strings.ToLower(name), ".")
	if name == "" {
		return false
	}
	for _, c := range ca.PermittedDNSDomains {
		c = strings.ToLower(c)
		if dot, ok := strings.CutPrefix(c, "."); ok {
			if strings.HasSuffix(name, c) && name != dot {
				return true
			}
			continue
		}
		if name == c || strings.HasSuffix(name, "."+c) {
			return true
		}
	}
	return false
}

// permitsIP tells whether the authority's constraints allow an address.
func permitsIP(ca *x509.Certificate, ip net.IP) bool {
	if ip == nil {
		return false
	}
	if len(ca.PermittedIPRanges) == 0 {
		return len(ca.ExcludedIPRanges) == 0
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, r := range ca.PermittedIPRanges {
		if len(r.IP) == len(ip) && r.Contains(ip) {
			return true
		}
	}
	return false
}

func permittedOnly(ca *x509.Certificate, names []string) []string {
	out := []string{}
	for _, name := range names {
		if permitsName(ca, name) && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

func permittedIPs(ca *x509.Certificate, ips []net.IP) []net.IP {
	var out []net.IP
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		if permitsIP(ca, ip) && !slices.ContainsFunc(out, ip.Equal) {
			out = append(out, ip)
		}
	}
	return out
}

// errNotPermitted means a name or address lies outside the authority's
// constraints; the caller falls back to the self-signed certificate.
var errNotPermitted = errors.New("tlscert: outside the constraints of the TitanOS authority")

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

func newCA(hostname string, permitted []string, now time.Time) (*x509.Certificate, *ecdsa.PrivateKey, []byte, []byte, error) {
	var ranges []*net.IPNet
	for _, cidr := range permittedRanges {
		_, r, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		ranges = append(ranges, r)
	}
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
		// Installed on a device, an authority without limits could vouch
		// for any site; this one only for the NAS's own names and private
		// addresses.
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         permitted,
		PermittedIPRanges:           ranges,
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

// leafSlot is one certificate issued by the authority: kept in RAM and
// on the stick, so that it stays the same across restarts (a browser
// exception for it, made before the authority was installed, then holds).
// It is written only when it is issued: when its names change or it is
// about to run out.
type leafSlot struct {
	file   string
	loaded bool
	cert   *tls.Certificate
}

// leaf returns the slot's certificate if fits accepts it and it is fresh,
// else issues one for names and ips and stores it.
func (m *Manager) leaf(slot *leafSlot, fits func(*x509.Certificate) bool, issue func() ([]string, []net.IP)) (*tls.Certificate, error) {
	caCert, caKey, err := m.caLoaded()
	if err != nil {
		return nil, err
	}
	now := m.now()
	if !slot.loaded {
		slot.loaded = true
		if data, err := m.store.Read(slot.file); err == nil {
			if cert, err := tls.X509KeyPair(data, data); err == nil && cert.Leaf != nil && cert.Leaf.CheckSignatureFrom(caCert) == nil {
				cert.Certificate = append(cert.Certificate[:1:1], caCert.Raw)
				slot.cert = &cert
			}
		}
	}
	if c := slot.cert; c != nil && fits(c.Leaf) && c.Leaf.NotAfter.Sub(now) > leafRenew && !now.Before(c.Leaf.NotBefore) {
		return c, nil
	}
	names, ips := issue()
	cert, certPEM, keyPEM, err := issueLeaf(caCert, caKey, names, ips, now)
	if err != nil {
		return nil, err
	}
	slot.cert = cert
	if err := m.store.Write(slot.file, append(certPEM, keyPEM...)); err != nil {
		// It serves from RAM until the next start.
		slog.Warn("certificate from the TitanOS authority not saved", "file", slot.file, "err", err)
	}
	return cert, nil
}

// caLeaf is the authority's certificate for the HTTPS names of apps; it
// names every one of names the authority may vouch for (wildcards, short
// names, domains). A server name outside its constraints (a domain added
// after the authority was made) gets errNotPermitted.
func (m *Manager) caLeaf(serverName string, names []string) (*tls.Certificate, error) {
	a := &m.ca
	a.mu.Lock()
	defer a.mu.Unlock()
	caCert, _, err := m.caLoaded()
	if err != nil {
		return nil, err
	}
	if serverName != "" && !permitsName(caCert, serverName) {
		return nil, errNotPermitted
	}
	names = permittedOnly(caCert, names)
	if len(names) == 0 {
		return nil, errNotPermitted
	}
	cert, err := m.leaf(&a.apps, func(leaf *x509.Certificate) bool {
		for _, name := range names {
			if !slices.Contains(leaf.DNSNames, name) {
				return false
			}
		}
		return true
	}, func() ([]string, []net.IP) { return slices.Clone(names), nil })
	if err != nil {
		return nil, err
	}
	if serverName != "" && cert.Leaf.VerifyHostname(serverName) != nil {
		return nil, errNotPermitted
	}
	return cert, nil
}

func issueLeaf(caCert *x509.Certificate, caKey *ecdsa.PrivateKey, names []string, ips []net.IP, now time.Time) (*tls.Certificate, []byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, nil, err
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
		return nil, nil, nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return &tls.Certificate{Certificate: [][]byte{der, caCert.Raw}, PrivateKey: key, Leaf: leaf}, certPEM, keyPEM, nil
}

// uiLeaf is the authority's certificate for the web UI: the NAS's name,
// its .local name, localhost and its addresses, as far as the authority
// may vouch for them. serverName is the name the client asked for (SNI),
// local the NAS's address it connected to when it asked for none (an
// address typed into the browser).
//
// The stored leaf is written to the stick only when it is first issued,
// when the NAS's names change and before it runs out. An address it does
// not name gets a leaf kept in RAM only, so that changing addresses (IPv6
// prefixes, DHCP) never write to the stick. A name or address outside the
// authority's constraints (a public address) gets errNotPermitted: the
// self-signed certificate serves it.
func (m *Manager) uiLeaf(serverName string, local net.IP) (*tls.Certificate, error) {
	a := &m.ca
	a.mu.Lock()
	defer a.mu.Unlock()
	caCert, caKey, err := m.caLoaded()
	if err != nil {
		return nil, err
	}
	if serverName != "" {
		if !permitsName(caCert, serverName) {
			return nil, errNotPermitted
		}
		local = nil // browsers send no name for an address
	}
	if local != nil && !permitsIP(caCert, local) {
		return nil, errNotPermitted
	}
	name := strings.ToLower(m.hostname)
	names := []string{"localhost"}
	if name != "" {
		names = []string{name, name + ".local", "localhost"}
	}
	names = permittedOnly(caCert, names)
	addresses := func() []net.IP {
		return permittedIPs(caCert, append([]net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}, m.addresses()...))
	}
	namesAll := func(leaf *x509.Certificate) bool {
		for _, n := range names {
			if !slices.Contains(leaf.DNSNames, n) {
				return false
			}
		}
		return true
	}
	stored, err := m.leaf(&a.ui, namesAll, func() ([]string, []net.IP) { return names, addresses() })
	if err != nil {
		return nil, err
	}
	if local == nil || slices.ContainsFunc(stored.Leaf.IPAddresses, local.Equal) {
		return stored, nil
	}
	now := m.now()
	if c := a.uiRAM; c != nil && namesAll(c.Leaf) && slices.ContainsFunc(c.Leaf.IPAddresses, local.Equal) && c.Leaf.NotAfter.Sub(now) > leafRenew && !now.Before(c.Leaf.NotBefore) && c.Leaf.CheckSignatureFrom(caCert) == nil {
		return c, nil
	}
	ips := addresses()
	if !slices.ContainsFunc(ips, local.Equal) {
		ips = append(ips, local)
	}
	cert, _, _, err := issueLeaf(caCert, caKey, names, ips, now)
	if err != nil {
		return nil, err
	}
	a.uiRAM = cert
	return cert, nil
}

// localIP is the NAS's address a TLS client connected to, or nil.
func localIP(hello *tls.ClientHelloInfo) net.IP {
	if hello == nil || hello.Conn == nil {
		return nil
	}
	if addr, ok := hello.Conn.LocalAddr().(*net.TCPAddr); ok {
		return addr.IP
	}
	return nil
}

func (m *Manager) addresses() []net.IP {
	if m.localAddresses != nil {
		return m.localAddresses()
	}
	return localAddresses()
}
