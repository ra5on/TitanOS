package appdevices

// The watch (Wear OS, PLAN 116 package G, docs/ANDROID-APP.md "Die Uhr"):
// it has no camera for the QR code and no Google Data Layer to the phone,
// so it talks to the NAS itself and the paired phone app confirms it.
//
// The watch knows only the address the person typed; the certificate it
// sees there is not known to be the NAS's yet. The six digits on the watch
// therefore are a short authentication string over everything a machine in
// the middle would have to change: the watch's key, a random value of the
// NAS, a random value of the watch and the certificate the watch saw. The
// watch commits to its random value before it learns the NAS's, so nobody
// can search for a key that gives the same digits. The person types the
// digits into the paired phone app; the NAS compares them with its own.

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

const (
	// WatchApp is the app name of a watch.
	WatchApp = "watch"
	// WatchTTL is how long a watch has from its start until the phone
	// confirms it.
	WatchTTL = 5 * time.Minute
	// MaxWatchPending bounds the open watch pairings of the whole NAS
	// (they are started without a login); MaxWatchPerAddress those of one
	// address.
	MaxWatchPending    = 20
	MaxWatchPerAddress = 3
	// WatchDigits is the length of the code on the watch.
	WatchDigits = 6
)

// Texts the watch hashes and signs (docs/ANDROID-APP.md).
const (
	watchCommitText = "titanos-watch-commit\n"
	watchCodeText   = "titanos-watch-code\n"
	watchWaitText   = "titanos-watch-wait\n"
)

// Watch pairing states besides Confirmed and Expired.
const (
	WatchStarted  = "started"
	WatchRevealed = "revealed"
)

var (
	// ErrWatchCertificate: the watch saw another certificate than the NAS
	// serves on that connection; someone may be in between.
	ErrWatchCertificate = errors.New("appdevices: the watch saw another certificate")
	// ErrWatchConfirmer: only a paired phone app confirms a watch.
	ErrWatchConfirmer = errors.New("appdevices: only a paired phone app confirms a watch")
)

// WatchPairing is one watch pairing in RAM.
type WatchPairing struct {
	ID      string
	Name    string
	Nonce   string
	Expires time.Time
	State   string
	User    string
	Device  string
	key     string
	commit  string
	code    string
	address string
}

// WatchCommit is what the watch sends at the start: base64url (no padding)
// of SHA-256("titanos-watch-commit\n" + publicKey + "\n" + secret), with
// publicKey exactly as sent.
func WatchCommit(publicKey, secret string) string {
	sum := sha256.Sum256([]byte(watchCommitText + publicKey + "\n" + secret))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// WatchCode is the six digits both the watch and the NAS work out:
// SHA-256 over "titanos-watch-code\n" + id, publicKey, the NAS's nonce,
// the watch's secret, the SPKI the watch saw and "1"/"0" for a public
// certificate, each on its own line; the first eight bytes big-endian
// modulo 10^6, with leading zeros.
func WatchCode(id, publicKey, nonce, secret, spki string, public bool) string {
	flag := "0"
	if public {
		flag = "1"
	}
	sum := sha256.Sum256([]byte(watchCodeText + id + "\n" + publicKey + "\n" + nonce + "\n" + secret + "\n" + spki + "\n" + flag))
	return fmt.Sprintf("%06d", binary.BigEndian.Uint64(sum[:8])%1_000_000)
}

// normalWatchCode keeps the digits of a typed code ("482 913").
func normalWatchCode(code string) string {
	var b strings.Builder
	for _, r := range code {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-':
		default:
			return ""
		}
	}
	if b.Len() != WatchDigits {
		return ""
	}
	return b.String()
}

func (m *Manager) pruneWatchesLocked(now time.Time) {
	for id, p := range m.watches {
		if now.After(p.Expires) {
			delete(m.watches, id)
		}
	}
}

