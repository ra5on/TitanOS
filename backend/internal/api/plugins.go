package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"

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
	{plugins.ErrDriverPending, http.StatusConflict, "plugin_driver_pending"},
	{plugins.ErrConflict, http.StatusConflict, "plugin_conflict"},
	{plugins.ErrBusy, http.StatusConflict, "plugin_busy"},
	{plugins.ErrDownloading, http.StatusConflict, "plugin_downloading"},
	{plugins.ErrUpdatePending, http.StatusConflict, "plugin_update_pending"},
	{plugins.ErrDownload, http.StatusBadGateway, "plugin_download_failed"},
	{plugins.ErrRelease, http.StatusNotFound, "plugin_release_unavailable"},
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

// handlePluginProgress returns the steps of running and recently finished
// installations, updates and removals. RAM only, never waits for an action.
func (s *Server) handlePluginProgress(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"operations": s.opts.Plugins.Progress()})
}

func (s *Server) handlePluginProgressForget(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Plugins.ForgetProgress(r.PathValue("id")); err != nil {
		writePluginError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePluginOldReleases(w http.ResponseWriter, r *http.Request) {
	list, err := s.opts.Plugins.OldReleases(s.opts.Storage.Mounted())
	if err != nil {
		writePluginError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"releases": list})
}
func (s *Server) handlePluginCleanup(w http.ResponseWriter, r *http.Request) {
	if !s.pluginsEditable(w) {
		return
	}
	var req struct {
		Selection []plugins.CleanupSelection `json:"selection"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Plugins.Cleanup(s.opts.Storage.Mounted(), req.Selection); err != nil {
		writePluginError(w, err)
		return
	}
	s.handlePluginOldReleases(w, r)
}

func (s *Server) handlePluginCatalog(w http.ResponseWriter, r *http.Request) {
	var req struct{ Force bool }
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Plugins.RefreshCatalog(r.Context(), req.Force); err != nil {
		slog.Warn("plugin catalog unavailable", "err", err)
	}
	// The catalog may bring plugins for hardware the NAS has; sysfs only.
	s.opts.Plugins.DetectHardware()
	// Opening or refreshing the page also looks for new files in shares;
	// the status poll in between does not read the volumes.
	s.opts.Plugins.ScanShares(s.opts.Storage.Mounted())
	// An unavailable catalog must not hide installed or locally uploaded plugins.
	s.handlePlugins(w, r)
}

// handlePluginDismiss stops recommending a plugin for the hardware it is
// recommended for now; it never installs or removes anything.
func (s *Server) handlePluginDismiss(w http.ResponseWriter, r *http.Request) {
	noteTarget(r, r.PathValue("name"))
	if err := s.opts.Plugins.Dismiss(r.PathValue("name"), s.opts.Storage.Mounted()); err != nil {
		writePluginError(w, err)
		return
	}
	s.handlePlugins(w, r)
}

// handlePluginRestoreDismissed shows dismissed recommendations again.
func (s *Server) handlePluginRestoreDismissed(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Plugins.RestoreDismissed(); err != nil {
		writePluginError(w, err)
		return
	}
	s.handlePlugins(w, r)
}

func (s *Server) handlePluginInstall(w http.ResponseWriter, r *http.Request) {
	if !s.pluginsEditable(w) {
		return
	}
	var req struct {
		Name, Path, Volume, Confirm string
		Activate                    *bool `json:"activate"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Name)
	if req.Name != "" && req.Path != "" {
		writeError(w, http.StatusBadRequest, "invalid_plugin", "choose a release plugin or a local file")
		return
	}
	activate := req.Activate == nil || *req.Activate
	// The steps are followed in the jobs list: leaving the page or closing the
	// tab does not abort a download or activation half-way.
	ctx := context.WithoutCancel(r.Context())
	var err error
	if req.Name != "" {
		err = s.opts.Plugins.InstallOfficialWithActivation(ctx, req.Name, req.Volume, s.opts.Storage.Mounted(), activate)
	} else {
		err = s.opts.Plugins.InstallWithActivation(ctx, req.Path, req.Volume, req.Confirm, s.opts.Storage.Mounted(), activate)
	}
	if err != nil {
		writePluginError(w, err)
		return
	}
	slog.Info("plugin installed", "name", req.Name, "from", req.Path, "by", currentUser(r).Name, "ip", remoteIP(r))
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

func (s *Server) handlePluginUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.pluginsEditable(w) {
		return
	}
	var req struct{ Path, Confirm string }
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	if err := s.opts.Plugins.Update(context.WithoutCancel(r.Context()), name, req.Path, req.Confirm, s.opts.Storage.Mounted()); err != nil {
		writePluginError(w, err)
		return
	}
	slog.Info("plugin updated", "plugin", name, "from", req.Path, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.opts.Plugins.ScanShares(s.opts.Storage.Mounted())
	s.handlePlugins(w, r)
}

func (s *Server) handlePluginRemove(w http.ResponseWriter, r *http.Request) {
	if !s.pluginsEditable(w) {
		return
	}
	name := r.PathValue("name")
	if err := s.opts.Plugins.Remove(context.WithoutCancel(r.Context()), name, s.opts.Storage.Mounted()); err != nil {
		writePluginError(w, err)
		return
	}
	slog.Info("plugin removed", "plugin", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handlePlugins(w, r)
}

func (s *Server) pluginsEditable(w http.ResponseWriter) bool {
	if s.opts.Tailscale != nil && s.opts.Tailscale.Pending() {
		writeError(w, 409, "tailscale_failed", "Confirm or undo the pending Tailscale settings first")
		return false
	}
	if s.opts.Netbird != nil && s.opts.Netbird.Pending() {
		writeError(w, 409, "netbird_failed", "Confirm or undo the pending NetBird settings first")
		return false
	}
	if s.opts.Update == nil {
		return true
	}
	status := s.opts.Update.Status()
	if status.Pending != nil || status.Progress.Phase == "download" || status.Progress.Phase == "switch" {
		writeError(w, http.StatusConflict, "plugin_update_pending", "Finish or cancel the system update before changing plugins")
		return false
	}
	return true
}

func (s *Server) handleDriverUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action    string `json:"action"`
		SHA256    string `json:"sha256"`
		Automatic bool   `json:"automatic"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	var err error
	switch req.Action {
	case "prepare":
		err = s.opts.Plugins.RequestDriverBuild(s.opts.Storage.Mounted())
	case "check":
		err = s.opts.Plugins.RequestDriverUpdate(req.SHA256, s.opts.Storage.Mounted())
	case "automatic":
		err = s.opts.Plugins.SetAutomaticDrivers(req.Automatic)
	case "select":
		if !s.pluginsEditable(w) {
			return
		}
		err = s.opts.Plugins.SelectDriver(req.SHA256, s.opts.Storage.Mounted())
	default:
		writeError(w, http.StatusBadRequest, "invalid_plugin", "unknown driver action")
		return
	}
	if err != nil {
		writePluginError(w, err)
		return
	}
	slog.Info("NVIDIA driver update action", "action", req.Action, "by", currentUser(r).Name)
	s.handlePlugins(w, r)
}

// linkHost names a link for the activity log: scheme and server only.
func linkHost(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	return ""
}

func (s *Server) handlePluginSources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sources": s.opts.Plugins.Sources()})
}

// handlePluginSourceAdd keeps a list of community plugins the owner entered.
// The address is checked once; nothing is installed.
func (s *Server) handlePluginSourceAdd(w http.ResponseWriter, r *http.Request) {
	var req struct{ URL string }
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, linkHost(req.URL))
	if _, err := s.opts.Plugins.AddSource(r.Context(), req.URL); err != nil {
		writePluginError(w, err)
		return
	}
	s.handlePluginSources(w, r)
}

func (s *Server) handlePluginSourceRemove(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Plugins.RemoveSource(r.PathValue("id")); err != nil {
		writePluginError(w, err)
		return
	}
	s.handlePluginSources(w, r)
}

// handlePluginInspect shows what a link to a <name>.plugin.json offers,
// without downloading the image.
func (s *Server) handlePluginInspect(w http.ResponseWriter, r *http.Request) {
	var req struct{ URL string }
	if !readJSON(w, r, &req) {
		return
	}
	plugin, err := s.opts.Plugins.InspectLink(r.Context(), req.URL)
	if err != nil {
		writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, plugin)
}

// handlePluginExternal installs a community plugin from a link or a source.
// Like a file from a share, an unsigned one needs its name as confirmation.
func (s *Server) handlePluginExternal(w http.ResponseWriter, r *http.Request) {
	if !s.pluginsEditable(w) {
		return
	}
	var req plugins.ExternalRequest
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Name, linkHost(req.URL))
	if err := s.opts.Plugins.InstallExternal(context.WithoutCancel(r.Context()), req, s.opts.Storage.Mounted()); err != nil {
		writePluginError(w, err)
		return
	}
	slog.Info("community plugin installed", "name", req.Name, "from", linkHost(req.URL), "source", req.Source, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handlePlugins(w, r)
}
