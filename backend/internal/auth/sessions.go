package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	// SessionIdleTimeout ends a session that was not used for this long.
	SessionIdleTimeout = 7 * 24 * time.Hour
	// SessionMaxAge ends a session this long after the login, used or not.
	SessionMaxAge = 30 * 24 * time.Hour
)

// Session is one login on one device.
type Session struct {
	User      string
	Created   time.Time
	LastSeen  time.Time
	RemoteIP  string
	UserAgent string
	// Confirmed is when the password was last entered again in this
	// session, as required before opening a console.
	Confirmed time.Time
	// Remember keeps the session over a restart of the NAS ("stay logged
	// in"): a hash of its token is kept on the stick. It ends the chosen
	// number of days after the login (or never), not after
	// SessionIdleTimeout. Every session has it while the setting is on.
	Remember bool `json:",omitempty"`
}

// ConfirmFor is how long entering the password again opens the door to
// consoles and other sensitive actions.
const ConfirmFor = 15 * time.Minute

// Sessions keeps the logins in memory. Only with "stay logged in" (see
// Remember) is something about them written to the stick. With Persist they are also kept in a file in RAM (/run), so
// that they outlive a restart of titand, as after an app update, but not
// a restart of the NAS.
type Sessions struct {
	mu       sync.Mutex
	sessions map[[sha256.Size]byte]*Session
	now      func() time.Time
	// file is where the sessions are kept between starts of titand; empty
	// keeps them in memory only. saved is when it was last written.
	file  string
	saved time.Time
	// keep holds the remembered sessions on the stick; kept is what was
	// last written there, so that only a real change writes again.
	keep Keeper
	kept []byte
	// want is what the stick should hold, wantMap the same as a map to
	// write. A single writer (writeKept) writes the latest of it outside
	// the lock, so that a slow stick never holds up logins; changes in
	// the meantime are taken together. wantGen counts the changes,
	// writing says whether the writer runs, idle wakes those who wait
	// for it.
	want    []byte
	wantMap map[string]Session
	wantGen uint64
	writing bool
	idle    *sync.Cond
	// stay is the setting "stay logged in": days after the login, 0 = off,
	// StayForever = no end.
	stay int
}

// Keeper reads and writes a file of the persistent configuration (/config).
type Keeper interface {
	ReadJSON(name string, v any) error
	WriteJSON(name string, v any) error
}

// RememberFile holds the remembered sessions on the stick: hashes of their
// tokens, the account, the login time and the device, nothing that changes
// while a session is used.
const RememberFile = "sessions/remembered.json"

// StayFile holds the setting "stay logged in" of the NAS.
const StayFile = "system/login.json"

// StayForever keeps a login until it is ended by hand.
const StayForever = -1

// StayChoices are the settings the page offers: off, days, no end.
var StayChoices = []int{0, 30, 90, 365, StayForever}

// StaySettings is the content of StayFile.
type StaySettings struct {
	StayDays int `json:"stayDays"`
}

// ErrStayChoice is a setting not in StayChoices.
var ErrStayChoice = errors.New("unknown choice for staying logged in")

// saveEvery is how often a session that is only used, not changed, is
// written: what the file misses is when it was last seen.
const saveEvery = time.Minute

// NewSessions returns an empty session store.
func NewSessions() *Sessions {
	return &Sessions{sessions: map[[sha256.Size]byte]*Session{}, now: time.Now}
}

// Create starts a session and returns its secret token for the cookie. Only
// a hash of the token is kept. With "stay logged in" on, it outlives a
// restart of the NAS.
func (s *Sessions) Create(user, remoteIP, userAgent string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.prune(now)
	s.sessions[sha256.Sum256([]byte(token))] = &Session{
		User: user, Created: now, LastSeen: now, RemoteIP: remoteIP, UserAgent: userAgent, Remember: s.stay != 0,
	}
	s.save()
	s.keepRemembered()
	return token, nil
}

// Get returns the session for a token and marks it as used.
func (s *Sessions) Get(token string) (Session, bool) {
	if token == "" {
		return Session{}, false
	}
	key := sha256.Sum256([]byte(token))

	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[key]
	if !ok {
		return Session{}, false
	}
	now := s.now()
	if s.expired(session, now) {
		delete(s.sessions, key)
		s.save()
		s.keepRemembered()
		return Session{}, false
	}
	session.LastSeen = now
	if now.Sub(s.saved) >= saveEvery {
		s.save()
	}
	return *session, true
}

// Peek returns the session for a token without marking it as used, for
// checks that run on their own, such as those of an open console.
func (s *Sessions) Peek(token string) (Session, bool) {
	if token == "" {
		return Session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sha256.Sum256([]byte(token))]
	if !ok || s.expired(session, s.now()) {
		return Session{}, false
	}
	return *session, true
}

// Delete ends the session for a token.
func (s *Sessions) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sha256.Sum256([]byte(token)))
	s.save()
	s.keepRemembered()
}

