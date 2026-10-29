package api

import (
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"

	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/docker"
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

// handleAppTailnet publishes an app in the tailnet with one click
// ("Im Tailnet veröffentlichen"): {port, tailscaleKey}, both optional.
// The page sends the WebUI port of the app's main service; without one,
// the NAS takes the app's WebUI label, else its first TCP port.
func (s *Server) handleAppTailnet(w http.ResponseWriter, r *http.Request) {
	if s.opts.AppProxy == nil {
		writeError(w, http.StatusNotFound, "not_found", "no app proxy")
		return
	}
	var req struct {
		Port         int    `json:"port"`
		TailscaleKey string `json:"tailscaleKey"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	app := r.PathValue("app")
	if req.Port == 0 && s.opts.Docker != nil {
		req.Port = webPort(s.opts.Docker.Status(r.Context()).Containers, app)
	}
	route, err := s.opts.AppProxy.PublishTailnet(app, req.Port, req.TailscaleKey)
	if err != nil {
		writeAppProxyError(w, err)
		return
	}
	if req.TailscaleKey != "" {
		// The key itself is never logged or noted, only that it changed.
		noteTarget(r, "tailscale-key")
	}
	noteTarget(r, app, route.TailscaleName, route.Name)
	slog.Info("app published in the tailnet", "app", app, "name", route.Name, "tailnet", route.TailscaleName, "port", route.Port, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleAppProxy(w, r)
}

// handleAppTailnetRemove takes an app out of the tailnet: its device logs
// out and stops (it stays in the tailnet's admin console until removed
// there).
func (s *Server) handleAppTailnetRemove(w http.ResponseWriter, r *http.Request) {
	if s.opts.AppProxy == nil {
		writeError(w, http.StatusNotFound, "not_found", "no app proxy")
		return
	}
	app := r.PathValue("app")
	if err := s.opts.AppProxy.UnpublishTailnet(app); err != nil {
		writeAppProxyError(w, err)
		return
	}
	noteTarget(r, app)
	slog.Info("app taken out of the tailnet", "app", app, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleAppProxy(w, r)
}

// handleAppTailnetCheck asks the app's ts.net address over the tailnet,
// with its certificate checked; the dialog shows the address once it works.
func (s *Server) handleAppTailnetCheck(w http.ResponseWriter, r *http.Request) {
	if s.opts.AppProxy == nil {
		writeError(w, http.StatusNotFound, "not_found", "no app proxy")
		return
	}
	check, err := s.opts.AppProxy.CheckTailnet(r.Context(), r.PathValue("app"))
	if err != nil {
		writeAppProxyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, check)
}

// webUIPort reads the port of a WebUI label: "8096", "8096/admin" or
// "http://[host]:8096/…".
var webUIPort = regexp.MustCompile(`^(?:(\d{1,5})(?:[/?#].*)?|(?i:https?)://\[host\]:(\d{1,5})(?:[/?#].*)?)$`)

// webPort is the port to publish an app on, like the page suggests it:
// the one of a WebUI label, else the first published TCP port; 0 if none.
func webPort(containers []docker.Container, app string) int {
	for _, c := range containers {
		if m := webUIPort.FindStringSubmatch(c.WebUI); c.App == app && m != nil {
			if port, err := strconv.Atoi(m[1] + m[2]); err == nil && port > 0 && port < 65536 {
				return port
			}
		}
	}
	for _, c := range containers {
		for _, p := range c.Ports {
			if c.App == app && p.Protocol != "udp" && p.Host > 0 {
				return p.Host
			}
		}
	}
	return 0
}

func writeAppProxyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, appproxy.ErrTailnetUnavailable):
		writeError(w, http.StatusConflict, "tailnet_unavailable", err.Error())
	case errors.Is(err, appproxy.ErrTailnetNoKey):
		writeError(w, http.StatusConflict, "tailnet_key_missing", err.Error())
	case errors.Is(err, appproxy.ErrProxyOff):
		writeError(w, http.StatusConflict, "proxy_off", err.Error())
	case errors.Is(err, appproxy.ErrNoPort):
		writeError(w, http.StatusBadRequest, "proxy_no_port", err.Error())
	case errors.Is(err, appproxy.ErrTailnetNotReady):
		writeError(w, http.StatusConflict, "tailnet_not_ready", err.Error())
	case errors.Is(err, appproxy.ErrInvalid), errors.Is(err, appproxy.ErrTailnetKey):
		writeError(w, http.StatusBadRequest, "proxy_invalid", err.Error())
	case errors.Is(err, appproxy.ErrNameTaken):
		writeError(w, http.StatusConflict, "proxy_name_taken", err.Error())
	default:
		slog.Error("app proxy settings not saved", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not save the setting")
	}
}
