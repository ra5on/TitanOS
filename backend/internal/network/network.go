// Package network configures how the NAS is connected: which ports it
// uses, with which address, and whether there is a bridge that VMs can
// join with an address of their own.
//
// The configuration lives in /config/network/network.json. From it the
// files for systemd-networkd are written on every start and on every
// change. Without a configuration every Ethernet port asks for an address
// by DHCP, which is what the image does on its own.
//
// A wrong network setting locks the administrator out. Every change is
// therefore on trial: unless it is confirmed within two minutes, through
// the web UI and so over the new connection, the previous state comes back.
package network

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

const (
	configFile = "network/network.json"
	// Bridge is the bridge VMs and containers can join.
	Bridge = "br0"
	bond   = "bond0"
	// trial is how long a change waits for its confirmation.
	trial        = 2 * time.Minute
	applyTimeout = 30 * time.Second
	// prefix marks the files this package writes for systemd-networkd.
	prefix = "10-titanos-"
)

var (
	ErrDisabled  = errors.New("network: not available in development mode")
	ErrNotLoaded = errors.New("network: configuration could not be read from /config")
	// ErrInvalid means a setting is not acceptable; the message says which.
	ErrInvalid    = errors.New("network: invalid setting")
	ErrNoPending  = errors.New("network: no change is waiting for confirmation")
	ErrPending    = errors.New("network: a change is waiting for confirmation")
	ErrReverting  = errors.New("network: restoring the previous configuration")
	ErrBridgePort = errors.New("network: select the Ethernet port for the bridge")
)

var hostnameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// BondModes are the ways several ports can work as one.
var BondModes = []string{"active-backup", "802.3ad", "balance-alb"}

// Config is /config/network/network.json.
type Config struct {
	// Networks/Bonds are the opt-in topology. The legacy file is preserved
	// separately for older boot slots until a topology is explicitly confirmed.
	Networks []LogicalNetwork `json:"networks,omitempty"`
	Bonds    []Bond           `json:"bonds,omitempty"`
	Hostname string           `json:"hostname"`
	// Ports are the Ethernet ports the NAS is connected through. None
	// means: every port, each with DHCP. Several form a bond.
	Ports []string `json:"ports"`
	// BondMode says how several ports work together.
	BondMode string `json:"bondMode"`
	// VLAN is the tagged network the NAS lives in, 0 for none.
	VLAN int `json:"vlan"`
	// Bridge puts a bridge on top, which VMs can join.
	Bridge bool `json:"bridge"`
	// DHCP asks the network for an address; otherwise Address (with
	// prefix length, 192.168.1.20/24) and Gateway are used.
	DHCP    bool     `json:"dhcp"`
	Address string   `json:"address"`
	Gateway string   `json:"gateway"`
	DNS     []string `json:"dns"`
}

// Interface is a network port of the NAS.
type Interface struct {
	Name string `json:"name"`
	MAC  string `json:"mac"`
	// Carrier says whether a cable with a link is plugged in.
	Carrier   bool `json:"carrier"`
	SpeedMbps int  `json:"speedMbps"`
}

// Status is everything the network page shows.
type Status struct {
	Config      Config `json:"config"`
	Configured  bool   `json:"configured"`
	BridgeReady bool   `json:"bridgeReady"`
	// Pending is a change on trial; PendingSeconds is how long it has left.
	Pending        *Config     `json:"pending"`
	PendingSeconds int         `json:"pendingSeconds"`
	Interfaces     []Interface `json:"interfaces"`
	// Device is what carries the address of the NAS right now, with its
	// addresses, the gateway and the name servers in use.
	Device    string   `json:"device"`
	Addresses []string `json:"addresses"`
	Gateway   string   `json:"gateway"`
	DNS       []string `json:"dns"`
	// Migrated says that Config (and Pending) only have the flat fields on
	// disk. Config and Pending stay exactly as stored, so older clients and
	// scripts keep the flat form; Topology and PendingTopology are the
	// multi-network form the page shows and edits. It takes effect only
	// through the next change on trial.
	Migrated        bool    `json:"migrated"`
	Topology        Config  `json:"topology"`
	PendingTopology *Config `json:"pendingTopology"`
	// Live is, per network ID, the interface carrying it with its link state
	// and global addresses right now.
	Live map[string]Live `json:"live"`
	// VLANs says whether the page offers VLANs (switched on, or in use);
	// VLANsInUse that a network uses one, so they cannot be switched off.
	VLANs      bool `json:"vlans"`
	VLANsInUse bool `json:"vlansInUse"`
}

