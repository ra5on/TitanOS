package main

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
	"github.com/ra5on/TitanOS/backend/internal/logkeep"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	command "github.com/ra5on/TitanOS/backend/internal/run"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/stickwatch"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/terminal"
	"github.com/ra5on/TitanOS/backend/internal/webpush"
)

// wireNotifications wires the terminal, notifications, Web Push, Home Assistant and what volumes report.
func (w *titand) wireNotifications() error {
	ctx := w.ctx
	// The root shell of the terminal panel.
	w.shells = terminal.NewManager(w.store)
	w.shells.Disabled = w.opts.dev
	if err := w.shells.Load(); err != nil {
		slog.Warn("terminal settings not readable from /config", "err", err)
	}

	// Tells in the web UI, by mail and by push message when a volume or a
	// disk needs attention.
	w.notifier = notify.NewManager(w.store)
	// The journal beyond a restart, on the first volume; an earlier start
	// that did not end cleanly becomes a notification.
	w.logs = logkeep.New(command.Exec{})
	w.logs.Target = func() (string, bool) {
		mounted := w.volumes.Mounted()
		if len(mounted) == 0 {
			return "", false
		}
		slices.Sort(mounted)
		return w.volumes.MountPoint(mounted[0]), true
	}
	w.logs.OnUnclean = func(u logkeep.Unclean) {
		when := u.Started.Local().Format("02.01.2006 15:04")
		if !u.LastLine.IsZero() {
			when = u.LastLine.Local().Format("02.01.2006 15:04")
		}
		w.notifier.Notify(notify.Warning, "unclean_shutdown", when)
	}
	w.notifier.Urgent = w.logs.Urgent
	// A failing boot stick: kernel errors and failed writes to /config.
	w.stick = stickwatch.New()
	w.stick.Report = func(string) { w.notifier.Notify(notify.Error, "stick_error", "TITANOS") }
	if !w.opts.dev {
		w.store.OnWriteError = w.stick.Failed
	}
	// Only owners and administrators may point their own channels into the
	// home network; a plain user's go to public addresses only.
	w.notifier.Confined = func(name string) bool {
		user, ok := w.users.Get(name)
		return !ok || !user.Role.CanAdminister()
	}
	if err := w.notifier.Load(); err != nil {
		slog.Warn("notification settings not readable from /config", "err", err)
	}
	// The daily summary of the ways out, at the time in the NAS time zone.
	w.background(func() {
		w.notifier.RunDigest(ctx, func() *time.Location {
			if zone, err := time.LoadLocation(w.settings.CurrentTimezone()); err == nil {
				return zone
			}
			return time.Local
		})
	})
	// Notifications on phones and browsers (Web Push): what the bell shows,
	// to owners and administrators who subscribed a device.
	w.pushes = webpush.New(w.store)
	w.pushes.Allowed = func(name string) bool {
		user, ok := w.users.Get(name)
		return ok && user.Role.CanAdminister() && !user.Disabled
	}
	if err := w.pushes.Load(); err != nil {
		slog.Warn("web push settings not readable from /config", "err", err)
	}
	w.notifier.Push = func(message notify.Message, title string) {
		w.pushes.Broadcast(webpush.Message{Title: title, Body: message.Text, Tag: message.Kind + ":" + message.Subject, URL: "/", Urgent: message.Severity == notify.Error})
	}
	// Once per plugin and kind of device; installing stays a click.
	w.extensions.Notify = func(title string) { w.notifier.Notify(notify.Info, "plugin_recommended", title) }
	w.extensions.DriverBuilt = func(version string, err error) {
		if err != nil {
			w.notifier.Notify(notify.Warning, "nvidia_build_failed", version)
			return
		}
		w.notifier.Notify(notify.Info, "nvidia_built", version)
	}
	// Home Assistant: MCP clients switch entities released to them, and
	// messages can go to the Home Assistant app.
	w.home = homeassistant.NewManager(w.store)
	if err := w.home.Load(); err != nil {
		slog.Warn("Home Assistant settings not readable from /config", "err", err)
	}
	// Further connections (PLAN §45); the first one stays w.home.
	w.homes = homeassistant.NewHub(w.store, w.home)
	if err := w.homes.Load(); err != nil {
		slog.Warn("further Home Assistant connections not readable from /config", "err", err)
	}
	w.notifier.SetHomeAssistant(w.homes)
	w.background(func() { w.homes.Run(ctx, func(ctx context.Context, m *homeassistant.Manager) { m.Run(ctx) }) })
	w.background(func() { notify.NewWatcher(w.notifier).Run(ctx, w.volumes.Status) })
	w.volumes.OnSnapshotRestoreDone = func(job storage.SnapshotFolderJob) {
		switch job.State {
		case storage.RestoreDone:
			w.notifier.Notify(notify.Info, "snapshot_restore_done", job.Volume+"/"+job.Target)
		case storage.RestoreFailed:
			w.notifier.Notify(notify.Error, "snapshot_restore_failed", job.Volume+"/"+job.Path)
		}
	}
	w.volumes.OnTestDone = func(test storage.DiskTest) {
		disk := test.Disk
		if test.Serial != "" {
			disk += " (" + test.Model + ", " + test.Serial + ")"
		}
		switch test.Result {
		case storage.TestPassed:
			w.notifier.Notify(notify.Info, "disk_test_passed", disk)
		case storage.TestFailed:
			w.notifier.Notify(notify.Error, "disk_test_failed", disk)
		}
	}
	// A test that a restart stopped and that waits to be continued.
	// Ransomware guard: a share that looks attacked becomes read-only, and
	// a Btrfs volume gets a snapshot of what was still intact.
	w.background(func() {
		w.folders.FollowAudit(ctx, func(share shares.Share) {
			w.notifier.Notify(notify.Error, "share_guard_locked", share.Name)
			if w.volumes.FilesystemOf(share.Volume) == "btrfs" {
				name := "guard-" + time.Now().UTC().Format("20060102T150405")
				if err := w.volumes.SnapshotAction(context.WithoutCancel(ctx), share.Volume, name, "create", ""); err != nil {
					slog.Error("snapshot after ransomware lock failed", "volume", share.Volume, "err", err)
				}
			}
		})
	})
	// Previous versions for Windows follow the snapshots.
	w.background(func() {
		for {
			w.volumes.SyncShadowCopies(ctx, w.folders.PreviousVersionVolumes())
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Minute):
			}
		}
	})
	// Encrypted volumes: notice after a start, stopping and restarting
	// what runs on them around locking (encryption.go).
	wireEncryption(w.volumes, w.folders, w.machines, w.systemContainers, w.notifier)
	w.volumes.OnCacheMissing = func(volume string) { w.notifier.Notify(notify.Error, "volume_cache_missing", volume) }
	w.volumes.OnCacheRemoved = func(volume string) { w.notifier.Notify(notify.Info, "volume_cache_removed", volume) }
	w.background(func() { w.volumes.RunCacheCleaner(ctx) })
	w.volumes.OnReshaped = func(volume string, err error) {
		if err != nil {
			w.notifier.Notify(notify.Error, "volume_disk_remove_failed", volume)
			return
		}
		w.notifier.Notify(notify.Info, "volume_disk_removed", volume)
	}
	w.volumes.OnTestInterrupted = func(test storage.DiskTest) {
		disk := test.Disk
		if test.Serial != "" {
			disk += " (" + test.Model + ", " + test.Serial + ")"
		}
		w.notifier.Notify(notify.Warning, "disk_test_waiting", disk)
	}
	return nil
}
