package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
}

// ConfirmFor is how long entering the password again opens the door to
// consoles and other sensitive actions.
const ConfirmFor = 15 * time.Minute

// Sessions keeps the logins in memory. They end with a restart of titand;
// nothing about them is written to the stick.
type Sessions struct {
	mu       sync.Mutex
	sessions map[[sha256.Size]byte]*Session
	now      func() time.Time
}

// NewSessions returns an empty session store.
func NewSessions() *Sessions {
	return &Sessions{sessions: map[[sha256.Size]byte]*Session{}, now: time.Now}
}

// Create starts a session and returns its secret token for the cookie. Only
// a hash of the token is kept.
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
		User: user, Created: now, LastSeen: now, RemoteIP: remoteIP, UserAgent: userAgent,
	}
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
		return Session{}, false
	}
	session.LastSeen = now
	return *session, true
}

// Delete ends the session for a token.
func (s *Sessions) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sha256.Sum256([]byte(token)))
}

func expired(session *Session, now time.Time) bool {
	return now.Sub(session.LastSeen) > SessionIdleTimeout || now.Sub(session.Created) > SessionMaxAge
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
}

// Confirm notes that the password was just entered again in this session.
func (s *Sessions) Confirm(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.sessions[sha256.Sum256([]byte(token))]; ok {
		session.Confirmed = s.now()
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
			return true
		}
	}
	return false
}
