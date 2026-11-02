package nvr

import (
	"bytes"
	"context"
	"fmt"
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
	// LoginRefused is set while logins are paused after the camera refused one.
	LoginRefused bool `json:"loginRefused,omitempty"`
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
	// ExtraEnv is added to ffmpeg's environment; for tests only.
	ExtraEnv []string

	mu     sync.Mutex
	runs   map[string]*run
	status map[string]CameraStatus
	logins map[string]*loginState
	parent context.Context
}

type run struct {
	cancel context.CancelFunc
	done   chan struct{}
	spec   string
	// pictures counts event pictures still being written (motion check).
	pictures sync.WaitGroup
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
			if c.Mode() != "off" {
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
		// The volume may have gone: never write into a bare mount point.
		if r.Root != nil && r.Root() != root {
			r.setStatus(c.ID, func(s *CameraStatus) { s.Recording, s.LastError = false, "recordings folder not available" })
			return
		}
		if wait := r.blockedFor(c.ID); wait > 0 {
			if !sleep(ctx, wait) {
				return
			}
			continue
		}
		r.setStatus(c.ID, func(s *CameraStatus) { s.Recording, s.Since = true, time.Now() })
		started := time.Now()
		refused, err := r.runOnce(ctx, c, root)
		r.setStatus(c.ID, func(s *CameraStatus) { s.Recording = false })
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.Warn("nvr: recording ended", "camera", c.ID, "err", Redact(err.Error()))
		}
		r.noteLogin(c.ID, refused, time.Since(started))
		r.setStatus(c.ID, func(s *CameraStatus) { s.Restarts++ })
		if !sleep(ctx, r.backoff()) {
			return
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// After a refused login the camera is left alone for a while, doubling up to
// half an hour: cameras lock the account after a few wrong logins, and a
// password changed on the camera would otherwise be tried every few seconds
// by the recorder, the motion check and every snapshot.
const (
	firstLoginPause = time.Minute
	maxLoginPause   = 30 * time.Minute
)

type loginState struct {
	pause time.Duration
	until time.Time
}

// noteLogin records how a run ended: refused logins lengthen the pause,
// a run that lasted a while clears it.
func (r *Recorder) noteLogin(id string, refused bool, ran time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.logins == nil {
		r.logins = map[string]*loginState{}
	}
	st := r.logins[id]
	if st == nil {
		st = &loginState{}
		r.logins[id] = st
	}
	switch {
	case refused:
		st.pause = min(max(st.pause*2, firstLoginPause), maxLoginPause)
		st.until = time.Now().Add(st.pause)
		if r.status == nil {
			r.status = map[string]CameraStatus{}
		}
		s := r.status[id]
		s.LoginRefused = true
		r.status[id] = s
	case ran > time.Minute:
		st.pause, st.until = 0, time.Time{}
		s := r.status[id]
		s.LoginRefused = false
		r.status[id] = s
	}
}

// blockedFor is how long the camera must still be left alone.
func (r *Recorder) blockedFor(id string) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if st := r.logins[id]; st != nil {
		return time.Until(st.until)
	}
	return 0
}

// Blocked reports whether logins to the camera are paused (for the motion
// check and snapshots, which use the same password).
func (r *Recorder) Blocked(id string) bool { return r.blockedFor(id) > 0 }

// ClearLogin lifts a pause, after the password or address was changed.
func (r *Recorder) ClearLogin(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.logins, id)
	if s, ok := r.status[id]; ok {
		s.LoginRefused = false
		r.status[id] = s
	}
}

// args is the ffmpeg command line: copy video (and audio only if asked),
// cut into fragmented MP4 files that stay playable even if cut short. File
// names are in UTC (the process runs with TZ=UTC), so the hour that repeats
// when the clocks go back does not overwrite recordings.
func (r *Recorder) args(c Camera, root, input string) []string {
	dir := filepath.Join(root, c.ID)
	args := []string{
		"-hide_banner", "-nostdin", "-loglevel", "warning",
		"-rtsp_transport", "tcp", "-timeout", "10000000",
		"-i", input,
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

// ffmpegEnv is the environment of every ffmpeg the NVR starts.
func ffmpegEnv() []string { return []string{"PATH=/usr/bin:/bin", "LC_ALL=C", "TZ=UTC"} }

// runOnce runs ffmpeg once. refused is set when the camera turned the login
// down or reported a locked account.
func (r *Recorder) runOnce(ctx context.Context, c Camera, root string) (refused bool, err error) {
	// The folder of the recordings must exist (made when the NVR started on
	// this volume); only the camera's own folder is made here.
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return false, fmt.Errorf("recordings folder not available")
	}
	if err := os.MkdirAll(filepath.Join(root, c.ID), 0o755); err != nil {
		return false, err
	}
	input, err := r.Store.Pinned(ctx, c, false)
	if err != nil {
		r.setStatus(c.ID, func(s *CameraStatus) { s.LastError = scrub(err.Error(), "") })
		return false, err
	}
	cmd := exec.CommandContext(ctx, r.program(), r.args(c, root, input)...)
	cmd.Env = append(ffmpegEnv(), r.ExtraEnv...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) } // lets ffmpeg close the file
	cmd.WaitDelay = 15 * time.Second
	tail := &lastLine{secret: r.Store.Password(c.ID), set: func(line string) {
		r.setStatus(c.ID, func(s *CameraStatus) { s.LastError = line })
	}}
	cmd.Stderr = tail
	err = cmd.Run()
	return tail.refused, err
}

// lastLine takes ffmpeg's messages without ever blocking it: it keeps only
// the latest line (passwords removed) and notes refused logins.
type lastLine struct {
	mu      sync.Mutex
	secret  string
	partial []byte
	refused bool
	set     func(string)
}

func (l *lastLine) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.partial = append(l.partial, p...)
	for {
		i := bytes.IndexByte(l.partial, '\n')
		if i < 0 {
			break
		}
		l.line(string(l.partial[:i]))
		l.partial = l.partial[i+1:]
	}
	if len(l.partial) > 4096 { // a line without end: keep its end only
		l.partial = l.partial[len(l.partial)-1024:]
	}
	return len(p), nil
}

func (l *lastLine) line(text string) {
	text = strings.TrimRight(text, "\r")
	if text == "" {
		return
	}
	if loginRefused(text) {
		l.refused = true
	}
	if l.set != nil {
		l.set(scrub(text, l.secret))
	}
}

// loginRefused recognises ffmpeg's report of a rejected or locked login.
func loginRefused(line string) bool {
	low := strings.ToLower(line)
	return strings.Contains(low, "401 unauthorized") || strings.Contains(low, "403 forbidden") ||
		strings.Contains(low, "server returned 401") || strings.Contains(low, "server returned 403") ||
		strings.Contains(low, "locked")
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
