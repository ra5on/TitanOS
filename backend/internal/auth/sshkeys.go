package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"golang.org/x/crypto/ssh"
)

// maxSSHKeys is how many keys one account may hold.
const maxSSHKeys = 20

var (
	// ErrInvalidKey means a line is not a public SSH key TitanOS accepts.
	ErrInvalidKey = errors.New("auth: not a public SSH key")
	// ErrKeyExists means the account has this key already.
	ErrKeyExists = errors.New("auth: this key is already there")
	// ErrTooManyKeys means the account holds as many keys as it may.
	ErrTooManyKeys = errors.New("auth: too many keys")
	// ErrKeyNotFound means the account has no such key.
	ErrKeyNotFound = errors.New("auth: no such key")
)

// SSHKey describes a public key of an account without repeating it.
type SSHKey struct {
	// ID names the key in the API.
	ID string `json:"id"`
	// Type is ssh-ed25519, ssh-rsa, …
	Type string `json:"type"`
	// Fingerprint as ssh-keygen prints it: SHA256:…
	Fingerprint string `json:"fingerprint"`
	Comment     string `json:"comment"`
}

// parseSSHKey reads one line of an authorized_keys file. Options in front
// of the key are refused: they could run commands or lift restrictions.
func parseSSHKey(line string) (key ssh.PublicKey, comment string, err error) {
	line = strings.TrimSpace(line)
	if line == "" || len(line) > 8192 || strings.ContainsAny(line, "\r\n") {
		return nil, "", ErrInvalidKey
	}
	key, comment, options, rest, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil || len(options) > 0 || len(strings.TrimSpace(string(rest))) > 0 {
		return nil, "", ErrInvalidKey
	}
	switch key.Type() {
	case ssh.KeyAlgoED25519, ssh.KeyAlgoRSA, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521, ssh.KeyAlgoSKED25519, ssh.KeyAlgoSKECDSA256:
	default:
		return nil, "", ErrInvalidKey
	}
	// A comment is free text; keep it to what fits on a line.
	comment = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, comment)
	if len(comment) > 100 {
		comment = comment[:100]
	}
	return key, comment, nil
}

func describeSSHKey(line string) (SSHKey, bool) {
	key, comment, err := parseSSHKey(line)
	if err != nil {
		return SSHKey{}, false
	}
	sum := sha256.Sum256(key.Marshal())
	return SSHKey{ID: hex.EncodeToString(sum[:8]), Type: key.Type(), Fingerprint: ssh.FingerprintSHA256(key), Comment: comment}, true
}

// SSHKeys describes the keys of an account.
func (u User) DescribeSSHKeys() []SSHKey {
	keys := []SSHKey{}
	for _, line := range u.SSHKeys {
		if key, ok := describeSSHKey(line); ok {
			keys = append(keys, key)
		}
	}
	return keys
}

// AddSSHKey gives an account one more key to log in with over SSH.
func (u *Users) AddSSHKey(name, line string) (User, error) {
	key, comment, err := parseSSHKey(line)
	if err != nil {
		return User{}, err
	}
	// Stored in one form, whatever spacing came in.
	stored := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	if comment != "" {
		stored += " " + comment
	}
	added, _ := describeSSHKey(stored)
	return u.update(name, func(user *User) error {
		if len(user.SSHKeys) >= maxSSHKeys {
			return ErrTooManyKeys
		}
		for _, existing := range user.DescribeSSHKeys() {
			if existing.ID == added.ID {
				return ErrKeyExists
			}
		}
		user.SSHKeys = append(user.SSHKeys, stored)
		return nil
	})
}

// RemoveSSHKey takes a key away from an account.
func (u *Users) RemoveSSHKey(name, id string) (User, error) {
	return u.update(name, func(user *User) error {
		for i, line := range user.SSHKeys {
			if key, ok := describeSSHKey(line); ok && key.ID == id {
				user.SSHKeys = append(user.SSHKeys[:i:i], user.SSHKeys[i+1:]...)
				return nil
			}
		}
		return ErrKeyNotFound
	})
}
