package appproxy

// HTTPS ports of apps (PLAN 62): besides its names, every Docker app gets
// a fixed port of its own on the NAS's addresses, e.g.
// https://192.168.10.5:20001. Many clients do not resolve names such as
// immich.nas.local (Linux with nss-mdns resolves only two-label .local
// names); an address with a port needs no name at all. titand serves the
// ports itself with the same handler as the names: frame headers, the
// browser gate, the account's rights and WebSockets are the same.
//
// A port is given the first time an app's route is known (its own route
// or an automatic one) and stored in Settings.Ports, so that it stays the
// same across restarts and updates. Only removing the app frees it
// (ForgetApp); a stopped app keeps it.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The range of app ports: five digits, so that they do not meet the
// usual four-digit ports of apps, and below the ports Linux picks for
// outgoing connections (32768 and up).
const (
	PortFirst = 20000
	PortLast  = 20999
)

// fixedPorts are ports in the range that services of the NAS use without
// always listening: 20048 is the mountd of NFS (/etc/nfs.conf.d/
// titanos-ports.conf), which only runs with NFS on.
var fixedPorts = []int{20048}

// portCookiePrefix names the browser's pass for an app port. Cookies are
// not kept apart by port: all ports of 192.168.10.5 share one cookie jar,
// so each port has a pass of its own name.
const portCookiePrefix = gateCookie + "_"

// portListeners are the listeners of the app ports.
type portListeners struct {
	mu sync.Mutex
	// open maps a port to its listener.
	open map[int]net.Listener
	// failed are ports that could not be opened, logged once.
	failed map[int]bool
	// reported is what OnPorts was last told.
	reported []int
	// listen and serve are replaced in tests.
	listen func(port int) (net.Listener, error)
	serve  func(srv *http.Server, ln net.Listener) error
}

func (m *Manager) portBounds() (int, int) {
	if m.portRange != [2]int{} {
		return m.portRange[0], m.portRange[1]
	}
	return PortFirst, PortLast
}

// SetPublishedPorts tells the manager every TCP port the containers
// publish on the NAS (Docker's port bindings); none of them is given to
// an app. Until it is called once, no port is given at all.
func (m *Manager) SetPublishedPorts(ports []int) {
	m.mu.Lock()
	m.dockerPorts = slices.Clone(ports)
	m.dockerKnown = true
	m.mu.Unlock()
}

// needsPorts tells whether an app with a route has no port yet; m.mu is
// held (or s is a private copy).
func (m *Manager) needsPorts(s Settings, auto map[string]Route) bool {
	if m.Disabled || !m.dockerKnown {
		return false
	}
	for _, routes := range []map[string]Route{s.Apps, auto} {
		for app := range routes {
			if _, ok := s.Ports[app]; !ok {
				return true
			}
		}
	}
	return false
}

// assignPorts gives every app with a route (its own or an automatic one)
// a port if it has none. A stored port is never given anew. A free port
// is none the containers publish, none of titand's, none of the fixed
// ports of the NAS and none that something on the NAS listens on now.
// It tells whether a port was given. m.mu is held.
func (m *Manager) assignPorts(s *Settings, auto map[string]Route) bool {
	if !m.needsPorts(*s, auto) {
		return false
	}
	apps := slices.Sorted(maps.Keys(s.Apps))
	for app := range auto {
		if !slices.Contains(apps, app) {
			apps = append(apps, app)
		}
	}
	slices.Sort(apps)
	if s.Ports == nil {
		s.Ports = map[string]int{}
	}
	taken := map[int]bool{}
	for _, p := range s.Ports {
		taken[p] = true
	}
	for _, p := range slices.Concat(m.dockerPorts, m.Reserved, fixedPorts) {
		taken[p] = true
	}
	if m.HTTPSPort > 0 {
		taken[m.HTTPSPort] = true
	}
	free := m.portFree
	if free == nil {
		free = portFreeNow
	}
	first, last := m.portBounds()
	given := false
	next := first
	for _, app := range apps {
		if _, ok := s.Ports[app]; ok {
			continue
		}
		for ; next <= last; next++ {
			if !taken[next] && free(next) {
				break
			}
		}
		if next > last {
			slog.Warn("no free HTTPS port for app", "app", app, "range", fmt.Sprintf("%d-%d", first, last))
			return given
		}
		s.Ports[app] = next
		taken[next] = true
		given = true
		slog.Info("HTTPS port given to app", "app", app, "port", next)
	}
	return given
}

