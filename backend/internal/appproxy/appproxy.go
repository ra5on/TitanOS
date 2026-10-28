// Package appproxy gives Docker apps a HTTPS name of their own, such as
// https://immich.nas.local: titand answers on :443 for these names and
// passes the requests on to the port the app publishes on the NAS. Targets
// outside Docker (VMs, devices in the home network) get names the same
// way. The names are published over mDNS (avahi); a domain of one's own
// needs a wildcard entry in the DNS of the router.
//
// The routes live in /config/docker/proxy.json, a file of its own that
// older versions of titand do not know and leave alone.
package appproxy

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/netguard"
)

const configFile = "docker/proxy.json"

// ErrInvalid means a route or the domain is not acceptable; the message says why.
var ErrInvalid = errors.New("appproxy: invalid setting")

// ErrNameTaken means another app already uses the name.
var ErrNameTaken = errors.New("appproxy: name in use")

// Route is the HTTPS name of one app (a Compose project) or of another
// target in the home network.
type Route struct {
	Enabled bool `json:"enabled"`
	// Name is the first label of the host name: "immich" becomes
	// immich.<hostname>.local and immich.<domain>.
	Name string `json:"name"`
	// Aliases are further names for the same target (optional).
	Aliases []string `json:"aliases,omitempty"`
	// Short also answers to <name>.local, without the NAS's name (optional).
	Short bool `json:"short,omitempty"`
	// Host is the address of a target outside Docker (IP or host name in
	// the home network, Tailscale or NetBird). Empty for apps: 127.0.0.1.
	Host string `json:"host,omitempty"`
	// Port is the port the app publishes on the NAS (or the target's port).
	Port int `json:"port"`
	// Scheme is how the app itself answers: "http" (default) or "https".
	Scheme string `json:"scheme,omitempty"`
	// VerifyTLS checks the certificate of an app that answers HTTPS
	// itself; without it any certificate is taken (optional).
	VerifyTLS bool `json:"verifyTLS,omitempty"`
	// LocalOnly lets only the home network, Tailscale and NetBird in.
	LocalOnly bool `json:"localOnly,omitempty"`
	// Networks, if set, are the only networks let in (CIDR); they take the
	// place of LocalOnly. Older versions only know LocalOnly, so it is
	// stored as well (see toDisk).
	Networks []string `json:"networks,omitempty"`
	// Paused is only on the stick: the route is on, but every name is
	// switched off (Settings.Off). Older versions see it as off.
	Paused bool `json:"paused,omitempty"`
	// Tailscale makes the route a device of its own in the tailnet
	// (see tailnet.go); TailscaleName is its name there, Name if empty.
	Tailscale     bool   `json:"tailscale,omitempty"`
	TailscaleName string `json:"tailscaleName,omitempty"`
}

// Settings are the content of /config/docker/proxy.json. Every field after
// Apps is optional; older versions of titand ignore them (and lose them
// when they save the file).
type Settings struct {
	// Domain is an optional domain of one's own, e.g. "home.example.org";
	// the router's DNS must send *.<domain> to the NAS.
	Domain string `json:"domain,omitempty"`
	// Apps maps the name of a Compose project to its route.
	Apps map[string]Route `json:"apps"`
	// LocalName takes the place of the NAS's name in .local names
	// ("apps" gives immich.apps.local). Empty: the NAS's name.
	LocalName string `json:"localName,omitempty"`
	// NoMDNS publishes no .local names; only the domain is left.
	NoMDNS bool `json:"noMDNS,omitempty"`
	// Off switches every name off at once, keeping each route's setting.
	Off bool `json:"off,omitempty"`
	// Targets are names for things outside Docker (VMs, containers,
	// devices); a map of its own so that older versions, which would send
	// them to 127.0.0.1, never see them as apps.
	Targets map[string]Route `json:"targets,omitempty"`
}

