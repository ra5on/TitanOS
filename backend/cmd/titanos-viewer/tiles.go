package main

// Tile mode of the remote view (PLAN 66 and 72, the technique of KasmVNC,
// written anew): the screen is captured as raw frames, compared in tiles
// of 64×64 pixels, and only the changed areas are sent as JPEG. An area
// that has been calm for a moment is sent again losslessly as PNG, so text
// becomes sharp after scrolling. When much of the screen keeps moving
// (video, games) the client switches to H.264 and back (modeSwitch).

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/viewer/proto"
)

const (
	tileSize = 64
	// sharpenAfter: an area sent lossy and calm this long is sent losslessly.
	sharpenAfter = 300 * time.Millisecond
	// sharpenBudget bounds the lossless tiles per frame (CPU and bandwidth).
	sharpenBudget = 48
	// videoShare and the two durations follow KasmVNC: at least 45 % of the
	// screen changing for 5 s switches to video, 3 s below goes back.
	videoShare = 0.45
	videoAfter = 5 * time.Second
	tilesAfter = 3 * time.Second
	// detectFPS is the capture rate while H.264 runs, only to notice calm.
	detectFPS = 4
)

// rect is an area of the screen in pixels.
type rect struct{ X, Y, W, H int }

type tileState struct {
	known     bool // prev holds what the browser shows of this tile
	changedAt time.Time
	lossy     bool  // the last picture sent was lossy
	history   uint8 // bit i: changed i frames ago
}

// tileGrid compares frames tile by tile. Frames are BGRx, 4 bytes a pixel,
// rows without padding.
type tileGrid struct {
	w, h, cols, rows int
	prev             []byte
	state            []tileState
}

func newTileGrid(w, h int) *tileGrid {
	cols, rows := (w+tileSize-1)/tileSize, (h+tileSize-1)/tileSize
	return &tileGrid{w: w, h: h, cols: cols, rows: rows, prev: make([]byte, w*h*4), state: make([]tileState, cols*rows)}
}

// tileRect is the pixel area of a tile (smaller at the right and bottom).
func (g *tileGrid) tileRect(i int) rect {
	x, y := (i%g.cols)*tileSize, (i/g.cols)*tileSize
	return rect{x, y, min(tileSize, g.w-x), min(tileSize, g.h-y)}
}

// diff returns the tiles that differ from the previous frame (all on the
// first frame) and the share of the screen they cover. It does not take
// the frame over; commit does, for the tiles that were sent.
func (g *tileGrid) diff(frame []byte, now time.Time) ([]int, float64) {
	var changed []int
	area := 0
	stride := g.w * 4
	for i := range g.state {
		st := &g.state[i]
		st.history <<= 1
		r := g.tileRect(i)
		same := st.known
		for y := r.Y; same && y < r.Y+r.H; y++ {
			off := y*stride + r.X*4
			if !bytes.Equal(frame[off:off+r.W*4], g.prev[off:off+r.W*4]) {
				same = false
			}
		}
		if !same {
			st.history |= 1
			st.changedAt = now
			changed = append(changed, i)
			area += r.W * r.H
		}
	}
	return changed, float64(area) / float64(g.w*g.h)
}

// commit takes the pixels of the given tiles over as sent.
func (g *tileGrid) commit(frame []byte, tiles []int, lossy bool) {
	stride := g.w * 4
	for _, i := range tiles {
		r := g.tileRect(i)
		for y := r.Y; y < r.Y+r.H; y++ {
			off := y*stride + r.X*4
			copy(g.prev[off:off+r.W*4], frame[off:off+r.W*4])
		}
		g.state[i].lossy = lossy
		g.state[i].known = true
	}
}

// moving reports whether a tile changed in at least three of the last
// five frames; it then gets a lower JPEG quality.
func (g *tileGrid) moving(i int) bool {
	n := 0
	for h := g.state[i].history & 0x1f; h != 0; h &= h - 1 {
		n++
	}
	return n >= 3
}

// sharpen lists tiles whose last picture was lossy and that have been calm
// for sharpenAfter, at most limit of them.
func (g *tileGrid) sharpen(now time.Time, limit int) []int {
	var out []int
	for i, st := range g.state {
		if st.lossy && st.history&1 == 0 && now.Sub(st.changedAt) >= sharpenAfter {
			out = append(out, i)
			if len(out) == limit {
				break
			}
		}
	}
	return out
}

// runs joins horizontally neighbouring tiles of one row into rectangles,
// each with the tiles it covers.
func (g *tileGrid) runs(tiles []int) []struct {
	r     rect
	tiles []int
} {
	var out []struct {
		r     rect
		tiles []int
	}
	for k := 0; k < len(tiles); {
		start := tiles[k]
		end := k
		for end+1 < len(tiles) && tiles[end+1] == tiles[end]+1 && tiles[end+1]/g.cols == start/g.cols {
			end++
		}
		first, last := g.tileRect(start), g.tileRect(tiles[end])
		out = append(out, struct {
			r     rect
			tiles []int
		}{rect{first.X, first.Y, last.X + last.W - first.X, first.H}, tiles[k : end+1]})
		k = end + 1
	}
	return out
}

