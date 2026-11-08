package main

// The picture stream of a client (PLAN 72): tiles while the screen is
// mostly calm, H.264 while much of it moves (transport "auto"), or H.264
// only (transport "video"). While H.264 runs, a slow raw capture keeps
// measuring how much changes, so the stream can go back to tiles.

import (
	"log"
	"time"

	"github.com/coder/websocket"

	"github.com/ra5on/TitanOS/backend/internal/viewer/proto"
)

// tileStream is the state of the tile mode, guarded by client.vmu except
// where noted.
type tileStream struct {
	raw    *rawCapture
	grid   *tileGrid  // only touched by the capture goroutine
	sw     modeSwitch // only touched by the capture goroutine
	detect bool       // H.264 runs, the capture only measures
}

func (c *client) transport() string {
	c.a.mu.Lock()
	defer c.a.mu.Unlock()
	return c.a.transport
}

// startStream starts the picture stream for the current settings.
func (c *client) startStream(factor float64) {
	if c.transport() == proto.TransportVideo {
		c.startVideo(factor)
		return
	}
	c.startTiles()
}

// stopStream stops whatever picture stream runs.
func (c *client) stopStream() {
	c.stopVideo()
	c.stopTiles()
}

func (c *client) stopTiles() {
	c.vmu.Lock()
	t := c.tiles
	c.tiles = nil
	c.vmu.Unlock()
	if t != nil {
		t.raw.stop()
	}
}

// inVideo reports whether H.264 runs in the auto transport.
func (c *client) inVideo() bool {
	c.vmu.Lock()
	defer c.vmu.Unlock()
	return c.tiles != nil && c.tiles.detect
}

// startTiles starts the tile mode: hello with codec "tiles", then all
// tiles of the first frame.
func (c *client) startTiles() {
	a := c.a
	a.mu.Lock()
	w, h, fps, scale := a.w, a.h, a.fps, a.scale
	a.mu.Unlock()
	c.runCapture(w, h, fps, false)
	c.sendJSON(proto.Hello{T: "hello", Codec: proto.CodecTiles, Width: w, Height: h, Scale: scale, GPU: "cpu", Audio: c.audio != nil}, true)
	c.sendJSON(proto.Mode{T: "mode", Mode: "tiles"}, true)
	log.Printf("tiles: %dx%d@%d", w, h, fps)
}

// runCapture (re)starts the raw capture; detect only measures.
func (c *client) runCapture(w, h, fps int, detect bool) {
	c.stopTiles()
	c.vmu.Lock()
	// Not c.gen: that counts encoders, and onAU drops frames of an older one.
	t := &tileStream{grid: newTileGrid(w, h), detect: detect}
	if detect {
		t.sw.video = true
	}
	c.tiles = t
	c.vmu.Unlock()
	raw, err := startRaw(w, h, fps, func(frame []byte) { c.onFrame(t, w, frame) })
	if err != nil {
		log.Printf("tiles: %v", err)
		return
	}
	c.vmu.Lock()
	if c.tiles == t {
		t.raw = raw
		c.vmu.Unlock()
		return
	}
	c.vmu.Unlock()
	raw.stop()
}

// onFrame handles one captured frame (capture goroutine).
func (c *client) onFrame(t *tileStream, w int, frame []byte) {
	now := time.Now()
	changed, share := t.grid.diff(frame, now)
	if t.sw.update(share, now) {
		video := t.sw.video
		select {
		case c.ctrl <- ctrlEvent{video: &video}:
		default:
		}
	}
	if t.detect {
		t.grid.commit(frame, changed, false)
		return
	}
	c.vmu.Lock()
	current := c.tiles == t
	c.vmu.Unlock()
	if !current {
		return
	}
	c.a.mu.Lock()
	quality := c.a.quality
	c.a.mu.Unlock()
	sent := false
	for _, run := range t.grid.runs(changed) {
		moving := false
		for _, i := range run.tiles {
			moving = moving || t.grid.moving(i)
		}
		msg, err := encodeTile(frame, w, run.r, false, jpegQuality(quality, moving))
		if err != nil || !c.trySend(outMsg{typ: websocket.MessageBinary, data: msg, enq: now, vid: true}) {
			// Not committed: the tiles count as changed again next frame.
			continue
		}
		t.grid.commit(frame, run.tiles, true)
		sent = true
	}
	for _, run := range t.grid.runs(t.grid.sharpen(now, sharpenBudget)) {
		msg, err := encodeTile(frame, w, run.r, true, 0)
		if err != nil || !c.trySend(outMsg{typ: websocket.MessageBinary, data: msg, enq: now, vid: true}) {
			break
		}
		t.grid.commit(frame, run.tiles, false)
		sent = true
	}
	if sent {
		c.frames.Add(1)
	}
}

// switchMode follows a decision of the mode switch (control loop).
func (c *client) switchMode(video bool, factor float64) {
	if c.transport() != proto.TransportAuto || video == c.inVideo() {
		return
	}
	a := c.a
	a.mu.Lock()
	w, h := a.w, a.h
	a.mu.Unlock()
	if video {
		log.Printf("tiles: much of the screen moves, switching to video")
		c.stopTiles()
		c.startVideo(factor)
		c.sendJSON(proto.Mode{T: "mode", Mode: "video"}, true)
		c.runCapture(w, h, detectFPS, true)
		return
	}
	log.Printf("tiles: calm again, back to tiles")
	c.stopVideo()
	c.startTiles()
}

// streamExited is closed when the running capture or encoder ended.
func (c *client) streamExited() <-chan struct{} {
	c.vmu.Lock()
	defer c.vmu.Unlock()
	if c.video != nil {
		return c.video.exited()
	}
	if c.tiles != nil && c.tiles.raw != nil {
		return c.tiles.raw.exited()
	}
	return nil
}
