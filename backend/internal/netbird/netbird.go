// Package netbird configures the optional NetBird extension. Persistent peer
// keys and client state belong on the plugin data volume; /config only holds
// user choices and the setup key used to register the NAS.
package netbird

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/naming"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

const settingsFile = "plugins/netbird/settings.json"

// DefaultManagementURL is NetBird's hosted management service; an empty
// ManagementURL means this one.
const DefaultManagementURL = "https://api.netbird.io:443"

var ErrInvalid = errors.New("invalid NetBird settings")
var ErrUnavailable = errors.New("the NetBird plugin is not active")
var ErrPending = errors.New("confirm or undo the pending NetBird settings first")
var ErrSetupKey = errors.New("a setup key is needed to register this NAS with NetBird")
var errStatus = errors.New("invalid NetBird status")

// Settings are the choices that change routing; they run on trial.
type Settings struct {
	ManagementURL string `json:"managementUrl"`
	AcceptRoutes  bool   `json:"acceptRoutes"`
	ServeRoutes   bool   `json:"serveRoutes"`
	AcceptDNS     bool   `json:"acceptDNS"`
}

// stored is the file on the stick. The setup key never leaves it through
// the API; Connect remembers whether the owner wants the NAS connected.
type stored struct {
	Settings
	SetupKey string `json:"setupKey,omitempty"`
	Connect  bool   `json:"connect"`
}

type Status struct {
	Settings            Settings   `json:"settings"`
	SetupKeySet         bool       `json:"setupKeySet"`
	Connect             bool       `json:"connect"`
	Active              bool       `json:"active"`
	ReadOnly            bool       `json:"readOnly"`
	State               string     `json:"state"`
	Version             string     `json:"version"`
	FQDN                string     `json:"fqdn"`
	IPs                 []string   `json:"ips"`
	Management          string     `json:"management"`
	ManagementConnected bool       `json:"managementConnected"`
	SignalConnected     bool       `json:"signalConnected"`
	PeersConnected      int        `json:"peersConnected"`
	PeersTotal          int        `json:"peersTotal"`
	Health              []string   `json:"health"`
	PendingUntil        *time.Time `json:"pendingUntil,omitempty"`
	Error               string     `json:"error,omitempty"`
}

type Manager struct {
	Disabled       bool
	store, runtime *config.Store
	runner         run.Runner
	mountRoot      string
	// async runs the reconnect after an activation; tests run it inline.
	async    func(func())
	mu       sync.Mutex
	loaded   bool
	settings Settings
	key      string
	connect  bool
	volume   string
	pending  *Settings
	deadline time.Time
	timer    *time.Timer
	problem  string
	peers    peerCache
}

func NewManager(store *config.Store) *Manager {
	return &Manager{store: store, runtime: config.Open("/run/titanos-netbird"), runner: run.Exec{}, mountRoot: "/mnt", async: func(f func()) { go f() }}
}

func normalize(s Settings) (Settings, error) {
	text := strings.TrimSpace(s.ManagementURL)
	if text == "" || text == DefaultManagementURL {
		s.ManagementURL = ""
		return s, nil
	}
	// Only scheme, host and port: the value ends up on a command line and in
	// the daemon's configuration.
	u, err := url.Parse(text)
	if err != nil || len(text) > 256 || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(text, " \t\r\n,") {
		return s, ErrInvalid
	}
	if port := u.Port(); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return s, ErrInvalid
		}
	}
	s.ManagementURL = u.Scheme + "://" + u.Host
	return s, nil
}

// validKey accepts NetBird setup keys (a UUID today) without whitespace or
// control characters; the key is only ever handed over on stdin.
func validKey(key string) bool {
	if len(key) < 8 || len(key) > 128 {
		return false
	}
	for _, r := range key {
		if r <= ' ' || r > '~' {
			return false
		}
	}
	return true
}

func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var s stored
	if err := m.store.ReadJSON(settingsFile, &s); err != nil && !errors.Is(err, fs.ErrNotExist) {
		m.problem = err.Error()
		return err
	}
	settings, err := normalize(s.Settings)
	if err != nil || (s.SetupKey != "" && !validKey(s.SetupKey)) {
		m.problem = ErrInvalid.Error()
		return ErrInvalid
	}
	m.settings, m.key, m.connect = settings, s.SetupKey, s.Connect
	m.loaded = true
	return nil
}

