package appproxy

import (
	"context"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/run"
)

// Run publishes the .local names of the apps over mDNS until ctx is done.
// avahi reads them from /etc/avahi/hosts (in RAM, made again from /config
// after every start); titand rewrites it when the routes or the address of
// the NAS change and has avahi read it again.
func (m *Manager) Run(ctx context.Context) {
	if m.Disabled {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if err := m.Publish(ctx); err != nil {
			slog.Warn("app names not published over mDNS", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-m.kick:
		}
	}
}

// Publish brings the avahi hosts file up to date; it does nothing if the
// content did not change.
func (m *Manager) Publish(ctx context.Context) error {
	if m.Disabled {
		return nil
	}
	content := m.hostsContent()
	m.mu.RLock()
	same := content == m.published
	m.mu.RUnlock()
	if same {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.hostsFile), 0o755); err != nil {
		return err
	}
	tmp := m.hostsFile + ".titanos-new"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
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
	m.mu.Lock()
	m.published = content
	m.mu.Unlock()
	return nil
}

// hostsContent is the avahi hosts file: one line "address name" for each
// name of an enabled route, under the local name and, if chosen, as short
// name. Without an address or with mDNS off it names nothing.
func (m *Manager) hostsContent() string {
	var b strings.Builder
	b.WriteString("# Made by TitanOS from /config/docker/proxy.json; changes are overwritten.\n")
	address := m.address
	if address == nil {
		address = primaryIPv4
	}
	m.mu.RLock()
	local := m.localSuffix(m.settings)
	m.mu.RUnlock()
	ip := address()
	if ip == "" || local == "" {
		return b.String()
	}
	hosts := m.shortNames()
	for _, e := range m.enabled() {
		for _, name := range names(e.route) {
			hosts = append(hosts, name+"."+local)
		}
	}
	sort.Strings(hosts)
	for _, name := range slices.Compact(hosts) {
		b.WriteString(ip + " " + name + "\n")
	}
	return b.String()
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
