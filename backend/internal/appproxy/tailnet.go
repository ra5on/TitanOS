package appproxy

// Names in the tailnet: a route with "via Tailscale" becomes a device of
// its own in the tailnet, e.g. https://immich.<tailnet>.ts.net, with a
// certificate Tailscale issues and renews. titand runs one tailscaled per
// such route from the Tailscale plugin, in userspace mode (no interface,
// no firewall rules), as a transient systemd unit so that it outlives a
// restart of titand. Its state (the node key) lives on the plugin's data
// volume. Tailscale Serve terminates HTTPS and passes every request on to
// titand's own proxy on 127.0.0.1, which keeps its rules: no TitanOS
// cookies for apps, checked targets, the app's certificate if asked.
//
// Tailscale Serve keeps the Host header (ipn/ipnlocal/serve.go:
// r.Out.Host = r.In.Host), so the proxy finds the route by the ts.net name
// the node got. Requests for such a name are only taken from loopback:
// anyone else asking for it bypasses the tailnet and its access rules.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/naming"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

const (
	tailnetKeyFile = "docker/proxy-tailscale.key"
	tailnetUnit    = "titanos-ts-"
	tailnetState   = ".titanos/tailscale-names"
)

// ErrTailnetKey means the auth key is not acceptable.
var ErrTailnetKey = errors.New("appproxy: not a Tailscale auth key")

// Tailnet runs the tailscaled instances of routes with "via Tailscale".
type Tailnet struct {
	// Disabled starts nothing (development mode).
	Disabled bool
	// Volume returns the data volume of the Tailscale plugin and whether
	// the plugin is active; nil means never.
	Volume func() (string, bool)

	runner    run.Runner
	mountRoot string
	runDir    string
	binary    string

	// probe asks a device's address for its ts.net name over HTTPS
	// (oneclick.go); replaced in tests.
	probe func(ctx context.Context, ip, name string) (int, error)

	mu     sync.Mutex
	nodes  map[string]*TailnetNode
	served map[string]string // id → serve target that is set
}

// TailnetNode is the state of one route's device as the UI shows it.
type TailnetNode struct {
	// State: "starting", "login" (no auth key), "running", "https-off"
	// (HTTPS certificates are off in the tailnet), "stopped", "error".
	State string `json:"state"`
	URL   string `json:"url,omitempty"`
	Error string `json:"error,omitempty"`
	dns   string
	ips   []string // the device's addresses in the tailnet
}

// TailnetStatus is the global part the settings page shows.
type TailnetStatus struct {
	Available bool `json:"available"`
	KeySet    bool `json:"keySet"`
}

func newTailnet() *Tailnet {
	return &Tailnet{runner: run.Exec{}, mountRoot: "/mnt", runDir: "/run/titanos-ts-names", binary: "/usr/sbin/tailscaled", probe: probeTailnet, nodes: map[string]*TailnetNode{}, served: map[string]string{}}
}

// Configure points the devices at other commands and folders: runner
// plays systemctl, systemd-run and tailscale, mountRoot holds the data
// volumes, runDir the sockets, binary is tailscaled. For tests of other
// packages; titand keeps the defaults.
func (t *Tailnet) Configure(runner run.Runner, mountRoot, runDir, binary string) {
	t.runner, t.mountRoot, t.runDir, t.binary = runner, mountRoot, runDir, binary
}

// nodeID is the name of a route's unit, socket and state folder:
// "app:immich" becomes "app-immich" (both parts are [a-z0-9_-]).
func nodeID(key string) string { return strings.Replace(key, ":", "-", 1) }

// validNodeID accepts only ids nodeID can make, so nothing else under
// the state folder or another unit is ever touched.
func validNodeID(id string) bool {
	for _, prefix := range []string{"app-", "target-"} {
		if rest, ok := strings.CutPrefix(id, prefix); ok && appRE.MatchString(rest) {
			return true
		}
	}
	return false
}

