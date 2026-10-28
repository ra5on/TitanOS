package ddns

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/netip"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
)

// The healthcheck of IPv64.net ("Erreichbarkeit überwachen"): titand pings
// a push healthcheck every interval, and IPv64 raises its alarm through
// the channels chosen there when the pings stop. Off unless switched on;
// nothing is created at IPv64 before. With the API key of the account the
// check is paused before a planned shutdown or restart and resumed after
// the next start, so that intended downtime gives no alarm.

const (
	// DefaultBeat is the ping interval in minutes; IPv64 allows 72 pings a
	// day by default, so 20 minutes is the least.
	DefaultBeat = 30
	minBeat     = 20
	// pausedFile remembers across the restart that TitanOS paused the check.
	pausedFile = "ddns/heartbeat-paused"
)

// Heartbeat are the settings of the pings.
type Heartbeat struct {
	Enabled bool `json:"enabled"`
	// Token is the secret of the healthcheck; never shown.
	Token string `json:"token,omitempty"`
	// Check is the name of the healthcheck at IPv64, to show.
	Check string `json:"check,omitempty"`
	// Interval in minutes; 0 is DefaultBeat.
	Interval int `json:"interval,omitempty"`
	// AutoPause pauses the check before planned shutdowns (API key needed).
	AutoPause bool `json:"autoPause,omitempty"`
}

// HeartbeatStatus is what the page shows about the pings.
type HeartbeatStatus struct {
	LastPing  *time.Time `json:"lastPing,omitempty"`
	LastError string     `json:"lastError,omitempty"`
	NextPing  *time.Time `json:"nextPing,omitempty"`
	// Paused says TitanOS paused the check for a shutdown.
	Paused bool `json:"paused,omitempty"`
}

type beatState struct {
	lastPing  time.Time
	lastError string
	next      time.Time
	paused    bool
	kick      chan struct{}
}

func normalizeHeartbeat(s *Settings) error {
	h := &s.Heartbeat
	h.Token = strings.TrimSpace(h.Token)
	h.Check = strings.TrimSpace(h.Check)
	if len(h.Token) > 200 || strings.ContainsAny(h.Token, " \r\n\t") || len(h.Check) > 100 {
		return invalid("healthcheck")
	}
	if h.Interval != 0 && (h.Interval < minBeat || h.Interval > 1440) {
		return invalid("ping interval (%d–1440 minutes)", minBeat)
	}
	if h.Enabled && h.Token == "" {
		return invalid("no healthcheck chosen")
	}
	return nil
}

func (h Heartbeat) interval() time.Duration {
	if h.Interval == 0 {
		return DefaultBeat * time.Minute
	}
	return time.Duration(h.Interval) * time.Minute
}

func (m *Manager) pokeBeat() {
	select {
	case m.beat.kick <- struct{}{}:
	default:
	}
}

// IPv64Key returns the API key of IPv64 stored for dynamic DNS, if any.
func (m *Manager) IPv64Key() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.settings.Provider.Type != dnsprovider.IPv64Net {
		return ""
	}
	return m.settings.Provider.Token
}

// HeartbeatToken returns the token of the chosen healthcheck, if any.
func (m *Manager) HeartbeatToken() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings.Heartbeat.Token
}

func (m *Manager) ipv64(key string) *dnsprovider.IPv64 {
	return dnsprovider.NewIPv64(key, "", m.IPv64Options)
}

// SetHeartbeat switches the pings on for a healthcheck (created or chosen
// at IPv64 by the caller), keeping interval and the pause setting.
func (m *Manager) SetHeartbeat(token, name string, interval int, autoPause bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	s := m.settings
	s.Heartbeat = Heartbeat{Enabled: true, Token: token, Check: name, Interval: interval, AutoPause: autoPause}
	if err := normalizeHeartbeat(&s); err != nil {
		return err
	}
	if err := m.store.WriteJSON(settingsFile, s); err != nil {
		return err
	}
	m.settings = s
	m.beat.lastError = ""
	m.pokeBeat()
	return nil
}

// ReplaceTokens follows a new token made at IPv64 (rotate-token): a domain
// update token or a healthcheck token that this NAS keeps is swapped for
// the new one. It says whether anything was kept and changed.
func (m *Manager) ReplaceTokens(oldUpdate, newUpdate, oldCheck, newCheck string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return false, ErrNotLoaded
	}
	s := m.settings
	changed := false
	if oldUpdate != "" && newUpdate != "" && s.Provider.Type == dnsprovider.IPv64Net && s.Provider.UpdateToken == oldUpdate {
		s.Provider.UpdateToken, changed = newUpdate, true
	}
	if oldCheck != "" && newCheck != "" && s.Heartbeat.Token == oldCheck {
		s.Heartbeat.Token, changed = newCheck, true
	}
	if !changed {
		return false, nil
	}
	if err := m.store.WriteJSON(settingsFile, s); err != nil {
		return false, err
	}
	m.settings = s
	m.beat.lastError = ""
	m.pokeBeat()
	return true, nil
}

