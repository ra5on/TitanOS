package api

// Events of the NVR and the live feed (docs/NVR.md, "API Phase 2"). An
// event belongs to a camera: every route checks the camera right
// "recordings" of the account, and answers an event of a camera the
// account may not see like one that does not exist.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/nvr"
)

func (a nvrArea) registerEvents(mux *routeMux, g guards) {
	mux.HandleFunc("GET /api/v1/nvr/events", g.user(a.handleEvents))
	mux.HandleFunc("GET /api/v1/nvr/events/{id}", g.user(a.handleEvent))
	mux.HandleFunc("DELETE /api/v1/nvr/events/{id}", g.admin(a.handleEventDelete))
	mux.HandleFunc("POST /api/v1/nvr/events/{id}/reviewed", g.user(a.handleEventReviewed))
	mux.HandleFunc("GET /api/v1/nvr/events/{id}/thumbnail.jpg", g.user(a.handleEventMedia("thumbnail.jpg", "image/")))
	mux.HandleFunc("GET /api/v1/nvr/events/{id}/snapshot.jpg", g.user(a.handleEventMedia("snapshot.jpg", "image/")))
	mux.HandleFunc("GET /api/v1/nvr/events/{id}/clip.mp4", g.user(a.handleEventMedia("clip.mp4", "video/")))
	mux.HandleFunc("GET /api/v1/nvr/feed", g.user(a.handleFeed))
}

// camerasWith lists the cameras the account has a right on; for owners
// and administrators and the recordings also the deleted cameras whose
// recordings are kept.
func (a nvrArea) camerasWith(user auth.User, right string) []string {
	out := []string{}
	for _, id := range a.nvr.CameraIDs() {
		if a.allows(user, id, right) {
			out = append(out, id)
		}
	}
	if right == nvr.RightRecordings && user.Role.CanAdminister() {
		for _, d := range a.nvr.DeletedCameras() {
			out = append(out, d.ID)
		}
	}
	return out
}

// shown is an event as the account may see it: without what the NVR
// recognised (the name of a known face or plate, the plate it read)
// unless the account has the right "names" on the camera (PLAN 82-3;
// owners and administrators always). The pictures Frigate hands out
// draw no names into them (only the kind of object, frigate/util/file.py).
func (a nvrArea) shown(user auth.User, e nvr.Event) nvr.Event {
	if !a.allows(user, e.Camera, nvr.RightNames) {
		e.SubLabel, e.Plate = "", ""
	}
	return e
}

// queryTime reads a time in Unix seconds from the query; 0 if missing.
func queryTime(r *http.Request, name string) (float64, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value < 0 || value > 1e11 {
		return 0, false
	}
	return value, true
}

// handleEvents lists events: {events: […], next}.
func (a nvrArea) handleEvents(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	user, query := currentUser(r), r.URL.Query()
	filter := nvr.EventFilter{Cameras: a.camerasWith(user, nvr.RightRecordings), Limit: 50}
	if raw := query.Get("camera"); raw != "" {
		// Only what the account may see anyway.
		wanted := strings.Split(raw, ",")
		filter.Cameras = slices.DeleteFunc(filter.Cameras, func(id string) bool { return !slices.Contains(wanted, id) })
	}
	if raw := query.Get("label"); raw != "" {
		for _, label := range strings.Split(raw, ",") {
			if !slices.Contains(nvr.AllowedObjects(), label) {
				writeError(w, http.StatusBadRequest, "nvr_invalid", "label")
				return
			}
			filter.Labels = append(filter.Labels, label)
		}
	}
	var ok [3]bool
	filter.From, ok[0] = queryTime(r, "from")
	filter.To, ok[1] = queryTime(r, "to")
	filter.Before, ok[2] = queryTime(r, "before")
	if !ok[0] || !ok[1] || !ok[2] {
		writeError(w, http.StatusBadRequest, "nvr_invalid", "from, to, before: Unix seconds")
		return
	}
	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			writeError(w, http.StatusBadRequest, "nvr_invalid", "limit: 1 to 200")
			return
		}
		filter.Limit = limit
	}
	switch query.Get("reviewed") {
	case "":
	case "0", "1":
		reviewed := query.Get("reviewed") == "1"
		filter.Reviewed = &reviewed
	default:
		writeError(w, http.StatusBadRequest, "nvr_invalid", "reviewed: 0 or 1")
		return
	}
	events, next, err := a.nvr.Events(r.Context(), filter)
	if err != nil {
		writeNVRError(w, err)
		return
	}
	for i := range events {
		events[i] = a.shown(user, events[i])
	}
	out := map[string]any{"events": events, "next": nil}
	if next > 0 {
		out["next"] = next
	}
	writeJSON(w, http.StatusOK, out)
}

