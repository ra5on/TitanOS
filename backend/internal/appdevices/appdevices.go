// Package appdevices keeps the Android apps of an account (PLAN 116,
// docs/ANDROID-APP.md): one key pair per phone and app, made in the
// Android Keystore, of which the NAS keeps the public key in
// /config/users/<account>/apps.json. A paired app signs a challenge and
// gets an ordinary session of the account; the session is what every
// other request carries, so the app is a session like the browser, not an
// API token.
//
// Pairing runs in RAM: the browser starts it (a code for five minutes,
// once), the app claims it with its public key, the browser confirms it,
// and only then is the device written. The stick is written when a device
// is paired, ended or all of an account's are revoked, never while an app
// is used.
//
// Like the device passwords (pim/devices, S-25) the file carries a
// fingerprint of the account's password hash and TOTP secret: a file of
// other credentials counts as having no devices, so a changed password
// ends every app even if the revocation never reached the stick.
package appdevices

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// Limits.
const (
	// Max is the number of paired apps per account.
	Max = 20
	// MaxName is the length of a device name in characters.
	MaxName = 64
	// PairTTL is how long a pairing code is valid; claiming it gives the
	// browser as long again to confirm.
	PairTTL = 5 * time.Minute
	// ConfirmedTTL is how long a confirmed pairing waits for the app to
	// fetch the result.
	ConfirmedTTL = 2 * time.Minute
	// ChallengeTTL is how long a challenge for a session is valid.
	ChallengeTTL = 2 * time.Minute
	// MaxPending is the number of open pairings per account.
	MaxPending = 3
	// SiblingTTL is how long the code for the other app on the same phone
	// holds: the paired app hands it over at once.
	SiblingTTL = 2 * time.Minute
	// maxChallenges bounds the challenges in RAM (they are handed out
	// without a login).
	maxChallenges = 1000
)

// Apps are the apps that can be paired: the two phone apps and the watch
// (watch.go).
var Apps = []string{"titanos", "agent", WatchApp}

// phoneApps are the apps of a phone; a sibling pairing is only between them.
var phoneApps = []string{"titanos", "agent"}

// Pairing states.
const (
	Waiting   = "waiting"
	Claimed   = "claimed"
	Confirmed = "confirmed"
	Rejected  = "rejected"
	Expired   = "expired"
)

// Texts the app signs (docs/ANDROID-APP.md).
const (
	pairText    = "titanos-pair\n"
	sessionText = "titanos-app-session\n"
)

// manualAlphabet: upper case and digits without 0 1 I O (32 characters).
const manualAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

var (
	ErrInvalidName = errors.New("appdevices: invalid device name")
	ErrInvalidApp  = errors.New("appdevices: unknown app")
	ErrInvalidKey  = errors.New("appdevices: not a P-256 public key")
	ErrBadCode     = errors.New("appdevices: unknown or expired code")
	ErrClaimed     = errors.New("appdevices: code already used")
	ErrBadState    = errors.New("appdevices: pairing is not waiting for this")
	ErrTooMany     = errors.New("appdevices: at most 20 apps")
	ErrTooManyOpen = errors.New("appdevices: too many open pairings")
	ErrNotFound    = errors.New("appdevices: no such app or pairing")
	ErrUnknown     = errors.New("appdevices: unknown, ended or expired app")
	ErrSignature   = errors.New("appdevices: signature does not match")
	ErrUnavailable = errors.New("appdevices: apps.json not readable")
	ErrSameApp     = errors.New("appdevices: a sibling is the other app")
)

// Config is the part of config.Store the manager uses.
type Config interface {
	Read(name string) ([]byte, error)
	Write(name string, data []byte) error
}

// Accounts are the accounts (auth.Users).
type Accounts interface {
	Get(name string) (auth.User, bool)
}

// File is the path of an account's apps below /config.
func File(account string) string { return "users/" + account + "/apps.json" }

// Device is a paired app.
type Device struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	App     string    `json:"app"`
	Created time.Time `json:"created"`
	// Key is the public key, SPKI DER in base64.
	Key string `json:"key"`
}

