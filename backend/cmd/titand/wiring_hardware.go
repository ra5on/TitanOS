package main

import (
	"log/slog"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/ups"
)

// wireHardware wires the UPS, graphics, energy, the power schedule and the memory budget.
func (w *titand) wireHardware() error {
	ctx := w.ctx
	// The UPS: on battery the NAS shuts down cleanly before it is empty.
	w.supply = ups.NewManager(w.store)
	w.supply.Disabled = w.opts.dev
	w.supply.Poweroff = w.upsPower.Poweroff
	w.supply.OnEvent = func(kind, subject string) {
		severity := notify.Warning
		switch kind {
		case "ups_online":
			severity = notify.Info
		case "ups_low_battery", "ups_shutdown":
			severity = notify.Error
		}
		w.notifier.Notify(severity, kind, subject)
	}
	if err := w.supply.Load(); err != nil {
		slog.Warn("UPS settings not readable from /config", "err", err)
	}
	w.background(func() { w.supply.Run(ctx) })

	// The license texts lie in the image; in development mode in the repository.
	w.shareDir = "/usr/share/titanos"
	if w.opts.dev {
		w.shareDir = "."
	}
	w.nvidia = hardware.NewNvidiaControl(w.store, w.devices, w.opts.dev)
	if err := w.nvidia.Load(); err != nil {
		slog.Warn("NVIDIA settings unavailable", "err", err)
	}
	w.background(func() { w.nvidia.Run(ctx) })
	// GuC/HuC of Intel graphics (Gen9 to Gen11): a kernel parameter in the
	// boot entries, written only when the setting changes.
	w.intelGuc = hardware.NewIntelGuc(w.store, w.devices, w.updates, w.opts.dev || w.updates.Disabled)
	if err := w.intelGuc.Load(); err != nil {
		slog.Warn("Intel GuC/HuC setting unavailable", "err", err)
	}
	w.background(func() { w.intelGuc.Run(ctx) })
	// usb-storage instead of uas for USB bridges with known trouble: only
	// on the owner's click on the hardware page.
	w.uasQuirks = hardware.NewUASQuirks(w.store, w.updates, w.opts.dev || w.updates.Disabled)
	if err := w.uasQuirks.Load(); err != nil {
		slog.Warn("USB quirks unavailable", "err", err)
	}
	w.background(func() { w.uasQuirks.Run(ctx) })
	w.powerButton = system.NewPowerButtonManager(w.store)
	w.powerButton.Disabled = w.opts.dev
	if err := w.powerButton.Load(); err != nil {
		slog.Warn("power button settings unavailable", "err", err)
	}
	w.energy = system.NewEnergyManager(w.store)
	w.energy.Disabled = w.opts.dev
	if err := w.energy.Load(); err != nil {
		slog.Warn("energy settings unavailable", "err", err)
	}
	// Shutting down and waking on a schedule, and Wake-on-LAN.
	w.schedule = system.NewPowerScheduler(w.store)
	w.schedule.Timezone = func() string { return w.settings.CurrentTimezone() }
	w.schedule.Disabled = w.opts.dev
	w.schedule.Poweroff = w.unattended("poweroff", "power schedule", w.power.Poweroff)
	w.schedule.Notify = w.notifier.Notify
	if err := w.schedule.Load(); err != nil {
		slog.Warn("power schedule unavailable", "err", err)
	}
	// A VM is not started on its schedule shortly before the NAS shuts down.
	w.machines.PowerOffSoon = func() bool { return w.schedule.ShutdownSoon(15 * time.Minute) }
	w.machines.Notify = w.notifier.Notify
	w.monitor = system.NewMonitor(ctx)
	w.memoryPolicy = system.NewMemoryPolicy(w.store, system.ReadMemory)
	w.memoryPolicy.Disabled = w.opts.dev
	if err := w.memoryPolicy.Load(); err != nil {
		slog.Warn("memory budget unavailable", "err", err)
	}
	w.background(func() { w.memoryPolicy.Run(ctx) })
	return nil
}
