package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/ddns"
	"github.com/ra5on/TitanOS/backend/internal/firewall"
	"github.com/ra5on/TitanOS/backend/internal/letsencrypt"
)

// Let's Encrypt and dynamic DNS: settings with provider tokens that never
// come back (only "is set"), status from RAM, "renew now" / "update now".

// letsEncryptArea is Let's Encrypt; an area with narrow dependencies (PLAN 8a,
// see fansArea). firewall says whether port 80 is reachable from the
// internet; nil leaves that out.
type letsEncryptArea struct {
	acme     *letsencrypt.Manager
	firewall *firewall.Manager
}

func (a letsEncryptArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/system/letsencrypt", admin(a.handleLetsEncrypt))
	mux.HandleFunc("PUT /api/v1/system/letsencrypt", admin(a.handleLetsEncryptChange))
	mux.HandleFunc("POST /api/v1/system/letsencrypt/renew", admin(a.handleLetsEncryptRenew))
}

func (a letsEncryptArea) letsEncrypt(w http.ResponseWriter) bool {
	if a.acme == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Let's Encrypt is not available")
		return false
	}
	return true
}

// letsEncryptStatus adds what the firewall means for HTTP-01: with the
// firewall on, port 80 usually is not reachable from the internet (it
// belongs to the web UI, allowed from home and VPN only). The page then says
// so; TitanOS never opens the port by itself.
func (a letsEncryptArea) letsEncryptStatus() any {
	st := a.acme.Status()
	out := struct {
		letsencrypt.Status
		Firewall     bool `json:"firewall"`
		InternetHTTP bool `json:"internetHttp"`
	}{Status: st, InternetHTTP: st.HTTPPort}
	if a.firewall != nil {
		out.Firewall = a.firewall.Enabled()
		out.InternetHTTP = st.HTTPPort && a.firewall.InternetHTTP()
	}
	return out
}

func (a letsEncryptArea) handleLetsEncrypt(w http.ResponseWriter, r *http.Request) {
	if a.letsEncrypt(w) {
		writeJSON(w, http.StatusOK, a.letsEncryptStatus())
	}
}

func (a letsEncryptArea) handleLetsEncryptChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		letsencrypt.Settings
		ClearSecrets []string `json:"clearSecrets"`
	}
	if !readJSON(w, r, &req) || !a.letsEncrypt(w) {
		return
	}
	if err := a.acme.Set(req.Settings, req.ClearSecrets); err != nil {
		switch {
		case errors.Is(err, letsencrypt.ErrInvalid):
			writeError(w, http.StatusBadRequest, "invalid_letsencrypt", err.Error())
		case errors.Is(err, letsencrypt.ErrNotLoaded):
			writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "config_write_failed", "the settings could not be saved to /config")
		}
		return
	}
	slog.Info("Let's Encrypt settings changed", "by", currentUser(r).Name, "ip", remoteIP(r))
	noteTarget(r, a.acme.Status().Settings.Domains...)
	writeJSON(w, http.StatusOK, a.letsEncryptStatus())
}

func (a letsEncryptArea) handleLetsEncryptRenew(w http.ResponseWriter, r *http.Request) {
	if !a.letsEncrypt(w) {
		return
	}
	if err := a.acme.Renew(); err != nil {
		switch {
		case errors.Is(err, letsencrypt.ErrBusy):
			writeError(w, http.StatusConflict, "letsencrypt_busy", err.Error())
		default:
			writeError(w, http.StatusConflict, "letsencrypt_off", err.Error())
		}
		return
	}
	slog.Info("Let's Encrypt certificate renewal asked for", "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusAccepted, a.letsEncryptStatus())
}

// ddnsArea is dynamic DNS; an area with narrow dependencies (PLAN 8a, see
// fansArea).
type ddnsArea struct {
	ddns *ddns.Manager
}

func (a ddnsArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/network/ddns", admin(a.handleDDNS))
	mux.HandleFunc("PUT /api/v1/network/ddns", admin(a.handleDDNSChange))
	mux.HandleFunc("POST /api/v1/network/ddns/update", admin(a.handleDDNSUpdate))
}

func (a ddnsArea) dynamicDNS(w http.ResponseWriter) bool {
	if a.ddns == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "dynamic DNS is not available")
		return false
	}
	return true
}

// dynamicDNS is the same check for the IPv64 routes, which are not an area
// of their own yet.
func (s *Server) dynamicDNS(w http.ResponseWriter) bool {
	return ddnsArea{ddns: s.opts.DDNS}.dynamicDNS(w)
}

func (a ddnsArea) handleDDNS(w http.ResponseWriter, r *http.Request) {
	if a.dynamicDNS(w) {
		writeJSON(w, http.StatusOK, a.ddns.Status())
	}
}

func (a ddnsArea) handleDDNSChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ddns.Settings
		ClearSecrets []string `json:"clearSecrets"`
	}
	if !readJSON(w, r, &req) || !a.dynamicDNS(w) {
		return
	}
	if err := a.ddns.Set(req.Settings, req.ClearSecrets); err != nil {
		switch {
		case errors.Is(err, ddns.ErrInvalid):
			writeError(w, http.StatusBadRequest, "invalid_ddns", err.Error())
		case errors.Is(err, ddns.ErrNotLoaded):
			writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "config_write_failed", "the settings could not be saved to /config")
		}
		return
	}
	slog.Info("dynamic DNS settings changed", "by", currentUser(r).Name, "ip", remoteIP(r))
	noteTarget(r, a.ddns.Status().Settings.Hostnames...)
	writeJSON(w, http.StatusOK, a.ddns.Status())
}

func (a ddnsArea) handleDDNSUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.dynamicDNS(w) {
		return
	}
	if err := a.ddns.UpdateNow(); err != nil {
		writeError(w, http.StatusConflict, "ddns_off", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, a.ddns.Status())
}
