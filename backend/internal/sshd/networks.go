package sshd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// SSH only from chosen networks. sshd itself enforces it with AllowUsers
// patterns of the form *@<network>; the firewall is not touched. AllowUsers
// and AllowGroups (in the base configuration) must both let a login in.

const (
	// networksFile is written next to 10-titanos.conf of the image.
	networksFile = "sshd_config.d/20-titanos-networks.conf"
	// maxNetworks keeps the list and the sshd line short.
	maxNetworks = 32
)

var (
	// ErrInvalidNetwork means an entry is no network in CIDR notation.
	ErrInvalidNetwork = errors.New("sshd: not a network in CIDR notation")
	// ErrTooManyNetworks means the list is longer than maxNetworks.
	ErrTooManyNetworks = errors.New("sshd: too many networks")
)

// Presets the settings page offers next to the home network.
var (
	// TailscaleNetworks is what Tailscale hands out to the devices of a
	// tailnet. NetBird uses the same IPv4 range by default.
	TailscaleNetworks = []string{"100.64.0.0/10", "fd7a:115c:a1e0::/48"}
	// NetBirdNetworks is the default range of NetBird (it has no IPv6 range
	// of its own by default).
	NetBirdNetworks = []string{"100.64.0.0/10"}
)

// NormalizeNetworks checks a list of networks and returns it in one form:
// masked prefixes, a single address as /32 or /128, no duplicates, in the
// given order. An empty list means all networks.
func NormalizeNetworks(list []string) ([]string, error) {
	out := []string{}
	for _, entry := range list {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		prefix, err := parseNetwork(entry)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", ErrInvalidNetwork, entry)
		}
		if s := prefix.String(); !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	if len(out) > maxNetworks {
		return nil, ErrTooManyNetworks
	}
	return out, nil
}

func parseNetwork(entry string) (netip.Prefix, error) {
	// Zones (fe80::1%eth0) mean nothing to sshd.
	if strings.Contains(entry, "%") {
		return netip.Prefix{}, ErrInvalidNetwork
	}
	if !strings.Contains(entry, "/") {
		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return netip.Prefix{}, err
		}
		addr = addr.Unmap()
		return netip.PrefixFrom(addr, addr.BitLen()), nil
	}
	prefix, err := netip.ParsePrefix(entry)
	if err != nil {
		return netip.Prefix{}, err
	}
	if prefix.Addr().Is4In6() {
		// sshd sees IPv4 clients as IPv4, never as ::ffff:a.b.c.d.
		bits := prefix.Bits() - 96
		if bits < 0 {
			return netip.Prefix{}, ErrInvalidNetwork
		}
		prefix = netip.PrefixFrom(prefix.Addr().Unmap(), bits)
	}
	return prefix.Masked(), nil
}

