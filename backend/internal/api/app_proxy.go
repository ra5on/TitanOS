package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/appproxy"
)

// handleAppProxy lists the HTTPS names of apps with their addresses.
func (s *Server) handleAppProxy(w http.ResponseWriter, r *http.Request) {
	if s.opts.AppProxy == nil {
		writeError(w, http.StatusNotFound, "not_found", "no app proxy")
		return
	}
	domain, suffixes, apps := s.opts.AppProxy.Info()
	writeJSON(w, http.StatusOK, map[string]any{"domain": domain, "suffixes": suffixes, "apps": apps})
}

// handleAppProxyDomain sets the domain of one's own for all apps.
func (s *Server) handleAppProxyDomain(w http.ResponseWriter, r *http.Request) {
	if s.opts.AppProxy == nil {
		writeError(w, http.StatusNotFound, "not_found", "no app proxy")
		return
	}
	var req struct {
		Domain string `json:"domain"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.AppProxy.SetDomain(req.Domain); err != nil {
		writeAppProxyError(w, err)
		return
	}
	noteTarget(r, req.Domain)
	slog.Info("app proxy domain changed", "domain", req.Domain, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleAppProxy(w, r)
}

// handleAppProxyRoute sets the HTTPS name of one app.
func (s *Server) handleAppProxyRoute(w http.ResponseWriter, r *http.Request) {
	if s.opts.AppProxy == nil {
		writeError(w, http.StatusNotFound, "not_found", "no app proxy")
		return
	}
	var route appproxy.Route
	if !readJSON(w, r, &route) {
		return
	}
	app := r.PathValue("app")
	if err := s.opts.AppProxy.SetRoute(app, route); err != nil {
		writeAppProxyError(w, err)
		return
	}
	noteTarget(r, app, route.Name)
	slog.Info("app proxy route changed", "app", app, "name", route.Name, "port", route.Port, "enabled", route.Enabled, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleAppProxy(w, r)
}

// handleAppProxyRemove forgets the HTTPS name of an app.
func (s *Server) handleAppProxyRemove(w http.ResponseWriter, r *http.Request) {
	if s.opts.AppProxy == nil {
		writeError(w, http.StatusNotFound, "not_found", "no app proxy")
		return
	}
	app := r.PathValue("app")
	if err := s.opts.AppProxy.RemoveRoute(app); err != nil {
		writeAppProxyError(w, err)
		return
	}
	noteTarget(r, app)
	slog.Info("app proxy route removed", "app", app, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleAppProxy(w, r)
}

func writeAppProxyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, appproxy.ErrInvalid):
		writeError(w, http.StatusBadRequest, "proxy_invalid", err.Error())
	case errors.Is(err, appproxy.ErrNameTaken):
		writeError(w, http.StatusConflict, "proxy_name_taken", err.Error())
	default:
		slog.Error("app proxy settings not saved", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not save the setting")
	}
}
