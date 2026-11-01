// Package ups connects the NAS to an uninterruptible power supply through
// NUT (Network UPS Tools): one attached by USB or serial cable, or one that
// another machine watches and shares over the network.
//
// When the power fails the NAS runs on the battery and shuts down cleanly
// before it is empty: once the UPS says the battery is low, or earlier,
// after a chosen time on battery.
package ups

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

const (
	settingsFile = "system/ups.json"
	// upsName is what the UPS of this NAS is called towards NUT.
	upsName = "ups"
	// remoteUser is the account other machines watch this UPS with.
	remoteUser = "monitor"
)

var (
	// ErrDisabled means titand runs in development mode.
	ErrDisabled = errors.New("ups: not available in development mode")
	// ErrNotLoaded means /config could not be read.
	ErrNotLoaded = errors.New("ups: settings not loaded from /config")
	// ErrInvalid means a setting is not acceptable; the message says which.
	ErrInvalid = errors.New("ups: invalid setting")
)

// Drivers are the NUT drivers that can be chosen for a UPS on a cable.
// dummy-ups plays a UPS from a file; it is there for tests.
var Drivers = []string{"usbhid-ups", "nutdrv_qx", "blazer_usb", "blazer_ser", "apcsmart", "dummy-ups"}

// Settings is /config/system/ups.json.
type Settings struct {
	// Mode is off, local (UPS on a cable at this NAS) or network.
	Mode string `json:"mode"`
	// local: the driver and where the UPS is (auto for USB).
	Driver string `json:"driver,omitempty"`
	Port   string `json:"port,omitempty"`
	// Serve lets other machines in the network watch this UPS, with the
	// account "monitor" and RemotePassword.
	Serve          bool   `json:"serve"`
	RemotePassword string `json:"remotePassword,omitempty"`
	// network: the NUT server that watches the UPS, and how to log in.
	Host     string `json:"host,omitempty"`
	Name     string `json:"name,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	// ShutdownAfter is how many minutes on battery the NAS waits before
	// it shuts down; 0 waits until the UPS says the battery is low.
	ShutdownAfter int `json:"shutdownAfter"`
}

// Status is what the UPS says.
type Status struct {
	Mode string `json:"mode"`
	// Connected says whether NUT answers for the UPS.
	Connected  bool   `json:"connected"`
	OnBattery  bool   `json:"onBattery"`
	LowBattery bool   `json:"lowBattery"`
	Raw        string `json:"raw"`
	// Charge in percent, Runtime in seconds, Load in percent; -1 if the
	// UPS does not tell.
	Charge  int    `json:"charge"`
	Runtime int    `json:"runtime"`
	Load    int    `json:"load"`
	Model   string `json:"model"`
	Error   string `json:"error"`
	// Since is when it went on battery.
	Since *time.Time `json:"since"`
	// ShuttingDown is set once TitanOS has decided to shut down.
	ShuttingDown bool `json:"shuttingDown"`
}

// Found is a UPS that nut-scanner sees on a cable.
type Found struct {
	Driver  string `json:"driver"`
	Port    string `json:"port"`
	Product string `json:"product"`
}

// Manager writes the configuration of NUT, starts and stops it and keeps
// an eye on the UPS.
type Manager struct {
	store  *config.Store
	runner run.Runner
	// etcDir is /etc/nut.
	etcDir string
	now    func() time.Time

	// Disabled leaves the system alone (development mode).
	Disabled bool
	// OnEvent is told what users should hear of: ups_on_battery,
	// ups_online, ups_low_battery, ups_shutdown, ups_lost.
	OnEvent func(kind, subject string)
	// Poweroff shuts the NAS down when NUT on this NAS is not the one to
	// do it.
	Poweroff func() error

	kick chan struct{}

	mu       sync.Mutex
	loaded   bool
	settings Settings
	// localPassword is the password upsmon on this NAS logs in with; it
	// is made anew at every start and never leaves the NAS.
	localPassword string
	status        Status
}

// NewManager returns a manager working on the real system.
func NewManager(store *config.Store) *Manager {
	return &Manager{store: store, runner: run.Exec{}, etcDir: "/etc/nut", now: time.Now, kick: make(chan struct{}, 1),
		settings: Settings{Mode: "off"}, status: Status{Mode: "off", Charge: -1, Runtime: -1, Load: -1}}
}

// Load reads the settings from /config.
func (m *Manager) Load() error {
	settings := Settings{Mode: "off"}
	err := m.store.ReadJSON(settingsFile, &settings)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings, m.loaded = settings, true
	return nil
}

var (
	ttyForm   = regexp.MustCompile(`^/dev/tty[A-Za-z0-9]{1,16}$`)
	dummyForm = regexp.MustCompile(`^[a-z0-9_-]{1,40}\.dev$`)
	hostForm  = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	nameForm  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
	plainForm = regexp.MustCompile(`^[\x21-\x7e]{1,64}$`)
)

// quote writes a value for NUT's configuration files, where # starts a
// comment and quotes and backslashes have a meaning of their own. Plain
// values stay as they are, so that files written before do not change.
func quote(value string) string {
	if !strings.ContainsAny(value, "#\"\\'=") {
		return value
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}

func invalid(what string) error { return fmt.Errorf("%w: %s", ErrInvalid, what) }

func randomPassword() string {
	secret := make([]byte, 18)
	rand.Read(secret)
	return base64.RawURLEncoding.EncodeToString(secret)
}

// Settings returns the settings without the password for a network UPS;
// the second value says whether it is set. The password for other
// machines is shown: it is meant to be typed in there.
func (m *Manager) Settings() (Settings, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	settings := m.settings
	set := settings.Password != ""
	settings.Password = ""
	return settings, set
}

// SetSettings stores and applies new settings. An empty password keeps
// the stored one.
func (m *Manager) SetSettings(settings Settings) error {
	if m.Disabled {
		return ErrDisabled
	}
	m.mu.Lock()
	if !m.loaded {
		m.mu.Unlock()
		return ErrNotLoaded
	}
	old := m.settings
	m.mu.Unlock()
	if settings.Password == "" {
		settings.Password = old.Password
	}
	settings.RemotePassword = old.RemotePassword
	if settings.ShutdownAfter < 0 || settings.ShutdownAfter > 240 {
		return invalid("time on battery")
	}
	switch settings.Mode {
	case "off":
	case "local":
		if settings.Driver == "" {
			settings.Driver = "usbhid-ups"
		}
		if settings.Port == "" {
			settings.Port = "auto"
		}
		if !slices.Contains(Drivers, settings.Driver) {
			return invalid("driver")
		}
		switch {
		case settings.Driver == "dummy-ups":
			if !dummyForm.MatchString(settings.Port) {
				return invalid("port")
			}
		case settings.Driver == "usbhid-ups" || settings.Driver == "blazer_usb":
			if settings.Port != "auto" {
				return invalid("port")
			}
		default:
			if settings.Port != "auto" && !ttyForm.MatchString(settings.Port) {
				return invalid("port")
			}
		}
		if settings.Serve && settings.RemotePassword == "" {
			settings.RemotePassword = randomPassword()
		}
		settings.Host, settings.Name, settings.User, settings.Password = "", "", "", ""
	case "network":
		if !hostForm.MatchString(settings.Host) || !nameForm.MatchString(settings.Name) || !nameForm.MatchString(settings.User) || !plainForm.MatchString(settings.Password) {
			return invalid("server, name, user or password")
		}
		settings.Driver, settings.Port, settings.Serve = "", "", false
	default:
		return invalid("mode")
	}
	if err := m.store.WriteJSON(settingsFile, settings); err != nil {
		return err
	}
	m.mu.Lock()
	m.settings = settings
	m.mu.Unlock()
	m.Kick()
	return nil
}

// Kick has the settings applied soon.
func (m *Manager) Kick() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// Status returns what the UPS said at the last look.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// Scan looks for UPS on USB.
func (m *Manager) Scan(ctx context.Context) ([]Found, error) {
	if m.Disabled {
		return nil, ErrDisabled
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	output, err := m.runner.Run(ctx, "", "nut-scanner", "--usb_scan", "--quiet")
	found := []Found{}
	var current *Found
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "["):
			found = append(found, Found{})
			current = &found[len(found)-1]
		case current != nil && strings.Contains(line, "="):
			key, value, _ := strings.Cut(line, "=")
			value = strings.Trim(strings.TrimSpace(value), `"`)
			switch strings.TrimSpace(key) {
			case "driver":
				current.Driver = value
			case "port":
				current.Port = value
			case "product":
				current.Product = value
			}
		}
	}
	if len(found) == 0 && err != nil {
		// nut-scanner ends with an error when it finds nothing.
		return found, nil
	}
	return found, nil
}

