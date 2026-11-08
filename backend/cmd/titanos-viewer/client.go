package main

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/ra5on/TitanOS/backend/internal/viewer/proto"
)

const sendQueue = 48

type outMsg struct {
	typ  websocket.MessageType
	data []byte
	enq  time.Time
	vid  bool
}

// control events for the client's control loop
type ctrlEvent struct {
	msg     *proto.ClientMsg
	dropped bool
	// video: the mode switch of the tile mode decided (PLAN 72).
	video *bool
}

type client struct {
	a      *agent
	conn   *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	start  time.Time

	sendMu sync.Mutex
	send   chan outMsg
	ctrl   chan ctrlEvent

	closeOnce sync.Once
	byeMu     sync.Mutex
	bye       *proto.Bye

	up *uploads

	// video state, guarded by vmu
	vmu      sync.Mutex
	video    *videoEncoder
	gen      int
	helloGen int // generation whose hello was sent
	dropping bool
	params   encoderParams
	firstAU  chan struct{}

	audio *audioStream
	tiles *tileStream // tile mode or its measuring capture, guarded by vmu

	frames   atomic.Int64
	bytes    atomic.Int64
	queueSum atomic.Int64 // µs waited in the send queue (video)
	queueN   atomic.Int64

	lastClip string
	texts    chan string
}

func newClient(a *agent, conn *websocket.Conn) *client {
	// Not derived from the agent context: on SIGTERM the client must
	// still send "bye" (closeClient) before its context ends.
	ctx, cancel := context.WithCancel(context.Background())
	conn.SetReadLimit(proto.MaxTextMessage + 64)
	return &client{
		a: a, conn: conn, ctx: ctx, cancel: cancel, done: make(chan struct{}),
		start: time.Now(), send: make(chan outMsg, sendQueue), ctrl: make(chan ctrlEvent, 64),
		up: newUploads(), texts: make(chan string, 8),
	}
}

func (c *client) ts() uint64 { return uint64(time.Since(c.start).Microseconds()) }

// trySend queues messages together or not at all.
func (c *client) trySend(msgs ...outMsg) bool {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if c.ctx.Err() != nil || cap(c.send)-len(c.send) < len(msgs) {
		return false
	}
	for _, m := range msgs {
		c.send <- m
	}
	return true
}

// sendJSON queues a text message; important ones wait up to 2 s for room.
func (c *client) sendJSON(v any, important bool) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	m := outMsg{typ: websocket.MessageText, data: b, enq: time.Now()}
	if c.trySend(m) || !important {
		return
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && c.ctx.Err() == nil {
		time.Sleep(20 * time.Millisecond)
		if c.trySend(m) {
			return
		}
	}
}

func (c *client) close(reason, text string) {
	c.byeMu.Lock()
	if c.bye == nil {
		c.bye = &proto.Bye{T: "bye", Reason: reason, Text: text}
	}
	c.byeMu.Unlock()
	c.closeOnce.Do(func() { c.cancel() })
}

func (c *client) run() {
	defer close(c.done)
	log.Printf("client connected")
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); c.writer() }()
	go func() {
		defer wg.Done()
		for {
			select {
			case t := <-c.texts:
				c.a.x.text(c.ctx, t)
			case <-c.ctx.Done():
				return
			}
		}
	}()
	go func() { defer wg.Done(); c.control() }()
	c.reader()
	c.close(proto.ByeStopped, "")
	wg.Wait()
	c.stopMedia()
	c.a.x.releaseAll()
	c.up.abortAll()
	log.Printf("client disconnected")
}

// writer sends queued messages; on shutdown it sends bye and closes.
func (c *client) writer() {
	for {
		select {
		case m := <-c.send:
			if m.vid {
				c.queueSum.Add(time.Since(m.enq).Microseconds())
				c.queueN.Add(1)
			}
			ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
			err := c.conn.Write(ctx, m.typ, m.data)
			cancel()
			if err != nil {
				c.close(proto.ByeError, "")
				c.finish()
				return
			}
			c.bytes.Add(int64(len(m.data)))
		case <-c.ctx.Done():
			c.finish()
			return
		}
	}
}

func (c *client) finish() {
	c.byeMu.Lock()
	bye := c.bye
	c.byeMu.Unlock()
	if bye != nil {
		if b, err := json.Marshal(bye); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = c.conn.Write(ctx, websocket.MessageText, b)
			cancel()
		}
	}
	_ = c.conn.Close(websocket.StatusNormalClosure, "")
}

