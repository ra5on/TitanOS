package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Two-factor login with time-based one-time codes (TOTP, RFC 6238), as
// authenticator apps produce them: six digits that change every 30 seconds.

var (
	ErrNoTwoFactor      = errors.New("auth: two-factor login is not set up")
	ErrTwoFactorEnabled = errors.New("auth: two-factor login is already on")
	ErrWrongCode        = errors.New("auth: wrong code")
)

const (
	totpPeriod = 30 * time.Second
	// recoveryCodes is how many one-time codes there are for a lost phone.
	recoveryCodes = 8
)

var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

// newTOTPSecret returns a fresh secret in the form authenticator apps take.
func newTOTPSecret() string {
	raw := make([]byte, 20)
	rand.Read(raw)
	return base32NoPad.EncodeToString(raw)
}

// totpCode returns the code of a secret for a 30-second step.
func totpCode(secret string, step uint64) (string, error) {
	key, err := base32NoPad.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], step)
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1_000_000), nil
}

// checkTOTP reports the step a code is valid for, or 0. The step before
// and after the current one count as well: clocks differ and typing takes time.
func checkTOTP(secret, code string, now time.Time) uint64 {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 {
		return 0
	}
	current := uint64(now.Unix()) / uint64(totpPeriod.Seconds())
	for _, step := range []uint64{current, current - 1, current + 1} {
		want, err := totpCode(secret, step)
		if err == nil && subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return step
		}
	}
	return 0
}

// totpURI is what the QR code for an authenticator app holds.
func totpURI(secret, account, issuer string) string {
	values := url.Values{"secret": {secret}, "issuer": {issuer}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + values.Encode()
}

func newRecoveryCode() string {
	raw := make([]byte, 6)
	rand.Read(raw)
	code := strings.ToLower(base32NoPad.EncodeToString(raw))
	return code[:5] + "-" + code[5:10]
}

func hashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.ReplaceAll(strings.TrimSpace(code), " ", ""))))
	return hex.EncodeToString(sum[:])
}

// BeginTwoFactor makes a secret for an account and returns it with the
// address for the QR code. Nothing is stored until EnableTwoFactor proves
// with a code that the authenticator app has the secret.
func (u *Users) BeginTwoFactor(name, issuer string) (secret, uri string, err error) {
	user, ok := u.Get(name)
	if !ok {
		return "", "", ErrNotFound
	}
	if user.TOTPSecret != "" {
		return "", "", ErrTwoFactorEnabled
	}
	secret = newTOTPSecret()
	u.mu.Lock()
	u.pendingTOTP[name] = secret
	u.mu.Unlock()
	return secret, totpURI(secret, name, issuer), nil
}

// EnableTwoFactor switches two-factor login on once a code shows that the
// app is set up, and returns the recovery codes. They are shown this once;
// only their hashes are kept.
func (u *Users) EnableTwoFactor(name, code string) ([]string, error) {
	u.mu.Lock()
	secret := u.pendingTOTP[name]
	u.mu.Unlock()
	if secret == "" {
		return nil, ErrNoTwoFactor
	}
	step := checkTOTP(secret, code, time.Now())
	if step == 0 {
		return nil, ErrWrongCode
	}
	codes := make([]string, recoveryCodes)
	hashes := make([]string, recoveryCodes)
	for i := range codes {
		codes[i] = newRecoveryCode()
		hashes[i] = hashRecoveryCode(codes[i])
	}
	_, err := u.update(name, func(user *User) error {
		if user.TOTPSecret != "" {
			return ErrTwoFactorEnabled
		}
		user.TOTPSecret, user.RecoveryCodes = secret, hashes
		return nil
	})
	if err != nil {
		return nil, err
	}
	u.mu.Lock()
	delete(u.pendingTOTP, name)
	u.usedStep[name] = step
	u.mu.Unlock()
	return codes, nil
}

// DisableTwoFactor switches two-factor login off for an account.
func (u *Users) DisableTwoFactor(name string) error {
	_, err := u.update(name, func(user *User) error {
		user.TOTPSecret, user.RecoveryCodes = "", nil
		return nil
	})
	return err
}

// CheckSecondFactor checks a code from the authenticator app, or a
// recovery code, which is used up by it. An account without two-factor
// login passes with any code.
func (u *Users) CheckSecondFactor(name, code string) bool {
	user, ok := u.Get(name)
	if !ok {
		return false
	}
	if user.TOTPSecret == "" {
		return true
	}
	if step := checkTOTP(user.TOTPSecret, code, time.Now()); step != 0 {
		u.mu.Lock()
		defer u.mu.Unlock()
		// A code counts once: whoever read it over a shoulder cannot use it again.
		if step <= u.usedStep[name] {
			return false
		}
		u.usedStep[name] = step
		return true
	}
	hash := hashRecoveryCode(code)
	used := false
	u.update(name, func(user *User) error {
		for i, known := range user.RecoveryCodes {
			if subtle.ConstantTimeCompare([]byte(known), []byte(hash)) == 1 {
				user.RecoveryCodes = append(user.RecoveryCodes[:i:i], user.RecoveryCodes[i+1:]...)
				used = true
				return nil
			}
		}
		return ErrWrongCode
	})
	return used
}

// CodeForTest returns the code of a secret, offset time steps from now.
// Tests of other packages log in with it.
func CodeForTest(secret string, offset int) string {
	code, _ := totpCode(secret, uint64(int64(time.Now().Unix())/30+int64(offset)))
	return code
}
