package tlscert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// issue makes a certificate as an administrator might bring one: signed by
// an authority of their own, with the authority's certificate attached.
func issue(t *testing.T, change func(*x509.Certificate)) (certPEM, keyPEM []byte) {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Home CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "nas.example.org"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(12 * time.Hour),
		DNSNames: []string{"nas.example.org"}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if change != nil {
		change(leaf)
	}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	certPEM = append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})...)
	return certPEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func TestCustomCertificate(t *testing.T) {
	store := config.Open(t.TempDir())
	m, err := NewManager(store, "nas")
	if err != nil {
		t.Fatal(err)
	}
	own := m.Info()
	if own.Kind != KindSelfSigned || len(own.Fingerprint) != 64 || own.Names[0] != "nas" {
		t.Fatalf("at first: %+v", own)
	}

	certPEM, keyPEM := issue(t, nil)
	if err := m.SetCustom(certPEM, keyPEM); err != nil {
		t.Fatal(err)
	}
	info := m.Info()
	if info.Kind != KindCustom || info.Issuer != "Home CA" || len(info.Names) != 1 || info.Names[0] != "nas.example.org" || info.Fingerprint == own.Fingerprint {
		t.Errorf("custom: %+v", info)
	}
	served, _ := m.GetCertificate(nil)
	if len(served.Certificate) != 2 {
		t.Errorf("the chain is not served: %d certificates", len(served.Certificate))
	}

	// It is still there after a restart.
	again, err := NewManager(store, "nas")
	if err != nil || again.Info().Fingerprint != info.Fingerprint {
		t.Errorf("after a restart: %+v %v", again.Info(), err)
	}

	if err := m.RemoveCustom(); err != nil {
		t.Fatal(err)
	}
	if back := m.Info(); back.Kind != KindSelfSigned || back.Fingerprint != own.Fingerprint {
		t.Errorf("after removing: %+v", back)
	}
}

func TestUnusableCertificatesAreRefused(t *testing.T) {
	store := config.Open(t.TempDir())
	m, _ := NewManager(store, "nas")
	before := m.Info().Fingerprint
	good, goodKey := issue(t, nil)
	_, otherKey := issue(t, nil)
	expired, expiredKey := issue(t, func(c *x509.Certificate) { c.NotAfter = time.Now().Add(-time.Minute) })
	future, futureKey := issue(t, func(c *x509.Certificate) { c.NotBefore = time.Now().Add(time.Hour) })
	nameless, namelessKey := issue(t, func(c *x509.Certificate) { c.DNSNames = nil })
	authority, authorityKey := issue(t, func(c *x509.Certificate) { c.IsCA, c.BasicConstraintsValid = true, true })
	cases := map[string][2][]byte{
		"not a certificate":  {[]byte("hello"), goodKey},
		"no key":             {good, []byte("hello")},
		"key of another":     {good, otherKey},
		"key as certificate": {goodKey, goodKey},
		"expired":            {expired, expiredKey},
		"not valid yet":      {future, futureKey},
		"names no host":      {nameless, namelessKey},
		"an authority":       {authority, authorityKey},
		"too large":          {append(good, make([]byte, maxPEM)...), goodKey},
	}
	for name, pair := range cases {
		err := m.SetCustom(pair[0], pair[1])
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
		// Nothing of a key may show up in what is reported back.
		if err != nil && strings.Contains(err.Error(), "BEGIN") {
			t.Errorf("%s: the error repeats the input: %v", name, err)
		}
	}
	if m.Info().Fingerprint != before {
		t.Error("a refused certificate is being served")
	}
	if _, err := store.Read(customCertFile); err == nil {
		t.Error("a refused certificate was stored")
	}
}

func TestExpiredCustomCertificateFallsBack(t *testing.T) {
	store := config.Open(t.TempDir())
	m, _ := NewManager(store, "nas")
	certPEM, keyPEM := issue(t, nil)
	if err := m.SetCustom(certPEM, keyPEM); err != nil {
		t.Fatal(err)
	}
	// A day later it has run out; a restart must not leave the web UI
	// without a certificate.
	later, err := NewManager(store, "nas")
	if err != nil {
		t.Fatal(err)
	}
	later.now = func() time.Time { return time.Now().Add(24 * time.Hour) }
	if _, err := later.loadCustom(); !errors.Is(err, ErrInvalid) {
		t.Errorf("expired certificate loaded: %v", err)
	}
}
