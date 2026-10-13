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
	trial = 2 * time.Minute
	// prefix marks the files this package writes for systemd-networkd.
	prefix = "10-titanos-"
)

var (
	ErrDisabled  = errors.New("network: not available in development mode")
	ErrNotLoaded = errors.New("network: configuration could not be read from /config")
	// ErrInvalid means a setting is not acceptable; the message says which.
	ErrInvalid   = errors.New("network: invalid setting")
	ErrNoPending = errors.New("network: no change is waiting for confirmation")
	ErrPending   = errors.New("network: a change is waiting for confirmation")
)

var hostnameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// BondModes are the ways several ports can work as one.
var BondModes = []string{"active-backup", "802.3ad", "balance-alb"}

// Config is /config/network/network.json.
type Config struct {
	Hostname string `json:"hostname"`
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
	Config Config `json:"config"`
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
}

// Manager applies the configuration and keeps changes on trial.
type Manager struct {
	store  *config.Store
	runner run.Runner
	// Disabled leaves the system alone. Set in development mode.
	Disabled bool
	// dir is where systemd-networkd reads its files from; sys is /sys.
	dir, sys string
	// trial is how long a change waits for confirmation.
	trial time.Duration

	mu      sync.Mutex
	loaded  bool
	active  Config
	pending *Config
	// before is what comes back if the pending change is not confirmed.
	before   Config
	deadline time.Time
	timer    *time.Timer
}

// NewManager returns a manager working on the real system.
func NewManager(store *config.Store) *Manager {
	return &Manager{store: store, runner: run.Exec{}, dir: "/etc/systemd/network", sys: "/sys", trial: trial}
}

func defaults() Config {
	hostname, _ := os.Hostname()
	return Config{Hostname: hostname, DHCP: true, Ports: []string{}, DNS: []string{}}
}

// Load reads the configuration and puts it in place. It is called once
// at the start, before anyone could have changed anything.
func (m *Manager) Load(ctx context.Context) error {
	cfg := defaults()
	err := m.store.ReadJSON(configFile, &cfg)
	missing := errors.Is(err, fs.ErrNotExist)
	if err != nil && !missing {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active, m.loaded = normal(cfg), true
	if missing || m.Disabled {
		return nil
	}
	return m.apply(ctx, m.active)
}

func normal(cfg Config) Config {
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
		files[prefix+name+".network"] = "[Match]\nName=" + name + "\n\n[Network]\n" + line + "\nLinkLocalAddressing=no\nDHCP=no\n"
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
	b.WriteString("[Match]\nName=" + lower + "\n\n[Network]\n")
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
	files := m.render(cfg)
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	old, _ := os.ReadDir(m.dir)
	for _, entry := range old {
		if name := entry.Name(); strings.HasPrefix(name, prefix) && files[name] == "" {
			os.Remove(filepath.Join(m.dir, name))
			// A bridge, bond or VLAN that is no longer wanted has to go, or
			// it keeps the port.
			if device, isDevice := strings.CutSuffix(strings.TrimPrefix(name, prefix), ".netdev"); isDevice && files[prefix+device+".netdev"] == "" {
				m.runner.Run(ctx, "", "networkctl", "delete", device)
			}
		}
	}
	names := make([]string, 0, len(files))
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(m.dir, name), []byte(content), 0o644); err != nil {
			return err
		}
		names = append(names, name)
	}
	if _, err := m.runner.Run(ctx, "", "networkctl", "reload"); err != nil {
		return err
	}
	// Ports that changed their role do not notice by themselves.
	for _, port := range m.Interfaces() {
		m.runner.Run(ctx, "", "networkctl", "reconfigure", port.Name)
	}
	if current, _ := os.Hostname(); cfg.Hostname != "" && cfg.Hostname != current {
		if _, err := m.runner.Run(ctx, "", "hostnamectl", "hostname", cfg.Hostname); err != nil {
			slog.Warn("hostname not set", "err", err)
		}
		// What announces the NAS in the network does so by its name.
		m.runner.Run(ctx, "", "systemctl", "try-restart", "avahi-daemon.service", "smbd.service", "nmbd.service", "wsdd2.service")
	}
	sort.Strings(names)
	slog.Info("network configuration in place", "device", device(cfg), "dhcp", cfg.DHCP, "address", cfg.Address, "files", names)
	return nil
}

// Change puts a new configuration on trial. It stays only if Confirm is
// called within two minutes; otherwise the previous one comes back.
func (m *Manager) Change(ctx context.Context, cfg Config) error {
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
	if err := m.apply(ctx, cfg); err != nil {
		// Half a change is worse than none.
		m.apply(context.WithoutCancel(ctx), m.active)
		return err
	}
	m.before, m.pending, m.deadline = m.active, &cfg, time.Now().Add(m.trial)
	m.timer = time.AfterFunc(m.trial, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.pending != nil {
			slog.Warn("network change was not confirmed, going back")
			m.rollback(context.Background())
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
	if err := m.store.WriteJSON(configFile, cfg); err != nil {
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
	m.pending = nil
	return m.apply(ctx, m.before)
}

// Confirm keeps the change on trial and stores it on the stick.
func (m *Manager) Confirm() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil {
		return ErrNoPending
	}
	if err := m.store.WriteJSON(configFile, *m.pending); err != nil {
		return err
	}
	m.timer.Stop()
	m.active, m.pending = *m.pending, nil
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

// Status returns the configuration and what the network looks like right now.
func (m *Manager) Status(ctx context.Context) Status {
	m.mu.Lock()
	status := Status{Config: m.active, Addresses: []string{}, DNS: []string{}}
	if m.pending != nil {
		pending := *m.pending
		status.Pending = &pending
		status.PendingSeconds = max(1, int(time.Until(m.deadline).Seconds()))
	}
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
	if status.Device != "" {
		if out, err := m.runner.Run(ctx, "", "ip", "-4", "-o", "addr", "show", "dev", status.Device); err == nil {
			for _, line := range strings.Split(out, "\n") {
				fields := strings.Fields(line)
				if at := slices.Index(fields, "inet"); at >= 0 && at+1 < len(fields) {
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