func (s *Sessions) expired(session *Session, now time.Time) bool {
	if session.Remember && s.stay != 0 {
		return s.stay > 0 && now.Sub(session.Created) > time.Duration(s.stay)*24*time.Hour
	}
	return now.Sub(session.LastSeen) > SessionIdleTimeout || now.Sub(session.Created) > SessionMaxAge
}

// until is when a remembered session ends; zero for no end. The lock is held.
func (s *Sessions) until(session *Session) time.Time {
	if s.stay <= 0 {
		return time.Time{}
	}
	return session.Created.Add(time.Duration(s.stay) * 24 * time.Hour)
}

// Stay returns the setting "stay logged in".
func (s *Sessions) Stay() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stay
}

// SetStay changes the setting "stay logged in" and saves it. It applies to
// the sessions there are as well: switched on, they are kept on the stick
// from now on; switched off, they end with the next restart of the NAS.
func (s *Sessions) SetStay(days int) error {
	if !validStay(days) {
		return ErrStayChoice
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keep != nil && days != s.stay {
		if err := s.keep.WriteJSON(StayFile, StaySettings{StayDays: days}); err != nil {
			return err
		}
	}
	s.applyStay(days)
	s.save()
	return nil
}

func validStay(days int) bool {
	for _, choice := range StayChoices {
		if days == choice {
			return true
		}
	}
	return false
}

// applyStay sets the setting in memory and brings the sessions and the
// stick in line with it. The lock is held.
func (s *Sessions) applyStay(days int) {
	s.stay = days
	for _, session := range s.sessions {
		session.Remember = days != 0
	}
	s.prune(s.now())
	s.keepRemembered()
}

func (s *Sessions) prune(now time.Time) {
	for key, session := range s.sessions {
		if s.expired(session, now) {
			delete(s.sessions, key)
		}
	}
}

// DeleteUser ends all sessions of an account, except the one with the
// given token (pass "" to end all). Used when the password changes or the
// account is removed.
func (s *Sessions) DeleteUser(user, exceptToken string) {
	keep := sha256.Sum256([]byte(exceptToken))
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, session := range s.sessions {
		if session.User == user && (exceptToken == "" || key != keep) {
			delete(s.sessions, key)
		}
	}
	s.save()
	s.keepRemembered()
}

// Confirm notes that the password was just entered again in this session.
func (s *Sessions) Confirm(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.sessions[sha256.Sum256([]byte(token))]; ok {
		session.Confirmed = s.now()
		s.save()
	}
}

// Confirmed reports whether the password was entered again recently.
func (s *Sessions) Confirmed(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sha256.Sum256([]byte(token))]
	return ok && !session.Confirmed.IsZero() && s.now().Sub(session.Confirmed) < ConfirmFor
}

// SessionInfo is a session as its owner sees it in the list of devices.
type SessionInfo struct {
	// ID names the session without giving away its token.
	ID        string    `json:"id"`
	Created   time.Time `json:"created"`
	LastSeen  time.Time `json:"lastSeen"`
	RemoteIP  string    `json:"remoteIP"`
	UserAgent string    `json:"userAgent"`
	// Current marks the session the request came with.
	Current bool `json:"current"`
	// Kept is a session that outlives a restart of the NAS; Until is when
	// it ends, absent for no end.
	Kept  bool       `json:"kept"`
	Until *time.Time `json:"until,omitempty"`
}

func sessionID(key [sha256.Size]byte) string {
	// A hash of the hash: the ID is shown in the browser and must not
	// help anyone who sees it.
	sum := sha256.Sum256(key[:])
	return base64.RawURLEncoding.EncodeToString(sum[:12])
}

// List returns the sessions of an account, the newest first.
func (s *Sessions) List(user, currentToken string) []SessionInfo {
	current := sha256.Sum256([]byte(currentToken))
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.prune(now)
	list := []SessionInfo{}
	for key, session := range s.sessions {
		if session.User == user {
			info := SessionInfo{ID: sessionID(key), Created: session.Created, LastSeen: session.LastSeen, RemoteIP: session.RemoteIP, UserAgent: session.UserAgent, Current: key == current}
			if session.Remember && s.stay != 0 {
				info.Kept = true
				if until := s.until(session); !until.IsZero() {
					info.Until = &until
				}
			}
			list = append(list, info)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].LastSeen.After(list[j].LastSeen) })
	return list
}

// DeleteByID ends one session of an account and reports whether there was one.
func (s *Sessions) DeleteByID(user, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, session := range s.sessions {
		if session.User == user && sessionID(key) == id {
			delete(s.sessions, key)
			s.save()
			s.keepRemembered()
			return true
		}
	}
	return false
}

// Persist keeps the sessions in a file from now on and takes over those
// that are in it already. The file holds only hashes of the tokens, and
// belongs in RAM: a restart of the NAS ends every session as before.
func (s *Sessions) Persist(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.file = path
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var stored map[string]*Session
	if err := json.Unmarshal(data, &stored); err != nil {
		return err
	}
	now := s.now()
	for hash, session := range stored {
		raw, err := hex.DecodeString(hash)
		if err != nil || len(raw) != sha256.Size || session == nil || session.User == "" || s.expired(session, now) {
			continue
		}
		s.sessions[[sha256.Size]byte(raw)] = session
	}
	return nil
}

