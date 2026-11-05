package agent

import (
	"errors"
	"maps"
	"regexp"
	"time"
)

// Read state of the chats (PLAN 48): when a person last looked at a chat,
// on any of their devices. The list shows a dot for unread chats and push
// messages stay away for chats already seen. Kept in a file of its own so
// that marking a chat never races with a run that saves the chat.

const (
	seenFile    = "seen.json"
	maxSeenFile = 1 << 20
)

type seenState struct {
	// Since is when read states began; older chats without an entry count
	// as read, so an update does not mark every old chat unread.
	Since time.Time            `json:"since"`
	Chats map[string]time.Time `json:"chats"`
}

// seenLocked reads the read state; s.mu is held. A missing file starts
// now and is written once.
func (s *Store) seenLocked(user string) (seenState, error) {
	if s.seen == nil {
		s.seen = map[string]seenState{}
	}
	if st, ok := s.seen[user]; ok {
		return st, nil
	}
	var st seenState
	err := s.read(user, seenFile, &st, maxSeenFile)
	if errors.Is(err, ErrNotFound) {
		st = seenState{Since: time.Now().UTC()}
		err = s.write(user, seenFile, st)
	}
	if err != nil {
		return seenState{}, err
	}
	if st.Chats == nil {
		st.Chats = map[string]time.Time{}
	}
	s.seen[user] = st
	return st, nil
}

// Unread reports whether a chat changed after the person last saw it.
func (st seenState) unread(c ChatInfo) bool {
	if c.State == StateRunning {
		return false
	}
	seen, ok := st.Chats[c.ID]
	if !ok {
		seen = st.Since
	}
	return c.Updated.After(seen)
}

// MarkUnread fills Unread in a list of chats.
func (s *Store) MarkUnread(user string, list []ChatInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.seenLocked(user)
	if err != nil {
		return
	}
	for i := range list {
		list[i].Unread = st.unread(list[i])
	}
}

// Seen notes that the person looked at a chat at the given time. It only
// writes when the chat changed since the last look, so the page may call
// it often without waking the disks.
func (s *Store) Seen(user, id string, at time.Time) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.seenLocked(user)
	if err != nil {
		return err
	}
	// The last change from RAM; the index is read only once per account.
	known, cached := s.updated[user]
	if !cached {
		if _, err := s.indexLocked(user); err != nil {
			return err
		}
		known = s.updated[user]
	}
	updated, ok := known[id]
	if !ok {
		return ErrNotFound
	}
	if prev, ok := st.Chats[id]; ok && !updated.After(prev) {
		return nil
	}
	if _, ok := st.Chats[id]; !ok && !updated.After(st.Since) {
		return nil
	}
	st.Chats[id] = at.UTC()
	// Forget chats that no longer exist.
	for k := range st.Chats {
		if _, ok := known[k]; !ok {
			delete(st.Chats, k)
		}
	}
	return s.write(user, seenFile, st)
}

// SeenAt returns when the person last looked at a chat (or when read
// states began, for a chat never looked at).
func (s *Store) SeenAt(user, id string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.seenLocked(user)
	if err != nil {
		return time.Time{}
	}
	if t, ok := st.Chats[id]; ok {
		return t
	}
	return st.Since
}

// KnownUpdates returns the last change of every chat of an account, from
// RAM once the index was read (PLAN 49: the copy in the share looks every
// minute without reading the volume).
func (s *Store) KnownUpdates(user string) (map[string]time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.updated[user]; ok {
		return maps.Clone(m), nil
	}
	if _, err := s.indexLocked(user); err != nil {
		return nil, err
	}
	return maps.Clone(s.updated[user]), nil
}

var privateName = regexp.MustCompile(`^[a-z]+\.json$`)

// ReadPrivate reads a small file of the account's private folder.
func (s *Store) ReadPrivate(user, name string, v any) error {
	if !privateName.MatchString(name) {
		return ErrNotFound
	}
	return s.read(user, name, v, maxSeenFile)
}

// ReadPrivateLimit reads a larger file of the account's private folder
// (the photo index, PLAN 52).
func (s *Store) ReadPrivateLimit(user, name string, v any, limit int64) error {
	if !privateName.MatchString(name) {
		return ErrNotFound
	}
	return s.read(user, name, v, limit)
}

// WritePrivate writes a small file of the account's private folder.
func (s *Store) WritePrivate(user, name string, v any) error {
	if !privateName.MatchString(name) {
		return ErrNotFound
	}
	return s.write(user, name, v)
}
