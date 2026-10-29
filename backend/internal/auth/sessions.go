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
	// in"): a hash of its token is kept on the stick. It ends only after
	// SessionMaxAge, not after SessionIdleTimeout.
	Remember bool `json:",omitempty"`
}

// ConfirmFor is how long entering the password again opens the door to
// consoles and other sensitive actions.
const ConfirmFor = 15 * time.Minute

// Sessions keeps the logins in memory. Nothing about them is written to
// the stick. With Persist they are also kept in a file in RAM (/run), so
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

// saveEvery is how often a session that is only used, not changed, is
// written: what the file misses is when it was last seen.
const saveEvery = time.Minute

// NewSessions returns an empty session store.
func NewSessions() *Sessions {
	return &Sessions{sessions: map[[sha256.Size]byte]*Session{}, now: time.Now}
}

// Create starts a session and returns its secret token for the cookie. Only
// a hash of the token is kept.
func (s *Sessions) Create(user, remoteIP, userAgent string) (string, error) {
	return s.create(user, remoteIP, userAgent, false)
}

// CreateRemembered starts a session that outlives a restart of the NAS.
func (s *Sessions) CreateRemembered(user, remoteIP, userAgent string) (string, error) {
	return s.create(user, remoteIP, userAgent, true)
}

func (s *Sessions) create(user, remoteIP, userAgent string, remember bool) (string, error) {
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
		User: user, Created: now, LastSeen: now, RemoteIP: remoteIP, UserAgent: userAgent, Remember: remember,
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
	if expired(session, now) {
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
	if !ok || expired(session, s.now()) {
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

func expired(session *Session, now time.Time) bool {
	return (!session.Remember && now.Sub(session.LastSeen) > SessionIdleTimeout) || now.Sub(session.Created) > SessionMaxAge
}

func (s *Sessions) prune(now time.Time) {
	for key, session := range s.sessions {
		if expired(session, now) {
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
			list = append(list, SessionInfo{ID: sessionID(key), Created: session.Created, LastSeen: session.LastSeen, RemoteIP: session.RemoteIP, UserAgent: session.UserAgent, Current: key == current})
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
		if err != nil || len(raw) != sha256.Size || session == nil || session.User == "" || expired(session, now) {
			continue
		}
		s.sessions[[sha256.Size]byte(raw)] = session
	}
	return nil
}

// Remember keeps sessions with Remember on the stick from now on and takes
// over those stored there that are not known yet (after a restart of the
// NAS). Call it after Persist: the file in RAM knows more about them.
func (s *Sessions) Remember(store Keeper) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var stored map[string]*Session
	if err := store.ReadJSON(RememberFile, &stored); err != nil && !errors.Is(err, fs.ErrNotExist) {
		// Not readable: leave the file alone rather than overwrite it.
		return err
	}
	s.keep = store
	// What the file holds counts as written: a start alone does not write.
	written := map[string]Session{}
	for hash, session := range stored {
		if session != nil {
			written[hash] = fixed(session)
		}
	}
	s.kept, _ = json.Marshal(written)
	now := s.now()
	for hash, session := range stored {
		raw, err := hex.DecodeString(hash)
		if err != nil || len(raw) != sha256.Size || session == nil || session.User == "" || !session.Remember || expired(session, now) {
			continue
		}
		key := [sha256.Size]byte(raw)
		if _, known := s.sessions[key]; !known {
			session.LastSeen = session.Created
			s.sessions[key] = session
		}
	}
	s.keepRemembered()
	return nil
}

// fixed is what is kept of a remembered session on the stick.
func fixed(session *Session) Session {
	return Session{User: session.User, Created: session.Created, RemoteIP: session.RemoteIP, UserAgent: session.UserAgent, Remember: true}
}

// keepRemembered writes the remembered sessions to the stick if they
// changed. Only the fields that are fixed for a session are written, so
// using a session never writes. The lock is held.
func (s *Sessions) keepRemembered() {
	if s.keep == nil {
		return
	}
	now := s.now()
	stored := map[string]Session{}
	for key, session := range s.sessions {
		if session.Remember && !expired(session, now) {
			stored[hex.EncodeToString(key[:])] = fixed(session)
		}
	}
	data, err := json.Marshal(stored)
	if err != nil || string(data) == string(s.kept) {
		return
	}
	if err := s.keep.WriteJSON(RememberFile, stored); err != nil {
		slog.Warn("remembered logins not saved to the stick", "err", err)
		return
	}
	s.kept = data
}

// Flush writes the sessions now, for a clean stop of titand.
func (s *Sessions) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.save()
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