// Covers says whether address (as the web server saw it) lies in one of
// the networks. An empty list covers every address.
func Covers(networks []string, address string) bool {
	if len(networks) == 0 {
		return true
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(address))
	if err != nil {
		return false
	}
	addr = addr.Unmap().WithZone("")
	for _, entry := range networks {
		if prefix, err := parseNetwork(entry); err == nil && prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// networksConfig is the drop-in for sshd; empty for no restriction.
func networksConfig(networks []string) string {
	if len(networks) == 0 {
		return ""
	}
	patterns := make([]string, len(networks))
	for i, network := range networks {
		patterns[i] = "*@" + network
	}
	return "# Written by titand: SSH only from the networks chosen under\n" +
		"# Settings > Access. AllowGroups titanos-ssh still applies.\n" +
		"AllowUsers " + strings.Join(patterns, " ") + "\n"
}

// SetNetworks sets the networks SSH logins may come from; an empty list
// allows all. The list is checked here; sshd -t checks the result before
// sshd uses it.
func (m *Manager) SetNetworks(networks []string) ([]string, error) {
	if m.Disabled {
		return nil, ErrDisabled
	}
	normalized, err := NormalizeNetworks(networks)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if !m.loaded {
		m.mu.Unlock()
		return nil, ErrNotLoaded
	}
	settings := m.settings
	settings.AllowedNetworks = normalized
	if err := m.store.WriteJSON(settingsFile, settings); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	m.settings = settings
	m.mu.Unlock()
	m.Kick()
	return normalized, nil
}

// ClearNetworks lets SSH in from all networks again, straight in /config.
// It is for the local console (titand -network-recovery) while titand is
// stopped; titand applies it when it starts.
func ClearNetworks(store *config.Store) (bool, error) {
	var settings Settings
	if err := store.ReadJSON(settingsFile, &settings); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if len(settings.AllowedNetworks) == 0 {
		return false, nil
	}
	settings.AllowedNetworks = nil
	return true, store.WriteJSON(settingsFile, settings)
}

// applyNetworks puts the drop-in in place and has sshd check the whole
// configuration. A configuration sshd refuses is taken back, so the
// running sshd and the next start keep the last good one. It returns
// whether the file changed.
func (m *Manager) applyNetworks(ctx context.Context, networks []string) (bool, error) {
	file := filepath.Join(m.etcDir, networksFile)
	want := networksConfig(networks)
	before, readErr := os.ReadFile(file)
	had := readErr == nil
	if string(before) == want && (had || want == "") {
		return false, nil
	}
	if want == "" {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return false, err
		}
		tmp := file + ".tmp"
		if err := os.WriteFile(tmp, []byte(want), 0o644); err != nil {
			return false, err
		}
		if err := os.Rename(tmp, file); err != nil {
			os.Remove(tmp)
			return false, err
		}
	}
	if output, err := m.runner.Run(ctx, "", "sshd", "-t"); err != nil {
		if had {
			_ = os.WriteFile(file, before, 0o644) // going back to the file before; the check error is returned
		} else {
			os.Remove(file)
		}
		return false, fmt.Errorf("sshd refused the network list: %v %s", err, strings.TrimSpace(output))
	}
	slog.Info("SSH networks put in place", "networks", networks)
	return true, nil
}

// iface is what HomeNetworks needs to know of a network interface.
type iface struct {
	name  string
	up    bool
	addrs []netip.Prefix
}

// skippedInterfaces are virtual ones that do not lead to the home network:
// containers, VMs, VPNs.
var skippedInterfaces = []string{"lo", "docker", "br-", "veth", "virbr", "vnet", "tap", "tailscale", "wt", "tun", "wg"}

// homeNetworks returns the networks the NAS itself is in, without
// loopback, link-local and virtual interfaces.
func homeNetworks(ifaces []iface) []string {
	out := []string{}
	for _, i := range ifaces {
		if !i.up || slices.ContainsFunc(skippedInterfaces, func(prefix string) bool { return strings.HasPrefix(i.name, prefix) }) {
			continue
		}
		for _, p := range i.addrs {
			addr := p.Addr()
			if addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsMulticast() {
				continue
			}
			if s := p.Masked().String(); !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
	}
	return out
}

// HomeNetworks returns the networks the NAS is connected to right now.
func HomeNetworks() []string {
	list, err := net.Interfaces()
	if err != nil {
		return []string{}
	}
	var ifaces []iface
	for _, n := range list {
		i := iface{name: n.Name, up: n.Flags&net.FlagUp != 0}
		addrs, _ := n.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			addr, ok := netip.AddrFromSlice(ipnet.IP)
			if !ok {
				continue
			}
			ones, _ := ipnet.Mask.Size()
			addr = addr.Unmap()
			if addr.Is4() && ones > 32 {
				ones -= 96
			}
			i.addrs = append(i.addrs, netip.PrefixFrom(addr, ones))
		}
		ifaces = append(ifaces, i)
	}
	return homeNetworks(ifaces)
}
