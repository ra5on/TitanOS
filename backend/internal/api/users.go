package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

type accountResponse struct {
	Name    string    `json:"name"`
	Role    auth.Role `json:"role"`
	Created time.Time `json:"created"`
	// SMBReady is false for accounts from before shares existed: they can
	// use SMB after their next login to the web UI.
	SMBReady bool `json:"smbReady"`
}

func accountOf(user auth.User) accountResponse {
	return accountResponse{Name: user.Name, Role: user.Role, Created: user.Created, SMBReady: user.SMBHash != ""}
}

// mayManage reports whether actor may change target or give out role.
// Owners are only managed by owners.
func mayManage(actor auth.User, target auth.Role) bool {
	return actor.Role == auth.RoleOwner || (actor.Role == auth.RoleAdmin && target != auth.RoleOwner)
}

func writeAccountError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrNotLoaded):
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "/config is not available")
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, "user_not_found", "no such account")
	case errors.Is(err, auth.ErrExists):
		writeError(w, http.StatusConflict, "user_exists", "an account with this name exists")
	case errors.Is(err, auth.ErrInvalidName):
		writeError(w, http.StatusBadRequest, "invalid_name", "invalid account name")
	case errors.Is(err, auth.ErrReservedName):
		writeError(w, http.StatusBadRequest, "reserved_name", "this name is reserved for the system")
	case errors.Is(err, auth.ErrInvalidRole):
		writeError(w, http.StatusBadRequest, "invalid_role", "unknown role")
	case errors.Is(err, auth.ErrPasswordTooShort):
		writeError(w, http.StatusBadRequest, "password_too_short", "the password is too short")
	case errors.Is(err, auth.ErrPasswordTooLong):
		writeError(w, http.StatusBadRequest, "password_too_long", "the password is too long")
	case errors.Is(err, auth.ErrLastOwner):
		writeError(w, http.StatusConflict, "last_owner", "the last owner cannot be removed or demoted")
	default:
		slog.Error("account change failed", "err", err)
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the account could not be saved to /config")
	}
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	accounts := []accountResponse{}
	for _, user := range s.opts.Users.List() {
		accounts = append(accounts, accountOf(user))
	}
	writeJSON(w, http.StatusOK, accounts)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		credentials
		Role auth.Role `json:"role"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.normalize()
	actor := currentUser(r)
	if !mayManage(actor, req.Role) {
		writeError(w, http.StatusForbidden, "forbidden", "only an owner can create owners")
		return
	}
	user, err := s.opts.Users.Create(req.Name, req.Password, req.Role)
	if err != nil {
		writeAccountError(w, err)
		return
	}
	slog.Info("account created", "user", user.Name, "role", user.Role, "by", actor.Name, "ip", remoteIP(r))
	s.opts.Shares.Kick()
	writeJSON(w, http.StatusCreated, accountOf(user))
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role auth.Role `json:"role"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	actor := currentUser(r)
	target, ok := s.opts.Users.Get(r.PathValue("name"))
	if !ok {
		writeAccountError(w, auth.ErrNotFound)
		return
	}
	if !mayManage(actor, target.Role) || !mayManage(actor, req.Role) {
		writeError(w, http.StatusForbidden, "forbidden", "owners are only managed by owners")
		return
	}
	user, err := s.opts.Users.SetRole(target.Name, req.Role)
	if err != nil {
		writeAccountError(w, err)
		return
	}
	slog.Info("account role changed", "user", user.Name, "role", user.Role, "by", actor.Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, accountOf(user))
}

// handleSetPassword changes a password: one's own with the current one as
// proof, or someone else's as their administrator.
func (s *Server) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
		Current  string `json:"current"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	actor := currentUser(r)
	target, ok := s.opts.Users.Get(r.PathValue("name"))
	self := ok && target.Name == actor.Name
	switch {
	case self:
		// The limiter also guards this check: it is a password guess like any other.
		key := "user:" + actor.Name
		if wait := s.limiter.RetryAfter(key); wait > 0 {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed attempts, try again later")
			return
		}
		if _, valid := s.opts.Users.Authenticate(actor.Name, req.Current); !valid {
			s.limiter.Fail(key)
			writeError(w, http.StatusForbidden, "wrong_password", "the current password is wrong")
			return
		}
	case !actor.Role.CanAdminister():
		writeError(w, http.StatusForbidden, "forbidden", "this needs an owner or administrator")
		return
	case !ok:
		writeAccountError(w, auth.ErrNotFound)
		return
	case !mayManage(actor, target.Role):
		writeError(w, http.StatusForbidden, "forbidden", "owners are only managed by owners")
		return
	}

	if _, err := s.opts.Users.SetPassword(target.Name, req.Password); err != nil {
		writeAccountError(w, err)
		return
	}
	// Whoever knew the old password is logged out; the one changing their
	// own password stays logged in on this device.
	keep := ""
	if cookie, err := r.Cookie(s.cookieName()); err == nil && self {
		keep = cookie.Value
	}
	s.sessions.DeleteUser(target.Name, keep)
	slog.Info("password changed", "user", target.Name, "by", actor.Name, "ip", remoteIP(r))
	s.opts.Shares.Kick()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	target, ok := s.opts.Users.Get(r.PathValue("name"))
	if !ok {
		writeAccountError(w, auth.ErrNotFound)
		return
	}
	if target.Name == actor.Name {
		writeError(w, http.StatusConflict, "cannot_delete_self", "you cannot delete the account you are logged in with")
		return
	}
	if !mayManage(actor, target.Role) {
		writeError(w, http.StatusForbidden, "forbidden", "owners are only managed by owners")
		return
	}
	if err := s.opts.Users.Delete(target.Name); err != nil {
		writeAccountError(w, err)
		return
	}
	s.sessions.DeleteUser(target.Name, "")
	s.prefs.forget(target.Name)
	if err := s.opts.Shares.ForgetUser(target.Name); err != nil {
		slog.Error("remove account from shares", "user", target.Name, "err", err)
	}
	slog.Info("account deleted", "user", target.Name, "by", actor.Name, "ip", remoteIP(r))
	s.opts.Shares.Kick()
	w.WriteHeader(http.StatusNoContent)
}
