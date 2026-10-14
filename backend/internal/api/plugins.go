package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/plugins"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

var pluginErrors = []struct {
	err    error
	status int
	code   string
}{
	{plugins.ErrDisabled, http.StatusConflict, "read_only"},
	{plugins.ErrNotLoaded, http.StatusServiceUnavailable, "config_unavailable"},
	{plugins.ErrUnknownVolume, http.StatusConflict, "plugin_volume_unavailable"},
	{plugins.ErrNotFound, http.StatusNotFound, "plugin_not_found"},
	{plugins.ErrExists, http.StatusConflict, "plugin_exists"},
	{plugins.ErrInvalid, http.StatusBadRequest, "invalid_plugin"},
	{plugins.ErrNotConfirmed, http.StatusBadRequest, "not_confirmed"},
	{plugins.ErrBusy, http.StatusConflict, "plugin_busy"},
}

func writePluginError(w http.ResponseWriter, err error) {
	for _, known := range pluginErrors {
		if errors.Is(err, known.err) {
			writeError(w, known.status, known.code, err.Error())
			return
		}
	}
	var command *run.CommandError
	if errors.As(err, &command) {
		writeError(w, http.StatusInternalServerError, "plugin_failed", command.Output)
		return
	}
	slog.Error("plugin action failed", "err", err)
	writeError(w, http.StatusInternalServerError, "plugin_failed", err.Error())
}

func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.opts.Plugins.Status(s.opts.Storage.Mounted()))
}

func (s *Server) handlePluginInstall(w http.ResponseWriter, r *http.Request) {
	if !s.pluginsEditable(w) {
		return
	}
	var req struct{ Path, Volume, Confirm string }
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Plugins.Install(r.Context(), req.Path, req.Volume, req.Confirm, s.opts.Storage.Mounted()); err != nil {
		writePluginError(w, err)
		return
	}
	slog.Info("plugin installed", "from", req.Path, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handlePlugins(w, r)
}

func (s *Server) handlePluginAction(w http.ResponseWriter, r *http.Request) {
	if !s.pluginsEditable(w) {
		return
	}
	name, action := r.PathValue("name"), r.PathValue("action")
	if action != "enable" && action != "disable" {
		writeError(w, http.StatusNotFound, "not_found", "no such action")
		return
	}
	if err := s.opts.Plugins.SetEnabled(r.Context(), name, action == "enable", s.opts.Storage.Mounted()); err != nil {
		writePluginError(w, err)
		return
	}
	slog.Info("plugin "+action+"d", "plugin", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handlePlugins(w, r)
}

func (s *Server) handlePluginRemove(w http.ResponseWriter, r *http.Request) {
	if !s.pluginsEditable(w) {
		return
	}
	name := r.PathValue("name")
	if err := s.opts.Plugins.Remove(r.Context(), name, s.opts.Storage.Mounted()); err != nil {
		writePluginError(w, err)
		return
	}
	slog.Info("plugin removed", "plugin", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handlePlugins(w, r)
}

func (s *Server) pluginsEditable(w http.ResponseWriter) bool {
	if s.opts.Update == nil {
		return true
	}
	status := s.opts.Update.Status()
	if status.Pending != nil || status.Progress.Phase == "download" || status.Progress.Phase == "switch" {
		writeError(w, http.StatusConflict, "plugin_busy", "Finish or cancel the system update before changing plugins")
		return false
	}
	return true
}
