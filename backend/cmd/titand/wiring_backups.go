package main

import (
	"context"
	"log/slog"
	"os"
	"slices"

	"github.com/ra5on/TitanOS/backend/internal/appbackup"
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/backuptarget"
	"github.com/ra5on/TitanOS/backend/internal/cluster"
	"github.com/ra5on/TitanOS/backend/internal/maintenance"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/replication"
	command "github.com/ra5on/TitanOS/backend/internal/run"
	"github.com/ra5on/TitanOS/backend/internal/usb"
	"github.com/ra5on/TitanOS/backend/internal/version"
	"github.com/ra5on/TitanOS/backend/internal/vm"
)

// wireBackups wires backups, the cluster, replication and unmanaged drives.
func (w *titand) wireBackups() error {
	ctx := w.ctx
	// Backups of shares to another volume, another server or the cloud.
	w.upkeep = maintenance.New(w.store)
	w.machines.ScheduleAdmission = w.upkeep.Enter
	w.backups = backup.NewManager(w.store)
	w.backups.ScheduledStart = func(id string) error {
		leave, err := w.upkeep.Enter()
		if err != nil {
			return backup.ErrRunning
		}
		defer leave()
		return w.backups.Start(id)
	}
	w.backups.Disabled = w.opts.dev
	w.backups.Sources = func() []backup.Source {
		mounted := w.volumes.Mounted()
		sources := []backup.Source{}
		for _, share := range w.folders.List() {
			if slices.Contains(mounted, share.Volume) {
				sources = append(sources, backup.Source{Name: share.Name, Volume: share.Volume, Path: w.folders.Path(share)})
			}
		}
		return sources
	}
	w.backups.OnResult = func(job string, err error) {
		if err != nil {
			w.notifier.Notify(notify.Error, "backup_failed", job)
		}
	}
	w.backups.OnFreed = func(job string, versions int) {
		w.notifier.Notify(notify.Warning, "backup_space_freed", job)
	}
	w.backups.OnRecovered = func(job string) {
		w.notifier.Notify(notify.Info, "backup_recovered", job)
	}
	w.backups.OnCheck = func(job string, err error) {
		if err != nil {
			w.notifier.Notify(notify.Error, "backup_check_failed", job)
		}
	}
	w.backups.OnProbe = func(job string, err error) {
		if err != nil {
			w.notifier.Notify(notify.Error, "backup_probe_failed", job)
		}
	}
	w.backups.OnOverdue = func(job string) {
		w.notifier.Notify(notify.Warning, "backup_overdue", job)
	}
	w.backups.OnIncomplete = func(job string) {
		w.notifier.Notify(notify.Warning, "backup_incomplete", job)
	}
	// Disks of running VMs are read from a Btrfs snapshot made for the run
	// (PLAN 79-9); on other file systems they are read as they are and
	// reported if they changed meanwhile.
	w.backups.LiveDisks = func() map[string]string {
		live := map[string]string{}
		for _, disk := range w.machines.FileDisks() {
			if disk.Running {
				live[disk.Path] = disk.VM
			}
		}
		return live
	}
	// Without Btrfs and with the job's leave: a libvirt snapshot of the VM,
	// or a pause for raw images (vm/diskbackup.go).
	w.backups.VMDiskFiles = func() map[string]string {
		files := map[string]string{}
		for _, disk := range w.machines.FileDisks() {
			files[disk.Path] = disk.VM
		}
		return files
	}
	w.backups.VMRunning = w.machines.Running
	w.backups.BeginVMDisks = func(ctx context.Context, name string, opts backup.VMDiskOptions) (backup.VMDiskSession, error) {
		session, err := w.machines.BeginDiskBackup(ctx, name, vm.DiskBackupOptions{Snapshot: opts.Snapshot, Pause: opts.Pause})
		if err != nil {
			return nil, err
		}
		return session, nil
	}
	w.backups.RecoverVMDisks = func(ctx context.Context) error { return w.machines.RecoverDiskBackups(ctx, true) }
	w.backups.SnapshotSource = w.volumes.BackupSnapshot
	w.backups.CleanSnapshots = w.volumes.CleanBackupSnapshots
	w.backups.Timezone = w.settings.CurrentTimezone
	w.appBackups = &appbackup.Manager{Store: w.store, Docker: w.containers, VMs: w.machines, Mounted: w.volumes.Mounted, MountRoot: "/mnt", Disabled: w.opts.dev}
	if err := w.appBackups.Load(); err != nil {
		slog.Error("application backup recovery unavailable", "err", err)
	}
	// The pointer to a run lies on its volume, not on the stick (PLAN
	// 79-12): each volume is looked at once after it was mounted.
	w.appBackups.Settled = w.volumes.Settled
	if !w.opts.dev {
		w.background(func() { w.appBackups.Run(w.ctx) })
	}
	w.backups.PrepareApplications = w.appBackups.Prepare
	if err := w.backups.Load(); err != nil {
		slog.Warn("backup jobs not readable from /config", "err", err)
	}
	w.backups.ReleaseStaleSMB()
	// Backups of other TitanOS, taken in over Tailscale.
	w.backupTarget = backuptarget.NewManager(w.store)
	w.backupTarget.Disabled = w.opts.dev
	w.backupTarget.Addresses = func() []string { return w.tailnet.Status().IPs }
	w.backupTarget.Host = func() string { return w.tailnet.Status().Name }
	w.backupTarget.Mounted = w.volumes.Mounted
	// Btrfs replication: received over the same server, sent by jobs.
	w.replicaReceiver = replication.NewReceiver()
	w.replicaReceiver.Mounted, w.replicaReceiver.Filesystem = w.volumes.Mounted, w.volumes.FilesystemOf
	w.backupTarget.Replicas = w.replicaReceiver.Serve
	_ = w.backupTarget.Load() // Load keeps its own state; a failure shows up as not loaded
	w.background(func() { w.backupTarget.Run(ctx) })

	// The cluster: other TitanOS this NAS trusts fully (package cluster).
	w.nodes = cluster.NewManager(w.store)
	w.nodes.Users, w.nodes.Version, w.nodes.Backups = w.users, version.Version, w.backupTarget
	w.nodes.Name = func() string { name, _ := os.Hostname(); return name }
	w.nodes.Port = portOf(w.opts.httpsAddr, 443)
	if w.opts.dev {
		// Development mode serves plain HTTP on one address.
		w.nodes.Insecure, w.nodes.Loopback = true, true
		w.nodes.Port = portOf(w.opts.devAddr, 8080)
		devAddr := w.opts.devAddr
		w.nodes.Local = func() []string { return []string{devAddr} }
	}
	if !w.opts.dev {
		// The WireGuard tunnel of the cluster, when it is switched on.
		w.nodes.Runner = command.Exec{}
	}
	w.nodes.TailscaleName = cachedTailscaleName(w.tailnet)
	w.nodes.Update = func(ctx context.Context) (string, string, error) {
		if err := w.updates.Check(ctx); err != nil {
			return version.Version, "", err
		}
		st := w.updates.Status()
		if st.Available == nil || st.Available.Version == st.Version {
			return st.Version, "", nil
		}
		return st.Version, st.Available.Version, w.updates.Install(ctx)
	}
	if err := w.nodes.Load(); err != nil {
		slog.Warn("cluster settings not readable from /config", "err", err)
	}
	w.users.Changed = w.nodes.AccountChanged
	w.background(func() { w.nodes.Run(ctx) })
	w.background(func() { w.nodes.RunRelay(ctx) })
	w.replicas = replication.NewManager(w.store)
	w.replicas.Disabled = w.opts.dev
	w.replicas.Mounted, w.replicas.Filesystem = w.volumes.Mounted, w.volumes.FilesystemOf
	w.replicas.OnResult = func(job replication.Job, err error) {
		if err != nil {
			w.notifier.Notify(notify.Error, "replication_failed", job.Name)
		}
	}
	if err := w.replicas.Load(); err != nil {
		slog.Warn("replication jobs not readable from /config", "err", err)
	}
	w.background(func() { w.replicas.Run(ctx) })
	// Unmanaged data drives: shown, and mounted only on a click.
	w.drives = usb.NewManager()
	w.drives.Disabled = w.opts.dev
	w.drives.Claimed = func() map[string]string {
		claimed := w.machines.ClaimedDisks()
		for _, test := range w.volumes.Tests() {
			if test.Result == "" {
				claimed[test.Disk] = "disk test"
			}
		}
		return claimed
	}
	w.backups.WithUSB = w.drives.WithMounted
	w.background(func() { w.backups.Run(ctx) })
	w.drives.Reserved = w.volumes.Owns
	w.drives.Busy = func() bool { return w.volumes.ActionsRunning() || w.volumes.GrowthActive() }
	w.drives.OnPlug = func(d usb.Device) {
		w.notifier.Notify(notify.Info, "usb_plugged", d.Name)
		go w.backups.Plugged(d.ID)
	}
	w.drives.OnFormatted = func(job usb.FormatJob) {
		switch {
		case job.Action != "" && job.Phase == "done":
			w.notifier.Notify(notify.Info, "usb_partitions_changed", job.Name)
		case job.Action != "":
			w.notifier.Notify(notify.Warning, "usb_partitions_failed", job.Name)
		case job.Phase == "done":
			w.notifier.Notify(notify.Info, "usb_formatted", job.Name)
		default:
			w.notifier.Notify(notify.Warning, "usb_format_failed", job.Name)
		}
	}
	w.background(func() { w.drives.Watch(ctx) })
	return nil
}
