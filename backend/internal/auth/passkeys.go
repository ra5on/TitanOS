package auth

import (
	"bytes"
	"crypto/rand"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/webauthn"
)

// maxPasskeys is how many passkeys one account may hold.
const maxPasskeys = 10

var (
	ErrPasskeyExists   = errors.New("auth: this passkey is already registered")
	ErrTooManyPasskeys = errors.New("auth: too many passkeys")
	ErrPasskeyNotFound = errors.New("auth: no such passkey")
)

// Passkey is a WebAuthn credential of an account, stored in account.json.
// A passkey login replaces password and second factor: the authenticator
// has verified its user (PIN, fingerprint, face).
type Passkey struct {
	// ID is the credential ID, base64url without padding.
	ID   string `json:"id"`
	Name string `json:"name"`
	// PublicKey is the COSE key as the authenticator sent it.
	PublicKey []byte `json:"publicKey"`
	// RPID is the host name the passkey belongs to; it only works there.
	RPID      string    `json:"rpId"`
	SignCount uint32    `json:"signCount,omitempty"`
	Synced    bool      `json:"synced,omitempty"`
	Created   time.Time `json:"created"`
}

// Credential turns the stored passkey back into what webauthn checks.
func (p Passkey) Credential() webauthn.Credential {
	id, _ := webauthn.Encoding.DecodeString(p.ID)
	return webauthn.Credential{ID: id, PublicKey: p.PublicKey, SignCount: p.SignCount, Synced: p.Synced}
}

// PasskeyName keeps a name to one short line.
func PasskeyName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	for utf8.RuneCountInString(name) > 60 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

// WebAuthnUserID returns the random user handle of the account, creating
// it on first use. It names the account to authenticators without telling
// them the account name.
func (u *Users) WebAuthnUserID(name string) ([]byte, error) {
	user, err := u.update(name, func(user *User) error {
		if len(user.WebAuthnID) != 0 {
			return errUnchanged
		}
		id := make([]byte, 16)
		rand.Read(id)
		user.WebAuthnID = id
		return nil
	})
	return user.WebAuthnID, err
}

// AddPasskey stores a verified credential with the account.
func (u *Users) AddPasskey(name string, key Passkey) (User, error) {
	return u.update(name, func(user *User) error {
		if len(user.Passkeys) >= maxPasskeys {
			return ErrTooManyPasskeys
		}
		if _, _, found := u.findPasskeyLocked(key.ID); found {
			return ErrPasskeyExists
		}
		user.Passkeys = append(user.Passkeys, key)
		return nil
	})
}

// RemovePasskey deletes one passkey of the account.
func (u *Users) RemovePasskey(name, id string) (User, error) {
	return u.update(name, func(user *User) error {
		for i, key := range user.Passkeys {
			if key.ID == id {
				user.Passkeys = append(user.Passkeys[:i:i], user.Passkeys[i+1:]...)
				return nil
			}
		}
		return ErrPasskeyNotFound
	})
}

// ClearPasskeys removes every passkey, for example when an administrator
// resets the factors of an account whose devices were lost.
func (u *Users) ClearPasskeys(name string) error {
	_, err := u.update(name, func(user *User) error {
		if len(user.Passkeys) == 0 {
			return errUnchanged
		}
		user.Passkeys = nil
		return nil
	})
	return err
}

func (u *Users) findPasskeyLocked(id string) (User, Passkey, bool) {
	for _, user := range u.users {
		for _, key := range user.Passkeys {
			if key.ID == id {
				return user, key, true
			}
		}
	}
	return User{}, Passkey{}, false
}

// FindPasskey returns the account a credential belongs to. A user handle,
// when the authenticator sent one, has to match as well.
func (u *Users) FindPasskey(id string, userHandle []byte) (User, Passkey, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	user, key, ok := u.findPasskeyLocked(id)
	if !ok || (len(userHandle) != 0 && !bytes.Equal(userHandle, user.WebAuthnID)) {
		return User{}, Passkey{}, false
	}
	return user, key, true
}

// UsePasskey remembers the new signature counter. Most synced passkeys
// always send 0; then nothing is written to the stick.
func (u *Users) UsePasskey(name, id string, count uint32) error {
	_, err := u.update(name, func(user *User) error {
		for i := range user.Passkeys {
			if user.Passkeys[i].ID == id {
				if user.Passkeys[i].SignCount == count {
					return errUnchanged
				}
				user.Passkeys[i].SignCount = count
				return nil
			}
		}
		return ErrPasskeyNotFound
	})
	return err
}