func (m *Manager) save(settings Settings, key string, connect bool) error {
	return m.store.WriteJSON(settingsFile, stored{Settings: settings, SetupKey: key, Connect: connect})
}

func (m *Manager) command(timeout time.Duration, stdin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return m.runner.Run(ctx, stdin, "netbird", args...)
}

// statusJSON is the part of `netbird status --json` TitanOS reads.
type statusJSON struct {
	Peers struct {
		Total     int        `json:"total"`
		Connected int        `json:"connected"`
		Details   []peerJSON `json:"details"`
	} `json:"peers"`
	DaemonVersion string `json:"daemonVersion"`
	DaemonStatus  string `json:"daemonStatus"`
	Management    struct {
		URL       string `json:"url"`
		Connected bool   `json:"connected"`
		Error     string `json:"error"`
	} `json:"management"`
	Signal struct {
		Connected bool   `json:"connected"`
		Error     string `json:"error"`
	} `json:"signal"`
	IP       string   `json:"netbirdIp"`
	IPv6     string   `json:"netbirdIpv6"`
	PubKey   string   `json:"publicKey"`
	FQDN     string   `json:"fqdn"`
	Networks []string `json:"networks"`
}

func (m *Manager) readStatus() (statusJSON, error) {
	var status statusJSON
	raw, err := m.command(10*time.Second, "", "status", "--json")
	if err != nil {
		return status, err
	}
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		return status, errStatus
	}
	return status, nil
}

// address drops the prefix length the daemon reports for its own address.
func address(text string) string {
	if p, err := netip.ParsePrefix(text); err == nil {
		return p.Addr().String()
	}
	if a, err := netip.ParseAddr(text); err == nil {
		return a.String()
	}
	return ""
}

// needsLogin: the daemon has no valid registration and only a setup key helps.
func needsLogin(state string) bool {
	return state == "NeedsLogin" || state == "LoginFailed" || state == "SessionExpired"
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	s := Status{Settings: m.settings, SetupKeySet: m.key != "", Connect: m.connect, ReadOnly: m.Disabled || !m.loaded, IPs: []string{}, Health: []string{}, Error: m.problem, State: "inactive"}
	if m.pending != nil {
		d := m.deadline
		s.PendingUntil = &d
	}
	m.mu.Unlock()
	if m.Disabled {
		return s
	}
	// The command runs outside the lock: a reconnect may hold it a while.
	status, err := m.readStatus()
	if err != nil {
		if errors.Is(err, errStatus) {
			s.Error = err.Error()
		}
		return s
	}
	s.Active = true
	s.State = status.DaemonStatus
	s.Version = status.DaemonVersion
	s.FQDN = strings.TrimSuffix(status.FQDN, ".")
	for _, text := range []string{status.IP, status.IPv6} {
		if a := address(text); a != "" {
			s.IPs = append(s.IPs, a)
		}
	}
	s.Management = status.Management.URL
	s.ManagementConnected = status.Management.Connected
	s.SignalConnected = status.Signal.Connected
	s.PeersConnected, s.PeersTotal = status.Peers.Connected, status.Peers.Total
	for _, text := range []string{status.Management.Error, status.Signal.Error} {
		if text = strings.TrimSpace(text); text != "" {
			s.Health = append(s.Health, text)
		}
	}
	return s
}

// args are the complete client options: every flag is passed so the
// daemon's stored profile always matches the confirmed TitanOS choice.
func args(s Settings) []string {
	management := s.ManagementURL
	if management == "" {
		management = DefaultManagementURL
	}
	return []string{"up", "--no-browser", "--management-url=" + management,
		"--disable-auto-connect=true",
		"--disable-client-routes=" + strconv.FormatBool(!s.AcceptRoutes),
		"--disable-server-routes=" + strconv.FormatBool(!s.ServeRoutes),
		"--disable-dns=" + strconv.FormatBool(!s.AcceptDNS),
		"--block-lan-access=false", "--block-inbound=false", "--disable-firewall=false",
		"--allow-server-ssh=false", "--allow-remote-jobs=false"}
}

// up connects with the given settings. The setup key goes over stdin,
// never on a command line or into a file.
func (m *Manager) up(s Settings, key string) error {
	call := args(s)
	stdin := ""
	if key != "" {
		call = append(call, "--setup-key-file=/dev/stdin")
		stdin = key + "\n"
	}
	if _, err := m.command(45*time.Second, stdin, call...); err != nil {
		return errors.New("the NetBird connection could not be started; check the setup key, the management URL and the service log")
	}
	_ = m.runtime.Write("applied", []byte(appliedKey(s))) // a runtime or state file only; without it the default applies
	return nil
}

