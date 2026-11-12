package main

import (
	"log/slog"

	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/shares"
)

// wireShares wires shares and the HTTPS names of apps.
func (w *titand) wireShares() error {
	ctx := w.ctx
	// Keeps Linux and Samba accounts, smb.conf, the NFS exports and the
	// services in line with accounts, shares and mounted volumes.
	w.folders = shares.NewManager(w.store)
	w.folders.Disabled = w.opts.dev
	// Recycle bins on sleeping disks are emptied once the disks are awake.
	w.folders.Asleep = w.volumes.VolumeAsleep
	w.folders.Shadow = w.volumes.ShadowDir
	if err := w.folders.Load(); err != nil {
		slog.Warn("shares not readable from /config", "err", err)
	}
	// HTTPS names of Docker apps, served by titand itself (see below) and
	// published over mDNS; avahi then runs even without SMB shares.
	w.routes = appproxy.NewManager(w.store)
	w.routes.Disabled = w.opts.dev
	w.routes.HTTPSPort, w.routes.Reserved = proxyPorts(w.opts)
	w.routes.OnChange = w.folders.Kick
	w.routes.Gate = w.packetFilter.AllowHTTP
	if err := w.routes.Load(); err != nil {
		slog.Warn("app HTTPS names not readable from /config", "err", err)
	}
	w.background(func() { w.routes.Run(ctx) })

	w.background(func() {
		w.folders.Run(ctx, func() shares.State {
			return shares.State{Users: w.users.List(), Mounted: w.volumes.Mounted(), Quotas: w.volumes.ShareQuotas, Discovery: w.routes.Publishing()}
		})
	})
	return nil
}
