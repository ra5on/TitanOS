package auth

import (
	"encoding/json"
	"strings"
)

// lockedHashPrefix is put in front of the password hash of a locked
// account in account.json. titand before 0.28.0 does not know the
// "disabled" field and would let the account in; with the prefix the hash
// has one "$" part too many, which every version's verifyPassword refuses.
// The login of a locked account then fails there, its API tokens no longer
// match (TokenAccountKey hashes the stored hash) and the local network
// recovery refuses it as well.
const lockedHashPrefix = "!locked$"

// storedUser has the fields of User without its JSON methods.
type storedUser User

// userFile is account.json. While an account is locked, its SMB hash and
// SSH keys are parked in fields older versions do not read: they then
// remove the Samba account and give no SSH access. If an older version
// rewrites the file, it drops the parked fields; the account stays locked
// by the prefix, SMB comes back with the next login after unlocking (as
// for accounts from before shares existed) and SSH keys have to be added
// again.
type userFile struct {
	storedUser
	DisabledSMBHash string   `json:"disabledSmbHash,omitempty"`
	DisabledSSHKeys []string `json:"disabledSshKeys,omitempty"`
}

// MarshalJSON writes the account as stored in account.json.
func (u User) MarshalJSON() ([]byte, error) {
	file := userFile{storedUser: storedUser(u)}
	if u.Disabled {
		if u.PasswordHash != "" {
			file.PasswordHash = lockedHashPrefix + u.PasswordHash
		}
		file.DisabledSMBHash, file.SMBHash = u.SMBHash, ""
		file.DisabledSSHKeys, file.SSHKeys = u.SSHKeys, nil
	}
	return json.Marshal(file)
}

// UnmarshalJSON reads account.json. The prefix alone also counts as locked:
// an older version may have rewritten the file without the flag.
func (u *User) UnmarshalJSON(data []byte) error {
	var file userFile
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	*u = User(file.storedUser)
	if hash, locked := strings.CutPrefix(u.PasswordHash, lockedHashPrefix); locked {
		u.PasswordHash, u.Disabled = hash, true
	}
	if !u.Disabled {
		// A flag without the prefix comes from no version of titand; the
		// parked values are taken back only together with a lock.
		return nil
	}
	if u.SMBHash == "" {
		u.SMBHash = file.DisabledSMBHash
	}
	if len(u.SSHKeys) == 0 {
		u.SSHKeys = file.DisabledSSHKeys
	}
	return nil
}
