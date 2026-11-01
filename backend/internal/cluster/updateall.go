package cluster

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// "Update all nodes": the managing node looks for a release on each chosen
// node and installs it there, one after the other (waiting until the node
// answers with the new version, stopping at the first failure) or all at
// once. This node comes last: its update restarts titand, which ends the
// run. The run lives in RAM like the update itself.

// UpdateRun is the last run.
type UpdateRun struct {
	Parallel bool         `json:"parallel"`
	Running  bool         `json:"running"`
	Started  time.Time    `json:"started"`
	Nodes    []UpdateNode `json:"nodes"`
}

// UpdateNode is one node of a run. State is waiting, checking, installing,
// restarting, done, current (no newer release), failed or skipped (after a
// failure in order).
type UpdateNode struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

// updateWait is how long a node may take for its update, updatePoll how
// often it is asked meanwhile.
var (
	updateWait = 20 * time.Minute
	updatePoll = 5 * time.Second
)

var ErrRunning = errors.New("cluster: an update of the nodes is running")

// remoteUpdate is what the update page of a node answers.
type remoteUpdate struct {
	Version   string `json:"version"`
	Available *struct {
		Version string `json:"version"`
	} `json:"available"`
}

// UpdateRunState returns the last run, nil if there was none.
func (m *Manager) UpdateRunState() *UpdateRun {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.run == nil {
		return nil
	}
	r := *m.run
	r.Nodes = slices.Clone(m.run.Nodes)
	return &r
}

// UpdateAll starts a run for the nodes ids on behalf of user.
func (m *Manager) UpdateAll(ctx context.Context, user string, ids []string, parallel bool) error {
	if err := m.managing(); err != nil {
		return err
	}
	if len(ids) == 0 || len(ids) > MaxNodes {
		return fmt.Errorf("%w: nodes", ErrInvalid)
	}
	m.mu.Lock()
	if m.run != nil && m.run.Running {
		m.mu.Unlock()
		return ErrRunning
	}
	if m.state == nil {
		m.mu.Unlock()
		return ErrNoCluster
	}
	run := &UpdateRun{Parallel: parallel, Running: true, Started: time.Now().UTC().Truncate(time.Second)}
	var self *UpdateNode
	for _, n := range m.state.Nodes {
		if !slices.Contains(ids, n.ID) {
			continue
		}
		u := UpdateNode{ID: n.ID, Name: n.Name, State: "waiting"}
		if n.ID == m.self.ID {
			self = &u
			continue
		}
		run.Nodes = append(run.Nodes, u)
	}
	if self != nil {
		run.Nodes = append(run.Nodes, *self)
	}
	if len(run.Nodes) == 0 {
		m.mu.Unlock()
		return fmt.Errorf("%w: nodes", ErrInvalid)
	}
	m.run = run
	m.mu.Unlock()
	slog.Info("cluster: updating nodes", "nodes", len(run.Nodes), "parallel", parallel, "by", user)
	go m.updateRun(context.WithoutCancel(ctx), user)
	return nil
}

func (m *Manager) setRunNode(i int, change func(*UpdateNode)) {
	m.mu.Lock()
	change(&m.run.Nodes[i])
	m.mu.Unlock()
}

func (m *Manager) updateRun(ctx context.Context, user string) {
	m.mu.Lock()
	nodes := slices.Clone(m.run.Nodes)
	parallel := m.run.Parallel
	m.mu.Unlock()
	last := len(nodes) - 1
	selfLast := nodes[last].ID == m.ID()
	others := nodes
	if selfLast {
		others = nodes[:last]
	}
	failed := false
	if parallel {
		var wg sync.WaitGroup
		for i := range others {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if !m.updateOne(ctx, i, user) {
					m.mu.Lock()
					failed = true
					m.mu.Unlock()
				}
			}()
		}
		wg.Wait()
	} else {
		for i := range others {
			if failed {
				m.setRunNode(i, func(u *UpdateNode) { u.State = "skipped" })
				continue
			}
			failed = !m.updateOne(ctx, i, user)
		}
	}
	m.mu.Lock()
	stop := failed && !parallel
	m.mu.Unlock()
	if selfLast {
		if stop {
			m.setRunNode(last, func(u *UpdateNode) { u.State = "skipped" })
		} else {
			m.updateSelf(ctx, last)
		}
	}
	m.mu.Lock()
	m.run.Running = false
	m.mu.Unlock()
}

// updateOne updates node i of the run and waits for it; it reports
// whether that went well (also when there was nothing to do).
func (m *Manager) updateOne(ctx context.Context, i int, user string) bool {
	m.mu.Lock()
	id := m.run.Nodes[i].ID
	m.mu.Unlock()
	fail := func(err error) bool {
		m.setRunNode(i, func(u *UpdateNode) { u.State, u.Error = "failed", err.Error() })
		slog.Warn("cluster: node not updated", "node", id, "err", err)
		return false
	}
	n, err := m.node(id)
	if err != nil {
		return fail(err)
	}
	m.setRunNode(i, func(u *UpdateNode) { u.State = "checking" })
	var st remoteUpdate
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	err = m.call(cctx, n, "POST", "/api/v1/update/check", user, struct{}{}, &st)
	cancel()
	if err != nil {
		return fail(err)
	}
	if st.Available == nil || st.Available.Version == "" || st.Available.Version == st.Version {
		m.setRunNode(i, func(u *UpdateNode) { u.State, u.From = "current", st.Version })
		return true
	}
	to := st.Available.Version
	m.setRunNode(i, func(u *UpdateNode) { u.State, u.From, u.To = "installing", st.Version, to })
	cctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
	err = m.call(cctx, n, "POST", "/api/v1/update/install", user, struct{}{}, nil)
	cancel()
	if err != nil {
		return fail(err)
	}
	// The node downloads, then restarts titand (app update) or itself
	// (system update); it is done when it answers with the new version.
	deadline := time.Now().Add(updateWait)
	seenDown := false
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return fail(ctx.Err())
		case <-time.After(updatePoll):
		}
		status, err := m.freshStatus(ctx, n)
		if err != nil {
			if !seenDown {
				seenDown = true
				m.setRunNode(i, func(u *UpdateNode) { u.State = "restarting" })
			}
			continue
		}
		if status.Version == to {
			m.setRunNode(i, func(u *UpdateNode) { u.State = "done" })
			slog.Info("cluster: node updated", "node", n.Name, "version", to)
			return true
		}
	}
	return fail(fmt.Errorf("no answer with version %s after %s", to, updateWait))
}

// updateSelf installs the update on this node last.
func (m *Manager) updateSelf(ctx context.Context, i int) {
	if m.Update == nil {
		m.setRunNode(i, func(u *UpdateNode) { u.State, u.Error = "failed", "not available" })
		return
	}
	m.setRunNode(i, func(u *UpdateNode) { u.State = "checking" })
	from, to, err := m.Update(ctx)
	switch {
	case err != nil:
		m.setRunNode(i, func(u *UpdateNode) { u.State, u.From, u.Error = "failed", from, err.Error() })
	case to == "":
		m.setRunNode(i, func(u *UpdateNode) { u.State, u.From = "current", from })
	default:
		// titand restarts in a moment; the page sees this node come back.
		m.setRunNode(i, func(u *UpdateNode) { u.State, u.From, u.To = "restarting", from, to })
	}
}
