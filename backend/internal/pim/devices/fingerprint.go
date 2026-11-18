package devices

import "github.com/ra5on/TitanOS/backend/internal/auth"

// Fingerprint is the fingerprint of an account's credentials (password
// hash and TOTP secret) that binds devices.json to them (S-25). The
// one-time link of a profile (1.13) carries it, so that a password changed
// between the link and its fetch makes no device afterwards.
func Fingerprint(user auth.User) string { return credentials(user) }
