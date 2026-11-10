package api

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/ra5on/TitanOS/backend/internal/media"
)

// mediaArea is the import of VM media; an area with narrow dependencies
// (PLAN 8a, see fansArea). admitOperation admits a job unless maintenance
// runs; it answers itself when it refuses.
type mediaArea struct {
	media          *media.Manager
	admitOperation admitFunc
}

func (a mediaArea) register(mux *routeMux, admin, appAdmin, heavyAppAdmin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/vms/imports", admin(a.handleMediaImports))
	mux.HandleFunc("POST /api/v1/vms/imports", heavyAppAdmin(a.handleMediaImport))
	mux.HandleFunc("POST /api/v1/vms/virtio", heavyAppAdmin(a.handleVirtIO))
	mux.HandleFunc("PUT /api/v1/vms/imports/{id}/data", heavyAppAdmin(a.handleMediaChunk))
	mux.HandleFunc("POST /api/v1/vms/imports/{id}/verify", heavyAppAdmin(a.handleMediaUploadVerify))
	mux.HandleFunc("POST /api/v1/vms/imports/{id}/{action}", appAdmin(a.handleMediaControl))
}

func (a mediaArea) mediaAvailable(w http.ResponseWriter) bool {
	if a.media == nil {
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
func (a mediaArea) handleMediaImports(w http.ResponseWriter, r *http.Request) {
	if !a.mediaAvailable(w) {
		return
	}
	writeJSON(w, 200, map[string]any{"jobs": a.media.List(), "chunkBytes": media.ChunkBytes})
}
func (a mediaArea) handleMediaImport(w http.ResponseWriter, r *http.Request) {
	if !a.mediaAvailable(w) {
		return
	}
	var req media.Request
	if !readJSON(w, r, &req) {
		return
	}
	job, err := a.media.Create(req)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	writeJSON(w, 202, job)
}
func (a mediaArea) handleVirtIO(w http.ResponseWriter, r *http.Request) {
	if !a.mediaAvailable(w) {
		return
	}
	var req struct {
		Volume string `json:"volume"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	job, err := a.media.VirtIO(req.Volume)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	writeJSON(w, 202, job)
}
func (a mediaArea) handleMediaChunk(w http.ResponseWriter, r *http.Request) {
	if !a.mediaAvailable(w) {
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
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "the chunk is larger than allowed")
			return
		}
		writeMediaError(w, err)
		return
	}
	if err = a.media.Upload(r.Context(), r.PathValue("id"), offset, data); err != nil {
		writeMediaError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (a mediaArea) handleMediaControl(w http.ResponseWriter, r *http.Request) {
	if !a.mediaAvailable(w) {
		return
	}
	action := r.PathValue("action")
	if action == "resume" || action == "retry" || action == "finish" || action == "review" {
		leave, ok := a.admitOperation(w)
		if !ok {
			return
		}
		defer leave()
	}
	var err error
	if action == "finish" {
		err = a.media.FinishUpload(r.PathValue("id"))
	} else {
		err = a.media.Control(r.PathValue("id"), action)
	}
	if err != nil {
		writeMediaError(w, err)
		return
	}
	a.handleMediaImports(w, r)
}

func (a mediaArea) handleMediaUploadVerify(w http.ResponseWriter, r *http.Request) {
	if !a.mediaAvailable(w) {
		return
	}
	var req struct {
		Chunks []media.UploadProof `json:"chunks"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.media.VerifyUpload(r.Context(), r.PathValue("id"), req.Chunks); err != nil {
		writeMediaError(w, err)
		return
	}
	w.WriteHeader(204)
}
