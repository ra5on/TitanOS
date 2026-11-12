package api

// Moving a camera (PTZ) and talking back through it. Both need the
// camera right "live".

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/ra5on/TitanOS/backend/internal/nvr"
)

func (a nvrArea) registerPTZ(mux *routeMux, g guards) {
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/ptz", g.user(a.handlePTZ))
	mux.HandleFunc("POST /api/v1/nvr/cameras/{id}/ptz", g.user(a.handlePTZCommand))
	mux.HandleFunc("GET /api/v1/nvr/cameras/{id}/talk", g.user(a.handleTalk))
}

// watched returns the camera of the request if the account may watch it
// live; it answers the error itself otherwise.
func (a nvrArea) watched(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !a.available(w) {
		return "", false
	}
	id := r.PathValue("id")
	if _, ok := a.nvr.Camera(id); !ok || !a.allows(currentUser(r), id, nvr.RightLive) {
		writeNVRError(w, nvr.ErrNotFound)
		return "", false
	}
	return id, true
}

// handlePTZ tells what a camera can do: {supported, features, presets}.
func (a nvrArea) handlePTZ(w http.ResponseWriter, r *http.Request) {
	id, ok := a.watched(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	info, err := a.nvr.PTZ(ctx, id)
	if err != nil {
		writeNVRError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// handlePTZCommand moves a camera: {action, preset}. A movement goes on
// until "stop"; titand stops it by itself a few seconds after the last
// request for the camera (nvr.PTZStopAfter), so the page repeats the
// movement while the button is held.
func (a nvrArea) handlePTZCommand(w http.ResponseWriter, r *http.Request) {
	id, ok := a.watched(w, r)
	if !ok {
		return
	}
	var req struct {
		Action string `json:"action"`
		Preset string `json:"preset"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.nvr.PTZCommand(r.Context(), currentUser(r).Name, id, req.Action, req.Preset); err != nil {
		writeNVRError(w, err)
		return
	}
	// Presets are worth a line in the activity log; every step of a
	// movement is not.
	if req.Action == "preset" {
		noteTarget(r, id)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTalk passes the WebRTC signalling for talking back through a
// camera to go2rtc and back (offer, answer, candidates), unchanged. The
// sound itself does not go through titand: the browser sends it straight
// to port 8555 of the NAS. Ends like the live stream.
func (a nvrArea) handleTalk(w http.ResponseWriter, r *http.Request) {
	user, id := currentUser(r), r.PathValue("id")
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(256 << 10) // an offer with its candidates
	fail := func(code string) { conn.Close(websocket.StatusPolicyViolation, code) }
	if a.nvr == nil {
		fail("nvr_unavailable")
		return
	}
	if _, ok := a.nvr.Camera(id); !ok || !a.allows(user, id, nvr.RightLive) {
		fail("nvr_camera_not_found")
		return
	}
	address, err := a.nvr.Talk(id)
	if err != nil {
		fail("nvr_not_running")
		return
	}
	dctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	upstream, err := nvrLiveDial(dctx, address)
	cancel()
	if err != nil {
		slog.Warn("NVR talk not reached", "camera", id, "err", err)
		fail("nvr_no_picture")
		return
	}
	defer upstream.CloseNow()
	upstream.SetReadLimit(256 << 10)
	slog.Info("NVR talk opened", "camera", id, "user", user.Name, "ip", remoteIP(r))
	reason := a.watchLive(r.Context(), r, user, id, func(run context.Context) { relayWebSockets(run, conn, upstream) })
	if reason != "" {
		conn.Close(websocket.StatusNormalClosure, reason)
	}
	upstream.Close(websocket.StatusNormalClosure, "")
}
