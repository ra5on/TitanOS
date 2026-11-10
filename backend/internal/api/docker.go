package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"

	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/network"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/terminal"
	"github.com/ra5on/TitanOS/backend/internal/viewer"
)

var dockerErrors = []struct {
	err    error
	status int
	code   string
}{
	{docker.ErrNotLoaded, http.StatusServiceUnavailable, "config_unavailable"},
	{docker.ErrUnknownVolume, http.StatusBadRequest, "unknown_volume"},
	{docker.ErrNotRunning, http.StatusConflict, "docker_not_running"},
	{docker.ErrInvalidName, http.StatusBadRequest, "invalid_app_name"},
	{docker.ErrInvalidEnv, http.StatusBadRequest, "invalid_env"},
	{docker.ErrExists, http.StatusConflict, "app_exists"},
	{docker.ErrNotFound, http.StatusNotFound, "docker_not_found"},
	{docker.ErrBusy, http.StatusConflict, "app_busy"},
	{docker.ErrDisabled, http.StatusConflict, "read_only"},
}

func writeDockerError(w http.ResponseWriter, err error) {
	var invalid *docker.InvalidComposeError
	if errors.As(err, &invalid) {
		// What Docker said about the file is the useful part.
		writeError(w, http.StatusBadRequest, "invalid_compose", invalid.Detail)
		return
	}
	if errors.Is(err, docker.ErrInvalid) {
		writeError(w, http.StatusBadRequest, "invalid_compose", "")
		return
	}
	for _, known := range dockerErrors {
		if errors.Is(err, known.err) {
			writeError(w, known.status, known.code, known.err.Error())
			return
		}
	}
	slog.Error("docker action failed", "err", err)
	writeError(w, http.StatusInternalServerError, "docker_failed", err.Error())
}

// dockerArea is Docker: containers, apps, networks, images and volumes; an
// area with narrow dependencies (PLAN 8a, see fansArea). appProxy gives apps
// their HTTPS names and viewer their remote view (both nil: left out),
// network names the connections of the NAS, storage its volumes.
// serveConsole connects the console of a container.
type dockerArea struct {
	docker       *docker.Manager
	appProxy     *appproxy.Manager
	viewer       *viewer.Manager
	network      *network.Manager
	storage      *storage.Manager
	serveConsole consoleFunc
}

// dockerArea is the area of the server.
func (s *Server) dockerArea() dockerArea {
	return dockerArea{docker: s.opts.Docker, appProxy: s.opts.AppProxy, viewer: s.opts.Viewer, network: s.opts.Network,
		storage: s.opts.Storage, serveConsole: s.serveConsole}
}

