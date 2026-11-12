package api

import (
	"errors"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/firewall"
)

// cookieName uses the __Host- prefix over HTTPS: browsers then only accept
// the cookie from exactly this host, over a secure connection.
func (au authArea) cookieName() string {
	if au.secureCookies {
		return "__Host-titanos_session"
	}
	return "titanos_session"
}

func (au authArea) setSessionCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     au.cookieName(),
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   au.secureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

func (au authArea) startSession(w http.ResponseWriter, r *http.Request, user auth.User) bool {
	token, err := au.sessions().Create(user.Name, remoteIP(r), r.UserAgent())
	if err != nil {
		slog.Error("create session", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not start a session")
		return false
	}
	session, _ := au.sessions().Peek(token)
	au.setSessionCookie(w, token, cookieAge(session))
	return true
}

// keptCookieAge is how long the browser keeps the cookie of a session that
// stays logged in: as long as browsers allow (400 days). The NAS decides
// when the session ends; the cookie is renewed whenever the app starts.
const keptCookieAge = 400 * 24 * time.Hour

func cookieAge(session auth.Session) int {
	if session.Remember {
		return int(keptCookieAge.Seconds())
	}
	return int(auth.SessionMaxAge.Seconds())
}

// ensureUsersLoaded retries reading the accounts if that failed at startup,
// for example because the stick was not there yet.
func (au authArea) ensureUsersLoaded() bool {
	if au.users.Loaded() {
		return true
	}
	au.loadMu.Lock()
	defer au.loadMu.Unlock()
	if au.users.Loaded() {
		return true
	}
	if time.Since(*au.lastLoadTry) < 5*time.Second {
		return false
	}
	*au.lastLoadTry = time.Now()
	if err := au.users.Load(); err != nil {
		slog.Warn("accounts not readable", "err", err)
		return false
	}
	return true
}

type userResponse struct {
	Name string    `json:"name"`
	Role auth.Role `json:"role"`
	// TwoFactor says whether the account logs in with a second factor.
	TwoFactor bool `json:"twoFactor"`
}

func userOf(user auth.User) userResponse {
	return userResponse{Name: user.Name, Role: user.Role, TwoFactor: user.TwoFactor()}
}

type sessionResponse struct {
	// ConfigAvailable is false while /config cannot be read (stick missing).
	ConfigAvailable bool `json:"configAvailable"`
	// SetupRequired is true until the owner account exists.
	SetupRequired bool          `json:"setupRequired"`
	User          *userResponse `json:"user"`
	Hostname      string        `json:"hostname"`
	// Version is only told to logged-in users.
	Version string `json:"version,omitempty"`
	// Locked: the session is locked (session_lock.go); User names who may
	// unlock it, Passkey whether a passkey of the account works on this
	// address. LockMinutes is the account's automatic lock for this
	// browser (phones have their own, PLAN 69), 0 = off.
	Locked      bool `json:"locked,omitempty"`
	Passkey     bool `json:"passkey,omitempty"`
	LockMinutes *int `json:"lockMinutes,omitempty"`
}

func (au authArea) handleSession(w http.ResponseWriter, r *http.Request) {
	hostname, _ := os.Hostname()
	res := sessionResponse{Hostname: hostname}
	res.ConfigAvailable = au.ensureUsersLoaded()
	res.SetupRequired = res.ConfigAvailable && au.users.Count() == 0
	if user, state := au.authenticateState(r); state != authNone {
		answer := userOf(user)
		res.User = &answer
		// The lock of this login: a phone has its own (PLAN 69).
		minutes := au.lockMinutes(user.Name, r.UserAgent())
		res.LockMinutes = &minutes
		if state == authLocked {
			// What the lock screen needs, nothing more.
			res.Locked = true
			res.Passkey = hasPasskeyHere(r, user)
		} else {
			res.Version = au.version
		}
		// A login that stays keeps its cookie as long as the browser allows,
		// also after the setting was switched on for it.
		if cookie, err := r.Cookie(au.cookieName()); err == nil {
			if session, ok := au.sessions().Peek(cookie.Value); ok && session.Remember {
				au.setSessionCookie(w, cookie.Value, cookieAge(session))
			}
		}
	} else if look, _ := au.loginAppearance(); look.HideHost {
		// The login page was set not to name the NAS.
		res.Hostname = ""
	}
	writeJSON(w, http.StatusOK, res)
}

type credentials struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	// Code is the one-time code of an account with two-factor login.
	Code string `json:"code"`
}

func (c *credentials) normalize() {
	c.Name = strings.ToLower(strings.TrimSpace(c.Name))
}

// handleSetup creates the owner account. It only works while no account exists.
func (au authArea) handleSetup(w http.ResponseWriter, r *http.Request) {
	var req credentials
	if !readJSON(w, r, &req) {
		return
	}
	req.normalize()
	if !au.ensureUsersLoaded() {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "/config is not available, is the stick plugged in?")
		return
	}

	setupMu.Lock()
	user, err := au.users.CreateOwner(req.Name, req.Password)
	setupMu.Unlock()
	switch {
	case errors.Is(err, auth.ErrSetupDone):
		writeError(w, http.StatusConflict, "setup_done", "the owner account already exists")
	case errors.Is(err, auth.ErrInvalidName):
		writeError(w, http.StatusBadRequest, "invalid_name", "the name must start with a letter and may contain a-z, 0-9, - and _ (at most 32 characters)")
	case errors.Is(err, auth.ErrReservedName):
		writeError(w, http.StatusBadRequest, "reserved_name", "this name is reserved for the system")
	case errors.Is(err, auth.ErrPasswordTooShort):
		writeError(w, http.StatusBadRequest, "password_too_short", "the password is too short")
	case errors.Is(err, auth.ErrPasswordTooLong):
		writeError(w, http.StatusBadRequest, "password_too_long", "the password is too long")
	case err != nil:
		slog.Error("create owner", "err", err)
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the account could not be saved to /config")
	default:
		slog.Info("owner account created", "user", user.Name, "ip", remoteIP(r))
		au.recordEvent(r, user.Name, "setup", "", true)
		au.shares.Kick()
		au.ssh.Kick()
		// Defaults that only new installations get (an update never
		// changes how an existing NAS behaves).
		if au.configBackup != nil {
			if err := au.configBackup.NewInstall(); err != nil {
				slog.Warn("defaults of the configuration backup for a new installation not stored", "err", err)
			}
		}
		if au.startSession(w, r, user) {
			writeJSON(w, http.StatusCreated, userOf(user))
		}
	}
}

