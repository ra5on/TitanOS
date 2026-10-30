package cluster

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"time"
)

type cachedStatus struct {
	at     time.Time
	status *Status
	err    string
}

// NodeView is a node in the overview.
type NodeView struct {
	Node
	Self   bool    `json:"self"`
	Online bool    `json:"online"`
	Status *Status `json:"status,omitempty"`
	// Error says why a node did not answer.
	Error string `json:"error,omitempty"`
	// Address is the way it was reached last.
	Address string `json:"address,omitempty"`
}

// statusTTL is how long the overview of a node is kept.
const statusTTL = 15 * time.Second

// Overview returns all nodes with their state, asking those whose answer
// is older than statusTTL.
func (m *Manager) Overview(ctx context.Context) []NodeView {
	m.mu.Lock()
	if m.state == nil {
		m.mu.Unlock()
		return []NodeView{}
	}
	nodes, selfID := append([]Node(nil), m.state.Nodes...), m.self.ID
	m.mu.Unlock()
	views := make([]NodeView, len(nodes))
	var wg sync.WaitGroup
	for i, n := range nodes {
		views[i] = NodeView{Node: n, Self: n.ID == selfID}
		if n.ID == selfID {
			if m.Status != nil {
				st := m.Status(ctx)
				views[i].Status = &st
			}
			views[i].Online = true
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := m.peerStatus(ctx, n)
			views[i].Status, views[i].Online = st, err == nil
			if err != nil {
				_, views[i].Error = ErrorCode(err)
			}
			m.mu.Lock()
			views[i].Address = m.reach[n.ID]
			m.mu.Unlock()
		}()
	}
	wg.Wait()
	return views
}

func (m *Manager) peerStatus(ctx context.Context, n Node) (*Status, error) {
	m.mu.Lock()
	c, ok := m.status[n.ID]
	m.mu.Unlock()
	if ok && time.Since(c.at) < statusTTL {
		if c.status == nil {
			return nil, ErrUnreachable
		}
		return c.status, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var st Status
	err := m.call(ctx, n, "GET", PeerPrefix+"status", "", nil, &st)
	m.mu.Lock()
	if err != nil {
		m.status[n.ID] = cachedStatus{at: time.Now(), err: err.Error()}
	} else {
		m.status[n.ID] = cachedStatus{at: time.Now(), status: &st}
	}
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// proxyTarget travels with a request that goes to another node.
type proxyTarget struct {
	node    Node
	address string
	path    string
	user    string
}

type proxyKey struct{}

// Proxy passes a request of the web interface to node id, on behalf of
// user: path is the part after /api/v1. The other node checks the account
// and its rights itself. Consoles and other WebSockets go through as well.
func (m *Manager) Proxy(w http.ResponseWriter, r *http.Request, id, user, path string) {
	if !m.Manages() {
		writeError(w, http.StatusForbidden, "cluster_not_permitted", "this node may not manage the others")
		return
	}
	n, err := m.node(id)
	if err != nil {
		status, code := ErrorCode(err)
		writeError(w, status, code, err.Error())
		return
	}
	st, err := m.peerStatus(r.Context(), n)
	if err != nil {
		writeError(w, http.StatusBadGateway, "cluster_unreachable", "the node is not reachable")
		return
	}
	if st.Version != m.Version {
		writeError(w, http.StatusConflict, "cluster_version", "the node runs version "+st.Version)
		return
	}
	address, err := m.reachable(r.Context(), n)
	if err != nil {
		writeError(w, http.StatusBadGateway, "cluster_unreachable", "the node is not reachable")
		return
	}
	ctx := context.WithValue(r.Context(), proxyKey{}, proxyTarget{node: n, address: address, path: "/api/v1/" + strings.TrimPrefix(path, "/"), user: user})
	m.proxy.ServeHTTP(w, r.WithContext(ctx))
}

// browserHeaders are headers of the browser that mean nothing to the other
// node or would let it take the request for a cross-site one.
var browserHeaders = []string{
	"Cookie", "Authorization", "Origin", "Referer", "Sec-Fetch-Site", "Sec-Fetch-Mode",
	"Sec-Fetch-Dest", "Sec-Fetch-User", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Forwarded",
}

func (m *Manager) newProxy() *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			t := pr.In.Context().Value(proxyKey{}).(proxyTarget)
			for _, h := range browserHeaders {
				pr.Out.Header.Del(h)
			}
			pr.Out.URL.Scheme = m.scheme()
			pr.Out.URL.Host = t.address
			pr.Out.URL.Path = t.path
			pr.Out.URL.RawPath = ""
			pr.Out.URL.RawQuery = pr.In.URL.RawQuery
			pr.Out.Host = t.address
			if err := m.sign(pr.Out, t.user); err != nil {
				pr.Out.Header.Del(Header)
			}
		},
		Transport: proxyTransport{m},
		// Streams (logs, the assistant, consoles) pass at once.
		FlushInterval: -1,
		ModifyResponse: func(res *http.Response) error {
			// The other node sets no cookie of this one.
			res.Header.Del("Set-Cookie")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Info("cluster: request to node failed", "err", err)
			writeError(w, http.StatusBadGateway, "cluster_unreachable", "the node is not reachable")
		},
	}
}

type proxyTransport struct{ m *Manager }

func (t proxyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	target := r.Context().Value(proxyKey{}).(proxyTarget)
	res, err := t.m.transport(target.node.Cert).RoundTrip(r)
	if err != nil {
		t.m.mu.Lock()
		delete(t.m.reach, target.node.ID)
		delete(t.m.status, target.node.ID)
		t.m.mu.Unlock()
	}
	return res, err
}
