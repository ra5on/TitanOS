package api

// PLAN 116, docs/ANDROID-APP.md: pairing of the Android apps and their
// sessions. The browser routes are for the web session of this node only
// (not in publicRoutes, not in the tool catalog, not over the cluster);
// the routes of the app take no login, count wrong codes and signatures
// against the address, and end in an ordinary session of the account.

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/appdevices"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// appsArea is the area of the Android apps.
type appsArea struct {
	apps         *appdevices.Manager
	sessions     sessionsFunc
	cookieName   func() string
	webSession   webSessionFunc
	reconfirm    reconfirmFunc
	limiter      limiterFunc
	record       recordFunc
	addresses    func() []setupAddress
	certificate  func(host string) (*tls.Certificate, error)
	remoteIPFunc func(*http.Request) string
	guard        appSessionGuard
}

func (s *Server) appsArea() appsArea {
	var cert func(string) (*tls.Certificate, error)
	if m := s.core().certificate; m != nil {
		cert = func(host string) (*tls.Certificate, error) {
			return m.GetCertificate(&tls.ClientHelloInfo{ServerName: host})
		}
	}
	return appsArea{apps: s.appDevices, sessions: s.sessionStore, cookieName: s.authArea().cookieName,
		webSession: s.requireWebSession, reconfirm: s.authArea().reconfirm, limiter: s.rateLimiter, record: s.recordActivity,
		addresses: func() []setupAddress { return s.deviceSetup("").Addresses }, certificate: cert, remoteIPFunc: remoteIP,
		guard: s.appSessionGuard()}
}

func (s *Server) registerApps(mux *routeMux) {
	a, g := s.appsArea(), s.guards()
	web := func(h http.HandlerFunc) http.HandlerFunc { return g.user(g.clusterLocal(g.browserOnly(h))) }
	mux.HandleFunc("POST /api/v1/account/apps/pair", web(a.handlePairStart))
	mux.HandleFunc("GET /api/v1/account/apps/pair/{id}", web(a.handlePairStatus))
	mux.HandleFunc("POST /api/v1/account/apps/pair/{id}/confirm", web(a.handlePairConfirm))
	mux.HandleFunc("POST /api/v1/account/apps/pair/{id}/reject", web(a.handlePairReject))
	mux.HandleFunc("DELETE /api/v1/account/apps/pair/{id}", web(a.handlePairCancel))
	mux.HandleFunc("GET /api/v1/account/apps", web(a.handleList))
	mux.HandleFunc("DELETE /api/v1/account/apps/{id}", web(a.handleDelete))
	// The app's own routes: no login; the cluster keeps out (clusterLocal).
	mux.HandleFunc("POST /api/v1/app/pair/sibling", g.user(g.clusterLocal(g.browserOnly(a.guard.appOnly(a.handleSibling)))))
	mux.HandleFunc("POST /api/v1/app/pair/claim", g.clusterLocal(a.handleClaim))
	// The watch (docs/ANDROID-APP.md "Die Uhr"): it starts and reveals
	// without a login, a paired phone app confirms its code.
	mux.HandleFunc("POST /api/v1/app/pair/watch", g.clusterLocal(a.handleWatchStart))
	mux.HandleFunc("POST /api/v1/app/pair/watch/{id}/reveal", g.clusterLocal(a.handleWatchReveal))
	mux.HandleFunc("POST /api/v1/app/pair/watch/{id}/wait", g.clusterLocal(a.handleWatchWait))
	mux.HandleFunc("POST /api/v1/app/pair/watch/confirm", g.user(g.clusterLocal(g.browserOnly(a.guard.appOnly(a.handleWatchConfirm)))))
	mux.HandleFunc("POST /api/v1/app/pair/wait", g.clusterLocal(a.handleWait))
	mux.HandleFunc("POST /api/v1/app/challenge", g.clusterLocal(a.handleChallenge))
	mux.HandleFunc("POST /api/v1/app/session", g.clusterLocal(a.handleSession))
}

