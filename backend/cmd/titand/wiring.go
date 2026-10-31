package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/api"
	"github.com/ra5on/TitanOS/backend/internal/app"
	"github.com/ra5on/TitanOS/backend/internal/appbackup"
	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/backuptarget"
	"github.com/ra5on/TitanOS/backend/internal/catalog"
	"github.com/ra5on/TitanOS/backend/internal/cluster"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/configbackup"
	"github.com/ra5on/TitanOS/backend/internal/ddns"
	"github.com/ra5on/TitanOS/backend/internal/diagnose"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/fans"
	"github.com/ra5on/TitanOS/backend/internal/fileindex"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/firewall"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/healthreport"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
	"github.com/ra5on/TitanOS/backend/internal/ipv64"
	"github.com/ra5on/TitanOS/backend/internal/letsencrypt"
	"github.com/ra5on/TitanOS/backend/internal/logkeep"
	"github.com/ra5on/TitanOS/backend/internal/lxc"
	"github.com/ra5on/TitanOS/backend/internal/maintenance"
	"github.com/ra5on/TitanOS/backend/internal/media"
	"github.com/ra5on/TitanOS/backend/internal/monitoring"
	"github.com/ra5on/TitanOS/backend/internal/netbird"
	"github.com/ra5on/TitanOS/backend/internal/network"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/plugins"
	"github.com/ra5on/TitanOS/backend/internal/replication"
	command "github.com/ra5on/TitanOS/backend/internal/run"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/shutdown"
	"github.com/ra5on/TitanOS/backend/internal/sshd"
	"github.com/ra5on/TitanOS/backend/internal/stickwatch"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/tailscale"
	"github.com/ra5on/TitanOS/backend/internal/terminal"
	"github.com/ra5on/TitanOS/backend/internal/thumbs"
	"github.com/ra5on/TitanOS/backend/internal/tlscert"
	"github.com/ra5on/TitanOS/backend/internal/update"
	"github.com/ra5on/TitanOS/backend/internal/ups"
	"github.com/ra5on/TitanOS/backend/internal/usb"
	"github.com/ra5on/TitanOS/backend/internal/version"
	"github.com/ra5on/TitanOS/backend/internal/vm"
	"github.com/ra5on/TitanOS/backend/internal/webpush"
	"github.com/ra5on/TitanOS/backend/internal/webui"
	"github.com/ra5on/TitanOS/backend/internal/wgserver"
)

// titand holds what run wires together: the managers of the daemon and
// the background services they run.
type titand struct {
	ctx  context.Context
	opts options
	// services are the background goroutines, waited for on shutdown.
	services sync.WaitGroup

	store            *config.Store
	users            *auth.Users
	base             string
	settings         *system.Settings
	connection       *network.Manager
	packetFilter     *firewall.Manager
	volumes          *storage.Manager
	folders          *shares.Manager
	routes           *appproxy.Manager
	devices          *hardware.Manager
	containers       *docker.Manager
	appCatalog       *catalog.Manager
	machines         *vm.Manager
	systemContainers *lxc.Manager
	// consoleMu keeps giving the text console back apart from a VM start.
	consoleMu       sync.Mutex
	tailnet         *tailscale.Manager
	mesh            *netbird.Manager
	extensions      *plugins.Manager
	shells          *terminal.Manager
	notifier        *notify.Manager
	logs            *logkeep.Manager
	stick           *stickwatch.Watcher
	pushes          *webpush.Manager
	home            *homeassistant.Manager
	stickBackup     *configbackup.Manager
	backupFailed    bool
	access          *sshd.Manager
	certs           *tlscert.Manager
	power           system.Power
	upsPower        system.Power
	beforePowerOff  func()
	ending          *shutdown.Manager
	unattended      func(action, by string, run func() error) func() error
	slot            string
	updates         *update.Manager
	upkeep          *maintenance.Manager
	backups         *backup.Manager
	appBackups      *appbackup.Manager
	backupTarget    *backuptarget.Manager
	replicaReceiver *replication.Receiver
	nodes           *cluster.Manager
	replicas        *replication.Manager
	drives          *usb.Manager
	supply          *ups.Manager
	shareDir        string
	nvidia          *hardware.NvidiaControl
	intelGuc        *hardware.IntelGuc
	powerButton     *system.PowerButtonManager
	energy          *system.EnergyManager
	schedule        *system.PowerScheduler
	monitor         *system.Monitor
	memoryPolicy    *system.MemoryPolicy
	fileManager     *files.Manager
	previews        *thumbs.Manager
	fullText        *fileindex.Manager
	history         *monitoring.Manager
	audit           *activity.Manager
	dockerNetworks  []docker.EngineNetwork
	acmeCerts       *letsencrypt.Manager
	kickFolders     func()
	dynamicDNS      *ddns.Manager
	account64       *ipv64.Manager
	vpnServer       *wgserver.Manager
	report          *healthreport.Manager
	reportSince     atomic.Int64 // the last report, Unix nanoseconds
	coolers         *fans.Manager
	mediaImports    *media.Manager
	sessions        *auth.Sessions
	diagnostics     *diagnose.Manager
	handler         *api.Server
}

