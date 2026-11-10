package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/ra5on/TitanOS/backend/internal/netbird"
	"github.com/ra5on/TitanOS/backend/internal/plugins"
	"github.com/ra5on/TitanOS/backend/internal/run"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/tailscale"
	"github.com/ra5on/TitanOS/backend/internal/update"
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

// pluginsArea is the plugins; an area with narrow dependencies (PLAN 8a,
// see fansArea). storage names the mounted volumes; update, tailscale and
// netbird say whether a pending change forbids changing plugins.
type pluginsArea struct {
	plugins   *plugins.Manager
	storage   *storage.Manager
	update    *update.Manager
	tailscale *tailscale.Manager
	netbird   *netbird.Manager
}

func (a pluginsArea) register(mux *routeMux, admin, heavyAdmin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/plugins", admin(a.handlePlugins))
	mux.HandleFunc("GET /api/v1/plugins/progress", admin(a.handlePluginProgress))
	mux.HandleFunc("DELETE /api/v1/plugins/progress/{id}", admin(a.handlePluginProgressForget))
	mux.HandleFunc("GET /api/v1/plugins/releases", admin(a.handlePluginOldReleases))
	mux.HandleFunc("POST /api/v1/plugins/cleanup", admin(a.handlePluginCleanup))
	mux.HandleFunc("POST /api/v1/plugins/nvidia/updates", admin(a.handleDriverUpdate))
	mux.HandleFunc("POST /api/v1/plugins/catalog", admin(a.handlePluginCatalog))
	mux.HandleFunc("POST /api/v1/plugins/recommendations/{name}/dismiss", admin(a.handlePluginDismiss))
	mux.HandleFunc("DELETE /api/v1/plugins/recommendations/dismissed", admin(a.handlePluginRestoreDismissed))
	mux.HandleFunc("GET /api/v1/plugins/sources", admin(a.handlePluginSources))
	mux.HandleFunc("POST /api/v1/plugins/sources", admin(a.handlePluginSourceAdd))
	mux.HandleFunc("DELETE /api/v1/plugins/sources/{id}", admin(a.handlePluginSourceRemove))
	mux.HandleFunc("POST /api/v1/plugins/external/inspect", admin(a.handlePluginInspect))
	mux.HandleFunc("POST /api/v1/plugins/external", heavyAdmin(a.handlePluginExternal))
	mux.HandleFunc("POST /api/v1/plugins", heavyAdmin(a.handlePluginInstall))
	mux.HandleFunc("POST /api/v1/plugins/{name}/update", heavyAdmin(a.handlePluginUpdate))
	mux.HandleFunc("POST /api/v1/plugins/{name}/{action}", admin(a.handlePluginAction))
	mux.HandleFunc("DELETE /api/v1/plugins/{name}", admin(a.handlePluginRemove))
}

func (a pluginsArea) handlePlugins(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.plugins.Status(a.storage.Mounted()))
}

// handlePluginProgress returns the steps of running and recently finished
// installations, updates and removals. RAM only, never waits for an action.
func (a pluginsArea) handlePluginProgress(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"operations": a.plugins.Progress()})
}

