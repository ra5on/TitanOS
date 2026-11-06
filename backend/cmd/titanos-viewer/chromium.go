package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// policies returns the managed Chromium policies for the app origin.
// /etc/chromium/policies/managed is a symlink (made at image build time)
// to policyDir on the tmpfs, because the root file system is read-only
// and the allowed origin is only known at start.
func policies(origin string) map[string]any {
	return map[string]any{
		"URLBlocklist":                        []string{"*", "file://*", "chrome://*", "chrome-extension://*", "view-source:*", "devtools://*"},
		"URLAllowlist":                        []string{origin, "about:blank"},
		"DeveloperToolsAvailability":          2,
		"DownloadDirectory":                   downloadDir,
		"PromptForDownloadLocation":           false,
		"DownloadRestrictions":                0,
		"AllowFileSelectionDialogs":           true,
		"PasswordManagerEnabled":              true,
		"SyncDisabled":                        true,
		"BrowserSignin":                       0,
		"BrowserAddPersonEnabled":             false,
		"BrowserGuestModeEnabled":             false,
		"DefaultBrowserSettingEnabled":        false,
		"TranslateEnabled":                    false,
		"ExtensionInstallBlocklist":           []string{"*"},
		"BlockExternalExtensions":             true,
		"MetricsReportingEnabled":             false,
		"SearchSuggestEnabled":                false,
		"SafeBrowsingProtectionLevel":         0,
		"NetworkPredictionOptions":            2,
		"DnsOverHttpsMode":                    "off",
		"BackgroundModeEnabled":               false,
		"PrintingEnabled":                     false,
		"DefaultNotificationsSetting":         2,
		"DefaultGeolocationSetting":           2,
		"AudioCaptureAllowed":                 false,
		"VideoCaptureAllowed":                 false,
		"ScreenCaptureAllowed":                false,
		"AutofillCreditCardEnabled":           false,
		"ShowFullUrlsInAddressBar":            false,
		"PromotionalTabsEnabled":              false,
		"PrivacySandboxPromptEnabled":         false,
		"PrivacySandboxAdTopicsEnabled":       false,
		"PrivacySandboxSiteEnabledAdsEnabled": false,
		"PrivacySandboxAdMeasurementEnabled":  false,
		"CloudReportingEnabled":               false,
		"MediaRouterCastAllowAllIPs":          false,
		"EnableMediaRouter":                   false,
		"HardwareAccelerationModeEnabled":     true,
		"BrowserLabsEnabled":                  false,
		"FeedbackSurveysEnabled":              false,
		"UserFeedbackAllowed":                 false,
		"GenAiDefaultSettings":                2,
	}
}

func writePolicies(origin string) error {
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(policies(origin), "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(policyDir, ".titanos.json.tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(policyDir, "titanos.json"))
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
	return append(args, cfg.URL)
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
	once    sync.Once
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
				p.stop(5 * time.Second)
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
		p.stop(5 * time.Second)
	}
}

func (c *chromium) stop() {
	c.mu.Lock()
	c.stopped = true
	p := c.p
	c.mu.Unlock()
	if p != nil {
		p.stop(5 * time.Second)
	}
}
