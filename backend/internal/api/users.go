package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/sshd"
)

type accountResponse struct {
	Name    string    `json:"name"`
	Role    auth.Role `json:"role"`
	Created time.Time `json:"created"`
	// SMBReady is false for accounts from before shares existed: they can
	// use SMB after their next login to the web UI.
	SMBReady bool `json:"smbReady"`
	// TwoFactor says whether the account logs in with a second factor.
	TwoFactor bool `json:"twoFactor"`
	// Disabled says the account is locked, since DisabledAt by DisabledBy.
	Disabled   bool       `json:"disabled"`
	DisabledAt *time.Time `json:"disabledAt,omitempty"`
	DisabledBy string     `json:"disabledBy,omitempty"`
}

func accountOf(user auth.User) accountResponse {
	res := accountResponse{Name: user.Name, Role: user.Role, Created: user.Created, SMBReady: user.SMBHash != "", TwoFactor: user.TwoFactor(), Disabled: user.Disabled, DisabledBy: user.DisabledBy}
	if user.Disabled && !user.DisabledAt.IsZero() {
		at := user.DisabledAt
		res.DisabledAt = &at
	}
	return res
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
	case errors.Is(err, auth.ErrLastActiveOwner):
		writeError(w, http.StatusConflict, "last_active_owner", "the last owner that is not locked cannot be locked, removed or demoted")
	default:
		slog.Error("account change failed", "err", err)
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the account could not be saved to /config")
	}
}

// usersArea is the accounts: list, create, change, lock, password, delete;
// an area with narrow dependencies (PLAN 8a, see fansArea). shares and ssh
// take up a changed account at once, notify forgets where a removed account
// was reached. sessions, limiter, tokens and prefs are state of the server:
// the logins, the limit against guessed passwords, the API tokens and the
// preferences a changed or removed account loses. reconfirmProof asks for the
// password or passkey again, cookieName names the session cookie,
// forgetWebDAV and viewerForgetUser clear what else belongs to an account.
type usersArea struct {
	users            *auth.Users
	shares           *shares.Manager
	ssh              *sshd.Manager
	notify           *notify.Manager
	sessions         sessionsFunc
	limiter          limiterFunc
	tokens           *auth.Tokens
	prefs            *preferences
	reconfirmProof   func(w http.ResponseWriter, user auth.User, p proof) bool
	cookieName       func() string
	forgetWebDAV     func(name string)
	viewerForgetUser func(name string)
}

// usersArea is the area of the server.
func (s *Server) usersArea() usersArea {
	return usersArea{users: s.opts.Users, shares: s.opts.Shares, ssh: s.opts.SSH, notify: s.opts.Notify,
		sessions: s.sessionStore, limiter: s.rateLimiter, tokens: s.tokens, prefs: s.prefs,
		reconfirmProof: s.authArea().reconfirmProof, cookieName: s.authArea().cookieName, forgetWebDAV: s.davArea().forgetWebDAV, viewerForgetUser: s.viewerArea().viewerForgetUser}
}

func (a usersArea) register(mux *routeMux, admin, user func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/users", admin(a.handleListUsers))
	mux.HandleFunc("POST /api/v1/users", admin(a.handleCreateUser))
	mux.HandleFunc("PUT /api/v1/users/{name}", admin(a.handleUpdateUser))
	mux.HandleFunc("DELETE /api/v1/users/{name}", admin(a.handleDeleteUser))
	mux.HandleFunc("POST /api/v1/users/{name}/lock", admin(a.handleLockUser(true)))
	mux.HandleFunc("POST /api/v1/users/{name}/unlock", admin(a.handleLockUser(false)))
	// Everyone may change their own password.
	mux.HandleFunc("POST /api/v1/users/{name}/password", user(a.handleSetPassword))
}

func (a usersArea) handleListUsers(w http.ResponseWriter, r *http.Request) {
	accounts := []accountResponse{}
	for _, user := range a.users.List() {
		accounts = append(accounts, accountOf(user))
	}
	writeJSON(w, http.StatusOK, accounts)
}

