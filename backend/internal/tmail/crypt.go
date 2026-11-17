package tmail

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// Passwords are kept encrypted with AES-256-GCM under a key of the NAS in
// /config/tmail/key (hex, 0600). This protects against a single copied
// settings file or a log, not against someone in full control of the NAS:
// the key lies next to it (docs/TMAIL.md). The additional data binds a
// password to its account and mailbox, so that ciphertext copied into
// another mailbox does not open.

// Forms of a stored password: v1 (first build of 1.05) binds it to
// account and mailbox, v2 also to the account's creation time, so that it
// never opens for a later account of the same name.
const (
	sealPrefix   = "v2:"
	sealPrefixV1 = "v1:"
)

// ErrKeyMissing and ErrKeyDamaged: the key is gone or cannot be used
// while passwords are stored. No new key is made then: it would leave the
// stored passwords unreadable without a word (security review of 1.05).
// Restore the key from the backup of the stick, or remove the mailboxes
// and add them anew.
var (
	ErrKeyMissing = errors.New("tmail: the key of the stored mail passwords is missing on the stick")
	ErrKeyDamaged = errors.New("tmail: the key of the stored mail passwords is damaged")
)

// anySealedLocked tells whether a password is stored anywhere (m.mu
// held), also in files of accounts that are gone.
func (m *Manager) anySealedLocked() bool {
	for _, f := range m.users {
		for _, mb := range f.Mailboxes {
			if mb.Password != "" {
				return true
			}
		}
	}
	return len(m.unreadable) > 0
}

// keyLocked returns the key (m.mu held); create makes and stores one
// when there is none yet and no password is stored (the first save of a
// password, never at start).
func (m *Manager) keyLocked(create bool) ([]byte, error) {
	if m.key != nil {
		return m.key, nil
	}
	data, err := m.store.Read(keyFile)
	if err == nil {
		key, derr := hex.DecodeString(strings.TrimSpace(string(data)))
		if derr != nil || len(key) != 32 {
			slog.Error("T-Mail: the key of the mail passwords is damaged; nothing is stored or read until it is restored", "file", keyFile)
			return nil, ErrKeyDamaged
		}
		m.key = key
		return key, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		slog.Error("T-Mail: the key of the mail passwords cannot be read", "file", keyFile, "err", err)
		return nil, ErrKeyMissing
	}
	if !create || m.anySealedLocked() {
		if m.anySealedLocked() {
			slog.Error("T-Mail: the key of the mail passwords is missing while passwords are stored; no new key is made", "file", keyFile)
		}
		return nil, ErrKeyMissing
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := m.store.Write(keyFile, []byte(hex.EncodeToString(key)+"\n")); err != nil {
		return nil, err
	}
	m.key = key
	return key, nil
}

func additional(user, id string) []byte { return []byte("tmail/v1/" + user + "/" + id) }

// additionalV2 adds the creation time of the account.
func additionalV2(user string, created time.Time, id string) []byte {
	return []byte("tmail/v2/" + user + "/" + strconv.FormatInt(created.UnixNano(), 10) + "/" + id)
}

// createdOf is the creation time of an account (zero if unknown).
func (m *Manager) createdOf(user string) time.Time {
	account, _ := m.lookup(user)
	return account.Created.UTC()
}

// sealLocked encrypts a password for a mailbox (m.mu held).
func (m *Manager) sealLocked(user, id, password string) (string, error) {
	key, err := m.keyLocked(true)
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(password), additionalV2(user, m.createdOf(user), id))
	return sealPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// openLocked decrypts the password of a mailbox (m.mu held).
func (m *Manager) openLocked(user, id, sealed string) (string, error) {
	if sealed == "" {
		return "", ErrNoPassword
	}
	var aad []byte
	raw, ok := strings.CutPrefix(sealed, sealPrefix)
	if ok {
		aad = additionalV2(user, m.createdOf(user), id)
	} else if raw, ok = strings.CutPrefix(sealed, sealPrefixV1); ok {
		// Stored by the first build of 1.05: still read; the next save of
		// the password writes v2. The file itself names its account with
		// its creation time (sameAccount).
		aad = additional(user, id)
	} else {
		return "", errors.New("tmail: unknown form of a stored password")
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", ErrKeyDamaged
	}
	key, err := m.keyLocked(false)
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", ErrKeyDamaged
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], aad)
	if err != nil {
		slog.Error("T-Mail: a stored mail password does not open with the key", "account", user, "mailbox", id)
		return "", ErrKeyDamaged
	}
	return string(plain), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// password returns the plain password of a mailbox.
func (m *Manager) password(user string, mb Mailbox) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.openLocked(user, mb.ID, mb.Password)
}
