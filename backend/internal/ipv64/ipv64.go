// Package ipv64 keeps what TitanOS itself does with an IPv64.net account
// beyond dynamic DNS: its own API key (optional), notifications from
// polling the account, the v64 blocklist for the firewall, the WireGuard
// tunnel of the NAS into a Cloud Router, and the list of what TitanOS
// created at IPv64 for apps and the maintenance page.
//
// Everything is off by default and nothing is created at IPv64 without an
// explicit action in the web UI. Settings live in /config/ipv64/ and are
// written only when they change; polling works from RAM.
package ipv64

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

const settingsFile = "ipv64/settings.json"

var (
	ErrNotLoaded = errors.New("ipv64: settings could not be read from /config")
	ErrNoKey     = errors.New("ipv64: no API key of the account")
	ErrDisabled  = errors.New("ipv64: not available in development mode")
)

// Notify chooses which polled changes become notifications.
type Notify struct {
	Enabled bool `json:"enabled"`
	// Failover: a policy switched to its replacement or back.
	Failover bool `json:"failover"`
	// CDNTraffic: the CDN traffic of the month is used up.
	CDNTraffic bool `json:"cdnTraffic"`
	// Healthchecks: a healthcheck went down.
	Healthchecks bool `json:"healthchecks"`
}

// Blocklist feeds the firewall with the v64 blocklist.
type Blocklist struct {
	Enabled bool `json:"enabled"`
}

// Published is an app TitanOS made reachable through the CDN of IPv64.
type Published struct {
	App    string `json:"app"`
	Port   int    `json:"port"`
	Domain string `json:"domain"`
	Prefix string `json:"prefix,omitempty"`
	// CDN is the CDN ID; CreatedCDN says TitanOS took the domain into it.
	CDN        string   `json:"cdn"`
	CreatedCDN bool     `json:"createdCDN,omitempty"`
	Records    []string `json:"records"`
	// Firewall says TitanOS allowed the app from the internet zone;
	// Before are its zones until then.
	Firewall bool       `json:"firewall,omitempty"`
	Before   *AppZones  `json:"before,omitempty"`
	At       time.Time  `json:"at"`
	Pending  []string   `json:"pending,omitempty"`
	Updated  *time.Time `json:"updated,omitempty"`
}

// AppZones copies firewall.Zones without importing the package here.
type AppZones struct {
	Home     bool `json:"home"`
	VPN      bool `json:"vpn"`
	Internet bool `json:"internet"`
}

// Maintenance is the set of resources of "maintenance page when this NAS
// fails" for one domain.
type Maintenance struct {
	ID          string    `json:"id"`
	Domain      string    `json:"domain"`
	Check       string    `json:"check"`
	CheckName   string    `json:"checkName"`
	CreatedNew  bool      `json:"createdCheck,omitempty"`
	OfflinePage string    `json:"offlinePage"`
	Policy      string    `json:"policy"`
	Records     []string  `json:"records"`
	At          time.Time `json:"at"`
}

// Settings is /config/ipv64/settings.json.
type Settings struct {
	// APIKey is an own key for this area; empty takes the one of dynamic
	// DNS or Let's Encrypt. Never shown.
	APIKey      string        `json:"apiKey,omitempty"`
	Notify      Notify        `json:"notify"`
	Blocklist   Blocklist     `json:"blocklist"`
	WireGuard   WireGuard     `json:"wireguard"`
	Published   []Published   `json:"published"`
	Maintenance []Maintenance `json:"maintenance"`
}

// Status is what the page shows; the key only as "is set".
type Status struct {
	Settings Settings `json:"settings"`
	// KeySet says an own key is stored; KeySource names where the key in
	// use comes from: own, ddns, letsencrypt or "".
	KeySet    bool            `json:"keySet"`
	KeySource string          `json:"keySource"`
	Poll      PollStatus      `json:"poll"`
	List      BlocklistStatus `json:"blocklistStatus"`
	Tunnel    TunnelStatus    `json:"tunnel"`
	ReadOnly  bool            `json:"readOnly"`
}

// Manager keeps the settings and the background work.
type Manager struct {
	store  *config.Store
	runner run.Runner
	// Disabled changes nothing on the system (development mode).
	Disabled bool
	// Options lets tests point the client at a fake.
	Options dnsprovider.Options
	// Fallback returns the keys of dynamic DNS and Let's Encrypt.
	Fallback func() (ddns, letsencrypt string)
	// Notify sends a notification (severity, kind, subject).
	Notify func(severity, kind, subject string)
	// Firewall takes the blocklist; nil leaves it out.
	Firewall interface {
		SetBlocklist(ctx context.Context, list []netip.Prefix) error
	}
	// Fetch is the HTTP client for the blocklist file (tests).
	now     func() time.Time
	runDir  string
	pokeCh  chan struct{}
	poll    pollState
	list    listState
	tunnel  tunnelState
	mu      sync.Mutex
	loaded  bool
	current Settings
}

// NewManager returns a manager working on the real system.
func NewManager(store *config.Store) *Manager {
	return &Manager{store: store, runner: run.Exec{}, now: time.Now, runDir: "/run/titanos/ipv64", pokeCh: make(chan struct{}, 1)}
}

func normal(s Settings) Settings {
	if s.Published == nil {
		s.Published = []Published{}
	}
	if s.Maintenance == nil {
		s.Maintenance = []Maintenance{}
	}
	for i := range s.Published {
		if s.Published[i].Records == nil {
			s.Published[i].Records = []string{}
		}
	}
	return s
}

