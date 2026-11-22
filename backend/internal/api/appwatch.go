package api

// The pairing of the watch (PLAN 116 package G, docs/ANDROID-APP.md "Die
// Uhr", appdevices/watch.go). The watch has no camera and no Google Data
// Layer: it reaches the NAS at an address typed on it, shows six digits,
// and a paired phone app of the account confirms them with its session.

import (
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/appdevices"
)

// watchCertificate decides whether the certificate the watch reports can
// be the NAS's (appdevices.WatchCertificate):
//   - Over titand's own TLS it must be exactly the certificate titand
//     serves for the server name of this connection, public or not.
//   - Without (TLS ends before titand, as with Tailscale Serve) a public
//     certificate counts only for a name titand lists as having a valid
//     public certificate (Let's Encrypt, Tailscale); any other must be one
//     titand serves for one of its addresses.
func (a appsArea) watchCertificate(r *http.Request) appdevices.WatchCertificate {
	if r.TLS != nil && a.certificate != nil {
		if cert, err := a.certificate(r.TLS.ServerName); err == nil {
			if served := spkiOf(cert); served != "" {
				return func(spki string, _ bool) bool { return spki == served }
			}
		}
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	addresses := a.pairAddresses()
	return func(spki string, public bool) bool {
		for _, addr := range addresses {
			name, _, ok := splitServer(addr.Server)
			if public && ok && strings.EqualFold(name, host) && addr.Certificate == certValid {
				return true
			}
			if !public && addr.SPKI != "" && addr.SPKI == spki {
				return true
			}
		}
		if !public && a.certificate != nil {
			if cert, err := a.certificate(""); err == nil && spkiOf(cert) == spki {
				return true
			}
		}
		return false
	}
}

// watchAddress is what the limits of watch pairings count: the address,
// an IPv6 address as its /64.
func watchAddress(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() != nil {
		return ip
	}
	return parsed.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

func (a appsArea) handleWatchStart(w http.ResponseWriter, r *http.Request) {
	if _, limited := a.appLimited(w, r); limited {
		return
	}
	var req struct {
		Name      string `json:"name"`
		PublicKey string `json:"publicKey"`
		Commit    string `json:"commit"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	p, err := a.apps.WatchStart(strings.TrimSpace(req.Name), req.PublicKey, req.Commit, watchAddress(a.remoteIPFunc(r)))
	switch {
	case errors.Is(err, appdevices.ErrBadCode):
		writeError(w, http.StatusBadRequest, "invalid_commit", "the commitment is not a SHA-256 in base64url")
		return
	case errors.Is(err, appdevices.ErrTooManyOpen):
		writeError(w, http.StatusTooManyRequests, "pairing_limit", "too many open pairings")
		return
	case err != nil:
		writeAppsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": p.ID, "nonce": p.Nonce, "expires": p.Expires.UTC()})
}

func (a appsArea) handleWatchReveal(w http.ResponseWriter, r *http.Request) {
	key, limited := a.appLimited(w, r)
	if limited {
		return
	}
	var req struct {
		Secret     string `json:"secret"`
		SPKI       string `json:"spki"`
		PublicCert bool   `json:"publicCert"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	p, err := a.apps.WatchReveal(r.PathValue("id"), req.Secret, req.SPKI, req.PublicCert, a.watchCertificate(r))
	switch {
	case errors.Is(err, appdevices.ErrWatchCertificate):
		a.limiter().Fail(key)
		writeError(w, http.StatusConflict, "watch_certificate", "the watch saw another certificate than this NAS serves")
		return
	case err != nil:
		a.limiter().Fail(key)
		writeError(w, http.StatusNotFound, "pairing_code", "unknown, used or expired pairing")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": p.State, "expires": p.Expires.UTC()})
}

func (a appsArea) handleWatchWait(w http.ResponseWriter, r *http.Request) {
	key, limited := a.appLimited(w, r)
	if limited {
		return
	}
	var req struct {
		Signature string `json:"signature"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	p, err := a.apps.WatchWait(r.PathValue("id"), req.Signature)
	if err != nil {
		a.limiter().Fail(key)
		writeError(w, http.StatusNotFound, "pairing_code", "unknown or expired pairing")
		return
	}
	out := map[string]any{"state": p.State}
	if p.State == appdevices.Confirmed {
		// All addresses with their certificates: the watch knows only the
		// one typed, and reaches the NAS over LTE through the others.
		out["device"], out["user"], out["addresses"] = p.Device, p.User, a.pairAddresses()
	}
	writeJSON(w, http.StatusOK, out)
}

func (a appsArea) handleWatchConfirm(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	key := "app-watch:" + user.Name
	if wait := a.limiter().RetryAfter(key); wait > 0 {
		writeRateLimited(w, wait, "too many wrong codes, try again later")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	d, err := a.apps.WatchConfirm(user, a.guard.device(r), req.Code)
	switch {
	case errors.Is(err, appdevices.ErrBadCode):
		a.limiter().Fail(key)
		a.note(r, user.Name, "app.pair", "watch", false)
		writeError(w, http.StatusNotFound, "pairing_code", "no watch shows this code")
		return
	case errors.Is(err, appdevices.ErrWatchConfirmer):
		writeError(w, http.StatusForbidden, "watch_confirmer", "only a paired phone app confirms a watch")
		return
	case err != nil:
		writeAppsError(w, err)
		return
	}
	a.note(r, user.Name, "app.pair", d.Name, true)
	writeJSON(w, http.StatusOK, map[string]any{"state": appdevices.Confirmed, "id": d.ID, "name": d.Name, "app": d.App})
}