type file struct {
	Owner   owner    `json:"owner"`
	Devices []Device `json:"devices"`
}

type owner struct {
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
	Creds   string    `json:"creds"`
}

type state struct {
	created time.Time
	creds   string
	devices []Device
}

// Pairing is one pairing in RAM.
type Pairing struct {
	ID      string
	User    string
	Code    string
	Manual  string
	Expires time.Time
	State   string
	Name    string
	App     string
	Device  string
	key     *ecdsa.PublicKey
	keyText string
	// A sibling pairing is started by a paired app for the other app on
	// the same phone: confirmed by the claim itself, from the same address,
	// while the account's credentials and the starting app are unchanged.
	sibling bool
	from    string
	address string
	creds   string
}

type challenge struct {
	user, device string
	expires      time.Time
}

// Manager keeps the apps of all accounts.
type Manager struct {
	mu         sync.Mutex
	config     Config
	users      Accounts
	stay       func() int
	now        func() time.Time
	known      map[string]*state
	pairings   map[string]*Pairing
	byCode     map[string]string
	challenges map[string]challenge
	watches    map[string]*WatchPairing
	// watchStarts are the recent starts of a watch pairing per address.
	watchStarts map[string][]time.Time
}

// New makes a manager. stay is the setting "stay logged in" in days
// (auth.Sessions.Stay): 0 off, auth.StayForever no end.
func New(config Config, users Accounts, stay func() int) *Manager {
	return &Manager{config: config, users: users, stay: stay, now: time.Now, known: map[string]*state{},
		pairings: map[string]*Pairing{}, byCode: map[string]string{}, challenges: map[string]challenge{}, watches: map[string]*WatchPairing{}, watchStarts: map[string][]time.Time{}}
}

// SetClockForTest replaces the clock.
func (m *Manager) SetClockForTest(now func() time.Time) { m.now = now }

// credentials is the fingerprint of what ends the apps of an account when
// it changes: the password hash and the TOTP secret.
func credentials(user auth.User) string {
	sum := sha256.Sum256([]byte("titanos-apps-creds\x00" + user.PasswordHash + "\x00" + user.TOTPSecret))
	return hex.EncodeToString(sum[:16])
}

func randomText(n int) string {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func manualCode() string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	var b strings.Builder
	for _, c := range raw {
		b.WriteByte(manualAlphabet[int(c)%len(manualAlphabet)])
	}
	return b.String()
}

// normalCode makes a typed code comparable: the manual code without
// dashes and spaces in upper case; a long code stays as it is.
func normalCode(code string) string {
	code = strings.TrimSpace(code)
	short := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(code))
	if len(short) == 8 {
		return short
	}
	return code
}

// ValidName reports whether a device name can be kept: 1 to 64
// characters, no control or invisible characters.
func ValidName(name string) bool {
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > MaxName || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

// ParseKey reads a P-256 public key (SPKI DER, base64 with or without
// padding, standard or URL alphabet).
func ParseKey(text string) (*ecdsa.PublicKey, error) {
	text = strings.TrimRight(strings.TrimSpace(text), "=")
	var raw []byte
	var err error
	if strings.ContainsAny(text, "-_") {
		raw, err = base64.RawURLEncoding.DecodeString(text)
	} else {
		raw, err = base64.RawStdEncoding.DecodeString(text)
	}
	if err != nil || len(raw) > 512 {
		return nil, ErrInvalidKey
	}
	pub, err := x509.ParsePKIXPublicKey(raw)
	if err != nil {
		return nil, ErrInvalidKey
	}
	key, ok := pub.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, ErrInvalidKey
	}
	return key, nil
}

func verify(key *ecdsa.PublicKey, text, signature string) bool {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		sig, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(strings.TrimSpace(signature), "="))
	}
	if err != nil || len(sig) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(text))
	return ecdsa.VerifyASN1(key, sum[:], sig)
}

// ---------- devices on the stick ----------