func (a dockerArea) register(mux *routeMux, g guards) {
	mux.HandleFunc("GET /api/v1/docker", g.admin(a.handleDocker))
	mux.HandleFunc("GET /api/v1/docker/networks", g.admin(a.handleLANNetworks))
	mux.HandleFunc("POST /api/v1/docker/networks", g.heavyAppAdmin(a.handleLANCreate))
	mux.HandleFunc("DELETE /api/v1/docker/networks/{name}", g.appAdmin(a.handleLANRemove))
	// Images, volumes and the build cache (PLAN 67-1, docker_resources.go).
	mux.HandleFunc("GET /api/v1/docker/images", g.admin(a.handleDockerImages))
	mux.HandleFunc("DELETE /api/v1/docker/images/{id}", g.appAdmin(a.handleDockerImageRemove))
	mux.HandleFunc("POST /api/v1/docker/images/prune", g.appAdmin(a.handleDockerImagesPrune))
	mux.HandleFunc("GET /api/v1/docker/volumes", g.admin(a.handleDockerVolumes))
	mux.HandleFunc("DELETE /api/v1/docker/volumes/{name}", g.appAdmin(a.handleDockerVolumeRemove))
	mux.HandleFunc("POST /api/v1/docker/volumes/prune", g.appAdmin(a.handleDockerVolumesPrune))
	mux.HandleFunc("POST /api/v1/docker/builder/prune", g.appAdmin(a.handleDockerBuilderPrune))
	mux.HandleFunc("GET /api/v1/docker/df", g.admin(a.handleDockerDiskUsage))
	// All engine networks; plain bridge networks are created and deleted
	// here, the LAN networks above keep their own routes.
	mux.HandleFunc("GET /api/v1/docker/networks/all", g.admin(a.handleDockerAllNetworks))
	mux.HandleFunc("POST /api/v1/docker/networks/bridge", g.appAdmin(a.handleDockerBridgeCreate))
	mux.HandleFunc("DELETE /api/v1/docker/networks/bridge/{name}", g.appAdmin(a.handleDockerBridgeRemove))
	mux.HandleFunc("POST /api/v1/docker/networks/prune", g.appAdmin(a.handleDockerNetworksPrune))
	mux.HandleFunc("PUT /api/v1/docker/settings", g.appAdmin(a.handleDockerSettings))
	mux.HandleFunc("POST /api/v1/docker/containers/{id}/{action}", g.appAdmin(a.handleContainerAction))
	mux.HandleFunc("DELETE /api/v1/docker/containers/{id}", g.appAdmin(a.handleContainerRemove))
	mux.HandleFunc("GET /api/v1/docker/containers/{id}/logs", g.admin(a.handleContainerLogs))
	mux.HandleFunc("POST /api/v1/docker/apps", g.heavyAppAdmin(a.handleAppCreate))
	mux.HandleFunc("GET /api/v1/docker/apps/{name}", g.admin(a.handleAppCompose))
	mux.HandleFunc("POST /api/v1/docker/apps/{name}/network", g.heavyAppAdmin(a.handleLANAssign))
	mux.HandleFunc("DELETE /api/v1/docker/apps/{name}", g.appAdmin(a.handleAppRemove))
	mux.HandleFunc("PUT /api/v1/docker/apps/{name}", g.appAdmin(a.handleAppSetCompose))
	mux.HandleFunc("POST /api/v1/docker/apps/{name}/update", g.heavyAppAdmin(a.handleAppUpdate))
	mux.HandleFunc("DELETE /api/v1/docker/apps/{name}/job", g.appAdmin(a.handleAppJobDismiss))
	mux.HandleFunc("POST /api/v1/docker/updates/check", g.appAdmin(a.handleUpdatesCheck))
	mux.HandleFunc("GET /api/v1/docker/auto-update", g.admin(a.handleAutoUpdates))
	mux.HandleFunc("PUT /api/v1/docker/auto-update", g.appAdmin(a.handleAutoUpdatesSet))
	mux.HandleFunc("POST /api/v1/docker/updates/apply", g.heavyAppAdmin(a.handleUpdatesApply))
	mux.HandleFunc("GET /api/v1/docker/containers/{id}/console", g.appConsole(a.handleContainerConsole))
}

func (a dockerArea) handleDocker(w http.ResponseWriter, r *http.Request) {
	status := a.docker.Status(r.Context())
	if a.appProxy != nil {
		addProxyURLs(status.Containers, a.appProxy)
	}
	writeJSON(w, http.StatusOK, status)
}

// addProxyURLs gives the HTTPS name of an app to the container that
// publishes the port of its route; to all of the app's containers if none
// does (host network).
func addProxyURLs(containers []docker.Container, routes *appproxy.Manager) {
	publishes := map[string]bool{}
	for _, c := range containers {
		if route, ok := routes.Route(c.App); ok && c.App != "" && slices.ContainsFunc(c.Ports, func(p docker.Port) bool { return p.Host == route.Port }) {
			publishes[c.App] = true
		}
	}
	for i, c := range containers {
		route, ok := routes.Route(c.App)
		if !ok || c.App == "" {
			continue
		}
		if !publishes[c.App] || slices.ContainsFunc(c.Ports, func(p docker.Port) bool { return p.Host == route.Port }) {
			containers[i].ProxyURL = routes.URL(c.App)
			containers[i].TailnetURL = routes.TailnetURL(c.App)
		}
	}
}