func (a usersArea) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		credentials
		Role auth.Role `json:"role"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Name)
	req.normalize()
	actor := currentUser(r)
	if !mayManage(actor, req.Role) {
		writeError(w, http.StatusForbidden, "forbidden", "only an owner can create owners")
		return
	}
	user, err := a.users.Create(req.Name, req.Password, req.Role)
	if err != nil {
		writeAccountError(w, err)
		return
	}
	slog.Info("account created", "user", user.Name, "role", user.Role, "by", actor.Name, "ip", remoteIP(r))
	a.shares.Kick()
	a.ssh.Kick()
	writeJSON(w, http.StatusCreated, accountOf(user))
}

func (a usersArea) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role auth.Role `json:"role"`
		// ResetTwoFactor switches the second factor of the account off,
		// for someone who lost the phone and the recovery codes.
		ResetTwoFactor bool `json:"resetTwoFactor"`
		// For one's own account the reset needs the same proof as
		// switching the second factor off (password and code, or a
		// passkey); administrators resetting another account do not.
		proof
	}
	if !readJSON(w, r, &req) {
		return
	}
	actor := currentUser(r)
	target, ok := a.users.Get(r.PathValue("name"))
	if !ok {
		writeAccountError(w, auth.ErrNotFound)
		return
	}
	if !mayManage(actor, target.Role) || !mayManage(actor, req.Role) {
		writeError(w, http.StatusForbidden, "forbidden", "owners are only managed by owners")
		return
	}
	// Checked before anything changes: a reset must not happen for a
	// request that then fails.
	if req.Role != auth.RoleOwner && req.Role != auth.RoleAdmin && req.Role != auth.RoleUser {
		writeAccountError(w, auth.ErrInvalidRole)
		return
	}
	if req.ResetTwoFactor && target.Name == actor.Name && !a.reconfirmProof(w, actor, req.proof) {
		return
	}
	// The role first: it is what can be refused (the last owner stays
	// one), and a refused request must not have reset anything.
	user, err := a.users.SetRole(target.Name, req.Role)
	if err != nil {
		writeAccountError(w, err)
		return
	}
	if req.ResetTwoFactor {
		if err := a.users.DisableTwoFactor(target.Name); err != nil {
			writeAccountError(w, err)
			return
		}
		// Passkeys are factors of the lost devices too.
		if err := a.users.ClearPasskeys(target.Name); err != nil {
			writeAccountError(w, err)
			return
		}
		// Whoever had the lost phone is logged out as well.
		a.sessions().DeleteUser(target.Name, "")
		a.forgetWebDAV(target.Name)
		slog.Info("two-factor login reset", "user", target.Name, "by", actor.Name, "ip", remoteIP(r))
		if fresh, ok := a.users.Get(target.Name); ok {
			user = fresh
		}
	}
	slog.Info("account role changed", "user", user.Name, "role", user.Role, "by", actor.Name, "ip", remoteIP(r))
	// SSH is for owners and administrators only.
	a.ssh.Kick()
	writeJSON(w, http.StatusOK, accountOf(user))
}

// handleSetPassword changes a password: one's own with the current one as
// proof, or someone else's as their administrator.
func (a usersArea) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
		Current  string `json:"current"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	actor := currentUser(r)
	target, ok := a.users.Get(r.PathValue("name"))
	self := ok && target.Name == actor.Name
	switch {
	case self:
		// The limiter also guards this check: it is a password guess like any other.
		key := "user:" + actor.Name
		if wait := a.limiter().RetryAfter(key); wait > 0 {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed attempts, try again later")
			return
		}
		_, valid := a.users.Authenticate(actor.Name, req.Current)
		// Guesses sent at the same time all passed the check above.
		if a.limiter().RetryAfter(key) > 0 {
			if !valid {
				a.limiter().Fail(key)
			}
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed attempts, try again later")
			return
		}
		if !valid {
			a.limiter().Fail(key)
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

	if _, err := a.users.SetPassword(target.Name, req.Password); err != nil {
		writeAccountError(w, err)
		return
	}
	// Whoever knew the old password is logged out; the one changing their
	// own password stays logged in on this device.
	keep := ""
	if cookie, err := r.Cookie(a.cookieName()); err == nil && self {
		keep = cookie.Value
	}
	a.sessions().DeleteUser(target.Name, keep)
	a.forgetWebDAV(target.Name)
	slog.Info("password changed", "user", target.Name, "by", actor.Name, "ip", remoteIP(r))
	a.shares.Kick()
	a.ssh.Kick()
	w.WriteHeader(http.StatusNoContent)
}

func (a usersArea) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	target, ok := a.users.Get(r.PathValue("name"))
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
	if err := a.users.Delete(target.Name); err != nil {
		writeAccountError(w, err)
		return
	}
	a.sessions().DeleteUser(target.Name, "")
	if err := a.tokens.RemoveUser(target.Name); err != nil {
		slog.Error("remove API tokens of the deleted account", "user", target.Name, "err", err)
	}
	go a.viewerForgetUser(target.Name)
	a.prefs.forget(target.Name)
	a.notify.ForgetPersonal(target.Name)
	if err := a.shares.ForgetUser(target.Name); err != nil {
		slog.Error("remove account from shares", "user", target.Name, "err", err)
	}
	slog.Info("account deleted", "user", target.Name, "by", actor.Name, "ip", remoteIP(r))
	a.shares.Kick()
	a.ssh.Kick()
	w.WriteHeader(http.StatusNoContent)
}

// handleLockUser locks (POST …/lock) or unlocks (POST …/unlock) an account.
// Nobody locks themselves, owners are only locked by owners, and the last
// owner that is not locked stays unlocked. Locking ends every session of the
// account at once; open consoles notice within consoleRecheck, SSH sessions
// end with the next apply of sshd and SMB connections with that of Samba.
func (a usersArea) handleLockUser(disabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := currentUser(r)
		target, ok := a.users.Get(r.PathValue("name"))
		if !ok {
			writeAccountError(w, auth.ErrNotFound)
			return
		}
		if target.Name == actor.Name {
			writeError(w, http.StatusConflict, "cannot_lock_self", "you cannot lock the account you are logged in with")
			return
		}
		if !mayManage(actor, target.Role) {
			writeError(w, http.StatusForbidden, "forbidden", "owners are only managed by owners")
			return
		}
		user, err := a.users.SetDisabled(target.Name, disabled, actor.Name)
		if err != nil {
			writeAccountError(w, err)
			return
		}
		if disabled {
			a.sessions().DeleteUser(user.Name, "")
			slog.Info("account locked", "user", user.Name, "by", actor.Name, "ip", remoteIP(r))
		} else {
			slog.Info("account unlocked", "user", user.Name, "by", actor.Name, "ip", remoteIP(r))
		}
		// Samba and sshd take the account away or give it back.
		a.shares.Kick()
		a.ssh.Kick()
		writeJSON(w, http.StatusOK, accountOf(user))
	}
}
