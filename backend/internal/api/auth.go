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
)

// cookieName uses the __Host- prefix over HTTPS: browsers then only accept
// the cookie from exactly this host, over a secure connection.
func (s *Server) cookieName() string {
	if s.opts.SecureCookies {
		return "__Host-titanos_session"
	}
	return "titanos_session"
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName(),
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.opts.SecureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, user auth.User) bool {
	token, err := s.sessions.Create(user.Name, remoteIP(r), r.UserAgent())
	if err != nil {
		slog.Error("create session", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not start a session")
		return false
	}
	s.setSessionCookie(w, token, int(auth.SessionMaxAge.Seconds()))
	return true
}

// ensureUsersLoaded retries reading the accounts if that failed at startup,
// for example because the stick was not there yet.
func (s *Server) ensureUsersLoaded() bool {
	if s.opts.Users.Loaded() {
		return true
	}
	s.loadMu.Lock()
	defer s.loadMu.Unlock()
	if s.opts.Users.Loaded() {
		return true
	}
	if time.Since(s.lastLoadTry) < 5*time.Second {
		return false
	}
	s.lastLoadTry = time.Now()
	if err := s.opts.Users.Load(); err != nil {
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
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	hostname, _ := os.Hostname()
	res := sessionResponse{Hostname: hostname}
	res.ConfigAvailable = s.ensureUsersLoaded()
	res.SetupRequired = res.ConfigAvailable && s.opts.Users.Count() == 0
	if user, ok := s.authenticate(r); ok {
		answer := userOf(user)
		res.User = &answer
		res.Version = s.opts.Version
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
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var req credentials
	if !readJSON(w, r, &req) {
		return
	}
	req.normalize()
	if !s.ensureUsersLoaded() {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "/config is not available, is the stick plugged in?")
		return
	}

	setupMu.Lock()
	user, err := s.opts.Users.CreateOwner(req.Name, req.Password)
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
		s.recordEvent(r, user.Name, "setup", "", true)
		s.opts.Shares.Kick()
		s.opts.SSH.Kick()
		if s.startSession(w, r, user) {
			writeJSON(w, http.StatusCreated, userOf(user))
		}
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req credentials
	if !readJSON(w, r, &req) {
		return
	}
	req.normalize()
	ip := remoteIP(r)
	userKey, ipKey := "user:"+req.Name, "ip:"+ip

	if wait := s.limiter.RetryAfter(userKey, ipKey); wait > 0 {
		seconds := int(math.Ceil(wait.Seconds()))
		s.recordEvent(r, req.Name, "login_blocked", "", false)
		writeJSON(w, http.StatusTooManyRequests, map[string]apiError{"error": {
			Code: "rate_limited", Message: "too many failed logins, try again later", RetryAfter: seconds,
		}})
		return
	}
	if !s.ensureUsersLoaded() {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "/config is not available, is the stick plugged in?")
		return
	}

	user, ok := s.opts.Users.Authenticate(req.Name, req.Password)
	if !ok {
		s.limiter.Fail(userKey, ipKey)
		slog.Warn("login failed", "user", req.Name, "ip", ip)
		s.recordEvent(r, req.Name, "login_failed", "", false)
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
		if !s.opts.Users.CheckSecondFactor(user.Name, req.Code) {
			s.limiter.Fail(userKey, ipKey)
			slog.Warn("login failed at the second factor", "user", req.Name, "ip", ip)
			s.recordEvent(r, req.Name, "login_failed", "2fa", false)
			writeError(w, http.StatusUnauthorized, "invalid_code", "the code is wrong or was used already")
			return
		}
	}
	s.limiter.Reset(userKey)
	slog.Info("login", "user", user.Name, "ip", ip)
	// Accounts from before shares existed get their SMB password now, the
	// only moment the password is known.
	if _, stored := s.opts.Users.RememberSMBHash(user.Name, req.Password); stored {
		s.opts.Shares.Kick()
		s.opts.SSH.Kick()
	}
	if s.startSession(w, r, user) {
		s.recordEvent(r, user.Name, "login", "", true)
		if user.Role.CanAdminister() && s.opts.Update != nil {
			s.opts.Update.CheckAfterLogin()
		}
		writeJSON(w, http.StatusOK, userOf(user))
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(s.cookieName()); err == nil {
		if session, ok := s.sessions.Get(cookie.Value); ok {
			s.recordEvent(r, session.User, "logout", "", true)
		}
		s.sessions.Delete(cookie.Value)
	}
	s.setSessionCookie(w, "", -1)
	w.WriteHeader(http.StatusNoContent)
}