// Manager applies the configuration and keeps changes on trial.
type Manager struct {
	attachments sync.RWMutex
	// CheckChange protects configured workloads, including stopped ones.
	CheckChange func(Config, Config) error
	store       *config.Store
	runner      run.Runner
	// Disabled leaves the system alone. Set in development mode.
	Disabled bool
	// dir is where systemd-networkd reads its files from; sys is /sys.
	dir, sys string
	// trial is how long a change waits for confirmation.
	trial time.Duration
	// portWait is how long Load waits for the configured ports to appear
	// (slow drivers, firmware loading, USB adapters, late renames).
	portWait time.Duration

	mu         sync.Mutex
	loaded     bool
	configured bool
	active     Config
	pending    *Config
	// before is what comes back if the pending change is not confirmed.
	before    Config
	deadline  time.Time
	timer     *time.Timer
	reverting bool
	// Remember retired netdevs until deletion succeeds, including rollback retries.
	retired map[string]bool
	options Options
}

// NewManager returns a manager working on the real system.
func NewManager(store *config.Store) *Manager {
	return &Manager{store: store, runner: run.Exec{}, dir: "/etc/systemd/network", sys: "/sys", trial: trial, portWait: 90 * time.Second}
}

func defaults() Config {
	hostname, _ := os.Hostname()
	return Config{Hostname: hostname, DHCP: true, Ports: []string{}, DNS: []string{}}
}

// Load reads the configuration and puts it in place. It is called once
// at the start, before anyone could have changed anything.
func (m *Manager) Load(ctx context.Context) error {
	m.loadOptions()
	cfg := defaults()
	err := m.store.ReadJSON(configFile, &cfg)
	missing := errors.Is(err, fs.ErrNotExist)
	if err != nil && !missing {
		return err
	}
	var topology Config
	if err := m.store.ReadJSON(topologyFile, &topology); err == nil {
		cfg, missing = topology, false
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if !missing && !m.Disabled {
		m.waitPorts(ctx, normal(cfg))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active, m.loaded, m.configured = normal(cfg), true, !missing
	if missing || m.Disabled {
		return nil
	}
	if err := m.validate(m.active); err != nil {
		m.loaded = false
		return err
	}
	if err := m.apply(ctx, m.active); err != nil {
		m.loaded = false
		return err
	}
	return nil
}

func normal(cfg Config) Config {
	if len(cfg.Networks) > 0 {
		return stripLegacy(cloneTopology(cfg))
	}
	if cfg.Ports == nil {
		cfg.Ports = []string{}
	}
	if cfg.DNS == nil {
		cfg.DNS = []string{}
	}
	if len(cfg.Ports) < 2 {
		cfg.BondMode = ""
	}
	if cfg.DHCP {
		cfg.Address, cfg.Gateway = "", ""
	}
	return cfg
}

// Interfaces lists the Ethernet ports of the machine.
func (m *Manager) Interfaces() []Interface {
	entries, _ := os.ReadDir(filepath.Join(m.sys, "class/net"))
	ports := []Interface{}
	for _, entry := range entries {
		name := entry.Name()
		read := func(file string) string {
			data, _ := os.ReadFile(filepath.Join(m.sys, "class/net", name, file))
			return strings.TrimSpace(string(data))
		}
		// Real hardware has a device behind it; bridges, bonds and the
		// ends of containers and VMs do not. Type 1 is Ethernet.
		if _, err := os.Stat(filepath.Join(m.sys, "class/net", name, "device")); err != nil || read("type") != "1" {
			continue
		}
		port := Interface{Name: name, MAC: read("address"), Carrier: read("carrier") == "1"}
		if speed, err := strconv.Atoi(read("speed")); err == nil && speed > 0 {
			port.SpeedMbps = speed
		}
		ports = append(ports, port)
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i].Name < ports[j].Name })
	return ports
}