// portFreeNow tells whether nothing on the NAS listens on a TCP port: it
// tries to listen there (all addresses) and lets go at once.
func portFreeNow(port int) bool {
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// assignedPorts are all stored app ports.
func (m *Manager) assignedPorts() []int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Sorted(maps.Values(m.settings.Ports))
}

// ForgetApp forgets an app that was removed: its own route and its port.
// An app that only stopped keeps both.
func (m *Manager) ForgetApp(app string) error {
	m.mu.RLock()
	_, route := m.settings.Apps[app]
	_, port := m.settings.Ports[app]
	m.mu.RUnlock()
	if !route && !port {
		return nil
	}
	return m.change(func(s *Settings) error {
		delete(s.Apps, app)
		delete(s.Ports, app)
		return nil
	})
}

// Port is the stored HTTPS port of an app whose route answers, or 0.
func (m *Manager) Port(app string) int {
	if _, ok := m.Route(app); !ok {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settings.Ports[app]
}

// routeByPort finds the app on one of the app ports, whatever name the
// request asks for.
func (m *Manager) routeByPort(port int) (string, Route, bool) {
	if port == 0 {
		return "", Route{}, false
	}
	m.mu.RLock()
	app := ""
	for a, p := range m.settings.Ports {
		if p == port {
			app = a
			break
		}
	}
	m.mu.RUnlock()
	if app == "" {
		return "", Route{}, false
	}
	route, ok := m.Route(app)
	return "app:" + app, route, ok
}

// wantedPorts are the ports of apps whose route answers now, sorted.
func (m *Manager) wantedPorts() []int {
	var out []int
	for _, e := range m.enabled() {
		app, ok := strings.CutPrefix(e.key, "app:")
		if !ok {
			continue
		}
		m.mu.RLock()
		port, has := m.settings.Ports[app]
		m.mu.RUnlock()
		if has {
			out = append(out, port)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// servedPort is the port of an app if titand listens on it now.
func (m *Manager) servedPort(app string) (int, bool) {
	port := m.Port(app)
	if port == 0 {
		return 0, false
	}
	m.ports.mu.Lock()
	defer m.ports.mu.Unlock()
	_, ok := m.ports.open[port]
	return port, ok
}

// ServePorts keeps a listener on the port of every app whose route
// answers, with srv (TLS) on each, until ctx is done. A port that cannot
// be opened is tried again on the next change or after 30 seconds; it
// never stops titand. In development mode (Disabled) nothing is opened.
func (m *Manager) ServePorts(ctx context.Context, srv *http.Server) {
	if m.Disabled {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		m.syncPorts(srv)
		select {
		case <-ctx.Done():
			m.closePorts()
			return
		case <-ticker.C:
		case <-m.portKick:
		}
	}
}

// syncPorts opens the wanted ports and closes the others.
func (m *Manager) syncPorts(srv *http.Server) {
	want := m.wantedPorts()
	p := &m.ports
	p.mu.Lock()
	if p.open == nil {
		p.open, p.failed = map[int]net.Listener{}, map[int]bool{}
	}
	for port, ln := range p.open {
		if !slices.Contains(want, port) {
			_ = ln.Close()
			delete(p.open, port)
		}
	}
	listen, serve := p.listen, p.serve
	if listen == nil {
		listen = func(port int) (net.Listener, error) {
			// All addresses, IPv4 and IPv6, like the web UI on :443.
			return net.Listen("tcp", ":"+strconv.Itoa(port))
		}
	}
	if serve == nil {
		serve = func(srv *http.Server, ln net.Listener) error { return srv.ServeTLS(ln, "", "") }
	}
	for _, port := range want {
		if _, ok := p.open[port]; ok {
			continue
		}
		ln, err := listen(port)
		if err != nil {
			if !p.failed[port] {
				slog.Warn("HTTPS port of an app not opened", "port", port, "err", err)
				p.failed[port] = true
			}
			continue
		}
		delete(p.failed, port)
		p.open[port] = ln
		go func() {
			if err := serve(srv, ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
				slog.Warn("HTTPS port of an app stopped", "port", port, "err", err)
			}
		}()
	}
	open := slices.Sorted(maps.Keys(p.open))
	report := !slices.Equal(open, p.reported) && (len(open) > 0 || p.reported != nil)
	if report {
		p.reported = open
	}
	p.mu.Unlock()
	if report && m.OnPorts != nil {
		m.OnPorts(open)
	}
}

func (m *Manager) closePorts() {
	p := &m.ports
	p.mu.Lock()
	defer p.mu.Unlock()
	for port, ln := range p.open {
		_ = ln.Close()
		delete(p.open, port)
	}
}

// PortCookie is the name of the browser's pass for an app port.
func PortCookie(port int) string {
	return portCookiePrefix + strconv.Itoa(port)
}

// localPort is the port of the NAS a request came in on.
func localPort(r *http.Request) int {
	if addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		if tcp, ok := addr.(*net.TCPAddr); ok {
			return tcp.Port
		}
		if ap, err := netip.ParseAddrPort(addr.String()); err == nil {
			return int(ap.Port())
		}
	}
	return 0
}

// PortHandler answers the requests on the app ports: the port decides
// the app, whatever name or address the request asks for. Everything else
// is as on the app's names (Handler): firewall, networks of the route,
// the browser gate (with a pass of the port's own), frame headers.
func (m *Manager) PortHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		port := localPort(r)
		key, route, ok := m.routeByPort(port)
		if !ok {
			http.Error(w, "No app answers on this port.", http.StatusNotFound)
			return
		}
		if m.Gate != nil && !m.Gate(r.RemoteAddr, true) {
			http.Error(w, "The firewall of this NAS does not let your network reach this app.", http.StatusForbidden)
			return
		}
		if !allowed(route, r.RemoteAddr) {
			http.Error(w, "This app is not reachable from your network.", http.StatusForbidden)
			return
		}
		for _, name := range []string{"Tailscale-User-Login", "Tailscale-User-Name", "Tailscale-User-Profile-Pic", "Tailscale-App-Capabilities"} {
			r.Header.Del(name)
		}
		// The page that leads back to TitanOS names the host with its
		// port, so that /open sends the browser here again.
		if !m.gateWith(w, r, gateApp(key), PortCookie(port), r.Host) {
			return
		}
		m.handlerFor(key, route).proxy.ServeHTTP(w, r)
	})
}

