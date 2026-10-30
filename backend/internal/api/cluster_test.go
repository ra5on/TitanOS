package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/cluster"
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
	var view struct {
		Cluster cluster.View `json:"cluster"`
	}
	json.Unmarshal(body, &view)
	if !strings.HasPrefix(view.Cluster.JoinInfo, cluster.JoinPrefix) {
		t.Fatalf("no join information: %s", body)
	}
	res, body = b.request("POST", "/api/v1/cluster/preview", map[string]string{"info": view.Cluster.JoinInfo})
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"replaced":["alex"]`) {
		t.Fatalf("preview: %d %s", res.StatusCode, body)
	}
	res, body = b.request("POST", "/api/v1/cluster/join", map[string]string{"info": view.Cluster.JoinInfo})
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
