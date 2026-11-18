// Package devices keeps the device passwords of T-Kalender and Kontakte
// (TECHNIK 3): one per iPhone or Mac, made on the web page with the
// account password, shown once, stored as SHA-256 in
// /config/users/<account>/devices.json. They open /caldav/ and /carddav/
// of their account and nothing else; the account password, the WebDAV
// password and API tokens are not taken there.
//
// The stick is read once per account (and again only when the account's
// creation time, password hash or second factor changed) and written only
// when a device is made, revoked or deleted: a device sends its password
// with every request, and /config is never read or written for what a
// device repeats.
//
// The owner record of devices.json carries a fingerprint of the account's
// password hash and TOTP secret (Sicherheitsdurchsicht S-25): a file whose
// fingerprint is not that of the account now – the password changed while
// RevokeAll did not reach the stick, or titand stopped in between – has
// all its devices revoked when it is read, never accepted.
//
// The DAV handler (davsync) takes a *Manager and calls Authenticate for
// every request, WriteChallenge for a refusal and Synced after an answer.
package devices

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// Limits of TECHNIK 3.1 and 3.2.
const (
	// Max is the number of devices per account.
	Max = 20
	// MaxRevoked and RevokedAge bound the hashes of revoked devices.
	MaxRevoked = 50
	RevokedAge = 90 * 24 * time.Hour
	// MaxPassword: a longer password is refused before it is hashed.
	MaxPassword = 128
	// MaxName is the length of a device name in characters.
	MaxName = 64
)

// Kinds of a sync (Synced, lastKind).
const (
	CalDAV  = "caldav"
	CardDAV = "carddav"
)

// alphabet: lower case and digits without 0 1 l o (32 characters, 5 bits
// each); typed on an iPhone, so nothing to mix up and no shift key.
const alphabet = "23456789abcdefghijkmnpqrstuvwxyz"

// passwordGroups of passwordGroup characters: 28 characters, 140 bits.
const (
	passwordGroups = 7
	passwordGroup  = 4
)

var (
	// ErrInvalidName: empty, longer than 64 characters, or with control
	// or invisible characters (400).
	ErrInvalidName = errors.New("devices: invalid device name")
	// ErrNameTaken: the account has a device of this name (409).
	ErrNameTaken = errors.New("devices: a device of this name exists")
	// ErrTooMany: the account has 20 devices (409 device_limit).
	ErrTooMany = errors.New("devices: at most 20 devices")
	// ErrNotFound: no device of this id (404).
	ErrNotFound = errors.New("devices: no such device")
	// ErrUnavailable: devices.json is not readable now (stick away; 503).
	ErrUnavailable = errors.New("devices: device passwords not readable")
)

// Config is the part of config.Store the manager uses (tests count it).
type Config interface {
	Read(name string) ([]byte, error)
	Write(name string, data []byte) error
}

// Accounts are the accounts (auth.Users).
type Accounts interface {
	Get(name string) (auth.User, bool)
}

// SeenStore keeps "last synced" on the volume (pim/store: SaveSeen and
// LoadSeen of .titanos/calendar/<account>/devices-seen.json).
type SeenStore interface {
	SaveSeen(account string, v any) error
	LoadSeen(account string, v any) error
}

// File is the path of an account's device passwords below /config.
func File(account string) string { return "users/" + account + "/devices.json" }

// file is devices.json.
type file struct {
	Owner   owner     `json:"owner"`
	Devices []device  `json:"devices"`
	Revoked []revoked `json:"revoked"`
}

type owner struct {
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
	// Creds is the fingerprint of the account's credentials when the file
	// was written (credentials). Missing in a file of a 1.11 test build:
	// taken as matching once and written with the next write.
	Creds string `json:"creds,omitempty"`
}

type device struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	SHA256  string    `json:"sha256"`
	Created time.Time `json:"created"`
}

type revoked struct {
	SHA256 string    `json:"sha256"`
	At     time.Time `json:"at"`
}

// Device is a device as the page shows it (never with its hash).
type Device struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Created  time.Time  `json:"created"`
	LastSync *time.Time `json:"lastSync"`
	LastKind string     `json:"lastKind"`
	// LastSyncBeforeRestart: lastSync was read from the volume after a
	// restart ("vor dem Neustart: 12.11.").
	LastSyncBeforeRestart bool `json:"lastSyncBeforeRestart,omitempty"`
}

// Created is a new device with its password, shown once.
type Created struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	User     string `json:"user"`
	Password string `json:"password"`
}

