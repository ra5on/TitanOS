package api

// Docker › Images, Volumes and Aufräumen (PLAN 67-1): list, delete and
// clean up images, volumes and the build cache. Administrators only;
// changes wait like other app changes for a running application backup.
// docker/resources.go decides what may go.

import (
	"errors"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/docker"
)

// writeResourceError maps the refusals of images and volumes; kind is
// "image" or "volume".
func writeResourceError(w http.ResponseWriter, kind string, err error) {
	switch {
	case errors.Is(err, docker.ErrInUse):
		writeError(w, http.StatusConflict, kind+"_in_use", err.Error())
	case errors.Is(err, docker.ErrProtected):
		writeError(w, http.StatusConflict, kind+"_protected", err.Error())
	case errors.Is(err, docker.ErrBusy):
		writeError(w, http.StatusConflict, "app_busy", err.Error())
	default:
		writeDockerError(w, err)
	}
}

// readOptionalJSON reads a JSON body if one was sent; an empty body keeps
// the defaults in v.
func readOptionalJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.ContentLength == 0 {
		return true
	}
	return readJSON(w, r, v)
}

func (s *Server) handleDockerImages(w http.ResponseWriter, r *http.Request) {
	list, err := s.opts.Docker.Images(r.Context())
	if err != nil {
		writeResourceError(w, "image", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleDockerImageRemove(w http.ResponseWriter, r *http.Request) {
	image, err := s.opts.Docker.RemoveImage(r.Context(), r.PathValue("id"))
	if err != nil {
		writeResourceError(w, "image", err)
		return
	}
	noteTarget(r, image.RepoTags...)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDockerImagesPrune(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Unused bool `json:"unused"`
	}
	if !readOptionalJSON(w, r, &req) {
		return
	}
	result, err := s.opts.Docker.PruneImages(r.Context(), req.Unused)
	if err != nil {
		writeResourceError(w, "image", err)
		return
	}
	if req.Unused {
		noteTarget(r, "unused")
	} else {
		noteTarget(r, "dangling")
	}
	noteTarget(r, result.Deleted...)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleDockerVolumes(w http.ResponseWriter, r *http.Request) {
	list, err := s.opts.Docker.Volumes(r.Context())
	if err != nil {
		writeResourceError(w, "volume", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"volumes": list})
}

func (s *Server) handleDockerVolumeRemove(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Docker.RemoveVolume(r.Context(), r.PathValue("name")); err != nil {
		writeResourceError(w, "volume", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDockerVolumesPrune(w http.ResponseWriter, r *http.Request) {
	req := struct {
		AnonymousOnly bool `json:"anonymousOnly"`
	}{AnonymousOnly: true}
	if !readOptionalJSON(w, r, &req) {
		return
	}
	result, err := s.opts.Docker.PruneVolumes(r.Context(), req.AnonymousOnly)
	if err != nil {
		writeResourceError(w, "volume", err)
		return
	}
	if req.AnonymousOnly {
		noteTarget(r, "anonymous")
	} else {
		noteTarget(r, "all unused")
	}
	noteTarget(r, result.Deleted...)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleDockerBuilderPrune(w http.ResponseWriter, r *http.Request) {
	result, err := s.opts.Docker.PruneBuildCache(r.Context())
	if err != nil {
		writeResourceError(w, "builder", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reclaimed": result.Reclaimed, "deleted": len(result.Deleted)})
}

func (s *Server) handleDockerDiskUsage(w http.ResponseWriter, r *http.Request) {
	usage, err := s.opts.Docker.DiskUsage(r.Context(), r.URL.Query().Get("fresh") == "1")
	if err != nil {
		writeResourceError(w, "docker", err)
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

// writeBridgeError maps the refusals of the bridge network routes.
func writeBridgeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, docker.ErrOverlap):
		writeError(w, http.StatusConflict, "network_overlap", err.Error())
	case errors.Is(err, docker.ErrExists):
		writeError(w, http.StatusConflict, "network_exists", err.Error())
	case errors.Is(err, docker.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_network", err.Error())
	default:
		writeResourceError(w, "network", err)
	}
}

func (s *Server) handleDockerAllNetworks(w http.ResponseWriter, r *http.Request) {
	list, err := s.opts.Docker.AllNetworks(r.Context())
	if err != nil {
		writeBridgeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"networks": list})
}

func (s *Server) handleDockerBridgeCreate(w http.ResponseWriter, r *http.Request) {
	var req docker.BridgeRequest
	if !readJSON(w, r, &req) {
		return
	}
	n, err := s.opts.Docker.CreateBridge(r.Context(), req)
	if err != nil {
		writeBridgeError(w, err)
		return
	}
	noteTarget(r, req.Name)
	writeJSON(w, http.StatusCreated, n)
}

func (s *Server) handleDockerBridgeRemove(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Docker.RemoveNetwork(r.Context(), r.PathValue("name")); err != nil {
		writeBridgeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDockerNetworksPrune(w http.ResponseWriter, r *http.Request) {
	result, err := s.opts.Docker.PruneNetworks(r.Context())
	if err != nil {
		writeBridgeError(w, err)
		return
	}
	noteTarget(r, result.Deleted...)
	writeJSON(w, http.StatusOK, result)
}
