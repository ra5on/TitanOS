// Package ddns keeps host names pointed at the NAS: the A record at the
// public IPv4 address of the internet connection, the AAAA record at the
// global IPv6 address of the NAS itself.
//
// The public IPv4 address is asked from a "what is my IP" service over
// IPv4, so it does not depend on the router. The IPv6 address is read from
// the interfaces of the NAS (/proc/net/if_inet6), skipping temporary
// (privacy) and deprecated addresses and those of Docker, VMs, Tailscale
// and NetBird. Records are only changed when an address changed.
//
// Settings live in /config/ddns/settings.json (hidden from the file
// manager), read once at start. Status is kept in RAM.
package ddns

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
	"github.com/ra5on/TitanOS/backend/internal/netguard"
)

const (
	settingsFile = "ddns/settings.json"
	// DefaultInterval is how often the addresses are looked at, in minutes.
	DefaultInterval = 10
	maxHosts        = 8
	// retry is the wait after a failure when the interval is longer.
	retry = 5 * time.Minute
)

// LookupServices answer a plain request with the address it came from.
// They are asked in turn when no own service is set.
var LookupServices = []string{"https://api.ipify.org", "https://ipv4.icanhazip.com", "https://v4.ident.me"}

// IPv64Lookup is GET /ip of the API of IPv64.net (JSON, no sign-in).
const IPv64Lookup = "https://ipv64.net/api/v2/ip"

var (
	ErrInvalid    = errors.New("ddns: invalid settings")
	ErrNotLoaded  = errors.New("ddns: settings not loaded from /config")
	ErrNotEnabled = errors.New("ddns: not switched on")
)

// Settings say which names to keep up to date and how.
type Settings struct {
	Enabled   bool               `json:"enabled"`
	Hostnames []string           `json:"hostnames"`
	Provider  dnsprovider.Config `json:"provider"`
	IPv4      bool               `json:"ipv4"`
	IPv6      bool               `json:"ipv6"`
	// Interval in minutes, 5–1440; 0 is DefaultInterval.
	Interval int `json:"interval,omitempty"`
	// LookupURL is an own service for the IPv4 address; empty asks
	// LookupServices.
	LookupURL string `json:"lookupURL,omitempty"`
	// Interface takes the IPv6 address from this interface only.
	Interface string `json:"interface,omitempty"`
	// Heartbeat pings a healthcheck of IPv64.net (heartbeat.go). Off
	// unless switched on.
	Heartbeat Heartbeat `json:"heartbeat"`
}

// Status is what the settings page shows.
type Status struct {
	Settings Settings        `json:"settings"`
	Secrets  map[string]bool `json:"secrets"`
	// CurrentIPv4/6 are the addresses found at the last look.
	CurrentIPv4 string `json:"currentIPv4,omitempty"`
	CurrentIPv6 string `json:"currentIPv6,omitempty"`
	// PublishedIPv4/6 are the addresses the records point at.
	PublishedIPv4 string     `json:"publishedIPv4,omitempty"`
	PublishedIPv6 string     `json:"publishedIPv6,omitempty"`
	LastCheck     *time.Time `json:"lastCheck,omitempty"`
	LastUpdate    *time.Time `json:"lastUpdate,omitempty"`
	LastError     string     `json:"lastError,omitempty"`
	NextCheck     *time.Time `json:"nextCheck,omitempty"`
	Running       bool       `json:"running"`
	Disabled      bool       `json:"disabled,omitempty"`
	// Heartbeat is the state of the pings; the token is only "set".
	Heartbeat HeartbeatStatus `json:"heartbeat"`
}

