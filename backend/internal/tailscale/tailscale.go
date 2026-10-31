// Package tailscale configures the optional Tailscale extension. Persistent
// node keys belong on the plugin data volume; /config only holds user choices.
package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/naming"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

const settingsFile = "plugins/tailscale/settings.json"

var ErrInvalid = errors.New("invalid Tailscale settings")
var ErrUnavailable = errors.New("the Tailscale plugin is not active")
var ErrPending = errors.New("confirm or undo the pending Tailscale settings first")

type Settings struct {
	Routes        []string `json:"routes"`
	AdvertiseExit bool     `json:"advertiseExit"`
	ExitNode      string   `json:"exitNode"`
	AcceptRoutes  bool     `json:"acceptRoutes"`
	AcceptDNS     bool     `json:"acceptDNS"`
}
type Peer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	IP     string `json:"ip"`
	Online bool   `json:"online"`
}
type Status struct {
	Settings     Settings   `json:"settings"`
	Active       bool       `json:"active"`
	ReadOnly     bool       `json:"readOnly"`
	State        string     `json:"state"`
	Version      string     `json:"version"`
	Name         string     `json:"name"`
	IPs          []string   `json:"ips"`
	AuthURL      string     `json:"authUrl,omitempty"`
	ExitNodes    []Peer     `json:"exitNodes"`
	Health       []string   `json:"health"`
	PendingUntil *time.Time `json:"pendingUntil,omitempty"`
	Error        string     `json:"error,omitempty"`
	// HTTPS is the certificate of the NAS's own name in the tailnet (cert.go).
	HTTPS HTTPSStatus `json:"https"`
}
type Manager struct {
	Disabled       bool
	store, runtime *config.Store
	runner         run.Runner
	mountRoot      string
	mu             sync.Mutex
	loaded         bool
	settings       Settings
	volume         string
	pending        *Settings
	deadline       time.Time
	timer          *time.Timer
	problem        string
	devices        deviceCache
	cert           certState
	// queryFeature reaches the LocalAPI of tailscaled (cert.go).
	queryFeature func(ctx context.Context, feature string) ([]byte, error)
}

func NewManager(store *config.Store) *Manager {
	return &Manager{store: store, runtime: config.Open("/run/titanos-tailscale"), runner: run.Exec{}, mountRoot: "/mnt", settings: Settings{Routes: []string{}}, cert: certState{kick: make(chan struct{}, 1)}, queryFeature: queryFeature}
}
func normalize(s Settings) (Settings, error) {
	if len(s.Routes) > 32 || (s.AdvertiseExit && s.ExitNode != "") {
		return s, ErrInvalid
	}
	routes := []string{}
	for _, text := range s.Routes {
		p, err := netip.ParsePrefix(strings.TrimSpace(text))
		if err != nil || p.Bits() == 0 || p.Addr().Is4In6() || p.Addr().IsLoopback() || p.Addr().IsMulticast() || p.Addr().IsLinkLocalUnicast() || p.Addr().IsUnspecified() {
			return s, ErrInvalid
		}
		routes = append(routes, p.Masked().String())
	}
	sort.Strings(routes)
	s.Routes = slices.Compact(routes)
	if s.ExitNode != "" {
		a, err := netip.ParseAddr(s.ExitNode)
		if err != nil || a.Zone() != "" || (!netip.MustParsePrefix("100.64.0.0/10").Contains(a) && !netip.MustParsePrefix("fd7a:115c:a1e0::/48").Contains(a)) {
			return s, ErrInvalid
		}
		s.ExitNode = a.String()
	}
	return s, nil
}
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Settings{Routes: []string{}}
	if err := m.store.ReadJSON(settingsFile, &s); err != nil && !errors.Is(err, fs.ErrNotExist) {
		m.problem = err.Error()
		return err
	}
	var err error
	s, err = normalize(s)
	if err != nil {
		m.problem = err.Error()
		return err
	}
	m.settings = s
	m.loaded = true
	return nil
}
func (m *Manager) command(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return m.runner.Run(ctx, "", "tailscale", args...)
}

// Only an allowlisted login destination is exposed as a link. Never return
// raw command output containing authentication URLs or private node keys.
func loginURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "login.tailscale.com" || u.User != nil {
		return ""
	}
	return u.String()
}
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Status{Settings: m.settings, ReadOnly: m.Disabled || !m.loaded, IPs: []string{}, ExitNodes: []Peer{}, Health: []string{}, Error: m.problem, State: "inactive", HTTPS: m.cert.status()}
	m.watchCertificate()
	s.Settings.Routes = slices.Clone(s.Settings.Routes)
	if m.pending != nil {
		d := m.deadline
		s.PendingUntil = &d
	}
	if m.Disabled {
		return s
	}
	raw, err := m.command("status", "--json")
	if err != nil {
		return s
	}
	var status struct {
		Version, BackendState, AuthURL string
		TailscaleIPs                   []string
		Health                         []string
		Self                           struct{ DNSName string }
		Peer                           map[string]struct {
			ID, DNSName, HostName  string
			TailscaleIPs           []string
			ExitNodeOption, Online bool
		}
	}
	if json.Unmarshal([]byte(raw), &status) != nil {
		s.Error = "invalid Tailscale status"
		return s
	}
	s.Active = true
	s.State = status.BackendState
	s.Version = status.Version
	s.AuthURL = loginURL(status.AuthURL)
	s.Name = strings.TrimSuffix(status.Self.DNSName, ".")
	if status.TailscaleIPs != nil {
		s.IPs = status.TailscaleIPs
	}
	if status.Health != nil {
		s.Health = status.Health
	}
	for _, peer := range status.Peer {
		if peer.ExitNodeOption && len(peer.TailscaleIPs) > 0 {
			name := strings.TrimSuffix(peer.DNSName, ".")
			if name == "" {
				name = peer.HostName
			}
			s.ExitNodes = append(s.ExitNodes, Peer{peer.ID, name, peer.TailscaleIPs[0], peer.Online})
		}
	}
	slices.SortFunc(s.ExitNodes, func(a, b Peer) int { return strings.Compare(a.Name, b.Name) })
	return s
}
func args(s Settings) []string {
	return []string{"--advertise-routes=" + strings.Join(s.Routes, ","), "--advertise-exit-node=" + strconv.FormatBool(s.AdvertiseExit), "--exit-node=" + s.ExitNode, "--exit-node-allow-lan-access=" + strconv.FormatBool(s.ExitNode != ""), "--accept-routes=" + strconv.FormatBool(s.AcceptRoutes), "--accept-dns=" + strconv.FormatBool(s.AcceptDNS), "--snat-subnet-routes=true", "--netfilter-mode=on", "--ssh=false"}
}

