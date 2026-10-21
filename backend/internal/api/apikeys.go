package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// API keys for agents: made and removed in the web UI with a login, used
// only at the MCP endpoint. See docs/AGENT-API.md.

func writeAPIKeyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrKeyNotAllowed):
		writeError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, auth.ErrTooManyAPIKeys):
		writeError(w, http.StatusConflict, "too_many_keys", err.Error())
	case errors.Is(err, auth.ErrInvalidKeyName):
		writeError(w, http.StatusBadRequest, "invalid_key_name", err.Error())
	case errors.Is(err, auth.ErrInvalidScope):
		writeError(w, http.StatusBadRequest, "invalid_scope", err.Error())
	case errors.Is(err, auth.ErrInvalidNetwork):
		writeError(w, http.StatusBadRequest, "invalid_key_network", err.Error())
	case errors.Is(err, auth.ErrInvalidExpiry):
		writeError(w, http.StatusBadRequest, "invalid_expiry", err.Error())
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, "key_not_found", "no such key")
	default:
		slog.Error("API key change failed", "err", err)
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the key could not be saved to /config")
	}
}

type apiKeysResponse struct {
	// Enabled is the switch under Settings › Access.
	Enabled bool              `json:"enabled"`
	Keys    []auth.APIKeyInfo `json:"keys"`
	Scopes  []string          `json:"scopes"`
	// Fingerprint is the SHA-256 of the certificate of the web UI, for an
	// agent to pin; empty without TLS.
	Fingerprint string `json:"fingerprint"`
}

func (s *Server) apiKeysOf(user string) apiKeysResponse {
	res := apiKeysResponse{Enabled: s.opts.APIKeys.Enabled(), Keys: s.opts.APIKeys.List(user), Scopes: auth.Scopes}
	if s.opts.Certificate != nil {
		res.Fingerprint = s.opts.Certificate.Info().Fingerprint
	}
	return res
}

func (s *Server) handleAccountAPIKeys(w http.ResponseWriter, r *http.Request) {
	if !s.ensureKeysLoaded() {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "/config is not available")
		return
	}
	writeJSON(w, http.StatusOK, s.apiKeysOf(currentUser(r).Name))
}

func (s *Server) handleAccountAPIKeyCreate(w http.ResponseWriter, r *http.Request) {
	var req auth.NewAPIKey
	if !readJSON(w, r, &req) {
		return
	}
	if !s.ensureKeysLoaded() {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "/config is not available")
		return
	}
	user := currentUser(r)
	token, key, err := s.opts.APIKeys.Create(user.Name, req)
	if err != nil {
		writeAPIKeyError(w, err)
		return
	}
	slog.Info("API key created", "key", key.ID, "keyName", key.Name, "scopes", key.Scopes, "user", user.Name, "ip", remoteIP(r))
	// The key is shown this once; only its hash is kept.
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "key": key, "keys": s.apiKeysOf(user.Name)})
}

func (s *Server) handleAccountAPIKeyRemove(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id := r.PathValue("id")
	if err := s.opts.APIKeys.Remove(user.Name, id); err != nil {
		writeAPIKeyError(w, err)
		return
	}
	slog.Info("API key removed", "key", id, "user", user.Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, s.apiKeysOf(user.Name))
}

// handleUserAPIKeysRemove takes all keys from another account, for an
// owner or administrator who does not trust them any more.
func (s *Server) handleUserAPIKeysRemove(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	target, ok := s.opts.Users.Get(r.PathValue("name"))
	if !ok {
		writeAccountError(w, auth.ErrNotFound)
		return
	}
	if !mayManage(actor, target.Role) {
		writeError(w, http.StatusForbidden, "forbidden", "owners are only managed by owners")
		return
	}
	if err := s.opts.APIKeys.RemoveAll(target.Name); err != nil {
		writeAPIKeyError(w, err)
		return
	}
	slog.Info("all API keys of an account removed", "user", target.Name, "by", actor.Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAPIAccess(w http.ResponseWriter, r *http.Request) {
	if !s.ensureKeysLoaded() {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "/config is not available")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": s.opts.APIKeys.Enabled()})
}

func (s *Server) handleAPIAccessChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !s.ensureKeysLoaded() {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "/config is not available")
		return
	}
	if err := s.opts.APIKeys.SetEnabled(req.Enabled); err != nil {
		slog.Error("API access switch failed", "err", err)
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the setting could not be saved to /config")
		return
	}
	slog.Info("API keys switched", "enabled", req.Enabled, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": req.Enabled})
}