// uiHost returns the host of a Host header (without port and brackets)
// if it is one the web UI is reached under (the NAS's name, its .local
// name, its addresses, the names of Let's Encrypt, DDNS, the tailnet), or
// "". Only such a host may carry a window token: anything else could hand
// the token to a foreign server.
func (m *Manager) uiHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]"), ".")
	if host == "" {
		return ""
	}
	for _, origin := range m.frameOrigins() {
		h := strings.TrimPrefix(origin, "https://")
		if hh, _, err := net.SplitHostPort(h); err == nil {
			h = hh
		}
		h = strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
		if h == host {
			return host
		}
	}
	return ""
}

// PortURL is the address of an app on its port, on the host the web UI
// was reached under (uiHost; "192.168.10.5" or "nas.local", its port
// left out), e.g. https://192.168.10.5:20001. It is "" if the app has no
// port titand listens on or the host is not one of the web UI's.
func (m *Manager) PortURL(app, uiHost string) string {
	port, ok := m.servedPort(app)
	if !ok {
		return ""
	}
	host := m.uiHost(uiHost)
	if host == "" {
		return ""
	}
	return "https://" + net.JoinHostPort(host, strconv.Itoa(port))
}

// portEnterBase is the base of an enter address on host when host names
// the app's port ("192.168.10.5:20001"), or "".
func (m *Manager) portEnterBase(app, host string) string {
	_, portText, err := net.SplitHostPort(host)
	if err != nil {
		return ""
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return ""
	}
	if served, ok := m.servedPort(app); !ok || served != port {
		return ""
	}
	h := m.uiHost(host)
	if h == "" {
		return ""
	}
	return "https://" + net.JoinHostPort(h, portText)
}

// WindowURLs are the addresses that let a browser in to an app with a
// window token: on the app's port at the host the web UI was reached
// under (uiHost, see PortURL), and on the app's name. Either may be "".
func (m *Manager) WindowURLs(app, uiHost, token string) (portURL, nameURL string) {
	if base := m.PortURL(app, uiHost); base != "" {
		portURL = base + EnterPath + "?t=" + url.QueryEscape(token)
	}
	nameURL, _ = m.EnterURL(app, "", token)
	return portURL, nameURL
}
