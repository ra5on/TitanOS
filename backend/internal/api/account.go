package api

import (
	"errors"
	"log/slog"
	"net/http"
	"os"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// What everyone does with their own account: the second factor and the
// devices that are logged in.

func (s *Server) sessionToken(r *http.Request) string {
	if cookie, err := r.Cookie(s.cookieName()); err == nil {
		return cookie.Value
	}
	return ""
}

func (s *Server) handleAccountSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sessions": s.sessions.List(currentUser(r).Name, s.sessionToken(r))})
}

func (s *Server) handleAccountSessionEnd(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if !s.sessions.DeleteByID(user.Name, r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "session_not_found", "no such session")
		return
	}
	slog.Info("session ended from the list of devices", "user", user.Name, "ip", remoteIP(r))
	s.handleAccountSessions(w, r)
}

// handleAccountSessionsEndOthers ends every session of the account except
// the one the request came with ("log out all other devices").
func (s *Server) handleAccountSessionsEndOthers(w http.ResponseWriter, r *http.Request) {
	user, token := currentUser(r), s.sessionToken(r)
	if token == "" {
		// Without a session of its own the request would end all of them.
		writeError(w, http.StatusBadRequest, "session_required", "only a logged-in browser can end its other sessions")
		return
	}
	s.sessions.DeleteUser(user.Name, token)
	slog.Info("other sessions ended from the list of devices", "user", user.Name, "ip", remoteIP(r))
	s.handleAccountSessions(w, r)
}

// handleTwoFactorBegin hands out a secret for the authenticator app. It
// only counts once handleTwoFactorEnable has seen a code made from it.
func (s *Server) handleTwoFactorBegin(w http.ResponseWriter, r *http.Request) {
	hostname, _ := os.Hostname()
	secret, uri, err := s.opts.Users.BeginTwoFactor(currentUser(r).Name, "TitanOS "+hostname)
	if err != nil {
		writeTwoFactorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "uri": uri})
}

func (s *Server) handleTwoFactorEnable(w http.ResponseWriter, r *http.Request) {
	var req struct{ Code string }
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	codes, err := s.opts.Users.EnableTwoFactor(user.Name, req.Code)
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
func (s *Server) reconfirm(w http.ResponseWriter, user auth.User, password, code string) bool {
	key := "user:" + user.Name
	if wait := s.limiter.RetryAfter(key); wait > 0 {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed attempts, try again later")
		return false
	}
	if _, ok := s.opts.Users.Authenticate(user.Name, password); !ok {
		s.limiter.Fail(key)
		writeError(w, http.StatusForbidden, "wrong_password", "the password is wrong")
		return false
	}
	if !s.opts.Users.CheckSecondFactor(user.Name, code) {
		s.limiter.Fail(key)
		writeError(w, http.StatusForbidden, "invalid_code", "the code is wrong or was used already")
		return false
	}
	return true
}

// handleTwoFactorDisable switches the second factor off, with the password
// and a code as proof that it is the owner of the account who asks.
func (s *Server) handleTwoFactorDisable(w http.ResponseWriter, r *http.Request) {
	var req struct{ Password, Code string }
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	key := "user:" + user.Name
	if wait := s.limiter.RetryAfter(key); wait > 0 {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed attempts, try again later")
		return
	}
	if _, ok := s.opts.Users.Authenticate(user.Name, req.Password); !ok {
		s.limiter.Fail(key)
		writeError(w, http.StatusForbidden, "wrong_password", "the password is wrong")
		return
	}
	if !s.opts.Users.CheckSecondFactor(user.Name, req.Code) {
		s.limiter.Fail(key)
		writeError(w, http.StatusForbidden, "invalid_code", "the code is wrong or was used already")
		return
	}
	if err := s.opts.Users.DisableTwoFactor(user.Name); err != nil {
		writeTwoFactorError(w, err)
		return
	}
	slog.Info("two-factor login switched off", "user", user.Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// handleTwoFactorRecovery replaces the recovery codes, with the password
// and a code as proof like switching the second factor off. The new codes
// are shown this once; only their hashes are kept.
func (s *Server) handleTwoFactorRecovery(w http.ResponseWriter, r *http.Request) {
	var req struct{ Password, Code string }
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	if user.TOTPSecret == "" {
		writeError(w, http.StatusConflict, "two_factor_not_begun", "two-factor login is off")
		return
	}
	key := "user:" + user.Name
	if wait := s.limiter.RetryAfter(key); wait > 0 {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed attempts, try again later")
		return
	}
	if _, ok := s.opts.Users.Authenticate(user.Name, req.Password); !ok {
		s.limiter.Fail(key)
		writeError(w, http.StatusForbidden, "wrong_password", "the password is wrong")
		return
	}
	if !s.opts.Users.CheckSecondFactor(user.Name, req.Code) {
		s.limiter.Fail(key)
		writeError(w, http.StatusForbidden, "invalid_code", "the code is wrong or was used already")
		return
	}
	codes, err := s.opts.Users.NewRecoveryCodes(user.Name)
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
