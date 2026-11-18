package profile

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// testChain makes a root, an intermediate and a server certificate (only
// serverAuth, like the ones of Let's Encrypt and Tailscale) with keys of
// the given kind; the tls.Certificate is leaf + intermediate.
func testChain(t *testing.T, kind string) (root *x509.Certificate, cert tls.Certificate) {
	t.Helper()
	newKey := func() crypto.Signer {
		if kind == "rsa" {
			k, err := rsa.GenerateKey(rand.Reader, 2048)
			if err != nil {
				t.Fatal(err)
			}
			return k
		}
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	now := time.Now()
	make := func(tmpl, parent *x509.Certificate, pub, signer crypto.Signer) *x509.Certificate {
		der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub.Public(), signer)
		if err != nil {
			t.Fatal(err)
		}
		c, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	rootKey, midKey, leafKey := newKey(), newKey(), newKey()
	rootT := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test Root"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	root = make(rootT, rootT, rootKey, rootKey)
	midT := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Test Intermediate"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	mid := make(midT, root, midKey, rootKey)
	leafT := &x509.Certificate{SerialNumber: big.NewInt(300), Subject: pkix.Name{CommonName: "titan.tail1a2b3.ts.net"}, DNSNames: []string{"titan.tail1a2b3.ts.net"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leaf := make(leafT, mid, leafKey, midKey)
	return root, tls.Certificate{Certificate: [][]byte{leaf.Raw, mid.Raw}, PrivateKey: leafKey}
}

func TestSignVerifies(t *testing.T) {
	content := []byte("<?xml version=\"1.0\"?><plist version=\"1.0\"><dict/></plist>\n")
	for _, kind := range []string{"ecdsa", "rsa"} {
		t.Run(kind, func(t *testing.T) {
			root, cert := testChain(t, kind)
			signed, err := Sign(content, &cert, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			got, certs, err := Verify(signed)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, content) || len(certs) != 2 {
				t.Fatalf("content %q, %d certificates", got, len(certs))
			}
			// A changed byte of the content breaks it.
			broken := bytes.Replace(signed, []byte("plist version"), []byte("plist versioN"), 1)
			if _, _, err := Verify(broken); err == nil {
				t.Fatal("changed content verified")
			}
			opensslVerify(t, signed, root, content)
		})
	}
}

// opensslVerify checks the signature as a third party would: openssl with
// only the root as trusted (the intermediate must come from the data),
// any purpose (the leaf is for serverAuth, like the real ones).
func opensslVerify(t *testing.T, signed []byte, root *x509.Certificate, content []byte) {
	t.Helper()
	bin, err := exec.LookPath("openssl")
	if err != nil {
		t.Log("openssl not installed: checked with the Go parser only")
		return
	}
	dir := t.TempDir()
	in, ca, out := filepath.Join(dir, "p.mobileconfig"), filepath.Join(dir, "root.pem"), filepath.Join(dir, "out")
	if err := os.WriteFile(in, signed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"cms", "-verify", "-inform", "DER", "-in", in, "-CAfile", ca, "-purpose", "any", "-out", out},
		{"smime", "-verify", "-inform", "DER", "-in", in, "-CAfile", ca, "-purpose", "any", "-out", out},
	} {
		res, err := exec.Command(bin, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("openssl %s: %v\n%s", args[0], err, res)
		}
		got, err := os.ReadFile(out)
		if err != nil || !bytes.Equal(got, content) {
			t.Fatalf("openssl %s gave %q", args[0], got)
		}
	}
	// Another root: refused.
	other, _ := testChain(t, "ecdsa")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: other.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	if res, err := exec.Command(bin, "cms", "-verify", "-inform", "DER", "-in", in, "-CAfile", ca, "-purpose", "any", "-out", out).CombinedOutput(); err == nil {
		t.Fatalf("openssl verified with another root:\n%s", res)
	}
}

func TestSignRefusesWhatCannotSign(t *testing.T) {
	_, cert := testChain(t, "ecdsa")
	for name, c := range map[string]*tls.Certificate{
		"nil":      nil,
		"empty":    {},
		"no key":   {Certificate: cert.Certificate},
		"ed25519":  {Certificate: cert.Certificate, PrivateKey: ed25519.NewKeyFromSeed(make([]byte, 32))},
		"garbage":  {Certificate: [][]byte{{1, 2, 3}}, PrivateKey: cert.PrivateKey},
		"only key": {PrivateKey: cert.PrivateKey},
	} {
		if _, err := Sign([]byte("x"), c, time.Now()); err == nil {
			t.Errorf("%s: signed", name)
		}
	}
}
