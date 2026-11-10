// Package startup decides when Docker apps and VMs start after the NAS
// booted (PLAN 73, owner's report of 8.11.2026: the Home Assistant VM did not
// find its backup share after a restart). Apps and VMs wait until TitanOS is
// ready – the volumes settled and the shares served over SMB/NFS – plus an
// extra wait the owner chooses, and then start in the owner's order with a
// wait after each entry.
//
// Settings: /config/system/startup.json, read once and kept in RAM, written
// only when changed. Older titand never read the file.
package startup

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const settingsFile = "system/startup.json"

// giveUp: apps and VMs start at the latest this long after the volumes settled.
const giveUp = 5 * time.Minute

// MaxWait bounds every wait (seconds).
const MaxWait = 600

// Entry is one app or VM in the start order.
type Entry struct {
	// Kind is "vm" or "app" (a Docker app, i.e. a Compose project).
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Wait after starting this entry before the next one, in seconds.
	Wait int `json:"wait"`
	// Off: not started after boot (only for apps; VMs have their own autostart switch).
	Off bool `json:"off,omitempty"`
}

// Settings of the start after boot.
type Settings struct {
	// NoWait starts apps and VMs as before, without waiting for the shares.
	NoWait bool `json:"noWait,omitempty"`
	// Extra wait after TitanOS is ready, in seconds.
	Extra int     `json:"extra"`
	Order []Entry `json:"order"`
}

// Validate checks the settings strictly.
func (s *Settings) Validate() error {
	if s.Extra < 0 || s.Extra > MaxWait {
		return errors.New("extra wait out of range")
	}
	if len(s.Order) > 200 {
		return errors.New("too many entries")
	}
	seen := map[string]bool{}
	for _, e := range s.Order {
		if e.Kind != "vm" && e.Kind != "app" {
			return errors.New("unknown kind")
		}
		if e.Name == "" || len(e.Name) > 128 {
			return errors.New("invalid name")
		}
		if e.Wait < 0 || e.Wait > MaxWait {
			return errors.New("wait out of range")
		}
		key := e.Kind + "/" + e.Name
		if seen[key] {
			return errors.New("entry twice")
		}
		seen[key] = true
	}
	if s.Order == nil {
		s.Order = []Entry{}
	}
	return nil
}

// Manager keeps the settings and opens the gate once TitanOS is ready.
type Manager struct {
	store *config.Store
	// Settled tells whether the volumes were assembled and mounted.
	Settled func() bool
	// SharesApplied is when the shares last matched the configuration.
	SharesApplied func() time.Time
	now           func() time.Time

	mu       sync.Mutex
	loaded   bool
	settings Settings
	// readyAt is when the gate first saw TitanOS ready (zero until then).
	readyAt   time.Time
	settledAt time.Time
}

func NewManager(store *config.Store) *Manager {
	return &Manager{store: store, now: time.Now}
}

// Settings returns the current settings (read from the stick once).
func (m *Manager) Settings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadLocked()
	out := m.settings
	out.Order = slices.Clone(m.settings.Order)
	if out.Order == nil {
		out.Order = []Entry{}
	}
	return out
}

func (m *Manager) loadLocked() {
	if m.loaded || m.store == nil {
		return
	}
	var s Settings
	if err := m.store.ReadJSON(settingsFile, &s); err == nil && s.Validate() == nil {
		m.settings = s
	}
	m.loaded = true
}

// Save stores new settings.
func (m *Manager) Save(s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store != nil {
		if err := m.store.WriteJSON(settingsFile, s); err != nil {
			return err
		}
	}
	m.settings = s
	m.loaded = true
	return nil
}

// Ready tells whether apps and VMs may start: the volumes settled, the
// shares applied after that, and the extra wait passed. Once open it stays
// open until titand ends (a share changed later does not stop apps).
func (m *Manager) Ready() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadLocked()
	if m.settings.NoWait {
		return true
	}
	now := m.now()
	if m.readyAt.IsZero() {
		if m.Settled == nil || !m.Settled() {
			return false
		}
		if m.settledAt.IsZero() {
			m.settledAt = now
		}
		// The shares are applied every few seconds; if that keeps failing,
		// apps and VMs start anyway after giveUp rather than never.
		if m.SharesApplied != nil && now.Sub(m.settledAt) < giveUp {
			applied := m.SharesApplied()
			if applied.IsZero() || applied.Before(m.settledAt) {
				return false
			}
		}
		m.readyAt = now
	}
	return now.Sub(m.readyAt) >= time.Duration(m.settings.Extra)*time.Second
}

