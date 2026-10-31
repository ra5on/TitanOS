package api

import (
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/ra5on/TitanOS/backend/internal/catalog"
	"github.com/ra5on/TitanOS/backend/internal/shares"
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

func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	sources, apps := s.opts.Catalog.Status()
	writeJSON(w, http.StatusOK, map[string]any{"sources": sources, "apps": apps})
}

func (s *Server) handleAppsRefresh(w http.ResponseWriter, r *http.Request) {
	s.opts.Catalog.Refresh(r.Context())
	s.handleApps(w, r)
}

func (s *Server) handleSourceAdd(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name, URL string }
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Catalog.Add(r.Context(), req.Name, req.URL); err != nil {
		writeCatalogError(w, err)
		return
	}
	slog.Info("app source added", "url", req.URL, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleApps(w, r)
}

func (s *Server) handleSourceRemove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.opts.Catalog.Remove(id); err != nil {
		writeCatalogError(w, err)
		return
	}
	slog.Info("app source removed", "source", id, "by", currentUser(r).Name, "ip", remoteIP(r))
	s.handleApps(w, r)
}

// handleAppTemplate returns the compose file of a template together with
// what TitanOS fills in when it is installed.
func (s *Server) handleAppTemplate(w http.ResponseWriter, r *http.Request) {
	compose, err := s.opts.Catalog.Compose(r.PathValue("source"), r.PathValue("app"))
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	appdata := ""
	if settings := s.opts.Docker.Current(); settings.Enabled && settings.Volume != "" {
		appdata = filepath.Join(s.opts.Storage.MountPoint(settings.Volume), "appdata")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"compose":  compose,
		"timezone": s.opts.Settings.CurrentTimezone(),
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
		"cpuMissing": s.cpuMissing(compose),
	})
}
