package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/ddns"
	"github.com/ra5on/TitanOS/backend/internal/letsencrypt"
)

// Let's Encrypt and dynamic DNS: settings with provider tokens that never
// come back (only "is set"), status from RAM, "renew now" / "update now".

func (s *Server) letsEncrypt(w http.ResponseWriter) bool {
	if s.opts.LetsEncrypt == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Let's Encrypt is not available")
		return false
	}
	return true
}

func (s *Server) handleLetsEncrypt(w http.ResponseWriter, r *http.Request) {
	if s.letsEncrypt(w) {
		writeJSON(w, http.StatusOK, s.opts.LetsEncrypt.Status())
	}
}

func (s *Server) handleLetsEncryptChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		letsencrypt.Settings
		ClearSecrets []string `json:"clearSecrets"`
	}
	if !readJSON(w, r, &req) || !s.letsEncrypt(w) {
		return
	}
	if err := s.opts.LetsEncrypt.Set(req.Settings, req.ClearSecrets); err != nil {
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
	noteTarget(r, s.opts.LetsEncrypt.Status().Settings.Domains...)
	writeJSON(w, http.StatusOK, s.opts.LetsEncrypt.Status())
}

func (s *Server) handleLetsEncryptRenew(w http.ResponseWriter, r *http.Request) {
	if !s.letsEncrypt(w) {
		return
	}
	if err := s.opts.LetsEncrypt.Renew(); err != nil {
		switch {
		case errors.Is(err, letsencrypt.ErrBusy):
			writeError(w, http.StatusConflict, "letsencrypt_busy", err.Error())
		default:
			writeError(w, http.StatusConflict, "letsencrypt_off", err.Error())
		}
		return
	}
	slog.Info("Let's Encrypt certificate renewal asked for", "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusAccepted, s.opts.LetsEncrypt.Status())
}

func (s *Server) dynamicDNS(w http.ResponseWriter) bool {
	if s.opts.DDNS == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "dynamic DNS is not available")
		return false
	}
	return true
}

func (s *Server) handleDDNS(w http.ResponseWriter, r *http.Request) {
	if s.dynamicDNS(w) {
		writeJSON(w, http.StatusOK, s.opts.DDNS.Status())
	}
}

func (s *Server) handleDDNSChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ddns.Settings
		ClearSecrets []string `json:"clearSecrets"`
	}
	if !readJSON(w, r, &req) || !s.dynamicDNS(w) {
		return
	}
	if err := s.opts.DDNS.Set(req.Settings, req.ClearSecrets); err != nil {
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
	noteTarget(r, s.opts.DDNS.Status().Settings.Hostnames...)
	writeJSON(w, http.StatusOK, s.opts.DDNS.Status())
}

func (s *Server) handleDDNSUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.dynamicDNS(w) {
		return
	}
	if err := s.opts.DDNS.UpdateNow(); err != nil {
		writeError(w, http.StatusConflict, "ddns_off", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, s.opts.DDNS.Status())
}
