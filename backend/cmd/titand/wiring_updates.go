package main

import (
	"context"
	"log/slog"
	"maps"
	"slices"

	"github.com/ra5on/TitanOS/backend/internal/app"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	command "github.com/ra5on/TitanOS/backend/internal/run"
	"github.com/ra5on/TitanOS/backend/internal/shutdown"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/update"
	"github.com/ra5on/TitanOS/backend/internal/version"
)

// wireUpdates wires power, the orderly shutdown and updates.
func (w *titand) wireUpdates() error {
	ctx := w.ctx
	w.power = system.SystemdPower{}
	if w.opts.dev {
		w.power = system.NoPower{}
	}
	// A shutdown after a power failure (UPS) is no planned one: the
	// healthcheck of IPv64 keeps watching then.
	w.upsPower = w.power
	// Planned shutdowns and restarts first pause the healthcheck of IPv64
	// (dynamic DNS, set further down), so that it raises no alarm.
	w.power = announcedPower{Power: w.power, before: func() {
		if w.beforePowerOff != nil {
			w.beforePowerOff()
		}
	}}
	// Planned restarts and shutdowns first end the VMs, LXC containers and
	// Docker apps, with time for each and visible in the web UI. The UPS
	// keeps the fast way of systemd (upsPower).
	w.ending = shutdown.New(shutdown.Workloads{
		VMs: func() []string { return slices.Collect(maps.Keys(w.machines.RunningNames())) },
		StopVM: func(ctx context.Context, name string) error {
			return w.machines.Action(ctx, name, "shutdown")
		},
		StartVM: func(ctx context.Context, name string) error {
			return w.machines.Start(ctx, name, w.volumes.Mounted())
		},
		LXC: func() []string { return slices.Collect(maps.Keys(w.systemContainers.RunningNames())) },
		StopLXC: func(ctx context.Context, name string) error {
			return w.systemContainers.Action(ctx, name, "stop", w.volumes.Mounted())
		},
		StartLXC: func(ctx context.Context, name string) error {
			return w.systemContainers.Start(ctx, name, w.volumes.Mounted())
		},
		Containers:  w.containers.RunningContainers,
		StopDocker:  w.containers.StopEngine,
		StartDocker: w.containers.StartEngine,
		// The person's shutdown scripts, before the guests end (Tools ›
		// Scripts); w.userScripts is wired further down.
		Scripts: func(ctx context.Context, progress func(name, state string, err error)) {
			if w.userScripts != nil {
				w.userScripts.RunShutdown(ctx, progress)
			}
		},
	})
	// Nobody watches these: after the waiting time they go on by themselves.
	w.unattended = func(action, by string, run func() error) func() error {
		return func() error { return w.ending.Begin(action, by, true, run) }
	}

	// New versions go into the other slot of the stick and start on trial.
	w.slot = system.BootSlot()
	w.updates = update.NewManager(w.store, version.Version, w.slot)
	w.updates.Disabled = w.opts.dev || w.slot == ""
	// Debian's kernel or the newer one of trixie-backports (docs/KERNEL.md).
	w.updates.Variant = update.ReadVariant(update.VariantFile)
	// An encrypted volume waits for its password after the switch to a new
	// system: it must not make the boot check fail (and the update roll
	// back) just because nobody entered it within five minutes. It counts
	// as back, and so do plugins on it.
	w.updates.Mounted = func() []string { return append(w.volumes.Mounted(), w.volumes.WaitingForPassword(ctx)...) }
	// Going back to a system before encryption: it cannot open them, so
	// its boot check must not wait for them (see update.Pending.Volumes).
	w.updates.Encrypted = w.volumes.Encrypted
	w.updates.PluginsHealthy = func() bool {
		return w.extensions.Healthy() || slices.Contains(w.volumes.WaitingForPassword(ctx), w.extensions.Volume())
	}
	w.updates.PreparePlugins = func(ctx context.Context, version, variant, kernel, kernelPackage, baseVersion string, assets map[string]string, fetch func(context.Context, string, int64) ([]byte, error), download func(context.Context, string, string, string, int64) error) error {
		return w.extensions.PrepareUpdate(ctx, version, variant, kernel, kernelPackage, baseVersion, assets, w.volumes.Mounted(), fetch, download)
	}
	w.updates.Reboot = w.unattended("reboot", "update", w.power.Reboot)
	if !w.opts.dev && w.slot != "" && w.base != "" {
		w.updates.Base, w.updates.Apps = w.base, app.New("/efi", w.slot)
		// Both at once: the check waits for the new titand. From a unit of
		// its own, which the restart of titand does not take down with it.
		w.updates.Restart = func() error {
			_, err := (command.Exec{}).Run(context.Background(), "", "systemd-run", "--no-block", "--collect", "--quiet",
				"--description=TitanOS restart for an app update", "systemctl", "restart", "titand.service", "titanos-app-check.service")
			return err
		}
	}
	w.updates.OnEvent = func(kind, subject string) {
		severity := notify.Info
		if kind == "update_failed" {
			severity = notify.Error
		}
		w.notifier.Notify(severity, kind, subject)
	}
	// Started by GRUB (legacy BIOS, 32-bit UEFI): its try count becomes
	// part of the entry names before anything looks at them.
	w.updates.ReconcileBootLoader()
	if err := w.updates.Load(); err != nil {
		slog.Warn("update settings not readable from /config", "err", err)
	}
	w.updates.Confirm()
	w.extensions.ChangeAllowed = func() bool {
		status := w.updates.Status()
		return !w.tailnet.Pending() && !w.mesh.Pending() && status.Pending == nil && status.Progress.Phase != "download" && status.Progress.Phase != "switch"
	}
	w.extensions.UpdateAllowed = func() bool { return !w.tailnet.Pending() && !w.mesh.Pending() }
	w.extensions.CleanupProtected = w.updates.PluginReleaseProtection
	w.background(func() { w.extensions.Run(ctx, w.volumes.Mounted) })
	w.background(func() { w.containers.Run(ctx, w.volumes.Mounted) })
	return nil
}
