package tlscert

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
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

func TestNamesAreUnique(t *testing.T) {
	// Two certificates of the same NAS must not share subject and issuer:
	// Firefox would take the older one for the issuer of the newer.
	names := map[string]bool{}
	for range 2 {
		certPEM, _, err := generate("nas", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		block, _ := pem.Decode(certPEM)
		leaf, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		if leaf.Subject.String() != leaf.Issuer.String() || leaf.Subject.CommonName != "nas" {
			t.Errorf("subject %q, issuer %q", leaf.Subject, leaf.Issuer)
		}
		if names[leaf.Subject.String()] {
			t.Errorf("subject %q used twice", leaf.Subject)
		}
		names[leaf.Subject.String()] = true
	}
}

func TestReplacesCertificateWithSharedName(t *testing.T) {
	// A certificate as earlier versions made it: CN=nas, O=TitanOS only.
	store := config.Open(t.TempDir())
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: "nas", Organization: []string{"TitanOS"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(validity),
		DNSNames:     []string{"nas"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	store.Write(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	store.Write(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))

	cert, err := LoadOrCreate(store, "nas")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(cert.Certificate[0], der) {
		t.Fatal("certificate with a shared name was kept")
	}
	again, err := LoadOrCreate(store, "nas")
	if err != nil || !bytes.Equal(again.Certificate[0], cert.Certificate[0]) {
		t.Error("the replacement was not stored and reused")
	}
}