// device returns the interface that carries the address with a configuration.
func device(cfg Config) string {
	if len(cfg.Networks) > 0 {
		for _, n := range cfg.Networks {
			if n.Management {
				return networkDevice(n)
			}
		}
		return ""
	}
	switch {
	case cfg.Bridge:
		return Bridge
	case cfg.VLAN > 0:
		return "vlan" + strconv.Itoa(cfg.VLAN)
	case len(cfg.Ports) > 1:
		return bond
	case len(cfg.Ports) == 1:
		return cfg.Ports[0]
	}
	return ""
}

// validate checks a configuration against the machine.
func (m *Manager) validate(cfg Config) error {
	if len(cfg.Networks) > 0 {
		return m.validateTopology(cfg)
	}
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
	}
	if !hostnameRE.MatchString(cfg.Hostname) {
		return invalid("hostname")
	}
	known := m.Interfaces()
	seen := map[string]bool{}
	for _, port := range cfg.Ports {
		if seen[port] || !slices.ContainsFunc(known, func(i Interface) bool { return i.Name == port }) {
			return invalid("port %q", port)
		}
		seen[port] = true
	}
	if len(cfg.Ports) > 1 && !slices.Contains(BondModes, cfg.BondMode) {
		return invalid("bond mode")
	}
	if cfg.VLAN < 0 || cfg.VLAN > 4094 {
		return invalid("VLAN")
	}
	if len(cfg.Ports) == 0 && (cfg.Bridge || cfg.VLAN > 0 || !cfg.DHCP) {
		// A bridge, a VLAN or a fixed address need to know which port they are on.
		return invalid("no port chosen")
	}
	if len(cfg.DNS) > 3 {
		return invalid("too many name servers")
	}
	for _, server := range cfg.DNS {
		if _, err := netip.ParseAddr(server); err != nil {
			return invalid("name server %q", server)
		}
	}
	if cfg.DHCP {
		return nil
	}
	address, err := netip.ParsePrefix(cfg.Address)
	if err != nil || !address.Addr().Is4() || address.Bits() < 8 || address.Bits() > 30 {
		return invalid("address")
	}
	// The first and the last address of a network are not for machines.
	network := address.Masked()
	if address.Addr() == network.Addr() || !network.Contains(address.Addr().Next()) {
		return invalid("address")
	}
	if cfg.Gateway != "" {
		gateway, err := netip.ParseAddr(cfg.Gateway)
		if err != nil || !network.Contains(gateway) || gateway == address.Addr() {
			return invalid("gateway")
		}
	}
	return nil
}

// render returns the files for systemd-networkd, by name.
func (m *Manager) render(cfg Config) map[string]string {
	if len(cfg.Networks) > 0 {
		return m.renderTopology(cfg)
	}
	files := map[string]string{}
	if len(cfg.Ports) == 0 {
		return files // the image's own file applies: DHCP on every port
	}
	mac := func(port string) string {
		data, _ := os.ReadFile(filepath.Join(m.sys, "class/net", port, "address"))
		return strings.TrimSpace(string(data))
	}
	// member writes the file of an interface that only carries another one.
	member := func(name, line string) {
		files[prefix+name+".network"] = "[Match]\nName=" + name + "\n\n[Network]\n" + line + "\nLinkLocalAddressing=no\nIPv6AcceptRA=no\nDHCP=no\nKeepConfiguration=no\n"
	}

	lower := cfg.Ports[0]
	if len(cfg.Ports) > 1 {
		lower = bond
		files[prefix+bond+".netdev"] = "[NetDev]\nName=" + bond + "\nKind=bond\n\n[Bond]\nMode=" + cfg.BondMode + "\nMIIMonitorSec=1s\n"
		for _, port := range cfg.Ports {
			member(port, "Bond="+bond)
		}
	}
	if cfg.VLAN > 0 {
		vlan := "vlan" + strconv.Itoa(cfg.VLAN)
		files[prefix+vlan+".netdev"] = "[NetDev]\nName=" + vlan + "\nKind=vlan\n\n[VLAN]\nId=" + strconv.Itoa(cfg.VLAN) + "\n"
		member(lower, "VLAN="+vlan)
		lower = vlan
	}
	if cfg.Bridge {
		// The bridge takes the hardware address of the first port. Its own
		// would be made up anew on every boot, and with it the address the
		// router hands out.
		files[prefix+Bridge+".netdev"] = "[NetDev]\nName=" + Bridge + "\nKind=bridge\nMACAddress=" + mac(cfg.Ports[0]) + "\n\n[Bridge]\nSTP=no\n"
		member(lower, "Bridge="+Bridge)
		lower = Bridge
	}

	var b strings.Builder
	// Only the topmost link owns addresses/routes. Explicit RA reception keeps
	// IPv6 working even when Docker enables forwarding on the NAS.
	b.WriteString("[Match]\nName=" + lower + "\n\n[Network]\nIPv6AcceptRA=yes\nKeepConfiguration=no\n")
	if cfg.DHCP {
		b.WriteString("DHCP=yes\n")
	} else {
		b.WriteString("DHCP=no\nAddress=" + cfg.Address + "\n")
		if cfg.Gateway != "" {
			b.WriteString("Gateway=" + cfg.Gateway + "\n")
		}
	}
	for _, server := range cfg.DNS {
		b.WriteString("DNS=" + server + "\n")
	}
	if cfg.DHCP {
		// The machine ID changes on every boot. Identify by MAC address so
		// that the router hands out the same address each time.
		b.WriteString("\n[DHCPv4]\nClientIdentifier=mac\n")
		if len(cfg.DNS) > 0 {
			b.WriteString("UseDNS=no\n")
		}
	}
	files[prefix+lower+".network"] = b.String()
	return files
}

