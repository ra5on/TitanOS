// Package appaccess decides which accounts may use which Docker app
// ("Programme", PLAN 61) and hands out the short tokens with which a
// browser enters an app's HTTPS name through the app proxy.
//
// Owners and administrators may always use every app. A normal account
// gets an app only with a tick; an app without an entry follows the
// setting for new apps (owners and administrators only, or everyone).
//
// The rights live in /config/docker/app-access.json, written only when
// they change and read once at the start. The key that signs the tokens
// lives in /config/tls/app-gate.key (hidden in the stick file manager,
// never in the diagnostic bundle); it is made once, on first need.
package appaccess

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const (
	configFile = "docker/app-access.json"
	keyFile    = "tls/app-gate.key"

	// EnterTTL is how long a window token is good for; it is used once.
	EnterTTL = 60 * time.Second
	// CookieTTL is how long the browser stays let in to an app.
	CookieTTL = 12 * time.Hour

	// NewAppsAdmins gives a new app to owners and administrators only.
	NewAppsAdmins = "admins"
	// NewAppsEveryone gives a new app to every account.
	NewAppsEveryone = "everyone"
)

// ErrInvalid means a setting is not acceptable; the message says why.
var ErrInvalid = errors.New("appaccess: invalid setting")

var appRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// App is the setting of one app.
type App struct {
	// Users are the normal accounts with a tick. Owners and administrators
	// are never listed: they may always.
	Users []string `json:"users"`
	// NoGate switches "check the login in the browser" off for this app.
	NoGate bool `json:"noGate,omitempty"`
}

// Settings are the content of /config/docker/app-access.json.
type Settings struct {
	// NewApps is who gets an app that has no entry yet: NewAppsAdmins
	// (also when empty) or NewAppsEveryone. Since 0.72 it is only what
	// older titand read (the way back); PerUser decides.
	NewApps string         `json:"newApps,omitempty"`
	Apps    map[string]App `json:"apps"`
	// PerUser: who gets a new app is set per account (PLAN 75-20, U2:
	// "Neu installierte Apps automatisch freigeben" on the account's page);
	// NewAppsUsers are those normal accounts.
	PerUser      bool     `json:"newAppsPerUser,omitempty"`
	NewAppsUsers []string `json:"newAppsUsers,omitempty"`
}

// NewAppsFor tells whether a normal account gets an app without an entry.
func (s Settings) NewAppsFor(user string) bool {
	if s.PerUser {
		return slices.Contains(s.NewAppsUsers, user)
	}
	return s.NewApps == NewAppsEveryone
}

// Account is what the rights need to know of an account right now.
type Account struct {
	Name     string
	Admin    bool // owner or administrator
	Disabled bool
}

// Manager keeps the rights in RAM.
type Manager struct {
	store *config.Store
	// Lookup returns the account as it is now; false if it does not exist.
	Lookup func(name string) (Account, bool)
	// Now is the clock; time.Now if nil.
	Now func() time.Time
	// SessionUsable tells whether the TitanOS login a pass was made for
	// (auth.SessionID) still exists for the account and is not locked
	// (auth.Sessions.Usable). Without it no pass is taken.
	SessionUsable func(id, user string) bool

	mu       sync.RWMutex
	settings Settings
	key      []byte

	usedMu sync.Mutex
	used   map[string]time.Time // nonce → expiry of entered tokens
}

// New returns a manager with no rights stored; Load reads them.
func New(store *config.Store) *Manager {
	return &Manager{store: store, settings: Settings{Apps: map[string]App{}}, used: map[string]time.Time{}}
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Load reads the rights from /config. A missing file means none.
func (m *Manager) Load() error {
	var s Settings
	err := m.store.ReadJSON(configFile, &s)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if s.Apps == nil {
		s.Apps = map[string]App{}
	}
	m.mu.Lock()
	m.settings = s
	m.mu.Unlock()
	return nil
}

// Settings returns a copy of the rights.
func (m *Manager) Settings() Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return copySettings(m.settings)
}

func copySettings(s Settings) Settings {
	out := Settings{NewApps: s.NewApps, Apps: make(map[string]App, len(s.Apps)), PerUser: s.PerUser, NewAppsUsers: slices.Clone(s.NewAppsUsers)}
	for name, app := range s.Apps {
		app.Users = slices.Clone(app.Users)
		out.Apps[name] = app
	}
	return out
}

