package tailscale

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// certRunner answers "tailscale status --json" with status and writes a
// certificate for name on "tailscale cert".
type certRunner struct {
	status string
	name   string
	fail   bool
	calls  []string
}

func (f *certRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if len(args) > 0 && args[0] == "status" {
		if f.status == "" {
			return "", errors.New("not running")
		}
		return f.status, nil
	}
	if f.fail {
		return "", errors.New("no certificate")
	}
	certPEM, keyPEM := testPair(f.name)
	os.WriteFile(args[2], certPEM, 0o600)
	os.WriteFile(args[4], keyPEM, 0o600)
	return "", nil
}

func testPair(name string) ([]byte, []byte) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour)}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	keyDER, _ := x509.MarshalECPrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func certFixture(t *testing.T, f *certRunner) *Manager {
	m := NewManager(config.Open(t.TempDir()))
	m.runtime = config.Open(t.TempDir())
	m.runner = f
	m.queryFeature = func(context.Context, string) ([]byte, error) { return nil, errors.New("no tailscaled") }
	return m
}

const runningStatus = `{"BackendState":"Running","Self":{"DNSName":"titan.bleak-locrian.ts.net."},"CertDomains":["titan.bleak-locrian.ts.net"]}`

func TestCertificateForTheOwnName(t *testing.T) {
	f := &certRunner{status: runningStatus, name: "titan.bleak-locrian.ts.net"}
	m := certFixture(t, f)
	cert, ok := m.refreshCertificate(context.Background(), nil)
	if !ok || cert == nil || cert.Leaf.VerifyHostname("titan.bleak-locrian.ts.net") != nil {
		t.Fatalf("no certificate: %v %v", cert, ok)
	}
	if got := m.Status().HTTPS; got.State != "ok" || got.Name != "titan.bleak-locrian.ts.net" || got.NotAfter == nil {
		t.Fatalf("status %+v", got)
	}
	if call := f.calls[1]; !strings.HasPrefix(call, "tailscale cert --cert-file ") || !strings.HasSuffix(call, " titan.bleak-locrian.ts.net") {
		t.Fatalf("call %q", call)
	}
	// The key does not stay on disk.
	if entries, _ := os.ReadDir(m.runtime.Dir() + "/cert"); len(entries) != 0 {
		t.Fatalf("files left: %v", entries)
	}
}

func TestCertificateStates(t *testing.T) {
	// Plugin off or not connected.
	m := certFixture(t, &certRunner{})
	if cert, _ := m.refreshCertificate(context.Background(), nil); cert != nil || m.Status().HTTPS.State != "inactive" {
		t.Fatalf("inactive: %+v", m.Status().HTTPS)
	}
	m = certFixture(t, &certRunner{status: `{"BackendState":"NeedsLogin"}`})
	if cert, _ := m.refreshCertificate(context.Background(), nil); cert != nil || m.Status().HTTPS.State != "inactive" {
		t.Fatal("needs login counts as inactive")
	}
	// HTTPS off in the tailnet.
	f := &certRunner{status: `{"BackendState":"Running","Self":{"DNSName":"titan.bleak-locrian.ts.net."},"CertDomains":null}`}
	m = certFixture(t, f)
	if cert, _ := m.refreshCertificate(context.Background(), nil); cert != nil || len(f.calls) != 1 {
		t.Fatal("asked for a certificate although HTTPS is off")
	}
	if got := m.Status().HTTPS; got.State != "disabled" || got.Name != "titan.bleak-locrian.ts.net" {
		t.Fatalf("disabled: %+v", got)
	}
	// A failed renewal keeps the valid certificate in use.
	f = &certRunner{status: runningStatus, name: "titan.bleak-locrian.ts.net"}
	m = certFixture(t, f)
	first, _ := m.refreshCertificate(context.Background(), nil)
	f.fail = true
	if cert, ok := m.refreshCertificate(context.Background(), first); cert != first || ok || m.Status().HTTPS.State != "error" {
		t.Fatalf("previous certificate dropped: %v %+v", ok, m.Status().HTTPS)
	}
	// A certificate for another name is refused.
	m = certFixture(t, &certRunner{status: runningStatus, name: "other.example.org"})
	if cert, _ := m.refreshCertificate(context.Background(), nil); cert != nil || m.Status().HTTPS.State != "error" {
		t.Fatal("certificate for another name accepted")
	}
	// Odd names from the status are not passed on.
	m = certFixture(t, &certRunner{status: `{"BackendState":"Running","Self":{"DNSName":"x."},"CertDomains":["--bad name"]}`})
	if cert, _ := m.refreshCertificate(context.Background(), nil); cert != nil || m.Status().HTTPS.State != "error" {
		t.Fatal("odd name passed on")
	}
}

func TestRunCertificateHandsItOver(t *testing.T) {
	m := certFixture(t, &certRunner{status: runningStatus, name: "titan.bleak-locrian.ts.net"})
	got := make(chan *tls.Certificate, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Disabled = true
	m.RunCertificate(ctx, func(c *tls.Certificate) { got <- c })
	if len(got) != 0 {
		t.Fatal("development mode fetched a certificate")
	}
}

func TestEnableLinkWhileHTTPSIsOff(t *testing.T) {
	off := `{"BackendState":"Running","Self":{"DNSName":"titan.bleak-locrian.ts.net."},"CertDomains":null}`
	for _, c := range []struct{ answer, want string }{
		{`{"Complete":false,"Text":"Serve is not enabled","URL":"https://login.tailscale.com/f/serve?node=nXYZ"}`, "https://login.tailscale.com/f/serve?node=nXYZ"},
		{`{"Complete":false,"URL":"https://evil.example.org/f/serve"}`, ""},
		{`{"Complete":true,"URL":"https://login.tailscale.com/f/serve?node=n"}`, ""},
		{`not json`, ""},
	} {
		m := certFixture(t, &certRunner{status: off})
		var asked string
		m.queryFeature = func(_ context.Context, feature string) ([]byte, error) { asked = feature; return []byte(c.answer), nil }
		m.refreshCertificate(context.Background(), nil)
		if got := m.Status().HTTPS; got.State != "disabled" || got.EnableURL != c.want || asked != "serve" {
			t.Fatalf("answer %s: %+v (asked %q)", c.answer, got, asked)
		}
	}
	// With HTTPS on there is no link.
	m := certFixture(t, &certRunner{status: runningStatus, name: "titan.bleak-locrian.ts.net"})
	m.queryFeature = func(context.Context, string) ([]byte, error) { t.Fatal("asked for a link with HTTPS on"); return nil, nil }
	m.refreshCertificate(context.Background(), nil)
}

func TestOpenPageLooksAgainSoon(t *testing.T) {
	m := certFixture(t, &certRunner{status: `{"BackendState":"Running","Self":{"DNSName":"t.x.ts.net."},"CertDomains":null}`})
	m.refreshCertificate(context.Background(), nil)
	m.Status()
	if len(m.cert.kick) != 0 {
		t.Fatal("looked again right after the last look")
	}
	m.mu.Lock()
	m.cert.tried = time.Now().Add(-time.Minute)
	m.mu.Unlock()
	m.Status()
	if len(m.cert.kick) != 1 {
		t.Fatal("open page does not make titand look again")
	}
}
