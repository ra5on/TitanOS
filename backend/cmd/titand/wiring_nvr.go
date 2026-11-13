package main

import (
	"context"
	"log/slog"

	"github.com/ra5on/TitanOS/backend/internal/configbackup"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/nvr"
	command "github.com/ra5on/TitanOS/backend/internal/run"
	"github.com/ra5on/TitanOS/backend/internal/webpush"
)

// wireNVR wires the network video recorder (PLAN 79-7, docs/NVR.md):
// Frigate in a container of its own that the NVR manages through Docker.
// It is off until the owner switches it on.
//
// The container follows the volumes like Docker does: a volume that is
// being unmounted or locked no longer counts as mounted, and the NVR
// removes its container within one look (nvr.Manager.Tick). A restart or
// shutdown of the NAS stops Docker's engine and with it Frigate (which
// gets its 30 seconds); an app update of titand leaves it running, and
// the new titand takes it over.
//
// In development mode there is no Docker: the NVR runs on a stand-in
// engine and a stand-in for Frigate's API with four demo cameras
// (nvr/dev.go), so its pages can be built and tested.
func (w *titand) wireNVR() error {
	ctx := w.ctx
	w.recorder = nvr.New(w.store)
	w.recorder.Access.Lookup = func(name string) (nvr.Account, bool) {
		user, ok := w.users.Get(name)
		if !ok {
			return nvr.Account{}, false
		}
		return nvr.Account{Name: user.Name, Admin: user.Role.CanAdminister(), Disabled: user.Disabled, Created: user.Created}, true
	}
	if err := w.recorder.Access.Load(); err != nil {
		slog.Warn("camera rights not readable from /config; they are not changed until a start reads them", "err", err)
	}
	if w.opts.dev {
		if err := w.recorder.StartDev(ctx); err != nil {
			slog.Warn("NVR stand-in of development mode not started", "err", err)
			return nil
		}
	} else {
		w.recorder.Engine = w.containers
		w.recorder.Mounted = w.volumes.Mounted
		w.recorder.Sleeps = w.volumes.SleepingVolumes
		w.recorder.Asleep = w.volumes.VolumeAsleep
		w.recorder.Devices = w.devices.ContainerDevices
		w.recorder.Leased = w.containers.LeasedHardware
		w.recorder.DockerEnabled = func() bool { return w.containers.Current().Enabled }
		w.recorder.DockerVolume = func() string { return w.containers.Current().Volume }
		// The same switch as on the Docker page; its engine starts with the
		// next look of the Docker manager.
		w.recorder.EnableDocker = func(volume string) error {
			return w.containers.SetSettings(docker.Settings{Enabled: true, Volume: volume}, w.volumes.Names())
		}
		w.recorder.KernelLog = func(ctx context.Context) string {
			// The journal of the kernel is in RAM: no disk, no stick.
			out, _ := command.Exec{}.Run(ctx, "", "journalctl", "-k", "-b", "0", "-o", "cat", "--no-pager", "-n", "20000") // without the journal the diagnosis shows no firmware lines
			return out
		}
	}
	w.recorder.Notify = w.notifier.Notify
	w.recorder.Timezone = w.settings.CurrentTimezone
	// Messages about a camera go to the accounts chosen at the camera, each
	// its own ways (PLAN 82-3); a camera of 0.78 without such a list still
	// goes its old ways to the administrators. Home Assistant reads its
	// sensors from the NVR (api/homeassistant_mqtt.go).
	w.recorder.Route = func(n nvr.Notice) {
		if n.Legacy {
			w.notifier.NotifyVia(n.Severity, n.Message, n.Subject, notify.Ways{Bell: n.Titan, Push: n.Push, Email: n.Email, Agent: n.Agent, Ref: n.Camera})
			return
		}
		targets := make([]notify.Target, 0, len(n.Targets))
		for _, t := range n.Targets {
			targets = append(targets, notify.Target{User: t.User, Bell: t.Bell, Push: t.Push, Email: t.Email})
		}
		w.notifier.NotifyTo(n.Severity, n.Message, n.Subject, targets, n.Agent, n.Camera)
	}
	// A camera's push goes to the account's own devices that take camera
	// messages; tapping it opens the NVR.
	w.notifier.PushTo = func(user, ref string, message notify.Message, title string) {
		w.pushes.SendCamera(user, webpush.Message{Title: title, Body: message.Text, Tag: message.Kind + ":" + ref, URL: "/nvr"})
	}
	if err := w.recorder.Load(); err != nil {
		slog.Warn("NVR settings not readable from /config", "err", err)
	}
	// The faces the NVR knows go into the backup of /config (they live on
	// the NVR's volume; sleeping disks are not woken for it).
	if w.stickBackup != nil {
		w.stickBackup.Extra = func() []configbackup.ExtraFile {
			var out []configbackup.ExtraFile
			for _, f := range w.recorder.BackupFiles() {
				out = append(out, configbackup.ExtraFile{Name: f.Name, Data: f.Data})
			}
			return out
		}
	}
	w.background(func() { w.recorder.Run(ctx) })
	return nil
}
