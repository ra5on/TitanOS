package tlscert

import (
	"crypto/x509"
	"slices"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestAppCertificate(t *testing.T) {
	store := config.Open(t.TempDir())
	m, err := NewManager(store, "nas")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"*.nas.local"}
	cert, err := m.ForNames("immich.nas.local", names)
	if err != nil {
		t.Fatal(err)
	}
	if cert == m.current.Load() {
		t.Fatal("the web UI's own certificate for an app")
	}
	leaf, _ := x509.ParseCertificate(cert.Certificate[0])
	if !slices.Equal(leaf.DNSNames, names) || leaf.VerifyHostname("immich.nas.local") != nil || leaf.VerifyHostname("nas.local") == nil {
		t.Fatalf("names %v", leaf.DNSNames)
	}
	if _, err := store.Read(appsCertFile); err != nil {
		t.Fatal("not stored:", err)
	}

	// The same names: the same certificate, also after a restart, so that
	// the browser's exception holds.
	again, _ := m.ForNames("media.nas.local", names)
	if again != cert {
		t.Error("new certificate for the same names")
	}
	restarted, _ := NewManager(store, "nas")
	after, _ := restarted.ForNames("immich.nas.local", names)
	if string(after.Certificate[0]) != string(cert.Certificate[0]) {
		t.Error("new certificate after a restart")
	}

	// A domain of one's own is added: a certificate that names both.
	both := []string{"*.nas.local", "*.home.example.org"}
	next, _ := restarted.ForNames("immich.home.example.org", both)
	leaf, _ = x509.ParseCertificate(next.Certificate[0])
	if !slices.Equal(leaf.DNSNames, both) || leaf.VerifyHostname("immich.home.example.org") != nil {
		t.Fatalf("names %v", leaf.DNSNames)
	}
}

func TestAppCertificateFromImportedWildcard(t *testing.T) {
	store := config.Open(t.TempDir())
	m, err := NewManager(store, "nas")
	if err != nil {
		t.Fatal(err)
	}
	certPEM, keyPEM := issue(t, func(c *x509.Certificate) { c.DNSNames = []string{"nas.home.example.org", "*.home.example.org"} })
	if err := m.SetCustom(certPEM, keyPEM); err != nil {
		t.Fatal(err)
	}
	names := []string{"*.nas.local", "*.home.example.org"}
	cert, _ := m.ForNames("immich.home.example.org", names)
	if cert != m.current.Load() {
		t.Error("imported wildcard not used for an app of its domain")
	}
	// The .local name is not in it: the certificate TitanOS makes.
	cert, _ = m.ForNames("immich.nas.local", names)
	if cert == m.current.Load() {
		t.Error("imported certificate used for a name it does not cover")
	}
}
