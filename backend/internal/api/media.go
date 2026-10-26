package api

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/ra5on/TitanOS/backend/internal/media"
)

func (s *Server) mediaAvailable(w http.ResponseWriter) bool {
	if s.opts.Media == nil {
		writeError(w, 503, "media_unavailable", "VM media imports unavailable")
		return false
	}
	return true
}
func writeMediaError(w http.ResponseWriter, err error) {
	status := 400
	code := "media_failed"
	if errors.Is(err, media.ErrBusy) {
		status = 409
		code = "media_busy"
	} else if errors.Is(err, media.ErrUnavailable) {
		status = 409
		code = "media_volume_unavailable"
	} else if errors.Is(err, os.ErrNotExist) {
		status = 404
		code = "media_not_found"
	}
	writeError(w, status, code, err.Error())
}
func (s *Server) handleMediaImports(w http.ResponseWriter, r *http.Request) {
	if !s.mediaAvailable(w) {
		return
	}
	writeJSON(w, 200, map[string]any{"jobs": s.opts.Media.List(), "chunkBytes": media.ChunkBytes})
}
func (s *Server) handleMediaImport(w http.ResponseWriter, r *http.Request) {
	if !s.mediaAvailable(w) {
		return
	}
	var req media.Request
	if !readJSON(w, r, &req) {
		return
	}
	job, err := s.opts.Media.Create(req)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	writeJSON(w, 202, job)
}
func (s *Server) handleVirtIO(w http.ResponseWriter, r *http.Request) {
	if !s.mediaAvailable(w) {
		return
	}
	var req struct {
		Volume string `json:"volume"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	job, err := s.opts.Media.VirtIO(req.Volume)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	writeJSON(w, 202, job)
}
func (s *Server) handleMediaChunk(w http.ResponseWriter, r *http.Request) {
	if !s.mediaAvailable(w) {
		return
	}
	offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if err != nil {
		writeMediaError(w, media.ErrInvalid)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, media.ChunkBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	if err = s.opts.Media.Upload(r.Context(), r.PathValue("id"), offset, data); err != nil {
		writeMediaError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) handleMediaControl(w http.ResponseWriter, r *http.Request) {
	if !s.mediaAvailable(w) {
		return
	}
	action := r.PathValue("action")
	if action == "resume" || action == "retry" || action == "finish" || action == "review" {
		leave, ok := s.admitOperation(w)
		if !ok {
			return
		}
		defer leave()
	}
	var err error
	if action == "finish" {
		err = s.opts.Media.FinishUpload(r.PathValue("id"))
	} else {
		err = s.opts.Media.Control(r.PathValue("id"), action)
	}
	if err != nil {
		writeMediaError(w, err)
		return
	}
	s.handleMediaImports(w, r)
}

func (s *Server) handleMediaUploadVerify(w http.ResponseWriter, r *http.Request) {
	if !s.mediaAvailable(w) {
		return
	}
	var req struct {
		Chunks []media.UploadProof `json:"chunks"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Media.VerifyUpload(r.Context(), r.PathValue("id"), req.Chunks); err != nil {
		writeMediaError(w, err)
		return
	}
	w.WriteHeader(204)
}
