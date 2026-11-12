// Package ipv64 keeps the list of Docker apps TitanOS made reachable
// through the CDN of IPv64.net ("public through IPv64" in the app editor).
//
// The account area of IPv64.net (own API key, notifications from polling,
// v64 blocklist, WireGuard tunnel into a Cloud Router, maintenance pages)
// was removed with PLAN 79-2. Its settings share /config/ipv64/settings.json
// with the list kept here: they are neither used nor dropped, so that an
// older version of the app still finds them after a way back. The only
// thing still read besides the list is the area's own API key, as the last
// source of a key, so that an app published with it can be taken back.
package ipv64

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

const settingsFile = "ipv64/settings.json"

// tunnelInterface is the WireGuard tunnel an older version set up into a
// Cloud Router of IPv64.
const tunnelInterface = "ipv64wg"

var (
	ErrNotLoaded = errors.New("ipv64: settings could not be read from /config")
	ErrNoKey     = errors.New("ipv64: no API key of the account")
)

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

// Manager keeps the published apps.
type Manager struct {
	store  *config.Store
	runner run.Runner
	// Disabled changes nothing on the system (development mode).
	Disabled bool
	// Options lets tests point the client at a fake.
	Options dnsprovider.Options
	// Fallback returns the keys of dynamic DNS and Let's Encrypt.
	Fallback func() (ddns, letsencrypt string)

	// exists tells whether a network interface is there (tests).
	exists func(iface string) bool

	mu     sync.Mutex
	loaded bool
	// file is settings.json as it was read, field by field: what this
	// version does not know is written back unchanged.
	file      map[string]json.RawMessage
	legacyKey string
	published []Published
}

// NewManager returns a manager working on the real system.
func NewManager(store *config.Store) *Manager {
	return &Manager{store: store, runner: run.Exec{}, exists: func(iface string) bool {
		_, err := os.Stat("/sys/class/net/" + iface)
		return err == nil
	}}
}

// Load reads the settings once at the start. Settings of the removed
// account area are left alone; a tunnel an older version of the app left
// running (an app update restarts titand, not the NAS) is taken down,
// because nothing manages it any more.
func (m *Manager) Load(ctx context.Context) error {
	file := map[string]json.RawMessage{}
	err := m.store.ReadJSON(settingsFile, &file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var published []Published
	var key string
	if raw, ok := file["published"]; ok {
		if err := json.Unmarshal(raw, &published); err != nil {
			return err
		}
	}
	if raw, ok := file["apiKey"]; ok {
		_ = json.Unmarshal(raw, &key) // a key of another shape is no key
	}
	for i := range published {
		if published[i].Records == nil {
			published[i].Records = []string{}
		}
	}
	m.mu.Lock()
	m.file, m.published, m.legacyKey, m.loaded = file, published, key, true
	m.mu.Unlock()
	m.removeLeftoverTunnel(ctx)
	return nil
}

func (m *Manager) removeLeftoverTunnel(ctx context.Context) {
	if m.Disabled || m.exists == nil || !m.exists(tunnelInterface) {
		return
	}
	if _, err := m.runner.Run(ctx, "", "ip", "link", "del", "dev", tunnelInterface); err != nil {
		slog.Warn("tunnel to the IPv64 Cloud Router of an older version not removed", "interface", tunnelInterface, "err", err)
		return
	}
	slog.Info("tunnel to the IPv64 Cloud Router of an older version removed; the Cloud Router is no longer part of TitanOS", "interface", tunnelInterface)
}

// update changes the list under the lock and stores it, keeping every
// other field of the file as it was.
func (m *Manager) update(change func([]Published) []Published) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	next := change(slices.Clone(m.published))
	if next == nil {
		next = []Published{}
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	file := make(map[string]json.RawMessage, len(m.file)+1)
	for k, v := range m.file {
		file[k] = v
	}
	file["published"] = raw
	if err := m.store.WriteJSON(settingsFile, file); err != nil {
		return err
	}
	m.file, m.published = file, next
	return nil
}

// Key returns the API key in use and where it comes from: ddns,
// letsencrypt or legacy (the own key of the removed account area).
func (m *Manager) Key() (string, string) {
	if m.Fallback != nil {
		ddns, le := m.Fallback()
		if ddns != "" {
			return ddns, "ddns"
		}
		if le != "" {
			return le, "letsencrypt"
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.legacyKey != "" {
		return m.legacyKey, "legacy"
	}
	return "", ""
}

// Client returns a client with the key in use.
func (m *Manager) Client() (*dnsprovider.IPv64, error) {
	key, _ := m.Key()
	if key == "" {
		return nil, ErrNoKey
	}
	return dnsprovider.NewIPv64(key, "", m.Options), nil
}

// AddPublished records an app published through the CDN.
func (m *Manager) AddPublished(p Published) error {
	if p.Records == nil {
		p.Records = []string{}
	}
	return m.update(func(list []Published) []Published {
		list = slices.DeleteFunc(list, func(o Published) bool { return o.App == p.App })
		return append(list, p)
	})
}

// RemovePublished forgets a published app.
func (m *Manager) RemovePublished(app string) error {
	return m.update(func(list []Published) []Published {
		return slices.DeleteFunc(list, func(o Published) bool { return o.App == app })
	})
}

// PublishedApp returns the entry of an app, if any.
func (m *Manager) PublishedApp(app string) (Published, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.published {
		if p.App == app {
			return p, true
		}
	}
	return Published{}, false
}