// files are the configuration of NUT for the settings.
func (m *Manager) files(settings Settings, localPassword string) map[string]string {
	files := map[string]string{}
	header := "# Written by TitanOS from /config/system/ups.json; changes here are lost.\n"
	switch settings.Mode {
	case "local":
		mode := "standalone"
		if settings.Serve {
			mode = "netserver"
		}
		files["nut.conf"] = header + "MODE=" + mode + "\n"
		files["ups.conf"] = header + fmt.Sprintf("maxretry = 3\n\n[%s]\n\tdriver = %s\n\tport = %s\n\tdesc = \"TitanOS\"\n", upsName, settings.Driver, settings.Port)
		listen := "LISTEN 127.0.0.1 3493\nLISTEN ::1 3493\n"
		users := fmt.Sprintf("[local]\n\tpassword = %s\n\tupsmon primary\n", quote(localPassword))
		if settings.Serve {
			listen = "LISTEN 0.0.0.0 3493\nLISTEN :: 3493\n"
			users += fmt.Sprintf("\n[%s]\n\tpassword = %s\n\tupsmon secondary\n", remoteUser, quote(settings.RemotePassword))
		}
		files["upsd.conf"] = header + listen
		files["upsd.users"] = header + users
		files["upsmon.conf"] = header + m.upsmon(fmt.Sprintf("%s@localhost 1 local %s primary", upsName, quote(localPassword)))
	case "network":
		files["nut.conf"] = header + "MODE=netclient\n"
		files["upsmon.conf"] = header + m.upsmon(fmt.Sprintf("%s@%s 1 %s %s secondary", settings.Name, settings.Host, settings.User, quote(settings.Password)))
	default:
		files["nut.conf"] = header + "MODE=none\n"
	}
	return files
}

