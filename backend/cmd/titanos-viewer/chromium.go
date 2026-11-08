package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ra5on/TitanOS/backend/internal/viewer/proto"
)

// checkPolicyFile verifies the managed policy titand bind-mounts
// read-only (proto.PolicyPath): a regular file owned by rootUID in a
// directory owned by rootUID, neither writable by group/others nor by
// this process, that locks the browser to the app origin. Chromium is
// not started otherwise: a policy the browser (or the file dialog) could
// change would undo the URL lock.
func checkPolicyFile(path, origin string, rootUID uint32) error {
	dir := filepath.Dir(path)
	for _, p := range []string{dir, path} {
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("%s: no owner information", p)
		}
		switch {
		case p == dir && !info.IsDir():
			return fmt.Errorf("%s is not a directory", p)
		case p == path && !info.Mode().IsRegular():
			return fmt.Errorf("%s is not a regular file", p)
		case st.Uid != rootUID:
			return fmt.Errorf("%s is owned by uid %d, want %d", p, st.Uid, rootUID)
		case info.Mode().Perm()&0o022 != 0:
			return fmt.Errorf("%s is writable by group or others (%v)", p, info.Mode().Perm())
		}
		if unix.Access(p, unix.W_OK) == nil {
			return fmt.Errorf("%s is writable by the viewer", p)
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return err
	}
	return proto.CheckPolicy(data, origin)
}

// chromeArgs builds the Chromium command line.
func chromeArgs(cfg config, w, h int, scale float64) []string {
	args := []string{
		"/usr/bin/chromium",
		"--kiosk",
		"--user-data-dir=" + chromeProfile,
		"--window-position=0,0",
		fmt.Sprintf("--window-size=%d,%d", w, h),
		"--force-device-scale-factor=" + strconv.FormatFloat(scale, 'f', 2, 64),
		"--ozone-platform=x11",
		"--no-first-run",
		"--no-default-browser-check",
		"--password-store=basic",
		"--autoplay-policy=no-user-gesture-required",
		"--disable-background-networking",
		"--disable-component-update",
		"--disable-sync",
		"--disable-breakpad",
		"--noerrdialogs",
		"--hide-crash-restore-bubble",
		"--disable-session-crashed-bubble",
		"--disable-features=Translate,MediaRouter,DialMediaRouteProvider,OptimizationHints,InterestFeedContentSuggestions,AutofillServerCommunication,CertificateTransparencyComponentUpdater",
		"--test-type", // no "unsupported flag" info bar for --no-sandbox
		"--lang=" + envOr("VIEWER_LANG", "de"),
	}
	if !cfg.Sandbox {
		// The Chromium sandbox needs user namespaces, which the container
		// flags (cap-drop ALL, default seccomp/AppArmor) deny, and the
		// setuid helper is incompatible with no-new-privileges. The
		// container is the isolation boundary.
		args = append(args, "--no-sandbox")
	}
	if cfg.GPU == "cpu" {
		args = append(args, "--disable-gpu")
	}
	if cfg.TrustKey != "" {
		// Only this one key: every other certificate is checked as before.
		args = append(args, "--ignore-certificate-errors-spki-list="+cfg.TrustKey)
	}
	return append(args, cfg.URL)
}

// trustKey accepts only one base64 SHA-256 (44 characters, 32 bytes):
// anything else (a list, a flag) is dropped.
func trustKey(v string) string {
	raw, err := base64.StdEncoding.DecodeString(v)
	if err != nil || len(raw) != 32 || len(v) != 44 {
		return ""
	}
	return v
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// clearProfileLocks removes Chromium's singleton files. The container
// hostname changes for every container, so a stale lock would make
// Chromium refuse the profile ("in use on another computer").
func clearProfileLocks() {
	for _, n := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		_ = os.Remove(filepath.Join(chromeProfile, n))
	}
}

// chromium supervises the browser process and restarts it when it exits
// (e.g. after Ctrl+W in kiosk mode or a crash).
type chromium struct {
	cfg config

	mu      sync.Mutex
	p       *proc
	w, h    int
	scale   float64
	stopped bool
	kick    chan struct{}
	ready   chan struct{} // closed by the first size or after firstWait
	// closeWindows asks all browser windows to close (WM_DELETE_WINDOW).
	closeWindows func()
	once         sync.Once
}

// firstWait is how long the first start waits for the client's size, so
// a HiDPI client does not cause an immediate restart with a new scale.
const firstWait = 3 * time.Second

func newChromium(cfg config, w, h int, scale float64) *chromium {
	return &chromium{cfg: cfg, w: w, h: h, scale: scale, kick: make(chan struct{}, 1), ready: make(chan struct{})}
}

func (c *chromium) run(ctx context.Context) {
	select {
	case <-c.ready:
	case <-time.After(firstWait):
	case <-ctx.Done():
		return
	}
	c.once.Do(func() { close(c.ready) })
	backoff := time.Second
	for {
		c.mu.Lock()
		if c.stopped || ctx.Err() != nil {
			c.mu.Unlock()
			return
		}
		clearProfileLocks()
		env := append(baseEnv(), "GTK_THEME=Adwaita", "GSETTINGS_BACKEND=memory")
		p, err := startProc("chromium", chromeArgs(c.cfg, c.w, c.h, c.scale), env, nil, false)
		if err != nil {
			c.mu.Unlock()
			log.Printf("chromium: %v", err)
		} else {
			c.p = p
			c.mu.Unlock()
			started := time.Now()
			select {
			case <-p.done:
				if time.Since(started) > 30*time.Second {
					backoff = time.Second
				}
				log.Printf("chromium exited (%v), restarting", p.err)
			case <-ctx.Done():
				c.end(p)
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-c.kick:
			backoff = time.Second
			continue
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

// configure sets the window size and scale. Before the first start the
// values are just taken (and the start is released); afterwards a new
// scale restarts the browser: the page reloads, the profile and
// persistent logins are kept (session cookies are lost).
func (c *chromium) configure(w, h int, scale float64) (restarted bool) {
	c.mu.Lock()
	if c.p == nil {
		c.w, c.h, c.scale = w, h, scale
		c.mu.Unlock()
		c.once.Do(func() { close(c.ready) })
		return false
	}
	if scale == c.scale {
		c.w, c.h = w, h
		c.mu.Unlock()
		return false
	}
	c.mu.Unlock()
	c.restart(w, h, scale)
	return true
}

// end closes the browser windows (a normal browser shutdown that writes
// cookies and the profile; a SIGTERM makes Chromium skip writing the
// cookie database), waits up to 5 s, then stops the process group.
func (c *chromium) end(p *proc) {
	if c.closeWindows != nil && !p.exited() {
		c.closeWindows()
		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
			log.Printf("chromium did not close its windows, terminating it")
		}
	}
	p.stop(5 * time.Second)
}

// restart restarts the browser with a new size and scale.
func (c *chromium) restart(w, h int, scale float64) {
	c.mu.Lock()
	c.w, c.h, c.scale = w, h, scale
	p := c.p
	c.mu.Unlock()
	select {
	case c.kick <- struct{}{}:
	default:
	}
	if p != nil {
		c.end(p)
	}
}

func (c *chromium) stop() {
	c.mu.Lock()
	c.stopped = true
	p := c.p
	c.mu.Unlock()
	if p != nil {
		c.end(p)
	}
}