// Manager looks at the addresses and updates the records.
type Manager struct {
	store *config.Store
	// Disabled saves settings but changes no records (development mode).
	Disabled bool
	OnEvent  func(severity, kind, subject string)
	Activity func(action, target string, ok bool)
	// IPv64Options are for tests: another address for IPv64.net.
	IPv64Options dnsprovider.Options

	// For tests.
	lookupIPv4 func(ctx context.Context, address string) (netip.Addr, error)
	ipv6       func(iface string) (netip.Addr, error)
	resolve    func(ctx context.Context, host string) ([]netip.Addr, error)
	newUpdater func(dnsprovider.Config) (dnsprovider.Updater, error)
	now        func() time.Time

	mu         sync.Mutex
	loaded     bool
	settings   Settings
	current4   netip.Addr
	current6   netip.Addr
	published4 netip.Addr
	published6 netip.Addr
	seeded     bool
	lastCheck  time.Time
	lastUpdate time.Time
	lastError  string
	failing    bool
	running    bool
	nextCheck  time.Time
	retryAfter time.Duration
	limitKnown bool
	kick       chan bool

	// The healthcheck of IPv64 (heartbeat.go).
	beat beatState
}

// New returns a manager for the configuration in store.
func New(store *config.Store) *Manager {
	return &Manager{
		store:      store,
		lookupIPv4: lookupIPv4,
		ipv6:       globalIPv6,
		resolve: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
		newUpdater: func(c dnsprovider.Config) (dnsprovider.Updater, error) {
			return dnsprovider.NewUpdater(c, dnsprovider.Options{})
		},
		now:  time.Now,
		kick: make(chan bool, 1),
		beat: beatState{kick: make(chan struct{}, 1)},
	}
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Load reads the settings from /config.
func (m *Manager) Load() error {
	settings := Settings{IPv4: true, IPv6: true, Hostnames: []string{}}
	err := m.store.ReadJSON(settingsFile, &settings)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if settings.Hostnames == nil {
		settings.Hostnames = []string{}
	}
	m.mu.Lock()
	m.settings, m.loaded = settings, true
	m.loadPaused()
	m.mu.Unlock()
	return nil
}

var interfaceName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,15}$`)

func normalize(s Settings) (Settings, error) {
	seen := map[string]bool{}
	hosts := []string{}
	for _, h := range s.Hostnames {
		h = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), ".")
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		if !dnsprovider.ValidHost(h) || strings.HasSuffix(h, ".local") {
			return s, invalid("host name %q", h)
		}
		hosts = append(hosts, h)
	}
	if len(hosts) > maxHosts {
		return s, invalid("at most %d host names", maxHosts)
	}
	s.Hostnames = hosts
	if s.Interval != 0 && (s.Interval < 5 || s.Interval > 1440) {
		return s, invalid("interval")
	}
	s.LookupURL = strings.TrimSpace(s.LookupURL)
	if s.LookupURL != "" {
		parsed, err := url.Parse(s.LookupURL)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || len(s.LookupURL) > 500 {
			return s, invalid("address of the lookup service")
		}
	}
	s.Interface = strings.TrimSpace(s.Interface)
	if s.Interface != "" && !interfaceName.MatchString(s.Interface) {
		return s, invalid("interface")
	}
	s.Provider = s.Provider.Normalize()
	if s.Enabled || s.Provider.Type != "" {
		if err := s.Provider.Validate(dnsprovider.ForAddress); err != nil {
			return s, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
	}
	if s.Provider.Type == dnsprovider.DuckDNS {
		for _, h := range hosts {
			if !strings.HasSuffix(h, ".duckdns.org") {
				return s, invalid("DuckDNS only has names under duckdns.org")
			}
		}
	}
	if err := normalizeHeartbeat(&s); err != nil {
		return s, err
	}
	if s.Enabled {
		if len(hosts) == 0 {
			return s, invalid("no host name")
		}
		if !s.IPv4 && !s.IPv6 {
			return s, invalid("neither IPv4 nor IPv6")
		}
	}
	return s, nil
}

// Set stores new settings. Secrets left empty keep the stored ones for
// the same provider account; clear names secrets to drop.
func (m *Manager) Set(s Settings, clear []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	s.Provider = s.Provider.Normalize().KeepSecrets(m.settings.Provider, clear)
	if s.Heartbeat.Token == "" && !slices.Contains(clear, "heartbeat") {
		s.Heartbeat.Token = m.settings.Heartbeat.Token
		s.Heartbeat.Check = m.settings.Heartbeat.Check
	}
	return m.keep(s)
}

// keep checks and stores new settings; called with mu held.
func (m *Manager) keep(s Settings) error {
	s, err := normalize(s)
	if err != nil {
		return err
	}
	if err := m.store.WriteJSON(settingsFile, s); err != nil {
		return err
	}
	m.settings = s
	// Other names or another provider: look up again what they point at.
	m.published4, m.published6, m.seeded = netip.Addr{}, netip.Addr{}, false
	m.lastError, m.failing, m.retryAfter, m.limitKnown = "", false, 0, false
	m.poke(false)
	m.pokeBeat()
	return nil
}

// ErrOtherProvider means the names are kept up to date by a provider other
// than IPv64.net, so a name of IPv64 cannot join them.
var ErrOtherProvider = errors.New("ddns: another provider is in use")

// AddHostname adds one name to those kept up to date, leaving everything
// else as it is (Settings › IPv64 assigns domains this way). Without a
// provider, IPv64.net with ipv64Key becomes it. With IPv64.net and a key
// or update token, dynamic DNS is switched on (with IPv4 and IPv6 if
// neither was chosen). At most maxHosts names.
func (m *Manager) AddHostname(host, ipv64Key string) error {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	next := m.settings
	next.Hostnames = slices.Clone(m.settings.Hostnames)
	switch next.Provider.Type {
	case "":
		if ipv64Key == "" {
			return invalid("no provider and no API key of IPv64")
		}
		next.Provider = dnsprovider.Config{Type: dnsprovider.IPv64Net, Token: ipv64Key}
	case dnsprovider.IPv64Net:
	default:
		return ErrOtherProvider
	}
	if slices.Contains(next.Hostnames, host) && next.Enabled {
		return nil
	}
	if !slices.Contains(next.Hostnames, host) {
		next.Hostnames = append(next.Hostnames, host)
	}
	if next.Provider.Token != "" || next.Provider.UpdateToken != "" {
		next.Enabled = true
		if !next.IPv4 && !next.IPv6 {
			next.IPv4, next.IPv6 = true, true
		}
	}
	return m.keep(next)
}

// RemoveHostname takes one name out of those kept up to date; the record
// at the provider stays. Without names left, dynamic DNS switches off.
func (m *Manager) RemoveHostname(host string) error {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	if !slices.Contains(m.settings.Hostnames, host) {
		return nil
	}
	next := m.settings
	next.Hostnames = slices.DeleteFunc(slices.Clone(m.settings.Hostnames), func(h string) bool { return h == host })
	if len(next.Hostnames) == 0 {
		next.Enabled = false
	}
	return m.keep(next)
}

// Hostnames returns the names kept up to date.
func (m *Manager) Hostnames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.settings.Hostnames)
}

// UpdateNow sends the addresses even if they did not change.
func (m *Manager) UpdateNow() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.settings.Enabled {
		return ErrNotEnabled
	}
	m.poke(true)
	return nil
}

func (m *Manager) poke(force bool) {
	select {
	case m.kick <- force:
	default:
		if force {
			select {
			case <-m.kick:
			default:
			}
			select {
			case m.kick <- true:
			default:
			}
		}
	}
}

func (s Settings) interval() time.Duration {
	if s.Interval == 0 {
		return DefaultInterval * time.Minute
	}
	return time.Duration(s.Interval) * time.Minute
}

// Run looks at the addresses every interval until ctx ends.
func (m *Manager) Run(ctx context.Context) {
	go m.runHeartbeat(ctx)
	force := false
	for {
		m.check(ctx, force)
		m.mu.Lock()
		wait := m.settings.interval()
		if m.lastError != "" {
			wait = min(wait, retry)
		}
		// A provider that asks to wait (429, locked key) is obeyed.
		wait = max(wait, m.retryAfter)
		m.nextCheck = m.now().Add(wait)
		m.mu.Unlock()
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case force = <-m.kick:
			timer.Stop()
		case <-timer.C:
			force = false
		}
	}
}

func (m *Manager) event(severity, kind, subject string) {
	if m.OnEvent != nil {
		m.OnEvent(severity, kind, subject)
	}
}

func (m *Manager) activity(action, target string, ok bool) {
	if m.Activity != nil {
		m.Activity(action, target, ok)
	}
}

// detect finds the addresses to publish. A family that fails is left out
// and named in the error.
func (m *Manager) detect(ctx context.Context, s Settings) (v4, v6 netip.Addr, problems []string) {
	if s.IPv4 {
		services := LookupServices
		if s.Provider.Type == dnsprovider.IPv64Net {
			// Users of IPv64 ask IPv64 first.
			services = append([]string{IPv64Lookup}, LookupServices...)
		}
		if s.LookupURL != "" {
			services = []string{s.LookupURL}
		}
		var last error
		for _, service := range services {
			addr, err := m.lookupIPv4(ctx, service)
			if err == nil && (!addr.Is4() || !netguard.Public(addr)) {
				err = fmt.Errorf("%s is not a public IPv4 address", addr)
			}
			if err == nil {
				v4, last = addr, nil
				break
			}
			last = err
		}
		if last != nil {
			problems = append(problems, "IPv4: "+last.Error())
		}
	}
	if s.IPv6 {
		addr, err := m.ipv6(s.Interface)
		if err != nil {
			problems = append(problems, "IPv6: "+err.Error())
		} else {
			v6 = addr
		}
	}
	return v4, v6, problems
}

// seed learns what the names point at now, so that a start of titand does
// not send an update that changes nothing.
func (m *Manager) seed(ctx context.Context, hosts []string, v4, v6 netip.Addr) (p4, p6 netip.Addr) {
	all4, all6 := v4.IsValid(), v6.IsValid()
	for _, host := range hosts {
		lookup, cancel := context.WithTimeout(ctx, 10*time.Second)
		addrs, err := m.resolve(lookup, host)
		cancel()
		if err != nil {
			return netip.Addr{}, netip.Addr{}
		}
		var four, six []netip.Addr
		for _, a := range addrs {
			if a.Unmap().Is4() {
				four = append(four, a.Unmap())
			} else {
				six = append(six, a)
			}
		}
		all4 = all4 && len(four) == 1 && four[0] == v4
		all6 = all6 && len(six) == 1 && six[0] == v6
	}
	if all4 {
		p4 = v4
	}
	if all6 {
		p6 = v6
	}
	return p4, p6
}

// check looks at the addresses once and updates the records if needed.
func (m *Manager) check(ctx context.Context, force bool) {
	m.mu.Lock()
	s := m.settings
	if !m.loaded || !s.Enabled || m.Disabled {
		m.mu.Unlock()
		return
	}
	m.running = true
	seeded, p4, p6 := m.seeded, m.published4, m.published6
	m.mu.Unlock()

	m.learnLimit(ctx, s)
	v4, v6, problems := m.detect(ctx, s)
	if !seeded && !force && (v4.IsValid() || v6.IsValid()) {
		p4, p6 = m.seed(ctx, s.Hostnames, v4, v6)
		seeded = true
	}
	send4 := v4.IsValid() && (force || v4 != p4)
	send6 := v6.IsValid() && (force || v6 != p6)
	if (send4 || send6) && m.combined(s.Provider) {
		// The service could take a missing address from the request.
		send4, send6 = v4.IsValid(), v6.IsValid()
	}
	var sendErr error
	if send4 || send6 {
		sendErr = m.send(ctx, s, v4, v6, send4, send6)
	}
	sent := (send4 || send6) && sendErr == nil
	// A family that could not be found is worth a look even when the other
	// one went through.
	err := sendErr
	if err == nil && len(problems) > 0 {
		err = errors.New(strings.Join(problems, "; "))
	}

	now := m.now()
	m.mu.Lock()
	m.running = false
	if ctx.Err() != nil {
		m.mu.Unlock()
		return
	}
	// The settings may have changed meanwhile; then this look is stale.
	if !slices.Equal(m.settings.Hostnames, s.Hostnames) || m.settings.Provider != s.Provider {
		m.mu.Unlock()
		return
	}
	m.current4, m.current6, m.lastCheck, m.seeded = v4, v6, now, seeded
	m.published4, m.published6 = p4, p6
	if sent {
		if send4 {
			m.published4 = v4
		}
		if send6 {
			m.published6 = v6
		}
		// What was sent is known now; DNS caches would only tell old news.
		m.lastUpdate, m.seeded = now, true
	}
	wasFailing := m.failing
	m.retryAfter = 0
	var apiErr *dnsprovider.APIError
	if errors.As(err, &apiErr) {
		m.retryAfter = min(apiErr.RetryAfter, 24*time.Hour)
		if apiErr.Status == 429 || apiErr.Status == 401 {
			// IPv64 locks a key for 10 minutes after many failures.
			m.retryAfter = max(m.retryAfter, 10*time.Minute)
		}
	}
	if err != nil {
		m.lastError, m.failing = err.Error(), true
	} else {
		m.lastError, m.failing = "", false
	}
	m.mu.Unlock()

	target := strings.Join(s.Hostnames, ", ")
	if sent {
		var addrs []string
		if send4 {
			addrs = append(addrs, v4.String())
		}
		if send6 {
			addrs = append(addrs, v6.String())
		}
		slog.Info("dynamic DNS updated", "hosts", s.Hostnames, "addresses", addrs)
		m.activity("ddns_updated", target+": "+strings.Join(addrs, ", "), true)
	}
	if err != nil {
		slog.Warn("dynamic DNS", "hosts", s.Hostnames, "err", err)
		if !wasFailing {
			m.event("warning", "ddns_failed", s.Hostnames[0])
			m.activity("ddns_failed", target, false)
		}
	}
}

// combined tells whether the provider wants both addresses at once.
func (m *Manager) combined(c dnsprovider.Config) bool {
	updater, err := m.newUpdater(c)
	if err != nil {
		return false
	}
	combined, ok := updater.(dnsprovider.Combined)
	return ok && combined.CombinedUpdate()
}

// send updates every host name; any failure fails all, so the next look
// tries again.
func (m *Manager) send(ctx context.Context, s Settings, v4, v6 netip.Addr, send4, send6 bool) error {
	updater, err := m.newUpdater(s.Provider)
	if err != nil {
		return err
	}
	if !send4 {
		v4 = netip.Addr{}
	}
	if !send6 {
		v6 = netip.Addr{}
	}
	for _, host := range s.Hostnames {
		update, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err := updater.SetAddresses(update, host, v4, v6)
		cancel()
		if err != nil {
			return fmt.Errorf("%s: %w", host, err)
		}
	}
	return nil
}

// Status tells the settings page what is going on.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.settings
	st := Status{Settings: s, Secrets: s.Provider.Secrets(), LastError: m.lastError, Running: m.running, Disabled: m.Disabled}
	st.Settings.Provider = s.Provider.Public()
	st.Secrets["heartbeat"] = s.Heartbeat.Token != ""
	st.Settings.Heartbeat.Token = ""
	st.Heartbeat = m.beatStatus()
	text := func(a netip.Addr) string {
		if a.IsValid() {
			return a.String()
		}
		return ""
	}
	stamp := func(t time.Time) *time.Time {
		if t.IsZero() {
			return nil
		}
		return &t
	}
	st.CurrentIPv4, st.CurrentIPv6 = text(m.current4), text(m.current6)
	st.PublishedIPv4, st.PublishedIPv6 = text(m.published4), text(m.published6)
	st.LastCheck, st.LastUpdate = stamp(m.lastCheck), stamp(m.lastUpdate)
	if s.Enabled && !m.Disabled {
		st.NextCheck = stamp(m.nextCheck)
	}
	return st
}

// ipv4Client connects over IPv4 only; one for all lookups, so idle
// connections are reused and closed after a while instead of piling up.
var ipv4Client = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: 10 * time.Second}
			return dialer.DialContext(ctx, "tcp4", addr)
		},
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConnsPerHost: 2,
	},
}

// lookupIPv4 asks a service over IPv4 which address the request came from.
func lookupIPv4(ctx context.Context, address string) (netip.Addr, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return netip.Addr{}, errors.New("lookup address not usable")
	}
	req.Header.Set("User-Agent", "TitanOS")
	res, err := ipv4Client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return netip.Addr{}, fmt.Errorf("%s: %w", req.URL.Host, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 256))
	// Read what is left (bounded) so the connection can be used again.
	io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	if res.StatusCode != http.StatusOK {
		return netip.Addr{}, fmt.Errorf("%s: HTTP %d", req.URL.Host, res.StatusCode)
	}
	text := strings.TrimSpace(string(body))
	// IPv64 answers {"data":{"ip":…}}; the others the plain address.
	var answer struct {
		Data struct {
			IP string `json:"ip"`
		} `json:"data"`
	}
	if strings.HasPrefix(text, "{") && json.Unmarshal(body, &answer) == nil {
		text = answer.Data.IP
	}
	addr, err := netip.ParseAddr(text)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("%s: no address in the answer", req.URL.Host)
	}
	return addr.Unmap(), nil
}

// Flags of addresses in /proc/net/if_inet6.
const (
	flagTemporary  = 0x01
	flagDADFailed  = 0x08
	flagDeprecated = 0x20
	flagTentative  = 0x40
)

// skipInterface are interfaces whose addresses are not the NAS on the
// internet: containers, VMs and the overlay networks.
var skipInterface = []string{"lo", "docker", "veth", "br-", "virbr", "vnet", "tap", "lxc", "tailscale", "ts", "wt", "nb"}

var ifInet6 = "/proc/net/if_inet6"

// globalIPv6 returns the first stable global IPv6 address of the NAS.
func globalIPv6(iface string) (netip.Addr, error) {
	file, err := os.Open(ifInet6)
	if err != nil {
		return netip.Addr{}, err
	}
	defer file.Close()
	return pickIPv6(file, iface)
}

func pickIPv6(r io.Reader, iface string) (netip.Addr, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 6 || len(fields[0]) != 32 {
			continue
		}
		name := fields[5]
		if iface != "" && name != iface {
			continue
		}
		if iface == "" && slices.ContainsFunc(skipInterface, func(p string) bool { return strings.HasPrefix(name, p) }) {
			continue
		}
		flags, err := strconv.ParseUint(fields[4], 16, 32)
		if err != nil || flags&(flagTemporary|flagDADFailed|flagDeprecated|flagTentative) != 0 {
			continue
		}
		var raw [16]byte
		for i := range raw {
			b, err := strconv.ParseUint(fields[0][2*i:2*i+2], 16, 8)
			if err != nil {
				break
			}
			raw[i] = byte(b)
		}
		addr := netip.AddrFrom16(raw)
		if netguard.Public(addr) {
			return addr, nil
		}
	}
	if iface != "" {
		return netip.Addr{}, fmt.Errorf("no stable global address on %s", iface)
	}
	return netip.Addr{}, errors.New("the NAS has no stable global address")
}