// WatchStart begins the pairing of a watch: its name, its public key and
// its commitment. It answers with an id and the NAS's random value.
// address is where the watch asks from (for the limits only).
func (m *Manager) WatchStart(name, key, commit, address string) (WatchPairing, error) {
	if !ValidName(name) {
		return WatchPairing{}, ErrInvalidName
	}
	key = strings.TrimSpace(key)
	if _, err := ParseKey(key); err != nil {
		return WatchPairing{}, err
	}
	if raw, err := base64.RawURLEncoding.DecodeString(commit); err != nil || len(raw) != sha256.Size {
		return WatchPairing{}, ErrBadCode
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.pruneWatchesLocked(now)
	from := 0
	for _, p := range m.watches {
		if p.address == address {
			from++
		}
	}
	if len(m.watches) >= MaxWatchPending || from >= MaxWatchPerAddress {
		return WatchPairing{}, ErrTooManyOpen
	}
	p := &WatchPairing{ID: randomText(12), Name: name, Nonce: randomText(24), Expires: now.Add(WatchTTL), State: WatchStarted,
		key: key, commit: commit, address: address}
	m.watches[p.ID] = p
	return *p, nil
}

// WatchReveal takes the watch's secret and the certificate it saw (the
// SPKI hash, and whether it was a valid public certificate). served is
// the SPKI the NAS serves on that connection, "" when it cannot tell (TLS
// ends before titand). A wrong secret or certificate ends the pairing: it
// is tried once.
func (m *Manager) WatchReveal(id, secret, spki string, public bool, served []string) (WatchPairing, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.pruneWatchesLocked(now)
	p := m.watches[id]
	if p == nil || p.State != WatchStarted {
		return WatchPairing{}, ErrBadCode
	}
	if secret == "" || len(secret) > 128 || subtle.ConstantTimeCompare([]byte(WatchCommit(p.key, secret)), []byte(p.commit)) != 1 {
		delete(m.watches, id)
		return WatchPairing{}, ErrBadCode
	}
	if !public && len(served) > 0 {
		match := false
		for _, s := range served {
			match = match || (s != "" && s == spki)
		}
		if !match {
			delete(m.watches, id)
			return WatchPairing{}, ErrWatchCertificate
		}
	}
	p.code = WatchCode(p.ID, p.key, p.Nonce, secret, spki, public)
	p.State = WatchRevealed
	return *p, nil
}

// WatchConfirm pairs the watch whose code the person typed into a paired
// phone app (device, its session) of the account. ErrBadCode is for the
// caller to count.
func (m *Manager) WatchConfirm(user auth.User, device, code string) (Device, error) {
	code = normalWatchCode(code)
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.pruneWatchesLocked(now)
	st, err := m.stateLocked(user)
	if err != nil {
		return Device{}, err
	}
	allowed := false
	for _, d := range st.devices {
		allowed = allowed || (d.ID == device && d.App != WatchApp && m.live(d))
	}
	if !allowed {
		return Device{}, ErrWatchConfirmer
	}
	if code == "" {
		return Device{}, ErrBadCode
	}
	var found *WatchPairing
	for _, p := range m.watches {
		if p.State != WatchRevealed || subtle.ConstantTimeCompare([]byte(p.code), []byte(code)) != 1 {
			continue
		}
		if found != nil {
			// Two watches with the same digits at once: neither, both
			// start again with new values.
			delete(m.watches, found.ID)
			delete(m.watches, p.ID)
			return Device{}, ErrBadCode
		}
		found = p
	}
	if found == nil {
		return Device{}, ErrBadCode
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
	d := Device{ID: randomText(12), Name: found.Name, App: WatchApp, Created: now.UTC(), Key: found.key}
	if err := m.saveLocked(user, append(kept, d)); err != nil {
		return Device{}, err
	}
	found.State, found.User, found.Device = Confirmed, user.Name, d.ID
	found.Expires = now.Add(ConfirmedTTL)
	return d, nil
}

// WatchWait tells the watch the state of its pairing; signature is over
// "titanos-watch-wait\n<id>" with the key of the start. A confirmed
// pairing is told once.
func (m *Manager) WatchWait(id, signature string) (WatchPairing, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.pruneWatchesLocked(now)
	p := m.watches[id]
	if p == nil {
		return WatchPairing{}, ErrBadCode
	}
	pub, err := ParseKey(p.key)
	if err != nil || !verify(pub, watchWaitText+id, signature) {
		return WatchPairing{}, ErrSignature
	}
	out := *p
	if p.State == Confirmed {
		delete(m.watches, id)
	}
	return out, nil
}

// dropWatchesLocked forgets the confirmed watch pairings of an account.
func (m *Manager) dropWatchesLocked(name string) {
	for id, p := range m.watches {
		if p.User == name {
			delete(m.watches, id)
		}
	}
}
