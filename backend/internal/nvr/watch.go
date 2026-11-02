package nvr

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// WatchStatus is what the motion check knows about a camera.
type WatchStatus struct {
	Watching bool      `json:"watching"`
	Motion   bool      `json:"motion"`
	LastAt   time.Time `json:"lastAt,omitempty"`
}

// Watcher runs the motion check: per camera one ffmpeg that decodes the
// sub stream to small gray frames at two a second. Events are stored beside
// the recordings, with a picture of the moment motion began.
type Watcher struct {
	Store *Store
	Rec   *Recorder
	Root  func() string
	// Detector, if set, labels the picture of each event.
	Detector Detector
	// OnEvent is called when an event starts and ends (end set).
	OnEvent func(Camera, Event)
	// Backoff is the wait before a restart; zero means 10 seconds.
	Backoff time.Duration

	mu     sync.Mutex
	runs   map[string]*run
	status map[string]WatchStatus
}

func (w *Watcher) backoff() time.Duration {
	if w.Backoff > 0 {
		return w.Backoff
	}
	return 10 * time.Second
}

// Status returns the state of every watched camera.
func (w *Watcher) Status() map[string]WatchStatus {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make(map[string]WatchStatus, len(w.status))
	for k, v := range w.status {
		out[k] = v
	}
	return out
}

func (w *Watcher) set(id string, f func(*WatchStatus)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == nil {
		w.status = map[string]WatchStatus{}
	}
	s := w.status[id]
	f(&s)
	w.status[id] = s
}

// Apply starts the check for cameras with Detect on, stops it for others.
func (w *Watcher) Apply(ctx context.Context, enabled bool) {
	w.mu.Lock()
	if w.runs == nil {
		w.runs = map[string]*run{}
	}
	root := ""
	if w.Root != nil {
		root = w.Root()
	}
	want := map[string]string{}
	if enabled && root != "" {
		for _, c := range w.Store.Cameras() {
			if c.Watched() {
				want[c.ID] = strings.Join([]string{w.Store.Full(c, true), itoa(c.Sensitivity), root}, "\x00")
			}
		}
	}
	var stopping []*run
	for id, cur := range w.runs {
		if spec, ok := want[id]; !ok || spec != cur.spec {
			cur.cancel()
			stopping = append(stopping, cur)
			delete(w.runs, id)
		}
	}
	w.mu.Unlock()
	for _, s := range stopping {
		<-s.done
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, spec := range want {
		if _, ok := w.runs[id]; ok {
			continue
		}
		c, ok := w.Store.Camera(id)
		if !ok {
			continue
		}
		rctx, cancel := context.WithCancel(ctx)
		cur := &run{cancel: cancel, done: make(chan struct{}), spec: spec}
		w.runs[id] = cur
		go w.loop(rctx, cur, c, root)
	}
	for id, s := range w.status {
		if _, ok := w.runs[id]; !ok {
			s.Watching, s.Motion = false, false
			w.status[id] = s
		}
	}
}

func (w *Watcher) loop(ctx context.Context, cur *run, c Camera, root string) {
	defer close(cur.done)
	for ctx.Err() == nil {
		w.set(c.ID, func(s *WatchStatus) { s.Watching = true })
		err := w.once(ctx, c, root)
		w.set(c.ID, func(s *WatchStatus) { s.Watching, s.Motion = false, false })
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.Warn("nvr: motion check ended", "camera", c.ID, "err", Redact(err.Error()))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.backoff()):
		}
	}
}

func (w *Watcher) once(ctx context.Context, c Camera, root string) error {
	prog := w.Rec.program()
	cmd := exec.CommandContext(ctx, prog,
		"-hide_banner", "-nostdin", "-loglevel", "error",
		"-rtsp_transport", "tcp", "-timeout", "10000000",
		"-i", w.Store.Full(c, true),
		"-an", "-vf", "fps="+itoa(MotionFPS)+",scale="+itoa(MotionW)+":"+itoa(MotionH)+",format=gray",
		"-f", "rawvideo", "pipe:1")
	cmd.Cancel = func() error { return cmd.Process.Kill() }
	cmd.WaitDelay = 3 * time.Second
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	m := &Motion{Sensitivity: c.Sensitivity}
	r := bufio.NewReaderSize(out, 4*MotionW*MotionH)
	frame := make([]byte, MotionW*MotionH)
	var open *Event
	for {
		if _, err := io.ReadFull(r, frame); err != nil {
			break
		}
		now := time.Now()
		ev := m.Feed(frame, now)
		if ev == nil {
			continue
		}
		if ev.Start {
			e := Event{ID: eventID(ev.At), Camera: c.ID, Kind: "motion", Start: ev.At}
			open = &e
			w.set(c.ID, func(s *WatchStatus) { s.Motion, s.LastAt = true, ev.At })
			w.begin(ctx, c, root, open)
		} else if open != nil {
			open.End = ev.At
			*open = w.finish(root, *open)
			w.set(c.ID, func(s *WatchStatus) { s.Motion = false })
			if w.OnEvent != nil {
				w.OnEvent(c, *open)
			}
			open = nil
		}
	}
	if open != nil { // the stream ended during an event
		open.End = time.Now()
		*open = w.finish(root, *open)
		if w.OnEvent != nil {
			w.OnEvent(c, *open)
		}
	}
	return cmd.Wait()
}

// begin stores the event at once and adds the picture and labels in the
// background: asking the camera for a picture takes a moment.
func (w *Watcher) begin(ctx context.Context, c Camera, root string, e *Event) {
	_ = WriteEvent(root, *e)
	if w.OnEvent != nil {
		w.OnEvent(c, *e)
	}
	snapshot := *e
	go func() {
		jpeg, err := w.Rec.Snapshot(ctx, c)
		if err != nil || len(jpeg) == 0 {
			return
		}
		snapshot.Snapshot = true
		if w.Detector != nil {
			if labels, err := w.Detector.Detect(jpeg); err == nil {
				snapshot.Labels = labels
			}
		}
		if WriteSnapshot(root, snapshot, jpeg) != nil {
			return
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		// Keep the end time if the event already ended.
		if cur, ok := readEvent(root, snapshot); ok && !cur.End.IsZero() {
			snapshot.End = cur.End
		}
		_ = WriteEvent(root, snapshot)
	}()
}

// finish stores the end of an event, keeping the picture and labels the
// background step may have added meanwhile.
func (w *Watcher) finish(root string, e Event) Event {
	w.mu.Lock()
	defer w.mu.Unlock()
	if cur, ok := readEvent(root, e); ok {
		cur.End = e.End
		e = cur
	}
	_ = WriteEvent(root, e)
	return e
}

func readEvent(root string, e Event) (Event, bool) {
	data, err := os.ReadFile(eventPath(root, e) + ".json")
	if err != nil {
		return Event{}, false
	}
	var out Event
	return out, jsonUnmarshal(data, &out) == nil
}
