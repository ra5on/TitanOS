package agent

import (
	"errors"
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
	index, err := s.indexLocked(user)
	if err != nil {
		return err
	}
	var info *ChatInfo
	for i := range index {
		if index[i].ID == id {
			info = &index[i]
		}
	}
	if info == nil {
		return ErrNotFound
	}
	if prev, ok := st.Chats[id]; ok && !info.Updated.After(prev) {
		return nil
	}
	if _, ok := st.Chats[id]; !ok && !info.Updated.After(st.Since) {
		return nil
	}
	st.Chats[id] = at.UTC()
	// Forget chats that no longer exist.
	known := make(map[string]bool, len(index))
	for _, c := range index {
		known[c.ID] = true
	}
	for k := range st.Chats {
		if !known[k] {
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
