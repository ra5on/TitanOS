package api

// „Öffnen mit“ für Docker-Apps (PLAN 72, docker/opens.go). Not in
// publicRoutes and not in the tool catalog: only the web UI opens files in
// apps and mounts shares into them.

import (
	"errors"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/shares"
)

func (s *Server) registerDockerOpens(mux *routeMux) {
	s.dockerOpensArea().register(mux, s.guards(), s.appLifecycle)
}

// dockerOpensArea is "open with" for Docker apps; an area with narrow
// dependencies (PLAN 8a, see fansArea). files turns a path of the file
// manager into one of the host, shares names the folder of a share.
// allowsApp says whether the account of a request may use an app,
// fileRoot resolves a root of the file manager with the rights of the
// account (it answers itself when it refuses), findShare finds a share.
// The receiver is ar: a is an app in this file.
type dockerOpensArea struct {
	docker    *docker.Manager
	files     *files.Manager
	shares    *shares.Manager
	allowsApp func(r *http.Request, app string) bool
	fileRoot  func(w http.ResponseWriter, r *http.Request, id string) (files.Root, bool)
	findShare func(name string) (shares.Share, bool)
}

// dockerOpensArea is the area of the server.
func (s *Server) dockerOpensArea() dockerOpensArea {
	return dockerOpensArea{docker: s.opts.Docker, files: s.opts.Files, shares: s.opts.Shares,
		allowsApp: s.appAccessArea().allowsApp, fileRoot: s.filesArea().fileRoot, findShare: s.sharesArea().findShare}
}