func (m *Manager) readLocked(user auth.User) (file, error) {
	var f file
	data, err := m.config.Read(File(user.Name))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		slog.Warn("Android apps not readable", "user", user.Name, "err", err)
		return file{}, ErrUnavailable
	case json.Unmarshal(data, &f) != nil:
		slog.Warn("Android apps damaged", "user", user.Name)
		f = file{}
	}
	if f.Owner.Name != user.Name || !f.Owner.Created.Equal(user.Created) {
		f = file{}
	}
	return f, nil
}

func (m *Manager) stateLocked(user auth.User) (*state, error) {
	creds := credentials(user)
	if st := m.known[user.Name]; st != nil && st.created.Equal(user.Created) && st.creds == creds {
		return st, nil
	}
	f, err := m.readLocked(user)
	if err != nil {
		return nil, err
	}
	if f.Owner.Creds != creds && len(f.Devices) > 0 {
		// Written for other credentials: the password or the second
		// factor changed and RevokeAll did not reach the stick.
		slog.Warn("Android apps of other credentials ended", "user", user.Name)
		f.Devices = nil
	}
	st := &state{created: user.Created, creds: creds, devices: f.Devices}
	m.known[user.Name] = st
	return st, nil
}

func (m *Manager) saveLocked(user auth.User, devices []Device) error {
	if devices == nil {
		devices = []Device{}
	}
	f := file{Owner: owner{Name: user.Name, Created: user.Created, Creds: credentials(user)}, Devices: devices}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := m.config.Write(File(user.Name), data); err != nil {
		return err
	}
	m.known[user.Name] = &state{created: user.Created, creds: f.Owner.Creds, devices: devices}
	return nil
}

// Expires is when a device ends by the setting "stay logged in", nil for
// never.
func (m *Manager) Expires(d Device) *time.Time {
	days := 0
	if m.stay != nil {
		days = m.stay()
	}
	var at time.Time
	switch {
	case days == auth.StayForever:
		return nil
	case days > 0:
		at = d.Created.Add(time.Duration(days) * 24 * time.Hour)
	default:
		at = d.Created.Add(auth.SessionMaxAge)
	}
	return &at
}

func (m *Manager) live(d Device) bool {
	at := m.Expires(d)
	return at == nil || m.now().Before(*at)
}

// List returns the paired apps of an account that have not run out.
func (m *Manager) List(user auth.User) ([]Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.stateLocked(user)
	if err != nil {
		return nil, err
	}
	out := []Device{}
	for _, d := range st.devices {
		if m.live(d) {
			out = append(out, d)
		}
	}
	return out, nil
}

// Device returns a paired app of an account that has not run out.
func (m *Manager) Device(user auth.User, id string) (Device, error) {
	list, err := m.List(user)
	if err != nil {
		return Device{}, err
	}
	for _, d := range list {
		if d.ID == id {
			return d, nil
		}
	}
	return Device{}, ErrNotFound
}

// Cached says from RAM only whether a device of an account is paired and
// has not run out; known is false when the account's apps were not read
// since the start (the caller then keeps what it has). The account must
// be the same and its credentials unchanged, like stateLocked checks.
func (m *Manager) Cached(user auth.User, id string) (live, known bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.known[user.Name]
	if st == nil {
		return false, false
	}
	if !st.created.Equal(user.Created) || st.creds != credentials(user) {
		// Another account of the name or changed credentials: the apps
		// of the file end when it is read next.
		return false, true
	}
	for _, d := range st.devices {
		if d.ID == id {
			return m.live(d), true
		}
	}
	return false, true
}

// Delete ends one app of an account. The caller ends its sessions.
func (m *Manager) Delete(user auth.User, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.stateLocked(user)
	if err != nil {
		return err
	}
	kept := make([]Device, 0, len(st.devices))
	found := false
	for _, d := range st.devices {
		if d.ID == id {
			found = true
			continue
		}
		kept = append(kept, d)
	}
	if !found {
		return ErrNotFound
	}
	return m.saveLocked(user, kept)
}

