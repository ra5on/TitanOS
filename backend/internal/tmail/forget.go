package tmail

import (
	"errors"
	"log/slog"
	"strings"
)

// ForgetUser removes everything T-Mail keeps of an account (docs/TMAIL.md
// "Konto löschen"): its connections and event streams first, then the
// settings with the passwords, the grant, the address book and what RAM
// holds. It runs when an account is deleted here or by another node of the
// cluster, and when an account is made (a deletion that could not reach
// the volume must not leave anything to a new account of the same name).
// Errors are logged; a file that stays names its owner and is ignored by
// a later account of that name.
func (m *Manager) ForgetUser(name string) error {
	if !accountRE.MatchString(name) {
		return ErrInvalid
	}
	// Connections and streams first: nothing may write after the files go.
	m.watch.mu.Lock()
	m.watch.init()
	var stop []*watcher
	for k, w := range m.watch.running {
		if w.user == name {
			stop = append(stop, w)
			delete(m.watch.running, k)
		}
	}
	for _, table := range []map[string]Status{m.watch.status} {
		for k := range table {
			if strings.HasPrefix(k, name+"\x00") {
				delete(table, k)
			}
		}
	}
	for k := range m.watch.authFailed {
		if strings.HasPrefix(k, name+"\x00") {
			delete(m.watch.authFailed, k)
		}
	}
	for k := range m.watch.unread {
		if strings.HasPrefix(k, name+"\x00") {
			delete(m.watch.unread, k)
		}
	}
	for k := range m.watch.baseline {
		if strings.HasPrefix(k, name+"\x00") {
			delete(m.watch.baseline, k)
		}
	}
	for k := range m.watch.rounds {
		if strings.HasPrefix(k, name+"\x00") {
			delete(m.watch.rounds, k)
		}
	}
	m.watch.mu.Unlock()
	for _, w := range stop {
		w.stop()
		<-w.done
	}
	m.conns.close(name, "")
	m.events.closeUser(name)
	m.forgetTargets(name)
	m.notices.mu.Lock()
	for k := range m.notices.pending {
		if strings.HasPrefix(k, name+"\x00") {
			delete(m.notices.pending, k)
		}
	}
	m.notices.mu.Unlock()

	var errs []error
	m.mu.Lock()
	_, had := m.users[name]
	delete(m.users, name)
	if had || m.unreadable[name] {
		if err := m.store.Remove(userPath(name)); err != nil {
			errs = append(errs, err)
		}
	}
	delete(m.unreadable, name)
	if err := m.forgetAccessLocked(name); err != nil {
		errs = append(errs, err)
	}
	m.mu.Unlock()
	if !m.forgetContacts(name) {
		slog.Info("T-Mail: address book of a deleted account stays until its volume is there; a new account of the name does not see it", "account", name)
	}
	err := errors.Join(errs...)
	if err != nil {
		slog.Warn("T-Mail settings of a deleted account not removed", "account", name, "err", err)
	}
	return err
}
