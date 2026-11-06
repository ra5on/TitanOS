package tlscert

import (
	"crypto/tls"
	"net"
	"slices"
	"testing"
)

// localConn is a connection that came in on an address of the NAS.
type localConn struct {
	net.Conn
	local net.Addr
}

func (c localConn) LocalAddr() net.Addr { return c.local }

func helloOn(ip, serverName string) *tls.ClientHelloInfo {
	return &tls.ClientHelloInfo{ServerName: serverName, Conn: localConn{local: &net.TCPAddr{IP: net.ParseIP(ip), Port: 20001}}}
}

// TestAddressCertificate: an app port asked for by the NAS's private
// address gets the authority's leaf with that address, also when an own
// certificate (which names neither the address nor .local) serves the
// web UI; a name the own certificate covers keeps it.
func TestAddressCertificate(t *testing.T) {
	m, _ := caManager(t)
	caDER, err := m.CACertificate()
	if err != nil {
		t.Fatal(err)
	}
	check := func(when string) {
		cert, err := m.AddressCertificate(helloOn("192.168.1.20", ""))
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(cert.Leaf.IPAddresses, net.ParseIP("192.168.1.20").Equal) {
			t.Errorf("%s: leaf names %v", when, cert.Leaf.IPAddresses)
		}
		if err := verifyWith(t, caDER, cert, "192.168.1.20"); err != nil {
			t.Errorf("%s: address not verified: %v", when, err)
		}
		// A new private address (DHCP) gets one as well.
		cert, err = m.AddressCertificate(helloOn("10.1.2.3", ""))
		if err != nil || verifyWith(t, caDER, cert, "10.1.2.3") != nil {
			t.Errorf("%s: new address: %v", when, err)
		}
		cert, err = m.AddressCertificate(helloOn("192.168.1.20", "nas.local"))
		if err != nil || verifyWith(t, caDER, cert, "nas.local") != nil {
			t.Errorf("%s: .local name: %v", when, err)
		}
	}
	check("self-signed")
	certPEM, keyPEM := issue(t, nil)
	if err := m.SetCustom(certPEM, keyPEM); err != nil {
		t.Fatal(err)
	}
	check("own certificate")
	if cert, _ := m.AddressCertificate(helloOn("192.168.1.20", "nas.example.org")); cert != m.current.Load() {
		t.Error("own certificate not served for its name")
	}
	// A public address is outside the constraints: the certificate in use.
	if cert, _ := m.AddressCertificate(helloOn("203.0.113.5", "")); cert != m.current.Load() {
		t.Error("public address did not fall back")
	}
}