// reader handles incoming messages. Input goes straight to X; the rest
// to the control loop.
func (c *client) reader() {
	x := c.a.x
	for {
		// Not c.ctx: cancelling a Read context closes the connection, and
		// the writer still has to send "bye". The writer closes the
		// connection, which ends this Read.
		typ, data, err := c.conn.Read(context.Background())
		if err != nil {
			return
		}
		if typ == websocket.MessageBinary {
			id, payload, err := proto.ParseUploadChunk(data)
			if err != nil {
				continue
			}
			done, final, code := c.up.chunk(id, payload)
			if code != "" {
				c.sendJSON(proto.Uploaded{T: "uploaded", ID: id, Error: code}, true)
			} else if done {
				log.Printf("upload %d complete", id)
				c.sendJSON(proto.Uploaded{T: "uploaded", ID: id, Name: final}, true)
			}
			continue
		}
		m, err := proto.ParseClient(data)
		if err != nil {
			continue
		}
		switch m.T {
		case proto.MsgMouse:
			x.mouse(m.X, m.Y, m.Buttons)
		case proto.MsgWheel:
			x.wheel(m.DX, m.DY)
		case proto.MsgKey:
			x.key(m)
		case proto.MsgText:
			// typed in order on its own goroutine, so a long text does
			// not block the other input
			select {
			case c.texts <- m.Text:
			default:
				log.Printf("text dropped: typing queue full")
			}
		case proto.MsgUpload:
			if _, code := c.up.begin(m.ID, m.Name, m.Size); code != "" {
				c.sendJSON(proto.Uploaded{T: "uploaded", ID: m.ID, Error: code}, true)
			} else if m.Size == 0 {
				_, final, code := c.up.finish(m.ID)
				c.sendJSON(proto.Uploaded{T: "uploaded", ID: m.ID, Name: final, Error: code}, true)
			}
		default:
			mm := m
			select {
			case c.ctrl <- ctrlEvent{msg: &mm}:
			case <-c.ctx.Done():
				return
			}
		}
	}
}

// control owns encoder restarts, size changes and periodic work.
func (c *client) control() {
	a := c.a
	c.startMedia()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var sizeTimer <-chan time.Time
	var pendingSize *proto.ClientMsg
	lastKey := time.Time{}
	factor := 1.0
	lastDrop := time.Now()
	lastFrames, lastBytes := int64(0), int64(0)

	// restart starts the stream anew in its current mode (PLAN 72: tiles,
	// or H.264 with the measuring capture while much moves).
	restart := func() {
		video := c.inVideo()
		c.stopStream()
		if video {
			c.startVideo(factor)
			a.mu.Lock()
			w, h := a.w, a.h
			a.mu.Unlock()
			c.runCapture(w, h, detectFPS, true)
			return
		}
		c.startStream(factor)
	}
	for {
		exited := c.streamExited()
		select {
		case <-c.ctx.Done():
			return
		case <-exited:
			if c.ctx.Err() != nil {
				return
			}
			log.Printf("video pipeline ended, restarting")
			time.Sleep(time.Second)
			restart()
		case ev := <-c.ctrl:
			if ev.video != nil {
				c.switchMode(*ev.video, factor)
				continue
			}
			if ev.dropped {
				lastDrop = time.Now()
				a.mu.Lock()
				auto := a.quality == proto.QualityAuto
				a.mu.Unlock()
				if auto && factor > 0.3 {
					factor *= 0.7
				}
				if time.Since(lastKey) > time.Second {
					lastKey = time.Now()
					restart()
				}
				continue
			}
			m := ev.msg
			switch m.T {
			case proto.MsgSize:
				pendingSize = m
				sizeTimer = time.After(150 * time.Millisecond)
			case proto.MsgKeyframe:
				if time.Since(lastKey) > time.Second {
					lastKey = time.Now()
					restart()
				}
			case proto.MsgQuality:
				a.mu.Lock()
				if m.Mode != "" {
					a.quality = m.Mode
				}
				if m.FPS != 0 {
					a.fps = m.FPS
				}
				changed := m.Transport != "" && m.Transport != a.transport
				if m.Transport != "" {
					a.transport = m.Transport
				}
				a.mu.Unlock()
				factor = 1
				if changed {
					c.stopStream()
					c.startStream(factor)
				} else {
					restart()
				}
			case proto.MsgClipboard:
				if err := writeClipboard(m.Text); err != nil {
					log.Printf("clipboard: %v", err)
				}
				c.lastClip = m.Text
			}
		case <-sizeTimer:
			sizeTimer = nil
			if pendingSize != nil {
				c.applySize(pendingSize)
				pendingSize = nil
				restart()
			}
		case <-tick.C:
			f, b := c.frames.Load(), c.bytes.Load()
			lat := 0
			c.vmu.Lock()
			fps := c.params.FPS
			c.vmu.Unlock()
			if fps > 0 {
				lat = 1000 / fps // AU framing delay
			}
			if n := c.queueN.Swap(0); n > 0 {
				lat += int(c.queueSum.Swap(0) / n / 1000)
			}
			c.sendJSON(proto.Stats{T: "stats", FPS: int(f - lastFrames), Kbps: int((b - lastBytes) * 8 / 1000), LatencyMs: lat}, false)
			lastFrames, lastBytes = f, b
			if text, ok := readClipboard(); ok && text != c.lastClip {
				c.lastClip = text
				c.sendJSON(proto.Clipboard{T: "clipboard", Text: text}, false)
			}
			enforceDownloadLimits()
			for _, dl := range a.dl.poll() {
				c.sendJSON(proto.Download{T: "download", Name: dl.Name, Size: dl.Size, ID: dl.ID}, true)
			}
			if factor < 1 && time.Since(lastDrop) > 20*time.Second {
				factor = min(1, factor/0.7)
				lastDrop = time.Now()
				restart()
			}
		}
	}
}