// RevokeAll ends every app of an account whose password changed or whose
// second factor was reset. If the stick cannot be written, the changed
// fingerprint ends them when the file is read next.
func (m *Manager) RevokeAll(name string) {
	user, ok := m.users.Get(name)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dropPairingsLocked(name)
	if !ok {
		delete(m.known, name)
		return
	}
	if err := m.saveLocked(user, nil); err != nil {
		slog.Warn("Android apps: revoking not written", "user", name, "err", err)
		delete(m.known, name)
	}
}

// Rebind keeps the apps of an account whose second factor the person
// switched on or off in its own session: the file is written with the new
// fingerprint. before is the account as it was.
func (m *Manager) Rebind(before auth.User) {
	user, ok := m.users.Get(before.Name)
	if !ok {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.stateLocked(before)
	if err != nil || len(st.devices) == 0 {
		delete(m.known, before.Name)
		return
	}
	if err := m.saveLocked(user, st.devices); err != nil {
		slog.Warn("Android apps: rebinding not written", "user", before.Name, "err", err)
	}
}

// Forget drops what is in RAM of an account that was deleted.
func (m *Manager) Forget(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.known, name)
	m.dropPairingsLocked(name)
}

// ---------- pairing ----------

func (m *Manager) pruneLocked(now time.Time) {
	for id, p := range m.pairings {
		if now.After(p.Expires) {
			delete(m.pairings, id)
			delete(m.byCode, p.Code)
			delete(m.byCode, p.Manual)
		}
	}
	for n, c := range m.challenges {
		if now.After(c.expires) {
			delete(m.challenges, n)
		}
	}
}

func (m *Manager) dropPairingsLocked(name string) {
	for id, p := range m.pairings {
		if p.User == name {
			delete(m.pairings, id)
			delete(m.byCode, p.Code)
			delete(m.byCode, p.Manual)
		}
	}
	for n, c := range m.challenges {
		if c.user == name {
			delete(m.challenges, n)
		}
	}
	m.dropWatchesLocked(name)
}

// Start begins a pairing for an account.
func (m *Manager) Start(user auth.User) (Pairing, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.pruneLocked(now)
	open := 0
	for _, p := range m.pairings {
		if p.User == user.Name && (p.State == Waiting || p.State == Claimed) {
			open++
		}
	}
	if open >= MaxPending {
		return Pairing{}, ErrTooManyOpen
	}
	p := &Pairing{ID: randomText(12), User: user.Name, Code: randomText(24), Expires: now.Add(PairTTL), State: Waiting}
	for {
		p.Manual = manualCode()
		if _, taken := m.byCode[p.Manual]; !taken {
			break
		}
	}
	m.pairings[p.ID] = p
	m.byCode[p.Code] = p.ID
	m.byCode[p.Manual] = p.ID
	return *p, nil
}

// Status returns a pairing of an account; an expired one reads as
// Expired until it is pruned.
func (m *Manager) Status(user auth.User, id string) (Pairing, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pairings[id]
	if p == nil || p.User != user.Name {
		return Pairing{}, ErrNotFound
	}
	out := *p
	if m.now().After(p.Expires) {
		out.State = Expired
	}
	return out, nil
}

// Cancel drops a pairing of an account.
func (m *Manager) Cancel(user auth.User, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pairings[id]
	if p == nil || p.User != user.Name {
		return ErrNotFound
	}
	delete(m.pairings, id)
	delete(m.byCode, p.Code)
	delete(m.byCode, p.Manual)
	return nil
}

// Reject refuses a claimed pairing.
func (m *Manager) Reject(user auth.User, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pairings[id]
	if p == nil || p.User != user.Name || m.now().After(p.Expires) {
		return ErrNotFound
	}
	if p.State != Claimed && p.State != Waiting {
		return ErrBadState
	}
	p.State = Rejected
	// The app may still read the answer for a while; the codes are no
	// longer of use.
	p.Expires = m.now().Add(ConfirmedTTL)
	return nil
}

