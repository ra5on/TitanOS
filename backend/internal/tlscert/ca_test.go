package tlscert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func caManager(t *testing.T) (*Manager, *config.Store) {
	t.Helper()
	store := config.Open(t.TempDir())
	m, err := NewManager(store, "nas")
	if err != nil {
		t.Fatal(err)
	}
	m.localAddresses = func() []net.IP { return []net.IP{net.ParseIP("192.168.1.20")} }
	return m, store
}

func verifyWith(t *testing.T, caDER []byte, cert *tls.Certificate, name string) error {
	t.Helper()
	root, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(root)
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	_, err = leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: name, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	return err
}

func TestAuthority(t *testing.T) {
	m, store := caManager(t)
	caDER, err := m.CACertificate()
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(caDER)
	if !ca.IsCA || ca.Subject.CommonName != "TitanOS nas CA" || ca.KeyUsage&x509.KeyUsageCertSign == 0 || len(ca.SubjectKeyId) == 0 {
		t.Fatalf("authority: %+v", ca.Subject)
	}
	if years := ca.NotAfter.Sub(ca.NotBefore).Hours() / 24 / 365; years < 9.9 {
		t.Errorf("authority lasts %.1f years", years)
	}
	if pub, ok := ca.PublicKey.(*ecdsa.PublicKey); !ok || pub.Curve != elliptic.P256() {
		t.Error("not ECDSA P-256")
	}

	// The web UI gets a leaf from it, for its names and addresses.
	ui, err := m.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nas", "nas.local", "192.168.1.20"} {
		if err := verifyWith(t, caDER, ui, name); err != nil {
			t.Errorf("web UI leaf for %s: %v", name, err)
		}
	}
	if left := time.Until(ui.Leaf.NotAfter); left > 398*24*time.Hour {
		t.Errorf("leaf lasts longer than browsers accept: %v", left)
	}
	if info := m.Info(); info.Kind != KindCA || info.Issuer != "TitanOS nas CA" {
		t.Errorf("info: %+v", info)
	}

	// Apps get one for their wildcards; the same leaf while it is fresh.
	app, err := m.ForNames("immich.nas.local", []string{"*.nas.local", "immich.local"})
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyWith(t, caDER, app, "immich.nas.local"); err != nil {
		t.Error(err)
	}
	if err := verifyWith(t, caDER, app, "immich.local"); err != nil {
		t.Error(err)
	}
	again, _ := m.ForNames("jellyfin.nas.local", []string{"*.nas.local", "immich.local"})
	if again != app {
		t.Error("a new leaf for the same names")
	}
	// No self-signed apps certificate any more.
	if _, err := store.Read(appsCertFile); err == nil {
		t.Error("self-signed apps certificate made")
	}

	// The same authority and the same leaves after a restart, so that a
	// browser exception made before installing the authority holds.
	changes := store.Writes()
	restarted, _ := NewManager(store, "nas")
	restarted.localAddresses = m.localAddresses
	der2, err := restarted.CACertificate()
	if err != nil || string(der2) != string(caDER) {
		t.Error("authority not kept across restarts")
	}
	appAgain, _ := restarted.ForNames("immich.nas.local", []string{"*.nas.local", "immich.local"})
	if string(appAgain.Certificate[0]) != string(app.Certificate[0]) {
		t.Error("new apps leaf after a restart")
	}
	uiAgain, _ := restarted.GetCertificate(&tls.ClientHelloInfo{})
	if string(uiAgain.Certificate[0]) != string(ui.Certificate[0]) || len(uiAgain.Certificate) != 2 {
		t.Error("new web UI leaf after a restart")
	}
	if store.Writes() != changes {
		t.Error("a restart wrote to the stick")
	}
	// A new address of the NAS: a leaf that names it, kept in RAM only.
	restarted.localAddresses = func() []net.IP { return []net.IP{net.ParseIP("192.168.1.20"), net.ParseIP("10.0.0.5")} }
	if leaf, _ := restarted.uiLeaf("", net.ParseIP("192.168.1.20")); string(leaf.Certificate[0]) != string(ui.Certificate[0]) {
		t.Error("new leaf for a known address")
	}
	leaf, _ := restarted.uiLeaf("", net.ParseIP("10.0.0.5"))
	if err := verifyWith(t, caDER, leaf, "nas.local"); err != nil || !slices.ContainsFunc(leaf.Leaf.IPAddresses, net.ParseIP("10.0.0.5").Equal) {
		t.Error("no leaf for a new address")
	}
	if store.Writes() != changes {
		t.Error("a new address wrote to the stick")
	}
}