func (m *Manager) upsmon(monitor string) string {
	// upsmon shuts the NAS down when the battery is low: systemd ends
	// the VMs, containers and shares and unmounts the volumes first.
	return "MONITOR " + monitor + "\nMINSUPPLIES 1\nSHUTDOWNCMD \"/usr/bin/systemctl poweroff\"\nPOWERDOWNFLAG /etc/killpower\nFINALDELAY 5\nDEADTIME 25\n"
}

// target is the UPS to ask about, as NUT names it.
func target(settings Settings) string {
	if settings.Mode == "network" {
		return settings.Name + "@" + settings.Host
	}
	return upsName + "@localhost"
}

// Apply writes the configuration of NUT and starts or stops it.
func (m *Manager) Apply(ctx context.Context) error {
	if m.Disabled {
		return nil
	}
	m.mu.Lock()
	settings := m.settings
	if m.localPassword == "" {
		// titand was restarted (an app update), the NAS was not: NUT
		// still runs with the password of before.
		m.localPassword = currentPassword(m.etcDir)
	}
	if m.localPassword == "" {
		m.localPassword = randomPassword()
	}
	localPassword := m.localPassword
	m.mu.Unlock()

	// Nothing changed and NUT runs: the watch over the UPS goes on without a gap.
	if m.unchanged(m.files(settings, localPassword)) && m.running(ctx, settings.Mode) {
		return nil
	}

	// Stopped first, so that nothing runs with half the new configuration.
	_, _ = m.runner.Run(ctx, "", "systemctl", "stop", "nut-monitor.service", "nut-server.service", "nut-driver.target", "nut-driver-enumerator.path") // cleanup, best effort: nothing more can be done if it fails
	if err := os.MkdirAll(m.etcDir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"ups.conf", "upsd.conf", "upsd.users", "upsmon.conf"} {
		os.Remove(filepath.Join(m.etcDir, name))
	}
	for name, content := range m.files(settings, localPassword) {
		// Passwords stand in them: readable for root and the group nut.
		if err := os.WriteFile(filepath.Join(m.etcDir, name), []byte(content), 0o640); err != nil {
			return err
		}
		_, _ = m.runner.Run(ctx, "", "chgrp", "nut", filepath.Join(m.etcDir, name)) // without the group only root reads the file; NUT then reports it
	}
	switch settings.Mode {
	case "local":
		// Makes the units for the drivers in ups.conf. It ends with 42
		// when it changed something, which is what it is asked to do.
		if _, err := m.runner.Run(ctx, "", "upsdrvsvcctl", "resync"); err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 42 {
				return err
			}
		}
		// A USB UPS can show up after titand: the driver unit then fails
		// but restarts by itself. Server and monitor start anyway and
		// wait for it, so the NAS still shuts down on a low battery.
		_, driverErr := m.runner.Run(ctx, "", "systemctl", "start", "nut-driver@"+upsName+".service")
		if _, err := m.runner.Run(ctx, "", "systemctl", "start", "nut-server.service", "nut-monitor.service"); err != nil {
			return err
		}
		if driverErr != nil {
			return fmt.Errorf("the driver does not reach the UPS: %w", driverErr)
		}
	case "network":
		if _, err := m.runner.Run(ctx, "", "systemctl", "start", "nut-monitor.service"); err != nil {
			return err
		}
	}
	slog.Info("UPS settings put in place", "mode", settings.Mode)
	return nil
}