// Prepare is called under the plugin action lock before its service starts.
// The caller has already verified the chosen volume is mounted.
func (m *Manager) Prepare(name, volume string) error {
	if name != "tailscale" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Disabled || !m.loaded || !naming.ValidVolume(volume) {
		return ErrUnavailable
	}
	// Reject pre-seeded share directories before touching private node keys.
	root, err := os.OpenRoot(filepath.Join(m.mountRoot, volume))
	if err != nil {
		return err
	}
	defer root.Close()
	for _, path := range []string{".titanos", ".titanos/tailscale"} {
		if err := root.Mkdir(path, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		st, err := root.Lstat(path)
		if err != nil {
			return err
		}
		owner, ok := st.Sys().(*syscall.Stat_t)
		if !st.IsDir() || st.Mode().Perm()&0022 != 0 || !ok || owner.Uid != uint32(os.Geteuid()) {
			return errors.New("unsafe Tailscale data directory")
		}
	}
	if err = root.Chmod(".titanos/tailscale", 0700); err != nil {
		return err
	}
	m.volume = volume
	return m.writeRuntime(m.settings)
}
func (m *Manager) writeRuntime(s Settings) error {
	if !naming.ValidVolume(m.volume) {
		return ErrUnavailable
	}
	env := "TAILSCALE_STATE=" + filepath.Join(m.mountRoot, m.volume, ".titanos/tailscale/tailscaled.state") + "\n"
	// All values are validated addresses, numbers, booleans or fixed paths.
	env += "TAILSCALE_ROUTES=" + strings.Join(s.Routes, ",") + "\nTAILSCALE_ADVERTISE_EXIT=" + strconv.FormatBool(s.AdvertiseExit) + "\nTAILSCALE_EXIT_NODE=" + s.ExitNode + "\nTAILSCALE_ACCEPT_ROUTES=" + strconv.FormatBool(s.AcceptRoutes) + "\nTAILSCALE_ACCEPT_DNS=" + strconv.FormatBool(s.AcceptDNS) + "\n"
	if err := m.runtime.Write("runtime", []byte(env)); err != nil {
		return err
	}
	return nil
}

// A titand restart must restore saved choices even when tailscaled kept
// running. systemctl start alone does not rerun ExecStartPost in that case.
func (m *Manager) Activate(name string) error {
	if name != "tailscale" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Disabled || !m.loaded || m.pending != nil {
		return ErrUnavailable
	}
	return m.apply(m.settings)
}
func (m *Manager) apply(s Settings) error {
	old := m.settings
	if err := m.writeRuntime(s); err != nil {
		return err
	}
	// The helper also sets forwarding before it asks tailscaled to route.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := m.runner.Run(ctx, "", "/usr/libexec/titanos-tailscale-runtime", "apply")
	if err != nil {
		_ = m.writeRuntime(old) // going back to the settings before; the first error is returned
		return fmt.Errorf("apply Tailscale settings: %w", err)
	}
	return nil
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
	if err := m.apply(s); err != nil {
		if rollbackErr := m.apply(m.settings); rollbackErr != nil {
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
	if err := m.store.WriteJSON(settingsFile, m.settings); err != nil {
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
	if err := m.apply(*m.pending); err != nil {
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
		call := append([]string{"up", "--timeout=3s"}, args(m.settings)...)
		if _, err := m.command(call...); err != nil {
			raw, statusErr := m.command("status", "--json")
			var st struct{ AuthURL, BackendState string }
			if statusErr == nil && json.Unmarshal([]byte(raw), &st) == nil && (loginURL(st.AuthURL) != "" || st.BackendState == "Running" || st.BackendState == "NeedsMachineAuth") {
				return nil
			}
			return errors.New("the Tailscale connection could not be started; check the service log")
		}
		return nil
	case "disconnect":
		_, err := m.command("down")
		return err
	case "logout":
		_, err := m.command("logout")
		return err
	default:
		return ErrInvalid
	}
}

// Pending prevents plugin removal or system updates during the access trial.
// Volume is the data volume the plugin keeps its keys on, once it has
// been prepared there; other devices of this NAS in the tailnet (the
// HTTPS names of apps) keep theirs beside it.
func (m *Manager) Volume() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.volume, m.volume != "" && !m.Disabled
}

func (m *Manager) Pending() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.pending != nil }
