package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
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
	"github.com/ra5on/TitanOS/backend/internal/scripts"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/shutdown"
	"github.com/ra5on/TitanOS/backend/internal/smarthistory"
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
	"github.com/ra5on/TitanOS/backend/internal/vm"
	"github.com/ra5on/TitanOS/backend/internal/webpush"
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
	smartHistory     *smarthistory.Recorder
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
	userScripts     *scripts.Manager
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
	if !w.opts.dev {
		waitForConfig(ctx, w.opts.configDir, 2*time.Minute)
	}
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
	// Once a day the SMART values of each disk, on the first volume.
	w.smartHistory = smarthistory.New("/mnt")
	w.smartHistory.Disabled = w.opts.dev
	w.smartHistory.Mounted = w.volumes.Mounted
	w.smartHistory.Reports = func(ctx context.Context) ([]storage.SmartReport, error) {
		return w.volumes.SmartReports(ctx, false)
	}
	w.background(func() { w.smartHistory.Run(ctx) })
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

// waitForConfig waits until /config can be read: the stick is mounted on
// first access (automount) and a slow USB controller can make that fail at
// boot. Every part of titand reads its settings once at start, so reading
// too early would leave them at their defaults until the next start. A
// missing config directory (a new stick) does not count as "not ready".
func waitForConfig(ctx context.Context, dir string, patience time.Duration) {
	deadline := time.Now().Add(patience)
	logged := false
	for {
		_, err := os.Stat(dir)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			if logged {
				slog.Info("stick readable", "config", dir)
			}
			return
		}
		if time.Now().After(deadline) {
			slog.Warn("stick still not readable, going on without it", "config", dir, "err", err)
			return
		}
		if !logged {
			slog.Info("waiting for the stick", "config", dir, "err", err)
			logged = true
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}