// appAddress is an address in the pairing: the server and the SHA-256 of
// the public key of the certificate the NAS serves for it.
type appAddress struct {
	Server      string `json:"server"`
	Kind        string `json:"kind"`
	Certificate string `json:"certificate"`
	SPKI        string `json:"spki"`
}

// spkiOf is the SHA-256 of the public key of a certificate, base64url
// without padding.
func spkiOf(cert *tls.Certificate) string {
	if cert == nil || len(cert.Certificate) == 0 {
		return ""
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (a appsArea) pairAddresses() []appAddress {
	out := []appAddress{}
	for _, s := range a.addresses() {
		host, _, ok := splitServer(s.Server)
		if !ok {
			continue
		}
		addr := appAddress{Server: s.Server, Kind: s.Kind, Certificate: s.Certificate}
		if a.certificate != nil {
			name := host
			if net.ParseIP(host) != nil {
				name = ""
			}
			if cert, err := a.certificate(name); err == nil {
				addr.SPKI = spkiOf(cert)
			}
		}
		out = append(out, addr)
	}
	return out
}

// pairLink is what the QR code holds (docs/ANDROID-APP.md).
func pairLink(code string, addresses []appAddress) string {
	q := url.Values{}
	q.Set("v", "1")
	q.Set("c", code)
	for _, addr := range addresses {
		q.Add("a", addr.Server+"!"+addr.SPKI)
	}
	return "titanos://pair?" + q.Encode()
}

func writeAppsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, appdevices.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "no such app or pairing")
	case errors.Is(err, appdevices.ErrBadState):
		writeError(w, http.StatusConflict, "pairing_state", "the pairing is not waiting for this")
	case errors.Is(err, appdevices.ErrTooMany):
		writeError(w, http.StatusConflict, "app_limit", "at most 20 apps per account")
	case errors.Is(err, appdevices.ErrTooManyOpen):
		writeError(w, http.StatusTooManyRequests, "pairing_limit", "too many open pairings")
	case errors.Is(err, appdevices.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "apps_unavailable", "the paired apps cannot be read now")
	case errors.Is(err, appdevices.ErrInvalidName):
		writeError(w, http.StatusBadRequest, "invalid_device_name", "the device name is empty, too long or has invisible characters")
	case errors.Is(err, appdevices.ErrUnknown):
		writeError(w, http.StatusUnauthorized, "app_unknown", "this app is not paired (any more)")
	case errors.Is(err, appdevices.ErrInvalidApp), errors.Is(err, appdevices.ErrSameApp):
		writeError(w, http.StatusBadRequest, "invalid_app", "unknown app")
	case errors.Is(err, appdevices.ErrInvalidKey):
		writeError(w, http.StatusBadRequest, "invalid_key", "not a P-256 public key")
	default:
		writeError(w, http.StatusInternalServerError, "internal", "could not handle the app")
	}
}

func (a appsArea) note(r *http.Request, user, action, target string, ok bool) {
	a.record(activity.Entry{At: time.Now(), User: user, Via: activity.ViaWeb, IP: a.remoteIPFunc(r), Action: action, Target: target, OK: ok})
}

// ---------- browser ----------

func (a appsArea) handlePairStart(w http.ResponseWriter, r *http.Request) {
	if !a.webSession(w, r) {
		return
	}
	user := currentUser(r)
	p, err := a.apps.Start(user)
	if err != nil {
		writeAppsError(w, err)
		return
	}
	addresses := a.pairAddresses()
	writeJSON(w, http.StatusOK, map[string]any{"id": p.ID, "code": p.Code, "manualCode": p.Manual[:4] + "-" + p.Manual[4:],
		"expires": p.Expires.UTC(), "link": pairLink(p.Code, addresses), "addresses": addresses})
}