// cut copies an area of a BGRx frame into an RGBA picture.
func cut(frame []byte, w int, r rect) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, r.W, r.H))
	for y := 0; y < r.H; y++ {
		src := frame[((r.Y+y)*w+r.X)*4:]
		dst := img.Pix[y*img.Stride:]
		for x := 0; x < r.W; x++ {
			s, d := src[x*4:x*4+4], dst[x*4:x*4+4]
			d[0], d[1], d[2], d[3] = s[2], s[1], s[0], 0xff
		}
	}
	return img
}

var pngEncoder = png.Encoder{CompressionLevel: png.BestSpeed}

// encodeTile builds one 0x04 message.
func encodeTile(frame []byte, w int, r rect, lossless bool, quality int) ([]byte, error) {
	var buf bytes.Buffer
	codec := proto.TileJPEG
	if lossless {
		codec = proto.TilePNG
	}
	buf.Write(proto.TileHeader(codec, r.X, r.Y, r.W, r.H))
	img := cut(frame, w, r)
	var err error
	if lossless {
		err = pngEncoder.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	}
	return buf.Bytes(), err
}

// jpegQuality maps the quality setting to JPEG qualities for calm and
// moving areas (KasmVNC: lower quality where things move).
func jpegQuality(mode string, moving bool) int {
	calm, move := 80, 55
	switch mode {
	case proto.QualityHigh:
		calm, move = 90, 75
	case proto.QualityBalanced:
		calm, move = 75, 50
	case proto.QualityLow:
		calm, move = 55, 35
	}
	if moving {
		return move
	}
	return calm
}

// modeSwitch decides between tiles and video from the changed share of
// each frame.
type modeSwitch struct {
	video     bool
	highSince time.Time
	lowSince  time.Time
}

// update returns true when the mode changes; m.video is the new mode.
func (m *modeSwitch) update(share float64, now time.Time) bool {
	high := share >= videoShare
	if high {
		m.lowSince = time.Time{}
		if m.highSince.IsZero() {
			m.highSince = now
		}
	} else {
		m.highSince = time.Time{}
		if m.lowSince.IsZero() {
			m.lowSince = now
		}
	}
	switch {
	case !m.video && high && now.Sub(m.highSince) >= videoAfter:
		m.video = true
		m.lowSince = time.Time{}
		return true
	case m.video && !high && now.Sub(m.lowSince) >= tilesAfter:
		m.video = false
		m.highSince = time.Time{}
		return true
	}
	return false
}

// rawPipelineArgs captures raw BGRx frames to fd 3.
func rawPipelineArgs(w, h, fps int) ([]string, error) {
	if w <= 0 || h <= 0 || fps <= 0 {
		return nil, errors.New("bad capture size")
	}
	line := fmt.Sprintf("ximagesrc display-name=%s show-pointer=false use-damage=false startx=0 starty=0 endx=%d endy=%d"+
		" ! video/x-raw,framerate=%d/1 ! queue max-size-buffers=1 leaky=downstream ! videoconvert n-threads=2"+
		" ! video/x-raw,format=BGRx,width=%d,height=%d ! fdsink fd=3 sync=false", display, w-1, h-1, fps, w, h)
	return append([]string{"/usr/bin/gst-launch-1.0", "-q"}, strings.Fields(line)...), nil
}

// rawCapture runs the raw pipeline and hands over whole frames.
type rawCapture struct {
	p    *proc
	r    *os.File
	done chan struct{}
	once sync.Once
}

func startRaw(w, h, fps int, onFrame func([]byte)) (*rawCapture, error) {
	args, err := rawPipelineArgs(w, h, fps)
	if err != nil {
		return nil, err
	}
	r, wr, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	setPipeSize(wr, 1<<20)
	p, err := startProc("gst-raw", args, baseEnv(), nil, true, wr)
	wr.Close()
	if err != nil {
		r.Close()
		return nil, err
	}
	c := &rawCapture{p: p, r: r, done: make(chan struct{})}
	go func() {
		defer close(c.done)
		frame := make([]byte, w*h*4)
		for {
			if _, err := io.ReadFull(r, frame); err != nil {
				return
			}
			onFrame(frame)
		}
	}()
	return c, nil
}

func (c *rawCapture) exited() <-chan struct{} {
	if c == nil {
		return nil
	}
	return c.done
}

func (c *rawCapture) stop() {
	if c == nil {
		return
	}
	c.once.Do(func() {
		c.p.stop(2 * time.Second)
		c.r.Close()
		<-c.done
	})
}
