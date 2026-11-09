package api

// Programme (PLAN 61): who may use which Docker app, the apps of the
// current account for the desktop, the short tokens that open an app in a
// window, and the certificate of the TitanOS authority for devices.
// None of these routes is in publicRoutes: they are for the web UI only.

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/appaccess"
	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/docker"
)

// dockerApp is one Docker app (Compose project) as the pages list it.
type dockerApp struct {
	App     string `json:"app"`
	Icon    string `json:"icon"`
	URL     string `json:"url"`
	WebUI   string `json:"webui"`
	Running bool   `json:"running"`
	// Stream is the desktop stream the app brings itself (PLAN 75-14):
	// "novnc", "selkies" or "kasmvnc"; such an app opens directly in its
	// window when the browser reaches its name.
	Stream string `json:"stream,omitempty"`
}

// appStream tells from a container's image (and the container port of its
// WebUI) whether the app brings its own desktop stream.
func appStream(c docker.Container) string {
	image := strings.ToLower(c.Image)
	switch {
	case strings.Contains(image, "jlesage/"):
		return "novnc"
	case strings.Contains(image, "kasmweb/"):
		return "kasmvnc"
	case strings.Contains(image, "linuxserver/"):
		// The desktop images of linuxserver (Selkies, before that
		// KasmVNC) serve their desktop on 3000 (http) and 3001 (https).
		port := appproxy.WebUIPort(c.WebUI)
		for _, p := range c.Ports {
			if p.Host == port && (p.Container == 3000 || p.Container == 3001) {
				return "selkies"
			}
		}
	}
	return ""
}

// dockerApps lists the apps with their icon, HTTPS name and state, sorted.
func (s *Server) dockerApps(r *http.Request) []dockerApp {
	if s.opts.Docker == nil {
		return nil
	}
	status := s.opts.Docker.Status(r.Context())
	byName := map[string]*dockerApp{}
	add := func(name string) *dockerApp {
		if a := byName[name]; a != nil {
			return a
		}
		a := &dockerApp{App: name}
		if s.opts.AppProxy != nil {
			a.URL = s.opts.AppProxy.URL(name)
		}
		byName[name] = a
		return a
	}
	for _, name := range status.Apps {
		add(name)
	}
	for _, c := range status.Containers {
		if c.App == "" {
			continue
		}
		a := add(c.App)
		if a.Icon == "" {
			a.Icon = c.Icon
		}
		if a.WebUI == "" {
			a.WebUI = c.WebUI
		}
		if a.Stream == "" && c.WebUI != "" {
			a.Stream = appStream(c)
		}
		a.Running = a.Running || c.State == "running"
	}
	out := make([]dockerApp, 0, len(byName))
	for _, a := range byName {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].App < out[j].App })
	return out
}

// appAccessView is GET /api/v1/apps/access.
type appAccessView struct {
	// NewApps is "admins" or "everyone".
	NewApps  string          `json:"newApps"`
	Apps     []appAccessApp  `json:"apps"`
	Accounts []appAccessUser `json:"accounts"`
}

type appAccessApp struct {
	dockerApp
	// Gate is "Anmeldung im Browser prüfen".
	Gate bool `json:"gate"`
	// Users are the normal accounts with a tick.
	Users []string `json:"users"`
}

type appAccessUser struct {
	Name     string    `json:"name"`
	Role     auth.Role `json:"role"`
	Disabled bool      `json:"disabled"`
}

func (s *Server) appAccessView(r *http.Request) appAccessView {
	settings := appaccess.Settings{Apps: map[string]appaccess.App{}}
	if s.opts.AppAccess != nil {
		settings = s.opts.AppAccess.Settings()
	}
	view := appAccessView{NewApps: appaccess.NewAppsAdmins, Apps: []appAccessApp{}, Accounts: []appAccessUser{}}
	if settings.NewApps == appaccess.NewAppsEveryone {
		view.NewApps = appaccess.NewAppsEveryone
	}
	var normal []string
	for _, u := range s.opts.Users.List() {
		view.Accounts = append(view.Accounts, appAccessUser{Name: u.Name, Role: u.Role, Disabled: u.Disabled})
		if !u.Role.CanAdminister() {
			normal = append(normal, u.Name)
		}
	}
	for _, a := range s.dockerApps(r) {
		entry, ok := settings.Apps[a.App]
		users := []string{}
		switch {
		case ok:
			for _, name := range entry.Users {
				if slices.Contains(normal, name) {
					users = append(users, name)
				}
			}
		default:
			for _, u := range normal {
				if settings.NewAppsFor(u) {
					users = append(users, u)
				}
			}
		}
		view.Apps = append(view.Apps, appAccessApp{dockerApp: a, Gate: !entry.NoGate, Users: users})
	}
	return view
}