// Addresses are the public addresses found at the last look, if any.
func (m *Manager) Addresses() (netip.Addr, netip.Addr) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current4, m.current6
}

// StopHeartbeat ends the pings; forget also drops the healthcheck (after
// the caller deleted it at IPv64).
func (m *Manager) StopHeartbeat(forget bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	s := m.settings
	s.Heartbeat.Enabled = false
	if forget {
		s.Heartbeat.Token, s.Heartbeat.Check = "", ""
	}
	if err := m.store.WriteJSON(settingsFile, s); err != nil {
		return err
	}
	m.settings = s
	m.beat.next = time.Time{}
	m.pokeBeat()
	return nil
}

func (m *Manager) beatStatus() HeartbeatStatus {
	stamp := func(t time.Time) *time.Time {
		if t.IsZero() {
			return nil
		}
		return &t
	}
	st := HeartbeatStatus{LastPing: stamp(m.beat.lastPing), LastError: m.beat.lastError, Paused: m.beat.paused}
	if m.settings.Heartbeat.Enabled && !m.Disabled {
		st.NextPing = stamp(m.beat.next)
	}
	return st
}

// runHeartbeat pings while the heartbeat is switched on.
func (m *Manager) runHeartbeat(ctx context.Context) {
	for {
		m.mu.Lock()
		h := m.settings.Heartbeat
		on := m.loaded && h.Enabled && h.Token != "" && !m.Disabled
		resume := m.beat.paused
		key := ""
		if m.settings.Provider.Type == dnsprovider.IPv64Net {
			key = m.settings.Provider.Token
		}
		m.mu.Unlock()
		wait := time.Hour
		if on {
			if resume && key != "" {
				m.resume(ctx, key, h.Token)
			}
			err := m.ipv64("").Ping(ctx, h.Token)
			now := m.now()
			m.mu.Lock()
			if err == nil {
				m.beat.lastPing, m.beat.lastError = now, ""
			} else {
				m.beat.lastError = err.Error()
				slog.Warn("healthcheck of IPv64 not pinged", "err", err)
			}
			wait = h.interval()
			var apiErr *dnsprovider.APIError
			if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
				wait = max(min(wait, 10*time.Minute), apiErr.RetryAfter)
			}
			m.beat.next = now.Add(wait)
			m.mu.Unlock()
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-m.beat.kick:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (m *Manager) resume(ctx context.Context, key, token string) {
	if err := m.ipv64(key).ResumeHealthcheck(ctx, token); err != nil {
		slog.Warn("healthcheck of IPv64 not resumed", "err", err)
		return
	}
	m.mu.Lock()
	m.beat.paused = false
	m.mu.Unlock()
	if err := m.store.Remove(pausedFile); err != nil {
		slog.Warn("note of the paused healthcheck not removed", "err", err)
	}
	slog.Info("healthcheck of IPv64 resumed after the start")
}

// PauseForShutdown pauses the healthcheck before a planned shutdown or
// restart, if that is wanted and the API key is there. It never holds the
// shutdown up for long.
func (m *Manager) PauseForShutdown(ctx context.Context) {
	m.mu.Lock()
	h := m.settings.Heartbeat
	key := ""
	if m.settings.Provider.Type == dnsprovider.IPv64Net {
		key = m.settings.Provider.Token
	}
	skip := !h.Enabled || !h.AutoPause || h.Token == "" || key == "" || m.Disabled
	m.mu.Unlock()
	if skip {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := m.ipv64(key).PauseHealthcheck(ctx, h.Token); err != nil {
		slog.Warn("healthcheck of IPv64 not paused before the shutdown", "err", err)
		return
	}
	if err := m.store.Write(pausedFile, []byte("paused by TitanOS for a planned shutdown\n")); err != nil {
		slog.Warn("note of the paused healthcheck not saved", "err", err)
	}
	m.mu.Lock()
	m.beat.paused = true
	m.mu.Unlock()
	slog.Info("healthcheck of IPv64 paused for the shutdown")
}

// loadPaused reads whether TitanOS paused the check before the last stop.
func (m *Manager) loadPaused() {
	if _, err := m.store.Read(pausedFile); err == nil {
		m.beat.paused = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("note of the paused healthcheck not readable", "err", err)
	}
}

// learnLimit asks IPv64 once for the daily update limit of the plan, so
// that the updater keeps well below it.
func (m *Manager) learnLimit(ctx context.Context, s Settings) {
	m.mu.Lock()
	known := m.limitKnown
	m.limitKnown = true
	m.mu.Unlock()
	if known || s.Provider.Type != dnsprovider.IPv64Net || s.Provider.Token == "" {
		return
	}
	account, err := m.ipv64(s.Provider.Token).Account(ctx)
	if err != nil {
		slog.Warn("limits of the IPv64 account not read", "err", err)
		return
	}
	if usage, ok := account.Usage["dyndns_updates_24h"]; ok && usage.Limit != nil {
		dnsprovider.SetIPv64DailyLimit(*usage.Limit)
	}
}
