package cluster

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/config"
)

type testNode struct {
	m      *Manager
	users  *auth.Users
	srv    *httptest.Server
	store  *config.Store
	mu     sync.Mutex
	backup []string // paths the fake backup target saw
}

type fakeTarget struct{ n *testNode }

func (f fakeTarget) PairToken(name string) (string, error) { return "token-" + name, nil }
func (f fakeTarget) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.n.mu.Lock()
		f.n.backup = append(f.n.backup, r.URL.Path)
		f.n.mu.Unlock()
		io.WriteString(w, "backup ok")
	})
}

func newTestNode(t *testing.T, name string, accounts map[string]string) *testNode {
	t.Helper()
	store := config.Open(t.TempDir())
	users := auth.NewUsers(store)
	if err := users.Load(); err != nil {
		t.Fatal(err)
	}
	first := true
	for user, password := range accounts {
		var err error
		if first {
			_, err = users.CreateOwner(user, password)
			first = false
		} else {
			_, err = users.Create(user, password, auth.RoleAdmin)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	m := NewManager(store)
	n := &testNode{m: m, users: users, store: store}
	m.Users, m.Version, m.Loopback, m.Backups = users, "1.0.0", true, fakeTarget{n}
	m.Name = func() string { return name }
	m.Status = func(context.Context) Status { return Status{Name: name, Version: m.Version} }
	m.Applied = func(auth.User, bool, auth.User, bool) {}
	users.Changed = m.AccountChanged
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle(PeerPrefix, m.PeerHandler())
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		id, err := m.Verify(r)
		if err != nil {
			http.Error(w, "unsigned", http.StatusUnauthorized)
			return
		}
		io.WriteString(w, id.User+" "+r.URL.Path+" cookie="+r.Header.Get("Cookie"))
	})
	srv := httptest.NewUnstartedServer(mux)
	srv.TLS = &tls.Config{GetCertificate: func(h *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if c := m.Certificate(h); c != nil {
			return c, nil
		}
		return nil, errors.New("no certificate")
	}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	address := srv.Listener.Addr().String()
	m.Local = func() []string { return []string{address} }
	n.srv = srv
	return n
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func sameAccount(a, b *testNode, name string) bool {
	x, ok1 := a.users.Get(name)
	y, ok2 := b.users.Get(name)
	return ok1 && ok2 && accountHash(x) == accountHash(y)
}

func TestClusterJoinAccountsProxyAndRemoval(t *testing.T) {
	ctx := context.Background()
	a := newTestNode(t, "nas-a", map[string]string{"alex": "password-of-a"})
	b := newTestNode(t, "nas-b", map[string]string{"bob": "password-of-b"})
	if _, err := b.users.Create("alex", "other-password", auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	if err := a.m.Create("Home"); err != nil {
		t.Fatal(err)
	}
	info := a.m.JoinInfo()
	if !strings.HasPrefix(info, JoinPrefix) {
		t.Fatalf("join information %q", info)
	}

	// A wrong key is refused, and tells nothing.
	bad := strings.Replace(info, info[len(info)-6:], "AAAAAA", 1)
	if _, err := b.m.Preview(ctx, bad); err == nil {
		t.Fatal("preview with a damaged join information worked")
	}

	p, err := b.m.Preview(ctx, info)
	if err != nil {
		t.Fatal(err)
	}
	if p.Cluster != "Home" || strings.Join(p.Replaced, ",") != "alex" || strings.Join(p.Added, ",") != "bob" {
		t.Fatalf("preview %+v", p)
	}
	if err := b.m.Join(ctx, info); err != nil {
		t.Fatal(err)
	}
	// The cluster's alex replaced B's; bob went to A.
	if !sameAccount(a, b, "alex") {
		t.Fatal("alex of the cluster not taken over")
	}
	if u, _ := b.users.Get("alex"); u.Role != auth.RoleOwner {
		t.Fatalf("alex on B has role %s", u.Role)
	}
	eventually(t, "bob on A", func() bool { return sameAccount(a, b, "bob") })
	if len(a.m.View().Nodes) != 2 || len(b.m.View().Nodes) != 2 {
		t.Fatal("both nodes should know both")
	}

	// A change on A reaches B; the Linux number stays B's own.
	uidB, _ := b.users.Get("bob")
	if _, err := a.users.SetPassword("alex", "a-new-password"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "new password on B", func() bool { _, ok := b.users.Authenticate("alex", "a-new-password"); return ok })
	if u, _ := b.users.Get("bob"); u.UID != uidB.UID {
		t.Fatal("UID changed")
	}

	// A third node joins with the information shown on B.
	c := newTestNode(t, "nas-c", map[string]string{"carl": "password-of-c"})
	if err := c.m.Join(ctx, b.m.JoinInfo()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "C known everywhere", func() bool { return len(a.m.View().Nodes) == 3 && len(b.m.View().Nodes) == 3 })
	eventually(t, "carl on A", func() bool { return sameAccount(a, c, "carl") })

	// Removing on B reaches A and C.
	if err := b.users.Delete("carl"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "carl gone", func() bool {
		_, onA := a.users.Get("carl")
		_, onC := c.users.Get("carl")
		return !onA && !onC
	})

	// Proxy: A passes a request of alex to B, without the cookie of A.
	var bID string
	for _, n := range a.m.View().Nodes {
		if n.Name == "nas-b" {
			bID = n.ID
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/cluster/nodes/"+bID+"/api/storage?x=1", nil)
	req.Header.Set("Cookie", "titanos_session=secret")
	a.m.Proxy(rec, req, bID, "alex", "storage")
	if rec.Code != 200 || rec.Body.String() != "alex /api/v1/storage cookie=" {
		t.Fatalf("proxy: %d %q", rec.Code, rec.Body.String())
	}

	// Overview: every node online.
	for _, v := range a.m.Overview(ctx) {
		if !v.Online || v.Status == nil {
			t.Fatalf("node %s not online: %s", v.Name, v.Error)
		}
	}

	// Backups: A pairs with B and restic's requests reach B's target.
	code, err := a.m.PairBackup(ctx, bID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, "titanos-backup:"))
	var pc struct{ URL, User string }
	json.Unmarshal(raw, &pc)
	if pc.User != "nas-a" || !strings.HasPrefix(pc.URL, "http://127.0.0.1:8751/"+bID+"/nas-a") {
		t.Fatalf("pairing code %s", raw)
	}
	rec = httptest.NewRecorder()
	a.m.Relay().ServeHTTP(rec, httptest.NewRequest("GET", "/"+bID+"/nas-a/config", nil))
	if rec.Code != 200 || len(b.backup) != 1 || b.backup[0] != "/nas-a/config" {
		t.Fatalf("relay: %d %q %v", rec.Code, rec.Body.String(), b.backup)
	}

	// Removing C: C leaves, the secret changes, A and B keep talking.
	var cID string
	for _, n := range a.m.View().Nodes {
		if n.Name == "nas-c" {
			cID = n.ID
		}
	}
	oldInfo := a.m.JoinInfo()
	if err := a.m.Remove(ctx, cID); err != nil {
		t.Fatal(err)
	}
	if c.m.InCluster() {
		t.Fatal("removed node still in the cluster")
	}
	key := func(m *Manager) string { info, _ := parseJoinInfo(m.JoinInfo()); return info.Key }
	if old, _ := parseJoinInfo(oldInfo); key(b.m) == old.Key || key(b.m) != key(a.m) {
		t.Fatal("join key not replaced everywhere")
	}
	if _, err := c.m.Preview(ctx, oldInfo); err == nil {
		t.Fatal("old join information still works")
	}
	if _, ok := c.users.Get("alex"); !ok {
		t.Fatal("removed node lost its accounts")
	}
	a.m.mu.Lock()
	delete(a.m.status, bID)
	a.m.mu.Unlock()
	if _, err := a.m.peerStatus(ctx, mustNode(t, a.m, bID)); err != nil {
		t.Fatalf("A and B after the removal: %v", err)
	}

	// B leaves on its own: A removes it, both are alone.
	if err := b.m.Leave(ctx); err != nil {
		t.Fatal(err)
	}
	eventually(t, "B gone from A", func() bool { return len(a.m.View().Nodes) == 1 })
	if b.m.InCluster() {
		t.Fatal("B still in the cluster")
	}
}

func mustNode(t *testing.T, m *Manager, id string) Node {
	n, err := m.node(id)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestVerifyRefusesReplayWrongSecretAndPublicAddresses(t *testing.T) {
	a := newTestNode(t, "nas-a", map[string]string{"alex": "password-of-a"})
	b := newTestNode(t, "nas-b", map[string]string{"bob": "password-of-b"})
	if err := a.m.Create("Home"); err != nil {
		t.Fatal(err)
	}
	if err := b.m.Join(context.Background(), a.m.JoinInfo()); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/v1/storage", nil)
	req.RemoteAddr = "192.168.1.20:5000"
	if err := b.m.sign(req, "alex"); err != nil {
		t.Fatal(err)
	}
	if id, err := a.m.Verify(req); err != nil || id.User != "alex" {
		t.Fatalf("signed request refused: %v", err)
	}
	if _, err := a.m.Verify(req); err == nil {
		t.Fatal("replay accepted")
	}
	// Another path with the same signature.
	req2 := req.Clone(context.Background())
	req2.URL.Path = "/api/v1/users"
	req2.Header.Set(Header, strings.Replace(req.Header.Get(Header), "nonce=", "nonce=0", 1)[:len(req.Header.Get(Header))])
	if _, err := a.m.Verify(req2); err == nil {
		t.Fatal("changed request accepted")
	}
	// From the internet, while the cluster does not allow it.
	req3 := httptest.NewRequest("GET", "/api/v1/storage", nil)
	req3.RemoteAddr = "8.8.8.8:5000"
	b.m.sign(req3, "alex")
	if _, err := a.m.Verify(req3); err == nil {
		t.Fatal("request from the internet accepted")
	}
	// Signed by a node that does not know the secret.
	stranger := newTestNode(t, "nas-x", map[string]string{"xavier": "password-of-x"})
	stranger.m.Create("Other")
	req4 := httptest.NewRequest("GET", "/api/v1/storage", nil)
	req4.RemoteAddr = "192.168.1.30:5000"
	stranger.m.sign(req4, "alex")
	if _, err := a.m.Verify(req4); err == nil {
		t.Fatal("request of another cluster accepted")
	}
}

func TestJoinNeedsTheRightKeyAndSlowsGuessing(t *testing.T) {
	a := newTestNode(t, "nas-a", map[string]string{"alex": "password-of-a"})
	b := newTestNode(t, "nas-b", map[string]string{"bob": "password-of-b"})
	a.m.Create("Home")
	info, _ := parseJoinInfo(a.m.JoinInfo())
	info.Key = strings.Repeat("0", 64)
	data, _ := json.Marshal(info)
	wrong := JoinPrefix + base64.RawURLEncoding.EncodeToString(data)
	var last error
	for range 8 {
		last = b.m.Join(context.Background(), wrong)
	}
	if !IsPeerError(last, "rate_limited") {
		t.Fatalf("guessing not slowed down: %v", last)
	}
	if b.m.InCluster() || len(a.m.View().Nodes) != 1 {
		t.Fatal("joined with a wrong key")
	}
}

func TestNormalizeAddress(t *testing.T) {
	m := &Manager{Port: 443}
	for in, want := range map[string]string{
		"nas.example.org":          "nas.example.org:443",
		"https://nas.example.org/": "nas.example.org:443",
		"10.0.0.2:8443":            "10.0.0.2:8443",
		"fd00::2":                  "[fd00::2]:443",
	} {
		if got, err := m.NormalizeAddress(in); err != nil || got != want {
			t.Errorf("%s: %s %v", in, got, err)
		}
	}
	for _, in := range []string{"", "a b", "nas:99999", "user@nas"} {
		if _, err := m.NormalizeAddress(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestSourceName(t *testing.T) {
	for in, want := range map[string]string{"nas-a": "nas-a", "Büro NAS": "B-ro-NAS", "1st": "Node-1st", "": "Node-"} {
		if got := sourceName(in, 1); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
	if sourceName("nas", 2) != "nas-2" {
		t.Error("suffix")
	}
}

func TestVerifyChecksEveryField(t *testing.T) {
	a := newTestNode(t, "nas-a", map[string]string{"alex": "password-of-a"})
	b := newTestNode(t, "nas-b", map[string]string{"bob": "password-of-b"})
	if err := a.m.Create("Home"); err != nil {
		t.Fatal(err)
	}
	if err := b.m.Join(context.Background(), a.m.JoinInfo()); err != nil {
		t.Fatal(err)
	}
	a.m.mu.Lock()
	secret, gen := a.m.secrets.Secret, a.m.state.Generation
	a.m.mu.Unlock()
	nonce := 0
	// signed builds a request with a correct signature over the given fields.
	signed := func(remote, node string, gen int, at time.Time, fixedNonce string) *http.Request {
		req := httptest.NewRequest("GET", "/api/v1/storage", nil)
		req.RemoteAddr = remote
		n := fixedNonce
		if n == "" {
			nonce++
			n = strings.Repeat("0", 16) + fmt.Sprintf("%08x", nonce)
		}
		sig := signature(secret, req.Method, req.URL.RequestURI(), node, gen, at.Unix(), n, "alex")
		req.Header.Set(Header, fmt.Sprintf("node=%s, gen=%d, time=%d, nonce=%s, user=alex, sig=%s", node, gen, at.Unix(), n, sig))
		return req
	}
	lan, now, peer := "192.168.1.20:5000", time.Now(), b.m.ID()
	if _, err := a.m.Verify(signed(lan, peer, gen, now, "")); err != nil {
		t.Fatalf("correct request refused: %v", err)
	}
	if _, err := a.m.Verify(signed("100.101.102.103:5000", peer, gen, now, "")); err != nil {
		t.Fatalf("request over Tailscale refused: %v", err)
	}
	for name, req := range map[string]*http.Request{
		"old generation":    signed(lan, peer, gen-1, now, ""),
		"newer generation":  signed(lan, peer, gen+1, now, ""),
		"too old":           signed(lan, peer, gen, now.Add(-skew-time.Minute), ""),
		"from the future":   signed(lan, peer, gen, now.Add(skew+time.Minute), ""),
		"unknown node":      signed(lan, "ghost", gen, now, ""),
		"own node":          signed(lan, a.m.ID(), gen, now, ""),
		"public address":    signed("8.8.8.8:5000", peer, gen, now, ""),
		"short nonce":       signed(lan, peer, gen, now, "abcd"),
		"unparsable remote": signed("somewhere", peer, gen, now, ""),
	} {
		if _, err := a.m.Verify(req); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// The same nonce again, even on a newly signed request.
	reused := strings.Repeat("ab", 12)
	if _, err := a.m.Verify(signed(lan, peer, gen, now, reused)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.m.Verify(signed(lan, peer, gen, now.Add(time.Second), reused)); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("repeated nonce: %v", err)
	}
	// Loopback only in development and tests (a node of its own: the
	// test nodes above run on loopback in the background).
	plain := &Manager{}
	if plain.allowedPeer("127.0.0.1:5000", false) || plain.allowedPeer("[::1]:5000", true) {
		t.Error("loopback allowed")
	}
	if !plain.allowedPeer("8.8.8.8:5000", true) || plain.allowedPeer("8.8.8.8:5000", false) {
		t.Error("internet setting not followed")
	}
}

func TestNoncesExpireAndAreBounded(t *testing.T) {
	n := newNonces()
	start := time.Unix(1_000_000, 0)
	if !n.fresh("a/1", start) || n.fresh("a/1", start.Add(time.Minute)) {
		t.Fatal("replay within the window")
	}
	// Long after the window the old nonce is dropped from the map.
	later := start.Add(2*skew + time.Second)
	if !n.fresh("a/2", later) {
		t.Fatal("new nonce refused")
	}
	if _, kept := n.seen["a/1"]; kept {
		t.Fatal("expired nonce kept")
	}
	// A full map refuses rather than forgetting nonces that are still valid.
	for i := len(n.seen); i < maxNonces; i++ {
		n.seen[fmt.Sprint("b/", i)] = later
	}
	if n.fresh("c/1", later.Add(time.Second)) {
		t.Fatal("nonce accepted beyond the cap")
	}
	if len(n.seen) > maxNonces {
		t.Fatalf("%d nonces", len(n.seen))
	}
	// Once they expire there is room again.
	if !n.fresh("c/1", later.Add(2*skew+time.Second)) || len(n.seen) != 1 {
		t.Fatalf("after expiry: %d nonces", len(n.seen))
	}
}
