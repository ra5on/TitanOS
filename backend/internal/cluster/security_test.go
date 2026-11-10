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
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
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

// The protocol between the nodes is not reachable for an account: neither
// through the proxy of the web interface (any account on a managing node
// could send accounts or members to the others) nor as a signed request
// that names an account.
func TestPeerProtocolNotForAccounts(t *testing.T) {
	a, b := joinedPair(t)
	for _, path := range []string{"cluster/peer/accounts", "/cluster/peer/members", "cluster/./peer/node", "cluster//peer/leave", "x/../cluster/peer/backup/pair", "cluster/peer"} {
		rec := httptest.NewRecorder()
		a.m.Proxy(rec, httptest.NewRequest("POST", "/api/v1/cluster/nodes/"+b.m.ID()+"/api/"+path, strings.NewReader("[]")), b.m.ID(), "alex", path)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "cluster_not_permitted") {
			t.Errorf("proxy to %s: %d %s", path, rec.Code, rec.Body)
		}
	}
	for _, target := range []string{PeerPrefix + "accounts", PeerPrefix + "status"} {
		req := withCertificate(signedBy(t, a.m, "GET", target, "alex"), a.m.cert.tls.Leaf)
		rec := httptest.NewRecorder()
		b.m.PeerHandler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s signed for an account: %d %s", target, rec.Code, rec.Body)
		}
		req = withCertificate(signedBy(t, a.m, "GET", target, ""), a.m.cert.tls.Leaf)
		rec = httptest.NewRecorder()
		b.m.PeerHandler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s signed by the node: %d %s", target, rec.Code, rec.Body)
		}
	}
	if peerPath("storage") || peerPath("cluster/nodes") || peerPath("cluster/peers") {
		t.Error("other paths taken for the protocol")
	}
}

// failingImport is Accounts whose Import fails while fail is set.
type failingImport struct {
	Accounts
	fail *bool
}

func (f failingImport) Import(user auth.User) (auth.User, bool, error) {
	if *f.fail {
		return auth.User{}, false, errors.New("stick not writable")
	}
	return f.Accounts.Import(user)
}

// An account that could not be taken over keeps its old version here, so
// that the admin node sends it again instead of thinking it arrived.
func TestFailedImportIsSentAgain(t *testing.T) {
	a, b := joinedPair(t)
	fail := true
	b.m.Users = failingImport{Accounts: b.users, fail: &fail}
	old := a.m.versions()["alex"]
	if _, err := a.users.SetPassword("alex", "a-new-password"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "new version on A", func() bool { return a.m.versions()["alex"] != old })
	a.m.pushAccount(context.Background(), "alex")
	if sameAccount(a, b, "alex") {
		t.Fatal("account changed although Import failed")
	}
	if b.m.versions()["alex"] == a.m.versions()["alex"] {
		t.Fatal("version noted for an account that was not taken over")
	}
	fail = false
	a.m.syncAccounts(context.Background())
	if !sameAccount(a, b, "alex") {
		t.Fatal("account not sent again")
	}
}

// Background work that started in the cluster must not panic when the node
// left meanwhile.
func TestBackgroundAfterLeaving(t *testing.T) {
	a := newTestNode(t, "nas-a", map[string]string{"alex": "password-of-a"})
	if err := a.m.Create("Home"); err != nil {
		t.Fatal(err)
	}
	a.m.mu.Lock()
	a.m.state = nil
	a.m.mu.Unlock()
	if _, ok := a.m.record("alex"); ok {
		t.Error("record outside a cluster")
	}
	a.m.watchNodes(context.Background())
}