// Load reads the settings once at the start and brings the tunnel up if
// it is switched on.
func (m *Manager) Load(ctx context.Context) error {
	s := Settings{}
	err := m.store.ReadJSON(settingsFile, &s)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.mu.Lock()
	m.current, m.loaded = normal(s), true
	wg := m.current.WireGuard
	m.mu.Unlock()
	if wg.Enabled && !m.Disabled {
		key, psk := m.readKeys()
		if err := m.up(ctx, wg, key, psk); err != nil {
			slog.Error("IPv64 WireGuard tunnel not up", "err", err)
		}
	}
	return nil
}

// Settings returns a copy of the settings with the key.
func (m *Manager) Settings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return clone(m.current)
}

func clone(s Settings) Settings {
	out := s
	out.Published = slices.Clone(s.Published)
	out.Maintenance = slices.Clone(s.Maintenance)
	return normal(out)
}

// update changes the settings under the lock and stores them.
func (m *Manager) update(change func(*Settings) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	next := clone(m.current)
	if err := change(&next); err != nil {
		return err
	}
	if err := m.store.WriteJSON(settingsFile, next); err != nil {
		return err
	}
	m.current = next
	m.wake()
	return nil
}

func (m *Manager) wake() {
	select {
	case m.pokeCh <- struct{}{}:
	default:
	}
}

// Key returns the API key in use and where it comes from.
func (m *Manager) Key() (string, string) {
	m.mu.Lock()
	own := m.current.APIKey
	m.mu.Unlock()
	if own != "" {
		return own, "own"
	}
	if m.Fallback != nil {
		ddns, le := m.Fallback()
		if ddns != "" {
			return ddns, "ddns"
		}
		if le != "" {
			return le, "letsencrypt"
		}
	}
	return "", ""
}

// Keys returns every distinct key TitanOS keeps, by where it is used.
func (m *Manager) Keys() map[string]string {
	out := map[string]string{}
	m.mu.Lock()
	if m.current.APIKey != "" {
		out["own"] = m.current.APIKey
	}
	m.mu.Unlock()
	if m.Fallback != nil {
		ddns, le := m.Fallback()
		if ddns != "" {
			out["ddns"] = ddns
		}
		if le != "" {
			out["letsencrypt"] = le
		}
	}
	return out
}

// Client returns a client with the key in use.
func (m *Manager) Client() (*dnsprovider.IPv64, error) {
	key, _ := m.Key()
	if key == "" {
		return nil, ErrNoKey
	}
	return dnsprovider.NewIPv64(key, "", m.Options), nil
}

// SetKey stores an own key; "" with clear removes it.
func (m *Manager) SetKey(key string, clear bool) error {
	return m.update(func(s *Settings) error {
		if clear {
			s.APIKey = ""
		} else if key != "" {
			s.APIKey = key
		}
		return nil
	})
}

// SetNotify stores the choice of notifications.
func (m *Manager) SetNotify(n Notify) error {
	return m.update(func(s *Settings) error {
		s.Notify = n
		return nil
	})
}

// SetBlocklist switches the blocklist on or off.
func (m *Manager) SetBlocklist(ctx context.Context, b Blocklist) error {
	err := m.update(func(s *Settings) error {
		s.Blocklist = b
		return nil
	})
	if err == nil && !b.Enabled {
		m.dropList(ctx)
	}
	return err
}

// AddPublished records an app published through the CDN.
func (m *Manager) AddPublished(p Published) error {
	return m.update(func(s *Settings) error {
		s.Published = slices.DeleteFunc(s.Published, func(o Published) bool { return o.App == p.App })
		s.Published = append(s.Published, p)
		return nil
	})
}

// RemovePublished forgets a published app.
func (m *Manager) RemovePublished(app string) error {
	return m.update(func(s *Settings) error {
		s.Published = slices.DeleteFunc(s.Published, func(o Published) bool { return o.App == app })
		return nil
	})
}

// PublishedApp returns the entry of an app, if any.
func (m *Manager) PublishedApp(app string) (Published, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.current.Published {
		if p.App == app {
			return p, true
		}
	}
	return Published{}, false
}

// AddMaintenance records a maintenance page setup.
func (m *Manager) AddMaintenance(entry Maintenance) error {
	return m.update(func(s *Settings) error {
		s.Maintenance = slices.DeleteFunc(s.Maintenance, func(o Maintenance) bool { return o.ID == entry.ID })
		s.Maintenance = append(s.Maintenance, entry)
		return nil
	})
}

// RemoveMaintenance forgets a maintenance page setup.
func (m *Manager) RemoveMaintenance(id string) error {
	return m.update(func(s *Settings) error {
		s.Maintenance = slices.DeleteFunc(s.Maintenance, func(o Maintenance) bool { return o.ID == id })
		return nil
	})
}

// Status returns the settings without the key.
func (m *Manager) Status() Status {
	s := m.Settings()
	_, source := m.Key()
	st := Status{KeySet: s.APIKey != "", KeySource: source, ReadOnly: m.Disabled}
	s.APIKey = ""
	st.Settings = s
	st.Poll = m.pollStatus()
	st.List = m.listStatus()
	st.Tunnel = m.tunnelStatus()
	return st
}

// Run polls for notifications and refreshes the blocklist while switched
// on. It never touches /config.
func (m *Manager) Run(ctx context.Context) {
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-m.pokeCh:
		}
		if !m.Disabled {
			m.pollOnce(ctx)
			m.refreshList(ctx)
			m.retryTunnel(ctx)
		}
		timer.Reset(time.Minute)
	}
}