// ReadySince is when TitanOS was first seen ready (zero until then).
func (m *Manager) ReadySince() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.readyAt
}

// Policy is a held restart policy of one container (docker.HeldPolicy).
type Policy struct {
	Name       string `json:"name"`
	MaxRetries int    `json:"maxRetries,omitempty"`
}

const heldFile = "system/startup-held.json"

// Hold stores the restart policies taken away before the NAS went down.
func (m *Manager) Hold(held map[string]Policy) error {
	if m.store == nil || len(held) == 0 {
		return nil
	}
	return m.store.WriteJSON(heldFile, held)
}

// ClearHeld forgets the held policies once they were given back.
func (m *Manager) ClearHeld() {
	if m.store != nil {
		_ = m.store.Remove(heldFile)
	}
}

// Held returns the stored policies (empty when none).
func (m *Manager) Held() map[string]Policy {
	held := map[string]Policy{}
	if m.store != nil {
		_ = m.store.ReadJSON(heldFile, &held)
	}
	return held
}

// Apps are the Docker apps in the order (also those switched off).
func (m *Manager) Apps() []string {
	var out []string
	for _, e := range m.Settings().Order {
		if e.Kind == "app" {
			out = append(out, e.Name)
		}
	}
	return out
}

// Ordered tells whether a VM is started by the order (not by its own autostart).
func (m *Manager) Ordered(kind, name string) bool {
	for _, e := range m.Settings().Order {
		if e.Kind == kind && e.Name == name {
			return true
		}
	}
	return false
}

// Steps carries out the start; set by titand.
type Steps struct {
	// EngineReady: Docker runs (or is off); apps start only then.
	EngineReady func() bool
	StartVM     func(ctx context.Context, name string) error
	StartApp    func(ctx context.Context, name string) error
	Restore     func(ctx context.Context, held map[string]Policy) error
	// DoneFile marks that the order ran in this boot (in /run, gone after a restart).
	DoneFile string
}

// Run waits until TitanOS is ready, starts the order once per boot with the
// waits between the entries, and then gives the held restart policies back.
func (m *Manager) Run(ctx context.Context, steps Steps) {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	// Read once: the note of the held policies is on the stick, and this
	// loop turns every two seconds until the NAS is ready.
	held := len(m.Held()) > 0
	for {
		hasApps := held || len(m.Apps()) > 0
		if m.Ready() && (!hasApps || steps.EngineReady == nil || steps.EngineReady()) {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
	if _, err := os.Stat(steps.DoneFile); err != nil || steps.DoneFile == "" {
		for _, e := range m.Settings().Order {
			if e.Off {
				continue
			}
			var err error
			switch {
			case e.Kind == "vm" && steps.StartVM != nil:
				err = steps.StartVM(ctx, e.Name)
			case e.Kind == "app" && steps.StartApp != nil:
				err = steps.StartApp(ctx, e.Name)
			}
			if err != nil {
				slog.Warn("start after boot", "kind", e.Kind, "name", e.Name, "err", err)
			} else {
				slog.Info("started after boot", "kind", e.Kind, "name", e.Name, "wait", e.Wait)
			}
			if e.Wait > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Duration(e.Wait) * time.Second):
				}
			}
		}
		if steps.DoneFile != "" {
			_ = os.MkdirAll(filepath.Dir(steps.DoneFile), 0o755)
			_ = os.WriteFile(steps.DoneFile, nil, 0o644)
		}
	}
	if held := m.Held(); len(held) > 0 && steps.Restore != nil {
		if err := steps.Restore(ctx, held); err != nil {
			slog.Warn("restart policies not given back", "err", err)
			return
		}
		m.ClearHeld()
	}
}
