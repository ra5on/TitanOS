package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/firewall"
)

// The packet filter: zones, services, Docker apps, own rules, the
// automatic blocking and what was dropped lately. See package firewall.

func writeFirewallError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, firewall.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_firewall", err.Error())
	case errors.Is(err, firewall.ErrExcludesYou):
		writeError(w, http.StatusConflict, "firewall_excludes_you", err.Error())
	case errors.Is(err, firewall.ErrPending):
		writeError(w, http.StatusConflict, "firewall_pending", err.Error())
	case errors.Is(err, firewall.ErrNoPending):
		writeError(w, http.StatusConflict, "firewall_not_pending", err.Error())
	case errors.Is(err, firewall.ErrSwitchManual):
		writeError(w, http.StatusForbidden, "firewall_switch_manual", err.Error())
	case errors.Is(err, firewall.ErrClosesWebUI):
		writeError(w, http.StatusConflict, "firewall_closes_webui", err.Error())
	case errors.Is(err, firewall.ErrNotBlocked):
		writeError(w, http.StatusNotFound, "firewall_not_blocked", err.Error())
	case errors.Is(err, firewall.ErrDisabled):
		writeError(w, http.StatusConflict, "read_only", err.Error())
	case errors.Is(err, firewall.ErrNotLoaded):
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
	case errors.Is(err, firewall.ErrUnavailable):
		slog.Error("firewall rules not loaded", "err", err)
		writeError(w, http.StatusInternalServerError, "firewall_failed", err.Error())
	default:
		slog.Error("firewall change failed", "err", err)
		writeError(w, http.StatusInternalServerError, "firewall_failed", err.Error())
	}
}

// firewallArea is the packet filter; an area with narrow dependencies
// (PLAN 8a, see fansArea).
type firewallArea struct {
	firewall *firewall.Manager
}

func (a firewallArea) register(mux *routeMux, admin, clusterLocal func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/firewall", admin(a.handleFirewall))
	mux.HandleFunc("PUT /api/v1/firewall", admin(clusterLocal(a.handleFirewallChange)))
	mux.HandleFunc("POST /api/v1/firewall/confirm", admin(clusterLocal(a.handleFirewallConfirm)))
	mux.HandleFunc("POST /api/v1/firewall/revert", admin(a.handleFirewallRevert))
	mux.HandleFunc("PUT /api/v1/firewall/autoblock", admin(a.handleFirewallAutoBlock))
	mux.HandleFunc("POST /api/v1/firewall/unblock", admin(a.handleFirewallUnblock))
	mux.HandleFunc("DELETE /api/v1/firewall/recent", admin(a.handleFirewallClearRecent))
}

func (a firewallArea) handleFirewall(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.firewall.Status(remoteIP(r)))
}

// handleFirewallChange puts new settings on trial (switching off applies
// at once). The address of this request must keep reaching the web UI.
func (a firewallArea) handleFirewallChange(w http.ResponseWriter, r *http.Request) {
	var cfg firewall.Config
	if !readJSON(w, r, &cfg) {
		return
	}
	// An MCP client has no address of a web session to keep in; it changes
	// rules only, never the switch, and a person confirms in the web UI.
	var err error
	if _, agent := r.Context().Value(inProcessIdentity{}).(string); agent {
		err = a.firewall.ChangeInProcess(r.Context(), cfg)
	} else {
		err = a.firewall.Change(r.Context(), cfg, remoteIP(r))
	}
	if err != nil {
		writeFirewallError(w, err)
		return
	}
	if cfg.Enabled {
		noteTarget(r, "on")
	} else {
		noteTarget(r, "off")
	}
	slog.Info("firewall settings changed", "enabled", cfg.Enabled, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleFirewall(w, r)
}

// handleFirewallConfirm keeps the change on trial. Only a person in the
// web UI may do it: API tokens and MCP never reach this route.
func (a firewallArea) handleFirewallConfirm(w http.ResponseWriter, r *http.Request) {
	if err := a.firewall.Confirm(r.Context()); err != nil {
		writeFirewallError(w, err)
		return
	}
	slog.Info("firewall change confirmed", "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleFirewall(w, r)
}

func (a firewallArea) handleFirewallRevert(w http.ResponseWriter, r *http.Request) {
	if err := a.firewall.Revert(r.Context()); err != nil {
		writeFirewallError(w, err)
		return
	}
	slog.Info("firewall change given up", "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleFirewall(w, r)
}

func (a firewallArea) handleFirewallAutoBlock(w http.ResponseWriter, r *http.Request) {
	var req firewall.AutoBlock
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.firewall.SetAutoBlock(r.Context(), req); err != nil {
		writeFirewallError(w, err)
		return
	}
	slog.Info("automatic blocking changed", "enabled", req.Enabled, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleFirewall(w, r)
}

func (a firewallArea) handleFirewallUnblock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Address string `json:"address"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.firewall.Unblock(r.Context(), req.Address); err != nil {
		writeFirewallError(w, err)
		return
	}
	noteTarget(r, req.Address)
	slog.Info("blocked address released", "address", req.Address, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleFirewall(w, r)
}

func (a firewallArea) handleFirewallClearRecent(w http.ResponseWriter, r *http.Request) {
	a.firewall.ClearRecent()
	a.handleFirewall(w, r)
}
