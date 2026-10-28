package firewall

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

const (
	// trial is how long a change waits for its confirmation.
	trial = 2 * time.Minute
	// tick is how often interfaces, Docker ports and the table are checked.
	tick         = 15 * time.Second
	applyTimeout = 20 * time.Second
)

// Status is everything the firewall page shows.
type Status struct {
	Config Config `json:"config"`
	// Pending is a change on trial; PendingSeconds is how long it has left.
	Pending        *Config `json:"pending"`
	PendingSeconds int     `json:"pendingSeconds"`
	// Active says whether the table is loaded (also for blocks alone).
	Active bool `json:"active"`
	// Error is the last failure to load the rules.
	Error    string    `json:"error,omitempty"`
	Services []Service `json:"services"`
	Apps     []App     `json:"apps"`
	// HomeNetworks and VPNNetworks are what the zones are made of now.
	HomeNetworks []string `json:"homeNetworks"`
	VPNNetworks  []string `json:"vpnNetworks"`
	// ClientAddress/ClientZone are of the request asking.
	ClientAddress string  `json:"clientAddress"`
	ClientZone    Zone    `json:"clientZone"`
	Blocked       []Block `json:"blocked"`
	Recent        []Drop  `json:"recent"`
}

// Manager keeps the rules in line with the settings.
type Manager struct {
	store  *config.Store
	runner run.Runner
	// Disabled leaves the system alone. Set in development mode.
	Disabled bool
	// Web are the ports of the web UI.
	Web WebPorts
	// HomeNetworks returns the networks of the NAS's own interfaces.
	HomeNetworks func() []string
	// Apps returns the Docker apps with their published ports; ok false
	// keeps the last known list (Docker not answering).
	Apps func(ctx context.Context) ([]App, bool)
	// OnBlock is told about every new automatic block.
	OnBlock func(Block)
	// OnRevert is told when a change was not confirmed and went back.
	OnRevert func()
	// OnFailure is told once when the rules cannot be loaded.
	OnFailure func(error)
	trial     time.Duration
	runFile   string
	now       func() time.Time

	mu      sync.Mutex
	loaded  bool
	active  Config
	pending *Config
	before  Config
	admin   netip.Addr
	// deadline/timer of the change on trial.
	deadline time.Time
	timer    *time.Timer
	home     []netip.Prefix
	apps     []App
	applied  string
	lastErr  string
	failed   bool

	attempts      map[netip.Prefix][]time.Time
	blocks        []Block
	blocksDirty   bool
	blocksWritten time.Time

	recentMu sync.Mutex
	recent   []Drop
}

// NewManager returns a manager working on the real system.
func NewManager(store *config.Store) *Manager {
	return &Manager{store: store, runner: run.Exec{}, trial: trial, runFile: "/run/titanos/firewall-blocks.json", now: time.Now, attempts: map[netip.Prefix][]time.Time{}, active: Defaults()}
}

// Load reads the settings and puts the rules in place. It is called once
// at the start; without settings the firewall is off and nothing changes.
func (m *Manager) Load(ctx context.Context) error {
	cfg := Defaults()
	err := m.store.ReadJSON(settingsFile, &cfg)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	cfg = normal(cfg)
	if err := validate(cfg); err != nil {
		slog.Warn("firewall settings not valid, firewall stays off", "err", err)
		cfg.Enabled = false
		if validateAutoBlock(cfg.AutoBlock) != nil {
			cfg.AutoBlock = Defaults().AutoBlock
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active, m.loaded = cfg, true
	m.refreshHome()
	m.loadBlocks(m.now())
	if m.Disabled {
		return nil
	}
	return m.applyLocked(ctx)
}

func (m *Manager) refreshHome() bool {
	var own []string
	if m.HomeNetworks != nil {
		own = m.HomeNetworks()
	}
	home := homeNetworks(own)
	if slices.Equal(home, m.home) {
		return false
	}
	m.home = home
	return true
}

// current is what the rules are made from: the change on trial, if any.
func (m *Manager) current() Config {
	if m.pending != nil {
		return *m.pending
	}
	return m.active
}

// applyLocked loads the rules of current(). The caller holds mu.
func (m *Manager) applyLocked(ctx context.Context) error {
	if m.Disabled {
		return nil
	}
	rs := ruleset{cfg: m.current(), web: m.Web, home: m.home, apps: m.apps, blocked: m.blockedPrefixes()}
	if m.pending != nil {
		rs.admin = m.admin
	}
	script := render(rs)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), applyTimeout)
	defer cancel()
	if _, err := m.runner.Run(ctx, script, "nft", "-f", "-"); err != nil {
		m.lastErr = err.Error()
		if !m.failed && m.OnFailure != nil {
			m.OnFailure(err)
		}
		m.failed = true
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	m.applied, m.lastErr, m.failed = script, "", false
	m.saveRun()
	return nil
}

// tableNeeded tells whether there should be a table at all.
func (m *Manager) tableNeeded() bool {
	return m.current().Enabled || len(m.blockedPrefixes()) > 0
}

// Run keeps the rules in line with interfaces, Docker and expiring blocks.
func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.mu.Lock()
			m.flushPermanent(m.now(), true)
			m.mu.Unlock()
			return
		case <-ticker.C:
		}
		m.refresh(ctx)
	}
}

