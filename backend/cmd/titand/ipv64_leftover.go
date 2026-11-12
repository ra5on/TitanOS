package main

import (
	"context"
	"log/slog"
	"os"

	command "github.com/ra5on/TitanOS/backend/internal/run"
)

// ipv64TunnelInterface is the WireGuard tunnel into a Cloud Router of
// IPv64.net that versions up to 0.76 could set up.
const ipv64TunnelInterface = "ipv64wg"

func interfaceExists(iface string) bool {
	_, err := os.Stat("/sys/class/net/" + iface)
	return err == nil
}

// removeLeftoverIPv64Tunnel takes down the tunnel an older version of the
// app left running: an app update restarts titand, not the NAS, and
// nothing manages the interface any more (PLAN 79-2). Once at the start,
// logged; the settings in /config/ipv64/ stay for a way back.
func removeLeftoverIPv64Tunnel(ctx context.Context, runner command.Runner, exists func(string) bool) {
	if !exists(ipv64TunnelInterface) {
		return
	}
	if _, err := runner.Run(ctx, "", "ip", "link", "del", "dev", ipv64TunnelInterface); err != nil {
		slog.Warn("tunnel to the IPv64 Cloud Router of an older version not removed", "interface", ipv64TunnelInterface, "err", err)
		return
	}
	slog.Info("tunnel to the IPv64 Cloud Router of an older version removed; the Cloud Router is no longer part of TitanOS", "interface", ipv64TunnelInterface)
}