// Remember reads the setting "stay logged in", keeps the sessions on the
// stick from now on while it is on and takes over those stored there that
// are not known yet (after a restart of the NAS). Call it after Persist:
// the file in RAM knows more about them.
func (s *Sessions) Remember(store Keeper) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var stored map[string]*Session
	if err := store.ReadJSON(RememberFile, &stored); err != nil && !errors.Is(err, fs.ErrNotExist) {
		// Not readable: leave the file alone rather than overwrite it.
		return err
	}
	var setting StaySettings
	switch err := store.ReadJSON(StayFile, &setting); {
	case errors.Is(err, fs.ErrNotExist):
		// 0.37.0 kept the logins of a tick on the login page for 30 days.
		// Whoever used it keeps that until the setting is chosen.
		setting.StayDays = 0
		if len(stored) > 0 {
			setting.StayDays = 30
		}
	case err != nil:
		return err
	case !validStay(setting.StayDays):
		setting.StayDays = 0
	}
	s.keep = store
	s.stay = setting.StayDays
	// What the file holds counts as written: a start alone does not write.
	written := map[string]Session{}
	for hash, session := range stored {
		if session != nil {
			written[hash] = fixed(session)
		}
	}
	s.kept, _ = json.Marshal(written)
	s.want = s.kept
	now := s.now()
	for hash, session := range stored {
		raw, err := hex.DecodeString(hash)
		if err != nil || len(raw) != sha256.Size || session == nil || session.User == "" || !session.Remember || s.stay == 0 || s.expired(session, now) {
			continue
		}
		key := [sha256.Size]byte(raw)
		if _, known := s.sessions[key]; !known {
			session.LastSeen = session.Created
			s.sessions[key] = session
		}
	}
	s.applyStay(s.stay)
	return nil
}

// fixed is what is kept of a remembered session on the stick.
func fixed(session *Session) Session {
	return Session{User: session.User, Created: session.Created, RemoteIP: session.RemoteIP, UserAgent: session.UserAgent, Remember: true}
}

// keepRemembered has the remembered sessions written to the stick if they
// changed. Only the fields that are fixed for a session are written, so
// using a session never writes. The lock is held; the writing itself
// happens in writeKept, outside of it.
func (s *Sessions) keepRemembered() {
	if s.keep == nil {
		return
	}
	now := s.now()
	stored := map[string]Session{}
	for key, session := range s.sessions {
		if session.Remember && s.stay != 0 && !s.expired(session, now) {
			stored[hex.EncodeToString(key[:])] = fixed(session)
		}
	}
	data, err := json.Marshal(stored)
	if err != nil || string(data) == string(s.want) {
		return
	}
	s.want, s.wantMap = data, stored
	s.wantGen++
	if !s.writing {
		s.writing = true
		go s.writeKept()
	}
}

// writeKept writes what keepRemembered asked for, always the latest, one
// write at a time and so in order, until nothing is left to write.
func (s *Sessions) writeKept() {
	s.mu.Lock()
	for done := uint64(0); done != s.wantGen; {
		gen, data, stored, keep := s.wantGen, s.want, s.wantMap, s.keep
		written := string(data) == string(s.kept)
		s.mu.Unlock()
		var err error
		if !written {
			err = keep.WriteJSON(RememberFile, stored)
		}
		s.mu.Lock()
		switch {
		case err != nil:
			slog.Warn("remembered logins not saved to the stick", "err", err)
			// The next change tries again, also back to the same state.
			if s.wantGen == gen {
				s.want = s.kept
			}
		case !written:
			s.kept = data
		}
		done = gen
	}
	s.writing = false
	s.waiters().Broadcast()
	s.mu.Unlock()
}

// waiters returns the condition writeKept signals. The lock is held.
func (s *Sessions) waiters() *sync.Cond {
	if s.idle == nil {
		s.idle = sync.NewCond(&s.mu)
	}
	return s.idle
}

// waitKept waits until the stick holds what it should. The lock is held.
func (s *Sessions) waitKept() {
	for s.writing {
		s.waiters().Wait()
	}
}

// Flush writes the sessions now, for a clean stop of titand, and waits
// for the remembered ones to be on the stick.
func (s *Sessions) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.save()
	s.waitKept()
}

// save writes the sessions to the file, if there is one. The lock is held.
func (s *Sessions) save() {
	if s.file == "" {
		return
	}
	s.saved = s.now()
	stored := make(map[string]*Session, len(s.sessions))
	for key, session := range s.sessions {
		stored[hex.EncodeToString(key[:])] = session
	}
	data, err := json.Marshal(stored)
	if err == nil {
		err = writePrivate(s.file, data)
	}
	if err != nil {
		slog.Warn("sessions not kept for the next start of titand", "err", err)
	}
}

// writePrivate replaces a file only root may read.
func writePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
