package system

import (
	"log/slog"
	"os/exec"
)

// Power shuts the machine down or restarts it.
type Power interface {
	Reboot() error
	Poweroff() error
}

// SystemdPower asks systemd, which stops all services cleanly first.
type SystemdPower struct{}

func (SystemdPower) Reboot() error   { return exec.Command("systemctl", "reboot").Run() }
func (SystemdPower) Poweroff() error { return exec.Command("systemctl", "poweroff").Run() }

// NoPower only logs. It is used in development, where titand runs on the
// developer's own machine.
type NoPower struct{}

func (NoPower) Reboot() error   { slog.Info("development mode: reboot not carried out"); return nil }
func (NoPower) Poweroff() error { slog.Info("development mode: poweroff not carried out"); return nil }
