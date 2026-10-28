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
	writeJSON(w, http.StatusOK, s.opts.AppProxy.Info())
}

// handleAppProxyDomain sets what holds for all names: the domain of one's
// own, the local name, mDNS and the switch for all names. A field that is
// not sent stays as it is (older pages only send the domain).
func (s *Server) handleAppProxyDomain(w http.ResponseWriter, r *http.Request) {
	if s.opts.AppProxy == nil {
		writeError(w, http.StatusNotFound, "not_found", "no app proxy")
		return
	}
	var req appproxy.General
	if !readJSON(w, r, &req) {
		return
	}
	if req.Domain != nil || req.LocalName != nil || req.MDNS != nil || req.Off != nil {
		if err := s.opts.AppProxy.SetGeneral(req); err != nil {
			writeAppProxyError(w, err)
			return
		}
	}
	if req.TailscaleKey != nil {
		// The key itself is never logged or noted, only that it changed.
		if err := s.opts.AppProxy.SetTailnetKey(*req.TailscaleKey); err != nil {
			writeAppProxyError(w, err)
			return
		}
		noteTarget(r, "tailscale-key")
	}
	if req.Domain != nil {
		noteTarget(r, *req.Domain)
	}
	slog.Info("app proxy settings changed", "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleAppProxy(w, r)
}

// handleAppProxyTarget sets the HTTPS name of a target outside Docker.
func (s *Server) handleAppProxyTarget(w http.ResponseWriter, r *http.Request) {
	if s.opts.AppProxy == nil {
		writeError(w, http.StatusNotFound, "not_found", "no app proxy")
		return
	}
	var route appproxy.Route
	if !readJSON(w, r, &route) {
		return
	}
	id := r.PathValue("id")
	if err := s.opts.AppProxy.SetTarget(id, route); err != nil {
		writeAppProxyError(w, err)
		return
	}
	noteTarget(r, id, route.Name, route.Host)
	slog.Info("app proxy target changed", "target", id, "name", route.Name, "host", route.Host, "port", route.Port, "enabled", route.Enabled, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleAppProxy(w, r)
}

// handleAppProxyTargetRemove forgets a target outside Docker.
func (s *Server) handleAppProxyTargetRemove(w http.ResponseWriter, r *http.Request) {
	if s.opts.AppProxy == nil {
		writeError(w, http.StatusNotFound, "not_found", "no app proxy")
		return
	}
	id := r.PathValue("id")
	if err := s.opts.AppProxy.RemoveTarget(id); err != nil {
		writeAppProxyError(w, err)
		return
	}
	noteTarget(r, id)
	slog.Info("app proxy target removed", "target", id, "by", currentUser(r).Name, "ip", remoteIP(r))
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
	case errors.Is(err, appproxy.ErrInvalid), errors.Is(err, appproxy.ErrTailnetKey):
		writeError(w, http.StatusBadRequest, "proxy_invalid", err.Error())
	case errors.Is(err, appproxy.ErrNameTaken):
		writeError(w, http.StatusConflict, "proxy_name_taken", err.Error())
	default:
		slog.Error("app proxy settings not saved", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not save the setting")
	}
}
