package tlscert

import (
	"bytes"
	"crypto/x509"
	"slices"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestLoadOrCreate(t *testing.T) {
	store := config.Open(t.TempDir())

	first, err := LoadOrCreate(store, "nas")
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(first.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(leaf.DNSNames, "nas") || !slices.Contains(leaf.DNSNames, "nas.local") {
		t.Errorf("DNS names = %v", leaf.DNSNames)
	}
	if err := leaf.VerifyHostname("127.0.0.1"); err != nil {
		t.Errorf("certificate does not cover 127.0.0.1: %v", err)
	}
	if days := time.Until(leaf.NotAfter).Hours() / 24; days < 800 || days > 825 {
		t.Errorf("valid for %.0f days", days)
	}

	// The second start reuses the stored certificate.
	second, err := LoadOrCreate(store, "nas")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Certificate[0], second.Certificate[0]) {
		t.Error("a new certificate was created although one was stored")
	}
}

func TestRenewsExpiringCertificate(t *testing.T) {
	store := config.Open(t.TempDir())

	certPEM, keyPEM, err := generate("nas", time.Now().Add(-validity+24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write(certFile, certPEM); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(keyFile, keyPEM); err != nil {
		t.Fatal(err)
	}

	cert, err := LoadOrCreate(store, "nas")
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(cert.Certificate[0])
	if time.Until(leaf.NotAfter) < renewBefore {
		t.Error("expiring certificate was not replaced")
	}
}

func TestWithoutStore(t *testing.T) {
	// A store that cannot be written to (stick missing) still yields a certificate.
	store := config.Open("/proc/titanos-no-such-dir")
	cert, err := LoadOrCreate(store, "nas")
	if err == nil {
		t.Error("expected an error about the certificate not being saved")
	}
	if len(cert.Certificate) == 0 {
		t.Error("no certificate returned")
	}
}
