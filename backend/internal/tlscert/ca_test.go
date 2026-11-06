package tlscert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
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
	if left := ui.Leaf.NotAfter.Sub(time.Now()); left > 31*24*time.Hour {
		t.Errorf("leaf lasts too long: %v", left)
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
	// Leaves stay in RAM: no apps certificate on the stick.
	if _, err := store.Read(appsCertFile); err == nil {
		t.Error("leaf written to the stick")
	}

	// The same authority after a restart.
	restarted, _ := NewManager(store, "nas")
	der2, err := restarted.CACertificate()
	if err != nil || string(der2) != string(caDER) {
		t.Error("authority not kept across restarts")
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
