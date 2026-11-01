package cluster

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Backups from one node to another use the receiving side of "Another
// TitanOS" (package backuptarget), carried by the cluster: the sending node
// runs a relay on 127.0.0.1:RelayPort that passes restic's requests to the
// node, signed and over the pinned connection, on whichever way reaches it.
// A backup job stores the relay as an ordinary REST target, so restic,
// the replication and the backup page need nothing new.

// RelayPort is the port of the relay on the loopback address.
const RelayPort = 8751

// BackupTarget is the receiving end of backups on this node.
type BackupTarget interface {
	PairToken(name string) (string, error)
	Handler() http.Handler
}

// ErrBackupsOff says that the other node takes no backups.
var ErrBackupsOff = errors.New("cluster: the node takes no backups of others")

var nonName = regexp.MustCompile(`[^A-Za-z0-9-]+`)

// sourceName makes the name of a source from the name of a node.
func sourceName(node string, n int) string {
	name := strings.Trim(nonName.ReplaceAllString(node, "-"), "-")
	if name == "" || !(name[0] >= 'A' && name[0] <= 'Z' || name[0] >= 'a' && name[0] <= 'z') {
		name = "Node-" + name
	}
	if len(name) > 28 {
		name = strings.TrimRight(name[:28], "-")
	}
	if n > 1 {
		name = fmt.Sprintf("%s-%d", name, n)
	}
	return name
}

type pairRequest struct {
	Name string `json:"name"`
}

type pairResponse struct {
	Source string `json:"source"`
	Token  string `json:"token"`
}

func (m *Manager) peerPair(w http.ResponseWriter, r *http.Request, id Identity) {
	var req pairRequest
	if !readBody(w, r, &req) {
		return
	}
	if m.Backups == nil {
		writeError(w, http.StatusConflict, "cluster_backups_off", ErrBackupsOff.Error())
		return
	}
	for n := 1; n <= 9; n++ {
		name := sourceName(req.Name, n)
		token, err := m.Backups.PairToken(name)
		switch {
		case err == nil:
			slog.Info("cluster: backups of a node taken", "source", name)
			writeJSON(w, http.StatusOK, pairResponse{Source: name, Token: token})
			return
		case strings.Contains(err.Error(), "name is used"):
			continue
		case strings.Contains(err.Error(), "is off"):
			writeError(w, http.StatusConflict, "cluster_backups_off", ErrBackupsOff.Error())
			return
		default:
			writeError(w, http.StatusConflict, "cluster_backups_failed", err.Error())
			return
		}
	}
	writeError(w, http.StatusConflict, "cluster_backups_failed", "no free name")
}

func (m *Manager) peerBackup(w http.ResponseWriter, r *http.Request, rest string) {
	if m.Backups == nil {
		writeError(w, http.StatusConflict, "cluster_backups_off", ErrBackupsOff.Error())
		return
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path = rest
	r2.URL.RawPath = ""
	m.Backups.Handler().ServeHTTP(w, r2)
}

// PairBackup asks node id to take backups of this node and returns the
// pairing code for the backup page, pointing at the relay.
func (m *Manager) PairBackup(ctx context.Context, id string) (string, error) {
	n, err := m.node(id)
	if err != nil {
		return "", err
	}
	name := "TitanOS"
	if m.Name != nil {
		name = m.Name()
	}
	var res pairResponse
	if err := m.call(ctx, n, "POST", PeerPrefix+"backup/pair", "", pairRequest{Name: name}, &res); err != nil {
		if IsPeerError(err, "cluster_backups_off") {
			return "", ErrBackupsOff
		}
		return "", err
	}
	payload, _ := json.Marshal(map[string]any{
		"v": 1, "url": fmt.Sprintf("http://127.0.0.1:%d/%s/%s", RelayPort, id, res.Source),
		"user": res.Source, "password": res.Token, "node": n.Name,
	})
	return "titanos-backup:" + base64.RawURLEncoding.EncodeToString(payload), nil
}

// reachable returns an address of n that answers for p, the last good
// one if there is one.
func (m *Manager) reachable(ctx context.Context, n Node, p purpose) (string, error) {
	key := p.key(n.ID)
	m.mu.Lock()
	last := m.reach[key]
	m.mu.Unlock()
	if last != "" && slices.Contains(m.ordered(n, p), last) {
		return last, nil
	}
	err := m.tryAddresses(ctx, n, m.candidates(n, p), key, func(ctx context.Context, address string) (*http.Response, error) {
		return m.sendSigned(ctx, n, address, "GET", PeerPrefix+"status", "", nil)
	}, nil)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reach[key], nil
}

// Relay passes the requests of restic to the nodes: /<node>/<rest>.
func (m *Manager) Relay() http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			n := pr.In.Context().Value(relayTarget{}).(relayNode)
			pr.Out.URL.Scheme = m.scheme()
			pr.Out.URL.Host = n.address
			pr.Out.URL.Path = PeerPrefix + "backup" + n.rest
			pr.Out.URL.RawPath = ""
			pr.Out.Host = n.address
			if err := m.sign(pr.Out, ""); err != nil {
				pr.Out.Header.Del(Header)
			}
		},
		Transport: relayTransport{m},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Info("cluster: backup relay failed", "err", err)
			http.Error(w, "node not reachable", http.StatusBadGateway)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		n, err := m.node(id)
		if err != nil {
			http.Error(w, "unknown node", http.StatusNotFound)
			return
		}
		address, err := m.reachable(r.Context(), n, forBackup)
		if err != nil {
			http.Error(w, "node not reachable", http.StatusBadGateway)
			return
		}
		ctx := context.WithValue(r.Context(), relayTarget{}, relayNode{node: n, address: address, rest: "/" + rest})
		proxy.ServeHTTP(w, r.WithContext(ctx))
	})
}

type relayTarget struct{}

type relayNode struct {
	node    Node
	address string
	rest    string
}

type relayTransport struct{ m *Manager }

func (t relayTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	n := r.Context().Value(relayTarget{}).(relayNode)
	res, err := t.m.transport(n.node.Cert).RoundTrip(r)
	if err != nil {
		// The next request looks for a way again.
		t.m.mu.Lock()
		delete(t.m.reach, forBackup.key(n.node.ID))
		t.m.mu.Unlock()
	}
	return res, err
}

// RunRelay serves the relay on the loopback address while this node is in
// a cluster.
func (m *Manager) RunRelay(ctx context.Context) {
	var (
		mu  sync.Mutex
		srv *http.Server
	)
	stop := func() {
		mu.Lock()
		defer mu.Unlock()
		if srv != nil {
			_ = srv.Close() // cleanup, best effort: nothing more can be done if it fails
			srv = nil
		}
	}
	defer stop()
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		mu.Lock()
		running := srv != nil
		mu.Unlock()
		switch in := m.InCluster(); {
		case in && !running:
			ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", RelayPort))
			if err != nil {
				slog.Warn("cluster: backup relay not started", "err", err)
				break
			}
			s := &http.Server{Handler: m.Relay(), ReadHeaderTimeout: 15 * time.Second}
			mu.Lock()
			srv = s
			mu.Unlock()
			go func() { _ = s.Serve(ln) }() // ends with Close; the error then says only that
		case !in && running:
			stop()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
