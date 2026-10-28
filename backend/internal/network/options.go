package network

import (
	"errors"
	"io/fs"
)

// optionsFile holds settings about the network page itself, not about the
// network: older titand do not know it and simply ignore it.
const optionsFile = "network/options.json"

// ErrVLANsInUse refuses to hide VLANs while a network still uses one.
var ErrVLANsInUse = errors.New("network: VLANs are in use")

// Options are NAS-wide choices of the network page.
type Options struct {
	// VLANs offers VLAN IDs in the network editor and VLAN networks in the
	// firewall. Off by default: VLANs need a router or switch that
	// understands them. A configuration that already uses one counts as on.
	VLANs bool `json:"vlans"`
}

// loadOptions reads the options once at the start (the stick is not read
// again for them).
func (m *Manager) loadOptions() {
	var opts Options
	if err := m.store.ReadJSON(optionsFile, &opts); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return
	}
	m.options = opts
}

// usesVLAN says whether a configuration tags any traffic.
func usesVLAN(cfg Config) bool {
	if cfg.VLAN > 0 {
		return true
	}
	for _, n := range cfg.Networks {
		if n.VLAN > 0 {
			return true
		}
	}
	return false
}

// vlansInUseLocked looks at the active and the pending configuration. m.mu is held.
func (m *Manager) vlansInUseLocked() bool {
	return usesVLAN(m.active) || (m.pending != nil && usesVLAN(*m.pending))
}

// SetOptions saves the options. VLANs cannot be switched off while a
// network uses one; they have to be removed first.
func (m *Manager) SetOptions(opts Options) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !opts.VLANs && m.vlansInUseLocked() {
		return ErrVLANsInUse
	}
	if opts == m.options {
		return nil
	}
	if err := m.store.WriteJSON(optionsFile, opts); err != nil {
		return err
	}
	m.options = opts
	return nil
}
