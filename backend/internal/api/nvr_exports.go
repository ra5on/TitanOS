package api

// Exports of the NVR: pieces of recording saved as one file. An export
// belongs to the account that made it; owners and administrators see
// all. Making one needs the camera right "recordings".

import (
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/nvr"
)

func (a nvrArea) registerExports(mux *routeMux, g guards) {
	mux.HandleFunc("GET /api/v1/nvr/exports", g.user(a.handleExports))
	mux.HandleFunc("POST /api/v1/nvr/exports", g.user(a.handleExportCreate))
	mux.HandleFunc("GET /api/v1/nvr/exports/{id}", g.user(a.handleExport))
	mux.HandleFunc("PUT /api/v1/nvr/exports/{id}", g.user(a.handleExportRename))
	mux.HandleFunc("DELETE /api/v1/nvr/exports/{id}", g.user(a.handleExportDelete))
	mux.HandleFunc("GET /api/v1/nvr/exports/{id}/download", g.user(a.handleExportDownload))
	mux.HandleFunc("GET /api/v1/nvr/exports/{id}/thumbnail.jpg", g.user(a.handleExportThumbnail))
}

func (a nvrArea) handleExports(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	user := currentUser(r)
	list, err := a.nvr.Exports(r.Context(), user.Name, user.Role.CanAdminister())
	if err != nil {
		writeNVRError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"exports": list})
}

// handleExportCreate starts an export: {camera, from, to, name}.
func (a nvrArea) handleExportCreate(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	var req struct {
		Camera string  `json:"camera"`
		From   float64 `json:"from"`
		To     float64 `json:"to"`
		Name   string  `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	if !a.reaches(user, req.Camera, nvr.RightRecordings) {
		writeNVRError(w, nvr.ErrNotFound)
		return
	}
	export, err := a.nvr.CreateExport(r.Context(), user.Name, req.Camera, req.From, req.To, req.Name)
	if err != nil {
		writeNVRError(w, err)
		return
	}
	noteTarget(r, req.Camera)
	slog.Info("NVR export started", "camera", req.Camera, "by", user.Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusCreated, export)
}

// export returns the export of the request if the account may see it.
// An export of a camera the account lost the right on is not handed out
// either.
func (a nvrArea) export(w http.ResponseWriter, r *http.Request) (nvr.Export, bool) {
	if !a.available(w) {
		return nvr.Export{}, false
	}
	user := currentUser(r)
	export, err := a.nvr.Export(r.Context(), r.PathValue("id"), user.Name, user.Role.CanAdminister())
	if err == nil && !a.allows(user, export.Camera, nvr.RightRecordings) {
		err = nvr.ErrExportNotFound
	}
	if err != nil {
		writeNVRError(w, err)
		return nvr.Export{}, false
	}
	return export, true
}

func (a nvrArea) handleExport(w http.ResponseWriter, r *http.Request) {
	if export, ok := a.export(w, r); ok {
		writeJSON(w, http.StatusOK, export)
	}
}

// handleExportRename renames an export: {name}.
func (a nvrArea) handleExportRename(w http.ResponseWriter, r *http.Request) {
	export, ok := a.export(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	renamed, err := a.nvr.RenameExport(r.Context(), export.ID, user.Name, user.Role.CanAdminister(), req.Name)
	if err != nil {
		writeNVRError(w, err)
		return
	}
	noteTarget(r, export.Camera)
	writeJSON(w, http.StatusOK, renamed)
}

func (a nvrArea) handleExportDelete(w http.ResponseWriter, r *http.Request) {
	export, ok := a.export(w, r)
	if !ok {
		return
	}
	user := currentUser(r)
	if err := a.nvr.DeleteExport(r.Context(), export.ID, user.Name, user.Role.CanAdminister()); err != nil {
		writeNVRError(w, err)
		return
	}
	noteTarget(r, export.Camera)
	slog.Info("NVR export deleted", "camera", export.Camera, "by", user.Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// handleExportDownload hands the video of an export over as an attachment.
func (a nvrArea) handleExportDownload(w http.ResponseWriter, r *http.Request) {
	export, ok := a.export(w, r)
	if !ok {
		return
	}
	video, _, err := export.ExportMedia()
	if err != nil {
		writeNVRError(w, err)
		return
	}
	noteTarget(r, export.Camera)
	a.passMedia(w, r, video, "", "video/", export.Name+".mp4")
}

func (a nvrArea) handleExportThumbnail(w http.ResponseWriter, r *http.Request) {
	export, ok := a.export(w, r)
	if !ok {
		return
	}
	_, thumbnail, err := export.ExportMedia()
	if err != nil || thumbnail == "" {
		writeError(w, http.StatusNotFound, "nvr_no_picture", "the export has no preview picture")
		return
	}
	a.passMedia(w, r, thumbnail, "", "image/", "")
}