// Manager holds the device passwords of all accounts.
type Manager struct {
	config  Config
	users   Accounts
	limiter func() *auth.Limiter
	seen    SeenStore

	// RemoteIP gives the address of a request for the limiter and the
	// journal; nil: the host of RemoteAddr.
	RemoteIP func(r *http.Request) string
	// Now is the clock; nil: time.Now.
	Now func() time.Time

	mu sync.Mutex
	// known are the device passwords as read once per account.
	known map[string]*state
	// synced is "last synced" per account and device (RAM, 3.5).
	synced     map[string]map[string]*seen
	seenLoaded map[string]bool
	// journal: when a revoked device was last written to the journal.
	journal map[string]time.Time
	// dummy are compared for an unknown account (the same work as for a
	// known one).
	dummy [][]byte
}

// state is the device passwords of one account, bound to the account's
// creation time and credentials (password hash and TOTP secret).
type state struct {
	created time.Time
	creds   string
	file    file
	sums    [][]byte // of file.Devices
	revoked [][]byte // of file.Revoked
}

// New returns a manager; seen may be nil (no "last synced" on a volume).
func New(config Config, users Accounts, limiter func() *auth.Limiter, seenStore SeenStore) *Manager {
	m := &Manager{config: config, users: users, limiter: limiter, seen: seenStore,
		known: map[string]*state{}, synced: map[string]map[string]*seen{}, seenLoaded: map[string]bool{}, journal: map[string]time.Time{}}
	for range Max + MaxRevoked {
		b := make([]byte, sha256.Size)
		_, _ = rand.Read(b) // crypto/rand does not fail on Linux
		m.dummy = append(m.dummy, b)
	}
	return m
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Manager) remoteIP(r *http.Request) string {
	if m.RemoteIP != nil {
		return m.RemoteIP(r)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ValidName reports whether a device name may be used: 1–64 characters
// after trimming spaces, none of them a control or invisible character
// (format characters like zero-width space, bidi marks, BOM).
func ValidName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > MaxName {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == utf8.RuneError ||
			(unicode.IsSpace(r) && r != ' ') || unicode.Is(unicode.Co, r) {
			return false
		}
	}
	return true
}

// NewPassword makes a device password: seven groups of four characters
// of the alphabet, joined with "-" (140 bits from crypto/rand).
func NewPassword() (string, error) {
	raw := make([]byte, passwordGroups*passwordGroup)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	var b strings.Builder
	for i, c := range raw {
		if i > 0 && i%passwordGroup == 0 {
			b.WriteByte('-')
		}
		b.WriteByte(alphabet[int(c)%len(alphabet)]) // 256 is a multiple of 32: uniform
	}
	return b.String(), nil
}

// Normalize is what is hashed: without dashes and spaces, in lower case
// (typed on an iPhone with the first letter in upper case, say).
func Normalize(password string) string {
	password = strings.NewReplacer("-", "", " ", "").Replace(password)
	return strings.ToLower(password)
}

// Sum is the SHA-256 of the normalized password.
func Sum(password string) []byte {
	sum := sha256.Sum256([]byte(Normalize(password)))
	return sum[:]
}

// credentials is the fingerprint of what revokes the devices of an
// account when it changes: the password hash and the TOTP secret (the
// first 16 bytes of a SHA-256, hex; not the secret itself, and nothing a
// device password could be guessed from).
func credentials(user auth.User) string {
	sum := sha256.Sum256([]byte("titanos-devices-creds\x00" + user.PasswordHash + "\x00" + user.TOTPSecret))
	return hex.EncodeToString(sum[:16])
}

// readLocked reads devices.json of an account (m.mu held): an empty file
// for none, a damaged one or one of an earlier account of the name.
func (m *Manager) readLocked(user auth.User) (file, error) {
	var f file
	data, err := m.config.Read(File(user.Name))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		slog.Warn("Calendar: device passwords not readable", "user", user.Name, "err", err)
		return file{}, ErrUnavailable
	case json.Unmarshal(data, &f) != nil:
		// Damaged: no devices, until one is made.
		slog.Warn("Calendar: device passwords damaged", "user", user.Name)
		f = file{}
	}
	if f.Owner.Name != user.Name || !f.Owner.Created.Equal(user.Created) {
		// Of an earlier account of the name (or none): not read.
		f = file{}
	}
	return f, nil
}

