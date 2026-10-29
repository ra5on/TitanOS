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

func (s *Server) handleFirewall(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.opts.Firewall.Status(remoteIP(r)))
}

// handleFirewallChange puts new settings on trial (switching off applies
// at once). The address of this request must keep reaching the web UI.
func (s *Server) handleFirewallChange(w http.ResponseWriter, r *http.Request) {
	var cfg firewall.Config
	if !readJSON(w, r, &cfg) {
		return
	}
	// The assistant has no address of a web session to keep in; it changes
	// rules only, never the switch, and a person confirms in the web UI.
	var err error
	if _, agent := r.Context().Value(agentIdentity{}).(string); agent {
		err = s.opts.Firewall.ChangeFromAssistant(r.Context(), cfg)
	} else {
		err = s.opts.Firewall.Change(r.Context(), cfg, remoteIP(r))
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
	s.handleFirewall(w, r)
}

// handleFirewallConfirm keeps the change on trial. Only a person in the
// web UI may do it: API tokens and the assistant never reach this route.
func (s *Server) handleFirewallConfirm(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Firewall.Confirm(r.Context()); err != nil {
		writeFirewallError(w, err)
		return
	}
	slog.Info("firewall change confirmed", "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleFirewall(w, r)
}

func (s *Server) handleFirewallRevert(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Firewall.Revert(r.Context()); err != nil {
		writeFirewallError(w, err)
		return
	}
	slog.Info("firewall change given up", "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleFirewall(w, r)
}

func (s *Server) handleFirewallAutoBlock(w http.ResponseWriter, r *http.Request) {
	var req firewall.AutoBlock
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Firewall.SetAutoBlock(r.Context(), req); err != nil {
		writeFirewallError(w, err)
		return
	}
	slog.Info("automatic blocking changed", "enabled", req.Enabled, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleFirewall(w, r)
}

func (s *Server) handleFirewallUnblock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Address string `json:"address"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Firewall.Unblock(r.Context(), req.Address); err != nil {
		writeFirewallError(w, err)
		return
	}
	noteTarget(r, req.Address)
	slog.Info("blocked address released", "address", req.Address, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleFirewall(w, r)
}

func (s *Server) handleFirewallClearRecent(w http.ResponseWriter, r *http.Request) {
	s.opts.Firewall.ClearRecent()
	s.handleFirewall(w, r)
}
