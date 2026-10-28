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

func (s *Server) handleDocker(w http.ResponseWriter, r *http.Request) {
	status := s.opts.Docker.Status(r.Context())
	if s.opts.AppProxy != nil {
		addProxyURLs(status.Containers, s.opts.AppProxy)
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
		}
	}
}

func (s *Server) handleLANNetworks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"networks": s.opts.Docker.LANNetworks(), "parents": s.opts.Network.Connections()})
}
func (s *Server) handleLANCreate(w http.ResponseWriter, r *http.Request) {
	var n docker.LANNetwork
	if !readJSON(w, r, &n) {
		return
	}
	if err := s.opts.Docker.CreateLAN(r.Context(), n); err != nil {
		writeLANError(w, err)
		return
	}
	s.handleLANNetworks(w, r)
}
func (s *Server) handleLANRemove(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Docker.RemoveLAN(r.Context(), r.PathValue("name")); err != nil {
		writeLANError(w, err)
		return
	}
	s.handleLANNetworks(w, r)
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

func (s *Server) handleLANAssign(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Service string `json:"service"`
		Network string `json:"network"`
		Address string `json:"address"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	var selected *docker.LANNetwork
	for _, n := range s.opts.Docker.LANNetworks() {
		if n.Name == req.Network {
			copy := n
			selected = &copy
		}
	}
	if selected == nil {
		writeLANError(w, docker.ErrInvalid)
		return
	}
	text, err := s.opts.Docker.Compose(r.PathValue("name"))
	if err == nil {
		text, err = docker.AssignLAN(text, req.Service, *selected, req.Address)
	}
	if err == nil {
		err = s.opts.Docker.SetCompose(r.Context(), r.PathValue("name"), text)
	}
	if err != nil {
		writeLANError(w, err)
		return
	}
	w.WriteHeader(202)
}

func (s *Server) handleDockerSettings(w http.ResponseWriter, r *http.Request) {
	var settings docker.Settings
	if !readJSON(w, r, &settings) {
		return
	}
	if err := s.opts.Docker.SetSettings(settings, s.opts.Storage.Names()); err != nil {
		writeDockerError(w, err)
		return
	}
	slog.Info("docker settings changed", "enabled", settings.Enabled, "volume", settings.Volume, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleContainerAction(w http.ResponseWriter, r *http.Request) {
	id, action := r.PathValue("id"), r.PathValue("action")
	if err := s.opts.Docker.Action(r.Context(), id, action); err != nil {
		writeDockerError(w, err)
		return
	}
	slog.Info("container "+action, "container", id, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleContainerRemove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.opts.Docker.Remove(r.Context(), id); err != nil {
		writeDockerError(w, err)
		return
	}
	slog.Info("container removed", "container", id, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleContainerLogs(w http.ResponseWriter, r *http.Request) {
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	if tail == 0 {
		tail = 300
	}
	logs, err := s.opts.Docker.Logs(r.Context(), r.PathValue("id"), tail)
	if err != nil {
		writeDockerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"logs": logs})
}

func (s *Server) handleAppCreate(w http.ResponseWriter, r *http.Request) {
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
	if err := s.opts.Docker.CreateApp(r.Context(), req.Name, req.Compose, req.Env); err != nil {
		writeDockerError(w, err)
		return
	}
	slog.Info("app created from a compose file", "app", req.Name, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleAppCompose(w http.ResponseWriter, r *http.Request) {
	compose, err := s.opts.Docker.Compose(r.PathValue("name"))
	if err != nil {
		writeDockerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"compose": compose})
}

func (s *Server) handleAppRemove(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	// Deleting the data is asked for by naming the app once more.
	purge := r.URL.Query().Get("deleteData") == name
	if err := s.opts.Docker.RemoveApp(r.Context(), name, purge); err != nil {
		writeDockerError(w, err)
		return
	}
	// The HTTPS name goes with the app; a new app of that name starts without.
	if s.opts.AppProxy != nil {
		if err := s.opts.AppProxy.RemoveRoute(name); err != nil {
			slog.Warn("HTTPS name of a removed app not forgotten", "app", name, "err", err)
		}
	}
	slog.Info("app removed", "app", name, "dataDeleted", purge, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAppSetCompose(w http.ResponseWriter, r *http.Request) {
	var req struct{ Compose string }
	r.Body = http.MaxBytesReader(w, r.Body, 512*1024)
	if !readJSONBody(w, r, &req) {
		return
	}
	name := r.PathValue("name")
	if err := s.opts.Docker.SetCompose(r.Context(), name, req.Compose); err != nil {
		writeDockerError(w, err)
		return
	}
	slog.Info("app changed", "app", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleAppUpdate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.opts.Docker.UpdateApp(r.Context(), name); err != nil {
		writeDockerError(w, err)
		return
	}
	slog.Info("app update started", "app", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusAccepted)
}

// handleAppJobDismiss hides the message of a failed app setup.
func (s *Server) handleAppJobDismiss(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Docker.DismissJob(r.PathValue("name")); err != nil {
		writeDockerError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUpdatesCheck asks the registries now instead of waiting for the next round.
func (s *Server) handleUpdatesCheck(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Docker.CheckUpdates(r.Context()); err != nil {
		writeDockerError(w, err)
		return
	}
	s.handleDocker(w, r)
}

// handleUpdatesApply updates every app that has a newer image available.
func (s *Server) handleUpdatesApply(w http.ResponseWriter, r *http.Request) {
	apps := s.opts.Docker.AppsWithUpdates(r.Context())
	for _, app := range apps {
		if err := s.opts.Docker.UpdateApp(r.Context(), app); err != nil && !errors.Is(err, docker.ErrBusy) {
			writeDockerError(w, err)
			return
		}
	}
	slog.Info("update of all apps started", "apps", apps, "by", currentUser(r).Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusAccepted, map[string][]string{"apps": apps})
}

// handleContainerConsole connects a WebSocket to a shell inside a container.
func (s *Server) handleContainerConsole(w http.ResponseWriter, r *http.Request) {
	id, shell, user := r.PathValue("id"), r.URL.Query().Get("shell"), r.URL.Query().Get("user")
	// The short form of the ID, as Docker's own tools show it.
	target := "container " + id[:min(len(id), 12)]
	if user != "" {
		target += " as " + user
	}
	s.serveConsole(w, r, target, func(ctx context.Context) (io.ReadWriteCloser, func(cols, rows int), error) {
		return s.opts.Docker.Console(ctx, id, shell, user)
	})
}

// handleShell connects a WebSocket to a root shell on the NAS.
func (s *Server) handleShell(w http.ResponseWriter, r *http.Request) {
	s.serveConsole(w, r, "nas", func(ctx context.Context) (io.ReadWriteCloser, func(cols, rows int), error) {
		session, err := s.opts.Terminal.Shell(ctx)
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
func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{
		"enabled": s.opts.Terminal.Enabled(),
		"confirm": s.opts.Terminal.ConfirmRequired(),
		// Without a shell on the NAS there are container consoles only.
		"nasShell": !s.opts.Terminal.Disabled,
	})
}

func (s *Server) handleTerminalSettings(w http.ResponseWriter, r *http.Request) {
	var req struct{ Enabled, Confirm *bool }
	if !readJSON(w, r, &req) {
		return
	}
	if req.Enabled != nil {
		if err := s.opts.Terminal.SetEnabled(*req.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
			return
		}
		slog.Info("terminals switched", "enabled", *req.Enabled, "by", currentUser(r).Name, "ip", remoteIP(r))
	}
	if req.Confirm != nil {
		if err := s.opts.Terminal.SetConfirmRequired(*req.Confirm); err != nil {
			writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
			return
		}
		slog.Info("password prompt for consoles switched", "required", *req.Confirm, "by", currentUser(r).Name, "ip", remoteIP(r))
	}
	s.handleTerminal(w, r)
}

// handleConfirm marks the session as confirmed after the password was
// entered again. Consoles ask for this.
func (s *Server) handleConfirm(w http.ResponseWriter, r *http.Request) {
	var req struct{ Password, Code string }
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	key := "user:" + user.Name
	if wait := s.limiter.RetryAfter(key); wait > 0 {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed attempts, try again later")
		return
	}
	if _, ok := s.opts.Users.Authenticate(user.Name, req.Password); !ok {
		s.limiter.Fail(key)
		writeError(w, http.StatusForbidden, "wrong_password", "the password is wrong")
		return
	}
	// With two-factor login the confirmation takes both factors as well.
	if user.TwoFactor() && !s.opts.Users.CheckSecondFactor(user.Name, req.Code) {
		s.limiter.Fail(key)
		writeError(w, http.StatusForbidden, "invalid_code", "the code is wrong or was used already")
		return
	}
	if cookie, err := r.Cookie(s.cookieName()); err == nil {
		s.sessions.Confirm(cookie.Value)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleConfirmed(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"confirmed": s.confirmed(r)})
}

// confirmed reports whether the password was entered again recently. When
// the owner switched the prompt off in the settings, it always counts.
func (s *Server) confirmed(r *http.Request) bool {
	if !s.opts.Terminal.ConfirmRequired() {
		return true
	}
	cookie, err := r.Cookie(s.cookieName())
	return err == nil && s.sessions.Confirmed(cookie.Value)
}
