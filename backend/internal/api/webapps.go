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

	"github.com/ra5on/TitanOS/backend/internal/webapps"
)

// registerWebapps registers "Web-Apps direkt" (PLAN 66, docs/WEBAPPS.md):
// the catalog and its installation, and the files of installed apps.
func (s *Server) registerWebapps(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/webapps", s.requireUser(s.handleWebapps))
	mux.HandleFunc("POST /api/v1/webapps/defaults/install", s.requireAdmin(s.handleWebappsDefaults))
	mux.HandleFunc("POST /api/v1/webapps/migrate", s.requireAppAdmin(s.handleWebappsMigrate))
	mux.HandleFunc("POST /api/v1/webapps/{id}/install", s.requireAdmin(s.handleWebappInstall))
	mux.HandleFunc("DELETE /api/v1/webapps/{id}", s.requireAdmin(s.handleWebappRemove))
	// The mediator in the app's document is a file of the web UI; "_host"
	// is never an app id, and this route is more specific than the apps'.
	mux.HandleFunc("GET /webapps/_host/{file}", s.handleWebappHost)
	// The apps' files: public open-source code, without sign-in; the
	// sandboxed frame sends no cookies anyway.
	if s.opts.Webapps != nil {
		mux.Handle("GET /webapps/{id}/{version}/{path...}", s.opts.Webapps)
	}
}