func (t *Tailnet) available() (string, bool) {
	if t.Disabled || t.Volume == nil {
		return "", false
	}
	volume, ok := t.Volume()
	if !ok || !naming.ValidVolume(volume) {
		return "", false
	}
	if _, err := os.Stat(t.binary); err != nil {
		return "", false
	}
	return volume, true
}

// SetTailnetKey stores the auth key the devices join the tailnet with;
// "" removes it. Devices that joined keep running without it.
func (m *Manager) SetTailnetKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		if err := m.store.Remove(tailnetKeyFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	} else {
		if !strings.HasPrefix(key, "tskey-") || len(key) > 200 || strings.ContainsAny(key, " \t\r\n") {
			return ErrTailnetKey
		}
		if err := m.store.Write(tailnetKeyFile, []byte(key)); err != nil {
			return err
		}
	}
	m.mu.Lock()
	m.tailnetKey = key
	m.mu.Unlock()
	m.kickTailnet()
	return nil
}

func (m *Manager) loadTailnetKey() {
	data, err := m.store.Read(tailnetKeyFile)
	if err != nil {
		return
	}
	m.mu.Lock()
	m.tailnetKey = strings.TrimSpace(string(data))
	m.mu.Unlock()
}

func (m *Manager) kickTailnet() {
	select {
	case m.tailnetKick <- struct{}{}:
	default:
	}
}

// TailnetInfo is the global state for the settings page.
func (m *Manager) TailnetInfo() TailnetStatus {
	_, ok := m.Tailnet.available()
	m.mu.RLock()
	defer m.mu.RUnlock()
	return TailnetStatus{Available: ok, KeySet: m.tailnetKey != ""}
}

// tailnetNode returns the state of a route's device, nil without one.
func (m *Manager) tailnetNode(key string) *TailnetNode {
	t := m.Tailnet
	t.mu.Lock()
	defer t.mu.Unlock()
	if n := t.nodes[key]; n != nil {
		c := *n
		return &c
	}
	return nil
}

// tailnetMatch finds the route a ts.net name belongs to.
func (m *Manager) tailnetMatch(host string) (string, bool) {
	t := m.Tailnet
	t.mu.Lock()
	defer t.mu.Unlock()
	for key, n := range t.nodes {
		if n.dns != "" && n.dns == host {
			return key, true
		}
	}
	return "", false
}

// RunTailnet keeps the devices in line with the routes until ctx is done:
// at the start, after every change and every minute.
func (m *Manager) RunTailnet(ctx context.Context) {
	if m.Tailnet.Disabled {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		m.reconcileTailnet(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-m.tailnetKick:
		}
	}
}

type tailnetWant struct {
	key, name string
	port      int
	run       bool // enabled and the main switch on; else only kept
}

func (m *Manager) tailnetWanted() []tailnetWant {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []tailnetWant
	add := func(prefix string, routes map[string]Route) {
		for key, route := range routes {
			if route.Tailscale {
				name := route.TailscaleName
				if name == "" {
					name = route.Name
				}
				out = append(out, tailnetWant{key: prefix + key, name: name, run: route.Enabled && !m.settings.Off})
			}
		}
	}
	add("app:", m.settings.Apps)
	add("target:", m.settings.Targets)
	port := m.HTTPSPort
	if port == 0 {
		port = 443
	}
	for i := range out {
		out[i].port = port
	}
	return out
}