// An account a node keeps for itself does not act on another node that
// has its own account of that name: neither the sending node nor the
// receiving one lets it through.
func TestLocalAccountDoesNotActElsewhere(t *testing.T) {
	a, b := joinedPair(t)
	c := newTestNode(t, "nas-c", map[string]string{"cora": "password-of-c"})
	if err := c.m.Join(context.Background(), a.m.JoinInfo()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "C known on B", func() bool { _, err := b.m.node(c.m.ID()); return err == nil })
	if err := a.m.SetManage(context.Background(), b.m.ID(), true); err != nil {
		t.Fatal(err)
	}
	eventually(t, "B may manage", func() bool { return b.m.Manages() && c.m.MayManage(b.m.ID()) })
	// carol is plain on B and owner on C, neither from the admin node.
	if _, err := b.users.Create("carol", "password-of-b", auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	if _, err := c.users.Create("carol", "password-of-c", auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if b.m.ClusterAccount("carol") || c.m.ClusterAccount("carol") {
		t.Fatal("local account taken for one of the cluster")
	}
	if !b.m.ClusterAccount("alex") || !c.m.ClusterAccount("alex") || !a.m.ClusterAccount("alex") {
		t.Fatal("account of the admin node not one of the cluster")
	}
	rec := httptest.NewRecorder()
	b.m.Proxy(rec, httptest.NewRequest("GET", "/api/v1/storage", nil), c.m.ID(), "carol", "storage")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("proxy for a local account: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	b.m.Proxy(rec, httptest.NewRequest("GET", "/api/v1/storage", nil), c.m.ID(), "alex", "storage")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "alex ") {
		t.Fatalf("proxy for an account of the cluster: %d %s", rec.Code, rec.Body)
	}
}

// The relay passes restic's requests on, not the pairing: that is for the
// backup page of an administrator, and every program here reaches the relay.
func TestRelayRefusesPairing(t *testing.T) {
	a, b := joinedPair(t)
	for _, p := range []string{"/pair", "/./pair", "//pair", "/x/../pair/"} {
		rec := httptest.NewRecorder()
		a.m.Relay().ServeHTTP(rec, httptest.NewRequest("POST", "/"+b.m.ID()+p, strings.NewReader(`{"name":"x"}`)))
		if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "token") {
			t.Errorf("pairing through the relay %s: %d %s", p, rec.Code, rec.Body)
		}
	}
	rec := httptest.NewRecorder()
	a.m.Relay().ServeHTTP(rec, httptest.NewRequest("GET", "/"+b.m.ID()+"/Node-a/config", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "backup ok" {
		t.Errorf("restic through the relay: %d %s", rec.Code, rec.Body)
	}
}

// Preview and join come from anyone who reaches the port: they read only a
// small body before the join key is checked, and a real join still fits.
func TestJoinBodyIsSmallBeforeTheKey(t *testing.T) {
	a := newTestNode(t, "nas-a", map[string]string{"alex": "password-of-a"})
	if err := a.m.Create("Home"); err != nil {
		t.Fatal(err)
	}
	big := `{"key":"` + strings.Repeat("0", joinBodyLimit) + `"}`
	for _, path := range []string{"preview", "join"} {
		req := httptest.NewRequest("POST", PeerPrefix+path, strings.NewReader(big))
		req.RemoteAddr = "127.0.0.1:40000"
		rec := httptest.NewRecorder()
		a.m.PeerHandler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_json") {
			t.Errorf("%s with %d bytes: %d %s", path, len(big), rec.Code, rec.Body)
		}
	}
	b := newTestNode(t, "nas-b", map[string]string{"bob": "password-of-b"})
	if err := b.m.Join(context.Background(), a.m.JoinInfo()); err != nil {
		t.Fatalf("a real join: %v", err)
	}
}

// PLAN 76 (F-9-10): a note that an account was deleted in the cluster does
// not remove an account of the same name that only ever existed on this node.
func TestDeletedClusterAccountLeavesALocalNamesake(t *testing.T) {
	a, b := joinedPair(t)
	if _, err := b.users.Create("carol", "password-of-b", auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	if b.m.ClusterAccount("carol") {
		t.Fatal("carol counts as an account of the cluster")
	}
	gone := accountRecord{Name: "carol", Version: AccountVersion{Time: time.Now().UnixNano(), Node: a.m.ID(), Deleted: true}}
	if err := b.m.takeAccounts([]accountRecord{gone}); err != nil {
		t.Fatal(err)
	}
	if !hasAccount(b.users, "carol") {
		t.Fatal("the local account was removed")
	}
	// An account the cluster did hold here goes as before.
	gone.Name = "alex"
	if err := b.m.takeAccounts([]accountRecord{gone}); err != nil {
		t.Fatal(err)
	}
	if hasAccount(b.users, "alex") {
		t.Fatal("the account of the cluster stayed")
	}
}

func hasAccount(users *auth.Users, name string) bool {
	for _, u := range users.List() {
		if u.Name == name {
			return true
		}
	}
	return false
}