// event returns the event of the request if the account may see it; it
// answers the error itself otherwise.
func (a nvrArea) event(w http.ResponseWriter, r *http.Request) (nvr.Event, bool) {
	if !a.available(w) {
		return nvr.Event{}, false
	}
	event, err := a.nvr.Event(r.Context(), r.PathValue("id"))
	if err == nil && !a.reaches(currentUser(r), event.Camera, nvr.RightRecordings) {
		err = nvr.ErrEventNotFound
	}
	if err != nil {
		writeNVRError(w, err)
		return nvr.Event{}, false
	}
	return event, true
}

func (a nvrArea) handleEvent(w http.ResponseWriter, r *http.Request) {
	if event, ok := a.event(w, r); ok {
		writeJSON(w, http.StatusOK, a.shown(currentUser(r), event))
	}
}

// handleEventDelete removes an event with its snapshot.
func (a nvrArea) handleEventDelete(w http.ResponseWriter, r *http.Request) {
	event, ok := a.event(w, r)
	if !ok {
		return
	}
	if err := a.nvr.DeleteEvent(r.Context(), event.ID); err != nil {
		writeNVRError(w, err)
		return
	}
	noteTarget(r, event.Camera)
	slog.Info("NVR event deleted", "camera", event.Camera, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// handleEventReviewed marks an event as seen or not: {reviewed}.
func (a nvrArea) handleEventReviewed(w http.ResponseWriter, r *http.Request) {
	event, ok := a.event(w, r)
	if !ok {
		return
	}
	req := struct {
		Reviewed bool `json:"reviewed"`
	}{Reviewed: true}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.nvr.SetReviewed(r.Context(), event.ID, req.Reviewed); err != nil {
		writeNVRError(w, err)
		return
	}
	event.Reviewed = req.Reviewed
	writeJSON(w, http.StatusOK, a.shown(currentUser(r), event))
}

// handleEventMedia passes a picture or the clip of an event on.
func (a nvrArea) handleEventMedia(file, kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		event, ok := a.event(w, r)
		if !ok {
			return
		}
		name := ""
		if r.URL.Query().Get("download") == "1" {
			name = event.Camera + "-" + strings.ReplaceAll(event.ID, ".", "-") + "." + file[strings.LastIndex(file, ".")+1:]
		}
		a.passMedia(w, r, "/api/events/"+url.PathEscape(event.ID)+"/"+file, "", kind, name)
	}
}

// passMedia streams an answer of Frigate to the browser: status (200,
// 206, 304, 416), the headers that describe the content, and the body.
// Range and the conditional headers of the request go to Frigate, so
// that seeking in a video works. kind is the start of the content type
// Frigate must answer with ("image/", "video/", "" for any of the list
// below); download names the file for "save as".
//
// The caller has checked the right; path is made by the caller from
// checked parts, never from the request as it came.
func (a nvrArea) passMedia(w http.ResponseWriter, r *http.Request, path, rawQuery, kind, download string) {
	res, err := a.nvr.Media(r.Context(), path, rawQuery, r.Header)
	if err != nil {
		if errors.Is(err, nvr.ErrNotRunning) {
			writeNVRError(w, err)
			return
		}
		slog.Warn("NVR media", "err", err)
		writeError(w, http.StatusBadGateway, "nvr_no_picture", "the NVR did not answer")
		return
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK, http.StatusPartialContent, http.StatusNotModified, http.StatusRequestedRangeNotSatisfiable:
	default:
		writeError(w, http.StatusNotFound, "nvr_no_picture", "there is no picture or video for this")
		return
	}
	contentType := res.Header.Get("Content-Type")
	allowed := []string{"image/jpeg", "image/png", "image/webp", "video/mp4", "application/vnd.apple.mpegurl", "application/x-mpegurl"}
	base, _, _ := strings.Cut(contentType, ";")
	if res.StatusCode < 300 && (!slices.Contains(allowed, strings.ToLower(strings.TrimSpace(base))) || !strings.HasPrefix(base, kind)) {
		writeError(w, http.StatusNotFound, "nvr_no_picture", "there is no picture or video for this")
		return
	}
	h := w.Header()
	for _, name := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if value := res.Header.Get(name); value != "" {
			h.Set(name, value)
		}
	}
	// Pictures of people: not for shared caches, not kept by the browser.
	h.Set("Cache-Control", "private, no-store")
	if download != "" {
		h.Set("Content-Disposition", `attachment; filename="`+nvrFileName(download)+`"`)
	}
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, res.Body) // the browser went away or Frigate did; nothing to do
}