func (a dockerArea) handleLANNetworks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"networks": a.docker.LANNetworks(), "parents": a.network.Connections()})
}
func (a dockerArea) handleLANCreate(w http.ResponseWriter, r *http.Request) {
	var n docker.LANNetwork
	if !readJSON(w, r, &n) {
		return
	}
	if err := a.docker.CreateLAN(r.Context(), n); err != nil {
		writeLANError(w, err)
		return
	}
	a.handleLANNetworks(w, r)
}
func (a dockerArea) handleLANRemove(w http.ResponseWriter, r *http.Request) {
	if err := a.docker.RemoveLAN(r.Context(), r.PathValue("name")); err != nil {
		writeLANError(w, err)
		return
	}
	a.handleLANNetworks(w, r)
}

func writeLANError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, docker.ErrInvalid):
		writeError(w, 400, "invalid_lan_network", err.Error())
	case errors.Is(err, docker.ErrExists):
		writeError(w, 409, "lan_exists", err.Error())
	case errors.Is(err, docker.ErrBusy):
		writeError(w, 409, "lan_in_use", err.Error())
	default:
		writeDockerError(w, err)
	}
}

func (a dockerArea) handleLANAssign(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Service string `json:"service"`
		Network string `json:"network"`
		Address string `json:"address"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	var selected *docker.LANNetwork
	for _, n := range a.docker.LANNetworks() {
		if n.Name == req.Network {
			copy := n
			selected = &copy
		}
	}
	if selected == nil {
		writeLANError(w, docker.ErrInvalid)
		return
	}
	text, err := a.docker.Compose(r.PathValue("name"))
	if err == nil {
		text, err = docker.AssignLAN(text, req.Service, *selected, req.Address)
	}
	if err == nil {
		err = a.docker.SetCompose(r.Context(), r.PathValue("name"), text)
	}
	if err != nil {
		writeLANError(w, err)
		return
	}
	w.WriteHeader(202)
}

func (a dockerArea) handleDockerSettings(w http.ResponseWriter, r *http.Request) {
	var settings docker.Settings
	if !readJSON(w, r, &settings) {
		return
	}
	if err := a.docker.SetSettings(settings, a.storage.Names()); err != nil {
		writeDockerError(w, err)
		return
	}
	slog.Info("docker settings changed", "enabled", settings.Enabled, "volume", settings.Volume, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (a dockerArea) handleContainerAction(w http.ResponseWriter, r *http.Request) {
	id, action := r.PathValue("id"), r.PathValue("action")
	if err := a.docker.Action(r.Context(), id, action); err != nil {
		writeDockerError(w, err)
		return
	}
	slog.Info("container "+action, "container", id, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (a dockerArea) handleContainerRemove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.docker.Remove(r.Context(), id); err != nil {
		writeDockerError(w, err)
		return
	}
	slog.Info("container removed", "container", id, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (a dockerArea) handleContainerLogs(w http.ResponseWriter, r *http.Request) {
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	if tail == 0 {
		tail = 300
	}
	logs, err := a.docker.Logs(r.Context(), r.PathValue("id"), tail)
	if err != nil {
		writeDockerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"logs": logs})
}

func (a dockerArea) handleAppCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name, Compose string
		// Env holds secrets; they are stored apart from the compose file.
		Env map[string]string
	}
	// A compose file may be larger than the usual request.
	r.Body = http.MaxBytesReader(w, r.Body, 512*1024)
	if !readJSONBody(w, r, &req) {
		return
	}
	noteTarget(r, req.Name)
	if err := a.docker.CreateApp(r.Context(), req.Name, req.Compose, req.Env); err != nil {
		writeDockerError(w, err)
		return
	}
	slog.Info("app created from a compose file", "app", req.Name, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusAccepted)
}

func (a dockerArea) handleAppCompose(w http.ResponseWriter, r *http.Request) {
	compose, err := a.docker.Compose(r.PathValue("name"))
	if err != nil {
		writeDockerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"compose": compose})
}

func (a dockerArea) handleAppRemove(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	// Deleting the data is asked for by naming the app once more.
	purge := r.URL.Query().Get("deleteData") == name
	if err := a.removeDockerApp(r.Context(), name, purge); err != nil {
		writeDockerError(w, err)
		return
	}
	slog.Info("app removed", "app", name, "dataDeleted", purge, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// removeDockerApp is "remove app": its remote views stop, Compose takes
// the containers down (with purge also its volumes, images and data
// folder), its compose file leaves /config and its HTTPS name is forgotten.
func (a dockerArea) removeDockerApp(ctx context.Context, name string, purge bool) error {
	if a.viewer != nil {
		a.viewer.Stop("", name)
	}
	if err := a.docker.RemoveApp(ctx, name, purge); err != nil {
		return err
	}
	// The HTTPS name goes with the app; a new app of that name starts without.
	if a.appProxy != nil {
		if err := a.appProxy.RemoveRoute(name); err != nil {
			slog.Warn("HTTPS name of a removed app not forgotten", "app", name, "err", err)
		}
	}
	return nil
}

func (a dockerArea) handleAppSetCompose(w http.ResponseWriter, r *http.Request) {
	var req struct{ Compose string }
	r.Body = http.MaxBytesReader(w, r.Body, 512*1024)
	if !readJSONBody(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	if err := a.docker.SetCompose(r.Context(), name, req.Compose); err != nil {
		writeDockerError(w, err)
		return
	}
	slog.Info("app changed", "app", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusAccepted)
}

func (a dockerArea) handleAppUpdate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := a.docker.UpdateApp(r.Context(), name); err != nil {
		writeDockerError(w, err)
		return
	}
	slog.Info("app update started", "app", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusAccepted)
}

// handleAppJobDismiss hides the message of a failed app setup.
func (a dockerArea) handleAppJobDismiss(w http.ResponseWriter, r *http.Request) {
	if err := a.docker.DismissJob(r.PathValue("name")); err != nil {
		writeDockerError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUpdatesCheck asks the registries now instead of waiting for the next round.
func (a dockerArea) handleUpdatesCheck(w http.ResponseWriter, r *http.Request) {
	if err := a.docker.CheckUpdates(r.Context()); err != nil {
		writeDockerError(w, err)
		return
	}
	a.handleDocker(w, r)
}

// handleUpdatesApply updates every app that has a newer image available.
func (a dockerArea) handleUpdatesApply(w http.ResponseWriter, r *http.Request) {
	apps := a.docker.AppsWithUpdates(r.Context())
	// One app that cannot be updated does not hold back the others, and
	// the answer does not read as "nothing happened" while updates run.
	started, failed := []string{}, map[string]string{}
	var first error
	for _, app := range apps {
		if err := a.docker.UpdateApp(r.Context(), app); err != nil && !errors.Is(err, docker.ErrBusy) {
			failed[app] = err.Error()
			if first == nil {
				first = err
			}
			continue
		}
		started = append(started, app)
	}
	if len(started) == 0 && first != nil {
		writeDockerError(w, first)
		return
	}
	slog.Info("update of all apps started", "apps", started, "failed", len(failed), "by", currentUser(r).Name, "ip", remoteIP(r))
	out := map[string]any{"apps": started}
	if len(failed) > 0 {
		out["failed"] = failed
	}
	writeJSON(w, http.StatusAccepted, out)
}

// handleContainerConsole connects a WebSocket to a shell inside a container.
func (a dockerArea) handleContainerConsole(w http.ResponseWriter, r *http.Request) {
	id, shell, user := r.PathValue("id"), r.URL.Query().Get("shell"), r.URL.Query().Get("user")
	// The short form of the ID, as Docker's own tools show it.
	target := "container " + id[:min(len(id), 12)]
	if user != "" {
		target += " as " + user
	}
	a.serveConsole(w, r, target, func(ctx context.Context) (io.ReadWriteCloser, func(cols, rows int), error) {
		return a.docker.Console(ctx, id, shell, user)
	})
}

// terminalArea is the shell on the NAS and whether consoles are allowed; an
// area with narrow dependencies (PLAN 8a, see fansArea). serveConsole
// connects a WebSocket to a shell with the rules of every console.
type terminalArea struct {
	terminal     *terminal.Manager
	serveConsole consoleFunc
}

func (a terminalArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/terminal", admin(a.handleTerminal))
	mux.HandleFunc("PUT /api/v1/terminal", admin(a.handleTerminalSettings))
	mux.HandleFunc("GET /api/v1/terminal/shell", admin(a.handleShell))
}

// handleShell connects a WebSocket to a root shell on the NAS.
func (a terminalArea) handleShell(w http.ResponseWriter, r *http.Request) {
	a.serveConsole(w, r, "nas", func(ctx context.Context) (io.ReadWriteCloser, func(cols, rows int), error) {
		session, err := a.terminal.Shell(ctx)
		if err != nil {
			return nil, nil, err
		}
		return session, session.Resize, nil
	})
}

// serveConsole connects a WebSocket to a shell. Binary messages carry the
// terminal data in both directions; a text message {"cols":80,"rows":24}
// tells the size of the terminal.
//
// Routes require an authenticated administrator. Every console uses the
// existing login without asking for the password again.
func (s *Server) serveConsole(w http.ResponseWriter, r *http.Request, target string, open func(ctx context.Context) (io.ReadWriteCloser, func(cols, rows int), error)) {
	s.serveStream(w, r, consoleOptions{target: target}, open)
}

// handleTerminal tells the web UI whether terminals are allowed.
func (a terminalArea) handleTerminal(w http.ResponseWriter, r *http.Request) {
	idle, _ := a.terminal.IdleMinutes()
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": a.terminal.Enabled(),
		"confirm": a.terminal.ConfirmRequired(),
		// Without a shell on the NAS there are container consoles only.
		"nasShell": !a.terminal.Disabled,
		// Minutes without input until consoles and VM screens end; 0: never.
		"idleMinutes": idle,
	})
}

func (a terminalArea) handleTerminalSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled, Confirm *bool
		IdleMinutes      *int `json:"idleMinutes"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.IdleMinutes != nil {
		if !slices.Contains(terminal.IdleChoices, *req.IdleMinutes) {
			writeError(w, http.StatusBadRequest, "invalid_idle", "idle time must be one of 0, 5, 15, 30, 60, 120 minutes")
			return
		}
		if err := a.terminal.SetIdleMinutes(*req.IdleMinutes); err != nil {
			writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
			return
		}
		slog.Info("console idle time changed", "minutes", *req.IdleMinutes, "by", currentUser(r).Name, "ip", remoteIP(r))
	}
	if req.Enabled != nil {
		if err := a.terminal.SetEnabled(*req.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
			return
		}
		slog.Info("terminals switched", "enabled", *req.Enabled, "by", currentUser(r).Name, "ip", remoteIP(r))
	}
	if req.Confirm != nil {
		if err := a.terminal.SetConfirmRequired(*req.Confirm); err != nil {
			writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
			return
		}
		slog.Info("password prompt for consoles switched", "required", *req.Confirm, "by", currentUser(r).Name, "ip", remoteIP(r))
	}
	a.handleTerminal(w, r)
}