// handleAppAccess shows who may use which Docker app.
func (s *Server) handleAppAccess(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.appAccessView(r))
}

// handleAppAccessSet stores who may use which Docker app:
// {newApps, apps: [{app, gate, users}]}. Apps left out fall back to the
// setting for new apps.
func (s *Server) handleAppAccessSet(w http.ResponseWriter, r *http.Request) {
	if s.opts.AppAccess == nil {
		writeError(w, http.StatusNotFound, "not_found", "no app rights")
		return
	}
	var req struct {
		NewApps string `json:"newApps"`
		Apps    []struct {
			App   string   `json:"app"`
			Gate  bool     `json:"gate"`
			Users []string `json:"users"`
		} `json:"apps"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	current := s.opts.AppAccess.Settings()
	settings := appaccess.Settings{NewApps: req.NewApps, Apps: map[string]appaccess.App{}, PerUser: current.PerUser, NewAppsUsers: current.NewAppsUsers}
	for _, a := range req.Apps {
		settings.Apps[a.App] = appaccess.App{Users: a.Users, NoGate: !a.Gate}
	}
	if err := s.opts.AppAccess.Set(settings); err != nil {
		if errors.Is(err, appaccess.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "app_access_invalid", err.Error())
			return
		}
		slog.Error("app rights not saved", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not save the setting")
		return
	}
	noteTarget(r, "apps")
	go s.viewerRecheckRights()
	slog.Info("app rights changed", "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleAppAccess(w, r)
}

// allowsApp tells whether the account of the request may use an app.
func (s *Server) allowsApp(r *http.Request, app string) bool {
	user := currentUser(r)
	if s.opts.AppAccess == nil {
		return user.Role.CanAdminister()
	}
	return s.opts.AppAccess.Allows(user.Name, app)
}

// handleAppsMine lists the Docker apps the account may use, for the
// desktop: {apps: [{app, icon, url, webui, running}]}; url is the HTTPS
// name ("" without one).
func (s *Server) handleAppsMine(w http.ResponseWriter, r *http.Request) {
	out := []dockerApp{}
	for _, a := range s.dockerApps(r) {
		if (a.URL != "" || a.WebUI != "") && s.allowsApp(r, a.App) {
			out = append(out, a)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": out})
}

// appFrameSources are the origins the web UI may frame (direct windows).
func (s *Server) appFrameSources() []string {
	if s.opts.AppProxy == nil {
		return nil
	}
	return s.opts.AppProxy.FrameSources()
}

// enterURL makes the address that lets the account's browser in to an
// app once; host picks one of the app's names.
func (s *Server) enterURL(r *http.Request, app, host string) (string, time.Time, int, string) {
	if !s.allowsApp(r, app) {
		return "", time.Time{}, http.StatusForbidden, "app_forbidden"
	}
	if s.opts.AppProxy == nil || s.opts.AppAccess == nil {
		return "", time.Time{}, http.StatusConflict, "proxy_no_route"
	}
	// The pass is bound to this login: it ends with logout and lock. Only
	// a browser session has one (no API tokens, no cluster calls).
	session := auth.SessionID(s.sessionToken(r))
	if session == "" {
		return "", time.Time{}, http.StatusForbidden, "session_required"
	}
	token, expires := s.opts.AppAccess.EnterToken(currentUser(r).Name, app, session)
	link, ok := s.opts.AppProxy.EnterURL(app, host, token)
	if !ok {
		return "", time.Time{}, http.StatusConflict, "proxy_no_route"
	}
	return link, expires, 0, ""
}

// handleAppWindow returns the address that opens an app in a window (or
// a tab): {url, expires}. The token in it holds 60 seconds, once.
func (s *Server) handleAppWindow(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	// ?direct=1: a direct window (PLAN 75-14) on the app's name under the
	// suffix the web UI is reached by; none (an IP address, a tunnel) and
	// the window takes the remote view at once.
	host, direct := "", r.URL.Query().Get("direct") == "1"
	if direct {
		ok := false
		if s.opts.AppProxy != nil {
			host, ok = s.opts.AppProxy.DirectHost(app, r.Host)
		}
		if !ok {
			writeError(w, http.StatusConflict, "direct_unreachable", "the web UI is not reached by a name the app has")
			return
		}
	}
	link, expires, status, code := s.enterURL(r, app, host)
	if status != 0 {
		writeError(w, status, code, appWindowMessage(code))
		return
	}
	if direct {
		link += "&frame=1"
	}
	noteTarget(r, app)
	writeJSON(w, http.StatusOK, map[string]any{"url": link, "expires": expires.UTC().Format(time.RFC3339)})
}

func appWindowMessage(code string) string {
	switch code {
	case "app_forbidden":
		return "this account may not use this app"
	case "session_required":
		return "only a browser logged in to TitanOS can open an app window"
	}
	return "the app has no HTTPS name"
}

// handleAppOpen sends a logged-in browser on to an app (links of the web
// UI and the page the app gate shows); without a login to the web UI.
func (s *Server) handleAppOpen(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticate(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), contextKey{}, user))
	app := r.PathValue("app")
	link, _, status, code := s.enterURL(r, app, r.URL.Query().Get("host"))
	if status != 0 {
		http.Error(w, appWindowMessage(code), status)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, link, http.StatusSeeOther)
}

// handleCAInfo describes the TitanOS authority: {available, name,
// fingerprint, notAfter}. Never its key.
func (s *Server) handleCAInfo(w http.ResponseWriter, r *http.Request) {
	if s.opts.Certificate == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	info, ok := s.opts.Certificate.CAInfo()
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "name": info.Issuer, "fingerprint": info.Fingerprint, "notAfter": info.NotAfter})
}

// handleCADownload hands out the certificate of the TitanOS authority to
// install on a device: ca.crt as PEM, ca.cer as DER.
func (s *Server) handleCADownload(w http.ResponseWriter, r *http.Request) {
	if s.opts.Certificate == nil {
		writeError(w, http.StatusNotFound, "not_found", "no certificate authority")
		return
	}
	der, err := s.opts.Certificate.CACertificate()
	if err != nil {
		slog.Warn("certificate authority not available", "err", err)
		writeError(w, http.StatusServiceUnavailable, "ca_unavailable", "the certificate authority is not available")
		return
	}
	host, _ := os.Hostname()
	host = strings.Map(func(c rune) rune {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' {
			return c
		}
		return -1
	}, host)
	if host == "" {
		host = "nas"
	}
	file := r.PathValue("file")
	body := der
	switch file {
	case "ca.crt":
		body = pemCertificate(der)
	case "ca.cer":
	default:
		writeError(w, http.StatusNotFound, "not_found", "no such file")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/x-x509-ca-cert")
	h.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="titanos-%s-%s"`, host, file))
	h.Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

