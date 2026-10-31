package cluster

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// joinedPair makes a cluster of two test nodes: a created it, b joined.
func joinedPair(t *testing.T) (a, b *testNode) {
	t.Helper()
	a = newTestNode(t, "nas-a", map[string]string{"alex": "password-of-a"})
	b = newTestNode(t, "nas-b", map[string]string{"bob": "password-of-b"})
	if err := a.m.Create("Home"); err != nil {
		t.Fatal(err)
	}
	if err := b.m.Join(context.Background(), a.m.JoinInfo()); err != nil {
		t.Fatal(err)
	}
	return a, b
}

func signedBy(t *testing.T, n *Manager, method, target, user string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = "192.168.1.20:5000"
	if err := n.sign(req, user); err != nil {
		t.Fatal(err)
	}
	return req
}

func withCertificate(req *http.Request, cert *x509.Certificate) *http.Request {
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	return req
}

func strangerCertificate(t *testing.T) *x509.Certificate {
	t.Helper()
	data, err := newCertificate()
	if err != nil {
		t.Fatal(err)
	}
	c, err := parseCertificate(data)
	if err != nil {
		t.Fatal(err)
	}
	return c.tls.Leaf
}

// A correct signature is not enough: the TLS connection must show the
// certificate of the node the request is signed for.
func TestVerifyRefusesForeignCertificate(t *testing.T) {
	a, b := joinedPair(t)
	if _, err := a.m.Verify(withCertificate(signedBy(t, b.m, "GET", "/api/v1/storage", "alex"), b.m.cert.tls.Leaf)); err != nil {
		t.Fatalf("own certificate refused: %v", err)
	}
	for name, cert := range map[string]*x509.Certificate{
		"certificate of the receiving node": a.m.cert.tls.Leaf,
		"certificate nobody knows":          strangerCertificate(t),
	} {
		req := withCertificate(signedBy(t, b.m, "GET", "/api/v1/storage", "alex"), cert)
		if _, err := a.m.Verify(req); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("%s: %v", name, err)
		}
	}
	empty := signedBy(t, b.m, "GET", "/api/v1/storage", "alex")
	empty.TLS = &tls.ConnectionState{}
	if _, err := a.m.Verify(empty); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("TLS without a certificate: %v", err)
	}
}

// Every signed part of the request counts: method, path, query, account.
func TestVerifyRefusesChangedRequest(t *testing.T) {
	a, b := joinedPair(t)
	leaf := b.m.cert.tls.Leaf
	change := map[string]func(*http.Request){
		"method": func(r *http.Request) { r.Method = "DELETE" },
		"path":   func(r *http.Request) { r.URL.Path = "/api/v1/users" },
		"query":  func(r *http.Request) { r.URL.RawQuery = "all=1" },
		"account": func(r *http.Request) {
			r.Header.Set(Header, strings.Replace(r.Header.Get(Header), "user=alex", "user=bob", 1))
		},
		"signature": func(r *http.Request) {
			h := r.Header.Get(Header)
			r.Header.Set(Header, h[:len(h)-2]+"AA")
		},
		"no header": func(r *http.Request) { r.Header.Del(Header) },
	}
	for name, apply := range change {
		req := withCertificate(signedBy(t, b.m, "GET", "/api/v1/storage", "alex"), leaf)
		apply(req)
		if _, err := a.m.Verify(req); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("changed %s: %v", name, err)
		}
	}
}

// A refused request must not use up the nonce of the real one, and a
// request accepted once is refused the second time, also on a node that
// left the cluster meanwhile.
func TestReplayAndForgedNonce(t *testing.T) {
	a, b := joinedPair(t)
	leaf := b.m.cert.tls.Leaf
	real := withCertificate(signedBy(t, b.m, "GET", "/api/v1/storage", "alex"), leaf)
	forged := real.Clone(context.Background())
	forged.Header.Set(Header, strings.Replace(real.Header.Get(Header), "user=alex", "user=bob", 1))
	if _, err := a.m.Verify(forged); err == nil {
		t.Fatal("forged request accepted")
	}
	if _, err := a.m.Verify(real); err != nil {
		t.Fatalf("real request refused after a forged one with its nonce: %v", err)
	}
	if _, err := a.m.Verify(real); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("replay accepted: %v", err)
	}
}

