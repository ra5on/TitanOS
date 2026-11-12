package api

// Recordings of the NVR: which hours have any, the segments for the
// timeline, playback (HLS) and a still at a time. Camera right
// "recordings".

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/ra5on/TitanOS/backend/internal/nvr"
)

func (a nvrArea) registerRecordings(mux *routeMux, g guards) {
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/recordings/summary", g.user(a.handleRecordingsSummary))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/recordings", g.user(a.handleRecordings))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/recordings/frame.jpg", g.user(a.handleRecordingFrame))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/vod/{from}/{to}/{file}", g.user(a.handleVOD))
}

// recorded returns the camera of the request if the account may see its
// recordings; it answers the error itself otherwise.
func (a nvrArea) recorded(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !a.available(w) {
		return "", false
	}
	id := r.PathValue("id")
	if _, ok := a.nvr.Camera(id); !ok || !a.allows(currentUser(r), id, nvr.RightRecordings) {
		writeNVRError(w, nvr.ErrNotFound)
		return "", false
	}
	return id, true
}

func (a nvrArea) handleRecordingsSummary(w http.ResponseWriter, r *http.Request) {
	id, ok := a.recorded(w, r)
	if !ok {
		return
	}
	days, err := a.nvr.RecordingsSummary(r.Context(), id)
	if err != nil {
		writeNVRError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": days})
}

func (a nvrArea) handleRecordings(w http.ResponseWriter, r *http.Request) {
	id, ok := a.recorded(w, r)
	if !ok {
		return
	}
	from, ok1 := queryTime(r, "from")
	to, ok2 := queryTime(r, "to")
	if !ok1 || !ok2 {
		writeError(w, http.StatusBadRequest, "nvr_invalid", "from, to: Unix seconds")
		return
	}
	segments, err := a.nvr.Segments(r.Context(), id, from, to)
	if err != nil {
		writeNVRError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "segments": segments})
}

// handleRecordingFrame answers the picture recorded at ?at= (JPEG).
func (a nvrArea) handleRecordingFrame(w http.ResponseWriter, r *http.Request) {
	id, ok := a.recorded(w, r)
	if !ok {
		return
	}
	at, valid := queryTime(r, "at")
	if !valid || at <= 0 {
		writeError(w, http.StatusBadRequest, "nvr_invalid", "at: Unix seconds")
		return
	}
	query := url.Values{}
	if raw := r.URL.Query().Get("height"); raw != "" {
		height, err := strconv.Atoi(raw)
		if err != nil || height < 90 || height > 2160 {
			writeError(w, http.StatusBadRequest, "nvr_invalid", "height: 90 to 2160")
			return
		}
		query.Set("height", raw)
	}
	a.passMedia(w, r, "/api/"+url.PathEscape(id)+"/recordings/"+strconv.FormatFloat(at, 'f', 3, 64)+"/snapshot.jpg", query.Encode(), "image/", "")
}

var (
	// vodTimeRE: whole Unix seconds.
	vodTimeRE = regexp.MustCompile(`^[0-9]{1,11}$`)
	// vodFileRE: the files of a playback, as Frigate's web server names
	// them (master.m3u8, index-v1.m3u8, init-v1.mp4, seg-3-v1.m4s). One
	// path component, nothing that could leave it.
	vodFileRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}\.(m3u8|mp4|m4s|ts)$`)
)

// handleVOD passes the playback of a camera's recordings between two
// times through (HLS). The playlists name their files relative to
// themselves, so nothing in them is rewritten: every file the player asks
// for comes through this route again, with the camera of the path and so
// with its right. The path to Frigate is put together from the camera's
// ID (a camera that exists), two numbers and one file name of a fixed
// form; nothing of the request reaches Frigate as it came.
func (a nvrArea) handleVOD(w http.ResponseWriter, r *http.Request) {
	id, ok := a.recorded(w, r)
	if !ok {
		return
	}
	from, to, file := r.PathValue("from"), r.PathValue("to"), r.PathValue("file")
	if !vodTimeRE.MatchString(from) || !vodTimeRE.MatchString(to) || !vodFileRE.MatchString(file) {
		writeError(w, http.StatusBadRequest, "nvr_invalid", "playback address")
		return
	}
	start, _ := strconv.ParseInt(from, 10, 64) // digits only, checked above
	end, _ := strconv.ParseInt(to, 10, 64)
	if end <= start || end-start > nvr.MaxExportSeconds {
		writeError(w, http.StatusBadRequest, "nvr_invalid", "from/to: at most 24 hours")
		return
	}
	a.passMedia(w, r, "/vod/"+url.PathEscape(id)+"/start/"+from+"/end/"+to+"/"+file, "", "", "")
}
