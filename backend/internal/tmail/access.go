package tmail

import (
	"errors"
	"io/fs"
	"maps"
	"time"
)

// The app grant "T-Mail" (docs/TMAIL.md "Rechte"): off for every account,
// owners included, until an owner or administrator gives it. The grant
// belongs to the account it was given to: one made later under the same
// name has none of it, even when the deletion of the earlier one could not
// be noted (like the camera rights of the NVR).

// accessState is /config/tmail/access.json in RAM.
type accessState struct {
	// given: account → when the grant was given.
	given map[string]time.Time
	// unreadable: the file could not be read at the start; it is never
	// written over and nobody has the grant until a start reads it.
	unreadable bool
}

type accessFileData struct {
	Given map[string]time.Time `json:"given"`
}

func (m *Manager) loadAccessLocked() error {
	var data accessFileData
	err := m.store.ReadJSON(accessFile, &data)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		m.access = accessState{given: map[string]time.Time{}, unreadable: true}
		return err
	}
	if data.Given == nil {
		data.Given = map[string]time.Time{}
	}
	m.access = accessState{given: data.Given}
	return nil
}

// Allowed tells whether an account may use T-Mail now: it exists, is not
// locked and has its own grant.
func (m *Manager) Allowed(user string) bool {
	account, ok := m.lookup(user)
	if !ok || account.Disabled {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.grantedLocked(account)
}

func (m *Manager) grantedLocked(account Account) bool {
	if m.access.unreadable {
		return false
	}
	given, ok := m.access.given[account.Name]
	return ok && (account.Created.IsZero() || !account.Created.After(given))
}

// Granted tells whether an account has the grant (locked or not), for
// the page of the account.
func (m *Manager) Granted(user string) bool {
	account, ok := m.lookup(user)
	if !ok {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.grantedLocked(account)
}

// SetAllowed gives or takes the grant. The file is written only when
// something changes. Taking it ends the account's connections; its
// settings stay.
func (m *Manager) SetAllowed(user string, enabled bool) error {
	account, ok := m.lookup(user)
	if !ok {
		return ErrNotFound
	}
	m.mu.Lock()
	if m.access.unreadable {
		m.mu.Unlock()
		return ErrUnreadable
	}
	has := m.grantedLocked(account)
	if has == enabled {
		m.mu.Unlock()
		return nil
	}
	next := maps.Clone(m.access.given)
	if enabled {
		next[user] = m.clock().UTC()
	} else {
		delete(next, user)
	}
	if err := m.store.WriteJSON(accessFile, accessFileData{Given: next}); err != nil {
		m.mu.Unlock()
		return err
	}
	m.access.given = next
	m.mu.Unlock()
	if !enabled {
		m.conns.close(user, "")
		m.events.closeUser(user)
	}
	m.Sync(user)
	return nil
}

// forgetAccessLocked drops the grant of a deleted account (m.mu held).
func (m *Manager) forgetAccessLocked(user string) error {
	if m.access.unreadable {
		return ErrUnreadable
	}
	if _, ok := m.access.given[user]; !ok {
		return nil
	}
	next := maps.Clone(m.access.given)
	delete(next, user)
	if err := m.store.WriteJSON(accessFile, accessFileData{Given: next}); err != nil {
		return err
	}
	m.access.given = next
	return nil
}
