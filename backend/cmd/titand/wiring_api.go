package main

import (
	"context"
	"github.com/ra5on/TitanOS/backend/internal/procs"
	"log/slog"

	"github.com/ra5on/TitanOS/backend/internal/api"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/diagnose"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
	"github.com/ra5on/TitanOS/backend/internal/version"
	"github.com/ra5on/TitanOS/backend/internal/webui"
)

// wireAPI wires sessions and the API with everything it serves.
func (w *titand) wireAPI() error {
	ctx := w.ctx
	// Logins outlive a restart of titand in RAM, as after an app update.
	w.sessions = auth.NewSessions()
	if !w.opts.dev {
		if err := w.sessions.Persist("/run/titanos/sessions.json"); err != nil {
			slog.Warn("logins of before not taken over", "err", err)
		}
		// With "stay logged in" on, logins also outlive a restart of the NAS.
		if err := w.sessions.Remember(w.store); err != nil {
			slog.Warn("remembered logins not taken over", "err", err)
		}
	}
	w.diagnostics = diagnose.NewManager(w.store, version.Version)
	w.diagnostics.SMART = func(ctx context.Context) any { return w.volumes.SmartDumps(ctx) }
	w.diagnostics.IntelGPU = w.intelGuc.Report
	w.handler = api.New(api.Options{
		Media:              w.mediaImports,
		UI:                 webui.FS(),
		Store:              w.store,
		Users:              w.users,
		Monitor:            w.monitor,
		Monitoring:         w.history,
		Maintenance:        w.upkeep,
		Scripts:            w.userScripts,
		Processes:          processSampler(w.opts.dev),
		ApplicationBackups: w.appBackups,
		Storage:            w.volumes,
		SmartHistory:       w.smartHistory,
		Shares:             w.folders,
		Startup:            w.start,
		Files:              w.fileManager,
		Thumbs:             w.previews,
		FileIndex:          w.fullText,
		Docker:             w.containers,
		AppProxy:           w.routes,
		AppAccess:          w.appRights,
		Catalog:            w.appCatalog,
		Terminal:           w.shells,
		Viewer:             w.remoteView,
		Webapps:            w.webApps,
		VMs:                w.machines,
		LXC:                w.systemContainers,
		Hardware:           w.devices,
		Nvidia:             w.nvidia,
		IntelGuc:           w.intelGuc,
		UASQuirks:          w.uasQuirks,
		Accelerators:       hardware.NewAcceleratorMonitor(ctx, w.devices),
		Plugins:            w.extensions,
		Tailscale:          w.tailnet,
		Netbird:            w.mesh,
		Network:            w.connection,
		Firewall:           w.packetFilter,
		Notify:             w.notifier,
		WebPush:            w.pushes,
		WireGuard:          w.vpnServer,
		HealthReport:       w.report,
		Fans:               w.coolers,
		HomeAssistant:      w.home,
		HomeAssistants:     w.homes,
		ConfigBackup:       w.stickBackup,
		USB:                w.drives,
		BackupTarget:       w.backupTarget,
		Replication:        w.replicas,
		ReplicaReceiver:    w.replicaReceiver,
		Diagnose:           w.diagnostics,
		LogKeep:            w.logs,
		HWCheck:            w.hardwareCheck(),
		SSH:                w.access,
		Update:             w.updates,
		Backup:             w.backups,
		UPS:                w.supply,
		Certificate:        w.certs,
		LetsEncrypt:        w.acmeCerts,
		DDNS:               w.dynamicDNS,
		Settings:           w.settings,
		Energy:             w.energy,
		PowerSchedule:      w.schedule,
		PowerButton:        w.powerButton,
		MemoryPolicy:       w.memoryPolicy,
		Power:              w.power,
		Shutdown:           w.ending,
		Version:            version.Version,
		ShareDir:           w.shareDir,
		SecureCookies:      !w.opts.dev,
		Sessions:           w.sessions,
		Activity:           w.audit,
		Cluster:            w.nodes,
	})
	if err := w.handler.Start(ctx); err != nil {
		slog.Warn("API settings not read", "err", err)
	}
	// The jobs of the API end with the daemon; waitServices bounds the wait.
	w.background(func() {
		<-ctx.Done()
		_ = w.handler.Close() // cleanup, best effort: nothing more can be done if it fails
	})

	// Old Craft apps give way to the T-apps by themselves (PLAN 75-20).
	w.background(func() { w.handler.RunCraftReplacement(ctx) })
	// Watches that Home Assistant keeps putting its backups on the NAS.
	w.background(func() { w.handler.RunHAStorage(ctx) })
	// Leftovers of the AI chat removed in 0.41.0, once.
	w.background(func() { w.handler.RemoveRetiredAssistant(ctx) })
	// The KI-Agent: interrupted runs, recurring tasks, triggers.
	w.background(func() { w.handler.AgentScheduler(ctx) })
	w.background(func() { w.logs.Run(ctx) })
	if !w.opts.dev {
		w.background(func() { w.stick.Follow(ctx) })
	}
	w.background(func() {
		w.homes.Run(ctx, func(ctx context.Context, m *homeassistant.Manager) {
			m.RunMQTT(ctx, w.handler.HASnapshot, w.handler.HACommand)
		})
	})
	w.schedule.Busy = w.handler.PowerBusy
	w.background(func() { w.schedule.Run(ctx) })
	return nil
}

// processSampler reads /proc for the task manager; in development mode
// it runs on someone's own computer, so it sends no signals there.
func processSampler(dev bool) *procs.Sampler {
	p := procs.New()
	p.Disabled = dev
	return p
}
