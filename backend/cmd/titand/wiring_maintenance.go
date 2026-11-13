package main

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/fans"
	"github.com/ra5on/TitanOS/backend/internal/healthreport"
	"github.com/ra5on/TitanOS/backend/internal/maintenance"
	"github.com/ra5on/TitanOS/backend/internal/media"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/scripts"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/version"
)

// wireMaintenance wires the health report, fans and maintenance jobs.
func (w *titand) wireMaintenance() error {
	ctx := w.ctx
	// A summary of the NAS, weekly or monthly, off unless switched on.
	w.report = healthreport.New(w.store)
	w.report.Timezone = w.settings.CurrentTimezone
	w.report.Language = func() string { general, _ := w.notifier.Settings(); return general.Language }
	w.reportSince.Store(time.Now().UnixNano())
	w.report.Collect = func() healthreport.Data {
		d := healthreport.Data{Version: version.Version, MemoryPercent: -1, Since: time.Unix(0, w.reportSince.Load())}
		info := w.monitor.Info()
		d.Uptime = time.Duration(info.UptimeSeconds) * time.Second
		d.CPUWatts = info.CPU.PowerWatts
		if mem := w.monitor.Memory(); mem.TotalBytes > 0 {
			d.MemoryPercent = int((mem.TotalBytes - mem.AvailableBytes) * 100 / mem.TotalBytes)
		}
		check, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if status, err := w.volumes.Status(check); err == nil {
			for _, v := range status.Volumes {
				d.Volumes = append(d.Volumes, healthreport.Volume{Name: v.Name, State: v.State, Mounted: v.Mounted, Locked: v.Encryption != nil && v.Encryption.Locked, Size: v.SizeBytes, Used: v.UsedBytes})
			}
			for _, disk := range status.Disks {
				x := healthreport.Disk{Name: disk.Name, Model: disk.Model, Volume: disk.Volume, Warnings: len(disk.SmartWarnings)}
				if disk.Smart != nil {
					healthy := disk.Smart.Healthy
					x.Healthy, x.TempC = &healthy, disk.Smart.TemperatureC
				}
				d.Disks = append(d.Disks, x)
			}
		}
		for _, view := range w.backups.CachedViews() {
			d.Backups = append(d.Backups, healthreport.Backup{Name: view.Job.ID, Enabled: view.Job.Enabled, LastOK: view.State.LastOK, LastError: view.State.LastError, Overdue: w.backups.Overdue(view)})
		}
		if available := w.updates.Status().Available; available != nil {
			d.UpdateAvailable = available.Version
		}
		if u := w.supply.Status(); u.Mode != "" && u.Mode != "none" {
			d.UPS = &healthreport.UPS{Connected: u.Connected, OnBattery: u.OnBattery, Charge: u.Charge}
		}
		for _, message := range w.notifier.Messages() {
			// Messages for some accounts only (camera recipients) are not
			// the NAS's warnings.
			if message.Time.Before(d.Since) || len(message.To) > 0 {
				continue
			}
			switch message.Severity {
			case notify.Error:
				d.Errors++
			case notify.Warning:
				d.Warnings++
			}
		}
		return d
	}
	w.report.Send = func(title, text, status string) error {
		if err := w.notifier.SendReport(title, text); err != nil {
			return err
		}
		w.reportSince.Store(time.Now().UnixNano())
		return nil
	}
	if err := w.report.Load(); err != nil {
		slog.Warn("health report settings not readable from /config", "err", err)
	}
	w.background(func() { w.report.Run(ctx) })
	// Fans: left to the BIOS unless a fan gets a speed or a curve.
	w.coolers = fans.New(w.store)
	w.coolers.Disabled = w.opts.dev
	w.coolers.Temperatures = func() map[string]float64 {
		out := map[string]float64{}
		for _, sensor := range w.monitor.Info().Temperatures {
			if sensor.Celsius != nil {
				out[sensor.ID] = *sensor.Celsius
			}
		}
		return out
	}
	w.coolers.DiskMax = func() (float64, bool) {
		c, ok := w.volumes.HottestDisk()
		return float64(c), ok
	}
	if err := w.coolers.Load(); err != nil {
		slog.Warn("fan settings not readable from /config", "err", err)
	}
	w.background(func() { w.coolers.Run(ctx) })
	// Fans that stopped, temperatures near their critical value, ECC errors.
	hardware := notify.NewHardwareWatcher(w.notifier)
	hardware.Fans = func() []notify.FanReading {
		var list []notify.FanReading
		for _, f := range w.coolers.Fans() {
			list = append(list, notify.FanReading{ID: f.ID, Label: f.Label, RPM: f.RPM, Percent: f.Percent})
		}
		return list
	}
	hardware.Temperatures = func() []notify.TempReading {
		var list []notify.TempReading
		for _, t := range system.ReadTemperatures() {
			list = append(list, notify.TempReading{ID: t.ID, Label: t.Label, Channel: t.Channel, Device: t.Device, Chip: t.Chip, Celsius: t.Celsius, Critical: t.CriticalC})
		}
		return list
	}
	// PLAN 75-9: shut down in order when a reliable sensor stays too hot
	// (Settings › Temperatures, off by default). Never in development mode:
	// there the watcher does not run at all.
	hardware.Poweroff = w.unattended("poweroff", "heat", w.power.Poweroff)
	if !w.opts.dev {
		w.background(func() { hardware.Run(ctx) })
	}
	w.upkeep.Disabled = w.opts.dev
	w.mediaImports = media.New(ctx, w.volumes.Mounted, shares.UsersGID)
	w.mediaImports.Disabled = w.opts.dev
	w.upkeep.Mounted = w.volumes.Mounted
	w.upkeep.ExternalBusy = func(job maintenance.Job) bool {
		if w.appBackups.Busy() || w.containers.Busy() || w.backups.Busy() || w.volumes.TestsRunning() || w.volumes.SnapshotRestoresRunning() || w.volumes.GrowthPending() || w.volumes.Reshaping() || w.replicas.Busy() || w.machines.Busy() || w.systemContainers.Busy() || w.fileManager.Busy() || w.extensions.Busy() || w.mediaImports.Working() || w.folders.PurgeBusy() {
			return true
		}
		check, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		status, err := w.volumes.Status(check)
		if err != nil {
			return true
		}
		return slices.ContainsFunc(status.Volumes, func(v storage.Volume) bool {
			return v.State == storage.VolumeRebuilding || v.State == storage.VolumeSyncing || v.State == storage.VolumeChecking
		})
	}
	w.upkeep.Conflict = func(job maintenance.Job) string {
		// The callback must not call upkeep.Status: admission already holds its lock.
		if w.backups.DueSoon(time.Now(), 5*time.Minute) {
			return "backup-priority"
		}
		return ""
	}
	w.upkeep.OnResult = func(result maintenance.Result) {
		if result.Error != "" {
			w.notifier.Notify(notify.Error, "maintenance_failed", result.Name)
		}
	}
	w.upkeep.Execute = func(work context.Context, job maintenance.Job) error {
		switch job.Kind {
		case "raid-check":
			return w.volumes.MaintenanceCheck(work, job.Target)
		case "smart-short":
			return w.volumes.ShortSMART(work, job.Target)
		case "smart-long":
			return w.volumes.LongSMART(work, job.Target)
		case "backup-check", "restore-probe":
			return w.backups.MaintenanceRun(work, job.Target, job.Kind, job.ReadData, backup.ProbeRequest{MaxBytes: job.MaxBytes, MaxFiles: job.MaxFiles})
		case "snapshot":
			if err := w.volumes.ScheduledSnapshot(work, job.Target, job.ID, job.Keep, job.LockDays, job.Versions); err != nil {
				return err
			}
			w.volumes.SyncShadowCopies(work, w.folders.PreviousVersionVolumes())
			return nil
		}
		return errors.New("unknown maintenance kind")
	}
	if err := w.upkeep.Load(); err != nil {
		slog.Warn("maintenance settings unavailable", "err", err)
	}
	w.background(func() { w.upkeep.Run(ctx) })

	// Own scripts (Tools › Scripts): on a data volume, run by hand, on a
	// schedule, at start once the volume is mounted and at shutdown.
	w.userScripts = scripts.New(w.store)
	w.userScripts.Disabled = w.opts.dev
	w.userScripts.Mounted = w.volumes.Mounted
	w.userScripts.DefaultVolume = w.extensions.Volume
	w.userScripts.Timezone = w.settings.CurrentTimezone
	w.userScripts.Notify = func(severity, kind, subject string) { w.notifier.Notify(severity, kind, subject) }
	if err := w.userScripts.Load(); err != nil {
		slog.Warn("script settings unavailable", "err", err)
	}
	w.background(func() { w.userScripts.Loop(ctx) })

	// An app update restarts titand: it waits for work that would break off.
	w.updates.Working = func() bool {
		return w.upkeep.Busy() || w.appBackups.Busy() || w.volumes.ActionsRunning() || w.volumes.SnapshotRestoresRunning() || w.volumes.GrowthPending() || w.volumes.Reshaping() || w.replicas.Busy() || w.fileManager.Busy() || w.backups.Busy() || w.volumes.TestsRunning() || w.machines.Busy() || w.systemContainers.Busy() || w.extensions.Busy() || w.mediaImports.Working() || w.folders.PurgeBusy() ||
			// docker compose runs below titand's unit and would die with it,
			// leaving an app stopped half way through pull, up or removal.
			w.containers.Busy() ||
			// A run of the KI-Agent would end as "interrupted".
			(w.handler != nil && w.handler.AgentRunning())
	}
	// Settings › Services: what was switched off stays off, before anything
	// is started automatically.
	var services struct{ VMsOff, LXCOff bool }
	if err := w.store.ReadJSON("system/services.json", &services); err == nil {
		if services.VMsOff {
			_ = w.machines.SetOff(ctx, true) // best effort at start; the switch is applied again when changed
		}
		if services.LXCOff {
			_ = w.systemContainers.SetOff(ctx, true, nil) // best effort at start; the switch is applied again when changed
		}
	}
	w.background(func() { w.machines.Run(ctx, w.volumes.Mounted) })
	w.background(func() { w.systemContainers.Run(ctx, w.volumes.Mounted) })
	w.background(func() { w.updates.Run(ctx) })
	return nil
}
