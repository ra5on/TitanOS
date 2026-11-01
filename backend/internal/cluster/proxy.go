package cluster

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"syscall"
	"time"
)

type cachedStatus struct {
	at     time.Time
	status *Status
	err    string
	// millis is the time for the answer, skew how far the clock of the
	// node is ahead of this one (seconds).
	millis int64
	skew   int64
	// restarting: the node refused the connection, titand there is
	// starting again (an app update, a restart).
	restarting bool
}

// ErrRestarting means the node refused the connection: its address answers,
// but titand there does not listen, which is what an app update or a
// restart of titand looks like for a few seconds. The web interface says
// so and tries again instead of showing an error.
var ErrRestarting = fmt.Errorf("%w: it is restarting", ErrUnreachable)

// restartRetry is the pause the web interface waits before it asks again.
const restartRetry = 3

// refused reports whether err is a refused connection.
func refused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }

// writeRestarting answers that the node is restarting, with the pause
// after which to try again.
func writeRestarting(w http.ResponseWriter) {
	w.Header().Set("Retry-After", fmt.Sprint(restartRetry))
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": map[string]any{
		"code": "cluster_restarting", "message": "the node is restarting", "retryAfter": restartRetry,
	}})
}

// writeUnreachable answers a node that did not answer: restarting or not
// reachable.
func writeUnreachable(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrRestarting) {
		writeRestarting(w)
		return
	}
	writeError(w, http.StatusBadGateway, "cluster_unreachable", "the node is not reachable")
}

// NodeView is a node in the overview.
type NodeView struct {
	Node
	Self   bool    `json:"self"`
	Online bool    `json:"online"`
	Status *Status `json:"status,omitempty"`
	// Error says why a node did not answer.
	Error string `json:"error,omitempty"`
	// Address is the way it was reached last, Way its kind and Millis the
	// time of the last answer.
	Address string `json:"address,omitempty"`
	Way     string `json:"way,omitempty"`
	Millis  int64  `json:"millis,omitempty"`
	// Preferred is the way chosen for the node when it is reached on
	// another one (Fallback), "" for automatic.
	Preferred string `json:"preferred,omitempty"`
	Fallback  bool   `json:"fallback,omitempty"`
	// Compatible is whether the node speaks the protocol of this one; else
	// only updating, looking and switching off go through.
	Compatible bool `json:"compatible"`
	// ClockSkew is how many seconds the clock of the node is ahead
	// (negative: behind), set when the comparison is on and it is off by
	// more than a minute.
	ClockSkew int64 `json:"clockSkew,omitempty"`
	// DownSince is when the node stopped answering.
	DownSince *time.Time `json:"downSince,omitempty"`
}

// statusTTL is how long the overview of a node is kept.
const statusTTL = 15 * time.Second

// clockLimit is the difference of the clocks the overview reports.
const clockLimit = 60

// Overview returns all nodes with their state, asking those whose answer
// is older than statusTTL.
func (m *Manager) Overview(ctx context.Context) []NodeView {
	m.mu.Lock()
	if m.state == nil {
		m.mu.Unlock()
		return []NodeView{}
	}
	nodes, selfID, clockOff := append([]Node(nil), m.state.Nodes...), m.self.ID, m.state.Ways.ClockOff
	m.mu.Unlock()
	views := make([]NodeView, len(nodes))
	var wg sync.WaitGroup
	for i, n := range nodes {
		views[i] = NodeView{Node: n, Self: n.ID == selfID, Compatible: true}
		if n.ID == selfID {
			if m.Status != nil {
				st := m.Status(ctx)
				st.Protocol = Protocol
				views[i].Status = &st
			}
			views[i].Online = true
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := m.peerStatus(ctx, n)
			v := &views[i]
			v.Status, v.Online = st, err == nil
			if err != nil {
				_, v.Error = ErrorCode(err)
			} else {
				v.Compatible = Compatible(st.Protocol)
			}
			m.mu.Lock()
			v.Address = m.reach[n.ID]
			c := m.status[n.ID]
			if since, ok := m.down[n.ID]; ok && err != nil {
				v.DownSince = &since
			}
			way, _ := m.routeLocked(n.ID, forManage)
			m.mu.Unlock()
			if v.Address != "" {
				v.Way = wayOf(n, v.Address)
			}
			if err == nil {
				v.Millis = c.millis
				if !clockOff && (c.skew > clockLimit || c.skew < -clockLimit) {
					v.ClockSkew = c.skew
				}
			}
			v.Preferred = way
			v.Fallback = way != WayAuto && v.Way != "" && v.Way != way
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
			if c.restarting {
				return nil, ErrRestarting
			}
			return nil, ErrUnreachable
		}
		return c.status, nil
	}
	return m.freshStatus(ctx, n)
}

// freshStatus asks node n for its status now.
func (m *Manager) freshStatus(ctx context.Context, n Node) (*Status, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var st Status
	var millis int64
	err := m.tryAddresses(ctx, n, m.candidates(n, forManage), n.ID, func(ctx context.Context, address string) (*http.Response, error) {
		start := time.Now()
		res, err := m.sendSigned(ctx, n, address, "GET", PeerPrefix+"status", "", nil)
		millis = time.Since(start).Milliseconds()
		return res, err
	}, &st)
	m.mu.Lock()
	if err != nil {
		m.status[n.ID] = cachedStatus{at: time.Now(), err: err.Error(), restarting: errors.Is(err, ErrRestarting)}
	} else {
		var skew int64
		if st.Time != 0 {
			// The node read its clock about halfway through the answer.
			skew = st.Time - time.Now().Add(-time.Duration(millis/2)*time.Millisecond).Unix()
		}
		m.status[n.ID] = cachedStatus{at: time.Now(), status: &st, millis: max(millis, 1), skew: skew}
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
		writeUnreachable(w, err)
		return
	}
	if !Compatible(st.Protocol) && !limited(r.Method, path) {
		writeError(w, http.StatusConflict, "cluster_version", "the node runs version "+st.Version)
		return
	}
	address, err := m.reachable(r.Context(), n, forManage)
	if err != nil {
		writeUnreachable(w, err)
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
		// Streams (logs, events, consoles) pass at once.
		FlushInterval: -1,
		ModifyResponse: func(res *http.Response) error {
			// The other node sets no cookie of this one.
			res.Header.Del("Set-Cookie")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// A refused connection is a node starting again (app update):
			// expected, the web interface tries again by itself.
			if refused(err) {
				slog.Info("cluster: node is restarting, the web interface tries again", "node", r.Context().Value(proxyKey{}).(proxyTarget).node.Name)
				writeRestarting(w)
				return
			}
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
