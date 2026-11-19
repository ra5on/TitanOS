package api

import (
	"errors"
	"github.com/ra5on/TitanOS/backend/internal/appdevices"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/pim/devices"
)

// Device passwords of T-Kalender and Kontakte (TECHNIK 3, pim/devices):
// one per iPhone or Mac, made under "Mein Konto" with the account password
// and second factor, shown once. Owners and administrators see and revoke
// the devices of the accounts they manage (a lost phone), but make none
// for others. Every route is for a web session of this node only: no
// token, no MCP, no agent, not through the cluster (the passwords live
// only on the node that made them, with the data).

// pimArea is calendars and contacts (PLAN 8a, see fansArea): for now the
// device passwords. users are the accounts, devices the manager (state of
// the server), webSession and reconfirm guard a new password.
type pimArea struct {
	users      *auth.Users
	devices    *devices.Manager
	webSession webSessionFunc
	reconfirm  reconfirmFunc
	// setup is what the iPhone needs (addresses, TECHNIK 4.2).
	setup func(account string) deviceSetup
}

// pimArea is the area of the server.
func (s *Server) pimArea() pimArea {
	return pimArea{users: s.opts.Users, devices: s.devices, webSession: s.requireWebSession, reconfirm: s.authArea().reconfirm,
		setup: s.deviceSetup}
}

// newAppDevices builds the manager of the paired Android apps.
func newAppDevices(opts Options, stay func() int) *appdevices.Manager {
	var users appdevices.Accounts
	if opts.Users != nil {
		users = opts.Users
	}
	return appdevices.New(opts.Store, users, stay)
}

// newDevices builds the manager of the device passwords; "last synced"
// goes to the store of calendars when there is one.
func newDevices(opts Options, limiter limiterFunc) *devices.Manager {
	var seen devices.SeenStore
	if opts.PIM != nil {
		seen = opts.PIM
	}
	var users devices.Accounts
	if opts.Users != nil {
		users = opts.Users
	}
	m := devices.New(opts.Store, users, limiter, seen)
	m.RemoteIP = remoteIP
	return m
}

func (a pimArea) register(mux *routeMux, g guards) {
	web := func(h http.HandlerFunc) http.HandlerFunc { return g.user(g.clusterLocal(g.browserOnly(h))) }
	admin := func(h http.HandlerFunc) http.HandlerFunc { return g.admin(g.clusterLocal(g.browserOnly(h))) }
	mux.HandleFunc("GET /api/v1/account/devices", web(a.handleDevices))
	mux.HandleFunc("POST /api/v1/account/devices", web(a.handleDeviceNew))
	mux.HandleFunc("DELETE /api/v1/account/devices/{id}", web(a.handleDeviceRevoke))
	mux.HandleFunc("GET /api/v1/account/devices/setup", web(a.handleDeviceSetup))
	mux.HandleFunc("GET /api/v1/users/{name}/devices", admin(a.handleUserDevices))
	mux.HandleFunc("DELETE /api/v1/users/{name}/devices/{id}", admin(a.handleUserDeviceRevoke))
}

func writeDeviceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, devices.ErrInvalidName):
		writeError(w, http.StatusBadRequest, "invalid_device_name", "the name needs 1 to 64 characters without control or invisible characters")
	case errors.Is(err, devices.ErrNameTaken):
		writeError(w, http.StatusConflict, "device_name_taken", "a device of this name exists")
	case errors.Is(err, devices.ErrTooMany):
		writeError(w, http.StatusConflict, "device_limit", "an account has at most 20 devices")
	case errors.Is(err, devices.ErrNotFound):
		writeError(w, http.StatusNotFound, "device_not_found", "no such device")
	default:
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "the device passwords could not be read or saved on /config")
	}
}

func (a pimArea) list(w http.ResponseWriter, user auth.User) {
	list, err := a.devices.List(user)
	if err != nil {
		writeDeviceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": list, "max": devices.Max})
}

// handleDevices lists the own devices with "last synced"; never a hash.
func (a pimArea) handleDevices(w http.ResponseWriter, r *http.Request) {
	a.list(w, currentUser(r))
}

// handleDeviceSetup tells the setup page of the iPhone the account and
// the servers to type in (TECHNIK 4.2): every account, host names and
// addresses only.
func (a pimArea) handleDeviceSetup(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.setup(currentUser(r).Name))
}