// General are the settings for all names; nil leaves a field as it is.
type General struct {
	Domain    *string `json:"domain"`
	LocalName *string `json:"localName"`
	MDNS      *bool   `json:"mdns"`
	Off       *bool   `json:"off"`
	// TailscaleKey is the auth key for names in the tailnet; "" removes
	// it. It is never sent back.
	TailscaleKey *string `json:"tailscaleKey"`
}

const (
	maxAliases  = 8
	maxNetworks = 32
)

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
	// Gate is the firewall's say on a client: app true for the HTTPS name
	// of an app, false for the web UI. Both share one port, so only titand
	// can tell them apart. nil lets everyone through.
	Gate func(remoteAddr string, app bool) bool
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

	// Tailnet runs the devices of routes with "via Tailscale".
	Tailnet     *Tailnet
	tailnetKey  string
	tailnetKick chan struct{}
}

// NewManager returns a manager with no routes; Load reads them.
func NewManager(store *config.Store) *Manager {
	return &Manager{store: store, settings: Settings{Apps: map[string]Route{}}, proxies: map[string]*appHandler{}, hostsFile: "/etc/avahi/hosts", kick: make(chan struct{}, 1), Tailnet: newTailnet(), tailnetKick: make(chan struct{}, 1)}
}

// Load reads the routes from /config. A missing file means none.
func (m *Manager) Load() error {
	var settings Settings
	err := m.store.ReadJSON(configFile, &settings)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.mu.Lock()
	m.settings = fromDisk(settings)
	m.proxies = map[string]*appHandler{}
	m.mu.Unlock()
	m.loadTailnetKey()
	return nil
}

// Settings returns a copy of the routes and the domain.
func (m *Manager) Settings() Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.copySettings()
}

func (m *Manager) copySettings() Settings {
	out := m.settings
	out.Apps = copyRoutes(m.settings.Apps)
	out.Targets = copyRoutes(m.settings.Targets)
	return out
}

func copyRoutes(in map[string]Route) map[string]Route {
	out := make(map[string]Route, len(in))
	for key, route := range in {
		route.Aliases = slices.Clone(route.Aliases)
		route.Networks = slices.Clone(route.Networks)
		out[key] = route
	}
	return out
}

// toDisk is what goes on the stick: with every name switched off, each
// enabled route is stored as off and paused, so that an older version
// (which knows neither) keeps them off; own networks also set LocalOnly,
// the nearest rule an older version knows.
func toDisk(s Settings) Settings {
	conv := func(in map[string]Route) map[string]Route {
		out := make(map[string]Route, len(in))
		for key, route := range in {
			route.Paused = false
			if s.Off && route.Enabled {
				route.Enabled, route.Paused = false, true
			}
			if len(route.Networks) > 0 {
				route.LocalOnly = true
			}
			out[key] = route
		}
		return out
	}
	s.Apps = conv(s.Apps)
	s.Targets = conv(s.Targets)
	if len(s.Targets) == 0 {
		s.Targets = nil
	}
	return s
}

// fromDisk turns paused routes back into enabled ones.
func fromDisk(s Settings) Settings {
	conv := func(in map[string]Route) map[string]Route {
		out := make(map[string]Route, len(in))
		for key, route := range in {
			if route.Paused && s.Off {
				route.Enabled = true
			}
			route.Paused = false
			out[key] = route
		}
		return out
	}
	s.Apps = conv(s.Apps)
	s.Targets = conv(s.Targets)
	return s
}

// SetDomain stores the domain of one's own; empty removes it.
func (m *Manager) SetDomain(domain string) error {
	return m.SetGeneral(General{Domain: &domain})
}

