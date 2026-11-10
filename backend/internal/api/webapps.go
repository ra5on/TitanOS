package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/webapps"
)

// registerWebapps registers "Web-Apps direkt" (PLAN 66, docs/WEBAPPS.md):
// the catalog and its installation, and the files of installed apps.
func (s *Server) registerWebapps(mux *routeMux) {
	s.webappsArea().register(mux, s.requireAdmin, s.requireUser, s.requireAppAdmin)
}

// webappsArea is the web apps; an area with narrow dependencies (PLAN 8a,
// see fansArea). webapps is nil where there are none. ui holds the
// mediator files, docker finds the Docker versions of the programs,
// migration is the move from them (state of the server), ctx the root
// context for a migration that outlives its request, and removeDockerApp
// removes a Docker app like "remove app" does.
type webappsArea struct {
	webapps         *webapps.Manager
	ui              fs.FS
	docker          *docker.Manager
	migration       *webappMigration
	ctx             context.Context
	removeDockerApp func(ctx context.Context, name string, purge bool) error
}

// webappsArea is the area of the server; the automatic replacement
// (craft_replace.go) uses its detection and migration.
func (s *Server) webappsArea() webappsArea {
	return webappsArea{webapps: s.opts.Webapps, ui: s.opts.UI, docker: s.opts.Docker, migration: &s.migration, ctx: s.ctx, removeDockerApp: s.removeDockerApp}
}

func (a webappsArea) register(mux *routeMux, admin, user, appAdmin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/webapps", user(a.handleWebapps))
	mux.HandleFunc("POST /api/v1/webapps/defaults/install", admin(a.handleWebappsDefaults))
	mux.HandleFunc("POST /api/v1/webapps/migrate", appAdmin(a.handleWebappsMigrate))
	mux.HandleFunc("POST /api/v1/webapps/volume", admin(a.handleWebappsVolume))
	mux.HandleFunc("POST /api/v1/webapps/{id}/install", admin(a.handleWebappInstall))
	mux.HandleFunc("DELETE /api/v1/webapps/{id}", admin(a.handleWebappRemove))
	// The mediator in the app's document is a file of the web UI; "_host"
	// is never an app id, and this route is more specific than the apps'.
	mux.HandleFunc("GET /webapps/_host/{file}", a.handleWebappHost)
	// The apps' files: public open-source code, without sign-in; the
	// sandboxed frame sends no cookies anyway.
	if a.webapps != nil {
		mux.Handle("GET /webapps/{id}/{version}/{path...}", a.webapps)
	}
}

// handleWebappHost serves the mediator files of the web UI under
// webapps/_host/ (only files that exist there, never the UI's index.html:
// a script answered with HTML would be blocked silently).
func (a webappsArea) handleWebappHost(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	h := w.Header()
	h.Set("Content-Security-Policy", "sandbox allow-scripts; frame-ancestors 'self'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
	h.Set("Access-Control-Allow-Origin", "*")
	if a.ui == nil || !strings.HasSuffix(name, ".js") || strings.HasPrefix(name, ".") || strings.ContainsAny(name, "/\\") {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(a.ui, "webapps/_host/"+name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.Set("Content-Type", "text/javascript; charset=utf-8")
	// Part of the web UI: a new titand may bring a new one.
	h.Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

func (a webappsArea) handleWebapps(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.webappsBody(r))
}

// webappsBody is the answer of GET /api/v1/webapps. Docker versions and
// the migration are for administrators only.
func (a webappsArea) webappsBody(r *http.Request) map[string]any {
	body := map[string]any{"apps": []webapps.Status{}, "volume": "", "docker": []webapps.DockerApp{}, "migration": nil}
	if a.webapps == nil {
		return body
	}
	body["apps"], body["volume"] = a.webapps.List(), a.webapps.Volume()
	// PLAN 75-21: where the programs can lie, and whether the owner has to choose first.
	body["volumes"], body["needsVolume"] = a.webapps.Volumes(), a.webapps.NeedsVolume()
	if currentUser(r).Role.CanAdminister() {
		if found := a.dockerCraftApps(r.Context()); found != nil {
			body["docker"] = found
		}
		body["migration"] = a.migration.status()
	}
	return body
}

// dockerCraftApps are the TitanOS Docker apps that are Craft apps.
func (a webappsArea) dockerCraftApps(ctx context.Context) []webapps.DockerApp {
	if a.migration.detect != nil {
		return a.migration.detect(ctx)
	}
	if a.docker == nil || a.docker.Disabled {
		return nil
	}
	// The list is asked for often while installs run; Docker is asked at
	// most every 20 seconds (and always for a migration, see fresh).
	a.migration.mu.Lock()
	if time.Since(a.migration.detectedAt) < 20*time.Second && ctx.Value(freshDetection{}) == nil {
		found := slices.Clone(a.migration.detected)
		a.migration.mu.Unlock()
		return found
	}
	a.migration.mu.Unlock()
	found := webapps.DetectDockerStatus(a.docker.Status(ctx))
	a.migration.mu.Lock()
	a.migration.detected, a.migration.detectedAt = slices.Clone(found), time.Now()
	a.migration.mu.Unlock()
	return found
}

// freshDetection asks dockerCraftApps not to use its cache.
type freshDetection struct{}

func writeWebappError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, webapps.ErrUnknown):
		writeError(w, http.StatusNotFound, "webapp_unknown", err.Error())
	case errors.Is(err, webapps.ErrNoApp):
		writeError(w, http.StatusNotFound, "webapp_not_installed", err.Error())
	case errors.Is(err, webapps.ErrBusy):
		writeError(w, http.StatusConflict, "webapp_busy", err.Error())
	case errors.Is(err, webapps.ErrVolume):
		writeError(w, http.StatusBadRequest, "volume_not_mounted", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the web apps could not be saved to /config")
	}
}

func (a webappsArea) handleWebappInstall(w http.ResponseWriter, r *http.Request) {
	if a.webapps == nil {
		writeError(w, http.StatusServiceUnavailable, "webapps_unavailable", "web apps are not available")
		return
	}
	if err := a.webapps.Install(r.PathValue("id")); err != nil {
		writeWebappError(w, err)
		return
	}
	a.handleWebappsAccepted(w, r)
}

func (a webappsArea) handleWebappsAccepted(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusAccepted, a.webappsBody(r))
}

