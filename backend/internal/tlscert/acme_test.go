package tlscert

import (
	"crypto/tls"
	"crypto/x509"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestLetsEncryptCertificateByName(t *testing.T) {
	store := config.Open(t.TempDir())
	m, err := NewManager(store, "nas")
	if err != nil {
		t.Fatal(err)
	}
	selfSigned, _ := m.GetCertificate(&tls.ClientHelloInfo{ServerName: "nas.example.org"})
	certPEM, keyPEM := issue(t, func(c *x509.Certificate) { c.DNSNames = []string{"nas.example.org", "*.example.org"} })
	acme, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	m.SetACME(&acme)
	for name, want := range map[string]*tls.Certificate{"nas.example.org": &acme, "": selfSigned, "nas.local": selfSigned} {
		got, _ := m.GetCertificate(&tls.ClientHelloInfo{ServerName: name})
		if got != want {
			t.Errorf("%q: wrong certificate", name)
		}
	}
	// App names under the wildcard get it too, others the app certificate.
	if got, _ := m.ForNames("immich.example.org", []string{"*.example.org"}); got != &acme {
		t.Error("app name under the wildcard")
	}
	if got, _ := m.ForNames("immich.nas.local", []string{"*.nas.local"}); got == &acme {
		t.Error(".local app name")
	}
	info := m.Info()
	if info.Kind != KindSelfSigned || info.LetsEncrypt == nil || info.LetsEncrypt.Kind != KindLetsEncrypt || len(info.LetsEncrypt.Names) != 2 {
		t.Fatalf("%+v", info)
	}
	m.SetACME(nil)
	if got, _ := m.GetCertificate(&tls.ClientHelloInfo{ServerName: "nas.example.org"}); got != selfSigned || m.Info().LetsEncrypt != nil {
		t.Error("back to the self-signed one")
	}
}