// wireBase wires configuration, accounts, time zone, network, firewall and volumes: everything after relies on them.
func (w *titand) wireBase() error {
	ctx := w.ctx
	w.store = config.Open(w.opts.configDir)
	w.users = auth.NewUsers(w.store)
	if err := w.users.Load(); err != nil {
		// Not fatal: the web UI says that /config is missing and titand
		// tries again when someone opens it.
		slog.Warn("accounts not readable, is the stick plugged in?", "config", w.opts.configDir, "err", err)
	}

	// The version of the system image; version.Version is newer when an
	// app update runs on it.
	w.base = app.ReadBase(app.BaseVersionFile)
	if w.opts.dev {
		w.base = ""
	}

	// Time zone and network first: everything after it relies on them.
	w.settings = system.NewSettings(w.store)
	w.settings.Disabled = w.opts.dev
	if err := w.settings.Load(); err != nil {
		slog.Warn("system settings not put in place", "err", err)
	}
	w.connection = network.NewManager(w.store)
	w.connection.Disabled = w.opts.dev
	if err := w.connection.Load(ctx); err != nil {
		slog.Warn("network configuration not put in place", "err", err)
	}
	// The packet filter: off unless switched on in the web UI, then its own
	// nftables table; the automatic blocking after failed logins can be on
	// without it. Off, titanos-firewall-open keeps everything open.
	w.packetFilter = firewall.NewManager(w.store)
	w.packetFilter.Disabled = w.opts.dev
	w.packetFilter.Web = webPorts(w.opts)
	w.packetFilter.HomeNetworks = sshd.HomeNetworks
	if err := w.packetFilter.Load(ctx); err != nil {
		slog.Warn("firewall settings not put in place", "err", err)
	}

	w.volumes = storage.NewManager(w.store)
	w.volumes.ReadOnly = w.opts.dev
	if err := w.volumes.Load(); err != nil {
		slog.Warn("volumes not readable from /config", "err", err)
	}
	// Mounts the volumes as their arrays come up and reads the disks' health.
	w.background(func() { w.volumes.Run(ctx) })
	return nil
}