// Set stores new rights. Users that are not normal accounts right now
// (unknown, owners, administrators) are dropped from the ticks. The file
// is only written when something changed.
func (m *Manager) Set(s Settings) error {
	switch s.NewApps {
	case "", NewAppsAdmins:
		s.NewApps = ""
	case NewAppsEveryone:
	default:
		return fmt.Errorf("%w: new apps", ErrInvalid)
	}
	clean := Settings{NewApps: s.NewApps, Apps: map[string]App{}, PerUser: s.PerUser}
	if s.PerUser {
		clean.NewAppsUsers = []string{}
		for _, user := range s.NewAppsUsers {
			if account, ok := m.lookup(user); ok && !account.Admin && !slices.Contains(clean.NewAppsUsers, user) {
				clean.NewAppsUsers = append(clean.NewAppsUsers, user)
			}
		}
		sort.Strings(clean.NewAppsUsers)
	}
	for name, app := range s.Apps {
		if !appRE.MatchString(name) {
			return fmt.Errorf("%w: app name %q", ErrInvalid, name)
		}
		users := []string{}
		for _, user := range app.Users {
			if account, ok := m.lookup(user); ok && !account.Admin && !slices.Contains(users, user) {
				users = append(users, user)
			}
		}
		sort.Strings(users)
		clean.Apps[name] = App{Users: users, NoGate: app.NoGate}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if reflect.DeepEqual(clean, m.settings) {
		return nil
	}
	if err := m.store.WriteJSON(configFile, clean); err != nil {
		return err
	}
	m.settings = clean
	return nil
}

func (m *Manager) lookup(name string) (Account, bool) {
	if m.Lookup == nil || name == "" {
		return Account{}, false
	}
	return m.Lookup(name)
}

// Allows tells whether an account may use an app now: it exists, is not
// locked, and is an owner or administrator or has the app's tick (or the
// app has no entry and new apps go to everyone).
func (m *Manager) Allows(user, app string) bool {
	account, ok := m.lookup(user)
	if !ok || account.Disabled {
		return false
	}
	if account.Admin {
		return true
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.settings.Apps[app]
	if !ok {
		return m.settings.NewAppsFor(user)
	}
	return slices.Contains(entry.Users, user)
}

// Gated tells whether page loads in a browser need the TitanOS login for
// an app ("Anmeldung im Browser prüfen", on unless switched off).
func (m *Manager) Gated(app string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return !m.settings.Apps[app].NoGate
}

// signingKey returns the key for the tokens, made and stored on first
// need. If it cannot be stored it still serves until titand restarts.
func (m *Manager) signingKey() []byte {
	m.mu.RLock()
	key := m.key
	m.mu.RUnlock()
	if key != nil {
		return key
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.key != nil {
		return m.key
	}
	data, err := m.store.Read(keyFile)
	if err == nil {
		if decoded, err := hex.DecodeString(strings.TrimSpace(string(data))); err == nil && len(decoded) >= 32 {
			m.key = decoded
			return m.key
		}
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err) // crypto/rand does not fail on Linux
	}
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		// Only a missing (or broken) key is replaced; a stick that cannot
		// be read keeps its key for the next start.
		if werr := m.store.Write(keyFile, []byte(hex.EncodeToString(key)+"\n")); werr != nil {
			slog.Warn("key for app windows not saved", "err", werr)
		}
	}
	m.key = key
	return m.key
}

// token is kind|user|app|expiry|nonce|session, base64url, a dot and its
// HMAC. session is the ID of the TitanOS login (auth.SessionID) the pass
// was made for: the pass ends with that login.
func (m *Manager) sign(kind, user, app, session string, expiry time.Time) string {
	nonce := make([]byte, 12)
	_, _ = rand.Read(nonce) // crypto/rand does not fail on Linux
	payload := strings.Join([]string{kind, user, app, strconv.FormatInt(expiry.Unix(), 10), base64.RawURLEncoding.EncodeToString(nonce), session}, "|")
	body := base64.RawURLEncoding.EncodeToString([]byte(payload))
	mac := hmac.New(sha256.New, m.signingKey())
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// pass is what a verified token says.
type pass struct {
	user, nonce, session string
	expiry               time.Time
}

// verify checks a token of a kind for an app: signed, not expired, made
// for a login that still exists and is not locked.
func (m *Manager) verify(kind, token, app string) (pass, bool) {
	body, sig, ok := strings.Cut(token, ".")
	if !ok || len(token) > 1024 {
		return pass{}, false
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return pass{}, false
	}
	mac := hmac.New(sha256.New, m.signingKey())
	mac.Write([]byte(body))
	if !hmac.Equal(got, mac.Sum(nil)) {
		return pass{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return pass{}, false
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 6 || parts[0] != kind || parts[2] != app || parts[1] == "" || parts[5] == "" {
		return pass{}, false
	}
	unix, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return pass{}, false
	}
	p := pass{user: parts[1], expiry: time.Unix(unix, 0), nonce: parts[4], session: parts[5]}
	if !m.now().Before(p.expiry) {
		return pass{}, false
	}
	if m.SessionUsable == nil || !m.SessionUsable(p.session, p.user) {
		return pass{}, false
	}
	return p, true
}

// EnterToken returns a token that lets the account's browser in to the
// app once, within EnterTTL; session is the ID of the TitanOS login that
// asked (auth.SessionID). The caller checks the right first.
func (m *Manager) EnterToken(user, app, session string) (string, time.Time) {
	expiry := m.now().Add(EnterTTL)
	return m.sign("enter", user, app, session, expiry), expiry
}

// Enter takes a window token for an app: valid, not used before, its
// login still there and not locked, and the account still allowed. It
// returns the value of the browser's cookie, bound to the same login, and
// how long it holds.
func (m *Manager) Enter(token, app string) (cookie string, ttl time.Duration, ok bool) {
	p, ok := m.verify("enter", token, app)
	if !ok || !m.use(p.nonce, p.expiry) || !m.Allows(p.user, app) {
		return "", 0, false
	}
	return m.sign("gate", p.user, app, p.session, m.now().Add(CookieTTL)), CookieTTL, true
}

// use marks a nonce as used; false if it was before.
func (m *Manager) use(nonce string, expiry time.Time) bool {
	m.usedMu.Lock()
	defer m.usedMu.Unlock()
	now := m.now()
	for n, until := range m.used {
		if !now.Before(until) {
			delete(m.used, n)
		}
	}
	if _, seen := m.used[nonce]; seen {
		return false
	}
	m.used[nonce] = expiry
	return true
}

// Admit tells whether the cookie of a browser lets it in to an app now:
// signed for the app, not expired, the TitanOS login it was made for
// still there and not locked (logging out, a password change that ends
// the sessions, expiry and the lock screen shut the app as well), and its
// account still allowed.
func (m *Manager) Admit(cookie, app string) bool {
	p, ok := m.verify("gate", cookie, app)
	return ok && m.Allows(p.user, app)
}
