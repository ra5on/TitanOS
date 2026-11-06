package api

// Remote view of Docker apps (Fernansicht, PLAN 63, docs/FERNANSICHT.md):
// the app runs in a browser on the NAS, the window gets picture and sound
// over one WebSocket through titand. None of these routes is in
// publicRoutes: they are for the web UI only (no tokens, no MCP, no agent).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/viewer"
)

// viewerOpener is the part of viewer.Manager the stream uses; tests fake it.
type viewerOpener interface {
	Open(ctx context.Context, user, app string) (*viewer.Lease, error)
}

// registerViewer registers the routes of the remote view.
func (s *Server) registerViewer(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/viewer/settings", s.requireAdmin(s.handleViewerSettings))
	mux.HandleFunc("PUT /api/v1/viewer/settings", s.requireAdmin(s.handleViewerSettingsSet))
	mux.HandleFunc("GET /api/v1/viewer/status", s.requireUser(s.handleViewerStatus))
	mux.HandleFunc("POST /api/v1/viewer/image", s.requireAdmin(s.handleViewerImage))
	mux.HandleFunc("GET /api/v1/viewer/{app}/stream", s.requireUser(s.handleViewerStream))
	mux.HandleFunc("GET /api/v1/viewer/{app}/downloads/{id}", s.requireUser(s.handleViewerDownload))
}

func (s *Server) handleViewerSettings(w http.ResponseWriter, r *http.Request) {
	if s.opts.Viewer == nil {
		writeJSON(w, http.StatusOK, viewer.Defaults())
		return
	}
	writeJSON(w, http.StatusOK, s.opts.Viewer.Settings())
}

// handleViewerSettingsSet stores {gpu, quality, fps, audio, volume}.
func (s *Server) handleViewerSettingsSet(w http.ResponseWriter, r *http.Request) {
	if s.opts.Viewer == nil {
		writeError(w, http.StatusNotFound, "not_found", "no remote view")
		return
	}
	req := s.opts.Viewer.Settings()
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.Viewer.SetSettings(r.Context(), req); err != nil {
		if errors.Is(err, viewer.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "viewer_settings_invalid", err.Error())
			return
		}
		slog.Error("remote view settings not saved", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not save the settings")
		return
	}
	noteTarget(r, "viewer")
	writeJSON(w, http.StatusOK, s.opts.Viewer.Settings())
}

// viewerStatus is GET /api/v1/viewer/status. Accounts that do not
// administer see the image and their own views only.
type viewerStatus struct {
	Image viewer.ImageStatus `json:"image"`
	// GPU is the encoder a new view would use: vaapi, nvenc or cpu.
	GPU      string               `json:"gpu"`
	GPUs     []viewer.GPU         `json:"gpus"`
	Sessions []viewer.SessionInfo `json:"sessions"`
	Memory   viewer.Memory        `json:"memory"`
	// IdleMinutes is the consoles' idle time that also ends views (0: never).
	IdleMinutes int `json:"idleMinutes"`
}

