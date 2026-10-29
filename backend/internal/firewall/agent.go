package firewall

import (
	"context"
	"errors"
	"net/netip"
)

// Changes the assistant proposes. It has no web session whose address
// could be kept in, so the checks are of their own: the switch stays where
// it is (on and off only in the web UI), and whoever in the home network or
// the VPN reached the web UI without a rule of their own still does. A
// change of a switched-on firewall goes on trial like every change; only a
// person in the web UI confirms it.

var (
	// ErrSwitchManual means a proposal would switch the firewall on or off.
	ErrSwitchManual = errors.New("firewall: switching the firewall on or off is only done in the web UI")
	// ErrClosesWebUI means a proposal would close the web UI for the home
	// network or the VPN.
	ErrClosesWebUI = errors.New("firewall: the change would close the web UI for the home network or the VPN")
)

// CheckProposal checks settings the assistant wants to propose against the
// settings in force now, without changing anything.
func (m *Manager) CheckProposal(cfg Config) error {
	if m.Disabled {
		return ErrDisabled
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.proposalLocked(cfg)
	return err
}

// ChangeFromAssistant applies settings the assistant proposed and a person
// (or the direct mode) approved, after the same checks as CheckProposal.
func (m *Manager) ChangeFromAssistant(ctx context.Context, cfg Config) error {
	if m.Disabled {
		return ErrDisabled
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.proposalLocked(cfg)
	if err != nil {
		return err
	}
	// No address is let in on trial: the person confirming in the web UI
	// must reach it with the new rules, or they go back by themselves.
	return m.changeLocked(ctx, cfg, netip.Addr{})
}

func (m *Manager) proposalLocked(cfg Config) (Config, error) {
	if !m.loaded {
		return cfg, ErrNotLoaded
	}
	if m.pending != nil {
		return cfg, ErrPending
	}
	cfg.AutoBlock = m.active.AutoBlock
	cfg = normal(clone(cfg))
	if err := validate(cfg); err != nil {
		return cfg, err
	}
	if cfg.Enabled != m.active.Enabled {
		return cfg, ErrSwitchManual
	}
	services, port := Services(&m.Web), m.Web.or().HTTPS
	for _, zone := range []Zone{Home, VPN} {
		// The zero address matches no policy with a sender of its own.
		if decide(m.active, services, zone, netip.Addr{}, "tcp", port, "webui") && !decide(cfg, services, zone, netip.Addr{}, "tcp", port, "webui") {
			return cfg, ErrClosesWebUI
		}
	}
	return cfg, nil
}
