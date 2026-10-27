package appproxy

import (
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/netguard"
)

// appHandler passes the requests of one route on to its app.
type appHandler struct {
	route Route
	proxy *httputil.ReverseProxy
}

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
		app, route, ok := m.match(r.Host)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		if route.LocalOnly && !fromLocal(r.RemoteAddr) {
			http.Error(w, "This app is only reachable from the home network, Tailscale or NetBird.", http.StatusForbidden)
			return
		}
		m.handlerFor(app, route).proxy.ServeHTTP(w, r)
	})
}

func (m *Manager) handlerFor(app string, route Route) *appHandler {
	m.mu.RLock()
	h := m.proxies[app]
	m.mu.RUnlock()
	if h != nil && h.route == route {
		return h
	}
	h = &appHandler{route: route, proxy: newProxy(app, route)}
	m.mu.Lock()
	m.proxies[app] = h
	m.mu.Unlock()
	return h
}

func newProxy(app string, route Route) *httputil.ReverseProxy {
	scheme := route.Scheme
	if scheme == "" {
		scheme = "http"
	}
	target := &url.URL{Scheme: scheme, Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(route.Port))}
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
		Transport: upstream,
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
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return netguard.Local(addr.WithZone(""))
}
