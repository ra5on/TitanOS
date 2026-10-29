package appproxy

// "Publish in the tailnet" with one click on a Docker app: the app gets
// a route with "via Tailscale" (or its route is switched on with it) and
// so a device of its own, https://<name>.<tailnet>.ts.net. Everything
// else is tailnet.go; this only fills in what the form would ask for.

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrTailnetUnavailable means the Tailscale plugin is not installed
	// or not active, so no device can start.
	ErrTailnetUnavailable = errors.New("appproxy: the Tailscale plugin is not active")
	// ErrTailnetNoKey means no auth key is stored for the devices.
	ErrTailnetNoKey = errors.New("appproxy: no Tailscale auth key stored")
	// ErrProxyOff means the switch for all HTTPS names is off.
	ErrProxyOff = errors.New("appproxy: HTTPS names are switched off")
	// ErrNoPort means the app has no port to publish.
	ErrNoPort = errors.New("appproxy: the app publishes no port")
	// ErrTailnetNotReady means the device is not running (yet).
	ErrTailnetNotReady = errors.New("appproxy: the device is not running")
)

var notLabel = regexp.MustCompile(`[^a-z0-9-]+`)

// DNSLabel makes a name into one DNS label: "My_App" becomes "my-app".
// Nothing usable left gives "app".
func DNSLabel(name string) string {
	label := strings.Trim(notLabel.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(label) > 63 {
		label = strings.TrimRight(label[:63], "-")
	}
	if label == "" {
		return "app"
	}
	return label
}

// freeLabel is base, or base-2, base-3 … if taken.
func freeLabel(base string, taken func(string) bool) string {
	if !taken(base) {
		return base
	}
	for i := 2; i < 100; i++ {
		suffix := "-" + strconv.Itoa(i)
		name := base
		if len(name)+len(suffix) > 63 {
			name = strings.TrimRight(name[:63-len(suffix)], "-")
		}
		if name += suffix; !taken(name) {
			return name
		}
	}
	return base
}

// PublishTailnet switches "via Tailscale" on for an app. An app without
// a route gets one on port (its WebUI port) with a name made from the
// app's name, reachable from the home network only; an existing route
// keeps its names and port and is switched on. key, if not empty, is
// stored as the auth key first; without one stored, ErrTailnetNoKey.
func (m *Manager) PublishTailnet(app string, port int, key string) (Route, error) {
	if !appRE.MatchString(app) {
		return Route{}, fmt.Errorf("%w: app name", ErrInvalid)
	}
	if _, ok := m.Tailnet.available(); !ok {
		return Route{}, ErrTailnetUnavailable
	}
	m.mu.RLock()
	off := m.settings.Off
	route, exists := m.settings.Apps[app]
	var names, devices []string
	for _, kind := range []struct {
		target bool
		routes map[string]Route
	}{{false, m.settings.Apps}, {true, m.settings.Targets}} {
		for key, r := range kind.routes {
			if !kind.target && key == app {
				continue
			}
			names = append(names, r.Name)
			names = append(names, r.Aliases...)
			if r.Tailscale {
				devices = append(devices, tailnetName(r))
			}
		}
	}
	m.mu.RUnlock()
	if off {
		return Route{}, ErrProxyOff
	}
	if key = strings.TrimSpace(key); key != "" {
		if err := m.SetTailnetKey(key); err != nil {
			return Route{}, err
		}
	} else if m.currentKey() == "" {
		return Route{}, ErrTailnetNoKey
	}
	label := DNSLabel(app)
	if !exists {
		if port < 1 || port > 65535 {
			return Route{}, ErrNoPort
		}
		route = Route{Name: freeLabel(label, func(n string) bool { return slices.Contains(names, n) }), Port: port, LocalOnly: true, TailnetOnly: true}
	} else if !route.Enabled {
		// It was off: taking the app out of the tailnet again turns it off.
		route.TailnetOnly = true
	}
	if !route.Tailscale {
		// The device is named after the app, unless that one is taken.
		route.TailscaleName = freeLabel(label, func(n string) bool { return slices.Contains(devices, n) })
	}
	route.Enabled, route.Tailscale = true, true
	if err := m.setRoute(false, app, route); err != nil {
		return Route{}, err
	}
	stored, _ := m.Route(app)
	return stored, nil
}

// UnpublishTailnet takes an app out of the tailnet: its device logs out
// and stops, and a route that only "Publish in the tailnet" switched on
// is switched off. The device stays listed in the tailnet's admin console
// until removed there. An app without Tailscale is no error.
func (m *Manager) UnpublishTailnet(app string) error {
	m.mu.RLock()
	route, ok := m.settings.Apps[app]
	m.mu.RUnlock()
	if !ok || !route.Tailscale {
		return nil
	}
	return m.change(func(s *Settings) error {
		route, ok := s.Apps[app]
		if !ok {
			return nil
		}
		route.Tailscale, route.TailscaleName = false, ""
		if route.TailnetOnly {
			route.Enabled, route.TailnetOnly = false, false
		}
		s.Apps[app] = route
		return nil
	})
}

// TailnetURL is the ts.net address of an app's device while it runs
// with a certificate, else "".
func (m *Manager) TailnetURL(app string) string {
	route, ok := m.Route(app)
	if !ok || !route.Tailscale {
		return ""
	}
	if n := m.tailnetNode("app:" + app); n != nil && n.State == "running" {
		return n.URL
	}
	return ""
}

// TailnetCheck is what asking an app's ts.net address gave.
type TailnetCheck struct {
	URL       string `json:"url"`
	Reachable bool   `json:"reachable"`
	// Status is the HTTP status the app answered with (502: it does not
	// answer, Serve and the certificate work).
	Status int    `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

// CheckTailnet asks the app's device over the tailnet, with its ts.net
// name and the certificate checked, as a browser would. It goes from the
// NAS's own Tailscale (the plugin) to the device's address, so the
// tailnet's access rules must let the NAS reach it.
func (m *Manager) CheckTailnet(ctx context.Context, app string) (TailnetCheck, error) {
	n := m.tailnetNode("app:" + app)
	if _, ok := m.Route(app); !ok || n == nil || n.State != "running" || n.dns == "" {
		return TailnetCheck{}, ErrTailnetNotReady
	}
	check := TailnetCheck{URL: n.URL}
	ips := slices.Clone(n.ips)
	// IPv4 first: it is there in every tailnet.
	slices.SortStableFunc(ips, func(a, b string) int {
		return strings.Count(a, ":") - strings.Count(b, ":")
	})
	if len(ips) == 0 {
		check.Error = "the device has no address in the tailnet"
		return check, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	status, err := m.Tailnet.probe(ctx, ips[0], n.dns)
	if err != nil {
		check.Error = err.Error()
		return check, nil
	}
	check.Reachable, check.Status = true, status
	return check, nil
}

// probeTailnet sends HEAD / for name to ip:443 and checks the certificate
// against the system's roots (Tailscale's is from Let's Encrypt). The
// first request may wait while Serve fetches the certificate.
func probeTailnet(ctx context.Context, ip, name string) (int, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, "443"))
			},
			TLSClientConfig:     &tls.Config{ServerName: name, MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout: 30 * time.Second,
			DisableKeepAlives:   true,
			Proxy:               nil,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, "https://"+name+"/", nil)
	if err != nil {
		return 0, err
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	res.Body.Close()
	return res.StatusCode, nil
}
