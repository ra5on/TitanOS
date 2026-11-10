package api

import (
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/firewall"
)

// The lock of a session (PLAN 61, lock screen of the desktop): the cookie
// stays, the windows stay open in the browser, but the NAS answers every
// request of the session with 423 "locked" until the account's password
// (and code) or one of its passkeys is given again. Not to be confused
// with locking an account (users/{name}/lock, auth/disabled.go).

// defaultLockMinutes is the automatic lock while an account chose nothing;
// maxLockMinutes bounds what it may choose.
// defaultPhoneLockMinutes is the lock of a login from a phone while the
// account chose nothing: never (owner decision 7.11.2026, PLAN 69 – the
// phone has its own screen lock).
const (
	defaultLockMinutes      = 15
	defaultPhoneLockMinutes = 0
	maxLockMinutes          = 24 * 60
)

// lockTimes are the automatic locks of an account: on a computer and,
// separately, on a phone (PLAN 69).
type lockTimes struct{ desk, phone int }

var defaultLockTimes = lockTimes{desk: defaultLockMinutes, phone: defaultPhoneLockMinutes}

// phoneAgent tells whether a browser is a phone by its user agent: iPhone,
// iPod, Android phones and others marked "Mobi". Tablets (iPad asks for the
// desktop site, Android tablets lack "Mobile") count as computers.
func phoneAgent(ua string) bool {
	return strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPod") || strings.Contains(ua, "Mobi")
}

// lockExempt are the routes a locked session may still call through
// requireUser: unlocking (with a passkey, its challenge first) and locking
// again. GET /session and logout do not go through requireUser.
var lockExempt = []string{
	"POST /api/v1/session/lock",
	"POST /api/v1/session/unlock",
	"POST /api/v1/account/passkeys/confirm",
}

// authState is what the cookie of a request stands for.
type authState int

const (
	authNone authState = iota
	authOK
	authLocked
)

// lockMinutes is the automatic lock the account chose in its desktop
// preferences ("lockMinutes", 0 = off; "lockMinutesPhone" for a login from
// a phone), read from the cached preferences. Never called with the lock
// of the session store held: the first read of an account may wake the
// stick.
func (au authArea) lockMinutes(user, userAgent string) int {
	times := au.prefs.lockTimes(user)
	if phoneAgent(userAgent) {
		return times.phone
	}
	return times.desk
}

// lockTimes reads the fields "lockMinutes" and "lockMinutesPhone" of the
// desktop preferences; the desktop stores other fields there as well,
// which stay untouched.
func (p *preferences) lockTimes(user string) lockTimes {
	p.mu.Lock()
	if times, ok := p.locks[user]; ok {
		p.mu.Unlock()
		return times
	}
	// Unreadable a moment ago (stick missing): the default, without asking
	// the stick again on every request.
	if failed, ok := p.lockFailed[user]; ok && time.Since(failed) < lockRetry {
		p.mu.Unlock()
		return defaultLockTimes
	}
	p.mu.Unlock()
	times := defaultLockTimes
	value, err := p.get(user, "desktop")
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		if p.lockFailed == nil {
			p.lockFailed = map[string]time.Time{}
		}
		p.lockFailed[user] = time.Now()
		return times
	}
	if value != nil {
		var desktop struct {
			LockMinutes      *float64 `json:"lockMinutes"`
			LockMinutesPhone *float64 `json:"lockMinutesPhone"`
		}
		if json.Unmarshal(value, &desktop) == nil {
			if desktop.LockMinutes != nil {
				times.desk = clampLockMinutes(*desktop.LockMinutes)
			}
			if desktop.LockMinutesPhone != nil {
				times.phone = clampLockMinutes(*desktop.LockMinutesPhone)
			}
		}
	}
	if p.locks == nil {
		p.locks = map[string]lockTimes{}
	}
	delete(p.lockFailed, user)
	p.locks[user] = times
	return times
}

// lockRetry is how long an unreadable desktop preference is not asked for again.
const lockRetry = 30 * time.Second

func clampLockMinutes(value float64) int {
	if math.IsNaN(value) || value <= 0 {
		return 0
	}
	return int(math.Min(math.Round(value), maxLockMinutes))
}

// cookieState looks at the session cookie of a request: it locks the
// session first if it was not used for the account's lockMinutes (the
// automatic lock also holds when no browser is left to lock it), then
// counts the request as use.
func (au authArea) cookieState(r *http.Request) (auth.User, authState) {
	cookie, err := r.Cookie(au.cookieName())
	if err != nil {
		return auth.User{}, authNone
	}
	if peek, ok := au.sessions().Peek(cookie.Value); ok && !peek.Locked {
		if minutes := au.lockMinutes(peek.User, peek.UserAgent); minutes > 0 && au.sessions().LockIfIdle(cookie.Value, time.Duration(minutes)*time.Minute) {
			slog.Info("session locked after inactivity", "user", peek.User, "ip", remoteIP(r))
			au.recordEvent(r, peek.User, "session_autolock", "", true)
		}
	}
	session, ok := au.sessions().Get(cookie.Value)
	if !ok {
		return auth.User{}, authNone
	}
	// The account may have been removed or locked since the login.
	user, ok := au.activeUser(session.User)
	if !ok {
		return auth.User{}, authNone
	}
	if session.Locked {
		return user, authLocked
	}
	return user, authOK
}

