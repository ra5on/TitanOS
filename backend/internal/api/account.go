package api

import (
	"errors"
	"log/slog"
	"net/http"
	"os"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/webauthn"
)

// What everyone does with their own account: the second factor and the
// devices that are logged in.

func (au authArea) sessionToken(r *http.Request) string {
	if cookie, err := r.Cookie(au.cookieName()); err == nil {
		return cookie.Value
	}
	return ""
}

func (au authArea) handleAccountSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sessions": au.sessions().List(currentUser(r).Name, au.sessionToken(r))})
}

func (au authArea) handleAccountSessionEnd(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if !au.sessions().DeleteByID(user.Name, r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "session_not_found", "no such session")
		return
	}
	slog.Info("session ended from the list of devices", "user", user.Name, "ip", remoteIP(r))
	au.handleAccountSessions(w, r)
}

// handleAccountSessionsEndOthers ends every session of the account except
// the one the request came with ("log out all other devices").
func (au authArea) handleAccountSessionsEndOthers(w http.ResponseWriter, r *http.Request) {
	user, token := currentUser(r), au.sessionToken(r)
	if token == "" {
		// Without a session of its own the request would end all of them.
		writeError(w, http.StatusBadRequest, "session_required", "only a logged-in browser can end its other sessions")
		return
	}
	au.sessions().DeleteUser(user.Name, token)
	slog.Info("other sessions ended from the list of devices", "user", user.Name, "ip", remoteIP(r))
	au.handleAccountSessions(w, r)
}

// handleTwoFactorBegin hands out a secret for the authenticator app. It
// only counts once handleTwoFactorEnable has seen a code made from it.
// Like switching it off it needs the password (or a passkey) of the
// account: a session alone must not set up a second factor that locks
// the owner of the account out (owner decision 7.11.2026).
func (au authArea) handleTwoFactorBegin(w http.ResponseWriter, r *http.Request) {
	var req proof
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	if user.TOTPSecret != "" {
		writeTwoFactorError(w, auth.ErrTwoFactorEnabled)
		return
	}
	if !au.reconfirmProof(w, user, req) {
		return
	}
	hostname, _ := os.Hostname()
	secret, uri, err := au.users.BeginTwoFactor(user.Name, "TitanOS "+hostname)
	if err != nil {
		writeTwoFactorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "uri": uri})
}

func (au authArea) handleTwoFactorEnable(w http.ResponseWriter, r *http.Request) {
	var req struct{ Code string }
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	codes, err := au.users.EnableTwoFactor(user.Name, req.Code)
	if err != nil {
		writeTwoFactorError(w, err)
		return
	}
	slog.Info("two-factor login switched on", "user", user.Name, "ip", remoteIP(r))
	// Shown this once; only hashes are kept.
	writeJSON(w, http.StatusOK, map[string][]string{"recoveryCodes": codes})
}

// reconfirm checks password and, if two-factor login is on, a code as
// proof that it is the owner of the account who asks for a change that
// opens another way into it. Failures count against the account like a
// failed login; it answers the request itself when the proof fails.
func (au authArea) reconfirm(w http.ResponseWriter, user auth.User, password, code string) bool {
	key := "user:" + user.Name
	// Proofs sent at the same time all pass this check before the first
	// of them is counted (hashing is slow), so it is made again before
	// every answer: once the limit was reached meanwhile, the answer is
	// not told, right or wrong; a wrong proof still counts.
	blocked := func() bool {
		if au.limiter().RetryAfter(key) <= 0 {
			return false
		}
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed attempts, try again later")
		return true
	}
	if blocked() {
		return false
	}
	_, ok := au.users.Authenticate(user.Name, password)
	if blocked() {
		if !ok {
			au.limiter().Fail(key)
		}
		return false
	}
	if !ok {
		au.limiter().Fail(key)
		writeError(w, http.StatusForbidden, "wrong_password", "the password is wrong")
		return false
	}
	ok = au.users.CheckSecondFactor(user.Name, code)
	if blocked() {
		if !ok {
			au.limiter().Fail(key)
		}
		return false
	}
	if !ok {
		au.limiter().Fail(key)
		writeError(w, http.StatusForbidden, "invalid_code", "the code is wrong or was used already")
		return false
	}
	return true
}

// passkeyAssertion is a passkey's answer to a challenge of
// handlePasskeyConfirmBegin.
type passkeyAssertion struct {
	ChallengeID       string `json:"challengeId"`
	ID                string `json:"id"`
	ClientDataJSON    string `json:"clientDataJSON"`
	AuthenticatorData string `json:"authenticatorData"`
	Signature         string `json:"signature"`
	UserHandle        string `json:"userHandle"`
}

