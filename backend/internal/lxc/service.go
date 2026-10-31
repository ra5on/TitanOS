package lxc

import (
	"context"
	"errors"
	"log/slog"
)

// ErrServiceOff means LXC is switched off (Settings › Services).
var ErrServiceOff = errors.New("lxc: system containers are switched off")

// Off reports whether system containers are switched off.
func (m *Manager) Off() bool { return m.off.Load() }

// SetOff switches system containers. Off stops the running ones; Run then
// starts none until switched on again. Their data stays on the volume.
func (m *Manager) SetOff(ctx context.Context, off bool, mounted []string) error {
	m.off.Store(off)
	if !off || m.Disabled {
		slog.Info("system containers switched", "off", off)
		return nil
	}
	var errs []error
	for name := range m.RunningNames() {
		if err := m.Action(ctx, name, "stop", mounted); err != nil && !errors.Is(err, ErrNotRunning) {
			errs = append(errs, err)
		}
	}
	slog.Info("system containers switched off")
	return errors.Join(errs...)
}
