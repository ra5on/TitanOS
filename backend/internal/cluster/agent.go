package cluster

import (
	"net/http"
	"strings"
)

// The KI-Agent across the cluster (PLAN 47): a node may run its own agent
// or use the model of the admin node. Its requests go to the admin node as
// signed cluster requests on /agent/...; the admin node decides there
// (Manager.Agent) whether the node was released.

// AdminNode returns the admin node of the cluster; ErrSelf on the admin
// node itself, ErrNoCluster outside a cluster.
func (m *Manager) AdminNode() (Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		return Node{}, ErrNoCluster
	}
	admin := m.adminLocked()
	if admin == m.self.ID {
		return Node{}, ErrSelf
	}
	for _, n := range m.state.Nodes {
		if n.ID == admin {
			return n, nil
		}
	}
	return Node{}, ErrUnknownNode
}

// NodeName returns the name of member id, "" if it is unknown.
func (m *Manager) NodeName(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		return ""
	}
	for _, n := range m.state.Nodes {
		if n.ID == id {
			return n.Name
		}
	}
	return ""
}

// AgentTransport sends requests to the agent end of the admin node: a
// request for /x goes to /agent/x there, signed and over the pinned
// connection. The answer streams (a chat of the model).
func (m *Manager) AgentTransport() http.RoundTripper { return agentTransport{m} }

type agentTransport struct{ m *Manager }

func (t agentTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	n, err := t.m.AdminNode()
	if err != nil {
		return nil, err
	}
	address, err := t.m.reachable(r.Context(), n, forManage)
	if err != nil {
		return nil, err
	}
	out := r.Clone(r.Context())
	out.URL.Scheme = t.m.scheme()
	out.URL.Host = address
	out.URL.Path = PeerPrefix + "agent/" + strings.TrimPrefix(r.URL.Path, "/")
	out.URL.RawPath = ""
	out.Host = address
	out.RequestURI = ""
	if err := t.m.sign(out, ""); err != nil {
		return nil, err
	}
	res, err := t.m.transport(n.Cert).RoundTrip(out)
	if err != nil {
		// The next request looks for a way again.
		t.m.mu.Lock()
		delete(t.m.reach, forManage.key(n.ID))
		t.m.mu.Unlock()
	}
	return res, err
}

// servePeerAgent hands a signed request for /agent/... to the agent end of
// this node, the path without the prefix.
func (m *Manager) servePeerAgent(w http.ResponseWriter, r *http.Request, rest string) {
	if m.Agent == nil {
		writeError(w, http.StatusNotFound, "not_found", "no such route")
		return
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/" + strings.TrimPrefix(rest, "/")
	r2.URL.RawPath = ""
	m.Agent.ServeHTTP(w, r2)
}
