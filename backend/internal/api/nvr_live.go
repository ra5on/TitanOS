package api

// Live pictures of the NVR's cameras: a still (Frigate's latest.jpg) and
// the live stream (Frigate's MSE WebSocket), both passed through by
// titand with the camera right "live" checked. Frigate itself is never
// reachable from a browser.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/nvr"
)

// nvrLiveMessage bounds one message of the live stream in either
// direction (a key frame of a 4K stream stays far below).
const nvrLiveMessage = 16 << 20

// handleSnapshot answers the latest picture of a camera as JPEG;
// ?height=<pixels> (90 to 2160) scales it.
func (a nvrArea) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	user, id := currentUser(r), r.PathValue("id")
	// A camera the account may not watch is answered like one that does
	// not exist.
	if _, ok := a.nvr.Camera(id); !ok || !a.allows(user, id, nvr.RightLive) {
		writeNVRError(w, nvr.ErrNotFound)
		return
	}
	height := 0
	if raw := r.URL.Query().Get("height"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 90 || n > 2160 {
			writeError(w, http.StatusBadRequest, "nvr_invalid", "height: 90 to 2160")
			return
		}
		height = n
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	picture, err := a.nvr.Snapshot(ctx, id, height)
	if err != nil {
		if errors.Is(err, nvr.ErrNotRunning) || errors.Is(err, nvr.ErrNotFound) {
			writeNVRError(w, err)
			return
		}
		slog.Warn("NVR snapshot", "camera", id, "err", err)
		writeError(w, http.StatusBadGateway, "nvr_no_picture", "the camera has no picture right now")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Length", strconv.Itoa(len(picture)))
	_, _ = w.Write(picture) // the browser went away; nothing to do
}

// nvrLiveDial connects to Frigate's live WebSocket; tests replace it.
var nvrLiveDial = func(ctx context.Context, address string) (*websocket.Conn, error) {
	conn, _, err := websocket.Dial(ctx, address, nil)
	return conn, err
}

// handleLive passes the live stream of a camera through: the browser
// speaks go2rtc's MSE protocol (text messages for the codecs, binary ones
// with the video) with Frigate, titand in between. ?stream=sub takes the
// small stream (tiles), anything else the main one.
//
// Like a console the connection belongs to the login: it ends when the
// account logs out, is locked or loses the right on the camera (checked
// every consoleRecheck). Problems are told after the upgrade as
// {"t":"error","code":…}: a browser cannot read the answer of a refused
// WebSocket handshake.
func (a nvrArea) handleLive(w http.ResponseWriter, r *http.Request) {
	user, id := currentUser(r), r.PathValue("id")
	// Accept checks that the page asking is this site, not another one.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(64 << 10) // the browser only sends short control messages
	ctx := r.Context()
	fail := func(code, text string) {
		if data, err := json.Marshal(map[string]string{"t": "error", "code": code, "text": text}); err == nil {
			wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_ = conn.Write(wctx, websocket.MessageText, data)
			cancel()
		}
		conn.Close(websocket.StatusPolicyViolation, code)
	}
	if a.nvr == nil {
		fail("nvr_unavailable", "this NAS has no NVR")
		return
	}
	if _, ok := a.nvr.Camera(id); !ok || !a.allows(user, id, nvr.RightLive) {
		fail("nvr_camera_not_found", "no such camera")
		return
	}
	address, err := a.nvr.Live(id, r.URL.Query().Get("stream") == "sub")
	if err != nil {
		fail("nvr_not_running", "the NVR does not run")
		return
	}
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	upstream, err := nvrLiveDial(dctx, address)
	cancel()
	if err != nil {
		slog.Warn("NVR live stream not reached", "camera", id, "err", err)
		fail("nvr_no_picture", "the camera has no picture right now")
		return
	}
	defer upstream.CloseNow()
	upstream.SetReadLimit(nvrLiveMessage)

	reason := a.watchLive(ctx, r, user, id, func(run context.Context) { relayWebSockets(run, conn, upstream) })
	if reason != "" {
		bye, _ := json.Marshal(map[string]string{"t": "bye", "reason": reason})
		wctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = conn.Write(wctx, websocket.MessageText, bye)
		cancel()
		conn.Close(websocket.StatusNormalClosure, reason)
	}
	upstream.Close(websocket.StatusNormalClosure, "")
}

// watchLive runs relay until it ends or the login or the right on the
// camera ends. It returns why it was stopped: "logout", "forbidden" or ""
// (the window or Frigate closed the stream).
func (a nvrArea) watchLive(ctx context.Context, r *http.Request, user auth.User, camera string, relay func(context.Context)) string {
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	why := ""
	go func() {
		recheck := time.NewTicker(time.Duration(consoleRecheck.Load()))
		defer recheck.Stop()
		for {
			select {
			case <-recheck.C:
				reason := ""
				if current, ok := a.loginHolds(r, user); !ok {
					reason = "logout"
				} else if _, exists := a.nvr.Camera(camera); !exists || !a.allows(current, camera, nvr.RightLive) {
					reason = "forbidden"
				}
				if reason != "" {
					mu.Lock()
					why = reason
					mu.Unlock()
					cancel()
					return
				}
			case <-run.Done():
				return
			}
		}
	}()
	relay(run)
	mu.Lock()
	defer mu.Unlock()
	return why
}

// relayWebSockets copies messages between two WebSockets, text and
// binary as they come, until one side ends or ctx is done.
//
// The copies do not stop with ctx: a cancelled read would close the
// browser's connection before the caller could say why it ends (as in
// viewer.Relay). The caller closes both connections after this returned;
// that ends the copies.
func relayWebSockets(ctx context.Context, a, b *websocket.Conn) {
	open := context.WithoutCancel(ctx)
	ended := make(chan struct{}, 2)
	copyMessages := func(from, to *websocket.Conn) {
		defer func() { ended <- struct{}{} }()
		for {
			kind, data, err := from.Read(open)
			if err != nil {
				return
			}
			wctx, done := context.WithTimeout(open, 30*time.Second)
			err = to.Write(wctx, kind, data)
			done()
			if err != nil {
				return
			}
		}
	}
	go copyMessages(a, b)
	go copyMessages(b, a)
	select {
	case <-ended:
	case <-ctx.Done():
	}
}