// proof is password (and code) or one of the account's passkeys.
type proof struct {
	Password string            `json:"password"`
	Code     string            `json:"code"`
	Passkey  *passkeyAssertion `json:"passkey"`
}

// reconfirmProof is reconfirm that also takes a passkey of the account
// instead of password and code: it asks for user verification and counts
// as both factors, as at the login.
func (au authArea) reconfirmProof(w http.ResponseWriter, user auth.User, p proof) bool {
	if p.Passkey == nil {
		return au.reconfirm(w, user, p.Password, p.Code)
	}
	key := "user:" + user.Name
	if wait := au.limiter().RetryAfter(key); wait > 0 {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed attempts, try again later")
		return false
	}
	switch au.checkPasskeyProof(user, p.Passkey) {
	case "":
		return true
	case "passkey_expired":
		writeError(w, http.StatusBadRequest, "passkey_expired", "the request expired, start again")
	default:
		au.limiter().Fail(key)
		writeError(w, http.StatusForbidden, "passkey_failed", "the passkey was not accepted")
	}
	return false
}

// checkPasskeyProof checks a passkey's answer to a challenge of
// handlePasskeyConfirmBegin for the account. It returns "" when it holds,
// "passkey_expired" when the challenge is unknown (no guess, nothing to
// count) and "passkey_failed" when the answer is wrong; the caller counts
// that as a failed attempt with the keys of its own limiter.
func (au authArea) checkPasskeyProof(user auth.User, a *passkeyAssertion) string {
	c, ok := au.passkeyState.take(a.ChallengeID)
	if !ok || !c.confirm || c.user != user.Name {
		return "passkey_expired"
	}
	fail := func(why string) string {
		slog.Warn("passkey confirmation failed", "user", user.Name, "why", why)
		return "passkey_failed"
	}
	clientData, err1 := webauthn.Encoding.DecodeString(a.ClientDataJSON)
	authData, err2 := webauthn.Encoding.DecodeString(a.AuthenticatorData)
	signature, err3 := webauthn.Encoding.DecodeString(a.Signature)
	handle, err4 := webauthn.Encoding.DecodeString(a.UserHandle)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return fail("malformed")
	}
	owner, passkey, found := au.users.FindPasskey(a.ID, handle)
	if !found || owner.Name != user.Name || passkey.RPID != c.rpID {
		return fail("not a passkey of the account")
	}
	count, err := webauthn.Login(clientData, authData, signature, passkey.Credential(), webauthn.Expect{Challenge: c.challenge, Origin: c.origin, RPID: c.rpID})
	if err != nil {
		return fail(err.Error())
	}
	if err := au.users.UsePasskey(user.Name, passkey.ID, count); err != nil {
		slog.Warn("passkey counter not saved", "user", user.Name, "err", err)
	}
	return ""
}

// handleTwoFactorDisable switches the second factor off, with the password
// and a code (or a passkey) as proof that it is the owner of the account
// who asks.
func (au authArea) handleTwoFactorDisable(w http.ResponseWriter, r *http.Request) {
	var req proof
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	if !au.reconfirmProof(w, user, req) {
		return
	}
	if err := au.users.DisableTwoFactor(user.Name); err != nil {
		writeTwoFactorError(w, err)
		return
	}
	slog.Info("two-factor login switched off", "user", user.Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// handleTwoFactorRecovery replaces the recovery codes, with the password
// and a code as proof like switching the second factor off. The new codes
// are shown this once; only their hashes are kept.
func (au authArea) handleTwoFactorRecovery(w http.ResponseWriter, r *http.Request) {
	var req proof
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	if user.TOTPSecret == "" {
		writeError(w, http.StatusConflict, "two_factor_not_begun", "two-factor login is off")
		return
	}
	if !au.reconfirmProof(w, user, req) {
		return
	}
	codes, err := au.users.NewRecoveryCodes(user.Name)
	if err != nil {
		writeTwoFactorError(w, err)
		return
	}
	slog.Info("two-factor recovery codes renewed", "user", user.Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string][]string{"recoveryCodes": codes})
}

func writeTwoFactorError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrTwoFactorEnabled):
		writeError(w, http.StatusConflict, "two_factor_enabled", err.Error())
	case errors.Is(err, auth.ErrNoTwoFactor):
		writeError(w, http.StatusConflict, "two_factor_not_begun", err.Error())
	case errors.Is(err, auth.ErrWrongCode):
		writeError(w, http.StatusBadRequest, "invalid_code", err.Error())
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, "user_not_found", err.Error())
	default:
		slog.Error("two-factor login", "err", err)
		writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
	}
}
