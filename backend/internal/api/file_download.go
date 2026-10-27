package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"path"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/files"
)

// Downloads from a link into the current folder. The root is resolved with
// the account's current rights like every file request; accounts with the
// role "user" only reach public addresses, owners and administrators also
// their home network. Not part of the assistant's API catalog: a model that
// reads web pages must not make the NAS fetch what a page tells it to.

// handleFileDownloadProbe asks a link for its file name and size.
func (s *Server) handleFileDownloadProbe(w http.ResponseWriter, r *http.Request) {
	var req struct{ Root, Dir, URL string }
	if !readJSON(w, r, &req) {
		return
	}
	root, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	probe, err := s.opts.Files.ProbeDownload(ctx, root, req.Dir, req.URL, !currentUser(r).Role.CanAdminister())
	if err != nil {
		writeDownloadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, probe)
}

// handleFileURLDownload starts a download job.
func (s *Server) handleFileURLDownload(w http.ResponseWriter, r *http.Request) {
	if s.opts.Maintenance != nil {
		leave, ok := s.admitOperation(w)
		if !ok {
			return
		}
		defer leave()
	}
	var req struct {
		Root, Dir, URL, Name, SHA256, Conflict string
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, filePlace(req.Root, path.Join(req.Dir, req.Name)), urlPlace(req.URL))
	root, ok := s.fileRoot(w, r, req.Root)
	if !ok {
		return
	}
	user := currentUser(r)
	job, err := s.opts.Files.StartDownload(root, files.DownloadRequest{URL: req.URL, Dir: req.Dir, Name: req.Name, SHA256: req.SHA256, Conflict: req.Conflict}, user.Name, user.UID, !user.Role.CanAdminister())
	if err != nil {
		writeDownloadError(w, err)
		return
	}
	slog.Info("download from link started", "to", root.ID, "source", job.From, "by", user.Name, "ip", remoteIP(r))
	writeJSON(w, http.StatusAccepted, job)
}

// writeDownloadError answers transport failures of a probe as bad gateway;
// everything else like other file errors.
func writeDownloadError(w http.ResponseWriter, err error) {
	for _, known := range fileErrors {
		if errors.Is(err, known.err) {
			writeFileError(w, err)
			return
		}
	}
	writeError(w, http.StatusBadGateway, "download_failed", err.Error())
}