func pairingJSON(p appdevices.Pairing) map[string]any {
	out := map[string]any{"state": p.State, "expires": p.Expires.UTC()}
	if p.Name != "" {
		out["name"], out["app"] = p.Name, p.App
	}
	return out
}

func (a appsArea) handlePairStatus(w http.ResponseWriter, r *http.Request) {
	p, err := a.apps.Status(currentUser(r), r.PathValue("id"))
	if err != nil {
		writeAppsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pairingJSON(p))
}

func (a appsArea) handlePairConfirm(w http.ResponseWriter, r *http.Request) {
	if !a.webSession(w, r) {
		return
	}
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	// Only a claimed pairing costs an attempt of the password.
	if p, err := a.apps.Status(user, r.PathValue("id")); err != nil || p.State != appdevices.Claimed {
		if err == nil {
			err = appdevices.ErrBadState
		}
		writeAppsError(w, err)
		return
	}
	// Password and code like a device password or an API token: the app
	// is another way into the whole account.
	if !a.reconfirm(w, user, req.Password, req.Code) {
		return
	}
	d, err := a.apps.Confirm(user, r.PathValue("id"))
	if err != nil {
		writeAppsError(w, err)
		return
	}
	a.note(r, user.Name, "app.pair", d.Name, true)
	writeJSON(w, http.StatusOK, map[string]any{"state": appdevices.Confirmed, "id": d.ID, "name": d.Name, "app": d.App})
}

