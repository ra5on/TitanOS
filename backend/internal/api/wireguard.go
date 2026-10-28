package api

import (
	"errors"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/wgserver"
)

// Settings › WireGuard (docs/WIREGUARD.md): the server of the NAS and its
// devices. Owners and administrators only; the configuration of a new
// device, with its private key, is answered once and kept nowhere.

func (s *Server) handleWireGuard(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.opts.WireGuard.Status())
}

func (s *Server) handleWireGuardSet(w http.ResponseWriter, r *http.Request) {
	var req wgserver.Settings
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.WireGuard.Set(r.Context(), req); err != nil {
		wireGuardError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.opts.WireGuard.Status())
}

func (s *Server) handleWireGuardAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Name)
	peer, config, err := s.opts.WireGuard.AddPeer(r.Context(), req.Name)
	if err != nil {
		wireGuardError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"name": peer.Name, "address": peer.Address, "publicKey": peer.PublicKey, "config": config})
}

func (s *Server) handleWireGuardRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PublicKey string `json:"publicKey"`
		Name      string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Name)
	if err := s.opts.WireGuard.RemovePeer(r.Context(), req.PublicKey); err != nil {
		wireGuardError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.opts.WireGuard.Status())
}

func wireGuardError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, wgserver.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_wireguard", err.Error())
	case errors.Is(err, wgserver.ErrNameUsed):
		writeError(w, http.StatusConflict, "wireguard_name_used", err.Error())
	case errors.Is(err, wgserver.ErrTooMany):
		writeError(w, http.StatusConflict, "wireguard_too_many", err.Error())
	case errors.Is(err, wgserver.ErrNotFound):
		writeError(w, http.StatusNotFound, "wireguard_not_found", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "wireguard_failed", err.Error())
	}
}