// apply puts a configuration in place. The caller holds the lock.
func (m *Manager) apply(ctx context.Context, cfg Config) error {
	ctx, cancel := context.WithTimeout(ctx, applyTimeout)
	defer cancel()
	if _, err := m.runner.Run(ctx, "", "/usr/libexec/titanos-firewall-open"); err != nil {
		return fmt.Errorf("disable firewall filtering: %w", err)
	}
	files := m.render(cfg)
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	old, err := os.ReadDir(m.dir)
	if err != nil {
		return err
	}
	if m.retired == nil {
		m.retired = map[string]bool{}
	}
	for name := range m.retired {
		if len(cfg.Networks) == 0 && files[prefix+name+".netdev"] != "" {
			delete(m.retired, name)
		}
	}
	// Nothing new: a restart of titand (as for an app update) must not
	// take the network down for a moment, nor the bridge of the VMs.
	changed := len(m.retired) > 0 || cfg.Bridge && !m.HasBridge()
	for _, entry := range old {
		if name := entry.Name(); strings.HasPrefix(name, prefix) && files[name] == "" {
			changed = true
			if err := os.Remove(filepath.Join(m.dir, name)); err != nil {
				return err
			}
			if device, isDevice := strings.CutSuffix(strings.TrimPrefix(name, prefix), ".netdev"); isDevice && files[prefix+device+".netdev"] == "" {
				m.retired[device] = true
			}
		}
	}
	names := make([]string, 0, len(files))
	for name, content := range files {
		path := filepath.Join(m.dir, name)
		names = append(names, name)
		if current, err := os.ReadFile(path); err == nil && string(current) == content {
			continue
		} else if err == nil && len(cfg.Networks) > 0 && strings.HasSuffix(name, ".netdev") {
			m.retired[strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".netdev")] = true
		}
		changed = true
		if err := os.WriteFile(path+".tmp", []byte(content), 0o644); err != nil {
			return err
		}
		if err := os.Rename(path+".tmp", path); err != nil {
			os.Remove(path + ".tmp")
			return err
		}
	}
	if changed {
		if err := m.reconfigure(ctx, files); err != nil {
			return err
		}
	}
	if current, _ := os.Hostname(); cfg.Hostname != "" && cfg.Hostname != current {
		if _, err := m.runner.Run(ctx, "", "hostnamectl", "hostname", cfg.Hostname); err != nil {
			slog.Warn("hostname not set", "err", err)
		}
		// What announces the NAS in the network does so by its name.
		_, _ = m.runner.Run(ctx, "", "systemctl", "try-restart", "avahi-daemon.service", "smbd.service", "nmbd.service", "wsdd2.service") // announcing the new name is a convenience; the services take it at their next start
	}
	sort.Strings(names)
	slog.Info("network configuration in place", "device", device(cfg), "dhcp", cfg.DHCP, "address", cfg.Address, "files", names, "changed", changed)
	return nil
}

