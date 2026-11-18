package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// The mark of kept data (BAUSTAND, open point of step 4): an account whose
// folder is missing on the current system data volume is not served the
// virtual "privat"/"kontakte" (ctag 0-0) when it kept data before – iOS
// would mirror the empty collection and delete its copy. A move cut short
// by a restart, a default volume changed without a move, an old volume
// unplugged: the folders stay where they were.
//
// /config/users/<account>/pim.json holds, per feature, when the account
// first kept data there, bound to the account by name and creation time
// (an earlier account of the name has no mark). It is written once per
// account and feature (a change of the configuration, not a periodic
// write), read once into RAM, and goes with users/<account> when the
// account is deleted (auth.Users.Delete). 1.10 ignores the file.
//
// Remaining limit: the mark only stops the empty answer. The data does not
// come back by itself; the owner moves the system data back (or plugs the
// volume in again), or starts anew with Abandon. T-Mail has the same move
// limit (its folders follow the same volumedata.Home) without the mark.

// ErrElsewhere: the account kept data of the feature, but its folder is
// not on the current volume (503 like ErrUnavailable, errors.Is matches
// both). errors.As gives *ElsewhereError with the volume that holds it.
var ErrElsewhere = fmt.Errorf("pim: the data of the account is on another volume: %w", ErrUnavailable)

// ElsewhereError names the mounted volume that holds the account's folder;
// Volume is "" when no mounted volume has it.
type ElsewhereError struct {
	Kind   Kind
	Volume string
}

func (e *ElsewhereError) Error() string {
	if e.Volume == "" {
		return fmt.Sprintf("%v (%s, on no mounted volume)", ErrElsewhere, e.Kind)
	}
	return fmt.Sprintf("%v (%s, on %s)", ErrElsewhere, e.Kind, e.Volume)
}

func (e *ElsewhereError) Unwrap() error { return ErrElsewhere }

// mark is pim.json.
// Tasks (1.13): 1.12 ignores the field, and drops it when it writes a
// first mark of its own (the next write of a reminder sets it again).
type mark struct {
	Owner    owner     `json:"owner"`
	Calendar time.Time `json:"calendar,omitzero"`
	Contacts time.Time `json:"contacts,omitzero"`
	Tasks    time.Time `json:"tasks,omitzero"`
}

func (m mark) has(kind Kind) bool {
	switch kind {
	case Contacts:
		return !m.Contacts.IsZero()
	case Tasks:
		return !m.Tasks.IsZero()
	}
	return !m.Calendar.IsZero()
}

func (m *mark) set(kind Kind, at time.Time) {
	switch kind {
	case Contacts:
		m.Contacts = at
	case Tasks:
		m.Tasks = at
	default:
		m.Calendar = at
	}
}

func (m *mark) clear(kind Kind) {
	m.set(kind, time.Time{})
}

// knownMark is a mark as read for one account (by its creation time).
type knownMark struct {
	created time.Time
	m       mark
}

// hintCache is where the folders of accounts were found on the other
// mounted volumes; sig names the mounted volumes it holds for.
type hintCache struct {
	sig   string
	found map[string]string // name + creation time → volume ("" none)
}

// markOfLocked returns the mark of an account (s.markMu held): from RAM, read
// once from the stick. An error means it is not known now (stick away).
func (s *Store) markOfLocked(name string, created time.Time) (*knownMark, error) {
	if k := s.marks[name]; k != nil && k.created.Equal(created) {
		return k, nil
	}
	var m mark
	data, err := s.ReadMark(name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("pim: pim.json of %s not readable (%v): %w", name, err, ErrUnavailable)
	case json.Unmarshal(data, &m) != nil:
		// Damaged: as without a mark (the data is not known to be elsewhere).
		slog.Warn("Calendar: pim.json damaged; read as without a mark", "account", name)
		m = mark{}
	}
	if !sameAccount(m.Owner, name, created) {
		m = mark{} // of an earlier account of the name
	}
	if s.marks == nil {
		s.marks = map[string]*knownMark{}
	}
	k := &knownMark{created: created, m: m}
	s.marks[name] = k
	return k, nil
}

// marked says whether the account kept data of the feature; false without
// the hooks.
func (s *Store) marked(acc *account) (bool, error) {
	if s.ReadMark == nil {
		return false, nil
	}
	s.markMu.Lock()
	defer s.markMu.Unlock()
	k, err := s.markOfLocked(acc.name, acc.created)
	if err != nil {
		return false, err
	}
	return k.m.has(acc.kind), nil
}