// SetGeneral changes the settings for all names; nil fields stay.
func (m *Manager) SetGeneral(g General) error {
	var domain, local string
	if g.Domain != nil {
		domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(*g.Domain)), ".")
		if err := validDomain(domain); err != nil {
			return err
		}
	}
	if g.LocalName != nil {
		local = normalLocalName(*g.LocalName)
		if err := validLocalName(local); err != nil {
			return err
		}
	}
	return m.change(func(s *Settings) error {
		if g.Domain != nil {
			s.Domain = domain
		}
		if g.LocalName != nil {
			s.LocalName = local
		}
		if g.MDNS != nil {
			s.NoMDNS = !*g.MDNS
		}
		if g.Off != nil {
			s.Off = *g.Off
		}
		// A short name must not be the head of the local suffix.
		head := m.localLabelOf(*s)
		for _, routes := range []map[string]Route{s.Apps, s.Targets} {
			for _, route := range routes {
				if route.Short && slices.Contains(names(route), head) {
					return fmt.Errorf("%w: the short name %s.local would be the name of the NAS", ErrInvalid, head)
				}
			}
		}
		return nil
	})
}

// SetRoute stores the route of an app.
func (m *Manager) SetRoute(app string, route Route) error {
	if route.Host != "" {
		return fmt.Errorf("%w: an app is always reached on the NAS itself", ErrInvalid)
	}
	return m.setRoute(false, app, route)
}

// SetTarget stores the route of a target outside Docker.
func (m *Manager) SetTarget(id string, route Route) error {
	route.Host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(route.Host)), ".")
	if route.Host == "" {
		return fmt.Errorf("%w: address of the target", ErrInvalid)
	}
	return m.setRoute(true, id, route)
}

func (m *Manager) setRoute(target bool, key string, route Route) error {
	route.Name = strings.ToLower(strings.TrimSpace(route.Name))
	route.TailscaleName = strings.ToLower(strings.TrimSpace(route.TailscaleName))
	if !route.Tailscale || route.TailscaleName == route.Name {
		route.TailscaleName = ""
	}
	route.Paused = false
	if route.Scheme == "" {
		route.Scheme = "http"
	}
	var aliases []string
	for _, alias := range route.Aliases {
		if alias = strings.ToLower(strings.TrimSpace(alias)); alias != "" && !slices.Contains(aliases, alias) && alias != route.Name {
			aliases = append(aliases, alias)
		}
	}
	route.Aliases = aliases
	networks, err := normalNetworks(route.Networks)
	if err != nil {
		return err
	}
	route.Networks = networks
	if err := m.validRoute(key, route); err != nil {
		return err
	}
	if route.Scheme == "http" {
		// The default is not written, like any other optional field.
		route.Scheme = ""
		route.VerifyTLS = false
	}
	return m.change(func(s *Settings) error {
		mine := names(route)
		for _, kind := range []struct {
			target bool
			routes map[string]Route
		}{{false, s.Apps}, {true, s.Targets}} {
			for other, r := range kind.routes {
				if kind.target == target && other == key {
					continue
				}
				for _, name := range names(r) {
					if slices.Contains(mine, name) {
						return fmt.Errorf("%w: %s", ErrNameTaken, other)
					}
				}
			}
		}
		if route.Tailscale {
			name := tailnetName(route)
			for _, kind := range []struct {
				target bool
				routes map[string]Route
			}{{false, s.Apps}, {true, s.Targets}} {
				for other, r := range kind.routes {
					if !(kind.target == target && other == key) && r.Tailscale && tailnetName(r) == name {
						return fmt.Errorf("%w: %s", ErrNameTaken, other)
					}
				}
			}
		}
		if route.Short && slices.Contains(mine, m.localLabelOf(*s)) {
			return fmt.Errorf("%w: the short name %s.local would be the name of the NAS", ErrInvalid, m.localLabelOf(*s))
		}
		if target {
			if s.Targets == nil {
				s.Targets = map[string]Route{}
			}
			s.Targets[key] = route
		} else {
			s.Apps[key] = route
		}
		return nil
	})
}

// RemoveRoute forgets the route of an app; a missing one is no error.
func (m *Manager) RemoveRoute(app string) error {
	return m.remove(false, app)
}

// RemoveTarget forgets a target outside Docker; a missing one is no error.
func (m *Manager) RemoveTarget(id string) error {
	return m.remove(true, id)
}