// stateLocked returns the device passwords of an account (m.mu held):
// from RAM, else read once from the stick. ErrUnavailable: not readable
// now; nothing is kept, and the next request asks again.
func (m *Manager) stateLocked(user auth.User) (*state, error) {
	creds := credentials(user)
	if st := m.known[user.Name]; st != nil && st.created.Equal(user.Created) && st.creds == creds {
		return st, nil
	}
	f, err := m.readLocked(user)
	if err != nil {
		return nil, err
	}
	if f.Owner.Creds != "" && f.Owner.Creds != creds && len(f.Devices) > 0 {
		// Written for other credentials, and RevokeAll never reached the
		// stick (not readable then, not written, or titand stopped before
		// it): the devices count as revoked – refused without counting, and
		// moved to revoked with the next write.
		at := m.now().UTC()
		f.Revoked = slices.Clone(f.Revoked)
		for _, d := range f.Devices {
			f.Revoked = append(f.Revoked, revoked{SHA256: d.SHA256, At: at})
		}
		f.Devices = nil
		slog.Warn("Calendar: device passwords of other credentials revoked", "user", user.Name)
	}
	f.Revoked = m.prune(f.Revoked)
	st := m.build(user, f)
	m.known[user.Name] = st
	return st, nil
}

// build makes the state of a file for an account.
func (m *Manager) build(user auth.User, f file) *state {
	f.Owner = owner{Name: user.Name, Created: user.Created, Creds: credentials(user)}
	st := &state{created: user.Created, creds: f.Owner.Creds, file: f}
	for _, d := range f.Devices {
		sum, _ := hex.DecodeString(d.SHA256)
		st.sums = append(st.sums, sum)
	}
	for _, r := range f.Revoked {
		sum, _ := hex.DecodeString(r.SHA256)
		st.revoked = append(st.revoked, sum)
	}
	return st
}

// prune drops revoked hashes older than 90 days and keeps the newest 50.
func (m *Manager) prune(list []revoked) []revoked {
	cutoff := m.now().Add(-RevokedAge)
	out := make([]revoked, 0, len(list))
	for _, r := range list {
		if r.At.After(cutoff) && r.SHA256 != "" {
			out = append(out, r)
		}
	}
	slices.SortStableFunc(out, func(a, b revoked) int { return a.At.Compare(b.At) })
	if len(out) > MaxRevoked {
		out = out[len(out)-MaxRevoked:]
	}
	return out
}

