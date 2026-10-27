// Package appproxy gives Docker apps a HTTPS name of their own, such as
// https://immich.nas.local: titand answers on :443 for these names and
// passes the requests on to the port the app publishes on the NAS. The
// names are published over mDNS (avahi); a domain of one's own needs a
// wildcard entry in the DNS of the router.
//
// The routes live in /config/docker/proxy.json, a file of its own that
// older versions of titand do not know and leave alone.
package appproxy

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const configFile = "docker/proxy.json"

// ErrInvalid means a route or the domain is not acceptable; the message says why.
var ErrInvalid = errors.New("appproxy: invalid setting")

// ErrNameTaken means another app already uses the name.
var ErrNameTaken = errors.New("appproxy: name in use")

// Route is the HTTPS name of one app (a Compose project).
type Route struct {
	Enabled bool `json:"enabled"`
	// Name is the first label of the host name: "immich" becomes
	// immich.<hostname>.local and immich.<domain>.
	Name string `json:"name"`
	// Port is the port the app publishes on the NAS. titand connects to it
	// on 127.0.0.1.
	Port int `json:"port"`
	// Scheme is how the app itself answers: "http" (default) or "https".
	Scheme string `json:"scheme,omitempty"`
	// LocalOnly lets only the home network, Tailscale and NetBird in.
	LocalOnly bool `json:"localOnly,omitempty"`
}

// Settings are the content of /config/docker/proxy.json.
type Settings struct {
	// Domain is an optional domain of one's own, e.g. "home.example.org";
	// the router's DNS must send *.<domain> to the NAS.
	Domain string `json:"domain,omitempty"`
	// Apps maps the name of a Compose project to its route.
	Apps map[string]Route `json:"apps"`
}

var (
	labelRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	// The same names as Docker apps (docker.appNameRE).
	appRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
)

// Manager holds the routes in memory and answers for their host names.
type Manager struct {
	store *config.Store
	// Hostname returns the name of the NAS; os.Hostname if nil.
	Hostname func() string
	// HTTPSPort is the port of the web UI when it is not 443; it is part
	// of the links.
	HTTPSPort int
	// Reserved are the ports of titand itself: a route to them would send
	// requests back to the proxy.
	Reserved []int
	// Disabled leaves avahi alone (development mode). Routing still works.
	Disabled bool
	// OnChange is called after the routes changed, e.g. to start avahi.
	OnChange func()

	mu       sync.RWMutex
	settings Settings
	proxies  map[string]*appHandler
	// published is the content last written to the avahi hosts file.
	published string
	// hostsFile and reload are replaced in tests.
	hostsFile string
	reload    func() error
	address   func() string
	// kick asks Run to publish the names now.
	kick chan struct{}
}

// NewManager returns a manager with no routes; Load reads them.
func NewManager(store *config.Store) *Manager {
	return &Manager{store: store, settings: Settings{Apps: map[string]Route{}}, proxies: map[string]*appHandler{}, hostsFile: "/etc/avahi/hosts", kick: make(chan struct{}, 1)}
}

// Load reads the routes from /config. A missing file means none.
func (m *Manager) Load() error {
	var settings Settings
	err := m.store.ReadJSON(configFile, &settings)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if settings.Apps == nil {
		settings.Apps = map[string]Route{}
	}
	m.mu.Lock()
	m.settings = settings
	m.proxies = map[string]*appHandler{}
	m.mu.Unlock()
	return nil
}

// Settings returns a copy of the routes and the domain.
func (m *Manager) Settings() Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.copySettings()
}

func (m *Manager) copySettings() Settings {
	out := Settings{Domain: m.settings.Domain, Apps: make(map[string]Route, len(m.settings.Apps))}
	for app, route := range m.settings.Apps {
		out.Apps[app] = route
	}
	return out
}

// SetDomain stores the domain of one's own; empty removes it.
func (m *Manager) SetDomain(domain string) error {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if err := validDomain(domain); err != nil {
		return err
	}
	return m.change(func(s *Settings) error {
		s.Domain = domain
		return nil
	})
}

// SetRoute stores the route of an app.
func (m *Manager) SetRoute(app string, route Route) error {
	route.Name = strings.ToLower(strings.TrimSpace(route.Name))
	if route.Scheme == "" {
		route.Scheme = "http"
	}
	if err := m.validRoute(app, route); err != nil {
		return err
	}
	if route.Scheme == "http" {
		// The default is not written, like any other optional field.
		route.Scheme = ""
	}
	return m.change(func(s *Settings) error {
		for other, r := range s.Apps {
			if other != app && r.Name == route.Name {
				return fmt.Errorf("%w: %s", ErrNameTaken, other)
			}
		}
		s.Apps[app] = route
		return nil
	})
}

// RemoveRoute forgets the route of an app; a missing one is no error.
func (m *Manager) RemoveRoute(app string) error {
	m.mu.RLock()
	_, ok := m.settings.Apps[app]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	return m.change(func(s *Settings) error {
		delete(s.Apps, app)
		return nil
	})
}