// applySize resizes the browser window (or restarts the browser when
// the scale changes) and the capture region.
func (c *client) applySize(m *proto.ClientMsg) {
	a := c.a
	w, h, s := proto.Screen(m.Width, m.Height, m.Scale)
	a.mu.Lock()
	a.w, a.h, a.scale = w, h, s
	a.mu.Unlock()
	a.x.setScreen(w, h)
	if a.chrome.configure(w, h, s) {
		log.Printf("size %dx%d scale %.2f: browser restarted", w, h, s)
	}
	a.x.fitWindows()
}

func (c *client) startMedia() {
	if c.a.cfg.Audio {
		au, err := startAudio(func(pkt []byte) {
			msg := append(proto.AudioHeader(c.ts()), pkt...)
			c.trySend(outMsg{typ: websocket.MessageBinary, data: msg, enq: time.Now()})
		})
		if err != nil {
			log.Printf("audio: %v", err)
		}
		c.audio = au
	}
	c.startStream(1)
}

func (c *client) stopMedia() {
	c.stopStream()
	c.audio.stop()
}

func (c *client) stopVideo() {
	c.vmu.Lock()
	v := c.video
	c.video = nil
	c.vmu.Unlock()
	v.stop()
}

// startVideo starts the encoder; a GPU encoder that does not deliver a
// frame within 4 s is given up for this container and the CPU is used.
func (c *client) startVideo(factor float64) {
	a := c.a
	a.mu.Lock()
	p := encoderParams{GPU: a.cfg.GPU, Device: a.cfg.Device, W: a.w, H: a.h, FPS: a.fps}
	if a.gpuFailed {
		p.GPU = "cpu"
	}
	p.Kbps = bitrate(p.W, p.H, p.FPS, a.quality, factor)
	a.mu.Unlock()
	for {
		ok := c.tryVideo(p)
		if ok || p.GPU == "cpu" {
			if !ok {
				log.Printf("video: cpu encoder failed")
			}
			return
		}
		log.Printf("video: %s encoder failed, using cpu", p.GPU)
		a.mu.Lock()
		a.gpuFailed = true
		a.mu.Unlock()
		p.GPU = "cpu"
	}
}

func (c *client) tryVideo(p encoderParams) bool {
	c.vmu.Lock()
	c.gen++
	gen := c.gen
	c.params = p
	c.dropping = false
	first := make(chan struct{})
	c.firstAU = first
	c.vmu.Unlock()
	var once sync.Once
	logEncoder(p)
	e, err := startVideo(p, func(au accessUnit) {
		once.Do(func() { close(first) })
		c.onAU(gen, p, au)
	})
	if err != nil {
		log.Printf("video: %v", err)
		return false
	}
	c.vmu.Lock()
	c.video = e
	c.vmu.Unlock()
	select {
	case <-first:
		return true
	case <-e.exited():
		// exited before the first frame
	case <-time.After(4 * time.Second):
	case <-c.ctx.Done():
		return true
	}
	c.vmu.Lock()
	if c.video == e {
		c.video = nil
	}
	c.vmu.Unlock()
	e.stop()
	return false
}

// onAU queues one access unit, preceded by hello for a new encoder.
func (c *client) onAU(gen int, p encoderParams, au accessUnit) {
	c.vmu.Lock()
	defer c.vmu.Unlock()
	if gen != c.gen {
		return
	}
	var msgs []outMsg
	now := time.Now()
	if c.helloGen != gen {
		if au.Codec == "" || !au.Key {
			return // wait for the first key frame with SPS
		}
		c.a.mu.Lock()
		scale := c.a.scale
		c.a.mu.Unlock()
		b, _ := json.Marshal(proto.Hello{T: "hello", Codec: au.Codec, Width: p.W, Height: p.H,
			Scale: scale, GPU: p.GPU, Audio: c.audio != nil})
		msgs = append(msgs, outMsg{typ: websocket.MessageText, data: b, enq: now})
	}
	if c.dropping && !au.Key {
		return
	}
	data := append(proto.VideoHeader(au.Key, c.ts()), au.Data...)
	msgs = append(msgs, outMsg{typ: websocket.MessageBinary, data: data, enq: now, vid: true})
	if !c.trySend(msgs...) {
		if !c.dropping {
			c.dropping = true
			select {
			case c.ctrl <- ctrlEvent{dropped: true}:
			default:
			}
		}
		return
	}
	if c.helloGen != gen {
		c.helloGen = gen
	}
	c.dropping = false
	c.frames.Add(1)
}