func (m *Manager) remove(target bool, key string) error {
	m.mu.RLock()
	routes := m.settings.Apps
	if target {
		routes = m.settings.Targets
	}
	_, ok := routes[key]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	return m.change(func(s *Settings) error {
		if target {
			delete(s.Targets, key)
		} else {
			delete(s.Apps, key)
		}
		return nil
	})
}

// tailnetName is a route's device name in the tailnet.
func tailnetName(route Route) string {
	if route.TailscaleName != "" {
		return route.TailscaleName
	}
	return route.Name
}

// routeByKey returns an enabled route by its key ("app:…", "target:…").
func (m *Manager) routeByKey(key string) (Route, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.settings.Off {
		return Route{}, false
	}
	kind, name, _ := strings.Cut(key, ":")
	routes := m.settings.Apps
	if kind == "target" {
		routes = m.settings.Targets
	}
	route, ok := routes[name]
	return route, ok && route.Enabled
}

// names are the first labels a route answers to.
func names(route Route) []string {
	return append([]string{route.Name}, route.Aliases...)
}

// change applies fn to a copy, stores it and only then takes it over.
func (m *Manager) change(fn func(*Settings) error) error {
	m.mu.Lock()
	next := m.copySettings()
	if err := fn(&next); err != nil {
		m.mu.Unlock()
		return err
	}
	if err := m.store.WriteJSON(configFile, toDisk(next)); err != nil {
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
	m.kickTailnet()
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
	case len(route.Aliases) > maxAliases:
		return fmt.Errorf("%w: at most %d further names", ErrInvalid, maxAliases)
	case route.Port < 1 || route.Port > 65535:
		return fmt.Errorf("%w: port", ErrInvalid)
	case slices.Contains(m.Reserved, route.Port) && loopbackHost(route.Host):
		return fmt.Errorf("%w: port %d belongs to the web UI of TitanOS", ErrInvalid, route.Port)
	case route.Scheme != "http" && route.Scheme != "https":
		return fmt.Errorf("%w: scheme", ErrInvalid)
	}
	if route.TailscaleName != "" && !labelRE.MatchString(route.TailscaleName) {
		return fmt.Errorf("%w: the name in the tailnet may only hold a-z, 0-9 and hyphens, at most 63 characters", ErrInvalid)
	}
	for _, alias := range route.Aliases {
		if !labelRE.MatchString(alias) {
			return fmt.Errorf("%w: further name %q: only a-z, 0-9 and hyphens, at most 63 characters", ErrInvalid, alias)
		}
	}
	if route.Host != "" {
		if addr, err := netip.ParseAddr(route.Host); err == nil {
			if !netguard.Local(addr) {
				return fmt.Errorf("%w: the target must be in the home network, Tailscale or NetBird", ErrInvalid)
			}
		} else if !validHostName(route.Host) {
			return fmt.Errorf("%w: address of the target", ErrInvalid)
		}
	}
	return nil
}

// loopbackHost tells whether a route's host means the NAS itself.
func loopbackHost(host string) bool {
	if host == "" || host == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Unmap().IsLoopback()
}

func validHostName(host string) bool {
	if len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !labelRE.MatchString(label) {
			return false
		}
	}
	return true
}

// normalNetworks checks a list of networks (CIDR or single addresses) and
// writes each in its shortest form.
func normalNetworks(in []string) ([]string, error) {
	var out []string
	for _, network := range in {
		network = strings.TrimSpace(network)
		if network == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(network)
		if err != nil {
			addr, err := netip.ParseAddr(network)
			if err != nil {
				return nil, fmt.Errorf("%w: network %q", ErrInvalid, network)
			}
			prefix = netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen())
		}
		if text := prefix.Masked().String(); !slices.Contains(out, text) {
			out = append(out, text)
		}
	}
	if len(out) > maxNetworks {
		return nil, fmt.Errorf("%w: at most %d networks", ErrInvalid, maxNetworks)
	}
	return out, nil
}