// Confirm accepts a claimed pairing and writes the device.
func (m *Manager) Confirm(user auth.User, id string) (Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	p := m.pairings[id]
	if p == nil || p.User != user.Name || now.After(p.Expires) {
		return Device{}, ErrNotFound
	}
	if p.State != Claimed || p.sibling {
		return Device{}, ErrBadState
	}
	return m.confirmLocked(user, p, now)
}

// confirmLocked writes the device of a claimed pairing.
func (m *Manager) confirmLocked(user auth.User, p *Pairing, now time.Time) (Device, error) {
	st, err := m.stateLocked(user)
	if err != nil {
		return Device{}, err
	}
	kept := make([]Device, 0, len(st.devices)+1)
	for _, d := range st.devices {
		if m.live(d) {
			kept = append(kept, d)
		}
	}
	if len(kept) >= Max {
		return Device{}, ErrTooMany
	}
	d := Device{ID: randomText(12), Name: p.Name, App: p.App, Created: now.UTC(), Key: p.keyText}
	if err := m.saveLocked(user, append(kept, d)); err != nil {
		return Device{}, err
	}
	p.State, p.Device = Confirmed, d.ID
	p.Expires = now.Add(ConfirmedTTL)
	return d, nil
}

// Sibling starts a pairing for the other app on the same phone, asked by
// the paired app device of the account (its session). The code goes from
// app to app on the phone; whoever claims it from address with the right
// app is paired without the browser (docs/ANDROID-APP.md).
func (m *Manager) Sibling(user auth.User, device, app, address string) (Pairing, error) {
	// Never the watch: its claim would come from another address, and a
	// code confirmed by the claim alone is for the other app on the same
	// phone only (the watch is confirmed with its code, watch.go).
	known := false
	for _, a := range phoneApps {
		known = known || a == app
	}
	if !known {
		return Pairing{}, ErrInvalidApp
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.pruneLocked(now)
	st, err := m.stateLocked(user)
	if err != nil {
		return Pairing{}, err
	}
	var from *Device
	for i, d := range st.devices {
		if d.ID == device && m.live(d) {
			from = &st.devices[i]
		}
	}
	if from == nil {
		return Pairing{}, ErrUnknown
	}
	if from.App == app {
		return Pairing{}, ErrSameApp
	}
	if from.App == WatchApp {
		// A watch has no other app beside it.
		return Pairing{}, ErrInvalidApp
	}
	open := 0
	for _, p := range m.pairings {
		if p.User == user.Name && (p.State == Waiting || p.State == Claimed) {
			open++
		}
	}
	if open >= MaxPending {
		return Pairing{}, ErrTooManyOpen
	}
	p := &Pairing{ID: randomText(12), User: user.Name, Code: randomText(24), Expires: now.Add(SiblingTTL), State: Waiting, App: app,
		sibling: true, from: device, address: address, creds: credentials(user)}
	m.pairings[p.ID] = p
	m.byCode[p.Code] = p.ID
	return *p, nil
}

// Claim takes a code for an app: its name, which app and its public key.
// ErrBadCode is for the caller to count against the address.
// address is where the claim comes from; it counts only for a sibling
// pairing, which it also confirms.
func (m *Manager) Claim(code, name, app, key, address string) (Pairing, error) {
	if !ValidName(name) {
		return Pairing{}, ErrInvalidName
	}
	known := false
	for _, a := range Apps {
		known = known || a == app
	}
	if !known {
		return Pairing{}, ErrInvalidApp
	}
	pub, err := ParseKey(key)
	if err != nil {
		return Pairing{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.pruneLocked(now)
	p := m.pairings[m.byCode[normalCode(code)]]
	if p == nil {
		return Pairing{}, ErrBadCode
	}
	if p.State != Waiting {
		return Pairing{}, ErrClaimed
	}
	if p.sibling {
		return m.claimSiblingLocked(p, name, app, pub, key, address, now)
	}
	p.State, p.Name, p.App, p.key, p.keyText = Claimed, name, app, pub, strings.TrimSpace(key)
	p.Expires = now.Add(PairTTL)
	// Only the code the app sent stays usable for Wait.
	if normalCode(code) == p.Manual {
		delete(m.byCode, p.Code)
	} else {
		delete(m.byCode, p.Manual)
	}
	return *p, nil
}

// claimSiblingLocked pairs the other app of the phone. Anything off reads
// as an unknown code, and the code is gone: it is tried once.
func (m *Manager) claimSiblingLocked(p *Pairing, name, app string, pub *ecdsa.PublicKey, key, address string, now time.Time) (Pairing, error) {
	drop := func() {
		delete(m.pairings, p.ID)
		delete(m.byCode, p.Code)
	}
	user, ok := m.users.Get(p.User)
	if !ok || app != p.App || address == "" || address != p.address || credentials(user) != p.creds {
		drop()
		return Pairing{}, ErrBadCode
	}
	st, err := m.stateLocked(user)
	if err != nil {
		return Pairing{}, err
	}
	still := false
	for _, d := range st.devices {
		still = still || (d.ID == p.from && m.live(d))
	}
	if !still {
		drop()
		return Pairing{}, ErrBadCode
	}
	p.State, p.Name, p.key, p.keyText = Claimed, name, pub, strings.TrimSpace(key)
	if _, err := m.confirmLocked(user, p, now); err != nil {
		drop()
		return Pairing{}, err
	}
	return *p, nil
}

// Wait tells the app the state of its pairing; signature is over
// "titanos-pair\n<code>" with the key of the claim.
func (m *Manager) Wait(code, signature string) (Pairing, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.pruneLocked(now)
	typed := strings.TrimSpace(code)
	code = normalCode(code)
	p := m.pairings[m.byCode[code]]
	if p == nil || p.key == nil {
		return Pairing{}, ErrBadCode
	}
	sent := p.Code
	if code == p.Manual {
		sent = p.Manual
	}
	// The app signs the code as it sends it, a typed manual code with its
	// dash and case; the normal form counts as well.
	if !verify(p.key, pairText+typed, signature) && !verify(p.key, pairText+sent, signature) {
		return Pairing{}, ErrSignature
	}
	out := *p
	if p.State == Confirmed || p.State == Rejected {
		// Told once: the pairing is over.
		delete(m.pairings, p.ID)
		delete(m.byCode, p.Code)
		delete(m.byCode, p.Manual)
	}
	return out, nil
}

// ---------- sessions ----------

// Challenge hands out a nonce for an app to sign. It is given for any
// account and device, so that it tells nothing about them.
func (m *Manager) Challenge(account, device string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.pruneLocked(now)
	if len(m.challenges) >= maxChallenges {
		return "", ErrTooManyOpen
	}
	nonce := randomText(24)
	m.challenges[nonce] = challenge{user: account, device: device, expires: now.Add(ChallengeTTL)}
	return nonce, nil
}

// Session checks a signed challenge ("titanos-app-session\n<account>\n
// <device>\n<nonce>") and returns the account and the device for a new
// session. Every failure is ErrUnknown (or ErrUnavailable when the stick
// is away), so that the answer tells nothing.
func (m *Manager) Session(account, device, nonce, signature string) (auth.User, Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	c, ok := m.challenges[nonce]
	delete(m.challenges, nonce)
	if !ok || now.After(c.expires) || c.user != account || c.device != device {
		return auth.User{}, Device{}, ErrUnknown
	}
	user, ok := m.users.Get(account)
	if !ok || user.Disabled {
		return auth.User{}, Device{}, ErrUnknown
	}
	st, err := m.stateLocked(user)
	if err != nil {
		return auth.User{}, Device{}, err
	}
	for _, d := range st.devices {
		if d.ID != device || !m.live(d) {
			continue
		}
		key, err := ParseKey(d.Key)
		if err != nil || !verify(key, sessionText+account+"\n"+device+"\n"+nonce, signature) {
			return auth.User{}, Device{}, ErrUnknown
		}
		return user, d, nil
	}
	return auth.User{}, Device{}, ErrUnknown
}