// saveLocked writes the file of an account and takes it into RAM (m.mu
// held). On a failed write RAM keeps the state before.
func (m *Manager) saveLocked(user auth.User, f file) error {
	f.Owner = owner{Name: user.Name, Created: user.Created, Creds: credentials(user)}
	f.Revoked = m.prune(f.Revoked)
	if f.Devices == nil {
		f.Devices = []device{}
	}
	if f.Revoked == nil {
		f.Revoked = []revoked{}
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := m.config.Write(File(user.Name), data); err != nil {
		return err
	}
	m.known[user.Name] = m.build(user, f)
	return nil
}

// List returns the devices of an account with "last synced"; the first
// call after a restart reads devices-seen.json from the volume (if it is
// there; without it lastSync stays empty).
func (m *Manager) List(user auth.User) ([]Device, error) {
	m.loadSeen(user.Name)
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.stateLocked(user)
	if err != nil {
		return nil, err
	}
	out := []Device{}
	for _, d := range st.file.Devices {
		dev := Device{ID: d.ID, Name: d.Name, Created: d.Created}
		if s := m.synced[user.Name][d.ID]; s != nil {
			at := s.At
			dev.LastSync, dev.LastKind, dev.LastSyncBeforeRestart = &at, s.Kind, s.before
		}
		out = append(out, dev)
	}
	return out, nil
}

// Available tells whether the account may make one more device and the
// name is free; Create checks again.
func (m *Manager) Available(user auth.User, name string) error {
	if !ValidName(name) {
		return ErrInvalidName
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.stateLocked(user)
	if err != nil {
		return err
	}
	return st.free(strings.TrimSpace(name))
}

func (st *state) free(name string) error {
	if len(st.file.Devices) >= Max {
		return ErrTooMany
	}
	for _, d := range st.file.Devices {
		if strings.EqualFold(d.Name, name) {
			return ErrNameTaken
		}
	}
	return nil
}

// Create makes a device and its password. The caller has checked the
// account password and second factor in a web session; the password is
// returned once and never kept, logged or shown again.
func (m *Manager) Create(user auth.User, name string) (Created, error) {
	if !ValidName(name) {
		return Created{}, ErrInvalidName
	}
	name = strings.TrimSpace(name)
	password, err := NewPassword()
	if err != nil {
		return Created{}, err
	}
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return Created{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.stateLocked(user)
	if err != nil {
		return Created{}, err
	}
	if err := st.free(name); err != nil {
		return Created{}, err
	}
	d := device{ID: "d_" + hex.EncodeToString(id), Name: name, SHA256: hex.EncodeToString(Sum(password)), Created: m.now().UTC()}
	next := st.file
	next.Devices = append(slices.Clone(st.file.Devices), d)
	if err := m.saveLocked(user, next); err != nil {
		return Created{}, err
	}
	slog.Info("Calendar: device password made", "user", user.Name, "device", d.ID)
	return Created{ID: d.ID, Name: d.Name, User: user.Name, Password: password}, nil
}

// Revoke deletes a device; its hash goes to the revoked ones, so that the
// device that keeps asking is refused without counting (3.2). It holds at
// once.
func (m *Manager) Revoke(user auth.User, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.stateLocked(user)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(st.file.Devices, func(d device) bool { return d.ID == id })
	if i < 0 {
		return ErrNotFound
	}
	next := st.file
	next.Devices = slices.Delete(slices.Clone(st.file.Devices), i, i+1)
	next.Revoked = append(slices.Clone(st.file.Revoked), revoked{SHA256: st.file.Devices[i].SHA256, At: m.now().UTC()})
	if err := m.saveLocked(user, next); err != nil {
		return err
	}
	delete(m.synced[user.Name], id)
	slog.Info("Calendar: device password revoked", "user", user.Name, "device", id)
	return nil
}

// RevokeAll revokes every device of an account: its password changed, its
// second factor was reset (here or through the cluster). The hashes go to
// the revoked ones (never cleared), so the old devices keep getting 401
// without locking the account's new device. Called after the account was
// changed; when the stick cannot be read or written, the revocation holds
// in RAM and, after a restart, through the fingerprint of the credentials
// in the owner record (S-25).
func (m *Manager) RevokeAll(name string) {
	user, ok := m.users.Get(name)
	if !ok {
		m.Forget(name)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.synced, name)
	// The stick, not RAM: a state read for the old password hash is not
	// the current one.
	// Read as stored (stateLocked would already revoke them in RAM for
	// the new credentials, and nothing would be written).
	delete(m.known, name)
	f, err := m.readLocked(user)
	if err != nil {
		// Not readable now: the file keeps the fingerprint of the old
		// credentials, so its devices are revoked when it is read (S-25).
		slog.Error("Calendar: device passwords not revoked now: not readable; revoked when read", "user", name, "err", err)
		return
	}
	if len(f.Devices) == 0 {
		return
	}
	next := f
	next.Revoked = slices.Clone(f.Revoked)
	at := m.now().UTC()
	for _, d := range f.Devices {
		next.Revoked = append(next.Revoked, revoked{SHA256: d.SHA256, At: at})
	}
	next.Devices = []device{}
	if err := m.saveLocked(user, next); err != nil {
		slog.Error("Calendar: revoked device passwords not saved; revoked in RAM, and by the fingerprint after a restart", "user", name, "err", err)
		next.Revoked = m.prune(next.Revoked)
		m.known[name] = m.build(user, next)
		return
	}
	slog.Info("Calendar: all device passwords revoked", "user", name, "devices", len(f.Devices))
}

// Rebind keeps the devices of an account whose second factor the person
// switched on or off in its own session, with password and code (before is
// the account as it was). Changed credentials revoke the devices
// otherwise (stateLocked); this writes the new fingerprint instead, if the
// file is still that of before. Not written (stick away): the devices hold
// in RAM and are revoked after a restart – the careful side.
func (m *Manager) Rebind(before auth.User) {
	user, ok := m.users.Get(before.Name)
	if !ok || !user.Created.Equal(before.Created) || credentials(user) == credentials(before) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// The stick, not RAM: a request in between may have revoked them in
	// RAM for the new credentials.
	delete(m.known, user.Name)
	f, err := m.readLocked(user)
	if err != nil {
		slog.Error("Calendar: device passwords not kept after a change of the second factor: not readable", "user", user.Name, "err", err)
		return
	}
	if f.Owner.Creds != "" && f.Owner.Creds != credentials(before) {
		return // already for other credentials: stays revoked
	}
	if len(f.Devices) == 0 && len(f.Revoked) == 0 {
		return
	}
	f.Revoked = m.prune(f.Revoked)
	if err := m.saveLocked(user, f); err != nil {
		slog.Error("Calendar: device passwords kept in RAM only after a change of the second factor", "user", user.Name, "err", err)
		m.known[user.Name] = m.build(user, f)
	}
}

// Forget drops what RAM knows of an account: deleted (its devices.json
// goes with users/<name>) or made anew.
func (m *Manager) Forget(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.known, name)
	delete(m.synced, name)
	delete(m.seenLoaded, name)
	prefix := name + "/"
	for key := range m.journal {
		if strings.HasPrefix(key, prefix) {
			delete(m.journal, key)
		}
	}
}
