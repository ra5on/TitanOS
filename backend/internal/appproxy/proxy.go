package appproxy

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/netguard"
)

// appHandler passes the requests of one route on to its app.
type appHandler struct {
	route Route
	proxy *httputil.ReverseProxy
}

// errTarget refuses a connection a route must not make.
var errTarget = errors.New("appproxy: target not allowed")

// upstream is the connection to the apps. Apps that answer HTTPS
// themselves do so with a certificate of their own making, on loopback.
var upstream = &http.Transport{
	Proxy:                 nil,
	DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
	MaxIdleConnsPerHost:   16,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: time.Second,
}

// Handler answers requests for the host names of apps and gives every
// other request to next, the web UI.
func (m *Manager) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, route, ok := m.match(r.Host)
		tailnet := false
		if !ok {
			// A name in the tailnet comes only from its own tailscaled on
			// this NAS; anyone else asking for it would bypass the tailnet.
			if k, found := m.tailnetMatch(hostOnly(r.Host)); found {
				if !fromLoopback(r.RemoteAddr) {
					http.Error(w, "This name is only reachable through Tailscale.", http.StatusMisdirectedRequest)
					return
				}
				key, tailnet = k, true
				route, ok = m.routeByKey(k)
			}
		}
		if !ok {
			if m.Gate != nil && !m.Gate(r.RemoteAddr, false) {
				http.Error(w, "The firewall of this NAS does not let your network reach its web interface.", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if !tailnet && m.Gate != nil && !m.Gate(r.RemoteAddr, true) {
			http.Error(w, "The firewall of this NAS does not let your network reach this app.", http.StatusForbidden)
			return
		}
		// Through the tailnet, its own access rules decide; Serve has set
		// who asks in Tailscale-User-*. Anyone else may not claim that.
		if !tailnet {
			if !allowed(route, r.RemoteAddr) {
				http.Error(w, "This app is not reachable from your network.", http.StatusForbidden)
				return
			}
			for _, name := range []string{"Tailscale-User-Login", "Tailscale-User-Name", "Tailscale-User-Profile-Pic", "Tailscale-App-Capabilities"} {
				r.Header.Del(name)
			}
		}
		m.handlerFor(key, route).proxy.ServeHTTP(w, r)
	})
}

// hostOnly is a Host header without port and final dot, in lower case.
func hostOnly(host string) string {
	host = strings.ToLower(host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.TrimSuffix(host, ".")
}

// fromLoopback tells whether a request comes from this NAS itself.
func fromLoopback(remoteAddr string) bool {
	addr, ok := remoteIP(remoteAddr)
	return ok && addr.IsLoopback()
}

// allowed tells whether a client may reach a route: own networks if the
// route has some, else the home network if it is local only.
func allowed(route Route, remoteAddr string) bool {
	if len(route.Networks) == 0 {
		return !route.LocalOnly || fromLocal(remoteAddr)
	}
	addr, ok := remoteIP(remoteAddr)
	if !ok {
		return false
	}
	for _, network := range route.Networks {
		if prefix, err := netip.ParsePrefix(network); err == nil && prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func (m *Manager) handlerFor(key string, route Route) *appHandler {
	m.mu.RLock()
	h := m.proxies[key]
	m.mu.RUnlock()
	if h != nil && sameRoute(h.route, route) {
		return h
	}
	h = &appHandler{route: route, proxy: m.newProxy(key, route)}
	m.mu.Lock()
	m.proxies[key] = h
	m.mu.Unlock()
	return h
}

func sameRoute(a, b Route) bool {
	return a.Host == b.Host && a.Port == b.Port && a.Scheme == b.Scheme && a.VerifyTLS == b.VerifyTLS
}

// transportFor is the connection for a route: apps without a certificate
// check share upstream; targets and checked certificates get their own,
// and a target may only be dialled in the home network, Tailscale or
// NetBird, never back to titand itself.
func (m *Manager) transportFor(route Route) http.RoundTripper {
	if route.Host == "" && !route.VerifyTLS {
		return upstream
	}
	transport := upstream.Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: !route.VerifyTLS}
	if route.Host != "" {
		reserved := m.Reserved
		dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
			return checkTarget(address, reserved)
		}}
		transport.DialContext = dialer.DialContext
	}
	return transport
}