func appliedKey(s Settings) string {
	data, _ := json.Marshal(s)
	return string(data)
}

// Prepare is called under the plugin action lock before its service starts.
// The caller has already verified the chosen volume is mounted.
func (m *Manager) Prepare(name, volume string) error {
	if name != "netbird" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Disabled || !m.loaded || !naming.ValidVolume(volume) {
		return ErrUnavailable
	}
	// Reject pre-seeded share directories before touching private peer keys.
	root, err := os.OpenRoot(filepath.Join(m.mountRoot, volume))
	if err != nil {
		return err
	}
	defer root.Close()
	for _, path := range []string{".titanos", ".titanos/netbird"} {
		if err := root.Mkdir(path, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		st, err := root.Lstat(path)
		if err != nil {
			return err
		}
		owner, ok := st.Sys().(*syscall.Stat_t)
		if !st.IsDir() || st.Mode().Perm()&0022 != 0 || !ok || owner.Uid != uint32(os.Geteuid()) {
			return errors.New("unsafe NetBird data directory")
		}
	}
	if err = root.Chmod(".titanos/netbird", 0700); err != nil {
		return err
	}
	m.volume = volume
	return m.writeRuntime(m.settings)
}

func (m *Manager) writeRuntime(s Settings) error {
	if !naming.ValidVolume(m.volume) {
		return ErrUnavailable
	}
	// All values are fixed paths and booleans.
	env := "NB_STATE_DIR=" + filepath.Join(m.mountRoot, m.volume, ".titanos/netbird") + "\nNETBIRD_SERVE_ROUTES=" + strconv.FormatBool(s.ServeRoutes) + "\n"
	return m.runtime.Write("runtime", []byte(env))
}

// prepareHost writes the runtime file and lets the helper set forwarding and
// the firewall hook before the daemon routes anything.
func (m *Manager) prepareHost(s Settings) error {
	if err := m.writeRuntime(s); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := m.runner.Run(ctx, "", "/usr/libexec/titanos-netbird-runtime", "apply"); err != nil {
		return fmt.Errorf("apply NetBird settings: %w", err)
	}
	return nil
}

// Activate is called after the service started. The daemon never connects
// by itself (auto-connect is off), so a wanted connection is made here with
// the saved choice. It runs in the background: an unreachable management
// server must not count as a failed plugin activation.
func (m *Manager) Activate(name string) error {
	if name != "netbird" {
		return nil
	}
	m.mu.Lock()
	if m.Disabled || !m.loaded || m.pending != nil {
		m.mu.Unlock()
		return ErrUnavailable
	}
	err := m.prepareHost(m.settings)
	connect := m.connect
	m.mu.Unlock()
	if err != nil {
		return err
	}
	if connect {
		m.async(m.restore)
	}
	return nil
}

// restore connects with the saved settings unless the running daemon (kept
// over a titand restart) already uses exactly them.
func (m *Manager) restore() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connect || m.pending != nil {
		return
	}
	status, err := m.readStatus()
	if err != nil {
		m.problem = "NetBird status unavailable"
		return
	}
	applied, _ := m.runtime.Read("applied")
	if (status.DaemonStatus == "Connected" || status.DaemonStatus == "Connecting") && string(applied) == appliedKey(m.settings) {
		return
	}
	key := ""
	if needsLogin(status.DaemonStatus) {
		if m.key == "" {
			m.problem = ErrSetupKey.Error()
			return
		}
		key = m.key
	}
	_, _ = m.command(20*time.Second, "", "down") // cleanup, best effort: nothing more can be done if it fails
	if err := m.up(m.settings, key); err != nil {
		slog.Warn("NetBird reconnect failed", "err", err)
		m.problem = err.Error()
		return
	}
	m.problem = ""
}

// apply changes the host and, if the NAS should be connected, reconnects the
// daemon with the new settings (it ignores option changes while connected).
func (m *Manager) apply(s, old Settings) error {
	if err := m.prepareHost(s); err != nil {
		return err
	}
	if !m.connect {
		return nil
	}
	key := ""
	status, err := m.readStatus()
	if (err == nil && needsLogin(status.DaemonStatus)) || s.ManagementURL != old.ManagementURL {
		// Without a key there is no registration to renew: the options
		// apply at the next connect. Set refuses a new server without one.
		if m.key == "" {
			return nil
		}
		key = m.key
	}
	_, _ = m.command(20*time.Second, "", "down") // cleanup, best effort: nothing more can be done if it fails
	return m.up(s, key)
}