// nvrFileName keeps what is safe in a quoted file name.
func nvrFileName(name string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		case r == ' ':
			return '_'
		}
		return -1
	}, name)
	if clean == "" || strings.HasPrefix(clean, ".") {
		clean = "nvr" + clean
	}
	return clean
}

// handleFeed sends what happens at the cameras to a window (WebSocket):
// {"t":"hello","running":…}, {"t":"event",…}, {"t":"activity",…}. titand
// keeps one connection to Frigate for all windows; checked for every
// message, a window gets what is in the picture right now ("activity")
// of the cameras the account may watch live, and the events of those
// whose recordings it may see – as the list of events and their pictures
// need (an event of a live-only camera could not be opened, and one of a
// recordings-only camera never came live); an event without the names
// and plates the account may not see (right "names", PLAN 82-3). Like a
// console the connection ends with the login.
func (a nvrArea) handleFeed(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	// Accept checks that the page asking is this site, not another one.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 << 10) // the window sends nothing
	if a.nvr == nil {
		conn.Close(websocket.StatusPolicyViolation, "nvr_unavailable")
		return
	}
	// The feed asks from its own goroutine: the account is taken once
	// here (its rights are looked up by name, as they are now).
	viewer := user
	messages, cancel := a.nvr.Subscribe(func(camera string) bool {
		return a.allows(viewer, camera, nvr.RightLive) || a.allows(viewer, camera, nvr.RightRecordings)
	})
	defer cancel()
	// Reading notices a window that closed (and answers pings).
	ctx := conn.CloseRead(r.Context())
	recheck := time.NewTicker(time.Duration(consoleRecheck.Load()))
	defer recheck.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-recheck.C:
			if _, ok := a.loginHolds(r, user); !ok {
				bye, _ := json.Marshal(map[string]string{"t": "bye", "reason": "logout"})
				wctx, done := context.WithTimeout(context.Background(), 2*time.Second)
				_ = conn.Write(wctx, websocket.MessageText, bye)
				done()
				conn.Close(websocket.StatusNormalClosure, "logout")
				return
			}
		case message := <-messages:
			if message.T == "activity" && !a.allows(viewer, message.Camera, nvr.RightLive) {
				continue
			}
			if message.Event != nil && !a.allows(viewer, message.Event.Camera, nvr.RightRecordings) {
				continue
			}
			if message.Event != nil {
				// Every window gets the same event: change a copy.
				event := a.shown(viewer, *message.Event)
				message.Event = &event
			}
			data, err := json.Marshal(message)
			if err != nil {
				continue
			}
			wctx, done := context.WithTimeout(ctx, 10*time.Second)
			err = conn.Write(wctx, websocket.MessageText, data)
			done()
			if err != nil {
				return
			}
		}
	}
}
