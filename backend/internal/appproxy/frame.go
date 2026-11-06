package appproxy

// The desktop of TitanOS (PLAN 61) shows apps in windows: frames of their
// HTTPS names inside the web UI. Many apps forbid being framed
// (X-Frame-Options, CSP frame-ancestors). The proxy takes that away and
// lets exactly the origins of the TitanOS web UI frame them, never '*',
// so that no other site can put an app into a frame of its own
// (clickjacking).

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// frameAncestors is the directive that lets the web UI frame an app.
func frameAncestors(origins []string) string {
	return "frame-ancestors " + strings.Join(append([]string{"'self'"}, origins...), " ")
}

// rewriteFrameHeaders drops X-Frame-Options and sets frame-ancestors in
// every policy of the app's Content-Security-Policy that names it, or adds
// it as a policy of its own if none does. Report-only policies get the
// same directive where they have one, but none is added there.
func rewriteFrameHeaders(h http.Header, origins []string) {
	h.Del("X-Frame-Options")
	directive := frameAncestors(origins)
	if !replaceFrameAncestors(h, "Content-Security-Policy", directive) {
		h.Add("Content-Security-Policy", directive)
	}
	replaceFrameAncestors(h, "Content-Security-Policy-Report-Only", directive)
}

// replaceFrameAncestors rewrites the frame-ancestors directive in every
// policy of a header (several lines, or several policies joined by
// commas, each enforced on its own) and tells whether one had it.
func replaceFrameAncestors(h http.Header, name, directive string) bool {
	lines := h.Values(name)
	if len(lines) == 0 {
		return false
	}
	found := false
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		policies := strings.Split(line, ",")
		for i, policy := range policies {
			directives := strings.Split(policy, ";")
			for j, d := range directives {
				field := strings.Fields(d)
				if len(field) > 0 && strings.EqualFold(field[0], "frame-ancestors") {
					lead := d[:len(d)-len(strings.TrimLeft(d, " \t"))]
					directives[j] = lead + directive
					found = true
				}
			}
			policies[i] = strings.Join(directives, ";")
		}
		out = append(out, strings.Join(policies, ","))
	}
	h.Del(name)
	for _, line := range out {
		h.Add(name, line)
	}
	return found
}

// uiOrigins builds the https origins under which the web UI is reached:
// the NAS's name with and without .local, the names given by hostnames
// (Let's Encrypt, dynamic DNS, the tailnet) and the NAS's addresses. The
// list is kept for a minute: it is asked for on every response.
type uiOrigins struct {
	mu    sync.Mutex
	list  []string
	until time.Time
}

func (m *Manager) frameOrigins() []string {
	m.origins.mu.Lock()
	defer m.origins.mu.Unlock()
	if m.origins.list != nil && time.Now().Before(m.origins.until) {
		return m.origins.list
	}
	m.origins.list = m.buildOrigins()
	m.origins.until = time.Now().Add(time.Minute)
	return m.origins.list
}

func (m *Manager) buildOrigins() []string {
	port := ""
	if m.HTTPSPort != 0 && m.HTTPSPort != 443 {
		port = ":" + strconv.Itoa(m.HTTPSPort)
	}
	var hosts []string
	add := func(host string) {
		host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
		if host == "" || strings.ContainsAny(host, " ;,'*") || slices.Contains(hosts, host) {
			return
		}
		hosts = append(hosts, host)
	}
	if name := m.host(); name != "" {
		add(name)
		add(name + ".local")
	}
	if m.UIHosts != nil {
		for _, host := range m.UIHosts() {
			// A certificate or DNS name may also name apps (Let's Encrypt
			// with the apps' domains): an app never frames another app.
			if _, _, app := m.match(host); app {
				continue
			}
			if _, app := m.tailnetMatch(hostOnly(host)); app {
				continue
			}
			add(host)
		}
	}
	addrs := m.addresses
	if addrs == nil {
		addrs = interfaceAddresses
	}
	for _, addr := range addrs() {
		if addr.Is6() {
			add("[" + addr.String() + "]")
		} else {
			add(addr.String())
		}
	}
	out := make([]string, 0, len(hosts))
	for _, host := range hosts {
		out = append(out, "https://"+host+port)
	}
	return out
}

// interfaceAddresses are the NAS's own addresses, without loopback and
// link-local ones (a browser cannot open those as an origin anyway).
func interfaceAddresses() []netip.Addr {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []netip.Addr
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		addr, ok := netip.AddrFromSlice(ipnet.IP)
		if !ok {
			continue
		}
		addr = addr.Unmap()
		if addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsMulticast() {
			continue
		}
		out = append(out, addr)
	}
	return out
}