func (a pluginsArea) handlePluginProgressForget(w http.ResponseWriter, r *http.Request) {
	if err := a.plugins.ForgetProgress(r.PathValue("id")); err != nil {
		writePluginError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a pluginsArea) handlePluginOldReleases(w http.ResponseWriter, r *http.Request) {
	list, err := a.plugins.OldReleases(a.storage.Mounted())
	if err != nil {
		writePluginError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"releases": list})
}
func (a pluginsArea) handlePluginCleanup(w http.ResponseWriter, r *http.Request) {
	if !a.pluginsEditable(w) {
		return
	}
	var req struct {
		Selection []plugins.CleanupSelection `json:"selection"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.plugins.Cleanup(a.storage.Mounted(), req.Selection); err != nil {
		writePluginError(w, err)
		return
	}
	a.handlePluginOldReleases(w, r)
}

func (a pluginsArea) handlePluginCatalog(w http.ResponseWriter, r *http.Request) {
	var req struct{ Force bool }
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.plugins.RefreshCatalog(r.Context(), req.Force); err != nil {
		slog.Warn("plugin catalog unavailable", "err", err)
	}
	// The catalog may bring plugins for hardware the NAS has; sysfs only.
	a.plugins.DetectHardware()
	// Opening or refreshing the page also looks for new files in shares;
	// the status poll in between does not read the volumes.
	a.plugins.ScanShares(a.storage.Mounted())
	// An unavailable catalog must not hide installed or locally uploaded plugins.
	a.handlePlugins(w, r)
}

// handlePluginDismiss stops recommending a plugin for the hardware it is
// recommended for now; it never installs or removes anything.
func (a pluginsArea) handlePluginDismiss(w http.ResponseWriter, r *http.Request) {
	noteTarget(r, r.PathValue("name"))
	if err := a.plugins.Dismiss(r.PathValue("name"), a.storage.Mounted()); err != nil {
		writePluginError(w, err)
		return
	}
	a.handlePlugins(w, r)
}

// handlePluginRestoreDismissed shows dismissed recommendations again.
func (a pluginsArea) handlePluginRestoreDismissed(w http.ResponseWriter, r *http.Request) {
	if err := a.plugins.RestoreDismissed(); err != nil {
		writePluginError(w, err)
		return
	}
	a.handlePlugins(w, r)
}

func (a pluginsArea) handlePluginInstall(w http.ResponseWriter, r *http.Request) {
	if !a.pluginsEditable(w) {
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
		err = a.plugins.InstallOfficialWithActivation(ctx, req.Name, req.Volume, a.storage.Mounted(), activate)
	} else {
		err = a.plugins.InstallWithActivation(ctx, req.Path, req.Volume, req.Confirm, a.storage.Mounted(), activate)
	}
	if err != nil {
		writePluginError(w, err)
		return
	}
	slog.Info("plugin installed", "name", req.Name, "from", req.Path, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handlePlugins(w, r)
}

func (a pluginsArea) handlePluginAction(w http.ResponseWriter, r *http.Request) {
	if !a.pluginsEditable(w) {
		return
	}
	name, action := r.PathValue("name"), r.PathValue("action")
	if action != "enable" && action != "disable" {
		writeError(w, http.StatusNotFound, "not_found", "no such action")
		return
	}
	if err := a.plugins.SetEnabled(r.Context(), name, action == "enable", a.storage.Mounted()); err != nil {
		writePluginError(w, err)
		return
	}
	slog.Info("plugin "+action+"d", "plugin", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handlePlugins(w, r)
}

func (a pluginsArea) handlePluginUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.pluginsEditable(w) {
		return
	}
	var req struct{ Path, Confirm string }
	if !readJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	if err := a.plugins.Update(context.WithoutCancel(r.Context()), name, req.Path, req.Confirm, a.storage.Mounted()); err != nil {
		writePluginError(w, err)
		return
	}
	slog.Info("plugin updated", "plugin", name, "from", req.Path, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.plugins.ScanShares(a.storage.Mounted())
	a.handlePlugins(w, r)
}

func (a pluginsArea) handlePluginRemove(w http.ResponseWriter, r *http.Request) {
	if !a.pluginsEditable(w) {
		return
	}
	name := r.PathValue("name")
	if err := a.plugins.Remove(context.WithoutCancel(r.Context()), name, a.storage.Mounted()); err != nil {
		writePluginError(w, err)
		return
	}
	slog.Info("plugin removed", "plugin", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handlePlugins(w, r)
}

func (a pluginsArea) pluginsEditable(w http.ResponseWriter) bool {
	if a.tailscale != nil && a.tailscale.Pending() {
		writeError(w, 409, "tailscale_failed", "Confirm or undo the pending Tailscale settings first")
		return false
	}
	if a.netbird != nil && a.netbird.Pending() {
		writeError(w, 409, "netbird_failed", "Confirm or undo the pending NetBird settings first")
		return false
	}
	if a.update == nil {
		return true
	}
	status := a.update.Status()
	if status.Pending != nil || status.Progress.Phase == "download" || status.Progress.Phase == "switch" {
		writeError(w, http.StatusConflict, "plugin_update_pending", "Finish or cancel the system update before changing plugins")
		return false
	}
	return true
}

func (a pluginsArea) handleDriverUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action    string `json:"action"`
		SHA256    string `json:"sha256"`
		Automatic bool   `json:"automatic"`
		// Version is the NVIDIA version to choose, "" the shipped one.
		Version string `json:"version"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	var err error
	switch req.Action {
	case "prepare":
		err = a.plugins.RequestDriverBuild(a.storage.Mounted())
	case "check":
		err = a.plugins.RequestDriverUpdate(req.SHA256, a.storage.Mounted())
	case "automatic":
		err = a.plugins.SetAutomaticDrivers(req.Automatic)
	case "select":
		if !a.pluginsEditable(w) {
			return
		}
		err = a.plugins.SelectDriver(req.SHA256, a.storage.Mounted())
	case "choose":
		if !a.pluginsEditable(w) {
			return
		}
		err = a.plugins.ChooseDriver(req.Version, a.storage.Mounted())
	default:
		writeError(w, http.StatusBadRequest, "invalid_plugin", "unknown driver action")
		return
	}
	if err != nil {
		writePluginError(w, err)
		return
	}
	slog.Info("NVIDIA driver update action", "action", req.Action, "version", req.Version, "by", currentUser(r).Name)
	a.handlePlugins(w, r)
}

// linkHost names a link for the activity log: scheme and server only.
func linkHost(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	return ""
}

func (a pluginsArea) handlePluginSources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sources": a.plugins.Sources()})
}

