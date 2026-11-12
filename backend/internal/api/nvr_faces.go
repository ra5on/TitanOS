package api

// The library of faces and the plates the NVR has read. Pictures of
// faces are personal data: owners and administrators only, never in a
// log, never in the diagnostic bundle.

import (
	"io"
	"log/slog"
	"mime"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/nvr"
)

func (a nvrArea) registerFaces(mux *routeMux, g guards) {
	mux.HandleFunc("GET /api/v1/nvr/faces", g.admin(a.handleFaces))
	mux.HandleFunc("POST /api/v1/nvr/faces", g.admin(a.handleFaceCreate))
	mux.HandleFunc("PUT /api/v1/nvr/faces/{name}", g.admin(a.handleFaceRename))
	mux.HandleFunc("DELETE /api/v1/nvr/faces/{name}", g.admin(a.handleFaceDelete))
	mux.HandleFunc("POST /api/v1/nvr/faces/{name}/images", g.admin(a.handleFaceImageAdd))
	mux.HandleFunc("GET /api/v1/nvr/faces/{name}/images/{file}", g.admin(a.handleFaceImage))
	mux.HandleFunc("DELETE /api/v1/nvr/faces/{name}/images/{file}", g.admin(a.handleFaceImageDelete))
	mux.HandleFunc("GET /api/v1/nvr/face-attempts/{file}", g.admin(a.handleFaceAttempt))
	mux.HandleFunc("POST /api/v1/nvr/face-attempts/{file}/assign", g.admin(a.handleFaceAttemptAssign))
	mux.HandleFunc("DELETE /api/v1/nvr/face-attempts/{file}", g.admin(a.handleFaceAttemptDelete))
	mux.HandleFunc("GET /api/v1/nvr/plates/recognized", g.admin(a.handlePlates))
}

func (a nvrArea) handleFaces(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	faces, err := a.nvr.Faces(r.Context())
	if err != nil {
		writeNVRError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, faces)
}

// faceDone answers a change of the library: the library as it is now.
func (a nvrArea) faceDone(w http.ResponseWriter, r *http.Request, status int, what string, err error) {
	if err != nil {
		writeNVRError(w, err)
		return
	}
	noteTarget(r, "nvr")
	// Never the name of the person: who is in the library is nobody
	// else's business.
	slog.Info("NVR faces changed", "what", what, "by", currentUser(r).Name, "ip", remoteIP(r))
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	faces, err := a.nvr.Faces(r.Context())
	if err != nil {
		writeNVRError(w, err)
		return
	}
	writeJSON(w, status, faces)
}

func (a nvrArea) handleFaceCreate(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	a.faceDone(w, r, http.StatusCreated, "person added", a.nvr.CreateFace(r.Context(), req.Name))
}

func (a nvrArea) handleFaceRename(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	a.faceDone(w, r, http.StatusOK, "person renamed", a.nvr.RenameFace(r.Context(), r.PathValue("name"), req.Name))
}

func (a nvrArea) handleFaceDelete(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	a.faceDone(w, r, http.StatusNoContent, "person deleted", a.nvr.DeleteFace(r.Context(), r.PathValue("name"), ""))
}

// handleFaceImageAdd takes one picture as the body of the request.
func (a nvrArea) handleFaceImageAdd(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	image, err := io.ReadAll(http.MaxBytesReader(w, r.Body, nvr.MaxFaceImage+1))
	if err != nil || len(image) > nvr.MaxFaceImage {
		writeError(w, http.StatusRequestEntityTooLarge, "nvr_invalid", "the picture is larger than 8 MB")
		return
	}
	a.faceDone(w, r, http.StatusCreated, "picture added", a.nvr.AddFaceImage(r.Context(), r.PathValue("name"), contentType, image))
}

func (a nvrArea) faceImage(w http.ResponseWriter, r *http.Request, person string) {
	if !a.available(w) {
		return
	}
	path, err := a.nvr.FaceImagePath(r.Context(), person, r.PathValue("file"))
	if err != nil {
		writeNVRError(w, err)
		return
	}
	a.passMedia(w, r, path, "", "image/", "")
}

func (a nvrArea) handleFaceImage(w http.ResponseWriter, r *http.Request) {
	a.faceImage(w, r, r.PathValue("name"))
}

func (a nvrArea) handleFaceImageDelete(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	a.faceDone(w, r, http.StatusNoContent, "picture deleted", a.nvr.DeleteFace(r.Context(), r.PathValue("name"), r.PathValue("file")))
}

func (a nvrArea) handleFaceAttempt(w http.ResponseWriter, r *http.Request) {
	a.faceImage(w, r, "train")
}

// handleFaceAttemptAssign gives an unassigned face to a person: {name}.
func (a nvrArea) handleFaceAttemptAssign(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	a.faceDone(w, r, http.StatusOK, "face assigned", a.nvr.AssignAttempt(r.Context(), r.PathValue("file"), req.Name))
}

func (a nvrArea) handleFaceAttemptDelete(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	a.faceDone(w, r, http.StatusNoContent, "face discarded", a.nvr.DeleteFace(r.Context(), "train", r.PathValue("file")))
}

func (a nvrArea) handlePlates(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	plates, err := a.nvr.RecognizedPlates(r.Context())
	if err != nil {
		writeNVRError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plates": plates})
}
