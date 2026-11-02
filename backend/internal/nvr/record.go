package nvr

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// SegmentSeconds is the length of one recording file.
const SegmentSeconds = 300

// CameraStatus is what the recorder knows about one camera.
type CameraStatus struct {
	Recording bool      `json:"recording"`
	Since     time.Time `json:"since,omitempty"`
	Restarts  int       `json:"restarts"`
	// LastError is the last line ffmpeg complained about, passwords removed.
	LastError string `json:"lastError,omitempty"`
}

// Recorder runs one ffmpeg per recording camera and restarts it when it
// ends. Recording copies the stream: nothing is decoded or re-encoded.
type Recorder struct {
	Store *Store
	// FFmpeg is the program to run; empty means "ffmpeg" from PATH.
	FFmpeg string
	// Root returns the folder for recordings, empty while none is chosen.
	Root func() string
	// Backoff is the wait before a restart; zero means 10 seconds.
	Backoff time.Duration

	mu     sync.Mutex
	runs   map[string]*run
	status map[string]CameraStatus
	parent context.Context
}

type run struct {
	cancel context.CancelFunc
	done   chan struct{}
	spec   string
}

func (r *Recorder) program() string {
	if r.FFmpeg != "" {
		return r.FFmpeg
	}
	return "ffmpeg"
}

func (r *Recorder) backoff() time.Duration {
	if r.Backoff > 0 {
		return r.Backoff
	}
	return 10 * time.Second
}

// Status returns the state of every camera that has been started.
func (r *Recorder) Status() map[string]CameraStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]CameraStatus, len(r.status))
	for k, v := range r.status {
		out[k] = v
	}
	return out
}

func (r *Recorder) setStatus(id string, f func(*CameraStatus)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == nil {
		r.status = map[string]CameraStatus{}
	}
	s := r.status[id]
	f(&s)
	r.status[id] = s
}

// Apply brings the running recordings in line with the cameras: starts
// missing ones, restarts changed ones, stops the others. Call it after any
// change; with the service off, pass enabled false.
func (r *Recorder) Apply(ctx context.Context, enabled bool) {
	r.mu.Lock()
	if r.runs == nil {
		r.runs = map[string]*run{}
	}
	r.parent = ctx
	root := ""
	if r.Root != nil {
		root = r.Root()
	}
	want := map[string]string{}
	if enabled && root != "" {
		for _, c := range r.Store.Cameras() {
			if c.Record {
				// The spec changes when anything that matters for the process does.
				want[c.ID] = strings.Join([]string{r.Store.Full(c, false), fmt.Sprint(c.Audio), root}, "\x00")
			}
		}
	}
	var stopping []*run
	for id, cur := range r.runs {
		if spec, ok := want[id]; !ok || spec != cur.spec {
			cur.cancel()
			stopping = append(stopping, cur)
			delete(r.runs, id)
		}
	}
	r.mu.Unlock()
	for _, s := range stopping {
		<-s.done
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, spec := range want {
		if _, ok := r.runs[id]; ok {
			continue
		}
		c, ok := r.Store.Camera(id)
		if !ok {
			continue
		}
		rctx, cancel := context.WithCancel(ctx)
		cur := &run{cancel: cancel, done: make(chan struct{}), spec: spec}
		r.runs[id] = cur
		go r.loop(rctx, cur, c, root)
	}
	for id := range r.status {
		if _, ok := r.runs[id]; !ok {
			s := r.status[id]
			s.Recording = false
			r.status[id] = s
		}
	}
}

// Stop ends every recording and waits for it.
func (r *Recorder) Stop() { r.Apply(context.Background(), false) }

func (r *Recorder) loop(ctx context.Context, cur *run, c Camera, root string) {
	defer close(cur.done)
	for ctx.Err() == nil {
		r.setStatus(c.ID, func(s *CameraStatus) { s.Recording, s.Since = true, time.Now() })
		err := r.runOnce(ctx, c, root)
		r.setStatus(c.ID, func(s *CameraStatus) { s.Recording = false })
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.Warn("nvr: recording ended", "camera", c.ID, "err", Redact(err.Error()))
		}
		r.setStatus(c.ID, func(s *CameraStatus) { s.Restarts++ })
		select {
		case <-ctx.Done():
			return
		case <-time.After(r.backoff()):
		}
	}
}

// args is the ffmpeg command line: copy video (and audio only if asked),
// cut into fragmented MP4 files that stay playable even if cut short.
func (r *Recorder) args(c Camera, root string) []string {
	dir := filepath.Join(root, c.ID)
	args := []string{
		"-hide_banner", "-nostdin", "-loglevel", "warning",
		"-rtsp_transport", "tcp", "-timeout", "10000000",
		"-i", r.Store.Full(c, false),
		"-map", "0:v:0",
	}
	if c.Audio {
		args = append(args, "-map", "0:a:0?", "-c:a", "aac")
	} else {
		args = append(args, "-an")
	}
	return append(args,
		"-c:v", "copy",
		"-f", "segment", "-segment_time", fmt.Sprint(SegmentSeconds), "-segment_atclocktime", "1",
		"-reset_timestamps", "1", "-strftime", "1", "-strftime_mkdir", "1",
		"-segment_format", "mp4", "-segment_format_options", "movflags=+frag_keyframe+empty_moov+default_base_moof",
		filepath.Join(dir, "%Y-%m-%d", "%H-%M-%S.mp4"),
	)
}

func (r *Recorder) runOnce(ctx context.Context, c Camera, root string) error {
	if err := os.MkdirAll(filepath.Join(root, c.ID), 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, r.program(), r.args(c, root)...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) } // lets ffmpeg close the file
	cmd.WaitDelay = 15 * time.Second
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	secret := r.Store.Password(c.ID)
	go func() {
		sc := bufio.NewScanner(io.LimitReader(stderr, 4<<20))
		for sc.Scan() {
			line := scrub(sc.Text(), secret)
			r.setStatus(c.ID, func(s *CameraStatus) { s.LastError = line })
		}
	}()
	return cmd.Wait()
}

var urlCreds = regexp.MustCompile(`(rtsps?://[^:/@\s]*:)[^@\s]*@`)

// scrub removes the password from a line, wherever ffmpeg printed it.
func scrub(line, secret string) string {
	line = urlCreds.ReplaceAllString(line, "${1}REDACTED@")
	if len(secret) >= 3 {
		line = strings.ReplaceAll(line, secret, "REDACTED")
	}
	if len(line) > 300 {
		line = line[:300]
	}
	return line
}