// handlePluginSourceAdd keeps a list of community plugins the owner entered.
// The address is checked once; nothing is installed.
func (a pluginsArea) handlePluginSourceAdd(w http.ResponseWriter, r *http.Request) {
	var req struct{ URL string }
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, linkHost(req.URL))
	if _, err := a.plugins.AddSource(r.Context(), req.URL); err != nil {
		writePluginError(w, err)
		return
	}
	a.handlePluginSources(w, r)
}

func (a pluginsArea) handlePluginSourceRemove(w http.ResponseWriter, r *http.Request) {
	if err := a.plugins.RemoveSource(r.PathValue("id")); err != nil {
		writePluginError(w, err)
		return
	}
	a.handlePluginSources(w, r)
}

// handlePluginInspect shows what a link to a <name>.plugin.json offers,
// without downloading the image.
func (a pluginsArea) handlePluginInspect(w http.ResponseWriter, r *http.Request) {
	var req struct{ URL string }
	if !readJSON(w, r, &req) {
		return
	}
	plugin, err := a.plugins.InspectLink(r.Context(), req.URL)
	if err != nil {
		writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, plugin)
}

// handlePluginExternal installs a community plugin from a link or a source.
// Like a file from a share, an unsigned one needs its name as confirmation.
func (a pluginsArea) handlePluginExternal(w http.ResponseWriter, r *http.Request) {
	if !a.pluginsEditable(w) {
		return
	}
	var req plugins.ExternalRequest
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Name, linkHost(req.URL))
	if err := a.plugins.InstallExternal(context.WithoutCancel(r.Context()), req, a.storage.Mounted()); err != nil {
		writePluginError(w, err)
		return
	}
	slog.Info("community plugin installed", "name", req.Name, "from", linkHost(req.URL), "source", req.Source, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handlePlugins(w, r)
}
