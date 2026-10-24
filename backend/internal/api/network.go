package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/network"
	"github.com/ra5on/TitanOS/backend/internal/system"
)

func writeNetworkError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, network.ErrBridgePort):
		writeError(w, http.StatusConflict, "network_bridge_port", err.Error())
	case errors.Is(err, network.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_network", err.Error())
	case errors.Is(err, network.ErrDisabled):
		writeError(w, http.StatusConflict, "read_only", err.Error())
	case errors.Is(err, network.ErrNotLoaded):
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
	case errors.Is(err, network.ErrPending), errors.Is(err, network.ErrReverting):
		writeError(w, http.StatusConflict, "network_pending", err.Error())
	case errors.Is(err, network.ErrNoPending):
		writeError(w, http.StatusConflict, "network_not_pending", err.Error())
	default:
		slog.Error("network change failed", "err", err)
		writeError(w, http.StatusInternalServerError, "network_failed", err.Error())
	}
}

func (s *Server) handleNetworkBridge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Port             string `json:"port"`
		OnlyUnconfigured bool   `json:"onlyUnconfigured"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Network.EnsureBridge(r.Context(), req.Port, req.OnlyUnconfigured); err != nil {
		writeNetworkError(w, err)
		return
	}
	slog.Info("network bridge requested", "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleNetwork(w, r)
}

func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"network":   s.opts.Network.Status(r.Context()),
		"bondModes": network.BondModes,
		"timezone":  s.opts.Settings.CurrentTimezone(),
	})
}

// handleNetworkChange puts a new configuration on trial.
func (s *Server) handleNetworkChange(w http.ResponseWriter, r *http.Request) {
	var cfg network.Config
	if !readJSON(w, r, &cfg) {
		return
	}
	if err := s.opts.Network.Change(r.Context(), cfg); err != nil {
		writeNetworkError(w, err)
		return
	}
	slog.Info("network change on trial", "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleNetwork(w, r)
}

// handleNetworkConfirm keeps the change. That this request arrived shows
// that the NAS is still reachable with it.
func (s *Server) handleNetworkConfirm(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Network.Confirm(); err != nil {
		writeNetworkError(w, err)
		return
	}
	slog.Info("network change confirmed", "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleNetwork(w, r)
}

func (s *Server) handleNetworkRevert(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Network.Revert(r.Context()); err != nil {
		writeNetworkError(w, err)
		return
	}
	slog.Info("network change given up", "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleNetwork(w, r)
}

func (s *Server) handleHostname(w http.ResponseWriter, r *http.Request) {
	var req struct{ Hostname string }
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Network.SetHostname(r.Context(), req.Hostname); err != nil {
		writeNetworkError(w, err)
		return
	}
	slog.Info("hostname changed", "hostname", req.Hostname, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTimezone(w http.ResponseWriter, r *http.Request) {
	var req struct{ Timezone string }
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Settings.SetTimezone(req.Timezone); err != nil {
		if errors.Is(err, system.ErrInvalidTimezone) {
			writeError(w, http.StatusBadRequest, "invalid_timezone", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
		return
	}
	slog.Info("time zone changed", "timezone", req.Timezone, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}