// handleConfirm marks the session as confirmed after the password was
// entered again. Consoles ask for this.
func (au authArea) handleConfirm(w http.ResponseWriter, r *http.Request) {
	var req struct{ Password, Code string }
	if !readJSON(w, r, &req) {
		return
	}
	// With two-factor login the confirmation takes both factors as well.
	// reconfirm also holds the limit against guesses sent at the same time.
	if !au.reconfirm(w, currentUser(r), req.Password, req.Code) {
		return
	}
	if cookie, err := r.Cookie(au.cookieName()); err == nil {
		au.sessions().Confirm(cookie.Value)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (au authArea) handleConfirmed(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"confirmed": au.confirmed(r)})
}

// confirmed reports whether the password was entered again recently. When
// the owner switched the prompt off in the settings, it always counts.
func (au authArea) confirmed(r *http.Request) bool {
	if !au.terminal.ConfirmRequired() {
		return true
	}
	cookie, err := r.Cookie(au.cookieName())
	return err == nil && au.sessions().Confirmed(cookie.Value)
}

// handleAutoUpdates shows the automatic app updates.
func (a dockerArea) handleAutoUpdates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.docker.AutoUpdates())
}

func (a dockerArea) handleAutoUpdatesSet(w http.ResponseWriter, r *http.Request) {
	var req docker.AutoUpdateSettings
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.docker.SetAutoUpdates(req); err != nil {
		if errors.Is(err, docker.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "invalid_auto_update", err.Error())
			return
		}
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a.docker.AutoUpdates())
}
