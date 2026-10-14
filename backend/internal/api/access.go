package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/sshd"
	"github.com/ra5on/TitanOS/backend/internal/tlscert"
)

// SSH access and the certificate of the web UI.

func (s *Server) handleSSH(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.opts.SSH.Status())
}

func (s *Server) handleSSHChange(w http.ResponseWriter, r *http.Request) {
	var req struct{ Enabled bool }
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.SSH.SetEnabled(req.Enabled); err != nil {
		switch {
		case errors.Is(err, sshd.ErrDisabled):
			writeError(w, http.StatusConflict, "read_only", err.Error())
		case errors.Is(err, sshd.ErrNotLoaded):
			writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
		}
		return
	}
	slog.Info("SSH access switched", "enabled", req.Enabled, "by", currentUser(r).Name, "ip", remoteIP(r))
	// What the settings say; the service follows within a moment.
	status := s.opts.SSH.Status()
	status.Enabled = req.Enabled
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) accountKeys(w http.ResponseWriter, user auth.User) {
	writeJSON(w, http.StatusOK, map[string]any{
		"keys": user.DescribeSSHKeys(),
		// Keys only open the door for owners and administrators, and
		// only while SSH is switched on.
		"mayLogin":   user.Role.CanAdminister(),
		"sshEnabled": s.opts.SSH.Status().Enabled,
	})
}

func (s *Server) handleAccountKeys(w http.ResponseWriter, r *http.Request) {
	s.accountKeys(w, currentUser(r))
}

func writeKeyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidKey):
		writeError(w, http.StatusBadRequest, "invalid_ssh_key", "this is not a public SSH key (one line, without options)")
	case errors.Is(err, auth.ErrKeyExists):
		writeError(w, http.StatusConflict, "ssh_key_exists", "this key is already there")
	case errors.Is(err, auth.ErrTooManyKeys):
		writeError(w, http.StatusConflict, "too_many_ssh_keys", "the account holds as many keys as it may")
	case errors.Is(err, auth.ErrKeyNotFound):
		writeError(w, http.StatusNotFound, "ssh_key_not_found", "no such key")
	default:
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the account could not be saved to /config")
	}
}

func (s *Server) handleAccountKeyAdd(w http.ResponseWriter, r *http.Request) {
	var req struct{ Key string }
	if !readJSON(w, r, &req) {
		return
	}
	user, err := s.opts.Users.AddSSHKey(currentUser(r).Name, req.Key)
	if err != nil {
		writeKeyError(w, err)
		return
	}
	slog.Info("SSH key added", "user", user.Name, "ip", remoteIP(r))
	s.opts.SSH.Kick()
	s.accountKeys(w, user)
}

func (s *Server) handleAccountKeyRemove(w http.ResponseWriter, r *http.Request) {
	user, err := s.opts.Users.RemoveSSHKey(currentUser(r).Name, r.PathValue("id"))
	if err != nil {
		writeKeyError(w, err)
		return
	}
	slog.Info("SSH key removed", "user", user.Name, "ip", remoteIP(r))
	s.opts.SSH.Kick()
	s.accountKeys(w, user)
}

// certificate answers for the certificate in use. Without TLS (development
// mode) there is none.
func (s *Server) certificate(w http.ResponseWriter) bool {
	if s.opts.Certificate == nil {
		writeError(w, http.StatusConflict, "read_only", "there is no certificate in development mode")
		return false
	}
	return true
}

func (s *Server) handleCertificate(w http.ResponseWriter, r *http.Request) {
	if s.certificate(w) {
		writeJSON(w, http.StatusOK, s.opts.Certificate.Info())
	}
}

// handleCertificateSet takes an own certificate with its key. The key is
// stored in /config and never handed out again.
func (s *Server) handleCertificateSet(w http.ResponseWriter, r *http.Request) {
	var req struct{ Certificate, Key string }
	if !readJSON(w, r, &req) || !s.certificate(w) {
		return
	}
	if err := s.opts.Certificate.SetCustom([]byte(req.Certificate), []byte(req.Key)); err != nil {
		if errors.Is(err, tlscert.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "invalid_certificate", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the certificate could not be saved to /config")
		return
	}
	slog.Info("own certificate put in place", "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, s.opts.Certificate.Info())
}

func (s *Server) handleCertificateRemove(w http.ResponseWriter, r *http.Request) {
	if !s.certificate(w) {
		return
	}
	if err := s.opts.Certificate.RemoveCustom(); err != nil {
		writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
		return
	}
	slog.Info("own certificate removed", "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, s.opts.Certificate.Info())
}