func (m *Manager) reconcileTailnet(ctx context.Context) {
	t := m.Tailnet
	wanted := m.tailnetWanted()
	volume, ok := t.available()
	units, err := t.units(ctx)
	if err != nil {
		slog.Warn("tailnet names: units not listed", "err", err)
	}
	keep := map[string]bool{}
	for _, w := range wanted {
		keep[nodeID(w.key)] = true
		switch {
		case !ok:
			t.set(w.key, &TailnetNode{State: "stopped", Error: "plugin"})
			t.stop(ctx, nodeID(w.key))
		case !w.run:
			t.stop(ctx, nodeID(w.key))
			t.set(w.key, &TailnetNode{State: "stopped"})
		default:
			t.set(w.key, t.ensure(ctx, volume, w, m.currentKey()))
		}
	}
	// Devices whose route is gone or no longer wants Tailscale leave the
	// tailnet and forget their key.
	for _, id := range units {
		if !keep[id] {
			t.forget(ctx, volume, id)
		}
	}
	t.mu.Lock()
	for key := range t.nodes {
		if !keep[nodeID(key)] {
			delete(t.nodes, key)
		}
	}
	t.mu.Unlock()
	if ok {
		// State folders without a unit (titand stopped them before) are
		// removed too when their route is gone.
		entries, _ := os.ReadDir(filepath.Join(t.mountRoot, volume, tailnetState))
		for _, e := range entries {
			if !keep[e.Name()] && !slices.Contains(units, e.Name()) {
				t.forget(ctx, volume, e.Name())
			}
		}
	}
}

func (m *Manager) currentKey() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tailnetKey
}

func (t *Tailnet) set(key string, n *TailnetNode) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if old := t.nodes[key]; old != nil && n.dns == "" && n.State == "running" {
		n.dns, n.ips = old.dns, old.ips
	}
	t.nodes[key] = n
}

func (t *Tailnet) cmd(ctx context.Context, id string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	return t.runner.Run(ctx, "", "tailscale", append([]string{"--socket=" + t.socket(id)}, args...)...)
}

func (t *Tailnet) socket(id string) string { return filepath.Join(t.runDir, id+".sock") }

