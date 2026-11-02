package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/nvr"
)

// NVR (camera recorder), see docs/rules/NVR.md. Administrators only: the
// pictures and the search for cameras are not for everyone.

type nvrCameraView struct {
	nvr.Camera
	Status nvr.CameraStatus `json:"status"`
	// Segments and Bytes describe the recordings on disk.
	Segments int   `json:"segments"`
	Bytes    int64 `json:"bytes"`
	Latest   int64 `json:"latest,omitempty"`
}

type nvrView struct {
	Settings  nvr.Settings    `json:"settings"`
	Volumes   []string        `json:"volumes"`
	Cameras   []nvrCameraView `json:"cameras"`
	Root      string          `json:"root,omitempty"`
	FreeBytes int64           `json:"freeBytes"`
	UsedBytes int64           `json:"usedBytes"`
	Vendors   []string        `json:"vendors"`
}

func (s *Server) nvrOK(w http.ResponseWriter) bool {
	if s.opts.NVR == nil {
		writeError(w, http.StatusNotFound, "nvr_unavailable", "the NVR is not available")
		return false
	}
	return true
}

func (s *Server) nvrState() nvrView {
	m := s.opts.NVR
	st := m.Store.Settings()
	view := nvrView{Settings: st, Volumes: s.opts.Storage.Mounted(), Cameras: []nvrCameraView{}}
	for _, v := range nvr.Vendors {
		view.Vendors = append(view.Vendors, v.Name)
	}
	root := m.Root()
	if root != "" {
		view.Root = filepath.Base(filepath.Dir(root))
		if free, err := nvr.FreeBytes(filepath.Dir(root)); err == nil {
			view.FreeBytes = free
		}
	}
	status := m.Rec.Status()
	for _, c := range m.Store.Cameras() {
		cv := nvrCameraView{Camera: c, Status: status[c.ID]}
		if root != "" {
			segs := nvr.Segments(root, c.ID)
			cv.Segments = len(segs)
			for _, sg := range segs {
				cv.Bytes += sg.Size
			}
			if n := len(segs); n > 0 {
				cv.Latest = segs[n-1].Start.Unix()
			}
		}
		view.UsedBytes += cv.Bytes
		view.Cameras = append(view.Cameras, cv)
	}
	return view
}

func (s *Server) handleNVR(w http.ResponseWriter, r *http.Request) {
	if s.nvrOK(w) {
		writeJSON(w, http.StatusOK, s.nvrState())
	}
}

func (s *Server) handleNVRSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Volume    string `json:"volume"`
		KeepDays  int    `json:"keepDays"`
		ReserveGB int    `json:"reserveGB"`
	}
	if !s.nvrOK(w) || !readJSON(w, r, &req) {
		return
	}
	cur := s.opts.NVR.Store.Settings()
	if req.Volume != "" && !contains(s.opts.Storage.Mounted(), req.Volume) {
		writeError(w, http.StatusBadRequest, "invalid_nvr", "the volume is not mounted")
		return
	}
	cur.Volume, cur.KeepDays, cur.ReserveGB = req.Volume, req.KeepDays, req.ReserveGB
	if err := s.opts.NVR.Store.SetSettings(cur); err != nil {
		s.nvrError(w, err)
		return
	}
	noteTarget(r, req.Volume)
	s.opts.NVR.Reload()
	writeJSON(w, http.StatusOK, s.nvrState())
}

func (s *Server) nvrError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, nvr.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_nvr", err.Error())
	case errors.Is(err, nvr.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "no such camera")
	case errors.Is(err, nvr.ErrTargetNotAllowed):
		writeError(w, http.StatusBadRequest, "nvr_target_not_allowed", err.Error())
	default:
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
	}
}

// handleNVRDiscover searches one camera. It never answers with a password.
func (s *Server) handleNVRDiscover(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host      string `json:"host"`
		User      string `json:"user"`
		Password  string `json:"password"`
		StreamURL string `json:"streamUrl"`
		Vendor    string `json:"vendor"`
	}
	if !s.nvrOK(w) || !readJSON(w, r, &req) {
		return
	}
	if req.User == "" {
		writeError(w, http.StatusBadRequest, "invalid_nvr", "user name missing")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	res, err := s.opts.NVR.Prober.Discover(ctx, nvr.Request{Host: strings.TrimSpace(req.Host), User: req.User, Password: req.Password, StreamURL: strings.TrimSpace(req.StreamURL), Vendor: req.Vendor})
	if err != nil {
		s.nvrError(w, err)
		return
	}
	noteTarget(r, req.Host)
	slog.Info("nvr: camera search", "by", currentUser(r).Name, "found", res.Found, "rejected", res.Rejected, "locked", res.Locked)
	writeJSON(w, http.StatusOK, res)
}

type nvrCameraRequest struct {
	nvr.Camera
	Password *string `json:"password"`
}

func (s *Server) handleNVRCameraAdd(w http.ResponseWriter, r *http.Request) {
	var req nvrCameraRequest
	if !s.nvrOK(w) || !readJSON(w, r, &req) {
		return
	}
	if err := s.checkCameraHost(r.Context(), req.Camera); err != nil {
		s.nvrError(w, err)
		return
	}
	pw := ""
	if req.Password != nil {
		pw = *req.Password
	}
	c, err := s.opts.NVR.Store.Add(req.Camera, pw)
	if err != nil {
		s.nvrError(w, err)
		return
	}
	noteTarget(r, c.Name)
	s.opts.NVR.Reload()
	writeJSON(w, http.StatusCreated, s.nvrState())
}

