package store

import (
	"errors"
	"io/fs"
	"log/slog"
)

// ForgetUser removes everything the store keeps of an account (TECHNIK
// 5.7): RAM first (state, locks, parsed entries), its context is cancelled
// so that running DAV requests end, then .titanos/calendar/<name> and
// .titanos/contacts/<name> (opened without creating anything). Without a
// volume the folders stay: owner.json names their account with its
// creation time, so a new account of the name neither reads them nor keeps
// them (they go with its first write). It runs when an account is deleted
// here or by another node, and when one is made.
func (s *Store) ForgetUser(name string) error {
	if !accountRE.MatchString(name) {
		return ErrInvalidAccount
	}
	s.mu.Lock()
	if s.forgetting == nil {
		s.forgetting = map[string]int{}
	}
	s.forgetting[name]++
	if c := s.ctxs[name]; c != nil {
		c.cancel()
		delete(s.ctxs, name)
	}
	var old []*account
	for _, fc := range s.features {
		if acc := fc.accounts[name]; acc != nil {
			old = append(old, acc)
			delete(fc.accounts, name)
		}
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.forgetting[name]--; s.forgetting[name] <= 0 {
			delete(s.forgetting, name)
		}
		s.mu.Unlock()
	}()
	s.parsed.dropAccount(name)
	s.forgetMarks(name)
	// Writes that run end first; later ones on these states fail.
	for _, acc := range old {
		acc.gate.Lock()
		acc.gone = true
		acc.gate.Unlock()
	}
	for _, kind := range []Kind{Calendar, Contacts} {
		root, release, err := s.open(kind, false)
		if errors.Is(err, fs.ErrNotExist) {
			continue // nothing kept on this volume
		}
		if err != nil {
			slog.Info("Calendar: folder of a deleted account stays until its volume is there; a new account of the name does not see it", "kind", kind, "account", name, "err", err)
			continue
		}
		if err := root.RemoveAll(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("Calendar: folder of a deleted account not removed", "kind", kind, "account", name, "err", err)
		}
		release()
	}
	return nil
}
