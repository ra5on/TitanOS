package cluster

import (
	"context"
	"slices"
)

// HandOver makes node id the admin node. The admin node hands over to any
// node; a node that may manage takes the role itself only while the admin
// node does not answer (it is gone for good, or for long). The old admin
// node keeps managing. The higher AdminGen wins on every node, also on
// the old admin node when it comes back.
func (m *Manager) HandOver(ctx context.Context, id string) error {
	if err := m.managing(); err != nil {
		return err
	}
	m.mu.Lock()
	if m.state == nil {
		m.mu.Unlock()
		return ErrNoCluster
	}
	admin := m.adminLocked()
	self := m.self.ID
	known := slices.ContainsFunc(m.state.Nodes, func(n Node) bool { return n.ID == id })
	m.mu.Unlock()
	switch {
	case !known:
		return ErrUnknownNode
	case id == admin:
		return ErrSelf
	case admin == self:
	case id != self:
		// Only the admin node gives the role to a third node.
		return ErrNotPermitted
	default:
		n, err := m.node(admin)
		if err != nil {
			return err
		}
		if _, err := m.freshStatus(ctx, n); err == nil {
			// The admin node is there: it hands over itself.
			return ErrNotPermitted
		}
	}
	return m.changeState(ctx, func(s *State) {
		s.AdminGen++
		s.Admin = id
		for i := range s.Nodes {
			switch s.Nodes[i].ID {
			case admin:
				s.Nodes[i].Manage = true
			case id:
				s.Nodes[i].Manage = false
			}
		}
	})
}