func (s *Server) handleNVRCameraSet(w http.ResponseWriter, r *http.Request) {
	var req nvrCameraRequest
	if !s.nvrOK(w) || !readJSON(w, r, &req) {
		return
	}
	req.ID = r.PathValue("id")
	if err := s.checkCameraHost(r.Context(), req.Camera); err != nil {
		s.nvrError(w, err)
		return
	}
	c, err := s.opts.NVR.Store.Update(req.Camera, req.Password)
	if err != nil {
		s.nvrError(w, err)
		return
	}
	noteTarget(r, c.Name)
	s.opts.NVR.Reload()
	writeJSON(w, http.StatusOK, s.nvrState())
}

func (s *Server) handleNVRCameraRemove(w http.ResponseWriter, r *http.Request) {
	if !s.nvrOK(w) {
		return
	}
	id := r.PathValue("id")
	if c, ok := s.opts.NVR.Store.Camera(id); ok {
		noteTarget(r, c.Name)
	}
	if err := s.opts.NVR.Store.Remove(id); err != nil {
		s.nvrError(w, err)
		return
	}
	s.opts.NVR.Reload()
	writeJSON(w, http.StatusOK, s.nvrState())
}

// checkCameraHost lets only home-network addresses in: the recorder
// connects to whatever is stored here.
func (s *Server) checkCameraHost(ctx context.Context, c nvr.Camera) error {
	for _, raw := range []string{c.MainURL, c.SubURL} {
		if raw == "" {
			continue
		}
		host, _, _, err := nvr.AddressOf(raw)
		if err != nil {
			return fmt.Errorf("%w: stream address", nvr.ErrInvalid)
		}
		if _, err := nvr.Resolve(ctx, nil, host); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) nvrCamera(w http.ResponseWriter, r *http.Request) (nvr.Camera, bool) {
	if !s.nvrOK(w) {
		return nvr.Camera{}, false
	}
	c, ok := s.opts.NVR.Store.Camera(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such camera")
	}
	return c, ok
}

func (s *Server) handleNVRSnapshot(w http.ResponseWriter, r *http.Request) {
	c, ok := s.nvrCamera(w, r)
	if !ok {
		return
	}
	data, err := s.opts.NVR.Rec.Snapshot(r.Context(), c)
	if err != nil {
		writeError(w, http.StatusBadGateway, "nvr_no_picture", "no picture from the camera")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (s *Server) handleNVRLive(w http.ResponseWriter, r *http.Request) {
	c, ok := s.nvrCamera(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "multipart/x-mixed-replace;boundary="+nvr.LiveBoundary)
	w.Header().Set("Cache-Control", "no-store")
	fw := flushWriter{w: w, f: w.(http.Flusher)}
	if err := s.opts.NVR.Rec.Live(r.Context(), c, fw); errors.Is(err, nvr.ErrTooManyLive) {
		writeError(w, http.StatusTooManyRequests, "nvr_too_many_live", "too many live views")
	}
}

type flushWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	f.f.Flush()
	return n, err
}

var nvrDay = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
var nvrFile = regexp.MustCompile(`^\d{2}-\d{2}-\d{2}\.mp4$`)

// handleNVRRecordings lists a camera's recordings of one day, or the days
// that have any when no day is given.
func (s *Server) handleNVRRecordings(w http.ResponseWriter, r *http.Request) {
	c, ok := s.nvrCamera(w, r)
	if !ok {
		return
	}
	root := s.opts.NVR.Root()
	out := struct {
		Days     []string      `json:"days"`
		Segments []nvr.Segment `json:"segments"`
	}{Days: []string{}, Segments: []nvr.Segment{}}
	if root != "" {
		day := r.URL.Query().Get("day")
		if day != "" && !nvrDay.MatchString(day) {
			writeError(w, http.StatusBadRequest, "invalid_nvr", "day")
			return
		}
		seen := map[string]bool{}
		for _, sg := range nvr.Segments(root, c.ID) {
			d := strings.SplitN(sg.Name, "/", 2)[0]
			if !seen[d] {
				seen[d] = true
				out.Days = append(out.Days, d)
			}
			if d == day {
				out.Segments = append(out.Segments, sg)
			}
		}
		sort.Sort(sort.Reverse(sort.StringSlice(out.Days)))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleNVRRecording serves one file (with ranges, so it can be skipped through).
func (s *Server) handleNVRRecording(w http.ResponseWriter, r *http.Request) {
	c, ok := s.nvrCamera(w, r)
	if !ok {
		return
	}
	day, file := r.PathValue("day"), r.PathValue("file")
	root := s.opts.NVR.Root()
	if root == "" || !nvrDay.MatchString(day) || !nvrFile.MatchString(file) {
		writeError(w, http.StatusNotFound, "not_found", "no such recording")
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeFile(w, r, filepath.Join(root, c.ID, day, file))
}

func (s *Server) registerNVR(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/nvr", s.requireAdmin(s.handleNVR))
	mux.HandleFunc("PUT /api/v1/nvr/settings", s.requireAdmin(s.handleNVRSettings))
	mux.HandleFunc("POST /api/v1/nvr/discover", s.requireAdmin(s.handleNVRDiscover))
	mux.HandleFunc("POST /api/v1/nvr/cameras", s.requireAdmin(s.handleNVRCameraAdd))
	mux.HandleFunc("PUT /api/v1/nvr/cameras/{id}", s.requireAdmin(s.handleNVRCameraSet))
	mux.HandleFunc("DELETE /api/v1/nvr/cameras/{id}", s.requireAdmin(s.handleNVRCameraRemove))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/snapshot", s.requireAdmin(s.handleNVRSnapshot))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/live", s.requireAdmin(s.handleNVRLive))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/recordings", s.requireAdmin(s.handleNVRRecordings))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/recordings/{day}/{file}", s.requireAdmin(s.handleNVRRecording))
}
