package tmail

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io/fs"
	"strings"
)

// Passwords are kept encrypted with AES-256-GCM under a key of the NAS in
// /config/tmail/key (hex, 0600). This protects against a single copied
// settings file or a log, not against someone in full control of the NAS:
// the key lies next to it (docs/TMAIL.md). The additional data binds a
// password to its account and mailbox, so that ciphertext copied into
// another mailbox does not open.

const sealPrefix = "v1:"

// keyLocked returns the key (m.mu held); create makes and stores one
// when there is none yet (the first save of a password, never at start).
func (m *Manager) keyLocked(create bool) ([]byte, error) {
	if m.key != nil {
		return m.key, nil
	}
	data, err := m.store.Read(keyFile)
	if err == nil {
		key, derr := hex.DecodeString(strings.TrimSpace(string(data)))
		if derr != nil || len(key) != 32 {
			return nil, errors.New("tmail: the key in /config is damaged")
		}
		m.key = key
		return key, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if !create {
		return nil, ErrNoPassword
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
	sealed := gcm.Seal(nonce, nonce, []byte(password), additional(user, id))
	return sealPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// openLocked decrypts the password of a mailbox (m.mu held).
func (m *Manager) openLocked(user, id, sealed string) (string, error) {
	if sealed == "" {
		return "", ErrNoPassword
	}
	raw, ok := strings.CutPrefix(sealed, sealPrefix)
	if !ok {
		return "", errors.New("tmail: unknown form of a stored password")
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", err
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
		return "", errors.New("tmail: a stored password is damaged")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], additional(user, id))
	if err != nil {
		return "", errors.New("tmail: a stored password does not open with the key")
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