// checkTarget refuses addresses outside the home network and titand's own
// ports on the NAS.
func checkTarget(address string, reserved []int) error {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	addr = addr.Unmap().WithZone("")
	if !netguard.Local(addr) {
		return fmt.Errorf("%w: %s is not in the home network, Tailscale or NetBird", errTarget, addr)
	}
	port, _ := strconv.Atoi(portText)
	if slices.Contains(reserved, port) && (addr.IsLoopback() || ownAddress(addr)) {
		return fmt.Errorf("%w: port %d of the NAS belongs to TitanOS", errTarget, port)
	}
	return nil
}

// ownAddress tells whether an address belongs to one of the NAS's
// interfaces.
func ownAddress(addr netip.Addr) bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			if own, ok := netip.AddrFromSlice(ipnet.IP); ok && own.Unmap() == addr {
				return true
			}
		}
	}
	return false
}

func (m *Manager) newProxy(app string, route Route) *httputil.ReverseProxy {
	scheme := route.Scheme
	if scheme == "" {
		scheme = "http"
	}
	host := route.Host
	if host == "" {
		host = "127.0.0.1"
	}
	target := &url.URL{Scheme: scheme, Host: net.JoinHostPort(host, strconv.Itoa(route.Port))}
	return &httputil.ReverseProxy{
		// Rewrite (not Director) drops X-Forwarded-* the client sent itself
		// and the hop-by-hop headers; upgrades to WebSocket pass through.
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			// Apps build their links from the name they were asked by.
			pr.Out.Host = pr.In.Host
			pr.SetXForwarded()
			stripSessionCookies(pr.Out.Header)
		},
		ModifyResponse: func(res *http.Response) error {
			dropSessionSetCookies(res.Header)
			return nil
		},
		Transport: m.transportFor(route),
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Warn("app proxy: app does not answer", "app", app, "port", route.Port, "err", err)
			http.Error(w, "The app does not answer. Is it running?", http.StatusBadGateway)
		},
	}
}

// isSessionCookie tells the cookies of the TitanOS login apart; whatever
// the name of the web UI's cookie, it starts like this.
func isSessionCookie(name string) bool {
	return strings.HasPrefix(name, "titanos_") || strings.HasPrefix(name, "__Host-titanos") || strings.HasPrefix(name, "__Secure-titanos")
}

// stripSessionCookies keeps the TitanOS login away from apps. Browsers do
// not send the host-only cookie to another name anyway; this also holds
// when an app is opened under the NAS's own address by mistake.
func stripSessionCookies(h http.Header) {
	lines := h.Values("Cookie")
	if len(lines) == 0 {
		return
	}
	h.Del("Cookie")
	var kept []string
	for _, line := range lines {
		for part := range strings.SplitSeq(line, ";") {
			part = strings.TrimSpace(part)
			name, _, _ := strings.Cut(part, "=")
			if part == "" || isSessionCookie(strings.TrimSpace(name)) {
				continue
			}
			kept = append(kept, part)
		}
	}
	if len(kept) > 0 {
		h.Set("Cookie", strings.Join(kept, "; "))
	}
}

// dropSessionSetCookies keeps an app from setting a cookie under the name
// of the TitanOS login, which a Domain attribute could hand to the web UI.
func dropSessionSetCookies(h http.Header) {
	lines := h.Values("Set-Cookie")
	if len(lines) == 0 {
		return
	}
	h.Del("Set-Cookie")
	for _, line := range lines {
		name, _, _ := strings.Cut(line, "=")
		if !isSessionCookie(strings.TrimSpace(name)) {
			h.Add("Set-Cookie", line)
		}
	}
}

// fromLocal tells whether a client is in the home network or comes over
// Tailscale or NetBird.
func fromLocal(remoteAddr string) bool {
	addr, ok := remoteIP(remoteAddr)
	return ok && netguard.Local(addr)
}

// remoteIP is the address of the connection's other end, as is; a
// forwarding header never counts.
func remoteIP(remoteAddr string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap().WithZone(""), true
}
