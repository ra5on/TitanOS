package cluster

import (
	"context"
	"slices"
)

// Roles in the cluster: the admin node manages all others and its accounts
// go to them. It may allow other nodes to manage as well (Node.Manage, off
// for every node that joins). A member is managed but manages nobody, and
// its own accounts stay with it.
//
// The other node checks this, not the one that asks: a request carries the
// node in its signature, and the TLS connection shows the certificate of
// that node (Verify), so a member cannot pass itself off as the admin node
// with the secret all nodes share.

// adminLocked returns the ID of the admin node. A cluster from before the
// roles, or one whose admin node left, takes the node that joined first
// (on the same second the lower ID): every node finds the same one.
// m.mu is held.
func (m *Manager) adminLocked() string {
	return adminOf(m.state)
}

// adminOf is adminLocked for state s.
func adminOf(s *State) string {
	if s == nil {
		return ""
	}
	if s.Admin != "" && slices.ContainsFunc(s.Nodes, func(n Node) bool { return n.ID == s.Admin }) {
		return s.Admin
	}
	first := ""
	var joined Node
	for _, n := range s.Nodes {
		if first == "" || n.Joined.Before(joined.Joined) || n.Joined.Equal(joined.Joined) && n.ID < first {
			first, joined = n.ID, n
		}
	}
	return first
}

// mayManageLocked reports whether node id may manage the others: the admin
// node and the nodes it allowed. m.mu is held.
func (m *Manager) mayManageLocked(id string) bool {
	if m.state == nil || id == "" {
		return false
	}
	if id == m.adminLocked() {
		return true
	}
	i := slices.IndexFunc(m.state.Nodes, func(n Node) bool { return n.ID == id })
	return i >= 0 && m.state.Nodes[i].Manage
}

// MayManage reports whether node id may manage the other nodes.
func (m *Manager) MayManage(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mayManageLocked(id)
}

// Manages reports whether this node may manage the others; outside a
// cluster there is nothing to manage.
func (m *Manager) Manages() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mayManageLocked(m.self.ID)
}

// IsAdmin reports whether this node is the admin node.
func (m *Manager) IsAdmin() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state != nil && m.adminLocked() == m.self.ID
}

// isAdmin reports whether node id is the admin node.
func (m *Manager) isAdmin(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state != nil && id != "" && m.adminLocked() == id
}

// SetManage allows node id to manage the others, or takes it back. Only
// the admin node does this, for a node other than itself.
func (m *Manager) SetManage(ctx context.Context, id string, on bool) error {
	m.mu.Lock()
	if m.state == nil {
		m.mu.Unlock()
		return ErrNoCluster
	}
	admin := m.adminLocked()
	if admin != m.self.ID {
		m.mu.Unlock()
		return ErrNotPermitted
	}
	if id == admin {
		m.mu.Unlock()
		return ErrSelf
	}
	i := slices.IndexFunc(m.state.Nodes, func(n Node) bool { return n.ID == id })
	if i < 0 {
		m.mu.Unlock()
		return ErrUnknownNode
	}
	next := cloneState(m.state)
	next.Admin = admin
	next.Nodes[i].Manage = on
	if err := m.saveLocked(next, m.secrets); err != nil {
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()
	m.announce(ctx, "")
	return nil
}

// managing returns ErrNotPermitted unless this node may manage the cluster.
func (m *Manager) managing() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		return ErrNoCluster
	}
	if !m.mayManageLocked(m.self.ID) {
		return ErrNotPermitted
	}
	return nil
}

// managersFirst orders nodes so that those that may manage come first:
// leaving asks one of them to do the removal.
func (m *Manager) managersFirst(nodes []Node) []Node {
	m.mu.Lock()
	defer m.mu.Unlock()
	slices.SortStableFunc(nodes, func(a, b Node) int {
		ma, mb := m.mayManageLocked(a.ID), m.mayManageLocked(b.ID)
		switch {
		case ma && !mb:
			return -1
		case mb && !ma:
			return 1
		}
		return 0
	})
	return nodes
}

// selfNode returns this node as the cluster lists it.
func (m *Manager) selfNode() Node {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.selfNodeLocked()
}