func (m *Manager) refresh(ctx context.Context) {
	m.mu.Lock()
	enabled := m.loaded && m.current().Enabled
	m.mu.Unlock()
	var apps []App
	appsKnown := false
	if enabled && m.Apps != nil {
		lookup, cancel := context.WithTimeout(ctx, 5*time.Second)
		apps, appsKnown = m.Apps(lookup)
		cancel()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded || m.Disabled {
		return
	}
	now := m.now()
	changed := m.expire(now)
	if changed {
		m.saveRun()
	}
	if m.refreshHome() {
		changed = true
	}
	if appsKnown && !sameApps(apps, m.apps) {
		m.apps, changed = apps, true
	}
	if !changed && m.tableNeeded() && !m.failed {
		// Someone (nft flush ruleset, a script) may have removed the table.
		check, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err := m.runner.Run(check, "", "nft", "-t", "list", "table", "inet", Table)
		cancel()
		changed = err != nil
	}
	if changed || m.failed {
		if err := m.applyLocked(ctx); err != nil {
			slog.Error("firewall rules not loaded", "err", err)
		}
	}
	m.flushPermanent(now, false)
}

func sameApps(a, b []App) bool {
	return slices.EqualFunc(a, b, func(x, y App) bool {
		return x.Key == y.Key && x.Name == y.Name && x.HostNetwork == y.HostNetwork && slices.Equal(x.Ports, y.Ports)
	})
}

// Change puts new settings on trial. The automatic blocking is not part of
// it (see SetAutoBlock). admin is the address of the web session saving
// it: it must keep reaching the web UI, and is let in while on trial.
func (m *Manager) Change(ctx context.Context, cfg Config, admin string) error {
	if m.Disabled {
		return ErrDisabled
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	if m.pending != nil {
		return ErrPending
	}
	cfg.AutoBlock = m.active.AutoBlock
	cfg = normal(clone(cfg))
	if err := validate(cfg); err != nil {
		return err
	}
	addr, err := netip.ParseAddr(admin)
	if err != nil {
		return fmt.Errorf("%w: unknown client address", ErrInvalid)
	}
	addr = addr.Unmap().WithZone("")
	if cfg.Enabled && !addr.IsLoopback() && !decide(cfg, classify(addr, m.home), addr, "tcp", m.Web.or().HTTPS, "webui") {
		return ErrExcludesYou
	}
	if !cfg.Enabled {
		// Switching off cannot lock anybody out: no trial.
		before := m.active
		m.active = cfg
		if err := m.applyLocked(ctx); err != nil {
			m.active = before
			m.applyLocked(ctx)
			return err
		}
		if err := m.store.WriteJSON(settingsFile, cfg); err != nil {
			return err
		}
		slog.Info("firewall switched off")
		return nil
	}
	m.before, m.pending, m.admin = m.active, &cfg, addr
	if err := m.applyLocked(ctx); err != nil {
		m.pending = nil
		m.applyLocked(ctx)
		return err
	}
	m.deadline = m.now().Add(m.trial)
	m.timer = time.AfterFunc(m.trial, m.trialOver)
	slog.Info("firewall change on trial", "admin", addr)
	return nil
}

func (m *Manager) trialOver() {
	m.mu.Lock()
	if m.pending == nil {
		m.mu.Unlock()
		return
	}
	slog.Warn("firewall change was not confirmed, going back")
	m.pending = nil
	err := m.applyLocked(context.Background())
	m.mu.Unlock()
	if err != nil {
		slog.Error("firewall rollback failed", "err", err)
	}
	if m.OnRevert != nil {
		m.OnRevert()
	}
}

// Confirm keeps the change on trial and stores it on the stick.
func (m *Manager) Confirm(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil {
		return ErrNoPending
	}
	cfg := *m.pending
	cfg.AutoBlock = m.active.AutoBlock
	if err := m.store.WriteJSON(settingsFile, cfg); err != nil {
		return err
	}
	m.timer.Stop()
	m.active, m.pending, m.admin = cfg, nil, netip.Addr{}
	slog.Info("firewall change confirmed")
	// The same rules without the exception for the admin.
	return m.applyLocked(ctx)
}

// Revert gives up the change on trial at once.
func (m *Manager) Revert(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil {
		return ErrNoPending
	}
	m.timer.Stop()
	m.pending, m.admin = nil, netip.Addr{}
	return m.applyLocked(ctx)
}

// SetAutoBlock stores the settings of the automatic blocking. They cannot
// lock anyone out who is not already blocked, so they apply at once.
func (m *Manager) SetAutoBlock(ctx context.Context, a AutoBlock) error {
	if m.Disabled {
		return ErrDisabled
	}
	a.Exempt = slices.DeleteFunc(slices.Clone(a.Exempt), func(s string) bool { return strings.TrimSpace(s) == "" })
	if a.Exempt == nil {
		a.Exempt = []string{}
	}
	for i := range a.Exempt {
		a.Exempt[i] = strings.TrimSpace(a.Exempt[i])
	}
	if err := validateAutoBlock(a); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	cfg := clone(m.active)
	cfg.AutoBlock = a
	if err := m.store.WriteJSON(settingsFile, cfg); err != nil {
		return err
	}
	m.active = cfg
	if m.pending != nil {
		m.pending.AutoBlock = a
	}
	if !a.Enabled {
		m.attempts = map[netip.Prefix][]time.Time{}
	}
	return m.applyLocked(ctx)
}

// Unblock lifts a block by hand.
func (m *Manager) Unblock(ctx context.Context, address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := slices.IndexFunc(m.blocks, func(b Block) bool { return b.Address == address })
	if i < 0 {
		return ErrNotBlocked
	}
	if m.blocks[i].Until.IsZero() {
		m.blocksDirty = true
	}
	m.blocks = slices.Delete(m.blocks, i, i+1)
	m.saveRun()
	m.flushPermanent(m.now(), true)
	return m.applyLocked(ctx)
}

// Failed is told about a failed login from a remote address.
func (m *Manager) Failed(remote, source string) {
	addr, err := netip.ParseAddr(remote)
	if err != nil {
		return
	}
	m.mu.Lock()
	if !m.loaded {
		m.mu.Unlock()
		return
	}
	block, blocked := m.countFailure(addr, source, m.now())
	if blocked {
		slog.Warn("firewall: sender blocked after failed logins", "address", block.Address, "source", source, "attempts", block.Attempts)
		if err := m.applyLocked(context.Background()); err != nil {
			slog.Error("firewall: block not loaded", "err", err)
		}
	}
	m.mu.Unlock()
	if blocked && m.OnBlock != nil {
		m.OnBlock(block)
	}
}

// AllowHTTP tells whether a request may reach the web UI (app false) or
// the HTTPS name of an app (app true). Both share one port, so nftables
// lets the port in when either is allowed and titand decides by name.
func (m *Manager) AllowHTTP(remoteAddr string, app bool) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap().WithZone("")
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg := m.current()
	if !cfg.Enabled || addr.IsLoopback() || (m.pending != nil && addr == m.admin) {
		return true
	}
	service := "webui"
	if app {
		service = "apps"
	}
	return decide(cfg, classify(addr, m.home), addr, "tcp", m.Web.or().HTTPS, service)
}

// Status returns the settings and the state; client is the address of the
// request asking.
func (m *Manager) Status(client string) Status {
	m.mu.Lock()
	status := Status{Config: clone(m.active), Services: Services(&m.Web), Apps: slices.Clone(m.apps), Error: m.lastErr, ClientAddress: client}
	if m.pending != nil {
		pending := clone(*m.pending)
		status.Pending = &pending
		status.PendingSeconds = max(1, int(m.deadline.Sub(m.now()).Seconds()))
	}
	status.Active = !m.Disabled && m.applied != "" && m.tableNeeded() && m.lastErr == ""
	for _, p := range m.home {
		status.HomeNetworks = append(status.HomeNetworks, p.String())
	}
	for _, p := range vpnPrefixes {
		status.VPNNetworks = append(status.VPNNetworks, p.String())
	}
	if addr, err := netip.ParseAddr(client); err == nil {
		status.ClientZone = classify(addr, m.home)
	}
	status.Blocked = slices.Clone(m.blocks)
	m.mu.Unlock()
	if status.Apps == nil {
		status.Apps = []App{}
	}
	sort.Slice(status.Blocked, func(i, j int) bool { return status.Blocked[i].Since.After(status.Blocked[j].Since) })
	if status.Blocked == nil {
		status.Blocked = []Block{}
	}
	m.recentMu.Lock()
	status.Recent = make([]Drop, 0, len(m.recent))
	for i := len(m.recent) - 1; i >= 0; i-- {
		status.Recent = append(status.Recent, m.recent[i])
	}
	m.recentMu.Unlock()
	return status
}

// ClearRecent empties the list of recently dropped packets.
func (m *Manager) ClearRecent() {
	m.recentMu.Lock()
	m.recent = nil
	m.recentMu.Unlock()
}
