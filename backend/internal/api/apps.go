package api

import (
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/ra5on/TitanOS/backend/internal/catalog"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/hwcheck"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/system"
)

var catalogErrors = []struct {
	err    error
	status int
	code   string
}{
	{catalog.ErrNotLoaded, http.StatusServiceUnavailable, "config_unavailable"},
	{catalog.ErrInvalidURL, http.StatusBadRequest, "invalid_source_url"},
	{catalog.ErrExists, http.StatusConflict, "source_exists"},
	{catalog.ErrNotFound, http.StatusNotFound, "source_not_found"},
	{catalog.ErrTooMany, http.StatusConflict, "too_many_sources"},
	{catalog.ErrUnreachable, http.StatusBadGateway, "source_unreachable"},
	{catalog.ErrEmpty, http.StatusUnprocessableEntity, "source_empty"},
}

func writeCatalogError(w http.ResponseWriter, err error) {
	for _, known := range catalogErrors {
		if errors.Is(err, known.err) {
			writeError(w, known.status, known.code, err.Error())
			return
		}
	}
	slog.Error("app source action failed", "err", err)
	writeError(w, http.StatusInternalServerError, "internal", err.Error())
}

// appStoreArea is the app store: templates to install as Docker apps; an
// area with narrow dependencies (PLAN 8a, see fansArea). docker, storage and
// settings fill in what a template asks for; hwcheck names what a compose
// file needs that the CPU lacks (nil: nothing).
type appStoreArea struct {
	catalog  *catalog.Manager
	docker   *docker.Manager
	storage  *storage.Manager
	settings *system.Settings
	hwcheck  *hwcheck.Checker
}

// appStoreArea is the area of the server.
func (s *Server) appStoreArea() appStoreArea {
	return appStoreArea{catalog: s.opts.Catalog, docker: s.opts.Docker, storage: s.opts.Storage, settings: s.opts.Settings, hwcheck: s.opts.HWCheck}
}

func (a appStoreArea) register(mux *routeMux, g guards) {
	mux.HandleFunc("GET /api/v1/apps", g.admin(a.handleApps))
	mux.HandleFunc("POST /api/v1/apps/refresh", g.admin(a.handleAppsRefresh))
	mux.HandleFunc("POST /api/v1/apps/sources", g.admin(a.handleSourceAdd))
	mux.HandleFunc("DELETE /api/v1/apps/sources/{id}", g.admin(a.handleSourceRemove))
	mux.HandleFunc("GET /api/v1/apps/sources/{source}/apps/{app}", g.admin(a.handleAppTemplate))
	mux.HandleFunc("GET /api/v1/apps/blank", g.admin(a.handleBlankTemplate))
}

func (a appStoreArea) handleApps(w http.ResponseWriter, r *http.Request) {
	sources, apps := a.catalog.Status()
	writeJSON(w, http.StatusOK, map[string]any{"sources": sources, "apps": apps})
}

func (a appStoreArea) handleAppsRefresh(w http.ResponseWriter, r *http.Request) {
	a.catalog.Refresh(r.Context())
	a.handleApps(w, r)
}

func (a appStoreArea) handleSourceAdd(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name, URL string }
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.catalog.Add(r.Context(), req.Name, req.URL); err != nil {
		writeCatalogError(w, err)
		return
	}
	slog.Info("app source added", "url", req.URL, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleApps(w, r)
}

func (a appStoreArea) handleSourceRemove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.catalog.Remove(id); err != nil {
		writeCatalogError(w, err)
		return
	}
	slog.Info("app source removed", "source", id, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleApps(w, r)
}

// handleAppTemplate returns the compose file of a template together with
// what TitanOS fills in when it is installed.
func (a appStoreArea) handleAppTemplate(w http.ResponseWriter, r *http.Request) {
	compose, err := a.catalog.Compose(r.PathValue("source"), r.PathValue("app"))
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	a.writeTemplate(w, r, compose)
}

// blankTemplate is where an own container starts: one service whose image
// the form asks for.
const blankTemplate = "services:\n  app:\n    image: \"\"\n    restart: unless-stopped\n"

// handleBlankTemplate answers like a template of the catalog, for an own
// container that is set up in the same form.
func (a appStoreArea) handleBlankTemplate(w http.ResponseWriter, r *http.Request) {
	a.writeTemplate(w, r, blankTemplate)
}

func (a appStoreArea) writeTemplate(w http.ResponseWriter, r *http.Request, compose string) {
	appdata := ""
	if settings := a.docker.Current(); settings.Enabled && settings.Volume != "" {
		appdata = filepath.Join(a.storage.MountPoint(settings.Volume), "appdata")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"compose":  compose,
		"timezone": a.settings.CurrentTimezone(),
		// Files the app writes into shares belong to whoever installs it,
		// and to the group all accounts are in.
		"uid":       currentUser(r).UID,
		"gid":       shares.UsersGID,
		"appdata":   appdata,
		"listening": system.ListeningPorts(),
		// Devices of the template this machine does not have; with them the
		// containers would not start.
		"missingDevices": catalog.MissingDevices(compose),
		// Instruction sets (AVX, x86-64-v3 …) the app needs and this CPU lacks.
		"cpuMissing": a.cpuMissing(compose),
	})
}
