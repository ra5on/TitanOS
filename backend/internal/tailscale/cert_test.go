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
	on := true
	m.settings.HTTPSCertificate = &on
	m.loaded = true
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
	m.queryFeature = func(context.Context, string) ([]byte, error) {
		t.Fatal("asked for a link with HTTPS on")
		return nil, nil
	}
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

// Off by default: without the switch nothing is asked of Tailscale (the
// name would land in the public certificate logs); a certificate already
// served is withdrawn when it is switched off.
func TestCertificateSwitch(t *testing.T) {
	f := &certRunner{status: runningStatus, name: "titan.bleak-locrian.ts.net"}
	m := certFixture(t, f)
	m.settings.HTTPSCertificate = nil
	if cert, _ := m.refreshCertificate(context.Background(), nil); cert != nil {
		t.Fatal("certificate without the switch")
	}
	if got := m.Status().HTTPS; got.State != "off" || got.Enabled || got.Name != "titan.bleak-locrian.ts.net" {
		t.Fatalf("off: %+v", got)
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "tailscale cert") {
			t.Fatal("asked Tailscale for a certificate while off")
		}
	}
	if err := m.SetHTTPSCertificate(true); err != nil {
		t.Fatal(err)
	}
	if len(m.cert.kick) != 1 {
		t.Fatal("switching on does not make titand look at once")
	}
	<-m.cert.kick
	var stored Settings
	if err := m.store.ReadJSON(settingsFile, &stored); err != nil || stored.HTTPSCertificate == nil || !*stored.HTTPSCertificate {
		t.Fatalf("stored %+v %v", stored, err)
	}
	cert, ok := m.refreshCertificate(context.Background(), nil)
	if cert == nil || !ok || !m.Status().HTTPS.Enabled {
		t.Fatal("no certificate when switched on")
	}
	// The trial of the other settings keeps the switch.
	m.volume = "data"
	m.runner = okRunner{}
	if err := m.Set(Settings{Routes: []string{}}); err != nil {
		t.Fatal(err)
	}
	if m.settings.HTTPSCertificate == nil || !*m.settings.HTTPSCertificate {
		t.Fatal("settings form reset the switch")
	}
	if err := m.SetHTTPSCertificate(false); !errors.Is(err, ErrPending) {
		t.Fatalf("switched during the trial: %v", err)
	}
	if err := m.Undo(); err != nil {
		t.Fatal(err)
	}
	if err := m.SetHTTPSCertificate(false); err != nil {
		t.Fatal(err)
	}
	m.runner = f
	if cert, _ := m.refreshCertificate(context.Background(), cert); cert != nil || m.Status().HTTPS.State != "off" {
		t.Fatal("certificate still served after switching off")
	}
}

// Settings of an older version have no switch: on only if tailscaled
// already holds a certificate for the name (the name is public already).
func TestCertificateSwitchFromOlderSettings(t *testing.T) {
	f := &certRunner{status: runningStatus, name: "titan.bleak-locrian.ts.net"}
	m := certFixture(t, f)
	m.settings.HTTPSCertificate = nil
	m.mountRoot, m.volume = t.TempDir(), "data"
	if m.httpsWanted("titan.bleak-locrian.ts.net") {
		t.Fatal("on without an earlier certificate")
	}
	dir := m.mountRoot + "/data/.titanos/tailscale/certs"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/titan.bleak-locrian.ts.net.crt", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !m.httpsWanted("titan.bleak-locrian.ts.net") || m.httpsWanted("../../x") {
		t.Fatal("earlier certificate not found, or odd name used")
	}
	off := false
	m.settings.HTTPSCertificate = &off
	if m.httpsWanted("titan.bleak-locrian.ts.net") {
		t.Fatal("explicit off ignored")
	}
}

// okRunner answers every command without output (the runtime helper).
type okRunner struct{}

func (okRunner) Run(context.Context, string, string, ...string) (string, error) { return "", nil }
