package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/cluster"
	"github.com/ra5on/TitanOS/backend/internal/webauthn/webauthntest"
)

// clusterTestServer is a test server that is a node of a cluster. Nodes on
// one machine talk plain HTTP (development mode).
func clusterTestServer(t *testing.T, name string) (*testServer, *cluster.Manager) {
	t.Helper()
	var node *cluster.Manager
	ts := newTestServerWith(t, func(o *Options) {
		node = cluster.NewManager(o.Store)
		node.Users, node.Version, node.Backups = o.Users, o.Version, o.BackupTarget
		node.Insecure, node.Loopback = true, true
		node.Name = func() string { return name }
		if err := node.Load(); err != nil {
			t.Fatal(err)
		}
		o.Cluster = node
	})
	u, _ := url.Parse(ts.url)
	node.Local = func() []string { return []string{u.Host} }
	ts.users.Changed = node.AccountChanged
	return ts, node
}

func TestClusterThroughTheAPI(t *testing.T) {
	a, nodeA := clusterTestServer(t, "nas-a")
	b, nodeB := clusterTestServer(t, "nas-b")
	a.setup()
	b.setup()

	res, body := a.request("POST", "/api/v1/cluster", map[string]string{"name": "Home"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	// The join information is never part of the page, only shown after
	// the password.
	if strings.Contains(string(body), cluster.JoinPrefix) {
		t.Fatalf("join information without the password: %s", body)
	}
	res, body = a.request("POST", "/api/v1/cluster/join-info", map[string]string{"password": "wrong"})
	if res.StatusCode != http.StatusForbidden || errorCode(t, body) != "wrong_password" {
		t.Fatalf("join information with a wrong password: %d %s", res.StatusCode, body)
	}
	res, body = a.request("POST", "/api/v1/cluster/join-info", map[string]string{"password": testPassword})
	var view struct {
		JoinInfo string `json:"joinInfo"`
	}
	json.Unmarshal(body, &view)
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(view.JoinInfo, cluster.JoinPrefix) {
		t.Fatalf("no join information: %d %s", res.StatusCode, body)
	}
	res, body = b.request("POST", "/api/v1/cluster/preview", map[string]string{"info": view.JoinInfo})
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"replaced":["alex"]`) {
		t.Fatalf("preview: %d %s", res.StatusCode, body)
	}
	res, body = b.request("POST", "/api/v1/cluster/join", map[string]string{"info": view.JoinInfo})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("join: %d %s", res.StatusCode, body)
	}
	bID := nodeB.ID()
	if len(nodeA.View().Nodes) != 2 {
		t.Fatal("A does not know B")
	}

	// The web interface of A manages B: the account is A's session's.
	res, body = a.request("GET", "/api/v1/cluster/nodes/"+bID+"/api/users", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"name":"alex"`) {
		t.Fatalf("users of B through A: %d %s", res.StatusCode, body)
	}
	// B is a member: it manages nobody until A allows it. Joining ended
	// the session of alex on B (the cluster's alex replaced it).
	aID := nodeA.ID()
	if res, body := b.request("POST", "/api/v1/login", credentials{Name: "alex", Password: testPassword}); res.StatusCode != http.StatusOK {
		t.Fatalf("login on B: %d %s", res.StatusCode, body)
	}
	res, body = b.request("GET", "/api/v1/cluster/nodes/"+aID+"/api/users", nil)
	if res.StatusCode != http.StatusForbidden || errorCode(t, body) != "cluster_not_permitted" {
		t.Fatalf("member managed the admin node: %d %s", res.StatusCode, body)
	}
	res, body = b.request("POST", "/api/v1/cluster/join-info", map[string]string{"password": testPassword})
	if res.StatusCode != http.StatusForbidden || errorCode(t, body) != "cluster_not_permitted" {
		t.Fatalf("member showed the join information: %d %s", res.StatusCode, body)
	}
	if res, body := b.request("PUT", "/api/v1/cluster/nodes/"+bID+"/manage", map[string]bool{"manage": true}); res.StatusCode != http.StatusForbidden {
		t.Fatalf("member allowed itself: %d %s", res.StatusCode, body)
	}
	if res, body := a.request("PUT", "/api/v1/cluster/nodes/"+bID+"/manage", map[string]bool{"manage": true}); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"manage":true`) {
		t.Fatalf("allow B: %d %s", res.StatusCode, body)
	}
	if res, body := b.request("GET", "/api/v1/cluster/nodes/"+aID+"/api/users", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("allowed node not let through: %d %s", res.StatusCode, body)
	}
	if res, body := b.request("GET", "/api/v1/cluster/nodes", nil); !strings.Contains(string(body), `"manages":true`) {
		t.Fatalf("nodes of B: %d %s", res.StatusCode, body)
	}
	// Confirming network and firewall changes stays on the node itself.
	for _, path := range []string{"network/confirm", "firewall/confirm"} {
		res, body = a.request("POST", "/api/v1/cluster/nodes/"+bID+"/api/"+path, nil)
		if res.StatusCode != http.StatusForbidden || errorCode(t, body) != "cluster_local_only" {
			t.Fatalf("%s through A: %d %s", path, res.StatusCode, body)
		}
	}
	// The cluster is managed on each node, not through another.
	res, body = a.request("POST", "/api/v1/cluster/nodes/"+bID+"/api/cluster/leave", nil)
	if res.StatusCode != http.StatusForbidden || errorCode(t, body) != "web_session_required" {
		t.Fatalf("leave through A: %d %s", res.StatusCode, body)
	}
	// Without a session of A nothing goes through.
	req, _ := http.NewRequest("GET", a.url+"/api/v1/cluster/nodes/"+bID+"/api/users", nil)
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("proxy without login: %v %v", err, res)
	}
	// A forged signature does not reach B.
	req, _ = http.NewRequest("GET", b.url+"/api/v1/users", nil)
	req.Header.Set(cluster.Header, "node="+nodeA.ID()+", gen=0, time=1, nonce=000000000000000000000000, user=alex, sig=AAAA")
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("forged request: %v %v", err, res)
	}

	// An account made on A arrives on B.
	res, body = a.request("POST", "/api/v1/users", map[string]any{"name": "carol", "password": testPassword, "role": "user"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create carol: %d %s", res.StatusCode, body)
	}
	eventuallyAPI(t, func() bool { _, ok := b.users.Get("carol"); return ok })

	// The overview shows both nodes online.
	res, body = a.request("GET", "/api/v1/cluster/nodes", nil)
	if res.StatusCode != http.StatusOK || strings.Count(string(body), `"online":true`) != 2 {
		t.Fatalf("nodes: %d %s", res.StatusCode, body)
	}
}

func eventuallyAPI(t *testing.T, ok func() bool) {
	t.Helper()
	for range 250 {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out")
}

// TestClusterJoinInfoWithPasskey: a passkey of the account shows the join
// information instead of the password; its challenge registers nothing.
func TestClusterJoinInfoWithPasskey(t *testing.T) {
	ts, _ := clusterTestServer(t, "nas-a")
	ts.url = strings.Replace(ts.url, "127.0.0.1", "localhost", 1)
	origin := strings.ToLower(ts.url)
	ts.client = ts.newClient()
	ts.setup()
	if res, body := ts.request("POST", "/api/v1/cluster", map[string]string{"name": "Home"}); res.StatusCode != http.StatusOK {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/account/passkeys/confirm", nil); res.StatusCode != http.StatusConflict || errorCode(t, body) != "no_passkey" {
		t.Fatalf("confirm without a passkey: %d %s", res.StatusCode, body)
	}

	_, body := ts.request("POST", "/api/v1/account/passkeys/begin", map[string]string{"password": testPassword})
	var begun struct {
		ChallengeID string `json:"challengeId"`
		PublicKey   struct {
			Challenge string
			User      struct{ ID string }
		} `json:"publicKey"`
	}
	json.Unmarshal(body, &begun)
	key := webauthntest.New()
	key.Handle, _ = decodeB64(begun.PublicKey.User.ID)
	cd, att := key.Create(begun.PublicKey.Challenge, origin, "localhost")
	if res, body := ts.request("POST", "/api/v1/account/passkeys", map[string]string{"challengeId": begun.ChallengeID, "name": "Laptop", "clientDataJSON": cd, "attestationObject": att}); res.StatusCode != http.StatusOK {
		t.Fatalf("add passkey: %d %s", res.StatusCode, body)
	}

	type challenge struct {
		ChallengeID string `json:"challengeId"`
		PublicKey   struct {
			Challenge        string
			AllowCredentials []struct{ ID string } `json:"allowCredentials"`
		} `json:"publicKey"`
	}
	confirm := func() challenge {
		t.Helper()
		res, body := ts.request("POST", "/api/v1/account/passkeys/confirm", nil)
		var c challenge
		if res.StatusCode != http.StatusOK || json.Unmarshal(body, &c) != nil || len(c.PublicKey.AllowCredentials) != 1 {
			t.Fatalf("confirm: %d %s", res.StatusCode, body)
		}
		return c
	}
	// A confirmation challenge never registers a passkey: whoever holds
	// only the session must not add one without the password.
	c := confirm()
	other := webauthntest.New()
	other.Handle = key.Handle
	cd, att = other.Create(c.PublicKey.Challenge, origin, "localhost")
	if res, body := ts.request("POST", "/api/v1/account/passkeys", map[string]string{"challengeId": c.ChallengeID, "clientDataJSON": cd, "attestationObject": att}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("registered with a confirmation challenge: %d %s", res.StatusCode, body)
	}

	reveal := func(tamper bool) (*http.Response, []byte) {
		c := confirm()
		ch := c.PublicKey.Challenge
		if tamper {
			ch = strings.Repeat("A", len(ch))
		}
		id, cd, ad, sig, handle := key.Get(ch, origin, "localhost")
		return ts.request("POST", "/api/v1/cluster/join-info", map[string]any{"passkey": map[string]string{
			"challengeId": c.ChallengeID, "id": id, "clientDataJSON": cd, "authenticatorData": ad, "signature": sig, "userHandle": handle,
		}})
	}
	if res, body := reveal(true); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "passkey_failed" {
		t.Fatalf("tampered passkey: %d %s", res.StatusCode, body)
	}
	res, body := reveal(false)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), cluster.JoinPrefix) {
		t.Fatalf("join information with the passkey: %d %s", res.StatusCode, body)
	}
}