// writeLocked answers a request of a locked session.
func writeLocked(w http.ResponseWriter) {
	writeError(w, http.StatusLocked, "locked", "this session is locked; unlock it with the password or a passkey")
}

// handleSessionLock locks the session the request came with.
func (au authArea) handleSessionLock(w http.ResponseWriter, r *http.Request) {
	token := au.sessionToken(r)
	if token == "" {
		writeError(w, http.StatusBadRequest, "session_required", "only a logged-in browser can lock its session")
		return
	}
	user := currentUser(r)
	if peek, ok := au.sessions().Peek(token); ok && !peek.Locked && au.sessions().Lock(token) {
		slog.Info("session locked", "user", user.Name, "ip", remoteIP(r))
		au.recordEvent(r, user.Name, "session_lock", "", true)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSessionUnlock lifts the lock with the account's password (and
// code with two-factor login) or a passkey answer to a challenge of
// POST /account/passkeys/confirm. Failures count like failed logins:
// the same limiter keys (known device or account name, and the address)
// and the firewall's count of failures.
func (au authArea) handleSessionUnlock(w http.ResponseWriter, r *http.Request) {
	token := au.sessionToken(r)
	if token == "" {
		writeError(w, http.StatusBadRequest, "session_required", "only a logged-in browser can unlock its session")
		return
	}
	var req proof
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	ip := remoteIP(r)
	userKey, ipKey := "user:"+user.Name, passkeySource(r)
	guessKey := userKey
	if device := au.knownDevice(r, user.Name); device != "" {
		guessKey = device
	}
	if wait := au.limiter.RetryAfter(guessKey, ipKey); wait > 0 {
		au.recordEvent(r, user.Name, "session_unlock_blocked", "", false)
		writeJSON(w, http.StatusTooManyRequests, map[string]apiError{"error": {
			Code: "rate_limited", Message: "too many failed attempts, try again later", RetryAfter: int(math.Ceil(wait.Seconds())),
		}})
		return
	}
	// Guesses sent at the same time all pass the check above before the
	// first of them is counted (hashing is slow); once the limit was
	// reached meanwhile, the answer is not told, right or wrong.
	blocked := func(how string) bool {
		wait := au.limiter.RetryAfter(guessKey, ipKey)
		if wait <= 0 {
			return false
		}
		au.recordEvent(r, user.Name, "session_unlock_blocked", how, false)
		writeRateLimited(w, wait, "too many failed attempts, try again later")
		return true
	}
	fail := func(code, message, how string) {
		wasBlocked := au.limiter.RetryAfter(guessKey, ipKey) > 0
		au.limiter.Fail(guessKey, ipKey)
		au.firewall.Failed(ip, firewall.SourceWeb)
		if wasBlocked && blocked(how) {
			return
		}
		slog.Warn("unlock failed", "user", user.Name, "ip", ip, "how", how)
		au.recordEvent(r, user.Name, "session_unlock_failed", how, false)
		writeError(w, http.StatusForbidden, code, message)
	}
	how := "password"
	if req.Passkey != nil {
		how = "passkey"
		switch au.checkPasskeyProof(user, req.Passkey) {
		case "":
		case "passkey_expired":
			writeError(w, http.StatusBadRequest, "passkey_expired", "the request expired, start again")
			return
		default:
			fail("passkey_failed", "the passkey was not accepted", how)
			return
		}
	} else {
		if _, ok := au.users.Authenticate(user.Name, req.Password); !ok {
			fail("wrong_password", "the password is wrong", how)
			return
		}
		if user.TwoFactor() {
			// The password was right; only now is the code asked for.
			if blocked(how) {
				return
			}
			if req.Code == "" {
				writeError(w, http.StatusForbidden, "second_factor_required", "enter the code from your authenticator app")
				return
			}
			if !au.users.CheckSecondFactor(user.Name, req.Code) {
				fail("invalid_code", "the code is wrong or was used already", "2fa")
				return
			}
		}
	}
	if blocked(how) {
		return
	}
	au.limiter.Reset(userKey)
	au.limiter.Reset(guessKey)
	if !au.sessions().Unlock(token) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	slog.Info("session unlocked", "user", user.Name, "ip", ip, "how", how)
	au.recordEvent(r, user.Name, "session_unlock", how, true)
	writeJSON(w, http.StatusOK, userOf(user))
}

// hasPasskeyHere reports whether the account holds a passkey for the
// address of the request, for the button of the lock screen.
func hasPasskeyHere(r *http.Request, user auth.User) bool {
	rpID, _, ok := passkeyOrigin(r)
	return ok && slices.ContainsFunc(user.Passkeys, func(key auth.Passkey) bool { return key.RPID == rpID })
}