// reconfigure makes networkd take up changed files. The caller holds the lock.
func (m *Manager) reconfigure(ctx context.Context, files map[string]string) error {
	// New links can appear in sysfs before networkd has consumed RTM_NEWLINK.
	// They configure themselves on discovery; reconfigure only preexisting links.
	existing := map[string]bool{}
	for name := range files {
		if link, ok := strings.CutSuffix(strings.TrimPrefix(name, prefix), ".netdev"); ok && !m.retired[link] {
			if _, err := os.Stat(filepath.Join(m.sys, "class/net", link)); err == nil {
				existing[link] = true
			}
		}
	}
	if _, err := m.runner.Run(ctx, "", "networkctl", "reload"); err != nil {
		return err
	}
	// Forget obsolete .netdev files before deleting their links, so networkd
	// cannot recreate a bridge we are removing (including during rollback).
	deleted := false
	for name := range m.retired {
		if _, err := os.Stat(filepath.Join(m.sys, "class/net", name)); errors.Is(err, fs.ErrNotExist) {
			delete(m.retired, name)
			continue
		}
		if _, err := m.runner.Run(ctx, "", "networkctl", "delete", name); err != nil {
			return err
		}
		delete(m.retired, name)
		deleted = true
	}
	if deleted {
		if _, err := m.runner.Run(ctx, "", "networkctl", "reload"); err != nil {
			return err
		}
	}
	// Reconfigure both released/selected physical ports and existing virtual
	// links. Newly created links are configured by networkd on appearance.
	links := []string{}
	for _, port := range m.Interfaces() {
		links = append(links, port.Name)
	}
	for name := range files {
		if link, ok := strings.CutSuffix(strings.TrimPrefix(name, prefix), ".netdev"); ok && existing[link] {
			if _, err := os.Stat(filepath.Join(m.sys, "class/net", link)); err == nil {
				links = append(links, link)
			}
		}
	}
	sort.Strings(links)
	if len(links) > 0 {
		if _, err := m.runner.Run(ctx, "", "networkctl", append([]string{"reconfigure"}, links...)...); err != nil {
			return err
		}
	}
	return nil
}

// Change puts a new configuration on trial. It stays only if Confirm is
// called within two minutes; otherwise the previous one comes back.
func (m *Manager) Change(ctx context.Context, cfg Config) error {
	m.attachments.Lock()
	defer m.attachments.Unlock()
	if m.Disabled {
		return ErrDisabled
	}
	cfg = normal(cfg)
	if err := m.validate(cfg); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	if m.pending != nil {
		return ErrPending
	}
	if len(m.active.Networks) > 0 && len(cfg.Networks) == 0 {
		return fmt.Errorf("%w: topology cannot be replaced by a legacy request", ErrInvalid)
	}
	if m.CheckChange != nil {
		if err := m.CheckChange(cloneTopology(m.active), cloneTopology(cfg)); err != nil {
			return err
		}
	}
	return m.changeLocked(ctx, cfg)
}

// EnsureBridge preserves the current network topology and addressing. A new
// installation may request setup only while no explicit configuration exists.
// Port selection is required when the current uplink cannot be determined.
func (m *Manager) EnsureBridge(ctx context.Context, port string, onlyUnconfigured bool) error {
	m.attachments.Lock()
	defer m.attachments.Unlock()
	if m.Disabled {
		return ErrDisabled
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	if m.pending != nil {
		return ErrPending
	}
	if onlyUnconfigured && m.configured {
		return nil
	}
	if (m.active.Bridge || topologyHasBridge(m.active)) && m.HasBridge() {
		return nil
	}
	cfg := m.active
	if len(cfg.Networks) > 0 {
		return ErrBridgePort
	}
	cfg.Ports = slices.Clone(cfg.Ports)
	if len(cfg.Ports) == 0 {
		known := m.Interfaces()
		if port == "" {
			lookup, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			out, err := m.runner.Run(lookup, "", "ip", "-4", "route", "show", "default")
			if err != nil {
				return ErrBridgePort
			}
			candidates := map[string]bool{}
			unknown := false
			for _, line := range strings.Split(out, "\n") {
				fields := strings.Fields(line)
				for i := 0; i+1 < len(fields); i++ {
					if fields[i] == "dev" {
						if slices.ContainsFunc(known, func(p Interface) bool { return p.Name == fields[i+1] }) {
							candidates[fields[i+1]] = true
						} else {
							unknown = true
						}
					}
				}
			}
			if unknown || len(candidates) != 1 {
				return ErrBridgePort
			}
			for candidate := range candidates {
				port = candidate
			}
		}
		if !slices.ContainsFunc(known, func(p Interface) bool { return p.Name == port }) {
			return ErrBridgePort
		}
		cfg.Ports = []string{port}
	} else if port != "" && !slices.Contains(cfg.Ports, port) {
		return ErrBridgePort
	}
	cfg.Bridge = true
	if err := m.validate(cfg); err != nil {
		return err
	}
	return m.changeLocked(ctx, cfg)
}

// The caller holds mu and has checked configuration and pending changes.
func (m *Manager) changeLocked(ctx context.Context, cfg Config) error {
	// Losing the HTTP connection is expected while its interface moves to br0.
	// Finish the bounded operation and leave rollback to the confirmation timer.
	ctx = context.WithoutCancel(ctx)
	if err := m.apply(ctx, cfg); err != nil {
		// Half a change is worse than none.
		m.before, m.pending = m.active, &cfg
		return errors.Join(err, m.rollback(ctx))
	}
	m.before, m.pending, m.deadline = m.active, &cfg, time.Now().Add(m.trial)
	m.timer = time.AfterFunc(m.trial, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.pending != nil {
			slog.Warn("network change was not confirmed, going back")
			if err := m.rollback(context.Background()); err != nil {
				slog.Error("network rollback failed; will retry", "err", err)
			}
		}
	})
	return nil
}

