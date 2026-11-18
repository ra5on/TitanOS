package profile

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"slices"
	"sync"
	"time"
)

// One-time links of a profile (1.13): the web session asks for one with
// the account password; the iPhone, which is not logged in, fetches the
// profile once with the token of the link. The token is 32 bytes from
// crypto/rand (256 bits) in base64url; RAM keeps only its SHA-256, the
// link is valid 10 minutes and taken out of the table before anything is
// made of it, so two fetches at once get one profile.

const (
	// TTL is how long a link can be fetched.
	TTL = 10 * time.Minute
	// MaxPerAccount links wait per account; a new one ends the oldest.
	MaxPerAccount = 3
	// MaxLinks wait in all; beyond that a new link is refused.
	MaxLinks = 100
	// tokenBytes of randomness in a token.
	tokenBytes = 32
	// keepDone: how long a fetched, expired or cancelled link answers its
	// state to the page that made it.
	keepDone = TTL
)

// States of a link as the page sees it.
const (
	StatePending   = "pending"
	StateFetched   = "fetched"
	StateFailed    = "failed"
	StateExpired   = "expired"
	StateCancelled = "cancelled"
)

var (
	// ErrFull: MaxLinks links wait (503).
	ErrFull = errors.New("profile: too many links waiting")
	// ErrNoLink: no such link of the account (404).
	ErrNoLink = errors.New("profile: no such link")
)

// Request is what a link makes when it is fetched: the device and the
// profile's choices, bound to the account and its login.
type Request struct {
	Account string
	// Created and Creds bind the link to the account as it was: a new
	// account of the name, a changed password or second factor end it.
	Created time.Time
	Creds   string
	// Session is the ID (auth.SessionID) of the web session that made it:
	// when it ends or is locked, the link ends.
	Session  string
	Device   string
	Host     string
	Port     int
	Lang     string
	Holidays string // region, "" for none
}

// Link is what the page that made a link learns of it; never the token
// after it was handed out once.
type Link struct {
	ID      string    `json:"id"`
	Device  string    `json:"device"`
	Expires time.Time `json:"expires"`
	State   string    `json:"state"`
	// Code says why a fetch failed (a code of the API, never a secret).
	Code string `json:"code,omitempty"`
}

type entry struct {
	req   Request
	link  Link
	sum   [32]byte // of the token; zero once taken
	until time.Time
}

// Links is the table of links, in RAM only: a restart of titand ends them
// all, and nothing is left behind (no device exists before a fetch).
type Links struct {
	// Now is the clock; nil: time.Now.
	Now func() time.Time

	mu      sync.Mutex
	entries []*entry
}

func (l *Links) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

// pruneLocked marks expired links and drops those done long enough.
func (l *Links) pruneLocked(now time.Time) {
	l.entries = slices.DeleteFunc(l.entries, func(e *entry) bool {
		if e.link.State == StatePending && !now.Before(e.link.Expires) {
			e.link.State = StateExpired
			e.sum = [32]byte{}
			e.until = now.Add(keepDone)
		}
		return e.link.State != StatePending && !now.Before(e.until)
	})
}

// New makes a link for a request and returns it with its token, which is
// never stored and never shown again.
func (l *Links) New(req Request) (Link, string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return Link{}, "", err
	}
	idRaw := make([]byte, 8)
	if _, err := rand.Read(idRaw); err != nil {
		return Link{}, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
	var waiting []*entry
	pending := 0
	for _, e := range l.entries {
		if e.link.State == StatePending {
			pending++
			if e.req.Account == req.Account {
				waiting = append(waiting, e)
			}
		}
	}
	// The oldest of the account's waiting links ends for the new one.
	for len(waiting) >= MaxPerAccount {
		cancel(waiting[0], now)
		waiting = waiting[1:]
		pending--
	}
	if pending >= MaxLinks {
		return Link{}, "", ErrFull
	}
	e := &entry{req: req, sum: sha256.Sum256([]byte(token)),
		link: Link{ID: "p_" + hex.EncodeToString(idRaw), Device: req.Device, Expires: now.Add(TTL), State: StatePending}}
	l.entries = append(l.entries, e)
	return e.link, token, nil
}

func cancel(e *entry, now time.Time) {
	e.link.State = StateCancelled
	e.sum = [32]byte{}
	e.until = now.Add(keepDone)
}

// ValidToken reports whether a token has the form New gives (43
// characters of base64url); anything else is not looked up.
func ValidToken(token string) bool {
	if len(token) != base64.RawURLEncoding.EncodedLen(tokenBytes) {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil
}

// Take uses a link up: the request of a pending, unexpired link of the
// token, or false. The link is spent at once, whatever comes of it;
// Done says how the fetch ended.
func (l *Links) Take(token string) (Request, string, bool) {
	if !ValidToken(token) {
		return Request{}, "", false
	}
	sum := sha256.Sum256([]byte(token))
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
	for _, e := range l.entries {
		// The zero sum of a spent link never equals a SHA-256.
		if e.link.State == StatePending && e.sum == sum {
			e.sum = [32]byte{}
			e.link.State = StateFailed // until Done says otherwise
			e.until = now.Add(keepDone)
			return e.req, e.link.ID, true
		}
	}
	return Request{}, "", false
}

// Done records how the fetch of a taken link ended: "" for a profile
// handed out, else the code of the API it was refused with.
func (l *Links) Done(id, code string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.entries {
		if e.link.ID == id {
			if code == "" {
				e.link.State, e.link.Code = StateFetched, ""
			} else {
				e.link.State, e.link.Code = StateFailed, code
			}
		}
	}
}

// Status is the state of a link of the account.
func (l *Links) Status(account, id string) (Link, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(l.now())
	for _, e := range l.entries {
		if e.link.ID == id && e.req.Account == account {
			return e.link, nil
		}
	}
	return Link{}, ErrNoLink
}

// Cancel ends a waiting link of the account (the page was closed, or the
// person chose another way).
func (l *Links) Cancel(account, id string) error {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
	for _, e := range l.entries {
		if e.link.ID == id && e.req.Account == account {
			if e.link.State == StatePending {
				cancel(e, now)
			}
			return nil
		}
	}
	return ErrNoLink
}

// Forget ends every link of an account: deleted, password changed, second
// factor reset (the fetch checks these too; this frees the table).
func (l *Links) Forget(account string) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.entries {
		if e.req.Account == account && e.link.State == StatePending {
			cancel(e, now)
		}
	}
}
