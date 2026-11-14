package appproxy

import (
	"context"
	"log/slog"
	"net"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/run"
)

// Run publishes the .local names of the apps over mDNS until ctx is done:
// over avahi's D-Bus interface (mdns.go), looked at again every 30 s and
// when the routes or the address of the NAS change. A failure is logged
// when it is new, not every 30 s.
func (m *Manager) Run(ctx context.Context) {
	if m.Disabled {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	said := ""
	for {
		err := m.Publish(ctx)
		switch {
		case err != nil && err.Error() != said:
			slog.Warn("app names not published over mDNS", "err", err)
			said = err.Error()
		case err == nil && said != "":
			slog.Info("app names published over mDNS again")
			said = ""
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-m.kick:
		}
	}
}

// Publish brings the published names up to date: nothing happens when
// address and names are the same and avahi still holds them.
func (m *Manager) Publish(ctx context.Context) error {
	if m.Disabled {
		return nil
	}
	if err := m.clearHostsFile(ctx); err != nil {
		return err
	}
	ip, names := m.mdnsNames()
	key := ip + " " + strings.Join(names, " ")
	m.mu.RLock()
	same := key == m.published
	m.mu.RUnlock()
	if same && (len(names) == 0 || m.mdns.Alive(ctx)) {
		return nil
	}
	if err := m.mdns.Publish(ctx, ip, names); err != nil {
		return err
	}
	m.mu.Lock()
	m.published = key
	m.mu.Unlock()
	return nil
}

// hostsHeader is all /etc/avahi/hosts holds now. Up to 0.80 titand wrote
// the names there (in RAM, made after every start); each failed with
// "Local name collision" (mdns.go). A file with names of an older titand
// is emptied once and avahi reads it again, so that its log stops.
const hostsHeader = "# The .local names of apps are published by titand over D-Bus (not here).\n"

func (m *Manager) clearHostsFile(ctx context.Context) error {
	m.mu.RLock()
	done := m.hostsCleared
	m.mu.RUnlock()
	if done {
		return nil
	}
	data, err := os.ReadFile(m.hostsFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if hasHostLines(string(data)) {
		tmp := m.hostsFile + ".titanos-new"
		if err := os.WriteFile(tmp, []byte(hostsHeader), 0o644); err != nil {
			return err
		}
		if err := os.Rename(tmp, m.hostsFile); err != nil {
			return err
		}
		reload := m.reload
		if reload == nil {
			reload = func() error {
				// Only a running avahi is told; the shares start it when needed.
				_, err := (run.Exec{}).Run(ctx, "", "systemctl", "try-reload-or-restart", "avahi-daemon.service")
				return err
			}
		}
		if err := reload(); err != nil {
			return err
		}
	}
	m.mu.Lock()
	m.hostsCleared = true
	m.mu.Unlock()
	return nil
}

// hasHostLines tells whether an avahi hosts file names a host.
func hasHostLines(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			return true
		}
	}
	return false
}

// mdnsNames are the address of the NAS and the names published on it:
// each name of an enabled route under the local name and, if chosen, as
// short name. Without an address or with mDNS off, none.
func (m *Manager) mdnsNames() (string, []string) {
	address := m.address
	if address == nil {
		address = primaryIPv4
	}
	m.mu.RLock()
	local := m.localSuffix(m.settings)
	m.mu.RUnlock()
	ip := address()
	if ip == "" || local == "" {
		return ip, nil
	}
	hosts := m.shortNames()
	for _, e := range m.enabled() {
		for _, name := range names(e.route) {
			hosts = append(hosts, name+"."+local)
		}
	}
	sort.Strings(hosts)
	return ip, slices.Compact(hosts)
}

// virtual are interfaces whose addresses are not the NAS's in the home
// network: containers, VMs, Tailscale, NetBird.
var virtual = []string{"lo", "docker", "br-", "veth", "virbr", "vnet", "tailscale", "wt", "macvtap"}

// primaryIPv4 returns the address the NAS has in the home network: the one
// it would send from to the internet, else the first private IPv4 of a
// physical interface or bridge.
func primaryIPv4() string {
	// No packet is sent: connecting a UDP socket only picks the route.
	if conn, err := net.Dial("udp4", "192.0.2.1:9"); err == nil {
		addr, _ := conn.LocalAddr().(*net.UDPAddr)
		conn.Close()
		if addr != nil && addr.IP.IsPrivate() {
			return addr.IP.String()
		}
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || hasAnyPrefix(iface.Name, virtual) {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil && ipnet.IP.IsPrivate() {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

func hasAnyPrefix(name string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// PrimaryIPv4 is the address of the NAS in the home network (see primaryIPv4).
func PrimaryIPv4() string { return primaryIPv4() }
