package shares

import (
	"context"
	"time"
)

// Time limits for the commands of the loop that keeps shares, accounts and
// services in line (1.03): a command that hangs (smbd that does not answer
// smbcontrol, an NFS export that blocks) no longer stops that loop, and
// with it every later change of shares, for good. Chosen generously; not
// measured on real hardware. Variables for tests.
var (
	// commandLimit bounds accounts (getent, useradd, pdbedit, smbpasswd,
	// userdel), smbstatus, smbcontrol, kill, exportfs and bind mounts.
	commandLimit = 2 * time.Minute
	// serviceLimit bounds systemctl: a restart waits for the stop of the
	// service, which systemd itself gives 90 seconds.
	serviceLimit = 3 * time.Minute
)

// run runs one command of the loop with its time limit.
func (m *Manager) run(ctx context.Context, name string, args ...string) (string, error) {
	limit := commandLimit
	if name == "systemctl" {
		limit = serviceLimit
	}
	cctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	return m.runner.Run(cctx, "", name, args...)
}