// register takes appLifecycle of the server: opening a file waits like
// other actions on an app while its backup runs.
func (ar dockerOpensArea) register(mux *routeMux, g guards, appLifecycle func(hold bool, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/docker/openers", g.user(ar.handleDockerOpeners))
	mux.HandleFunc("POST /api/v1/docker/apps/{name}/open", g.user(appLifecycle(false, ar.handleDockerOpen)))
	mux.HandleFunc("GET /api/v1/docker/apps/{name}/opens", g.admin(ar.handleDockerOpensGet))
	mux.HandleFunc("PUT /api/v1/docker/apps/{name}/opens", g.appAdmin(ar.handleDockerOpensSet))
	mux.HandleFunc("POST /api/v1/docker/apps/{name}/shares", g.appAdmin(ar.handleDockerShareMount))
	mux.HandleFunc("DELETE /api/v1/docker/apps/{name}/shares/{share}", g.appAdmin(ar.handleDockerShareUnmount))
}

var dockerOpenErrors = []struct {
	err    error
	status int
	code   string
}{
	{docker.ErrNotMounted, http.StatusConflict, "not_mounted"},
	{docker.ErrNoOpener, http.StatusBadRequest, "no_opener"},
	{docker.ErrMounted, http.StatusConflict, "already_mounted"},
}

func writeDockerOpenError(w http.ResponseWriter, err error) {
	for _, known := range dockerOpenErrors {
		if errors.Is(err, known.err) {
			writeError(w, known.status, known.code, known.err.Error())
			return
		}
	}
	var invalid *docker.InvalidComposeError
	if errors.Is(err, docker.ErrInvalid) && !errors.As(err, &invalid) {
		writeError(w, http.StatusBadRequest, "invalid_opens", err.Error())
		return
	}
	writeDockerError(w, err)
}

// fileOpener is a Docker app that can open one file, for the „Öffnen mit“ menu.
type fileOpener struct {
	App     string `json:"app"`
	Name    string `json:"name"`
	Running bool   `json:"running"`
	// Mounted: the file lies in a folder of the app.
	Mounted  bool `json:"mounted"`
	ReadOnly bool `json:"readOnly,omitempty"`
	// Share is the share of the file; CanMount whether this account may
	// mount it into the app (owner and administrators).
	Share    string `json:"share,omitempty"`
	CanMount bool   `json:"canMount,omitempty"`
}

// handleDockerOpeners lists the Docker apps that open a file
// (?root=&path=), or without root the apps and their types for the
// standard apps of the account: [{app, name, types}].
func (ar dockerOpensArea) handleDockerOpeners(w http.ResponseWriter, r *http.Request) {
	if ar.docker == nil {
		writeJSON(w, http.StatusOK, []fileOpener{})
		return
	}
	all, err := ar.docker.Openers(r.Context())
	if err != nil {
		// No engine, no apps to open with; the menu simply has none.
		slog.Warn("docker openers", "err", err)
		writeJSON(w, http.StatusOK, []fileOpener{})
		return
	}
	q := r.URL.Query()
	if q.Get("root") == "" {
		type typesOf struct {
			App   string   `json:"app"`
			Name  string   `json:"name"`
			Types []string `json:"types"`
		}
		out := []typesOf{}
		for _, a := range all {
			if a.Error == "" && !a.Hidden && ar.allowsApp(r, a.App) {
				out = append(out, typesOf{App: a.App, Name: a.Name, Types: a.Types})
			}
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	root, ok := ar.fileRoot(w, r, q.Get("root"))
	if !ok {
		return
	}
	host, err := ar.files.HostPath(root, q.Get("path"))
	if err != nil {
		writeFileError(w, err)
		return
	}
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(host)), ".")
	user := currentUser(r)
	out := []fileOpener{}
	for _, a := range all {
		if a.Error != "" || a.Hidden || !a.Opens(ext) || !ar.allowsApp(r, a.App) {
			continue
		}
		item := fileOpener{App: a.App, Name: a.Name, Running: a.Running}
		if _, mount, ok := docker.ContainerPath(a.Mounts, host); ok {
			item.Mounted, item.ReadOnly = true, mount.ReadOnly
		} else if root.Kind == "share" {
			item.Share = root.Name
			item.CanMount = user.Role.CanAdminister() && ar.mountableShare(root.Name)
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
}

// mountableShare: a share that can go into an app as a whole (not the
// per-account share of the KI agent).
func (ar dockerOpensArea) mountableShare(name string) bool {
	share, ok := ar.findShare(name)
	return ok && !share.PerUser
}

// handleDockerOpen starts the app's program for a file: {root, path}.
func (ar dockerOpensArea) handleDockerOpen(w http.ResponseWriter, r *http.Request) {
	var req struct{ Root, Path string }
	if !readJSON(w, r, &req) {
		return
	}
	app := r.PathValue("name")
	noteTarget(r, app+": "+filePlace(req.Root, req.Path))
	if !ar.allowsApp(r, app) {
		writeError(w, http.StatusForbidden, "app_forbidden", appWindowMessage("app_forbidden"))
		return
	}
	root, ok := ar.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	host, err := ar.files.HostPath(root, req.Path)
	if err != nil {
		writeFileError(w, err)
		return
	}
	if err := ar.docker.OpenFile(r.Context(), app, host); err != nil {
		writeDockerOpenError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDockerOpensGet is the opens block of an app with its mounts, for
// Docker › Bearbeiten; 404 when the app has none.
func (ar dockerOpensArea) handleDockerOpensGet(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("name")
	all, err := ar.docker.Openers(r.Context())
	if err != nil {
		writeDockerError(w, err)
		return
	}
	for _, a := range all {
		if a.App == app {
			writeJSON(w, http.StatusOK, a)
			return
		}
	}
	writeError(w, http.StatusNotFound, "no_opener", docker.ErrNoOpener.Error())
}

func (ar dockerOpensArea) handleDockerOpensSet(w http.ResponseWriter, r *http.Request) {
	var op docker.Opener
	if !readJSON(w, r, &op) {
		return
	}
	app := r.PathValue("name")
	noteTarget(r, app)
	if err := ar.docker.SetOpener(r.Context(), app, op); err != nil {
		writeDockerOpenError(w, err)
		return
	}
	slog.Info("app opens changed", "app", app, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusAccepted)
}

// handleDockerShareMount mounts a share into an app: {share, readOnly}.
// The app sees the whole share; the container is created anew.
func (ar dockerOpensArea) handleDockerShareMount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Share    string
		ReadOnly bool
	}
	if !readJSON(w, r, &req) {
		return
	}
	app := r.PathValue("name")
	noteTarget(r, app+": "+req.Share)
	share, ok := ar.findShare(req.Share)
	if !ok || share.PerUser {
		writeError(w, http.StatusNotFound, "share_not_found", "no such share")
		return
	}
	if err := ar.docker.MountShare(r.Context(), app, share.Name, ar.shares.Path(share), req.ReadOnly); err != nil {
		writeDockerOpenError(w, err)
		return
	}
	slog.Info("share mounted into app", "app", app, "share", share.Name, "readOnly", req.ReadOnly, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusAccepted)
}

func (ar dockerOpensArea) handleDockerShareUnmount(w http.ResponseWriter, r *http.Request) {
	app, share := r.PathValue("name"), r.PathValue("share")
	noteTarget(r, app+": "+share)
	if err := ar.docker.UnmountShare(r.Context(), app, share); err != nil {
		writeDockerOpenError(w, err)
		return
	}
	slog.Info("share removed from app", "app", app, "share", share, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusAccepted)
}