// SetHostname gives the NAS a new name. Unlike everything else here, a
// name cannot cut the connection, so it applies and is stored at once.
func (m *Manager) SetHostname(ctx context.Context, hostname string) error {
	if m.Disabled {
		return ErrDisabled
	}
	if !hostnameRE.MatchString(hostname) {
		return fmt.Errorf("%w: hostname", ErrInvalid)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	if m.pending != nil {
		return ErrPending
	}
	cfg := m.active
	cfg.Hostname = hostname
	if err := m.apply(ctx, cfg); err != nil {
		return err
	}
	if err := m.saveConfig(cfg); err != nil {
		return err
	}
	m.active = cfg
	return nil
}

// rollback brings the previous configuration back. The caller holds the lock.
func (m *Manager) rollback(ctx context.Context) error {
	if m.timer != nil {
		m.timer.Stop()
	}
	m.reverting = true
	if err := m.apply(context.WithoutCancel(ctx), m.before); err != nil {
		// A failed rollback must not silently clear the pending change, nor may
		// it be confirmed while half-restored. Retry transient networkd failures.
		m.timer = time.AfterFunc(5*time.Second, func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.pending != nil {
				if err := m.rollback(context.Background()); err != nil {
					slog.Error("network rollback failed; will retry", "err", err)
				}
			}
		})
		return err
	}
	m.pending, m.reverting = nil, false
	return nil
}

// Confirm keeps the change on trial and stores it on the stick.
func (m *Manager) Confirm() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil {
		return ErrNoPending
	}
	if m.reverting {
		return ErrReverting
	}
	if len(m.pending.Networks) > 0 {
		return fmt.Errorf("%w: confirm through the new management connection", ErrInvalid)
	}
	if err := m.saveConfig(*m.pending); err != nil {
		return err
	}
	m.timer.Stop()
	m.active, m.pending = *m.pending, nil
	m.configured = true
	slog.Info("network change confirmed")
	return nil
}

// Revert gives up the change on trial at once.
func (m *Manager) Revert(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil {
		return ErrNoPending
	}
	return m.rollback(ctx)
}

// PendingSeconds returns how long a change on trial has left, 0 if there is none.
func (m *Manager) PendingSeconds() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil {
		return 0
	}
	return max(1, int(time.Until(m.deadline).Seconds()))
}

// HasBridge reports whether the bridge for VMs is there.
func (m *Manager) HasBridge() bool {
	_, err := os.Stat(filepath.Join(m.sys, "class/net", Bridge))
	return err == nil
}

// BridgeReady excludes a bridge that is still on trial or being reverted.
func (m *Manager) BridgeReady() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loaded && (m.active.Bridge || topologyHasBridge(m.active)) && m.pending == nil && m.HasBridge()
}