func (a webappsArea) handleWebappRemove(w http.ResponseWriter, r *http.Request) {
	if a.webapps == nil {
		writeError(w, http.StatusServiceUnavailable, "webapps_unavailable", "web apps are not available")
		return
	}
	if err := a.webapps.Uninstall(r.PathValue("id")); err != nil {
		writeWebappError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.webappsBody(r))
}

// handleWebappsDefaults installs the basic set, or {"apps":[…]} if given;
// an empty list only records that the offer was answered. Without a
// mounted volume the apps wait until one is.
func (a webappsArea) handleWebappsDefaults(w http.ResponseWriter, r *http.Request) {
	if a.webapps == nil {
		writeError(w, http.StatusServiceUnavailable, "webapps_unavailable", "web apps are not available")
		return
	}
	var req struct {
		Apps *[]string `json:"apps"`
	}
	if r.ContentLength != 0 && r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "could not read the request")
			return
		}
		if strings.TrimSpace(string(data)) != "" {
			r.Body = io.NopCloser(strings.NewReader(string(data)))
			if !readJSONBody(w, r, &req) {
				return
			}
		}
	}
	var ids []string
	if req.Apps != nil {
		ids = append([]string{}, *req.Apps...)
	}
	if err := a.webapps.InstallDefaults(ids); err != nil {
		writeWebappError(w, err)
		return
	}
	a.handleWebappsAccepted(w, r)
}

// MigrationItem is one Docker app of a migration.
type MigrationItem struct {
	App    string `json:"app"`
	Webapp string `json:"webapp"`
	// State: waiting (for the web app), removing, removed, kept (the web
	// app did not install; the Docker app stays), failed (removal failed).
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

// MigrationStatus is the last migration, in RAM.
type MigrationStatus struct {
	Running  bool            `json:"running"`
	Started  time.Time       `json:"started"`
	Install  []string        `json:"install"`
	Items    []MigrationItem `json:"items"`
	Finished time.Time       `json:"finished,omitzero"`
}

type webappMigration struct {
	mu         sync.Mutex
	last       *MigrationStatus
	detected   []webapps.DockerApp
	detectedAt time.Time
	// Tests replace detection and removal.
	detect func(ctx context.Context) []webapps.DockerApp
	remove func(ctx context.Context, name string) error
}

func (m *webappMigration) status() *MigrationStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.last == nil {
		return nil
	}
	c := *m.last
	c.Items = slices.Clone(m.last.Items)
	c.Install = slices.Clone(m.last.Install)
	return &c
}

func (m *webappMigration) set(i int, state, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.last.Items[i].State, m.last.Items[i].Error = state, msg
}

