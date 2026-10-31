package vm

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// ErrServiceOff means the VM service is switched off (Settings › Services).
var ErrServiceOff = errors.New("vm: the VM service is switched off")

// libvirtUnits are the daemons and sockets of libvirt. Missing ones are
// left out by systemctl with an error that does not matter here.
var libvirtUnits = []string{
	"virtqemud.socket", "virtqemud-ro.socket", "virtqemud-admin.socket", "virtqemud.service",
	"virtlogd.socket", "virtlogd-admin.socket", "virtlogd.service",
	"virtlockd.socket", "virtlockd-admin.socket", "virtlockd.service",
	"libvirtd.socket", "libvirtd-ro.socket", "libvirtd-admin.socket", "libvirtd.service",
}

// Off reports whether the VM service is switched off.
func (m *Manager) Off() bool { return m.off.Load() }

// SetOff switches the VM service. Off shuts the running VMs down (and
// pulls the plug after three minutes), then stops libvirt and keeps it
// from being started until the service is switched on again. The disks of
// the VMs are never touched.
func (m *Manager) SetOff(ctx context.Context, off bool) error {
	if m.Disabled {
		m.off.Store(off)
		return nil
	}
	if !off {
		m.off.Store(false)
		_, _ = m.runner.Run(ctx, "", "systemctl", append([]string{"unmask", "--runtime"}, libvirtUnits...)...) // units that were not masked need no unmask; starting below reports errors
		_, err := m.runner.Run(ctx, "", "systemctl", "start", "virtqemud.socket", "virtlogd.socket", "virtlockd.socket")
		slog.Info("VM service switched on")
		return err
	}
	m.off.Store(true)
	for name := range m.RunningNames() {
		if err := m.Action(ctx, name, "shutdown"); err != nil && !errors.Is(err, ErrNotRunning) {
			slog.Warn("VM not asked to shut down", "vm", name, "err", err)
		}
	}
	deadline := time.Now().Add(3 * time.Minute)
	for len(m.RunningNames()) > 0 && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	for name := range m.RunningNames() {
		slog.Warn("VM did not shut down in time, switching it off", "vm", name)
		_ = m.Action(ctx, name, "poweroff") // last resort while switching off; nothing more can be done
	}
	_, _ = m.runner.Run(ctx, "", "systemctl", append([]string{"stop"}, libvirtUnits...)...) // cleanup, best effort: nothing more can be done if it fails
	_, err := m.runner.Run(ctx, "", "systemctl", append([]string{"mask", "--runtime"}, libvirtUnits...)...)
	slog.Info("VM service switched off")
	return err
}