// change applies fn to a copy, stores it and only then takes it over.
func (m *Manager) change(fn func(*Settings) error) error {
	m.mu.Lock()
	next := m.copySettings()
	if err := fn(&next); err != nil {
		m.mu.Unlock()
		return err
	}
	if err := m.store.WriteJSON(configFile, next); err != nil {
		m.mu.Unlock()
		return err
	}
	m.settings = next
	// Handlers are made again for the new targets on the next request.
	m.proxies = map[string]*appHandler{}
	m.mu.Unlock()
	select {
	case m.kick <- struct{}{}:
	default:
	}
	if m.OnChange != nil {
		m.OnChange()
	}
	return nil
}

func (m *Manager) validRoute(app string, route Route) error {
	switch {
	case !appRE.MatchString(app):
		return fmt.Errorf("%w: app name", ErrInvalid)
	case !labelRE.MatchString(route.Name):
		return fmt.Errorf("%w: the name may only hold a-z, 0-9 and hyphens, at most 63 characters", ErrInvalid)
	case route.Port < 1 || route.Port > 65535:
		return fmt.Errorf("%w: port", ErrInvalid)
	case slices.Contains(m.Reserved, route.Port):
		return fmt.Errorf("%w: port %d belongs to the web UI of TitanOS", ErrInvalid, route.Port)
	case route.Scheme != "http" && route.Scheme != "https":
		return fmt.Errorf("%w: scheme", ErrInvalid)
	}
	return nil
}

func validDomain(domain string) error {
	if domain == "" {
		return nil
	}
	labels := strings.Split(domain, ".")
	if len(domain) > 200 || len(labels) < 2 {
		return fmt.Errorf("%w: domain", ErrInvalid)
	}
	for _, label := range labels {
		if !labelRE.MatchString(label) {
			return fmt.Errorf("%w: domain", ErrInvalid)
		}
	}
	if labels[len(labels)-1] == "local" {
		// .local names come from mDNS and are there anyway.
		return fmt.Errorf("%w: a .local domain is published by TitanOS itself", ErrInvalid)
	}
	return nil
}

// host returns the first label of the NAS's name in lower case.
func (m *Manager) host() string {
	var name string
	if m.Hostname != nil {
		name = m.Hostname()
	} else {
		name, _ = os.Hostname()
	}
	name, _, _ = strings.Cut(strings.ToLower(name), ".")
	return name
}

// Suffixes are what follows the app's name in its host names:
// "<hostname>.local" and, if set, the domain of one's own.
func (m *Manager) Suffixes() []string {
	m.mu.RLock()
	domain := m.settings.Domain
	m.mu.RUnlock()
	var out []string
	if host := m.host(); host != "" {
		out = append(out, host+".local")
	}
	if domain != "" {
		out = append(out, domain)
	}
	return out
}

// match finds the enabled route whose host name a request asks for.
func (m *Manager) match(host string) (string, Route, bool) {
	host = strings.ToLower(host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(host, ".")
	label, rest, ok := strings.Cut(host, ".")
	if !ok || !slices.Contains(m.Suffixes(), rest) {
		return "", Route{}, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for app, route := range m.settings.Apps {
		if route.Enabled && route.Name == label {
			return app, route, true
		}
	}
	return "", Route{}, false
}

// CertificateNames returns the names a certificate for the TLS server
// name must cover, as wildcards ("*.nas.local"), or nil if the name is not
// the one of an app.
func (m *Manager) CertificateNames(serverName string) []string {
	if _, _, ok := m.match(serverName); !ok {
		return nil
	}
	var names []string
	for _, suffix := range m.Suffixes() {
		names = append(names, "*."+suffix)
	}
	return names
}

// Route returns the route of an app if it is enabled.
func (m *Manager) Route(app string) (Route, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	route, ok := m.settings.Apps[app]
	return route, ok && route.Enabled
}

// URL returns the address of an app's HTTPS name, or "" without an
// enabled route. With a domain of one's own that one is preferred.
func (m *Manager) URL(app string) string {
	m.mu.RLock()
	route, ok := m.settings.Apps[app]
	m.mu.RUnlock()
	suffixes := m.Suffixes()
	if !ok || !route.Enabled || len(suffixes) == 0 {
		return ""
	}
	return m.url(route.Name, suffixes[len(suffixes)-1])
}

func (m *Manager) url(name, suffix string) string {
	host := name + "." + suffix
	if m.HTTPSPort != 0 && m.HTTPSPort != 443 {
		host = net.JoinHostPort(host, strconv.Itoa(m.HTTPSPort))
	}
	return "https://" + host
}

// AppInfo is a route as the web UI shows it, with its addresses.
type AppInfo struct {
	Route
	App  string   `json:"app"`
	URLs []string `json:"urls"`
}

// Info lists every route with its addresses, sorted by app.
func (m *Manager) Info() (domain string, suffixes []string, apps []AppInfo) {
	settings := m.Settings()
	suffixes = m.Suffixes()
	apps = []AppInfo{}
	for app, route := range settings.Apps {
		if route.Scheme == "" {
			route.Scheme = "http"
		}
		info := AppInfo{Route: route, App: app, URLs: []string{}}
		for _, suffix := range suffixes {
			info.URLs = append(info.URLs, m.url(route.Name, suffix))
		}
		apps = append(apps, info)
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].App < apps[j].App })
	return settings.Domain, suffixes, apps
}

// Publishing tells whether names have to be published over mDNS.
func (m *Manager) Publishing() bool {
	if m.Disabled {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, route := range m.settings.Apps {
		if route.Enabled {
			return true
		}
	}
	return false
}