// currentPassword returns the password upsmon on this NAS logs in with now.
func currentPassword(etcDir string) string {
	data, err := os.ReadFile(filepath.Join(etcDir, "upsd.users"))
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "password = "); ok && value != "" {
			return value
		}
	}
	return ""
}

// unchanged reports whether /etc/nut holds exactly these files already.
func (m *Manager) unchanged(files map[string]string) bool {
	for _, name := range []string{"ups.conf", "upsd.conf", "upsd.users", "upsmon.conf"} {
		current, err := os.ReadFile(filepath.Join(m.etcDir, name))
		want, wanted := files[name]
		if wanted != (err == nil) || (wanted && string(current) != want) {
			return false
		}
	}
	return true
}

// running reports whether the services of a mode are up.
func (m *Manager) running(ctx context.Context, mode string) bool {
	units := map[string][]string{"local": {"nut-server.service", "nut-monitor.service"}, "network": {"nut-monitor.service"}}[mode]
	if len(units) == 0 {
		return false
	}
	_, err := m.runner.Run(ctx, "", "systemctl", append([]string{"is-active", "--quiet"}, units...)...)
	return err == nil
}

// parse reads what upsc prints: one "name: value" per line.
func parse(output string) map[string]string {
	values := map[string]string{}
	for line := range strings.SplitSeq(output, "\n") {
		if key, value, ok := strings.Cut(line, ": "); ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values
}

func number(values map[string]string, key string) int {
	if value, err := strconv.ParseFloat(values[key], 64); err == nil {
		return int(value)
	}
	return -1
}

// look asks NUT about the UPS and acts on what changed.
func (m *Manager) look(ctx context.Context) {
	m.mu.Lock()
	settings, before := m.settings, m.status
	m.mu.Unlock()
	status := Status{Mode: settings.Mode, Charge: -1, Runtime: -1, Load: -1, ShuttingDown: before.ShuttingDown}
	if settings.Mode == "off" {
		m.mu.Lock()
		m.status = status
		m.mu.Unlock()
		return
	}
	lookCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	output, err := m.runner.Run(lookCtx, "", "upsc", target(settings))
	cancel()
	values := parse(output)
	if err != nil || values["ups.status"] == "" {
		status.Error = strings.TrimSpace(output)
		// upsc explains a failure on stderr, which is not in output.
		var command *run.CommandError
		if status.Error == "" && errors.As(err, &command) && command.Output != "" {
			status.Error = command.Output
		}
		if status.Error == "" && err != nil {
			status.Error = err.Error()
		}
	} else {
		status.Connected = true
		status.Raw = values["ups.status"]
		flags := strings.Fields(status.Raw)
		status.OnBattery = slices.Contains(flags, "OB")
		status.LowBattery = slices.Contains(flags, "LB")
		status.Charge, status.Runtime, status.Load = number(values, "battery.charge"), number(values, "battery.runtime"), number(values, "ups.load")
		status.Model = strings.TrimSpace(firstOf(values, "device.mfr", "ups.mfr") + " " + firstOf(values, "device.model", "ups.model"))
	}
	now := m.now()
	switch {
	case status.OnBattery && before.Since != nil:
		status.Since = before.Since
	case status.OnBattery:
		status.Since = &now
	}

	event := func(kind, subject string) {
		if m.OnEvent != nil {
			m.OnEvent(kind, subject)
		}
	}
	switch {
	case status.Connected && status.OnBattery && !before.OnBattery:
		event("ups_on_battery", status.Model)
	case status.Connected && !status.OnBattery && before.OnBattery:
		event("ups_online", status.Model)
	case !status.Connected && before.Connected:
		event("ups_lost", "")
	}
	if status.LowBattery && !before.LowBattery {
		// upsmon shuts down now; this only tells.
		event("ups_low_battery", status.Model)
	}
	shutdown := status.OnBattery && settings.ShutdownAfter > 0 && status.Since != nil &&
		now.Sub(*status.Since) >= time.Duration(settings.ShutdownAfter)*time.Minute && !before.ShuttingDown
	m.mu.Lock()
	m.status = status
	if shutdown {
		m.status.ShuttingDown = true
	}
	m.mu.Unlock()
	if shutdown {
		slog.Warn("on battery for too long, shutting down", "minutes", settings.ShutdownAfter)
		event("ups_shutdown", strconv.Itoa(settings.ShutdownAfter))
		var err error
		if settings.Mode == "local" {
			// As the primary, upsmon also tells the machines that watch
			// this UPS to shut down, and then the NAS.
			_, err = m.runner.Run(ctx, "", "upsmon", "-c", "fsd")
		} else if m.Poweroff != nil {
			err = m.Poweroff()
		}
		if err != nil {
			slog.Error("shutdown on battery failed", "err", err)
		}
	}
}

func firstOf(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if values[key] != "" {
			return values[key]
		}
	}
	return ""
}

// Run applies the settings at the start and when they change, and looks at
// the UPS every ten seconds, until ctx is done.
func (m *Manager) Run(ctx context.Context) {
	if m.Disabled {
		return
	}
	apply := func() bool {
		err := m.Apply(ctx)
		if err != nil {
			slog.Warn("UPS not set up", "err", err)
			m.mu.Lock()
			m.status.Error = err.Error()
			m.mu.Unlock()
		}
		return err == nil
	}
	// At boot the UPS may answer late (USB): try again every minute for
	// the first ten minutes instead of staying unprotected until the
	// settings are saved again.
	retryUntil, nextTry := time.Time{}, time.Time{}
	if !apply() {
		retryUntil, nextTry = time.Now().Add(10*time.Minute), time.Now().Add(time.Minute)
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.kick:
			retryUntil = time.Time{}
			apply()
		case <-ticker.C:
			if now := time.Now(); now.Before(retryUntil) && !now.Before(nextTry) {
				if apply() {
					retryUntil = time.Time{}
				}
				nextTry = now.Add(time.Minute)
			}
		}
		m.look(ctx)
	}
}