func (s *Server) handleViewerStatus(w http.ResponseWriter, r *http.Request) {
	out := viewerStatus{Image: viewer.ImageStatus{State: "missing"}, GPU: "cpu", GPUs: []viewer.GPU{}, Sessions: []viewer.SessionInfo{},
		IdleMinutes: int(s.consoleIdleAfter() / time.Minute)}
	if m := s.opts.Viewer; m != nil {
		user := currentUser(r)
		out.Image, out.Memory = m.ImageStatus(), m.MemoryEstimate()
		out.GPU = m.NewViewGPU(r.Context())
		if user.Role.CanAdminister() {
			out.GPUs = m.GPUs(r.Context())
			out.Sessions = m.Sessions(r.Context(), "")
		} else {
			out.Sessions = m.Sessions(r.Context(), user.Name)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleViewerImage starts getting the image now (it is otherwise fetched
// with the first view) and answers the status of the image.
func (s *Server) handleViewerImage(w http.ResponseWriter, r *http.Request) {
	if s.opts.Viewer == nil {
		writeError(w, http.StatusNotFound, "not_found", "no remote view")
		return
	}
	s.opts.Viewer.PrepareImage()
	noteTarget(r, "viewer")
	writeJSON(w, http.StatusOK, s.opts.Viewer.ImageStatus())
}

// viewerError maps a failed Open to the code the window shows.
func viewerError(err error) (code, text string) {
	switch {
	case errors.Is(err, viewer.ErrImageNotReady):
		return "viewer_image", "the remote view is being prepared"
	case errors.Is(err, viewer.ErrMemory):
		return "viewer_memory", "not enough free memory for another remote view"
	case errors.Is(err, viewer.ErrHostNetwork):
		return "viewer_hostnet", "the app uses the network of the NAS; open it in its own tab"
	case errors.Is(err, viewer.ErrNoWebUI):
		return "viewer_no_webui", "the app has no web interface"
	case errors.Is(err, viewer.ErrNotRunning):
		return "viewer_not_running", "the app does not run"
	case errors.Is(err, viewer.ErrNoVolume):
		return "viewer_volume", "no volume for the remote view"
	case errors.Is(err, viewer.ErrName):
		return "viewer_name", "this account or app name cannot be viewed remotely"
	}
	return "viewer_error", "the remote view could not be started"
}

// allowsAppNow tells whether the account (as it is now) may use an app:
// the check of the app window, repeated while a view is open.
func (s *Server) allowsAppNow(user auth.User, app string) bool {
	if s.opts.AppAccess == nil {
		return user.Role.CanAdminister()
	}
	return s.opts.AppAccess.Allows(user.Name, app)
}

// handleViewerStream relays the remote view of an app. Problems are told
// after the upgrade as {"t":"error","code":…}: a browser cannot read the
// answer of a refused WebSocket handshake.
func (s *Server) handleViewerStream(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	app := r.PathValue("app")
	// Accept checks that the page asking is this site, not another one.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(viewer.MaxBrowserMessage)
	ctx := r.Context()
	record := func(action, target string, ok bool) {
		if s.opts.Activity != nil {
			status := http.StatusOK
			if !ok {
				status = http.StatusConflict
			}
			s.opts.Activity.Record(activity.Entry{User: user.Name, Via: activity.ViaWeb, IP: remoteIP(r), Action: action, Target: target, Status: status, OK: ok})
		}
	}
	fail := func(code, text string, extra map[string]any) {
		msg := map[string]any{"t": "error", "code": code, "text": text}
		for k, v := range extra {
			msg[k] = v
		}
		if data, err := json.Marshal(msg); err == nil {
			wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_ = conn.Write(wctx, websocket.MessageText, data)
			cancel()
		}
		conn.Close(websocket.StatusPolicyViolation, code)
	}
	var opener viewerOpener = s.opts.Viewer
	if s.viewerOpen != nil {
		opener = s.viewerOpen
	}
	if s.opts.Viewer == nil && s.viewerOpen == nil {
		fail("viewer_unavailable", "the remote view is not available", nil)
		return
	}
	if !s.allowsAppNow(user, app) {
		record("viewer_start", app+", app_forbidden", false)
		fail("app_forbidden", appWindowMessage("app_forbidden"), nil)
		return
	}
	// The window can show that the view is starting (a container may take
	// a few seconds).
	_ = conn.Write(ctx, websocket.MessageText, []byte(`{"t":"starting"}`))
	lease, err := opener.Open(ctx, user.Name, app)
	if err != nil {
		code, text := viewerError(err)
		var extra map[string]any
		switch code {
		case "viewer_image":
			if s.opts.Viewer != nil {
				extra = map[string]any{"image": s.opts.Viewer.ImageStatus()}
			}
		case "viewer_memory":
			if s.opts.Viewer != nil {
				extra = map[string]any{"memory": s.opts.Viewer.MemoryEstimate()}
			}
		case "viewer_error":
			slog.Warn("remote view not started", "user", user.Name, "app", app, "err", err)
		}
		if code != "viewer_image" {
			record("viewer_start", app+", "+code, false)
		}
		fail(code, text, extra)
		return
	}
	agent, err := lease.Dial(ctx)
	if err != nil {
		lease.Close(false)
		slog.Warn("remote view agent not reached", "user", user.Name, "app", app, "err", err)
		record("viewer_start", app+", viewer_agent", false)
		fail("viewer_agent", "the remote view did not start", nil)
		return
	}
	defer agent.CloseNow()
	record("viewer_start", app, true)
	slog.Info("remote view opened", "user", user.Name, "app", app, "ip", remoteIP(r))

	reason := s.watchViewer(ctx, r, user, app, lease, func(run context.Context, input func()) viewer.End {
		return viewer.Relay(run, conn, agent, input)
	})
	linger := false
	switch reason {
	case "closed":
		linger = true
	case "replaced":
		linger = true
	}
	if reason != "closed" {
		bye, _ := json.Marshal(map[string]string{"t": "bye", "reason": reason})
		wctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = conn.Write(wctx, websocket.MessageText, bye)
		cancel()
		conn.Close(websocket.StatusNormalClosure, reason)
	}
	agent.Close(websocket.StatusNormalClosure, "")
	lease.Close(linger)
	record("viewer_end", app+", "+reason, true)
	slog.Info("remote view closed", "user", user.Name, "app", app, "reason", reason, "ip", remoteIP(r))
}

// watchViewer runs relay until it ends, the login or the app right ends
// (checked every consoleRecheck, like consoles), no input came for the
// console idle time (0: never) or another window took the view over. It
// returns why: closed (the window went), idle, logout, forbidden,
// replaced, stopped or error.
func (s *Server) watchViewer(ctx context.Context, r *http.Request, user auth.User, app string, lease *viewer.Lease, relay func(context.Context, func()) viewer.End) string {
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	why := ""
	stop := func(reason string) {
		mu.Lock()
		if why == "" {
			why = reason
		}
		mu.Unlock()
		cancel()
	}
	idleAfter := s.consoleIdleAfter()
	var idle *time.Timer
	if idleAfter > 0 {
		idle = time.AfterFunc(idleAfter, func() { stop("idle") })
		defer idle.Stop()
	}
	var ended <-chan struct{}
	if lease != nil {
		ended = lease.Ended
	}
	go func() {
		recheck := time.NewTicker(time.Duration(consoleRecheck.Load()))
		defer recheck.Stop()
		for {
			select {
			case <-recheck.C:
				current, ok := s.loginHolds(r, user)
				if !ok {
					stop("logout")
					return
				}
				if !s.allowsAppNow(current, app) {
					stop("forbidden")
					return
				}
			case <-ended:
				if lease.Replaced() {
					stop("replaced")
				} else {
					stop("stopped")
				}
				return
			case <-run.Done():
				return
			}
		}
	}()
	end := relay(run, func() {
		if idle != nil {
			idle.Reset(idleAfter)
		}
	})
	mu.Lock()
	defer mu.Unlock()
	switch {
	case why != "":
		return why
	case end == viewer.EndBrowser:
		return "closed"
	case end == viewer.EndAgent:
		return "stopped"
	case end == viewer.EndRefused:
		return "error"
	}
	return "closed"
}

var viewerDownloadID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// handleViewerDownload hands over a file the browser in the view
// downloaded, from the agent, always as an attachment.
func (s *Server) handleViewerDownload(w http.ResponseWriter, r *http.Request) {
	app, id := r.PathValue("app"), r.PathValue("id")
	user := currentUser(r)
	if !s.allowsAppNow(user, app) {
		writeError(w, http.StatusForbidden, "app_forbidden", appWindowMessage("app_forbidden"))
		return
	}
	if s.opts.Viewer == nil || !viewerDownloadID.MatchString(id) {
		writeError(w, http.StatusNotFound, "not_found", "no such download")
		return
	}
	lease, ok := s.opts.Viewer.Peek(user.Name, app)
	if !ok {
		writeError(w, http.StatusNotFound, "viewer_no_session", "the remote view of this app is not open")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, lease.DownloadURL(id), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "download failed")
		return
	}
	res, err := viewerClient.Do(req)
	if err != nil {
		// The error would name the address with the key.
		writeError(w, http.StatusBadGateway, "viewer_agent", "the remote view did not answer")
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		writeError(w, http.StatusNotFound, "not_found", "no such download")
		return
	}
	name := "download"
	if _, params, err := mime.ParseMediaType(res.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
		name = params["filename"]
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", attachmentName(name))
	h.Set("Cache-Control", "no-store")
	if res.ContentLength >= 0 {
		h.Set("Content-Length", fmt.Sprint(res.ContentLength))
	}
	noteTarget(r, app)
	_, _ = io.Copy(w, res.Body)
}

// viewerClient talks to agents on the internal network only.
var viewerClient = &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}}

// attachmentName is a Content-Disposition with a safe ASCII fallback and
// the full name per RFC 5987.
func attachmentName(name string) string {
	name = strings.Map(func(c rune) rune {
		if c < 0x20 || c == 0x7f || c == '/' || c == '\\' {
			return -1
		}
		return c
	}, name)
	name = strings.TrimLeft(name, ".")
	if name == "" {
		name = "download"
	}
	if runes := []rune(name); len(runes) > 200 {
		name = string(runes[:200])
	}
	ascii := strings.Map(func(c rune) rune {
		if c > 0x7e || c == '"' || c == ';' || c == '%' {
			return '_'
		}
		return c
	}, name)
	return mime.FormatMediaType("attachment", map[string]string{"filename": ascii}) + "; filename*=UTF-8''" + rfc5987(name)
}

func rfc5987(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("!#$&+-.^_`|~", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
