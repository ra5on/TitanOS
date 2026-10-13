package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