// handleWebappsMigrate installs web apps and removes the Docker apps they
// replace: {"install":["wordcraft"],"removeDocker":["wordcraft"]}. Each
// Docker app must be detected as a Craft app, and its web app must be in
// "install" or installed. It is removed only after its web app installed;
// the removal is the normal "remove app" without deleting data.
func (a webappsArea) handleWebappsMigrate(w http.ResponseWriter, r *http.Request) {
	if a.webapps == nil {
		writeError(w, http.StatusServiceUnavailable, "webapps_unavailable", "web apps are not available")
		return
	}
	var req struct {
		Install      []string `json:"install"`
		RemoveDocker []string `json:"removeDocker"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, append(slices.Clone(req.Install), req.RemoveDocker...)...)
	for _, id := range req.Install {
		if _, ok := webapps.Lookup(id); !ok {
			writeError(w, http.StatusBadRequest, "webapp_unknown", "no such web app: "+id)
			return
		}
	}
	installed := map[string]bool{}
	for _, st := range a.webapps.List() {
		installed[st.ID] = st.Installed && st.State == "ready"
	}
	found := a.dockerCraftApps(context.WithValue(r.Context(), freshDetection{}, true))
	var items []MigrationItem
	for _, name := range req.RemoveDocker {
		i := slices.IndexFunc(found, func(d webapps.DockerApp) bool { return d.App == name })
		if i < 0 {
			writeError(w, http.StatusBadRequest, "webapp_not_craft_docker", "not a Docker app of a Craft program: "+name)
			return
		}
		if slices.ContainsFunc(items, func(it MigrationItem) bool { return it.App == name }) {
			continue
		}
		id := found[i].Webapp
		if !slices.Contains(req.Install, id) && !installed[id] {
			writeError(w, http.StatusBadRequest, "webapp_not_installed", "the web app "+id+" must be installed before "+name+" is removed")
			return
		}
		items = append(items, MigrationItem{App: name, Webapp: id, State: "waiting"})
	}
	a.migration.mu.Lock()
	if a.migration.last != nil && a.migration.last.Running {
		a.migration.mu.Unlock()
		writeError(w, http.StatusConflict, "webapp_busy", "a migration is running")
		return
	}
	a.migration.last = &MigrationStatus{Running: true, Started: time.Now(), Install: slices.Clone(req.Install), Items: items}
	a.migration.mu.Unlock()
	for _, id := range req.Install {
		// Already there in the catalog's version: nothing to fetch.
		if app, _ := webapps.Lookup(id); installed[id] && slices.ContainsFunc(a.webapps.List(), func(st webapps.Status) bool { return st.ID == id && st.Version == app.Version }) {
			continue
		}
		if err := a.webapps.Install(id); err != nil {
			slog.Warn("web app install not started", "app", id, "err", err)
		}
	}
	user := currentUser(r).Name
	ctx := a.ctx
	go a.runMigration(ctx, user, false)
	slog.Info("web app migration started", "install", req.Install, "removeDocker", req.RemoveDocker, "by", user, "ip", remoteIP(r))
	a.handleWebappsAccepted(w, r)
}

// runMigration waits for each web app and removes its Docker app after it.
// purge also deletes the Docker data of the app (automatic replacement).
func (a webappsArea) runMigration(ctx context.Context, user string, purge bool) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	items := a.migration.status().Items
	for i, it := range items {
		if err := a.webapps.Await(ctx, it.Webapp); err != nil {
			a.migration.set(i, "kept", err.Error())
			slog.Warn("Docker app kept: its web app did not install", "app", it.App, "webapp", it.Webapp, "err", err)
			continue
		}
		a.migration.set(i, "removing", "")
		remove := func(ctx context.Context, name string) error { return a.removeDockerApp(ctx, name, purge) }
		if a.migration.remove != nil {
			remove = a.migration.remove
		}
		if err := remove(ctx, it.App); err != nil {
			a.migration.set(i, "failed", err.Error())
			slog.Warn("Docker app not removed", "app", it.App, "err", err)
			continue
		}
		a.migration.set(i, "removed", "")
		slog.Info("Docker app replaced by its web app", "app", it.App, "webapp", it.Webapp, "by", user)
	}
	// Installs without a Docker app to remove are followed in the list.
	a.migration.mu.Lock()
	a.migration.last.Running, a.migration.last.Finished = false, time.Now()
	a.migration.mu.Unlock()
}

// handleWebappsVolume chooses where the programs lie and moves the installed
// ones there: {"volume":"data"} (PLAN 75-21).
func (a webappsArea) handleWebappsVolume(w http.ResponseWriter, r *http.Request) {
	if a.webapps == nil {
		writeError(w, http.StatusServiceUnavailable, "webapps_unavailable", "web apps are not available")
		return
	}
	var req struct {
		Volume string `json:"volume"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Volume)
	if err := a.webapps.SetVolume(req.Volume); err != nil {
		writeWebappError(w, err)
		return
	}
	slog.Info("web apps volume chosen", "volume", req.Volume, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleWebappsAccepted(w, r)
}