// Status returns the configuration and what the network looks like right now.
func (m *Manager) Status(ctx context.Context) Status {
	m.mu.Lock()
	status := Status{Config: cloneTopology(m.active), Configured: m.configured, BridgeReady: m.loaded && (m.active.Bridge || topologyHasBridge(m.active)) && m.pending == nil && m.HasBridge(), Addresses: []string{}, DNS: []string{}}
	if m.pending != nil {
		pending := *m.pending
		status.Pending = &pending
		status.PendingSeconds = max(1, int(time.Until(m.deadline).Seconds()))
	}
	status.VLANsInUse = m.vlansInUseLocked()
	status.VLANs = m.options.VLANs || status.VLANsInUse
	m.mu.Unlock()
	status.Interfaces = m.Interfaces()

	// What the kernel routes outgoing traffic through says more than the
	// configuration, which may not have worked.
	if out, err := m.runner.Run(ctx, "", "ip", "-4", "route", "show", "default"); err == nil {
		fields := strings.Fields(strings.SplitN(out, "\n", 2)[0])
		for i := 0; i+1 < len(fields); i++ {
			switch fields[i] {
			case "via":
				status.Gateway = fields[i+1]
			case "dev":
				status.Device = fields[i+1]
			}
		}
	}
	routeDevice := status.Device
	if status.Device == "" {
		cfg := status.Config
		if status.Pending != nil {
			cfg = *status.Pending
		}
		status.Device = device(cfg)
	}
	// Legacy configurations are shown in the multi-network form; the same
	// form is what the page sends back, on trial, to migrate.
	shown := status.Config
	if status.Pending != nil {
		shown = *status.Pending
	}
	up := uplink(status.Interfaces, routeDevice)
	var devices map[string]string
	status.Migrated = len(status.Config.Networks) == 0
	status.Topology = migrateLegacy(status.Config, up)
	if status.Pending != nil {
		pending := migrateLegacy(*status.Pending, up)
		status.PendingTopology = &pending
		devices = liveDevices(shown, pending, routeDevice)
	} else {
		devices = liveDevices(shown, status.Topology, routeDevice)
	}
	status.Live = m.readLive(ctx, devices)
	if status.Device != "" {
		if out, err := m.runner.Run(ctx, "", "ip", "-o", "addr", "show", "dev", status.Device); err == nil {
			for _, line := range strings.Split(out, "\n") {
				fields := strings.Fields(line)
				if at := slices.Index(fields, "inet"); at >= 0 && at+1 < len(fields) {
					status.Addresses = append(status.Addresses, fields[at+1])
				}
				if at := slices.Index(fields, "inet6"); at >= 0 && at+1 < len(fields) {
					status.Addresses = append(status.Addresses, fields[at+1])
				}
			}
		}
	}
	if data, err := os.ReadFile("/run/systemd/resolve/resolv.conf"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if server, ok := strings.CutPrefix(line, "nameserver "); ok {
				status.DNS = append(status.DNS, strings.TrimSpace(server))
			}
		}
	}
	return status
}

// configuredPorts are the Ethernet ports a configuration is built on.
func configuredPorts(cfg Config) []string {
	ports := slices.Clone(cfg.Ports)
	for _, b := range cfg.Bonds {
		ports = append(ports, b.Ports...)
	}
	for _, n := range cfg.Networks {
		if n.Parent != "" && !strings.HasPrefix(n.Parent, "bond:") {
			ports = append(ports, n.Parent)
		}
	}
	return ports
}

// waitPorts waits up to portWait for the configured ports: at boot titand
// may start before a slow driver has created them. A port that does not
// come is left to validate, which then refuses the configuration.
func (m *Manager) waitPorts(ctx context.Context, cfg Config) {
	wanted := configuredPorts(cfg)
	missing := func() []string {
		known := m.Interfaces()
		var absent []string
		for _, port := range wanted {
			if !slices.ContainsFunc(known, func(i Interface) bool { return i.Name == port }) {
				absent = append(absent, port)
			}
		}
		return absent
	}
	absent := missing()
	if len(absent) == 0 || m.portWait <= 0 {
		return
	}
	slog.Info("waiting for network ports", "ports", absent, "for", m.portWait)
	deadline := time.Now().Add(m.portWait)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if absent = missing(); len(absent) == 0 {
			slog.Info("network ports are there")
			return
		}
	}
	slog.Warn("network ports did not appear, the configuration cannot be put in place", "ports", absent)
}
