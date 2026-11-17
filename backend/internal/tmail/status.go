package tmail

import (
	"sync"
	"time"
)

// watchState is what titand knows of the mailboxes in RAM: the state of
// their permanent connection, a failed login, the unread mails of the
// INBOX and the UIDNEXT the messages count from.
type watchState struct {
	mu sync.Mutex
	// status of the watchers by key(user, id).
	status map[string]Status
	// authFailed: the fingerprint whose login the server refused.
	authFailed map[string]string
	unread     map[string]int
	baseline   map[string]baseline
	running    map[string]*watcher
}

// baseline is where new mail of an INBOX starts: UIDs from next on are
// new. It lives in RAM only: after a start of titand the UIDNEXT of the
// first connection is the line (docs/TMAIL.md).
type baseline struct {
	validity uint32
	next     uint32
}

func (w *watchState) init() {
	if w.status == nil {
		w.status, w.authFailed, w.unread = map[string]Status{}, map[string]string{}, map[string]int{}
		w.baseline, w.running = map[string]baseline{}, map[string]*watcher{}
	}
}

func (m *Manager) authFailed(user string, mb Mailbox) bool {
	m.watch.mu.Lock()
	defer m.watch.mu.Unlock()
	m.watch.init()
	return m.watch.authFailed[key(user, mb.ID)] == fingerprint(mb)
}

// setAuthFailed notes that the server refused the login: nobody tries
// again with these settings (no watcher, no connection of the web UI).
func (m *Manager) setAuthFailed(user string, mb Mailbox) {
	k := key(user, mb.ID)
	m.watch.mu.Lock()
	m.watch.init()
	m.watch.authFailed[k] = fingerprint(mb)
	w := m.watch.running[k]
	m.watch.mu.Unlock()
	if w != nil {
		w.stop()
	}
	m.events.publish(user, Event{Type: "status", Data: map[string]any{"account": mb.ID, "status": m.statusOf(user, mb)}})
}

// clearAuthFailed: a check of the connection the person asked for went
// through with these settings.
func (m *Manager) clearAuthFailed(user, id string) {
	m.watch.mu.Lock()
	m.watch.init()
	delete(m.watch.authFailed, key(user, id))
	m.watch.mu.Unlock()
}

// statusOf is the state of a mailbox for the web UI. It takes only the
// lock of the watchers (m.mu may be held).
func (m *Manager) statusOf(user string, mb Mailbox) Status {
	k := key(user, mb.ID)
	m.watch.mu.Lock()
	defer m.watch.mu.Unlock()
	m.watch.init()
	if fp, ok := m.watch.authFailed[k]; ok && fp == fingerprint(mb) {
		st := m.watch.status[k]
		if st.State != StateAuthFailed {
			st = Status{State: StateAuthFailed, Code: "mail_auth_failed", Since: time.Now().UTC()}
		}
		return st
	}
	if _, on := m.watch.running[k]; on {
		if st, ok := m.watch.status[k]; ok {
			return st
		}
		return Status{State: StateConnecting}
	}
	return Status{State: StateOff}
}

func (m *Manager) setStatus(user, id, state, code string) {
	k := key(user, id)
	m.watch.mu.Lock()
	m.watch.init()
	old := m.watch.status[k]
	changed := old.State != state || old.Code != code
	if changed {
		m.watch.status[k] = Status{State: state, Code: code, Since: m.clock().UTC()}
	}
	st := m.watch.status[k]
	m.watch.mu.Unlock()
	if changed {
		m.events.publish(user, Event{Type: "status", Data: map[string]any{"account": id, "status": st}})
	}
}

func (m *Manager) unreadOf(user, id string) int {
	m.watch.mu.Lock()
	defer m.watch.mu.Unlock()
	m.watch.init()
	return m.watch.unread[key(user, id)]
}

func (m *Manager) setUnread(user, id string, n int) {
	m.watch.mu.Lock()
	m.watch.init()
	m.watch.unread[key(user, id)] = n
	m.watch.mu.Unlock()
}