func (m *Manager) Set(s Settings) error {
	s, err := normalize(s)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Disabled || !m.loaded || m.volume == "" {
		return ErrUnavailable
	}
	if m.pending != nil {
		return ErrPending
	}
	// A new management server needs a registration there.
	if m.connect && s.ManagementURL != m.settings.ManagementURL && m.key == "" {
		return ErrSetupKey
	}
	if err := m.apply(s, m.settings); err != nil {
		if rollbackErr := m.apply(m.settings, s); rollbackErr != nil {
			previous := m.settings
			m.pending = &previous
			m.deadline = time.Now()
			m.timer = time.AfterFunc(10*time.Second, func() { _ = m.Undo() }) // Undo keeps its error in m.problem and tries again
			err = errors.Join(err, rollbackErr)
		}
		m.problem = err.Error()
		return err
	}
	previous := m.settings
	m.pending = &previous
	m.settings = s
	m.deadline = time.Now().Add(2 * time.Minute)
	m.timer = time.AfterFunc(2*time.Minute, func() { _ = m.Undo() }) // Undo keeps its error in m.problem and tries again
	m.problem = ""
	return nil
}

func (m *Manager) Confirm() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Disabled || m.pending == nil {
		return ErrPending
	}
	if time.Now().After(m.deadline) {
		return ErrPending
	}
	if err := m.save(m.settings, m.key, m.connect); err != nil {
		return err
	}
	m.timer.Stop()
	m.pending = nil
	m.problem = ""
	return nil
}

func (m *Manager) Undo() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil {
		return nil
	}
	if err := m.apply(*m.pending, m.settings); err != nil {
		m.problem = err.Error()
		m.timer = time.AfterFunc(10*time.Second, func() { _ = m.Undo() }) // Undo keeps its error in m.problem and tries again
		return err
	}
	m.settings = *m.pending
	m.pending = nil
	m.problem = ""
	if m.timer != nil {
		m.timer.Stop()
	}
	return nil
}

// SetKey stores or (with "") removes the setup key. It changes no routing and
// is saved at once; it is only used for the next registration.
func (m *Manager) SetKey(key string) error {
	key = strings.TrimSpace(key)
	if key != "" && !validKey(key) {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Disabled || !m.loaded {
		return ErrUnavailable
	}
	if m.pending != nil {
		return ErrPending
	}
	if err := m.save(m.settings, key, m.connect); err != nil {
		return err
	}
	m.key = key
	return nil
}

func (m *Manager) Action(action string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Disabled || !m.loaded || m.volume == "" {
		return ErrUnavailable
	}
	if m.pending != nil {
		return ErrPending
	}
	switch action {
	case "connect":
		status, err := m.readStatus()
		if err != nil {
			return ErrUnavailable
		}
		key := ""
		if needsLogin(status.DaemonStatus) {
			if m.key == "" {
				return ErrSetupKey
			}
			key = m.key
		}
		if err := m.prepareHost(m.settings); err != nil {
			return err
		}
		if err := m.up(m.settings, key); err != nil {
			return err
		}
		m.problem = ""
		return m.wanted(true)
	case "disconnect":
		if _, err := m.command(20*time.Second, "", "down"); err != nil {
			if status, statusErr := m.readStatus(); statusErr != nil || status.DaemonStatus == "Connected" || status.DaemonStatus == "Connecting" {
				return err
			}
		}
		return m.wanted(false)
	case "logout":
		// Removes this peer from the NetBird network; a new registration
		// needs a setup key again.
		if _, err := m.command(20*time.Second, "", "deregister"); err != nil {
			return err
		}
		return m.wanted(false)
	default:
		return ErrInvalid
	}
}

// wanted remembers whether the NAS should be connected after a restart.
func (m *Manager) wanted(connect bool) error {
	if m.connect == connect {
		return nil
	}
	if err := m.save(m.settings, m.key, connect); err != nil {
		return err
	}
	m.connect = connect
	return nil
}

// Pending prevents plugin removal or system updates during the access trial.
func (m *Manager) Pending() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.pending != nil }