// handleWebappHost serves the mediator files of the web UI under
// webapps/_host/ (only files that exist there, never the UI's index.html:
// a script answered with HTML would be blocked silently).
func (s *Server) handleWebappHost(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	h := w.Header()
	h.Set("Content-Security-Policy", "sandbox allow-scripts; frame-ancestors 'self'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
	h.Set("Access-Control-Allow-Origin", "*")
	if s.opts.UI == nil || !strings.HasSuffix(name, ".js") || strings.HasPrefix(name, ".") || strings.ContainsAny(name, "/\\") {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(s.opts.UI, "webapps/_host/"+name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.Set("Content-Type", "text/javascript; charset=utf-8")
	// Part of the web UI: a new titand may bring a new one.
	h.Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

func (s *Server) handleWebapps(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.webappsBody(r))
}

// webappsBody is the answer of GET /api/v1/webapps. Docker versions and
// the migration are for administrators only.
func (s *Server) webappsBody(r *http.Request) map[string]any {
	body := map[string]any{"apps": []webapps.Status{}, "volume": "", "docker": []webapps.DockerApp{}, "migration": nil}
	if s.opts.Webapps == nil {
		return body
	}
	body["apps"], body["volume"] = s.opts.Webapps.List(), s.opts.Webapps.Volume()
	if currentUser(r).Role.CanAdminister() {
		if found := s.dockerCraftApps(r.Context()); found != nil {
			body["docker"] = found
		}
		body["migration"] = s.migration.status()
	}
	return body
}

// dockerCraftApps are the TitanOS Docker apps that are Craft apps.
func (s *Server) dockerCraftApps(ctx context.Context) []webapps.DockerApp {
	if s.migration.detect != nil {
		return s.migration.detect(ctx)
	}
	if s.opts.Docker == nil || s.opts.Docker.Disabled {
		return nil
	}
	// The list is asked for often while installs run; Docker is asked at
	// most every 20 seconds (and always for a migration, see fresh).
	s.migration.mu.Lock()
	if time.Since(s.migration.detectedAt) < 20*time.Second && ctx.Value(freshDetection{}) == nil {
		found := slices.Clone(s.migration.detected)
		s.migration.mu.Unlock()
		return found
	}
	s.migration.mu.Unlock()
	found := webapps.DetectDockerStatus(s.opts.Docker.Status(ctx))
	s.migration.mu.Lock()
	s.migration.detected, s.migration.detectedAt = slices.Clone(found), time.Now()
	s.migration.mu.Unlock()
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
	default:
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the web apps could not be saved to /config")
	}
}

func (s *Server) handleWebappInstall(w http.ResponseWriter, r *http.Request) {
	if s.opts.Webapps == nil {
		writeError(w, http.StatusServiceUnavailable, "webapps_unavailable", "web apps are not available")
		return
	}
	if err := s.opts.Webapps.Install(r.PathValue("id")); err != nil {
		writeWebappError(w, err)
		return
	}
	s.handleWebappsAccepted(w, r)
}

func (s *Server) handleWebappsAccepted(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusAccepted, s.webappsBody(r))
}

func (s *Server) handleWebappRemove(w http.ResponseWriter, r *http.Request) {
	if s.opts.Webapps == nil {
		writeError(w, http.StatusServiceUnavailable, "webapps_unavailable", "web apps are not available")
		return
	}
	if err := s.opts.Webapps.Uninstall(r.PathValue("id")); err != nil {
		writeWebappError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.webappsBody(r))
}

// handleWebappsDefaults installs the basic set, or {"apps":[…]} if given;
// an empty list only records that the offer was answered. Without a
// mounted volume the apps wait until one is.
func (s *Server) handleWebappsDefaults(w http.ResponseWriter, r *http.Request) {
	if s.opts.Webapps == nil {
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
	if err := s.opts.Webapps.InstallDefaults(ids); err != nil {
		writeWebappError(w, err)
		return
	}
	s.handleWebappsAccepted(w, r)
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
func (s *Server) handleWebappsMigrate(w http.ResponseWriter, r *http.Request) {
	if s.opts.Webapps == nil {
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
	for _, st := range s.opts.Webapps.List() {
		installed[st.ID] = st.Installed && st.State == "ready"
	}
	found := s.dockerCraftApps(context.WithValue(r.Context(), freshDetection{}, true))
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
	s.migration.mu.Lock()
	if s.migration.last != nil && s.migration.last.Running {
		s.migration.mu.Unlock()
		writeError(w, http.StatusConflict, "webapp_busy", "a migration is running")
		return
	}
	s.migration.last = &MigrationStatus{Running: true, Started: time.Now(), Install: slices.Clone(req.Install), Items: items}
	s.migration.mu.Unlock()
	for _, id := range req.Install {
		// Already there in the catalog's version: nothing to fetch.
		if app, _ := webapps.Lookup(id); installed[id] && slices.ContainsFunc(s.opts.Webapps.List(), func(st webapps.Status) bool { return st.ID == id && st.Version == app.Version }) {
			continue
		}
		if err := s.opts.Webapps.Install(id); err != nil {
			slog.Warn("web app install not started", "app", id, "err", err)
		}
	}
	user := currentUser(r).Name
	ctx := s.ctx
	go s.runMigration(ctx, user)
	slog.Info("web app migration started", "install", req.Install, "removeDocker", req.RemoveDocker, "by", user, "ip", remoteIP(r))
	s.handleWebappsAccepted(w, r)
}

// runMigration waits for each web app and removes its Docker app after it.
func (s *Server) runMigration(ctx context.Context, user string) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	items := s.migration.status().Items
	for i, it := range items {
		if err := s.opts.Webapps.Await(ctx, it.Webapp); err != nil {
			s.migration.set(i, "kept", err.Error())
			slog.Warn("Docker app kept: its web app did not install", "app", it.App, "webapp", it.Webapp, "err", err)
			continue
		}
		s.migration.set(i, "removing", "")
		remove := func(ctx context.Context, name string) error { return s.removeDockerApp(ctx, name, false) }
		if s.migration.remove != nil {
			remove = s.migration.remove
		}
		if err := remove(ctx, it.App); err != nil {
			s.migration.set(i, "failed", err.Error())
			slog.Warn("Docker app not removed", "app", it.App, "err", err)
			continue
		}
		s.migration.set(i, "removed", "")
		slog.Info("Docker app replaced by its web app", "app", it.App, "webapp", it.Webapp, "by", user)
	}
	// Installs without a Docker app to remove are followed in the list.
	s.migration.mu.Lock()
	s.migration.last.Running, s.migration.last.Finished = false, time.Now()
	s.migration.mu.Unlock()
}
