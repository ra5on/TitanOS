package api

import (
	"errors"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/wgserver"
)

// Settings › WireGuard (docs/WIREGUARD.md): the server of the NAS and its
// devices. Owners and administrators only; the configuration of a new
// device, with its private key, is answered once and kept nowhere.

// wireGuardArea is an area with narrow dependencies (PLAN 8a, see fansArea).
type wireGuardArea struct {
	wireGuard *wgserver.Manager
}

func (a wireGuardArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/network/wireguard", admin(a.handleWireGuard))
	mux.HandleFunc("PUT /api/v1/network/wireguard", admin(a.handleWireGuardSet))
	mux.HandleFunc("POST /api/v1/network/wireguard/peers", admin(a.handleWireGuardAdd))
	mux.HandleFunc("POST /api/v1/network/wireguard/peers/remove", admin(a.handleWireGuardRemove))
}

func (a wireGuardArea) handleWireGuard(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.wireGuard.Status())
}

func (a wireGuardArea) handleWireGuardSet(w http.ResponseWriter, r *http.Request) {
	var req wgserver.Settings
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.wireGuard.Set(r.Context(), req); err != nil {
		wireGuardError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.wireGuard.Status())
}

func (a wireGuardArea) handleWireGuardAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Name)
	peer, config, err := a.wireGuard.AddPeer(r.Context(), req.Name)
	if err != nil {
		wireGuardError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"name": peer.Name, "address": peer.Address, "publicKey": peer.PublicKey, "config": config})
}

func (a wireGuardArea) handleWireGuardRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PublicKey string `json:"publicKey"`
		Name      string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Name)
	if err := a.wireGuard.RemovePeer(r.Context(), req.PublicKey); err != nil {
		wireGuardError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.wireGuard.Status())
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
