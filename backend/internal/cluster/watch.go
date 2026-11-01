package cluster

import (
	"context"
	"log/slog"
	"time"
)

// watchNodes reports nodes that did not answer for longer than the
// settings allow, and when they are back. Only nodes that manage watch:
// a member would only repeat what the admin node says.
func (m *Manager) watchNodes(ctx context.Context) {
	if !m.Manages() {
		return
	}
	m.mu.Lock()
	after := m.state.Ways.alertAfter()
	m.mu.Unlock()
	now := time.Now()
	for _, n := range m.others() {
		_, err := m.freshStatus(ctx, n)
		m.mu.Lock()
		since, wasDown := m.down[n.ID]
		alerted := m.alerted[n.ID]
		switch {
		case err == nil:
			delete(m.down, n.ID)
			delete(m.alerted, n.ID)
		case !wasDown:
			m.down[n.ID] = now
		case after > 0 && !alerted && now.Sub(since) >= after:
			m.alerted[n.ID] = true
		}
		report := ""
		switch {
		case err == nil && alerted:
			report = "cluster_node_back"
		case err != nil && wasDown && after > 0 && !alerted && now.Sub(since) >= after:
			report = "cluster_node_unreachable"
		}
		m.mu.Unlock()
		if report != "" {
			slog.Info("cluster: "+report, "node", n.Name)
			if m.Notify != nil {
				m.Notify(report, n.Name)
			}
		}
	}
}