// handleDeviceNew makes a device password and shows it once.
func (a pimArea) handleDeviceNew(w http.ResponseWriter, r *http.Request) {
	if !a.webSession(w, r) {
		return
	}
	var req struct {
		Name     string `json:"name"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	// A name that cannot be used, or a 21st device, costs no attempt of
	// the password.
	if err := a.devices.Available(user, req.Name); err != nil {
		writeDeviceError(w, err)
		return
	}
	// Password and code like an API token: the device password is another
	// way into the calendars and contacts of the account.
	if !a.reconfirm(w, user, req.Password, req.Code) {
		return
	}
	created, err := a.devices.Create(user, req.Name)
	if err != nil {
		writeDeviceError(w, err)
		return
	}
	noteTarget(r, created.Name)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, created)
}

// handleDeviceRevoke takes a device its way at once.
func (a pimArea) handleDeviceRevoke(w http.ResponseWriter, r *http.Request) {
	if err := a.devices.Revoke(currentUser(r), r.PathValue("id")); err != nil {
		writeDeviceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// managed is the account of {name} if the actor manages it (owners only by
// owners).
func (a pimArea) managed(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	noteTarget(r, r.PathValue("name"))
	target, ok := a.users.Get(r.PathValue("name"))
	if !ok {
		writeAccountError(w, auth.ErrNotFound)
		return auth.User{}, false
	}
	if !mayManage(currentUser(r), target.Role) {
		writeError(w, http.StatusForbidden, "forbidden", "owners are only managed by owners")
		return auth.User{}, false
	}
	return target, true
}

// handleUserDevices lists the devices of an account (Benutzer › Konto).
func (a pimArea) handleUserDevices(w http.ResponseWriter, r *http.Request) {
	if target, ok := a.managed(w, r); ok {
		a.list(w, target)
	}
}

// handleUserDeviceRevoke revokes a device of an account: a lost phone.
func (a pimArea) handleUserDeviceRevoke(w http.ResponseWriter, r *http.Request) {
	target, ok := a.managed(w, r)
	if !ok {
		return
	}
	if err := a.devices.Revoke(target, r.PathValue("id")); err != nil {
		writeDeviceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// devicesRevokeAll revokes the devices of an account whose password
// changed or whose second factor was reset (here or by another node).
func (s *Server) devicesRevokeAll(name string) {
	s.devices.RevokeAll(name)
	s.profileLinks.Forget(name)
	s.appsRevokeAll(name)
}

// devicesRebind keeps the devices of an account whose second factor the
// person switched on or off in its own session (password and code shown):
// without it the changed fingerprint would revoke them (S-25).
func (s *Server) devicesRebind(before auth.User) {
	if s.devices != nil {
		s.devices.Rebind(before)
	}
	s.appsRebind(before)
}

// pimForgetUser drops what T-Kalender and Kontakte keep of an account that
// was deleted or made anew: the device passwords in RAM (devices.json goes
// with users/<name>) and the calendars and address books on the volume
// (pim/store ForgetUser; without a volume owner.json keeps them from a new
// account of the name). It runs before the answer, like T-Mail's and unlike
// the agent's data (goJob): the store removes by name, and a removal still
// running after a new account of the same name was made could take that
// account's first entries along (TestDeviceAccountDeleteTakesCalendarAlong).
// The store refuses the account while it removes (503), so a device asking
// meanwhile is told to come back, never shown an empty collection.
func (s *Server) pimForgetUser(name string) {
	s.devices.Forget(name)
	s.profileLinks.Forget(name)
	s.appsForget(name)
	s.pimLive.forget(name)
	s.reminders.forget(name)
	s.pimCache.forget(name)
	if s.opts.PIM != nil {
		_ = s.opts.PIM.ForgetUser(name) // logged by the package
	}
}