func TestAuthorityNameConstraints(t *testing.T) {
	m, _ := caManager(t)
	m.CADomains = func() []string { return []string{"home.example.org", "photos.example.net"} }
	caDER, err := m.CACertificate()
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(caDER)
	if !ca.PermittedDNSDomainsCritical || !constrained(ca) {
		t.Fatal("authority without critical name constraints")
	}
	for _, want := range []string{"nas", "local", "localhost", "home.example.org", "photos.example.net"} {
		if !slices.Contains(ca.PermittedDNSDomains, want) {
			t.Errorf("%q not permitted: %v", want, ca.PermittedDNSDomains)
		}
	}
	// A leaf for a foreign name, signed with the authority's key behind the
	// back of the filter, is refused by every client that checks.
	bank, _, _, err := issueLeaf(m.ca.cert, m.ca.key, []string{"bank.de"}, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var invalid x509.CertificateInvalidError
	if err := verifyWith(t, caDER, bank, "bank.de"); !errors.As(err, &invalid) || invalid.Reason != x509.CANotAuthorizedForThisName {
		t.Errorf("leaf for bank.de: %v", err)
	}
	public, _, _, _ := issueLeaf(m.ca.cert, m.ca.key, []string{"nas.local"}, []net.IP{net.ParseIP("8.8.8.8")}, time.Now())
	if err := verifyWith(t, caDER, public, "nas.local"); err == nil {
		t.Error("leaf for a public address verified")
	}

	// The leaves it issues stay inside: web UI and apps verify.
	m.localAddresses = func() []net.IP {
		return []net.IP{net.ParseIP("192.168.1.20"), net.ParseIP("2001:db8::20"), net.ParseIP("fd00::20"), net.ParseIP("203.0.113.9")}
	}
	ui, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: "nas.local"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nas", "nas.local", "localhost", "192.168.1.20", "fd00::20", "127.0.0.1", "::1"} {
		if err := verifyWith(t, caDER, ui, name); err != nil {
			t.Errorf("web UI leaf for %s: %v", name, err)
		}
	}
	if slices.ContainsFunc(ui.Leaf.IPAddresses, net.ParseIP("2001:db8::20").Equal) || slices.ContainsFunc(ui.Leaf.IPAddresses, net.ParseIP("203.0.113.9").Equal) {
		t.Error("public addresses in the web UI leaf")
	}
	for _, c := range []struct{ server, names []string }{
		{[]string{"immich.nas.local", "immich.local"}, []string{"*.nas.local", "immich.local"}},
		{[]string{"immich.home.example.org"}, []string{"*.nas.local", "*.home.example.org", "immich.local"}},
		{[]string{"photos.example.net"}, []string{"*.nas.local", "photos.example.net"}},
	} {
		for _, server := range c.server {
			app, err := m.ForNames(server, c.names)
			if err != nil {
				t.Fatal(err)
			}
			if app.Leaf.Issuer.CommonName != "TitanOS nas CA" {
				t.Errorf("%s not served by the authority", server)
			}
			if err := verifyWith(t, caDER, app, server); err != nil {
				t.Errorf("app leaf for %s: %v", server, err)
			}
		}
	}
	// A domain added after the authority was made: the self-signed
	// certificate for apps, never a leaf the constraints refuse.
	names := []string{"*.nas.local", "*.later.example"}
	app, err := m.ForNames("immich.later.example", names)
	if err != nil {
		t.Fatal(err)
	}
	if app.Leaf.Issuer.CommonName == "TitanOS nas CA" {
		t.Error("authority leaf for a name outside its constraints")
	}
	if ca, _ := m.ForNames("immich.nas.local", names); ca.Leaf.Issuer.CommonName != "TitanOS nas CA" || slices.Contains(ca.Leaf.DNSNames, "*.later.example") {
		t.Error("permitted names lost the authority leaf")
	}
	// A public address typed into the browser: the self-signed one.
	if _, err := m.uiLeaf("", net.ParseIP("203.0.113.9")); !errors.Is(err, errNotPermitted) {
		t.Errorf("public address: %v", err)
	}
	if _, err := m.uiLeaf("nas.example.com", nil); !errors.Is(err, errNotPermitted) {
		t.Errorf("foreign name: %v", err)
	}
}