func pemCertificate(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// PLAN 75-20 (U2): the apps of one account on its page under Benutzer –
// which Docker apps it may use and whether new apps come to it by
// themselves. Owners and administrators may always use every app.
type userAppsView struct {
	Admin   bool          `json:"admin"`
	NewApps bool          `json:"newApps"`
	Apps    []userAppView `json:"apps"`
}

type userAppView struct {
	dockerApp
	Allowed bool `json:"allowed"`
}

func (s *Server) userAppsView(r *http.Request, name string, admin bool) userAppsView {
	settings := appaccess.Settings{Apps: map[string]appaccess.App{}}
	if s.opts.AppAccess != nil {
		settings = s.opts.AppAccess.Settings()
	}
	view := userAppsView{Admin: admin, NewApps: admin || settings.NewAppsFor(name), Apps: []userAppView{}}
	for _, a := range s.dockerApps(r) {
		allowed := admin
		if !admin {
			if entry, ok := settings.Apps[a.App]; ok {
				allowed = slices.Contains(entry.Users, name)
			} else {
				allowed = settings.NewAppsFor(name)
			}
		}
		view.Apps = append(view.Apps, userAppView{dockerApp: a, Allowed: allowed})
	}
	return view
}

func (s *Server) handleUserApps(w http.ResponseWriter, r *http.Request) {
	target, ok := s.opts.Users.Get(r.PathValue("name"))
	if !ok {
		writeError(w, http.StatusNotFound, "user_not_found", "no such account")
		return
	}
	writeJSON(w, http.StatusOK, s.userAppsView(r, target.Name, target.Role.CanAdminister()))
}

// handleUserAppsSet changes the apps of one normal account:
// {newApps: bool, apps: {"immich": true, …}}; apps left out stay as they are.
func (s *Server) handleUserAppsSet(w http.ResponseWriter, r *http.Request) {
	if s.opts.AppAccess == nil {
		writeError(w, http.StatusNotFound, "not_found", "no app rights")
		return
	}
	target, ok := s.opts.Users.Get(r.PathValue("name"))
	if !ok {
		writeError(w, http.StatusNotFound, "user_not_found", "no such account")
		return
	}
	if target.Role.CanAdminister() {
		writeError(w, http.StatusBadRequest, "app_access_admin", "owners and administrators may always use every app")
		return
	}
	var req struct {
		NewApps *bool           `json:"newApps"`
		Apps    map[string]bool `json:"apps"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	var normal []string
	for _, u := range s.opts.Users.List() {
		if !u.Role.CanAdminister() {
			normal = append(normal, u.Name)
		}
	}
	settings := s.opts.AppAccess.Settings()
	// The setting for new apps becomes one per account; the old value
	// carries over to every account first.
	if !settings.PerUser {
		settings.NewAppsUsers = []string{}
		if settings.NewApps == appaccess.NewAppsEveryone {
			settings.NewAppsUsers = slices.Clone(normal)
		}
		settings.PerUser = true
	}
	apps := s.dockerApps(r)
	// Apps without an entry went to whoever gets new apps; they keep that
	// before the setting for new apps changes.
	for _, a := range apps {
		if _, ok := settings.Apps[a.App]; ok {
			continue
		}
		entry := appaccess.App{Users: []string{}}
		for _, u := range normal {
			if settings.NewAppsFor(u) {
				entry.Users = append(entry.Users, u)
			}
		}
		settings.Apps[a.App] = entry
	}
	for _, a := range apps {
		want, given := req.Apps[a.App]
		if !given {
			continue
		}
		entry := settings.Apps[a.App]
		entry.Users = slices.DeleteFunc(entry.Users, func(u string) bool { return u == target.Name })
		if want {
			entry.Users = append(entry.Users, target.Name)
		}
		settings.Apps[a.App] = entry
	}
	if req.NewApps != nil {
		settings.NewAppsUsers = slices.DeleteFunc(settings.NewAppsUsers, func(u string) bool { return u == target.Name })
		if *req.NewApps {
			settings.NewAppsUsers = append(settings.NewAppsUsers, target.Name)
		}
	}
	// What older titand read on the way back: everyone only if every
	// normal account gets new apps.
	settings.NewApps = appaccess.NewAppsAdmins
	if len(normal) > 0 && !slices.ContainsFunc(normal, func(u string) bool { return !slices.Contains(settings.NewAppsUsers, u) }) {
		settings.NewApps = appaccess.NewAppsEveryone
	}
	if err := s.opts.AppAccess.Set(settings); err != nil {
		slog.Error("app rights not saved", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not save the setting")
		return
	}
	noteTarget(r, "apps")
	go s.viewerRecheckRights()
	slog.Info("app rights of an account changed", "account", target.Name, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, s.userAppsView(r, target.Name, false))
}
