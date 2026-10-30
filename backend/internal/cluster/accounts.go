package cluster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// The accounts of the admin node go to every node. A change there gets a
// new version (time and node) and goes to all others; a node takes over a
// change when its version is newer than the one it knows. Every minute the
// admin node compares the versions, so that a node that was away catches
// up. The own accounts of the other nodes stay with them.

// accountHash identifies the content of an account, without the Linux user
// number, which every node keeps for itself.
func accountHash(user auth.User) string {
	user.UID = 0
	data, _ := json.Marshal(user)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// accountRecord is an account sent to another node.
type accountRecord struct {
	Name    string         `json:"name"`
	Version AccountVersion `json:"version"`
	User    *auth.User     `json:"user,omitempty"` // nil when removed
}

// AccountChanged is called for every account written or removed on this
// node (auth.Users.Changed).
func (m *Manager) AccountChanged(name string) {
	if !m.IsAdmin() {
		return
	}
	if m.noteLocal(name) {
		m.pushAccount(context.Background(), name)
	}
}

// noteLocal gives the account a new version if it differs from what the
// cluster knows of it. It reports whether it did.
func (m *Manager) noteLocal(name string) bool {
	m.pushing.Lock()
	defer m.pushing.Unlock()
	user, exists := m.Users.Get(name)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		return false
	}
	known, had := m.state.Accounts[name]
	switch {
	case exists && had && !known.Deleted && known.Hash == accountHash(user):
		return false
	case !exists && (!had || known.Deleted):
		return false
	}
	v := AccountVersion{Time: time.Now().UnixNano(), Node: m.self.ID}
	if exists {
		v.Hash = accountHash(user)
	} else {
		v.Deleted = true
	}
	// A clock behind the last change must not lose this one.
	if had && !v.newer(known) {
		v.Time = known.Time + 1
	}
	next := cloneState(m.state)
	next.Accounts[name] = v
	if err := m.saveLocked(next, m.secrets); err != nil {
		slog.Error("cluster: version of the account not saved", "user", name, "err", err)
		return false
	}
	return true
}

// record returns the account as it goes to another node.
func (m *Manager) record(name string) (accountRecord, bool) {
	m.mu.Lock()
	v, ok := m.state.Accounts[name]
	m.mu.Unlock()
	if !ok {
		return accountRecord{}, false
	}
	rec := accountRecord{Name: name, Version: v}
	if !v.Deleted {
		user, exists := m.Users.Get(name)
		if !exists {
			return accountRecord{}, false
		}
		rec.User = &user
	}
	return rec, true
}

// pushAccount sends one account to all other nodes.
func (m *Manager) pushAccount(ctx context.Context, name string) {
	rec, ok := m.record(name)
	if !ok {
		return
	}
	for _, n := range m.others() {
		if err := m.call(ctx, n, "POST", "/api/v1/cluster/peer/accounts", "", []accountRecord{rec}, nil); err != nil {
			slog.Info("cluster: account not passed on yet", "user", name, "node", n.Name, "err", err)
		}
	}
}

// takeAccounts takes over the accounts another node sent that are newer
// than those here.
func (m *Manager) takeAccounts(records []accountRecord) error {
	m.pushing.Lock()
	defer m.pushing.Unlock()
	for _, rec := range records {
		m.mu.Lock()
		if m.state == nil {
			m.mu.Unlock()
			return ErrNoCluster
		}
		known, had := m.state.Accounts[rec.Name]
		m.mu.Unlock()
		if had && !rec.Version.newer(known) {
			continue
		}
		if rec.Version.Deleted {
			before, existed, err := m.Users.Forget(rec.Name)
			if err != nil {
				slog.Error("cluster: account not removed", "user", rec.Name, "err", err)
				continue
			}
			if existed && m.Applied != nil {
				m.Applied(before, true, auth.User{}, true)
			}
		} else {
			if rec.User == nil || rec.User.Name != rec.Name || accountHash(*rec.User) != rec.Version.Hash {
				continue
			}
			m.importAccount(*rec.User)
		}
		m.mu.Lock()
		if m.state != nil {
			next := cloneState(m.state)
			next.Accounts[rec.Name] = rec.Version
			if err := m.saveLocked(next, m.secrets); err != nil {
				slog.Error("cluster: version of the account not saved", "user", rec.Name, "err", err)
			}
		}
		m.mu.Unlock()
	}
	return nil
}

// versions returns the versions of all accounts this node knows.
func (m *Manager) versions() map[string]AccountVersion {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]AccountVersion{}
	if m.state != nil {
		for k, v := range m.state.Accounts {
			out[k] = v
		}
	}
	return out
}

// syncAccounts notes changes made here while the cluster did not look (an
// older titand, the local console), then sends every node the accounts it
// is behind on.
func (m *Manager) syncAccounts(ctx context.Context) {
	if !m.IsAdmin() {
		return
	}
	names := map[string]bool{}
	for _, u := range m.Users.List() {
		names[u.Name] = true
	}
	for name := range m.versions() {
		names[name] = true
	}
	for name := range names {
		m.noteLocal(name)
	}
	mine := m.versions()
	for _, n := range m.others() {
		var theirs map[string]AccountVersion
		if err := m.call(ctx, n, "GET", "/api/v1/cluster/peer/accounts", "", nil, &theirs); err != nil {
			continue
		}
		var send []accountRecord
		for name, v := range mine {
			if t, ok := theirs[name]; !ok || v.newer(t) {
				if rec, ok := m.record(name); ok {
					send = append(send, rec)
				}
			}
		}
		if len(send) > 0 {
			if err := m.call(ctx, n, "POST", "/api/v1/cluster/peer/accounts", "", send, nil); err != nil {
				slog.Info("cluster: accounts not passed on yet", "node", n.Name, "err", err)
			}
		}
	}
}

// clusterAccounts returns the accounts of this node that the cluster has:
// on the admin node all of them, elsewhere those that came from it. state
// nil means the current one.
func (m *Manager) clusterAccounts(state *State) []auth.User {
	m.mu.Lock()
	if state == nil {
		state = m.state
	}
	admin := state != nil && adminOf(state) == m.self.ID
	known := map[string]bool{}
	if state != nil {
		for name, v := range state.Accounts {
			known[name] = !v.Deleted
		}
	}
	m.mu.Unlock()
	var out []auth.User
	for _, u := range m.Users.List() {
		if admin || known[u.Name] {
			out = append(out, u)
		}
	}
	return out
}