func TestAuthorityWithoutConstraintsReplacedOnce(t *testing.T) {
	m, store := caManager(t)
	// An authority from a development build, without name constraints.
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "TitanOS nas CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour * 24 * 365),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	if err := store.Write(caCertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(caKeyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})); err != nil {
		t.Fatal(err)
	}
	got, err := m.CACertificate()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == string(der) {
		t.Fatal("authority without name constraints kept")
	}
	ca, _ := x509.ParseCertificate(got)
	if !constrained(ca) {
		t.Fatal("replacement without constraints")
	}
	// The next start keeps the replacement.
	changes := store.Writes()
	again, _ := NewManager(store, "nas")
	if der2, err := again.CACertificate(); err != nil || string(der2) != string(got) {
		t.Error("replaced a second time")
	}
	if store.Writes() != changes {
		t.Error("loading the replacement wrote to the stick")
	}
}

func TestNewAddressStaysInRAM(t *testing.T) {
	m, store := caManager(t)
	first, err := m.uiLeaf("", nil)
	if err != nil {
		t.Fatal(err)
	}
	changes := store.Writes()
	// IPv6 prefixes change: every new one gets a leaf, none is written.
	for _, addr := range []string{"fd00:1::20", "fd00:2::20", "fe80::20"} {
		ip := net.ParseIP(addr)
		leaf, err := m.uiLeaf("", ip)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(leaf.Leaf.IPAddresses, ip.Equal) {
			t.Errorf("leaf for %s does not name it", addr)
		}
		if again, _ := m.uiLeaf("", ip); again != leaf {
			t.Errorf("leaf for %s issued again", addr)
		}
	}
	// A global address is outside the constraints: no leaf, no write.
	if _, err := m.uiLeaf("", net.ParseIP("2001:db8::20")); !errors.Is(err, errNotPermitted) {
		t.Errorf("global IPv6: %v", err)
	}
	if store.Writes() != changes {
		t.Error("new addresses wrote to the stick")
	}
	// The stored leaf stays the one served by name.
	if byName, _ := m.uiLeaf("nas.local", net.ParseIP("fd00:9::1")); byName != first {
		t.Error("a request by name got another leaf")
	}
	// A new name of the NAS is written once.
	m.hostname = "titan"
	if _, err := m.uiLeaf("", nil); err != nil {
		t.Fatal(err)
	}
	if store.Writes() == changes {
		t.Error("a new name of the NAS was not stored")
	}
}

func TestAuthorityLeafRenewed(t *testing.T) {
	m, _ := caManager(t)
	now := time.Now()
	m.now = func() time.Time { return now }
	first, err := m.ForNames("a.nas.local", []string{"*.nas.local"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(leafValidity - leafRenew + time.Hour)
	second, _ := m.ForNames("a.nas.local", []string{"*.nas.local"})
	if second == first {
		t.Error("leaf not renewed before it runs out")
	}
}

func TestAuthorityNeverOverwritten(t *testing.T) {
	m, store := caManager(t)
	// Only the key is there (or the stick could not be read): no new one.
	if err := store.Write(caKeyFile, []byte("not a key")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.CACertificate(); err == nil {
		t.Fatal("authority made over an existing key")
	}
	data, _ := os.ReadFile(filepath.Join(store.Dir(), caKeyFile))
	if string(data) != "not a key" {
		t.Error("existing key overwritten")
	}
	// The web UI still comes up, with the self-signed certificate.
	cert, err := m.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil || cert != m.current.Load() {
		t.Error("no fallback to the self-signed certificate")
	}
}

func TestCustomBeatsAuthority(t *testing.T) {
	m, _ := caManager(t)
	certPEM, keyPEM := issue(t, nil)
	if err := m.SetCustom(certPEM, keyPEM); err != nil {
		t.Fatal(err)
	}
	if cert, _ := m.GetCertificate(&tls.ClientHelloInfo{}); cert != m.current.Load() {
		t.Error("own certificate not served")
	}
	if m.Info().Kind != KindCustom {
		t.Error("kind")
	}
}