func (au authArea) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req credentials
	if !readJSON(w, r, &req) {
		return
	}
	req.normalize()
	ip := remoteIP(r)
	userKey, ipKey := "user:"+req.Name, passkeySource(r)
	// A browser that logged into this account before is limited on its
	// own (login_device.go): failures of strangers lock the account name
	// for unknown devices only, never its owner out. The lock of the
	// address holds for everyone.
	guessKey := userKey
	if device := au.knownDevice(r, req.Name); device != "" {
		guessKey = device
	}

	blocked := func() bool {
		wait := au.limiter().RetryAfter(guessKey, ipKey)
		if wait <= 0 {
			return false
		}
		au.recordEvent(r, req.Name, "login_blocked", "", false)
		writeRateLimited(w, wait, "too many failed logins, try again later")
		return true
	}
	if blocked() {
		return
	}
	if !au.ensureUsersLoaded() {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "/config is not available, is the stick plugged in?")
		return
	}

	// Verify also passes a locked account: whether it is locked is told
	// only after the password (and the second factor) were right, so the
	// answer reveals nothing to someone guessing.
	user, ok := au.users.Verify(req.Name, req.Password)
	// Guesses sent at the same time all pass the check above before the
	// first of them is counted (hashing is slow and runs two at a time).
	// Once the limit was reached meanwhile, the answer is not told, right
	// or wrong; a wrong guess still counts.
	if au.limiter().RetryAfter(guessKey, ipKey) > 0 {
		if !ok {
			au.limiter().Fail(guessKey, ipKey)
			au.firewall.Failed(ip, firewall.SourceWeb)
		}
		blocked()
		return
	}
	if !ok {
		au.limiter().Fail(guessKey, ipKey)
		au.firewall.Failed(ip, firewall.SourceWeb)
		slog.Warn("login failed", "user", req.Name, "ip", ip)
		au.recordEvent(r, req.Name, "login_failed", "", false)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "wrong name or password")
		return
	}
	if user.TwoFactor() {
		// The password was right; say so by asking for the code, which
		// an attacker without the password never gets to see.
		if req.Code == "" {
			writeError(w, http.StatusUnauthorized, "second_factor_required", "enter the code from your authenticator app")
			return
		}
		codeOK := au.users.CheckSecondFactor(user.Name, req.Code)
		// As for the password: codes guessed at the same time are not
		// answered once the limit was reached meanwhile.
		if au.limiter().RetryAfter(guessKey, ipKey) > 0 {
			if !codeOK {
				au.limiter().Fail(guessKey, ipKey)
				au.firewall.Failed(ip, firewall.SourceWeb)
			}
			blocked()
			return
		}
		if !codeOK {
			au.limiter().Fail(guessKey, ipKey)
			au.firewall.Failed(ip, firewall.SourceWeb)
			slog.Warn("login failed at the second factor", "user", req.Name, "ip", ip)
			au.recordEvent(r, req.Name, "login_failed", "2fa", false)
			writeError(w, http.StatusUnauthorized, "invalid_code", "the code is wrong or was used already")
			return
		}
	}
	if user.Disabled {
		// Not a guess, so the limiter is left alone; no session, no SMB hash.
		slog.Warn("login of a locked account refused", "user", user.Name, "ip", ip)
		au.recordEvent(r, user.Name, "login_disabled", "", false)
		writeError(w, http.StatusForbidden, "account_disabled", "this account is locked; ask an owner or administrator")
		return
	}
	au.limiter().Reset(userKey)
	au.limiter().Reset(guessKey)
	au.rememberDevice(w, user.Name)
	slog.Info("login", "user", user.Name, "ip", ip)
	// Accounts from before shares existed get their SMB password now, the
	// only moment the password is known.
	if _, stored := au.users.RememberSMBHash(user.Name, req.Password); stored {
		au.shares.Kick()
		au.ssh.Kick()
	}
	if au.startSession(w, r, user) {
		au.recordEvent(r, user.Name, "login", "", true)
		if user.Role.CanAdminister() && au.update != nil {
			au.update.CheckAfterLogin()
		}
		writeJSON(w, http.StatusOK, userOf(user))
	}
}

// writeRateLimited answers a request that has to wait for the limiter.
func writeRateLimited(w http.ResponseWriter, wait time.Duration, message string) {
	writeJSON(w, http.StatusTooManyRequests, map[string]apiError{"error": {
		Code: "rate_limited", Message: message, RetryAfter: int(math.Ceil(wait.Seconds())),
	}})
}

func (au authArea) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(au.cookieName()); err == nil {
		if session, ok := au.sessions().Get(cookie.Value); ok {
			au.recordEvent(r, session.User, "logout", "", true)
		}
		au.sessions().Delete(cookie.Value)
	}
	au.setSessionCookie(w, "", -1)
	w.WriteHeader(http.StatusNoContent)
}