// noteData sets the mark of the feature once, after a write that left an
// own collection on the volume. A mark that cannot be written is tried
// again with the next write; the data write never fails for it.
func (s *Store) noteData(acc *account) {
	if s.ReadMark == nil || s.WriteMark == nil {
		return
	}
	s.markMu.Lock()
	defer s.markMu.Unlock()
	k, err := s.markOfLocked(acc.name, acc.created)
	if err != nil {
		slog.Warn("Calendar: mark of kept data not readable; tried again with the next write", "kind", acc.kind, "account", acc.name, "err", err)
		return
	}
	if k.m.has(acc.kind) {
		return
	}
	next := k.m
	next.Owner = owner{Name: acc.name, Created: acc.created}
	next.set(acc.kind, s.now().UTC())
	data, err := json.Marshal(next)
	if err == nil {
		err = s.WriteMark(acc.name, data)
	}
	if err != nil {
		slog.Warn("Calendar: mark of kept data not written; tried again with the next write", "kind", acc.kind, "account", acc.name, "err", err)
		return
	}
	k.m = next
}

// mountSig names the mounted volumes (RAM only); "" without the hook.
func (s *Store) mountSig() string {
	if s.Mounted == nil {
		return ""
	}
	list := slices.Clone(s.Mounted())
	slices.Sort(list)
	return strings.Join(list, "\x00")
}

// elsewhere is the error for an account whose folder is not here (acc.mu
// held): the mounted volume that holds it, looked for once per mount set,
// and a journal line once per account and finding.
func (s *Store) elsewhere(acc *account, current string) *ElsewhereError {
	sig := s.mountSig()
	key := acc.name + "\x00" + acc.created.UTC().Format(time.RFC3339Nano)
	s.markMu.Lock()
	if s.hints == nil {
		s.hints = map[Kind]*hintCache{}
	}
	hc := s.hints[acc.kind]
	if hc == nil || hc.sig != sig {
		hc = &hintCache{sig: sig, found: map[string]string{}}
		s.hints[acc.kind] = hc
	}
	volume, known := hc.found[key]
	s.markMu.Unlock()
	if !known {
		volume = s.findFolder(acc, current)
		s.markMu.Lock()
		if s.hints[acc.kind] == hc {
			hc.found[key] = volume
		}
		s.markMu.Unlock()
	}
	s.markMu.Lock()
	logKey := string(acc.kind) + "/" + key
	last, logged := s.logged[logKey]
	if s.logged == nil {
		s.logged = map[string]string{}
	}
	s.logged[logKey] = volume
	s.markMu.Unlock()
	if !logged || last != volume {
		found := volume
		if found == "" {
			found = "no mounted volume"
		}
		slog.Warn("Calendar: the account kept data, but its folder is not on the system data volume; answered as unavailable (503), never as empty",
			"kind", acc.kind, "account", acc.name, "volume", current, "found on", found)
	}
	return &ElsewhereError{Kind: acc.kind, Volume: volume}
}

// findFolder looks on the other mounted volumes for .titanos/<feature>/
// <account>/owner.json of this account; "" when none has it.
func (s *Store) findFolder(acc *account, current string) string {
	if s.Mounted == nil || s.Peek == nil {
		return ""
	}
	for _, volume := range s.Mounted() {
		if volume == current {
			continue
		}
		root, err := s.Peek(volume, string(acc.kind))
		if err != nil {
			continue
		}
		var o owner
		err = volumedata.ReadJSON(root, acc.name+"/"+ownerFile, &o, 4096)
		_ = root.Close() // closing a directory changes nothing on disk
		if err == nil && sameAccount(o, acc.name, acc.created) {
			return volume
		}
	}
	return ""
}

// forgetMarks drops what RAM knows of the marks of an account (ForgetUser).
func (s *Store) forgetMarks(name string) {
	s.markMu.Lock()
	defer s.markMu.Unlock()
	delete(s.marks, name)
	prefix := name + "\x00"
	for _, hc := range s.hints {
		for key := range hc.found {
			if strings.HasPrefix(key, prefix) {
				delete(hc.found, key)
			}
		}
	}
	for key := range s.logged {
		if _, rest, ok := strings.Cut(key, "/"); ok && strings.HasPrefix(rest, prefix) {
			delete(s.logged, key)
		}
	}
}

// Abandon gives up the data of a feature that is on another volume (or
// lost with it): the mark goes, and the account starts anew with the
// virtual default on the current volume. For the owner's explicit choice
// only (a later page); a device then mirrors the empty collection.
func (s *Store) Abandon(kind Kind, name string) error {
	if !kind.valid() {
		return ErrInvalidCollection
	}
	if !accountRE.MatchString(name) {
		return ErrInvalidAccount
	}
	var created time.Time
	if s.Account != nil {
		c, ok := s.Account(name)
		if !ok {
			return ErrNoAccount
		}
		created = c
	}
	if s.ReadMark != nil && s.WriteMark != nil {
		s.markMu.Lock()
		k, err := s.markOfLocked(name, created)
		if err == nil && k.m.has(kind) {
			next := k.m
			next.clear(kind)
			var data []byte
			if data, err = json.Marshal(next); err == nil {
				err = s.WriteMark(name, data)
			}
			if err == nil {
				k.m = next
			}
		}
		s.markMu.Unlock()
		if err != nil {
			return err
		}
	}
	// Read again with the next call.
	s.mu.Lock()
	if fc := s.features[kind]; fc != nil {
		delete(fc.accounts, name)
	}
	s.mu.Unlock()
	return nil
}