// units lists the ids of running or failed titanos-ts-* units.
func (t *Tailnet) units(ctx context.Context) ([]string, error) {
	out, err := t.runner.Run(ctx, "", "systemctl", "list-units", "--all", "--plain", "--no-legend", "--type=service", tailnetUnit+"*")
	if err != nil {
		return nil, err
	}
	var ids []string
	for line := range strings.Lines(out) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		id, ok := strings.CutPrefix(strings.TrimSuffix(fields[0], ".service"), tailnetUnit)
		if ok && validNodeID(id) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// stateDir makes sure the private state folder of a device exists, owned
// by root and closed to everyone else, like the plugin's own.
func (t *Tailnet) stateDir(volume, id string) (string, error) {
	root, err := os.OpenRoot(filepath.Join(t.mountRoot, volume))
	if err != nil {
		return "", err
	}
	defer root.Close()
	for _, path := range []string{".titanos", tailnetState, filepath.Join(tailnetState, id)} {
		if err := root.Mkdir(path, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		st, err := root.Lstat(path)
		if err != nil {
			return "", err
		}
		owner, ok := st.Sys().(*syscall.Stat_t)
		if !st.IsDir() || st.Mode().Perm()&0o022 != 0 || !ok || owner.Uid != uint32(os.Geteuid()) {
			return "", errors.New("unsafe state folder for a tailnet name")
		}
	}
	return filepath.Join(t.mountRoot, volume, tailnetState, id), nil
}

type tsStatus struct {
	BackendState string
	CertDomains  []string
	Self         struct {
		DNSName      string
		HostName     string
		TailscaleIPs []string
	}
}

// ensure starts a route's device if needed, lets it join the tailnet and
// points Serve at titand.
func (t *Tailnet) ensure(ctx context.Context, volume string, w tailnetWant, key string) *TailnetNode {
	id := nodeID(w.key)
	fail := func(err error) *TailnetNode {
		slog.Warn("tailnet name not ready", "route", w.key, "err", err)
		return &TailnetNode{State: "error", Error: err.Error()}
	}
	if out, _ := t.runner.Run(ctx, "", "systemctl", "is-active", tailnetUnit+id+".service"); strings.TrimSpace(out) != "active" {
		dir, err := t.stateDir(volume, id)
		if err != nil {
			return fail(err)
		}
		if err := os.MkdirAll(t.runDir, 0o700); err != nil {
			return fail(err)
		}
		_, _ = t.runner.Run(ctx, "", "systemctl", "reset-failed", tailnetUnit+id+".service") // cleanup, best effort: nothing more can be done if it fails
		if _, err := t.runner.Run(ctx, "", "systemd-run", "--unit="+tailnetUnit+id, "--collect",
			"--property=Restart=on-failure", "--property=RestartSec=5", "--property=UMask=0077",
			"--setenv=TS_NO_LOGS_NO_SUPPORT=true",
			t.binary, "--tun=userspace-networking", "--statedir="+dir, "--socket="+t.socket(id), "--port=0"); err != nil {
			return fail(err)
		}
		t.mu.Lock()
		delete(t.served, id)
		t.mu.Unlock()
	}
	var st tsStatus
	var raw string
	var err error
	for range 20 {
		if raw, err = t.cmd(ctx, id, "status", "--json"); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return fail(ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
	if err != nil {
		return &TailnetNode{State: "starting"}
	}
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return fail(err)
	}
	if st.BackendState == "NeedsLogin" || st.BackendState == "NoState" {
		if key == "" {
			return &TailnetNode{State: "login"}
		}
		// The key goes to tailscale as a file, never on a command line.
		keyFile := filepath.Join(t.runDir, "authkey")
		if err := os.WriteFile(keyFile, []byte(key), 0o600); err != nil {
			return fail(err)
		}
		_, err := t.cmd(ctx, id, "up", "--auth-key=file:"+keyFile, "--hostname="+w.name,
			"--accept-dns=false", "--accept-routes=false", "--ssh=false", "--webclient=false", "--timeout=30s")
		os.Remove(keyFile)
		if err != nil {
			return fail(err)
		}
		if raw, err = t.cmd(ctx, id, "status", "--json"); err != nil {
			return fail(err)
		}
		st = tsStatus{}
		_ = json.Unmarshal([]byte(raw), &st) // a value that cannot be read leaves the zero value, which the code handles
	}
	if st.BackendState != "Running" {
		return &TailnetNode{State: "starting"}
	}
	if !strings.EqualFold(st.Self.HostName, w.name) {
		// The name was changed in TitanOS: the device follows.
		_, _ = t.cmd(ctx, id, "set", "--hostname="+w.name) // the name is set again on the next check
	}
	dns := strings.TrimSuffix(strings.ToLower(st.Self.DNSName), ".")
	node := &TailnetNode{State: "running", URL: "https://" + dns, dns: dns, ips: st.Self.TailscaleIPs}
	if len(st.CertDomains) == 0 {
		// Without HTTPS certificates in the tailnet, Serve has no certificate.
		node.State = "https-off"
	}
	target := "https+insecure://127.0.0.1:" + strconv.Itoa(w.port)
	t.mu.Lock()
	served := t.served[id] == target
	t.mu.Unlock()
	if !served {
		if _, err := t.cmd(ctx, id, "serve", "--bg", "--yes", "--https=443", target); err != nil {
			node.State, node.Error = "error", fmt.Sprintf("serve: %v", err)
			return node
		}
		t.mu.Lock()
		t.served[id] = target
		t.mu.Unlock()
	}
	return node
}

// stop ends a device for now; it keeps its key and comes back with it.
func (t *Tailnet) stop(ctx context.Context, id string) {
	_, _ = t.runner.Run(ctx, "", "systemctl", "stop", tailnetUnit+id+".service") // cleanup, best effort: nothing more can be done if it fails
	t.mu.Lock()
	delete(t.served, id)
	t.mu.Unlock()
}

// forget lets a device leave the tailnet, ends it and removes its key.
func (t *Tailnet) forget(ctx context.Context, volume, id string) {
	if out, _ := t.runner.Run(ctx, "", "systemctl", "is-active", tailnetUnit+id+".service"); strings.TrimSpace(out) == "active" {
		_, _ = t.cmd(ctx, id, "logout") // cleanup, best effort: nothing more can be done if it fails
	}
	t.stop(ctx, id)
	if volume != "" && validNodeID(id) {
		os.RemoveAll(filepath.Join(t.mountRoot, volume, tailnetState, id))
	}
}
