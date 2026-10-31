package system

import (
	"context"
	"log/slog"
	"os/exec"
	"time"
)

// Power shuts the machine down or restarts it.
type Power interface {
	Reboot() error
	Poweroff() error
}

// SystemdPower asks systemd, which stops all services cleanly first.
type SystemdPower struct{}

// powerTimeout bounds the call to systemctl. It only queues the job and
// returns; a systemd that does not answer must not hold the caller forever.
const powerTimeout = time.Minute

func (SystemdPower) Reboot() error   { return systemctlPower("reboot") }
func (SystemdPower) Poweroff() error { return systemctlPower("poweroff") }

func systemctlPower(verb string) error {
	ctx, cancel := context.WithTimeout(context.Background(), powerTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "systemctl", verb).Run()
}

// NoPower only logs. It is used in development, where titand runs on the
// developer's own machine.
type NoPower struct{}

func (NoPower) Reboot() error   { slog.Info("development mode: reboot not carried out"); return nil }
func (NoPower) Poweroff() error { slog.Info("development mode: poweroff not carried out"); return nil }