// normalLocalName drops a trailing ".local": "apps.local" and "apps" mean
// the same.
func normalLocalName(name string) string {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	return strings.TrimSuffix(strings.TrimSuffix(name, ".local"), ".")
}

func validLocalName(name string) error {
	if name == "" {
		return nil
	}
	labels := strings.Split(name, ".")
	if len(labels) > 3 {
		return fmt.Errorf("%w: local name: at most three parts", ErrInvalid)
	}
	for _, label := range labels {
		if !labelRE.MatchString(label) || label == "local" {
			return fmt.Errorf("%w: local name: only a-z, 0-9, hyphens and dots", ErrInvalid)
		}
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

// localLabelOf is what stands before ".local" in the names: the local
// name if one is set, else the NAS's name.
func (m *Manager) localLabelOf(s Settings) string {
	if s.LocalName != "" {
		return s.LocalName
	}
	return m.host()
}

// localSuffix is "<local name>.local", or "" without mDNS.
func (m *Manager) localSuffix(s Settings) string {
	if s.NoMDNS {
		return ""
	}
	if label := m.localLabelOf(s); label != "" {
		return label + ".local"
	}
	return ""
}

// Suffixes are what follows the app's name in its host names:
// "<local name>.local" (unless mDNS is off) and, if set, the domain of
// one's own.
func (m *Manager) Suffixes() []string {
	m.mu.RLock()
	settings := m.settings
	m.mu.RUnlock()
	return m.suffixes(settings)
}

func (m *Manager) suffixes(s Settings) []string {
	var out []string
	if local := m.localSuffix(s); local != "" {
		out = append(out, local)
	}
	if s.Domain != "" {
		out = append(out, s.Domain)
	}
	return out
}

// entry is a route with the key it is stored under.
type entry struct {
	key   string // "app:<name>" or "target:<id>"
	route Route
}

// enabled lists the routes that answer now.
func (m *Manager) enabled() []entry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.settings.Off {
		return nil
	}
	var out []entry
	for app, route := range m.settings.Apps {
		if route.Enabled {
			out = append(out, entry{"app:" + app, route})
		}
	}
	for id, route := range m.settings.Targets {
		if route.Enabled {
			out = append(out, entry{"target:" + id, route})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
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
	if !ok {
		return "", Route{}, false
	}
	m.mu.RLock()
	settings := m.settings
	m.mu.RUnlock()
	suffix := slices.Contains(m.suffixes(settings), rest)
	short := rest == "local" && !settings.NoMDNS
	if !suffix && !short {
		return "", Route{}, false
	}
	for _, e := range m.enabled() {
		if (suffix || e.route.Short) && slices.Contains(names(e.route), label) {
			return e.key, e.route, true
		}
	}
	return "", Route{}, false
}

// shortNames are the <name>.local names of enabled routes, sorted.
func (m *Manager) shortNames() []string {
	m.mu.RLock()
	noMDNS := m.settings.NoMDNS
	m.mu.RUnlock()
	if noMDNS {
		return nil
	}
	var out []string
	for _, e := range m.enabled() {
		if e.route.Short {
			for _, name := range names(e.route) {
				out = append(out, name+".local")
			}
		}
	}
	sort.Strings(out)
	return out
}

// CertificateNames returns the names a certificate for the TLS server
// name must cover, as wildcards ("*.nas.local") plus every short name
// ("immich.local": a wildcard right under .local would not do), or nil if
// the name is not the one of an app.
func (m *Manager) CertificateNames(serverName string) []string {
	if _, _, ok := m.match(serverName); !ok {
		return nil
	}
	var names []string
	for _, suffix := range m.Suffixes() {
		names = append(names, "*."+suffix)
	}
	return append(names, m.shortNames()...)
}

// Route returns the route of an app if it is enabled.
func (m *Manager) Route(app string) (Route, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	route, ok := m.settings.Apps[app]
	return route, ok && route.Enabled && !m.settings.Off
}

// URL returns the address of an app's HTTPS name, or "" without an
// enabled route. With a domain of one's own that one is preferred.
func (m *Manager) URL(app string) string {
	route, ok := m.Route(app)
	if !ok {
		return ""
	}
	urls := m.urls(route)
	if len(urls) == 0 {
		return ""
	}
	if suffixes := m.Suffixes(); len(suffixes) > 0 {
		return m.url(route.Name, suffixes[len(suffixes)-1])
	}
	return urls[0]
}

// urls are the addresses of a route's main name: one per suffix, then
// the short name.
func (m *Manager) urls(route Route) []string {
	out := []string{}
	for _, suffix := range m.Suffixes() {
		out = append(out, m.url(route.Name, suffix))
	}
	m.mu.RLock()
	noMDNS := m.settings.NoMDNS
	m.mu.RUnlock()
	if route.Short && !noMDNS {
		out = append(out, m.url(route.Name, "local"))
	}
	return out
}

func (m *Manager) url(name, suffix string) string {
	host := name + "." + suffix
	if m.HTTPSPort != 0 && m.HTTPSPort != 443 {
		host = net.JoinHostPort(host, strconv.Itoa(m.HTTPSPort))
	}
	return "https://" + host
}

// AppInfo is a route as the web UI shows it, with its addresses. For a
// target, App is its id.
type AppInfo struct {
	Route
	App  string   `json:"app"`
	URLs []string `json:"urls"`
	// Tailnet is the state of its device in the tailnet, if it has one.
	Tailnet *TailnetNode `json:"tailnet,omitempty"`
}

// Status is everything the settings page shows.
type Status struct {
	Domain string `json:"domain"`
	// LocalName is the chosen local name ("" = the NAS's name, which
	// DefaultLocalName tells).
	LocalName        string        `json:"localName"`
	DefaultLocalName string        `json:"defaultLocalName"`
	MDNS             bool          `json:"mdns"`
	Off              bool          `json:"off"`
	Suffixes         []string      `json:"suffixes"`
	Apps             []AppInfo     `json:"apps"`
	Targets          []AppInfo     `json:"targets"`
	Tailnet          TailnetStatus `json:"tailnet"`
}

// Info lists every route with its addresses, sorted by app and target.
func (m *Manager) Info() Status {
	settings := m.Settings()
	status := Status{
		Domain: settings.Domain, LocalName: settings.LocalName, DefaultLocalName: m.host(),
		MDNS: !settings.NoMDNS, Off: settings.Off, Suffixes: m.Suffixes(),
	}
	list := func(prefix string, routes map[string]Route) []AppInfo {
		out := []AppInfo{}
		for key, route := range routes {
			if route.Scheme == "" {
				route.Scheme = "http"
			}
			info := AppInfo{Route: route, App: key, URLs: m.urls(route)}
			if route.Tailscale {
				info.Tailnet = m.tailnetNode(prefix + key)
				if info.Tailnet == nil {
					// Not looked at yet: it starts if the plugin runs.
					info.Tailnet = &TailnetNode{State: "stopped", Error: "plugin"}
					if _, ok := m.Tailnet.available(); ok {
						info.Tailnet = &TailnetNode{State: "starting"}
					}
				}
			}
			out = append(out, info)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].App < out[j].App })
		return out
	}
	status.Apps = list("app:", settings.Apps)
	status.Targets = list("target:", settings.Targets)
	status.Tailnet = m.TailnetInfo()
	if status.Suffixes == nil {
		status.Suffixes = []string{}
	}
	return status
}

// Publishing tells whether names have to be published over mDNS.
func (m *Manager) Publishing() bool {
	if m.Disabled {
		return false
	}
	m.mu.RLock()
	noMDNS := m.settings.NoMDNS
	m.mu.RUnlock()
	return !noMDNS && len(m.enabled()) > 0
}