// wireShares wires shares and the HTTPS names of apps.
func (w *titand) wireShares() error {
	ctx := w.ctx
	// Keeps Linux and Samba accounts, smb.conf, the NFS exports and the
	// services in line with accounts, shares and mounted volumes.
	w.folders = shares.NewManager(w.store)
	w.folders.Disabled = w.opts.dev
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

// wireApplications wires Docker, the app catalog, VMs, LXC containers and plugins.
func (w *titand) wireApplications() error {
	ctx := w.ctx
	// Runs the Docker engine while its volume is mounted.
	w.devices = hardware.NewManager()
	w.containers = docker.NewManager(w.store, w.opts.configDir)
	w.containers.NetworkLease = func() (func(), error) {
		release, err := w.connection.AttachmentLease()
		if err != nil {
			return nil, docker.ErrBusy
		}
		return release, nil
	}
	w.containers.NetworkResolver = func(id string) (string, string, bool) {
		dev, ok := w.connection.Device(id, false)
		if !ok {
			return "", "", false
		}
		for _, n := range w.connection.Connections() {
			if n.ID == id {
				if n.IPv4 == "dhcp" {
					address, ready := w.connection.CurrentIPv4(ctx, id)
					return dev, address, ready
				}
				return dev, n.Address, true
			}
		}
		return "", "", false
	}
	w.containers.HardwareDevices = w.devices.ContainerDevices
	w.containers.RefreshCDI = func(ctx context.Context) error {
		_, err := (command.Exec{}).Run(ctx, "", "systemctl", "restart", "titanos-nvidia.service")
		return err
	}
	if !w.opts.dev {
		if err := w.containers.LoadHardwareLeases(); err != nil {
			return fmt.Errorf("load GPU leases: %w", err)
		}
	}
	w.containers.Disabled = w.opts.dev
	if err := w.containers.Load(); err != nil {
		slog.Warn("docker settings not readable from /config", "err", err)
	}

	// Knows the apps that can be installed, from the built-in catalog and
	// from sources on the network.
	w.appCatalog = catalog.NewManager(w.store)
	if err := w.appCatalog.Load(); err != nil {
		slog.Warn("app sources not readable from /config", "err", err)
	}
	w.background(func() { w.appCatalog.Run(ctx) })

	// Virtual machines; starts those marked for autostart once their volumes are there.
	w.machines = vm.NewManager(w.store, shares.UsersGID)
	w.machines.NetworkBridge = func(id string) (string, bool) { return w.connection.Device(id, true) }
	w.machines.NetworkLease = func() (func(), error) {
		release, err := w.connection.AttachmentLease()
		if err != nil {
			return nil, vm.ErrNoBridge
		}
		return release, nil
	}
	// System containers (LXC); like VMs they join the bridges of the NAS.
	w.systemContainers = lxc.NewManager(w.store, shares.UsersGID)
	w.systemContainers.NetworkBridge = w.machines.NetworkBridge
	w.systemContainers.BridgeReady = w.connection.BridgeReady
	w.systemContainers.NetworkLease = w.machines.NetworkLease
	w.systemContainers.Disabled = w.opts.dev
	if err := w.systemContainers.Load(); err != nil {
		slog.Warn("containers not readable from /config", "err", err)
	}
	w.connection.CheckChange = func(before, after network.Config) error {
		refs := map[string]bool{}
		legacy := false
		for _, n := range w.containers.LANNetworks() {
			refs[n.Parent] = false
			for _, parent := range after.Networks {
				if parent.ID == n.Parent {
					if err := n.ValidateParentAddress(parent.Address); err != nil {
						return fmt.Errorf("%w: %s", network.ErrInvalid, err)
					}
				}
			}
		}
		for _, ref := range append(w.machines.NetworkReferences(), w.systemContainers.NetworkReferences()...) {
			if ref == "bridge" {
				legacy = true
			} else {
				refs[strings.TrimPrefix(ref, "network:")] = true
			}
		}
		return network.ProtectAttachments(before, after, refs, legacy)
	}
	w.machines.BridgeReady = w.connection.BridgeReady
	w.machines.Timezone = w.settings.CurrentTimezone
	// consoleMu keeps giving the text console back apart from a VM start,
	// which may just have taken it, until libvirt has the device.
	w.machines.BeforeStart = func(ctx context.Context, name string, pci, usb []string, autoGPU bool) (func(bool), error) {
		vmDevices := hardware.NewManager()
		if _, err := vmDevices.Check(pci, usb); err != nil {
			return nil, err
		}
		finish, err := w.containers.BeginVM(ctx, name, pci, usb, autoGPU)
		if err != nil {
			return nil, err
		}
		w.consoleMu.Lock()
		// After the containers: an automatic NVIDIA switch stops them first,
		// then nothing on the NAS may hold the device any more.
		if _, err := vmDevices.PrepareHandoff(pci); err != nil {
			w.consoleMu.Unlock()
			slog.Warn("VM hardware still in use on the NAS", "vm", name, "err", err)
			finish(false)
			return nil, err
		}
		return func(started bool) {
			finish(started)
			if !started {
				vmDevices.RestoreConsole()
			}
			w.consoleMu.Unlock()
		}, nil
	}
	w.machines.AfterPoll = func(ctx context.Context, running map[string]bool) error {
		err := w.containers.ReturnVMHardware(ctx, running)
		if !w.opts.dev {
			w.consoleMu.Lock()
			hardware.NewManager().RestoreConsole()
			w.consoleMu.Unlock()
		}
		return err
	}
	w.machines.Disabled = w.opts.dev
	// Copies of the VM settings go only to volumes mounted right now.
	w.machines.Mounted = w.volumes.Mounted
	if err := w.machines.Load(); err != nil {
		slog.Warn("VMs not readable from /config", "err", err)
	}
	// Only disks that are in no volume may be handed to a VM as a whole.
	w.machines.FreeDisks = func() []string {
		status, err := w.volumes.Status(ctx)
		if err != nil {
			return nil
		}
		var free []string
		for _, disk := range status.Disks {
			// A disk a guest has put a file system on is still one the
			// NAS does not use.
			if (disk.Role == storage.RoleFree || disk.Role == storage.RoleUsed) && !disk.Mounted && !disk.Testing {
				free = append(free, disk.Name)
			}
		}
		return free
	}
	// And a disk a VM uses is never overwritten by a disk test.
	w.volumes.Claimed = w.machines.ClaimedDisks
	w.background(func() { w.volumes.RunDiskSleep(ctx) })

	// System extensions on a volume, laid over the system once it is mounted.
	w.tailnet = tailscale.NewManager(w.store)
	w.tailnet.Disabled = w.opts.dev
	if err := w.tailnet.Load(); err != nil {
		slog.Warn("Tailscale settings not readable from /config", "err", err)
	}
	// HTTPS names in the tailnet: one device per app, beside the plugin's.
	w.routes.Tailnet.Disabled = w.opts.dev
	w.routes.Tailnet.Volume = w.tailnet.Volume
	w.background(func() { w.routes.RunTailnet(ctx) })
	w.mesh = netbird.NewManager(w.store)
	w.mesh.Disabled = w.opts.dev
	if err := w.mesh.Load(); err != nil {
		slog.Warn("NetBird settings not readable from /config", "err", err)
	}
	w.extensions = plugins.NewManager(w.store)
	// Each runtime ignores the plugins that are not its own.
	w.extensions.PrepareRuntime = func(name, volume string) error {
		return errors.Join(w.tailnet.Prepare(name, volume), w.mesh.Prepare(name, volume))
	}
	w.extensions.ActivateRuntime = func(name string) error {
		return errors.Join(w.tailnet.Activate(name), w.mesh.Activate(name))
	}
	w.extensions.LocalBuilds = true
	// Plugins are built for the system image, which an app update keeps.
	w.extensions.Release = strings.TrimPrefix(version.Version, "v")
	if w.base != "" {
		w.extensions.Release = w.base
	}
	w.extensions.Disabled = w.opts.dev
	if err := w.extensions.Load(); err != nil {
		slog.Warn("plugin settings not readable from /config", "err", err)
	}
	w.containers.Ready = w.extensions.Healthy
	w.machines.AutoReady = func() bool { return w.extensions.Healthy() && w.containers.StartupReady() }
	return nil
}

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
	// Home Assistant: MCP clients switch entities released to them, and
	// messages can go to the Home Assistant app.
	w.home = homeassistant.NewManager(w.store)
	if err := w.home.Load(); err != nil {
		slog.Warn("Home Assistant settings not readable from /config", "err", err)
	}
	w.notifier.SetHomeAssistant(w.home)
	w.background(func() { w.home.Run(ctx) })
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

// wireAccess wires the copies of /config, SSH and the certificate of the web UI.
func (w *titand) wireAccess() error {
	ctx := w.ctx
	// Copies of /config on a volume: the stick is the only place where
	// the configuration lives, and sticks wear out.
	w.stickBackup = configbackup.NewManager(w.store, version.Version)
	w.stickBackup.Disabled = w.opts.dev
	if err := w.stickBackup.Load(); err != nil {
		slog.Warn("backup settings not readable from /config", "err", err)
	}
	// A failure is reported once, and again only after a backup worked.
	w.backupFailed = false
	w.stickBackup.OnResult = func(volume string, err error) {
		if err != nil && !w.backupFailed {
			w.notifier.Notify(notify.Warning, "config_backup_failed", volume)
		}
		w.backupFailed = err != nil
	}
	w.stickBackup.OnNoVolume = func(volume string) {
		w.notifier.Notify(notify.Warning, "config_backup_no_volume", volume)
	}
	w.background(func() { w.stickBackup.Run(ctx, w.volumes.Mounted) })

	// SSH is off unless switched on; then for administrators with a key.
	w.access = sshd.NewManager(w.store, shares.UsersGID)
	w.access.Disabled = w.opts.dev
	if err := w.access.Load(); err != nil {
		slog.Warn("SSH settings not readable from /config", "err", err)
	}
	w.background(func() { w.access.Run(ctx, w.users.List) })

	// The certificate of the web UI: the administrator's own or one made here.
	if !w.opts.dev {
		hostname, _ := os.Hostname()
		var err error
		w.certs, err = tlscert.NewManager(w.store, hostname)
		if w.certs == nil {
			return fmt.Errorf("TLS certificate: %w", err)
		}
		if err != nil {
			slog.Warn("using a temporary TLS certificate", "err", err)
		}
		w.background(func() { watchCertificate(ctx, w.certs, w.notifier) })
	}
	return nil
}

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
	})
	// Nobody watches these: after the waiting time they go on by themselves.
	w.unattended = func(action, by string, run func() error) func() error {
		return func() error { return w.ending.Begin(action, by, true, run) }
	}

	// New versions go into the other slot of the stick and start on trial.
	w.slot = system.BootSlot()
	w.updates = update.NewManager(w.store, version.Version, w.slot)
	w.updates.Disabled = w.opts.dev || w.slot == ""
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
	w.updates.PreparePlugins = func(ctx context.Context, version, kernel, kernelPackage, baseVersion string, assets map[string]string, fetch func(context.Context, string, int64) ([]byte, error), download func(context.Context, string, string, string, int64) error) error {
		return w.extensions.PrepareUpdate(ctx, version, kernel, kernelPackage, baseVersion, assets, w.volumes.Mounted(), fetch, download)
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
	w.backups.Timezone = w.settings.CurrentTimezone
	w.appBackups = &appbackup.Manager{Store: w.store, Docker: w.containers, VMs: w.machines, Mounted: w.volumes.Mounted, MountRoot: "/mnt", Disabled: w.opts.dev}
	if err := w.appBackups.Load(); err != nil {
		slog.Error("application backup recovery unavailable", "err", err)
	}
	w.backups.PrepareApplications = w.appBackups.Prepare
	if err := w.backups.Load(); err != nil {
		slog.Warn("backup jobs not readable from /config", "err", err)
	}
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
	w.backupTarget.Load()
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

// wireRemoteAccess wires certificates from Let's Encrypt, dynamic DNS, IPv64 and WireGuard.
func (w *titand) wireRemoteAccess() error {
	ctx := w.ctx
	// Certificates from Let's Encrypt, served next to the own or
	// self-signed one without a restart; renewed by themselves.
	w.acmeCerts = letsencrypt.New(w.store)
	w.acmeCerts.Disabled = w.opts.dev || w.certs == nil
	w.acmeCerts.HTTPPort = !w.opts.dev && w.opts.httpAddr != ""
	w.acmeCerts.AppDomain = func() string { return w.routes.Settings().Domain }
	w.acmeCerts.AppNames = w.routes.Domains
	// New full domains of app routes join the certificate soon.
	w.kickFolders = w.routes.OnChange
	w.routes.OnChange = func() {
		if w.kickFolders != nil {
			w.kickFolders()
		}
		w.acmeCerts.NamesChanged()
	}
	w.acmeCerts.Install = func(cert *tls.Certificate) {
		if w.certs != nil {
			w.certs.SetACME(cert)
		}
	}
	w.acmeCerts.OnEvent = func(severity, kind, subject string) { w.notifier.Notify(severity, kind, subject) }
	w.acmeCerts.Activity = func(action, target string, ok bool) {
		w.audit.Record(activity.Entry{Via: activity.ViaSystem, Action: action, Target: target, OK: ok})
	}
	if err := w.acmeCerts.Load(); err != nil {
		slog.Warn("Let's Encrypt settings not readable from /config", "err", err)
	}
	w.background(func() { w.acmeCerts.Run(ctx) })
	// The NAS's own name in the tailnet gets the certificate Tailscale hands
	// out once HTTPS is on in the tailnet (passkeys and web push need it).
	if w.certs != nil {
		w.background(func() { w.tailnet.RunCertificate(ctx, w.certs.SetTailnet) })
	}
	// Dynamic DNS: host names at the public addresses of the NAS.
	w.dynamicDNS = ddns.New(w.store)
	w.dynamicDNS.Disabled = w.opts.dev
	w.dynamicDNS.OnEvent = w.acmeCerts.OnEvent
	w.dynamicDNS.Activity = w.acmeCerts.Activity
	if err := w.dynamicDNS.Load(); err != nil {
		slog.Warn("dynamic DNS settings not readable from /config", "err", err)
	}
	w.background(func() { w.dynamicDNS.Run(ctx) })
	w.beforePowerOff = func() { w.dynamicDNS.PauseForShutdown(context.Background()) }
	// The IPv64.net account beyond dynamic DNS: notifications from polling,
	// the v64 blocklist for the firewall and the WireGuard tunnel into a
	// Cloud Router; all off until switched on.
	w.account64 = ipv64.NewManager(w.store)
	w.account64.Disabled = w.opts.dev
	w.account64.Fallback = func() (string, string) { return w.dynamicDNS.IPv64Key(), w.acmeCerts.IPv64Key() }
	w.account64.Notify = func(severity, kind, subject string) { w.notifier.Notify(severity, kind, subject) }
	w.account64.Firewall = w.packetFilter
	if err := w.account64.Load(ctx); err != nil {
		slog.Warn("IPv64 settings not readable from /config", "err", err)
	}
	w.background(func() { w.account64.Run(ctx) })
	// The WireGuard server of the NAS, off unless switched on.
	w.vpnServer = wgserver.New(w.store)
	w.vpnServer.Disabled = w.opts.dev
	w.vpnServer.HomeNetworks = sshd.HomeNetworks
	w.vpnServer.SuggestEndpoint = func() string {
		if names := w.dynamicDNS.Hostnames(); len(names) > 0 {
			return names[0]
		}
		return ""
	}
	if err := w.vpnServer.Load(); err != nil {
		slog.Warn("WireGuard settings not readable from /config", "err", err)
	} else if err := w.vpnServer.Apply(ctx); err != nil {
		slog.Warn("WireGuard server not up", "err", err)
	}
	return nil
}

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
			if message.Time.Before(d.Since) {
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
			list = append(list, notify.TempReading{ID: t.ID, Label: t.Label, Celsius: t.Celsius, Critical: t.CriticalC})
		}
		return list
	}
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

	// An app update restarts titand: it waits for work that would break off.
	w.updates.Working = func() bool {
		return w.upkeep.Busy() || w.appBackups.Busy() || w.volumes.ActionsRunning() || w.volumes.SnapshotRestoresRunning() || w.volumes.GrowthPending() || w.volumes.Reshaping() || w.replicas.Busy() || w.fileManager.Busy() || w.backups.Busy() || w.volumes.TestsRunning() || w.machines.Busy() || w.systemContainers.Busy() || w.extensions.Busy() || w.mediaImports.Working() || w.folders.PurgeBusy()
	}
	// Settings › Services: what was switched off stays off, before anything
	// is started automatically.
	var services struct{ VMsOff, LXCOff bool }
	if err := w.store.ReadJSON("system/services.json", &services); err == nil {
		if services.VMsOff {
			w.machines.SetOff(ctx, true)
		}
		if services.LXCOff {
			w.systemContainers.SetOff(ctx, true, nil)
		}
	}
	w.background(func() { w.machines.Run(ctx, w.volumes.Mounted) })
	w.background(func() { w.systemContainers.Run(ctx, w.volumes.Mounted) })
	w.background(func() { w.updates.Run(ctx) })
	return nil
}

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
		ApplicationBackups: w.appBackups,
		Storage:            w.volumes,
		Shares:             w.folders,
		Files:              w.fileManager,
		Thumbs:             w.previews,
		FileIndex:          w.fullText,
		Docker:             w.containers,
		AppProxy:           w.routes,
		Catalog:            w.appCatalog,
		Terminal:           w.shells,
		VMs:                w.machines,
		LXC:                w.systemContainers,
		Hardware:           w.devices,
		Nvidia:             w.nvidia,
		IntelGuc:           w.intelGuc,
		Accelerators:       hardware.NewAcceleratorMonitor(ctx, w.devices),
		Plugins:            w.extensions,
		Tailscale:          w.tailnet,
		Netbird:            w.mesh,
		Network:            w.connection,
		Firewall:           w.packetFilter,
		IPv64:              w.account64,
		IPv64Reserved:      reservedPorts(w.opts),
		Notify:             w.notifier,
		WebPush:            w.pushes,
		WireGuard:          w.vpnServer,
		HealthReport:       w.report,
		Fans:               w.coolers,
		HomeAssistant:      w.home,
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

	// Watches that Home Assistant keeps putting its backups on the NAS.
	w.background(func() { w.handler.RunHAStorage(ctx) })
	// Leftovers of the AI chat removed in 0.41.0, once.
	w.background(func() { w.handler.RemoveRetiredAssistant(ctx) })
	w.background(func() { w.logs.Run(ctx) })
	if !w.opts.dev {
		w.background(func() { w.stick.Follow(ctx) })
	}
	w.background(func() { w.home.RunMQTT(ctx, w.handler.HASnapshot, w.handler.HACommand) })
	w.schedule.Busy = w.handler.PowerBusy
	w.background(func() { w.schedule.Run(ctx) })
	return nil
}

// wire builds the managers in the order they depend on each other and
// starts their background services.
func (w *titand) wire() error {
	for _, step := range []func() error{
		w.wireBase, w.wireShares, w.wireApplications, w.wireNotifications, w.wireAccess, w.wireUpdates,
		w.wireBackups, w.wireHardware, w.wireFiles, w.wireRemoteAccess, w.wireMaintenance, w.wireAPI,
	} {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// background runs a service until it returns, which it does once the
// context of the daemon ends.
func (w *titand) background(service func()) {
	w.services.Go(service)
}

// waitServices waits for the background services, at most until ctx ends.
func (w *titand) waitServices(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		w.services.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		slog.Warn("background services did not stop in time")
	}
}