// The client pins the certificate: a server with another one is refused,
// whoever signed it.
func TestPinnedRefusesForeignServerCertificate(t *testing.T) {
	a, b := joinedPair(t)
	cfg := pinned(a.m.cert.fingerprint, b.m.cert.tls)
	if err := cfg.VerifyPeerCertificate([][]byte{a.m.cert.tls.Certificate[0]}, nil); err != nil {
		t.Fatalf("pinned certificate refused: %v", err)
	}
	if err := cfg.VerifyPeerCertificate([][]byte{b.m.cert.tls.Certificate[0]}, nil); err == nil {
		t.Fatal("certificate of another node accepted")
	}
	if err := cfg.VerifyPeerCertificate(nil, nil); err == nil {
		t.Fatal("no certificate accepted")
	}
	if cfg.ServerName != ServerName {
		t.Fatalf("server name %q", cfg.ServerName)
	}
	// The node shows its cluster certificate only for the cluster name.
	if a.m.Certificate(&tls.ClientHelloInfo{ServerName: "nas.local"}) != nil || a.m.ServerConfig(&tls.ClientHelloInfo{}) != nil {
		t.Fatal("cluster certificate for another name")
	}
	if c := a.m.Certificate(&tls.ClientHelloInfo{ServerName: ServerName}); c == nil || fingerprint(c.Certificate[0]) != a.m.cert.fingerprint {
		t.Fatal("cluster certificate missing")
	}
}

// Joining with information whose certificate is not the one the node
// shows fails over TLS, before the key is sent, and changes nothing.
func TestJoinRefusesForeignCertificate(t *testing.T) {
	a := newTestNode(t, "nas-a", map[string]string{"alex": "password-of-a"})
	b := newTestNode(t, "nas-b", map[string]string{"bob": "password-of-b"})
	if err := a.m.Create("Home"); err != nil {
		t.Fatal(err)
	}
	info, err := parseJoinInfo(a.m.JoinInfo())
	if err != nil {
		t.Fatal(err)
	}
	other, err := parseCertificate(mustCertificate(t))
	if err != nil {
		t.Fatal(err)
	}
	for i := range info.Nodes {
		info.Nodes[i].Cert = other.fingerprint
	}
	data, _ := json.Marshal(info)
	err = b.m.Join(context.Background(), JoinPrefix+base64.RawURLEncoding.EncodeToString(data))
	if err == nil {
		t.Fatal("joined a node with a foreign certificate")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("refused for another reason: %v", err)
	}
	if b.m.InCluster() || len(a.m.View().Nodes) != 1 {
		t.Fatal("join with a foreign certificate changed the cluster")
	}
	// The real information still works afterwards.
	if err := b.m.Join(context.Background(), a.m.JoinInfo()); err != nil {
		t.Fatalf("join with the real information: %v", err)
	}
}

func mustCertificate(t *testing.T) string {
	t.Helper()
	data, err := newCertificate()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Join information that is damaged or incomplete is refused before any
// connection.
func TestParseJoinInfoRefusesBadInput(t *testing.T) {
	good := joinInfo{V: 1, ID: "x", Name: "Home", Key: strings.Repeat("a", 64), Nodes: []joinNode{{Addresses: []string{"10.0.0.2:443"}, Cert: strings.Repeat("b", 64)}}}
	encode := func(i joinInfo) string {
		data, _ := json.Marshal(i)
		return JoinPrefix + base64.RawURLEncoding.EncodeToString(data)
	}
	if _, err := parseJoinInfo(encode(good)); err != nil {
		t.Fatalf("good information refused: %v", err)
	}
	bad := map[string]func(*joinInfo){
		"version":      func(i *joinInfo) { i.V = 2 },
		"short key":    func(i *joinInfo) { i.Key = "abc" },
		"no nodes":     func(i *joinInfo) { i.Nodes = nil },
		"short cert":   func(i *joinInfo) { i.Nodes[0].Cert = "abc" },
		"no addresses": func(i *joinInfo) { i.Nodes[0].Addresses = nil },
		"too many nodes": func(i *joinInfo) {
			for range MaxNodes {
				i.Nodes = append(i.Nodes, i.Nodes[0])
			}
		},
	}
	for name, change := range bad {
		i := good
		i.Nodes = append([]joinNode(nil), good.Nodes...)
		change(&i)
		if _, err := parseJoinInfo(encode(i)); !errors.Is(err, ErrJoinInfo) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, text := range []string{"", "nonsense", JoinPrefix + "!!!", JoinPrefix + base64.RawURLEncoding.EncodeToString([]byte("{"))} {
		if _, err := parseJoinInfo(text); !errors.Is(err, ErrJoinInfo) {
			t.Errorf("%q: %v", text, err)
		}
	}
}