func (a appsArea) handlePairReject(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if err := a.apps.Reject(user, r.PathValue("id")); err != nil {
		writeAppsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": appdevices.Rejected})
}

func (a appsArea) handlePairCancel(w http.ResponseWriter, r *http.Request) {
	if err := a.apps.Cancel(currentUser(r), r.PathValue("id")); err != nil {
		writeAppsError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type appJSON struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	App     string     `json:"app"`
	Created time.Time  `json:"created"`
	Expires *time.Time `json:"expires,omitempty"`
}

func (a appsArea) handleList(w http.ResponseWriter, r *http.Request) {
	list, err := a.apps.List(currentUser(r))
	if err != nil {
		writeAppsError(w, err)
		return
	}
	out := make([]appJSON, 0, len(list))
	for _, d := range list {
		out = append(out, appJSON{ID: d.ID, Name: d.Name, App: d.App, Created: d.Created, Expires: a.apps.Expires(d)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": out})
}

func (a appsArea) handleDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id := r.PathValue("id")
	name := id
	if list, err := a.apps.List(user); err == nil {
		for _, d := range list {
			if d.ID == id {
				name = d.Name
			}
		}
	}
	if err := a.apps.Delete(user, id); err != nil {
		writeAppsError(w, err)
		return
	}
	a.sessions().DeleteDevice(user.Name, id)
	a.note(r, user.Name, "app.end", name, true)
	w.WriteHeader(http.StatusNoContent)
}

// ---------- app ----------

// appLimited answers 429 while the address has too many failures.
func (a appsArea) appLimited(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := "app-ip:" + a.remoteIPFunc(r)
	if wait := a.limiter().RetryAfter(key); wait > 0 {
		writeRateLimited(w, wait, "too many failed attempts, try again later")
		return key, true
	}
	return key, false
}

func (a appsArea) handleClaim(w http.ResponseWriter, r *http.Request) {
	key, limited := a.appLimited(w, r)
	if limited {
		return
	}
	var req struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		App       string `json:"app"`
		PublicKey string `json:"publicKey"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	p, err := a.apps.Claim(req.Code, strings.TrimSpace(req.Name), req.App, req.PublicKey, a.remoteIPFunc(r))
	switch {
	case errors.Is(err, appdevices.ErrBadCode), errors.Is(err, appdevices.ErrClaimed):
		a.limiter().Fail(key)
		writeError(w, http.StatusNotFound, "pairing_code", "unknown, used or expired code")
		return
	case err != nil:
		writeAppsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": p.State, "expires": p.Expires.UTC()})
}

// handleSibling gives a paired app the code for the other app on the same
// phone (it hands it over through an interface only apps with the same
// signature can use). The claim must come from the same address within
// SiblingTTL; it is confirmed by itself.
func (a appsArea) handleSibling(w http.ResponseWriter, r *http.Request) {
	var req struct {
		App string `json:"app"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	p, err := a.apps.Sibling(user, a.guard.device(r), req.App, a.remoteIPFunc(r))
	if err != nil {
		a.note(r, user.Name, "app.sibling", req.App, false)
		writeAppsError(w, err)
		return
	}
	a.note(r, user.Name, "app.sibling", req.App, true)
	writeJSON(w, http.StatusOK, map[string]any{"code": p.Code, "expires": p.Expires.UTC()})
}

func (a appsArea) handleWait(w http.ResponseWriter, r *http.Request) {
	key, limited := a.appLimited(w, r)
	if limited {
		return
	}
	var req struct {
		Code      string `json:"code"`
		Signature string `json:"signature"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	p, err := a.apps.Wait(req.Code, req.Signature)
	if err != nil {
		a.limiter().Fail(key)
		writeError(w, http.StatusNotFound, "pairing_code", "unknown or expired pairing")
		return
	}
	out := map[string]any{"state": p.State}
	if p.State == appdevices.Confirmed {
		out["device"], out["user"] = p.Device, p.User
	}
	writeJSON(w, http.StatusOK, out)
}

func (a appsArea) handleChallenge(w http.ResponseWriter, r *http.Request) {
	if _, limited := a.appLimited(w, r); limited {
		return
	}
	var req struct {
		User   string `json:"user"`
		Device string `json:"device"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	nonce, err := a.apps.Challenge(req.User, req.Device)
	if err != nil {
		writeAppsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nonce": nonce})
}

func (a appsArea) handleSession(w http.ResponseWriter, r *http.Request) {
	key, limited := a.appLimited(w, r)
	if limited {
		return
	}
	var req struct {
		User      string `json:"user"`
		Device    string `json:"device"`
		Nonce     string `json:"nonce"`
		Signature string `json:"signature"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user, d, err := a.apps.Session(req.User, req.Device, req.Nonce, req.Signature)
	switch {
	case errors.Is(err, appdevices.ErrUnavailable):
		writeAppsError(w, err)
		return
	case err != nil:
		a.limiter().Fail(key)
		writeError(w, http.StatusUnauthorized, "app_unknown", "this app is not paired (any more)")
		return
	}
	token, err := a.sessions().CreateApp(user.Name, d.ID, a.remoteIPFunc(r), appUserAgent(d))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not start a session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cookie": a.cookieName(), "token": token, "user": userOf(user)})
}

// appUserAgent is what the list of sessions keeps for the session of an
// app: "TitanOS-Android/<app>", then the device name. The web UI words it.
func appUserAgent(d appdevices.Device) string {
	return appAgentPrefix + d.App + " " + d.Name
}

// appAgentPrefix starts the user agent of an app's session.
const appAgentPrefix = "TitanOS-Android/"

// appsRevokeAll, appsRebind and appsForget follow the device passwords
// (devicesRevokeAll, devicesRebind, pimForgetUser).
func (s *Server) appsRevokeAll(name string) {
	if s.appDevices != nil {
		s.appDevices.RevokeAll(name)
	}
}

func (s *Server) appsRebind(before auth.User) {
	if s.appDevices != nil {
		s.appDevices.Rebind(before)
	}
}

func (s *Server) appsForget(name string) {
	if s.appDevices != nil {
		s.appDevices.Forget(name)
	}
	// What the photo backup kept goes with the account (logged if not).
	if err := s.opts.AppPhotos.Forget(name); err != nil {
		slog.Warn("photo backup: forget an account", "user", name, "err", err)
	}
}
