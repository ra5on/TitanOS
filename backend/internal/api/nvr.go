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
	Watch  nvr.WatchStatus  `json:"watch"`
	// Segments and Bytes describe the recordings on disk.
	Segments int   `json:"segments"`
	Bytes    int64 `json:"bytes"`
	Latest   int64 `json:"latest,omitempty"`
	// DayBytes is what the last 24 hours took (measured, for the estimate).
	DayBytes int64 `json:"dayBytes"`
}

type nvrView struct {
	Settings  nvr.Settings    `json:"settings"`
	Volumes   []string        `json:"volumes"`
	Cameras   []nvrCameraView `json:"cameras"`
	Root      string          `json:"root,omitempty"`
	FreeBytes int64           `json:"freeBytes"`
	UsedBytes int64           `json:"usedBytes"`
	Vendors   []string        `json:"vendors"`
	Views     []nvr.View      `json:"views"`
	// ArmedNow says whether movement is reported at this moment.
	ArmedNow bool `json:"armedNow"`
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
	view := nvrView{Settings: st, Volumes: s.opts.Storage.Mounted(), Cameras: []nvrCameraView{}, Views: m.Store.Views()}
	if view.Views == nil {
		view.Views = []nvr.View{}
	}
	view.ArmedNow = st.Arming.Active(time.Now())
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
	status, watch := m.Rec.Status(), m.Watch.Status()
	cams := m.Store.Cameras()
	var usage map[string]nvr.Usage
	if root != "" {
		usage = nvr.CameraUsage(root, cams, time.Now())
	}
	for _, c := range cams {
		cv := nvrCameraView{Camera: c, Status: status[c.ID], Watch: watch[c.ID]}
		if root != "" {
			u := usage[c.ID]
			cv.Segments, cv.Bytes, cv.DayBytes = u.Segments, u.Bytes, u.DayBytes
			if !u.Latest.IsZero() {
				cv.Latest = u.Latest.Unix()
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
		Volume        string `json:"volume"`
		KeepDays      int    `json:"keepDays"`
		EventKeepDays int    `json:"eventKeepDays"`
		ReserveGB     int    `json:"reserveGB"`
	}
	if !s.nvrOK(w) || !readJSON(w, r, &req) {
		return
	}
	cur := s.opts.NVR.Store.Settings()
	if req.Volume != "" && !contains(s.opts.Storage.Mounted(), req.Volume) {
		writeError(w, http.StatusBadRequest, "invalid_nvr", "the volume is not mounted")
		return
	}
	cur.Volume, cur.KeepDays, cur.EventKeepDays, cur.ReserveGB = req.Volume, req.KeepDays, req.EventKeepDays, req.ReserveGB
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
		// Camera, when editing: an empty password then means the stored
		// one, never an empty login (which would only spend the attempts).
		Camera string `json:"camera"`
	}
	if !s.nvrOK(w) || !readJSON(w, r, &req) {
		return
	}
	if req.Password == "" && req.Camera != "" {
		if _, ok := s.opts.NVR.Store.Camera(req.Camera); ok {
			req.Password = s.opts.NVR.Store.Password(req.Camera)
		}
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
	// New password or address: try again at once instead of after the pause.
	s.opts.NVR.Rec.ClearLogin(c.ID)
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
	if errors.Is(err, nvr.ErrLoginPaused) {
		writeError(w, http.StatusBadGateway, "nvr_login_refused", err.Error())
		return
	}
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
var nvrEventFile = regexp.MustCompile(`^\d{2}-\d{2}-\d{2}\.jpg$`)
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
		Days      []string      `json:"days"`
		Segments  []nvr.Segment `json:"segments"`
		Protected []nvr.Segment `json:"protected"`
	}{Days: []string{}, Segments: []nvr.Segment{}, Protected: []nvr.Segment{}}
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
		out.Protected = nvr.Protected(root, c.ID)
		if out.Protected == nil {
			out.Protected = []nvr.Segment{}
		}
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
	if r.URL.Query().Get("protected") == "1" {
		http.ServeFile(w, r, filepath.Join(root, c.ID, nvr.ProtectedDir, day, file))
		return
	}
	http.ServeFile(w, r, filepath.Join(root, c.ID, day, file))
}

// handleNVRProtect keeps one recording from being deleted by retention;
// DELETE releases it again.
func (s *Server) handleNVRProtect(w http.ResponseWriter, r *http.Request) {
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
	var err error
	if r.Method == http.MethodDelete {
		err = nvr.Unprotect(root, c.ID, day, file)
	} else {
		err = nvr.Protect(root, c.ID, day, file)
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such recording")
		return
	}
	noteTarget(r, c.Name, day+"/"+file)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleNVREvents lists the latest events of a camera.
func (s *Server) handleNVREvents(w http.ResponseWriter, r *http.Request) {
	c, ok := s.nvrCamera(w, r)
	if !ok {
		return
	}
	events := []nvr.Event{}
	if root := s.opts.NVR.Root(); root != "" {
		events = nvr.Events(root, c.ID, 50)
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

// handleNVREventPicture serves the picture of an event.
func (s *Server) handleNVREventPicture(w http.ResponseWriter, r *http.Request) {
	c, ok := s.nvrCamera(w, r)
	if !ok {
		return
	}
	day, file := r.PathValue("day"), r.PathValue("file")
	root := s.opts.NVR.Root()
	if root == "" || !nvrDay.MatchString(day) || !nvrEventFile.MatchString(file) {
		writeError(w, http.StatusNotFound, "not_found", "no such picture")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, nvr.SnapshotPath(root, c.ID, day, file))
}

// handleNVRArming sets when movement is reported (Settings › Notifications).
func (s *Server) handleNVRArming(w http.ResponseWriter, r *http.Request) {
	var req nvr.Arming
	if !s.nvrOK(w) || !readJSON(w, r, &req) {
		return
	}
	cur := s.opts.NVR.Store.Settings()
	cur.Arming = req
	if err := s.opts.NVR.Store.SetSettings(cur); err != nil {
		s.nvrError(w, err)
		return
	}
	noteTarget(r, req.Mode)
	writeJSON(w, http.StatusOK, s.nvrState())
}

func (s *Server) handleNVRViews(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Views []nvr.View `json:"views"`
	}
	if !s.nvrOK(w) || !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.NVR.Store.SetViews(req.Views); err != nil {
		s.nvrError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.nvrState())
}

// nvrQualityCall runs a video-settings call for a camera and answers errors.
func (s *Server) nvrQualityTarget(w http.ResponseWriter, r *http.Request) (nvr.Camera, string, bool) {
	c, ok := s.nvrCamera(w, r)
	if !ok {
		return c, "", false
	}
	host, _, _, err := nvr.AddressOf(c.MainURL)
	if err != nil {
		s.nvrError(w, nvr.ErrInvalid)
		return c, "", false
	}
	return c, host, true
}

func (s *Server) nvrQualityError(w http.ResponseWriter, o nvr.Outcome, err error) {
	switch {
	case o == nvr.OutcomeLocked:
		writeError(w, http.StatusBadGateway, "nvr_locked", "the camera reports a locked account")
	case o == nvr.OutcomeUnauthorized:
		writeError(w, http.StatusBadGateway, "nvr_login_refused", "the camera refused the login")
	case errors.Is(err, nvr.ErrOutsideOptions):
		writeError(w, http.StatusBadRequest, "invalid_nvr", err.Error())
	case errors.Is(err, nvr.ErrQualityUnsupported):
		writeError(w, http.StatusBadGateway, "nvr_quality_unsupported", err.Error())
	default:
		s.nvrError(w, err)
	}
}

func (s *Server) handleNVRQuality(w http.ResponseWriter, r *http.Request) {
	c, host, ok := s.nvrQualityTarget(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	list, o, err := s.opts.NVR.Prober.ReadQuality(ctx, host, c.ONVIFPort, c.User, s.opts.NVR.Store.Password(c.ID))
	if err != nil {
		s.nvrQualityError(w, o, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"streams": list, "original": c.QualityOriginal})
}

// handleNVRQualitySet changes the video setting of one stream. The page
// asks first; the original setting is kept for "reset".
func (s *Server) handleNVRQualitySet(w http.ResponseWriter, r *http.Request) {
	var change nvr.QualityChange
	if !s.nvrOK(w) || !readJSON(w, r, &change) {
		return
	}
	c, host, ok := s.nvrQualityTarget(w, r)
	if !ok {
		return
	}
	m := s.opts.NVR
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	password := m.Store.Password(c.ID)
	if _, kept := c.QualityOriginal[change.Token]; !kept {
		list, o, err := m.Prober.ReadQuality(ctx, host, c.ONVIFPort, c.User, password)
		if err != nil {
			s.nvrQualityError(w, o, err)
			return
		}
		for _, q := range list {
			if q.Token == change.Token {
				orig := nvr.QualityChange{Token: q.Token, Width: q.Width, Height: q.Height, FPS: q.FPS, BitrateKb: q.BitrateKb}
				if err := m.Store.SetQualityOriginal(c.ID, q.Token, &orig); err != nil {
					s.nvrError(w, err)
					return
				}
			}
		}
	}
	next, o, err := m.Prober.WriteQuality(ctx, host, c.ONVIFPort, c.User, password, change)
	noteTarget(r, c.Name)
	if err != nil {
		s.nvrQualityError(w, o, err)
		return
	}
	slog.Info("nvr: video setting changed", "camera", c.ID, "stream", change.Token, "by", currentUser(r).Name)
	writeJSON(w, http.StatusOK, next)
}

// handleNVRQualityReset puts a stream back to what it was before TitanOS
// changed it.
func (s *Server) handleNVRQualityReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !s.nvrOK(w) || !readJSON(w, r, &req) {
		return
	}
	c, host, ok := s.nvrQualityTarget(w, r)
	if !ok {
		return
	}
	orig, kept := c.QualityOriginal[req.Token]
	if !kept {
		writeError(w, http.StatusNotFound, "not_found", "nothing to reset")
		return
	}
	m := s.opts.NVR
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	next, o, err := m.Prober.WriteQuality(ctx, host, c.ONVIFPort, c.User, m.Store.Password(c.ID), orig)
	noteTarget(r, c.Name)
	if err != nil {
		s.nvrQualityError(w, o, err)
		return
	}
	if err := m.Store.SetQualityOriginal(c.ID, req.Token, nil); err != nil {
		s.nvrError(w, err)
		return
	}
	slog.Info("nvr: video setting reset", "camera", c.ID, "stream", req.Token, "by", currentUser(r).Name)
	writeJSON(w, http.StatusOK, next)
}

func (s *Server) registerNVR(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/nvr", s.requireAdmin(s.handleNVR))
	mux.HandleFunc("PUT /api/v1/nvr/settings", s.requireAdmin(s.handleNVRSettings))
	mux.HandleFunc("PUT /api/v1/nvr/arming", s.requireAdmin(s.handleNVRArming))
	mux.HandleFunc("PUT /api/v1/nvr/views", s.requireAdmin(s.handleNVRViews))
	mux.HandleFunc("POST /api/v1/nvr/discover", s.requireAdmin(s.handleNVRDiscover))
	mux.HandleFunc("POST /api/v1/nvr/cameras", s.requireAdmin(s.handleNVRCameraAdd))
	mux.HandleFunc("PUT /api/v1/nvr/cameras/{id}", s.requireAdmin(s.handleNVRCameraSet))
	mux.HandleFunc("DELETE /api/v1/nvr/cameras/{id}", s.requireAdmin(s.handleNVRCameraRemove))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/quality", s.requireAdmin(s.handleNVRQuality))
	mux.HandleFunc("PUT /api/v1/nvr/cameras/{id}/quality", s.requireAdmin(s.handleNVRQualitySet))
	mux.HandleFunc("POST /api/v1/nvr/cameras/{id}/quality/reset", s.requireAdmin(s.handleNVRQualityReset))
	mux.HandleFunc("POST /api/v1/nvr/cameras/{id}/time", s.requireAdmin(s.handleNVRTime))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/snapshot", s.requireAdmin(s.handleNVRSnapshot))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/live", s.requireAdmin(s.handleNVRLive))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/events", s.requireAdmin(s.handleNVREvents))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/events/{day}/{file}", s.requireAdmin(s.handleNVREventPicture))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/recordings", s.requireAdmin(s.handleNVRRecordings))
	mux.HandleFunc("POST /api/v1/nvr/cameras/{id}/recordings/{day}/{file}/protect", s.requireAdmin(s.handleNVRProtect))
	mux.HandleFunc("DELETE /api/v1/nvr/cameras/{id}/recordings/{day}/{file}/protect", s.requireAdmin(s.handleNVRProtect))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/recordings/{day}/{file}", s.requireAdmin(s.handleNVRRecording))
}

// handleNVRTime points a camera at the time server of this NAS. The page
// asks for confirmation first; the change shows in the activity log.
func (s *Server) handleNVRTime(w http.ResponseWriter, r *http.Request) {
	c, ok := s.nvrCamera(w, r)
	if !ok {
		return
	}
	m := s.opts.NVR
	host, _, _, err := nvr.AddressOf(c.MainURL)
	if err != nil {
		s.nvrError(w, nvr.ErrInvalid)
		return
	}
	server, err := nvr.LocalAddrFor(host)
	if err != nil {
		writeError(w, http.StatusBadGateway, "nvr_time_failed", "no route to the camera")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	res, err := m.Prober.SetTimeServer(ctx, host, c.ONVIFPort, c.Vendor, c.User, m.Store.Password(c.ID), server)
	noteTarget(r, c.Name)
	if err != nil {
		slog.Info("nvr: camera time not set", "camera", c.ID, "rejected", res.Rejected, "locked", res.Locked)
		code := "nvr_time_failed"
		if res.Locked {
			code = "nvr_locked"
		}
		writeError(w, http.StatusBadGateway, code, err.Error())
		return
	}
	if err := m.Store.SetTimeSync(c.ID, true); err != nil {
		s.nvrError(w, err)
		return
	}
	m.Reload()
	slog.Info("nvr: camera time server set", "camera", c.ID, "method", res.Method, "by", currentUser(r).Name)
	writeJSON(w, http.StatusOK, map[string]any{"method": res.Method, "server": server})
}
