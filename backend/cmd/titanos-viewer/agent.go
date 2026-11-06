package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/sys/unix"

	"github.com/ra5on/TitanOS/backend/internal/viewer/proto"
)

type agent struct {
	cfg    config
	ctx    context.Context
	cancel context.CancelFunc
	x      *xctl
	chrome *chromium
	xvfb   *proc
	pulse  *proc
	dl     *downloads

	mu     sync.Mutex
	client *client
	// screen state (guarded by mu)
	w, h      int
	scale     float64
	fps       int
	quality   string
	gpuFailed bool
}

func newAgent(cfg config) (*agent, error) {
	return &agent{cfg: cfg, w: cfg.Width, h: cfg.Height, scale: cfg.Scale, fps: cfg.FPS, quality: cfg.Quality}, nil
}

// keyOK compares the session key in constant time.
func keyOK(want, got string) bool {
	if want == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

func setPipeSize(f *os.File, n int) {
	_, _ = unix.FcntlInt(f.Fd(), unix.F_SETPIPE_SZ, n)
}

func waitFor(path string, d time.Duration) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("%s did not appear", path)
}

// prepareDirs creates the writable layout on /tmp and /profile.
func prepareDirs() error {
	for _, d := range []string{runDir, homeDir + "/.config/gtk-3.0", homeDir + "/.cache", pulseDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	if err := os.MkdirAll("/tmp/.X11-unix", 0o1777); err != nil {
		return err
	}
	_ = os.Chmod("/tmp/.X11-unix", 0o1777)
	for _, d := range []string{chromeProfile, downloadDir, uploadDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("profile not writable: %w", err)
		}
	}
	// The GTK file chooser opens in $HOME: offer the upload and download
	// folders there and as bookmarks.
	_ = os.Symlink(uploadDir, filepath.Join(homeDir, "Hochgeladen"))
	_ = os.Symlink(downloadDir, filepath.Join(homeDir, "Downloads"))
	bm := "file://" + uploadDir + " Hochgeladen\nfile://" + downloadDir + " Downloads\n"
	return os.WriteFile(filepath.Join(homeDir, ".config/gtk-3.0/bookmarks"), []byte(bm), 0o600)
}

func (a *agent) start(parent context.Context) error {
	a.ctx, a.cancel = context.WithCancel(parent)
	if err := prepareDirs(); err != nil {
		return err
	}
	if err := checkPolicyFile(proto.PolicyPath, a.cfg.Origin, 0); err != nil {
		return fmt.Errorf("refusing to start the browser: Chromium policy: %w", err)
	}
	var err error
	a.xvfb, err = startProc("xvfb", []string{"/usr/bin/Xvfb", display,
		"-screen", "0", fmt.Sprintf("%dx%dx24", xvfbWidth, xvfbHeight),
		"-nolisten", "tcp", "-noreset", "-dpi", "96", "+extension", "RANDR"}, baseEnv(), nil, true)
	if err != nil {
		return fmt.Errorf("xvfb: %w", err)
	}
	if err := waitFor("/tmp/.X11-unix/X"+displayNum, 10*time.Second); err != nil {
		return err
	}
	if a.x, err = connectX(); err != nil {
		return fmt.Errorf("x11: %w", err)
	}
	a.x.setScreen(a.w, a.h)
	go func() { // Xvfb is essential
		<-a.xvfb.done
		log.Printf("xvfb exited: %v", a.xvfb.err)
		a.cancel()
	}()
	if a.cfg.Audio {
		a.pulse, err = startProc("pulseaudio", []string{"/usr/bin/pulseaudio", "-n", "-F", pulseConfig,
			"--daemonize=no", "--exit-idle-time=-1", "--use-pid-file=no", "--system=no",
			"--disallow-exit", "--disable-shm=yes", "--high-priority=no", "--realtime=no",
			"--log-target=stderr", "--log-level=error"}, baseEnv(), nil, true)
		if err != nil {
			log.Printf("pulseaudio: %v (audio off)", err)
			a.cfg.Audio = false
		} else if err := waitFor(pulseSocket, 10*time.Second); err != nil {
			log.Printf("pulseaudio: %v (audio off)", err)
			a.cfg.Audio = false
		}
	}
	go warmGstRegistry()
	clearUploads()
	pruneDownloads(24 * time.Hour)
	a.dl = newDownloads()
	a.chrome = newChromium(a.cfg, a.w, a.h, a.scale)
	a.chrome.closeWindows = a.x.closeWindows
	go a.chrome.run(a.ctx)
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-t.C:
				a.x.fitWindows()
			}
		}
	}()
	a.x.watchCursor(func(shape string) {
		if c := a.current(); c != nil {
			c.sendJSON(proto.Cursor{T: "cursor", Shape: shape}, false)
		}
	})
	return nil
}

func (a *agent) shutdown() {
	if a.cancel != nil {
		a.cancel()
	}
	if a.chrome != nil {
		a.chrome.stop()
	}
	a.pulse.stop(2 * time.Second)
	a.xvfb.stop(2 * time.Second)
	clearUploads()
}

func (a *agent) current() *client {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.client
}

// closeClient ends the current stream with a bye message.
func (a *agent) closeClient(reason, text string) {
	if c := a.current(); c != nil {
		c.close(reason, text)
		<-c.done
	}
}

// requestKey is the session key of a request. Only the header counts: a
// key in the URL would end up in logs.
func requestKey(r *http.Request) string {
	return r.Header.Get("X-Viewer-Key")
}

func (a *agent) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /stream", func(w http.ResponseWriter, r *http.Request) {
		if !keyOK(a.cfg.Key, requestKey(r)) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
		if err != nil {
			return
		}
		a.serveClient(conn)
	})
	mux.HandleFunc("GET /downloads/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !keyOK(a.cfg.Key, requestKey(r)) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		dl, ok := a.dl.get(r.PathValue("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(dl.Path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", contentDisposition(dl.Name))
		http.ServeContent(w, r, "", info.ModTime(), f)
	})
	return mux
}

// contentDisposition builds an attachment header with an ASCII fallback
// and the UTF-8 name (RFC 6266/5987).
func contentDisposition(name string) string {
	var ascii strings.Builder
	for _, r := range name {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			ascii.WriteRune('_')
		} else {
			ascii.WriteRune(r)
		}
	}
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii.String(), url.PathEscape(name))
}

// serveClient runs one stream; a newer client replaces the older one.
func (a *agent) serveClient(conn *websocket.Conn) {
	c := newClient(a, conn)
	a.mu.Lock()
	old := a.client
	a.client = c
	a.mu.Unlock()
	if old != nil {
		old.close(proto.ByeReplaced, "")
		<-old.done
	}
	c.run()
	a.mu.Lock()
	if a.client == c {
		a.client = nil
	}
	a.mu.Unlock()
}

var errClosed = errors.New("client closed")
