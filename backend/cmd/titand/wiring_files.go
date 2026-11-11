package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/fileindex"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/firewall"
	"github.com/ra5on/TitanOS/backend/internal/monitoring"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/sshd"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/thumbs"
)

// wireFiles wires files, the full-text index, monitoring, the activity log and the firewall hooks.
func (w *titand) wireFiles() error {
	ctx := w.ctx
	w.fileManager = files.NewManager(shares.UsersGID)
	w.fileManager.Protected = w.machines.ProtectFile
	if !w.opts.dev {
		w.fileManager.Mounted = w.volumes.Mounted
		w.fileManager.MountRoot = "/mnt"
	}
	w.fileManager.Acquire = w.memoryPolicy.Acquire
	w.fileManager.Available = func(root files.Root) bool { return fileRootAvailable(root, w.volumes.Mounted(), w.drives.Mounted()) }
	if !w.opts.dev {
		w.background(func() { w.fileManager.RunUploadSweep(ctx, w.volumes.Mounted) })
	}
	// Thumbnails are made on request and cached on the volume of the file.
	w.previews = thumbs.New()
	if !w.opts.dev {
		w.previews.MountRoot, w.previews.Mounted = "/mnt", w.volumes.Mounted
	}
	// The full-text index is opt-in and runs only while a volume is in use.
	w.fullText = fileindex.New(w.store)
	w.fullText.Disabled = w.opts.dev
	w.fullText.MountRoot, w.fullText.Mounted = "/mnt", w.volumes.Mounted
	w.fullText.Protected = w.machines.ProtectFile
	w.fullText.Timezone = w.settings.CurrentTimezone
	w.fullText.ShareVolume = func(name string) (string, bool) {
		for _, share := range w.folders.List() {
			if share.Name == name {
				return share.Volume, true
			}
		}
		return "", false
	}
	if err := w.fullText.Load(); err != nil {
		slog.Warn("file index settings unavailable", "err", err)
	}
	w.background(func() { w.fullText.Run(ctx) })
	w.history = monitoring.New(w.store)
	w.history.Mounted = w.volumes.Mounted
	w.history.Snapshot = func() (system.Info, storage.Status) {
		sampleCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		status, err := w.volumes.Status(sampleCtx)
		if err != nil {
			return w.monitor.Info(), storage.Status{}
		}
		return w.monitor.Info(), status
	}
	if err := w.history.Load(); err != nil {
		slog.Warn("monitoring history settings unavailable", "err", err)
	}
	if !w.opts.dev {
		w.background(func() { w.history.Run(ctx) })
	}
	// The activity log stays in memory in development mode.
	w.audit = activity.New(w.store)
	if err := w.audit.Load(); err != nil {
		slog.Warn("activity log settings unavailable", "err", err)
	}
	if !w.opts.dev {
		w.audit.Mounted = w.volumes.Mounted
		w.audit.DefaultVolume = func() string { return w.systemData.Volume("activity") }
		w.background(func() { w.audit.Run(ctx) })
	}
	// SSH logins with the key they came in with, from the journal of sshd.
	w.background(func() {
		w.access.FollowLogins(ctx, w.users.List, func(login sshd.Login) {
			action := "ssh_login"
			if login.Blocked {
				action = "ssh_login_blocked"
			}
			w.audit.Record(activity.Entry{At: login.At, User: login.User, Via: activity.ViaSSH, IP: login.IP, Action: action, Target: login.Target(), OK: !login.Blocked})
		})
	})
	// The firewall learns the published ports of Docker apps and reports
	// automatic blocks and changes that went back.
	w.packetFilter.Apps = func(ctx context.Context) ([]firewall.App, bool) {
		status := w.containers.Status(ctx)
		if !status.Running {
			return nil, false
		}
		return firewallApps(status.Containers), true
	}
	// The networks own firewall zones are made of: logical networks of
	// Settings › Network, Docker networks, VM/LXC NAT bridges. While Docker
	// does not answer, its last known networks stay.
	w.packetFilter.Networks = func(ctx context.Context) ([]firewall.Network, bool) {
		if list, err := w.containers.Networks(ctx); err == nil {
			w.dockerNetworks = list
		}
		return firewallNetworks(w.connection, w.dockerNetworks), true
	}
	// Devices of the WireGuard server have private addresses but come in
	// over the VPN interface: the firewall counts them as VPN.
	w.packetFilter.VPNNetworks = func() []string {
		if w.vpnServer == nil {
			return nil
		}
		if s := w.vpnServer.Settings(); s.Enabled && s.Network != "" {
			return []string{s.Network}
		}
		return nil
	}
	if !w.opts.dev {
		w.packetFilter.RouteDevice = firewall.KernelRoute()
	}
	w.packetFilter.OnBlock = func(block firewall.Block) {
		w.notifier.Notify(notify.Warning, "firewall_blocked", block.Address)
		w.audit.Record(activity.Entry{Via: activity.ViaSystem, IP: block.Address, Action: "firewall_block", Target: block.Source, OK: true})
	}
	w.packetFilter.OnRevert = func() {
		w.notifier.Notify(notify.Warning, "firewall_reverted", "")
		w.audit.Record(activity.Entry{Via: activity.ViaSystem, Action: "firewall_reverted", OK: true})
	}
	w.packetFilter.OnFailure = func(err error) {
		w.notifier.Notify(notify.Error, "firewall_failed", "")
	}
	w.background(func() { w.packetFilter.Run(ctx) })
	w.background(func() { w.packetFilter.FollowJournal(ctx) })
	w.machines.Activity = func(action, target string, ok bool) {
		w.audit.Record(activity.Entry{Via: activity.ViaSystem, Action: action, Target: target, OK: ok})
	}
	return nil
}
