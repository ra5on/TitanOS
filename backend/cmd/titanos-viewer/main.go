// Command titanos-viewer is the agent inside the titanos-viewer container
// (TitanOS remote view, docs/FERNANSICHT.md). It supervises Xvfb,
// PulseAudio and Chromium, captures the screen with GStreamer and serves
// the stream protocol on :7300 to titand.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/viewer/proto"
)

// Download and upload folders (variables for tests).
var (
	downloadDir = "/profile/Downloads"
	uploadDir   = "/profile/Hochgeladen"
)

// Paths inside the container. Only /profile and /tmp are writable.
const (
	display       = ":99"
	displayNum    = "99"
	profileDir    = "/profile"
	chromeProfile = "/profile/chromium"
	runDir        = "/tmp/titanos-viewer"
	homeDir       = "/tmp/home"
	pulseDir      = "/tmp/pulse"
	pulseSocket   = "/tmp/pulse/native"
	pulseConfig   = "/etc/titanos-viewer/pulse.pa"
	// The X screen has a fixed size: Xvfb cannot change it via RandR.
	// The browser window and the capture region are resized instead.
	xvfbWidth  = proto.MaxWidth
	xvfbHeight = proto.MaxHeight
)

type config struct {
	URL     string
	Origin  string
	Key     string
	GPU     string // vaapi | nvenc | cpu
	Device  string // optional /dev/dri/renderD*
	Audio   bool
	FPS     int
	Quality string
	Width   int
	Height  int
	Scale   float64
	Sandbox bool
	Listen  string
}

func loadConfig() (config, error) {
	c := config{
		URL:     os.Getenv("VIEWER_URL"),
		Key:     os.Getenv("VIEWER_KEY"),
		GPU:     strings.ToLower(strings.TrimSpace(os.Getenv("VIEWER_GPU"))),
		Device:  os.Getenv("VIEWER_GPU_DEVICE"),
		Audio:   os.Getenv("VIEWER_AUDIO") == "1",
		FPS:     30,
		Quality: proto.QualityAuto,
		Width:   1280,
		Height:  800,
		Scale:   1,
		Sandbox: os.Getenv("VIEWER_SANDBOX") == "1",
		Listen:  fmt.Sprintf(":%d", proto.Port),
	}
	origin, err := appOrigin(c.URL)
	if err != nil {
		return c, err
	}
	c.Origin = origin
	if len(c.Key) < 16 {
		return c, errors.New("VIEWER_KEY missing or shorter than 16 characters")
	}
	switch c.GPU {
	case "", "cpu", "off":
		c.GPU = "cpu"
	case "vaapi", "nvenc":
	default:
		return c, fmt.Errorf("VIEWER_GPU %q: want vaapi, nvenc or cpu", c.GPU)
	}
	if c.Device != "" && !strings.HasPrefix(c.Device, "/dev/dri/renderD") {
		return c, errors.New("VIEWER_GPU_DEVICE must be /dev/dri/renderD*")
	}
	if v := os.Getenv("VIEWER_FPS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 5 || n > 60 {
			return c, errors.New("VIEWER_FPS must be 5..60")
		}
		c.FPS = n
	}
	if v := os.Getenv("VIEWER_QUALITY"); v != "" {
		switch v {
		case proto.QualityAuto, proto.QualityHigh, proto.QualityBalanced, proto.QualityLow:
			c.Quality = v
		default:
			return c, errors.New("VIEWER_QUALITY must be auto, high, balanced or low")
		}
	}
	if v := os.Getenv("VIEWER_SIZE"); v != "" { // e.g. 1280x800@2
		var w, h int
		s := 1.0
		spec, scale, _ := strings.Cut(v, "@")
		if _, err := fmt.Sscanf(spec, "%dx%d", &w, &h); err != nil {
			return c, errors.New("VIEWER_SIZE must look like 1280x800 or 1280x800@2")
		}
		if scale != "" {
			if s, err = strconv.ParseFloat(scale, 64); err != nil {
				return c, errors.New("VIEWER_SIZE scale invalid")
			}
		}
		c.Width, c.Height, c.Scale = proto.Screen(w, h, s)
	}
	return c, nil
}

// appOrigin validates VIEWER_URL and returns its origin (scheme://host[:port]).
func appOrigin(raw string) (string, error) { return proto.AppOrigin(raw) }

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetPrefix("titanos-viewer: ")
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	a, err := newAgent(cfg)
	if err != nil {
		log.Fatalf("start: %v", err)
	}
	if err := a.start(ctx); err != nil {
		a.shutdown()
		log.Fatalf("start: %v", err)
	}
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           a.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http: %v", err)
			stop()
		}
	}()
	log.Printf("ready on %s (gpu=%s audio=%v)", cfg.Listen, cfg.GPU, cfg.Audio)
	<-a.ctx.Done() // SIGTERM/SIGINT, or Xvfb died
	log.Printf("shutting down")
	a.closeClient(proto.ByeStopped, "")
	sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_ = srv.Shutdown(sctx)
	cancel()
	a.shutdown()
}
