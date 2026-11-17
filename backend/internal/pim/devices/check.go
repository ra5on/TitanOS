package devices

import (
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// Realm is the Basic realm of /caldav/ and /carddav/.
const Realm = "TitanOS Kalender"

// journalEvery: a revoked device that keeps asking is written to the
// journal at most this often.
const journalEvery = time.Hour

var (
	// ErrNoCredentials: no Basic credentials (401, not counted).
	ErrNoCredentials = errors.New("devices: no credentials")
	// ErrDenied: unknown account, wrong or too long password, locked
	// account (401, counted in the limiter).
	ErrDenied = errors.New("devices: wrong name or device password")
	// ErrRevoked: the password of a revoked device (401, not counted).
	ErrRevoked = errors.New("devices: revoked device")
)

// LimitedError: too many failures from this address or for this account
// (429 with Retry-After).
type LimitedError struct{ RetryAfter time.Duration }

func (e *LimitedError) Error() string {
	return fmt.Sprintf("devices: too many attempts, retry after %s", e.RetryAfter)
}

// Identity is a device that signed in.
type Identity struct {
	User   auth.User
	Device string // its id
	Name   string // its name
}

// limiterKeys are the keys of the calendar, apart from those of WebDAV
// ("dav:"): a wrong WebDAV password in Infuse locks no calendar, and the
// other way round.
func limiterKeys(ip, name string) []string { return []string{"cal:" + ip, "cal-user:" + name} }

// Authenticate checks the Basic credentials of a request to /caldav/ or
// /carddav/ (TECHNIK 3.4). Only a device password of the account is taken
// – no account password, WebDAV password or API token. Errors: a
// *LimitedError, ErrNoCredentials, ErrDenied, ErrRevoked or
// ErrUnavailable; WriteChallenge answers each. An unknown account and a
// wrong password get the same answer after the same work. A success is
// not recorded as "last synced": the handler calls Synced after it
// answered.
func (m *Manager) Authenticate(r *http.Request) (Identity, error) {
	ip := m.remoteIP(r)
	name, password, ok := r.BasicAuth()
	if wait := m.limiter().RetryAfter(limiterKeys(ip, name)...); wait > 0 {
		return Identity{}, &LimitedError{RetryAfter: wait}
	}
	if !ok || name == "" {
		return Identity{}, ErrNoCredentials
	}
	if len(password) > MaxPassword {
		return Identity{}, m.refuse(ip, name)
	}
	user, known := m.users.Get(name)
	var cur, old [][]byte
	var ids, names []string
	if known {
		m.mu.Lock()
		st, err := m.stateLocked(user)
		if err == nil {
			cur, old = st.sums, st.revoked
			for _, d := range st.file.Devices {
				ids, names = append(ids, d.ID), append(names, d.Name)
			}
		}
		m.mu.Unlock()
		if err != nil {
			// The stick is away: nobody is let in, and nobody is counted.
			return Identity{}, err
		}
	}
	sum := Sum(password)
	// Every slot is compared, a dummy where the account has fewer
	// devices: the same work for every account, known or not.
	match, wasRevoked := -1, false
	for i := range Max {
		want := m.dummy[i]
		if i < len(cur) && len(cur[i]) == len(sum) {
			want = cur[i]
		}
		if subtle.ConstantTimeCompare(sum, want) == 1 && i < len(cur) {
			match = i
		}
	}
	for i := range MaxRevoked {
		want := m.dummy[Max+i]
		if i < len(old) && len(old[i]) == len(sum) {
			want = old[i]
		}
		if subtle.ConstantTimeCompare(sum, want) == 1 && i < len(old) {
			wasRevoked = true
		}
	}
	switch {
	case known && match >= 0 && !user.Disabled:
		return Identity{User: user, Device: ids[match], Name: names[match]}, nil
	case known && match < 0 && wasRevoked:
		m.noteRevoked(user.Name, ip, sum)
		return Identity{}, ErrRevoked
	}
	return Identity{}, m.refuse(ip, name)
}

// refuse counts a failure and writes it to the journal (name and address,
// never the password).
func (m *Manager) refuse(ip, name string) error {
	m.limiter().Fail(limiterKeys(ip, name)...)
	slog.Warn("Calendar sign-in refused", "user", name, "ip", ip)
	return ErrDenied
}

// noteRevoked writes a journal line once per revoked device and hour.
func (m *Manager) noteRevoked(name, ip string, sum []byte) {
	key := name + "/" + hex.EncodeToString(sum[:8])
	now := m.now()
	m.mu.Lock()
	last, ok := m.journal[key]
	write := !ok || now.Sub(last) >= journalEvery
	if write {
		m.journal[key] = now
	}
	if len(m.journal) > 4096 {
		for k, at := range m.journal {
			if now.Sub(at) >= journalEvery {
				delete(m.journal, k)
			}
		}
	}
	m.mu.Unlock()
	if write {
		slog.Info("Calendar: revoked device signs in", "user", name, "ip", ip)
	}
}

// WriteChallenge answers a refused request: 429 with Retry-After, 503
// while the device passwords are not readable, else 401 with the Basic
// challenge (the same for every reason, so nothing tells an unknown
// account from a wrong password).
func WriteChallenge(w http.ResponseWriter, err error) {
	var limited *LimitedError
	switch {
	case errors.As(err, &limited):
		w.Header().Set("Retry-After", strconv.Itoa(int(limited.RetryAfter.Seconds())+1))
		http.Error(w, "too many attempts", http.StatusTooManyRequests)
	case errors.Is(err, ErrUnavailable):
		http.Error(w, "device passwords not available", http.StatusServiceUnavailable)
	default:
		w.Header().Set("WWW-Authenticate", `Basic realm="`+Realm+`", charset="UTF-8"`)
		http.Error(w, "sign in with a device password", http.StatusUnauthorized)
	}
}
