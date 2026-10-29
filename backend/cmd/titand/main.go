// titand is the TitanOS daemon. It serves the web UI and the API and manages
// storage, shares, containers and VMs.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/api"
	"github.com/ra5on/TitanOS/backend/internal/app"
	"github.com/ra5on/TitanOS/backend/internal/appbackup"
	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/assistant"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/backuptarget"
	"github.com/ra5on/TitanOS/backend/internal/catalog"
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
	"github.com/ra5on/TitanOS/backend/internal/sshd"
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

type options struct {
	configDir string
	httpsAddr string
	httpAddr  string
	dev       bool
	devAddr   string
}

func main() {
	var opts options
	flag.StringVar(&opts.configDir, "config", "/config", "directory with the persistent configuration")
	flag.StringVar(&opts.httpsAddr, "https", ":443", "address of the web UI (HTTPS)")
	flag.StringVar(&opts.httpAddr, "http", ":80", "address that redirects to HTTPS; empty to turn off")
	flag.BoolVar(&opts.dev, "dev", false, "development mode: plain HTTP on -dev-listen, no reboot or poweroff")
	flag.StringVar(&opts.devAddr, "dev-listen", "127.0.0.1:8080", "address in development mode")
	showVersion := flag.Bool("version", false, "print the version and exit")
	checkBoot := flag.Bool("check-boot", false, "wait until the running titand is ready, for the boot check after an update")
	checkApp := flag.Bool("check-app", false, "wait until a new app update is ready, and go back if it does not get there")
	recovery := flag.Bool("network-recovery", false, "authenticated local-console network recovery")
	flag.Parse()
	if *recovery {
		if err := networkRecovery(opts.configDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if !opts.dev {
		// The journal keeps the time itself.
		log.SetFlags(0)
		log.SetOutput(journalWriter{os.Stderr})
	}

	if *checkApp {
		if slot := system.BootSlot(); slot != "" {
			if err := checkAppUpdate(app.New("/efi", slot), opts.httpsAddr, 5*time.Minute); err != nil {
				fmt.Fprintln(os.Stderr, "app update:", err)
				os.Exit(1)
			}
		}
		return
	}

	if *checkBoot {
		// Only a system the boot loader started on trial has to prove itself.
		trial := update.NewManager(config.Open(opts.configDir), version.Version, system.BootSlot())
		if !trial.OnTrial() {
			return
		}
		if err := waitReady(opts.httpsAddr, 5*time.Minute); err != nil {
			fmt.Fprintln(os.Stderr, "titand is not ready, this system stays on trial:", err)
			os.Exit(1)
		}
		if err := trial.Bless(); err != nil {
			fmt.Fprintln(os.Stderr, "could not mark this system good:", err)
			os.Exit(1)
		}
		return
	}

	if *showVersion {
		fmt.Println(version.Version)
		return
	}

	handOver(opts)
	if err := run(opts); err != nil {
		slog.Error("titand stopped", "err", err)
		os.Exit(1)
	}
}

// journalWriter marks each log line with its level the way journald reads
// it from a service's output ("<3>" is an error), so that the log view
// can tell errors and warnings of titand from the rest.
type journalWriter struct{ out io.Writer }

func (w journalWriter) Write(line []byte) (int, error) {
	prefix := "<6>"
	switch {
	case bytes.HasPrefix(line, []byte("ERROR")):
		prefix = "<3>"
	case bytes.HasPrefix(line, []byte("WARN")):
		prefix = "<4>"
	case bytes.HasPrefix(line, []byte("DEBUG")):
		prefix = "<7>"
	}
	if _, err := w.out.Write(append([]byte(prefix), line...)); err != nil {
		return 0, err
	}
	return len(line), nil
}

// handOver starts the app update of the running slot in place of this
// titand, if there is one that passes the checks. It returns only when the
// titand of the system image itself is to run.
func handOver(opts options) {
	if opts.dev || os.Getenv(app.Env) != "" {
		return
	}
	slot := system.BootSlot()
	if slot == "" {
		return
	}
	stick := app.New("/efi", slot)
	if stick.Base == "" {
		return
	}
	path, appVersion, err := stick.Launch()
	if err != nil {
		slog.Error("app update not started, running the titand of the system image", "err", err)
		return
	}
	if path == "" {
		return
	}
	slog.Info("handing over to the app update", "version", appVersion, "base", stick.Base)
	err = syscall.Exec(path, os.Args, append(os.Environ(), app.Env+"="+appVersion))
	slog.Error("app update could not be started, running the titand of the system image", "err", err)
}

// checkAppUpdate settles an app update on trial: once titand runs it and
// is ready, it is kept; if not within five minutes, the app before it (or
// the titand of the system image) runs again.
func checkAppUpdate(stick *app.Stick, httpsAddr string, patience time.Duration) error {
	state, err := stick.ReadState()
	if err != nil || !state.Trial {
		return err
	}
	err = waitApp(stick, state.Current, httpsAddr, patience)
	// The base may have given up on the app in the meantime.
	now, readErr := stick.ReadState()
	if readErr != nil {
		return readErr
	}
	if !now.Trial || now.Current != state.Current {
		return nil
	}
	if err == nil {
		_, err = stick.Confirm(true)
		slog.Info("app update is ready and stays", "version", state.Current)
		return err
	}
	slog.Error("app update did not become ready, going back", "version", state.Current, "err", err)
	if _, confirmErr := stick.Confirm(false); confirmErr != nil {
		return confirmErr
	}
	if restartErr := restartTitand(); restartErr != nil {
		return restartErr
	}
	return err
}

// restartTitand starts titand anew, with whatever app is current now.
var restartTitand = func() error {
	_, err := (command.Exec{}).Run(context.Background(), "", "systemctl", "--no-block", "restart", "titand.service")
	return err
}

// waitApp waits until titand runs the app and says it is ready.
func waitApp(stick *app.Stick, appVersion, httpsAddr string, patience time.Duration) error {
	deadline := time.Now().Add(patience)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return errors.New("not ready in time")
		}
		if stick.Running() == appVersion {
			if err := waitReady(httpsAddr, min(left, 20*time.Second)); err == nil && stick.Running() == appVersion {
				return nil
			}
		} else {
			// Started over and fell back, or still starting.
			if state, err := stick.ReadState(); err == nil && (!state.Trial || state.Current != appVersion) {
				return errors.New("the system image gave up on it")
			}
			time.Sleep(3 * time.Second)
		}
	}
}

func run(opts options) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store := config.Open(opts.configDir)
	users := auth.NewUsers(store)
	if err := users.Load(); err != nil {
		// Not fatal: the web UI says that /config is missing and titand
		// tries again when someone opens it.
		slog.Warn("accounts not readable, is the stick plugged in?", "config", opts.configDir, "err", err)
	}
	// The chat in the web UI talks to Ollama.
	chat := assistant.NewManager(store)
	if err := chat.Load(); err != nil {
		slog.Warn("assistant settings not readable from /config", "err", err)
	}

	// The version of the system image; version.Version is newer when an
	// app update runs on it.
	base := app.ReadBase(app.BaseVersionFile)
	if opts.dev {
		base = ""
	}

	// Time zone and network first: everything after it relies on them.
	settings := system.NewSettings(store)
	settings.Disabled = opts.dev
	if err := settings.Load(); err != nil {
		slog.Warn("system settings not put in place", "err", err)
	}
	connection := network.NewManager(store)
	connection.Disabled = opts.dev
	if err := connection.Load(ctx); err != nil {
		slog.Warn("network configuration not put in place", "err", err)
	}
	// The packet filter: off unless switched on in the web UI, then its own
	// nftables table; the automatic blocking after failed logins can be on
	// without it. Off, titanos-firewall-open keeps everything open.
	packetFilter := firewall.NewManager(store)
	packetFilter.Disabled = opts.dev
	packetFilter.Web = webPorts(opts)
	packetFilter.HomeNetworks = sshd.HomeNetworks
	if err := packetFilter.Load(ctx); err != nil {
		slog.Warn("firewall settings not put in place", "err", err)
	}

	volumes := storage.NewManager(store)
	volumes.ReadOnly = opts.dev
	if err := volumes.Load(); err != nil {
		slog.Warn("volumes not readable from /config", "err", err)
	}
	// Mounts the volumes as their arrays come up and reads the disks' health.
	go volumes.Run(ctx)

	// Keeps Linux and Samba accounts, smb.conf, the NFS exports and the
	// services in line with accounts, shares and mounted volumes.
	folders := shares.NewManager(store)
	folders.Disabled = opts.dev
	folders.Shadow = volumes.ShadowDir
	if err := folders.Load(); err != nil {
		slog.Warn("shares not readable from /config", "err", err)
	}
	// HTTPS names of Docker apps, served by titand itself (see below) and
	// published over mDNS; avahi then runs even without SMB shares.
	routes := appproxy.NewManager(store)
	routes.Disabled = opts.dev
	routes.HTTPSPort, routes.Reserved = proxyPorts(opts)
	routes.OnChange = folders.Kick
	routes.Gate = packetFilter.AllowHTTP
	if err := routes.Load(); err != nil {
		slog.Warn("app HTTPS names not readable from /config", "err", err)
	}
	go routes.Run(ctx)

	go folders.Run(ctx, func() shares.State {
		return shares.State{Users: users.List(), Mounted: volumes.Mounted(), Quotas: volumes.ShareQuotas, Discovery: routes.Publishing()}
	})

	// Runs the Docker engine while its volume is mounted.
	devices := hardware.NewManager()
	containers := docker.NewManager(store, opts.configDir)
	containers.NetworkLease = func() (func(), error) {
		release, err := connection.AttachmentLease()
		if err != nil {
			return nil, docker.ErrBusy
		}
		return release, nil
	}
	containers.NetworkResolver = func(id string) (string, string, bool) {
		dev, ok := connection.Device(id, false)
		if !ok {
			return "", "", false
		}
		for _, n := range connection.Connections() {
			if n.ID == id {
				if n.IPv4 == "dhcp" {
					address, ready := connection.CurrentIPv4(ctx, id)
					return dev, address, ready
				}
				return dev, n.Address, true
			}
		}
		return "", "", false
	}
	containers.HardwareDevices = devices.ContainerDevices
	containers.RefreshCDI = func(ctx context.Context) error {
		_, err := (command.Exec{}).Run(ctx, "", "systemctl", "restart", "titanos-nvidia.service")
		return err
	}
	if !opts.dev {
		if err := containers.LoadHardwareLeases(); err != nil {
			return fmt.Errorf("load GPU leases: %w", err)
		}
	}
	containers.Disabled = opts.dev
	if err := containers.Load(); err != nil {
		slog.Warn("docker settings not readable from /config", "err", err)
	}

	// Knows the apps that can be installed, from the built-in catalog and
	// from sources on the network.
	appCatalog := catalog.NewManager(store)
	if err := appCatalog.Load(); err != nil {
		slog.Warn("app sources not readable from /config", "err", err)
	}
	go appCatalog.Run(ctx)

	// Virtual machines; starts those marked for autostart once their volumes are there.
	machines := vm.NewManager(store, shares.UsersGID)
	machines.NetworkBridge = func(id string) (string, bool) { return connection.Device(id, true) }
	machines.NetworkLease = func() (func(), error) {
		release, err := connection.AttachmentLease()
		if err != nil {
			return nil, vm.ErrNoBridge
		}
		return release, nil
	}
	// System containers (LXC); like VMs they join the bridges of the NAS.
	systemContainers := lxc.NewManager(store, shares.UsersGID)
	systemContainers.NetworkBridge = machines.NetworkBridge
	systemContainers.BridgeReady = connection.BridgeReady
	systemContainers.NetworkLease = machines.NetworkLease
	systemContainers.Disabled = opts.dev
	if err := systemContainers.Load(); err != nil {
		slog.Warn("containers not readable from /config", "err", err)
	}
	connection.CheckChange = func(before, after network.Config) error {
		refs := map[string]bool{}
		legacy := false
		for _, n := range containers.LANNetworks() {
			refs[n.Parent] = false
			for _, parent := range after.Networks {
				if parent.ID == n.Parent {
					if err := n.ValidateParentAddress(parent.Address); err != nil {
						return fmt.Errorf("%w: %s", network.ErrInvalid, err)
					}
				}
			}
		}
		for _, ref := range append(machines.NetworkReferences(), systemContainers.NetworkReferences()...) {
			if ref == "bridge" {
				legacy = true
			} else {
				refs[strings.TrimPrefix(ref, "network:")] = true
			}
		}
		return network.ProtectAttachments(before, after, refs, legacy)
	}
	machines.BridgeReady = connection.BridgeReady
	machines.Timezone = settings.CurrentTimezone
	// consoleMu keeps giving the text console back apart from a VM start,
	// which may just have taken it, until libvirt has the device.
	var consoleMu sync.Mutex
	machines.BeforeStart = func(ctx context.Context, name string, pci, usb []string, autoGPU bool) (func(bool), error) {
		devices := hardware.NewManager()
		if _, err := devices.Check(pci, usb); err != nil {
			return nil, err
		}
		finish, err := containers.BeginVM(ctx, name, pci, usb, autoGPU)
		if err != nil {
			return nil, err
		}
		consoleMu.Lock()
		// After the containers: an automatic NVIDIA switch stops them first,
		// then nothing on the NAS may hold the device any more.
		if _, err := devices.PrepareHandoff(pci); err != nil {
			consoleMu.Unlock()
			slog.Warn("VM hardware still in use on the NAS", "vm", name, "err", err)
			finish(false)
			return nil, err
		}
		return func(started bool) {
			finish(started)
			if !started {
				devices.RestoreConsole()
			}
			consoleMu.Unlock()
		}, nil
	}
	machines.AfterPoll = func(ctx context.Context, running map[string]bool) error {
		err := containers.ReturnVMHardware(ctx, running)
		if !opts.dev {
			consoleMu.Lock()
			hardware.NewManager().RestoreConsole()
			consoleMu.Unlock()
		}
		return err
	}
	machines.Disabled = opts.dev
	if err := machines.Load(); err != nil {
		slog.Warn("VMs not readable from /config", "err", err)
	}
	// Only disks that are in no volume may be handed to a VM as a whole.
	machines.FreeDisks = func() []string {
		status, err := volumes.Status(ctx)
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
	volumes.Claimed = machines.ClaimedDisks
	go volumes.RunDiskSleep(ctx)

	// System extensions on a volume, laid over the system once it is mounted.
	tailnet := tailscale.NewManager(store)
	tailnet.Disabled = opts.dev
	if err := tailnet.Load(); err != nil {
		slog.Warn("Tailscale settings not readable from /config", "err", err)
	}
	// HTTPS names in the tailnet: one device per app, beside the plugin's.
	routes.Tailnet.Disabled = opts.dev
	routes.Tailnet.Volume = tailnet.Volume
	go routes.RunTailnet(ctx)
	mesh := netbird.NewManager(store)
	mesh.Disabled = opts.dev
	if err := mesh.Load(); err != nil {
		slog.Warn("NetBird settings not readable from /config", "err", err)
	}
	extensions := plugins.NewManager(store)
	// Each runtime ignores the plugins that are not its own.
	extensions.PrepareRuntime = func(name, volume string) error {
		return errors.Join(tailnet.Prepare(name, volume), mesh.Prepare(name, volume))
	}
	extensions.ActivateRuntime = func(name string) error {
		return errors.Join(tailnet.Activate(name), mesh.Activate(name))
	}
	extensions.LocalBuilds = true
	// Plugins are built for the system image, which an app update keeps.
	extensions.Release = strings.TrimPrefix(version.Version, "v")
	if base != "" {
		extensions.Release = base
	}
	extensions.Disabled = opts.dev
	if err := extensions.Load(); err != nil {
		slog.Warn("plugin settings not readable from /config", "err", err)
	}
	containers.Ready = extensions.Healthy
	machines.AutoReady = func() bool { return extensions.Healthy() && containers.StartupReady() }

	// The root shell of the terminal panel.
	shells := terminal.NewManager(store)
	shells.Disabled = opts.dev
	if err := shells.Load(); err != nil {
		slog.Warn("terminal settings not readable from /config", "err", err)
	}

	// Tells in the web UI, by mail and by push message when a volume or a
	// disk needs attention.
	notifier := notify.NewManager(store)
	// Only owners and administrators may point their own channels into the
	// home network; a plain user's go to public addresses only.
	notifier.Confined = func(name string) bool {
		user, ok := users.Get(name)
		return !ok || !user.Role.CanAdminister()
	}
	if err := notifier.Load(); err != nil {
		slog.Warn("notification settings not readable from /config", "err", err)
	}
	// The daily summary of the ways out, at the time in the NAS time zone.
	go notifier.RunDigest(ctx, func() *time.Location {
		if zone, err := time.LoadLocation(settings.CurrentTimezone()); err == nil {
			return zone
		}
		return time.Local
	})
	// Notifications on phones and browsers (Web Push): what the bell shows,
	// to owners and administrators who subscribed a device.
	pushes := webpush.New(store)
	pushes.Allowed = func(name string) bool {
		user, ok := users.Get(name)
		return ok && user.Role.CanAdminister() && !user.Disabled
	}
	if err := pushes.Load(); err != nil {
		slog.Warn("web push settings not readable from /config", "err", err)
	}
	notifier.Push = func(message notify.Message, title string) {
		pushes.Broadcast(webpush.Message{Title: title, Body: message.Text, Tag: message.Kind + ":" + message.Subject, URL: "/", Urgent: message.Severity == notify.Error})
	}
	// Once per plugin and kind of device; installing stays a click.
	extensions.Notify = func(title string) { notifier.Notify(notify.Info, "plugin_recommended", title) }
	// Home Assistant: the assistant switches entities released to it, and
	// messages can go to the Home Assistant app.
	home := homeassistant.NewManager(store)
	if err := home.Load(); err != nil {
		slog.Warn("Home Assistant settings not readable from /config", "err", err)
	}
	notifier.SetHomeAssistant(home)
	go home.Run(ctx)
	go notify.NewWatcher(notifier).Run(ctx, volumes.Status)
	volumes.OnSnapshotRestoreDone = func(job storage.SnapshotFolderJob) {
		switch job.State {
		case storage.RestoreDone:
			notifier.Notify(notify.Info, "snapshot_restore_done", job.Volume+"/"+job.Target)
		case storage.RestoreFailed:
			notifier.Notify(notify.Error, "snapshot_restore_failed", job.Volume+"/"+job.Path)
		}
	}
	volumes.OnTestDone = func(test storage.DiskTest) {
		disk := test.Disk
		if test.Serial != "" {
			disk += " (" + test.Model + ", " + test.Serial + ")"
		}
		switch test.Result {
		case storage.TestPassed:
			notifier.Notify(notify.Info, "disk_test_passed", disk)
		case storage.TestFailed:
			notifier.Notify(notify.Error, "disk_test_failed", disk)
		}
	}
	// A test that a restart stopped and that waits to be continued.
	// Ransomware guard: a share that looks attacked becomes read-only, and
	// a Btrfs volume gets a snapshot of what was still intact.
	go folders.FollowAudit(ctx, func(share shares.Share) {
		notifier.Notify(notify.Error, "share_guard_locked", share.Name)
		if volumes.FilesystemOf(share.Volume) == "btrfs" {
			name := "guard-" + time.Now().UTC().Format("20060102T150405")
			if err := volumes.SnapshotAction(context.WithoutCancel(ctx), share.Volume, name, "create", ""); err != nil {
				slog.Error("snapshot after ransomware lock failed", "volume", share.Volume, "err", err)
			}
		}
	})
	// Previous versions for Windows follow the snapshots.
	go func() {
		for {
			volumes.SyncShadowCopies(ctx, folders.PreviousVersionVolumes())
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Minute):
			}
		}
	}()
	// Encrypted volumes: notice after a start, stopping and restarting
	// what runs on them around locking (encryption.go).
	wireEncryption(volumes, folders, machines, systemContainers, notifier)
	volumes.OnCacheMissing = func(volume string) { notifier.Notify(notify.Error, "volume_cache_missing", volume) }
	volumes.OnCacheRemoved = func(volume string) { notifier.Notify(notify.Info, "volume_cache_removed", volume) }
	go volumes.RunCacheCleaner(ctx)
	volumes.OnReshaped = func(volume string, err error) {
		if err != nil {
			notifier.Notify(notify.Error, "volume_disk_remove_failed", volume)
			return
		}
		notifier.Notify(notify.Info, "volume_disk_removed", volume)
	}
	volumes.OnTestInterrupted = func(test storage.DiskTest) {
		disk := test.Disk
		if test.Serial != "" {
			disk += " (" + test.Model + ", " + test.Serial + ")"
		}
		notifier.Notify(notify.Warning, "disk_test_waiting", disk)
	}

	// Copies of /config on a volume: the stick is the only place where
	// the configuration lives, and sticks wear out.
	stickBackup := configbackup.NewManager(store, version.Version)
	stickBackup.Disabled = opts.dev
	if err := stickBackup.Load(); err != nil {
		slog.Warn("backup settings not readable from /config", "err", err)
	}
	// A failure is reported once, and again only after a backup worked.
	backupFailed := false
	stickBackup.OnResult = func(volume string, err error) {
		if err != nil && !backupFailed {
			notifier.Notify(notify.Warning, "config_backup_failed", volume)
		}
		backupFailed = err != nil
	}
	stickBackup.OnNoVolume = func(volume string) {
		notifier.Notify(notify.Warning, "config_backup_no_volume", volume)
	}
	go stickBackup.Run(ctx, volumes.Mounted)

	// SSH is off unless switched on; then for administrators with a key.
	access := sshd.NewManager(store, shares.UsersGID)
	access.Disabled = opts.dev
	if err := access.Load(); err != nil {
		slog.Warn("SSH settings not readable from /config", "err", err)
	}
	go access.Run(ctx, users.List)

	// The certificate of the web UI: the administrator's own or one made here.
	var certs *tlscert.Manager
	if !opts.dev {
		hostname, _ := os.Hostname()
		var err error
		certs, err = tlscert.NewManager(store, hostname)
		if certs == nil {
			return fmt.Errorf("TLS certificate: %w", err)
		}
		if err != nil {
			slog.Warn("using a temporary TLS certificate", "err", err)
		}
		go watchCertificate(ctx, certs, notifier)
	}

	var power system.Power = system.SystemdPower{}
	if opts.dev {
		power = system.NoPower{}
	}
	// A shutdown after a power failure (UPS) is no planned one: the
	// healthcheck of IPv64 keeps watching then.
	upsPower := power
	// Planned shutdowns and restarts first pause the healthcheck of IPv64
	// (dynamic DNS, set further down), so that it raises no alarm.
	var beforePowerOff func()
	power = announcedPower{Power: power, before: func() {
		if beforePowerOff != nil {
			beforePowerOff()
		}
	}}

	// New versions go into the other slot of the stick and start on trial.
	slot := system.BootSlot()
	updates := update.NewManager(store, version.Version, slot)
	updates.Disabled = opts.dev || slot == ""
	// An encrypted volume waits for its password after the switch to a new
	// system: it must not make the boot check fail (and the update roll
	// back) just because nobody entered it within five minutes. It counts
	// as back, and so do plugins on it.
	updates.Mounted = func() []string { return append(volumes.Mounted(), volumes.WaitingForPassword(ctx)...) }
	// Going back to a system before encryption: it cannot open them, so
	// its boot check must not wait for them (see update.Pending.Volumes).
	updates.Encrypted = volumes.Encrypted
	updates.PluginsHealthy = func() bool {
		return extensions.Healthy() || slices.Contains(volumes.WaitingForPassword(ctx), extensions.Volume())
	}
	updates.PreparePlugins = func(ctx context.Context, version, kernel, kernelPackage, base string, assets map[string]string, fetch func(context.Context, string, int64) ([]byte, error), download func(context.Context, string, string, string, int64) error) error {
		return extensions.PrepareUpdate(ctx, version, kernel, kernelPackage, base, assets, volumes.Mounted(), fetch, download)
	}
	updates.Reboot = power.Reboot
	if !opts.dev && slot != "" && base != "" {
		updates.Base, updates.Apps = base, app.New("/efi", slot)
		// Both at once: the check waits for the new titand. From a unit of
		// its own, which the restart of titand does not take down with it.
		updates.Restart = func() error {
			_, err := (command.Exec{}).Run(context.Background(), "", "systemd-run", "--no-block", "--collect", "--quiet",
				"--description=TitanOS restart for an app update", "systemctl", "restart", "titand.service", "titanos-app-check.service")
			return err
		}
	}
	updates.OnEvent = func(kind, subject string) {
		severity := notify.Info
		if kind == "update_failed" {
			severity = notify.Error
		}
		notifier.Notify(severity, kind, subject)
	}
	if err := updates.Load(); err != nil {
		slog.Warn("update settings not readable from /config", "err", err)
	}
	updates.Confirm()
	extensions.ChangeAllowed = func() bool {
		status := updates.Status()
		return !tailnet.Pending() && !mesh.Pending() && status.Pending == nil && status.Progress.Phase != "download" && status.Progress.Phase != "switch"
	}
	extensions.UpdateAllowed = func() bool { return !tailnet.Pending() && !mesh.Pending() }
	extensions.CleanupProtected = updates.PluginReleaseProtection
	go extensions.Run(ctx, volumes.Mounted)
	go containers.Run(ctx, volumes.Mounted)
	// Backups of shares to another volume, another server or the cloud.
	upkeep := maintenance.New(store)
	machines.ScheduleAdmission = upkeep.Enter
	backups := backup.NewManager(store)
	backups.ScheduledStart = func(id string) error {
		leave, err := upkeep.Enter()
		if err != nil {
			return backup.ErrRunning
		}
		defer leave()
		return backups.Start(id)
	}
	backups.Disabled = opts.dev
	backups.Sources = func() []backup.Source {
		mounted := volumes.Mounted()
		sources := []backup.Source{}
		for _, share := range folders.List() {
			if slices.Contains(mounted, share.Volume) {
				sources = append(sources, backup.Source{Name: share.Name, Volume: share.Volume, Path: folders.Path(share)})
			}
		}
		return sources
	}
	backups.OnResult = func(job string, err error) {
		if err != nil {
			notifier.Notify(notify.Error, "backup_failed", job)
		}
	}
	backups.OnCheck = func(job string, err error) {
		if err != nil {
			notifier.Notify(notify.Error, "backup_check_failed", job)
		}
	}
	backups.OnProbe = func(job string, err error) {
		if err != nil {
			notifier.Notify(notify.Error, "backup_probe_failed", job)
		}
	}
	backups.OnOverdue = func(job string) {
		notifier.Notify(notify.Warning, "backup_overdue", job)
	}
	backups.OnIncomplete = func(job string) {
		notifier.Notify(notify.Warning, "backup_incomplete", job)
	}
	backups.Timezone = settings.CurrentTimezone
	appBackups := &appbackup.Manager{Store: store, Docker: containers, VMs: machines, Mounted: volumes.Mounted, MountRoot: "/mnt", Disabled: opts.dev}
	if err := appBackups.Load(); err != nil {
		slog.Error("application backup recovery unavailable", "err", err)
	}
	backups.PrepareApplications = appBackups.Prepare
	if err := backups.Load(); err != nil {
		slog.Warn("backup jobs not readable from /config", "err", err)
	}
	// Backups of other TitanOS, taken in over Tailscale.
	backupTarget := backuptarget.NewManager(store)
	backupTarget.Disabled = opts.dev
	backupTarget.Addresses = func() []string { return tailnet.Status().IPs }
	backupTarget.Host = func() string { return tailnet.Status().Name }
	backupTarget.Mounted = volumes.Mounted
	// Btrfs replication: received over the same server, sent by jobs.
	replicaReceiver := replication.NewReceiver()
	replicaReceiver.Mounted, replicaReceiver.Filesystem = volumes.Mounted, volumes.FilesystemOf
	backupTarget.Replicas = replicaReceiver.Serve
	backupTarget.Load()
	go backupTarget.Run(ctx)
	replicas := replication.NewManager(store)
	replicas.Disabled = opts.dev
	replicas.Mounted, replicas.Filesystem = volumes.Mounted, volumes.FilesystemOf
	replicas.OnResult = func(job replication.Job, err error) {
		if err != nil {
			notifier.Notify(notify.Error, "replication_failed", job.Name)
		}
	}
	if err := replicas.Load(); err != nil {
		slog.Warn("replication jobs not readable from /config", "err", err)
	}
	go replicas.Run(ctx)
	// Unmanaged data drives: shown, and mounted only on a click.
	drives := usb.NewManager()
	drives.Disabled = opts.dev
	drives.Claimed = func() map[string]string {
		claimed := machines.ClaimedDisks()
		for _, test := range volumes.Tests() {
			if test.Result == "" {
				claimed[test.Disk] = "disk test"
			}
		}
		return claimed
	}
	backups.WithUSB = drives.WithMounted
	go backups.Run(ctx)
	drives.Reserved = volumes.Owns
	drives.Busy = func() bool { return volumes.ActionsRunning() || volumes.GrowthActive() }
	drives.OnPlug = func(d usb.Device) { notifier.Notify(notify.Info, "usb_plugged", d.Name) }
	drives.OnFormatted = func(job usb.FormatJob) {
		switch {
		case job.Action != "" && job.Phase == "done":
			notifier.Notify(notify.Info, "usb_partitions_changed", job.Name)
		case job.Action != "":
			notifier.Notify(notify.Warning, "usb_partitions_failed", job.Name)
		case job.Phase == "done":
			notifier.Notify(notify.Info, "usb_formatted", job.Name)
		default:
			notifier.Notify(notify.Warning, "usb_format_failed", job.Name)
		}
	}
	go drives.Watch(ctx)

	// The UPS: on battery the NAS shuts down cleanly before it is empty.
	supply := ups.NewManager(store)
	supply.Disabled = opts.dev
	supply.Poweroff = upsPower.Poweroff
	supply.OnEvent = func(kind, subject string) {
		severity := notify.Warning
		switch kind {
		case "ups_online":
			severity = notify.Info
		case "ups_low_battery", "ups_shutdown":
			severity = notify.Error
		}
		notifier.Notify(severity, kind, subject)
	}
	if err := supply.Load(); err != nil {
		slog.Warn("UPS settings not readable from /config", "err", err)
	}
	go supply.Run(ctx)

	// The license texts lie in the image; in development mode in the repository.
	shareDir := "/usr/share/titanos"
	if opts.dev {
		shareDir = "."
	}
	nvidia := hardware.NewNvidiaControl(store, devices, opts.dev)
	if err := nvidia.Load(); err != nil {
		slog.Warn("NVIDIA settings unavailable", "err", err)
	}
	go nvidia.Run(ctx)
	// GuC/HuC of Intel graphics (Gen9 to Gen11): a kernel parameter in the
	// boot entries, written only when the setting changes.
	intelGuc := hardware.NewIntelGuc(store, devices, updates, opts.dev || updates.Disabled)
	if err := intelGuc.Load(); err != nil {
		slog.Warn("Intel GuC/HuC setting unavailable", "err", err)
	}
	go intelGuc.Run(ctx)
	powerButton := system.NewPowerButtonManager(store)
	powerButton.Disabled = opts.dev
	if err := powerButton.Load(); err != nil {
		slog.Warn("power button settings unavailable", "err", err)
	}
	energy := system.NewEnergyManager(store)
	energy.Disabled = opts.dev
	if err := energy.Load(); err != nil {
		slog.Warn("energy settings unavailable", "err", err)
	}
	// Shutting down and waking on a schedule, and Wake-on-LAN.
	schedule := system.NewPowerScheduler(store)
	schedule.Disabled = opts.dev
	schedule.Poweroff = power.Poweroff
	schedule.Notify = notifier.Notify
	if err := schedule.Load(); err != nil {
		slog.Warn("power schedule unavailable", "err", err)
	}
	// A VM is not started on its schedule shortly before the NAS shuts down.
	machines.PowerOffSoon = func() bool { return schedule.ShutdownSoon(15 * time.Minute) }
	machines.Notify = notifier.Notify
	monitor := system.NewMonitor(ctx)
	memoryPolicy := system.NewMemoryPolicy(store, system.ReadMemory)
	memoryPolicy.Disabled = opts.dev
	if err := memoryPolicy.Load(); err != nil {
		slog.Warn("memory budget unavailable", "err", err)
	}
	go memoryPolicy.Run(ctx)
	fileManager := files.NewManager(shares.UsersGID)
	fileManager.Protected = machines.ProtectFile
	if !opts.dev {
		fileManager.Mounted = volumes.Mounted
		fileManager.MountRoot = "/mnt"
	}
	fileManager.Acquire = memoryPolicy.Acquire
	fileManager.Available = func(root files.Root) bool { return fileRootAvailable(root, volumes.Mounted(), drives.Mounted()) }
	if !opts.dev {
		go fileManager.RunUploadSweep(ctx, volumes.Mounted)
	}
	// Thumbnails are made on request and cached on the volume of the file.
	previews := thumbs.New()
	if !opts.dev {
		previews.MountRoot, previews.Mounted = "/mnt", volumes.Mounted
	}
	// The full-text index is opt-in and runs only while a volume is in use.
	fullText := fileindex.New(store)
	fullText.Disabled = opts.dev
	fullText.MountRoot, fullText.Mounted = "/mnt", volumes.Mounted
	fullText.Protected = machines.ProtectFile
	fullText.Timezone = settings.CurrentTimezone
	fullText.ShareVolume = func(name string) (string, bool) {
		for _, share := range folders.List() {
			if share.Name == name {
				return share.Volume, true
			}
		}
		return "", false
	}
	if err := fullText.Load(); err != nil {
		slog.Warn("file index settings unavailable", "err", err)
	}
	go fullText.Run(ctx)
	history := monitoring.New(store)
	history.Mounted = volumes.Mounted
	history.Snapshot = func() (system.Info, storage.Status) {
		sampleCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		status, err := volumes.Status(sampleCtx)
		if err != nil {
			return monitor.Info(), storage.Status{}
		}
		return monitor.Info(), status
	}
	if err := history.Load(); err != nil {
		slog.Warn("monitoring history settings unavailable", "err", err)
	}
	if !opts.dev {
		go history.Run(ctx)
	}
	// The activity log stays in memory in development mode.
	audit := activity.New(store)
	if err := audit.Load(); err != nil {
		slog.Warn("activity log settings unavailable", "err", err)
	}
	if !opts.dev {
		audit.Mounted = volumes.Mounted
		go audit.Run(ctx)
	}
	// SSH logins with the key they came in with, from the journal of sshd.
	go access.FollowLogins(ctx, users.List, func(login sshd.Login) {
		action := "ssh_login"
		if login.Blocked {
			action = "ssh_login_blocked"
		}
		audit.Record(activity.Entry{At: login.At, User: login.User, Via: activity.ViaSSH, IP: login.IP, Action: action, Target: login.Target(), OK: !login.Blocked})
	})
	// The firewall learns the published ports of Docker apps and reports
	// automatic blocks and changes that went back.
	packetFilter.Apps = func(ctx context.Context) ([]firewall.App, bool) {
		status := containers.Status(ctx)
		if !status.Running {
			return nil, false
		}
		return firewallApps(status.Containers), true
	}
	// The networks own firewall zones are made of: logical networks of
	// Settings › Network, Docker networks, VM/LXC NAT bridges. While Docker
	// does not answer, its last known networks stay.
	var dockerNetworks []docker.EngineNetwork
	packetFilter.Networks = func(ctx context.Context) ([]firewall.Network, bool) {
		if list, err := containers.Networks(ctx); err == nil {
			dockerNetworks = list
		}
		return firewallNetworks(connection, dockerNetworks), true
	}
	packetFilter.OnBlock = func(block firewall.Block) {
		notifier.Notify(notify.Warning, "firewall_blocked", block.Address)
		audit.Record(activity.Entry{Via: activity.ViaSystem, IP: block.Address, Action: "firewall_block", Target: block.Source, OK: true})
	}
	packetFilter.OnRevert = func() {
		notifier.Notify(notify.Warning, "firewall_reverted", "")
		audit.Record(activity.Entry{Via: activity.ViaSystem, Action: "firewall_reverted", OK: true})
	}
	packetFilter.OnFailure = func(err error) {
		notifier.Notify(notify.Error, "firewall_failed", "")
	}
	go packetFilter.Run(ctx)
	go packetFilter.FollowJournal(ctx)
	machines.Activity = func(action, target string, ok bool) {
		audit.Record(activity.Entry{Via: activity.ViaSystem, Action: action, Target: target, OK: ok})
	}
	// Certificates from Let's Encrypt, served next to the own or
	// self-signed one without a restart; renewed by themselves.
	acmeCerts := letsencrypt.New(store)
	acmeCerts.Disabled = opts.dev || certs == nil
	acmeCerts.HTTPPort = !opts.dev && opts.httpAddr != ""
	acmeCerts.AppDomain = func() string { return routes.Settings().Domain }
	acmeCerts.AppNames = routes.Domains
	// New full domains of app routes join the certificate soon.
	kickFolders := routes.OnChange
	routes.OnChange = func() {
		if kickFolders != nil {
			kickFolders()
		}
		acmeCerts.NamesChanged()
	}
	acmeCerts.Install = func(cert *tls.Certificate) {
		if certs != nil {
			certs.SetACME(cert)
		}
	}
	acmeCerts.OnEvent = func(severity, kind, subject string) { notifier.Notify(severity, kind, subject) }
	acmeCerts.Activity = func(action, target string, ok bool) {
		audit.Record(activity.Entry{Via: activity.ViaSystem, Action: action, Target: target, OK: ok})
	}
	if err := acmeCerts.Load(); err != nil {
		slog.Warn("Let's Encrypt settings not readable from /config", "err", err)
	}
	go acmeCerts.Run(ctx)
	// The NAS's own name in the tailnet gets the certificate Tailscale hands
	// out once HTTPS is on in the tailnet (passkeys and web push need it).
	if certs != nil {
		go tailnet.RunCertificate(ctx, certs.SetTailnet)
	}
	// Dynamic DNS: host names at the public addresses of the NAS.
	dynamicDNS := ddns.New(store)
	dynamicDNS.Disabled = opts.dev
	dynamicDNS.OnEvent = acmeCerts.OnEvent
	dynamicDNS.Activity = acmeCerts.Activity
	if err := dynamicDNS.Load(); err != nil {
		slog.Warn("dynamic DNS settings not readable from /config", "err", err)
	}
	go dynamicDNS.Run(ctx)
	beforePowerOff = func() { dynamicDNS.PauseForShutdown(context.Background()) }
	// The IPv64.net account beyond dynamic DNS: notifications from polling,
	// the v64 blocklist for the firewall and the WireGuard tunnel into a
	// Cloud Router; all off until switched on.
	account64 := ipv64.NewManager(store)
	account64.Disabled = opts.dev
	account64.Fallback = func() (string, string) { return dynamicDNS.IPv64Key(), acmeCerts.IPv64Key() }
	account64.Notify = func(severity, kind, subject string) { notifier.Notify(severity, kind, subject) }
	account64.Firewall = packetFilter
	if err := account64.Load(ctx); err != nil {
		slog.Warn("IPv64 settings not readable from /config", "err", err)
	}
	go account64.Run(ctx)
	// The WireGuard server of the NAS, off unless switched on.
	vpnServer := wgserver.New(store)
	vpnServer.Disabled = opts.dev
	vpnServer.HomeNetworks = sshd.HomeNetworks
	vpnServer.SuggestEndpoint = func() string {
		if names := dynamicDNS.Hostnames(); len(names) > 0 {
			return names[0]
		}
		return ""
	}
	if err := vpnServer.Load(); err != nil {
		slog.Warn("WireGuard settings not readable from /config", "err", err)
	} else if err := vpnServer.Apply(ctx); err != nil {
		slog.Warn("WireGuard server not up", "err", err)
	}
	// A summary of the NAS, weekly or monthly, off unless switched on.
	report := healthreport.New(store)
	report.Timezone = settings.CurrentTimezone
	report.Language = func() string { general, _ := notifier.Settings(); return general.Language }
	var reportSince atomic.Int64 // the last report, Unix nanoseconds
	reportSince.Store(time.Now().UnixNano())
	report.Collect = func() healthreport.Data {
		d := healthreport.Data{Version: version.Version, MemoryPercent: -1, Since: time.Unix(0, reportSince.Load())}
		info := monitor.Info()
		d.Uptime = time.Duration(info.UptimeSeconds) * time.Second
		d.CPUWatts = info.CPU.PowerWatts
		if mem := monitor.Memory(); mem.TotalBytes > 0 {
			d.MemoryPercent = int((mem.TotalBytes - mem.AvailableBytes) * 100 / mem.TotalBytes)
		}
		check, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if status, err := volumes.Status(check); err == nil {
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
		for _, view := range backups.CachedViews() {
			d.Backups = append(d.Backups, healthreport.Backup{Name: view.Job.ID, Enabled: view.Job.Enabled, LastOK: view.State.LastOK, LastError: view.State.LastError, Overdue: backups.Overdue(view)})
		}
		if available := updates.Status().Available; available != nil {
			d.UpdateAvailable = available.Version
		}
		if u := supply.Status(); u.Mode != "" && u.Mode != "none" {
			d.UPS = &healthreport.UPS{Connected: u.Connected, OnBattery: u.OnBattery, Charge: u.Charge}
		}
		for _, message := range notifier.Messages() {
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
	report.Send = func(title, text, status string) error {
		if err := notifier.SendReport(title, text); err != nil {
			return err
		}
		reportSince.Store(time.Now().UnixNano())
		return nil
	}
	if err := report.Load(); err != nil {
		slog.Warn("health report settings not readable from /config", "err", err)
	}
	go report.Run(ctx)
	// Fans: left to the BIOS unless a fan gets a speed or a curve.
	coolers := fans.New(store)
	coolers.Disabled = opts.dev
	coolers.Temperatures = func() map[string]float64 {
		out := map[string]float64{}
		for _, sensor := range monitor.Info().Temperatures {
			if sensor.Celsius != nil {
				out[sensor.ID] = *sensor.Celsius
			}
		}
		return out
	}
	coolers.DiskMax = func() (float64, bool) {
		c, ok := volumes.HottestDisk()
		return float64(c), ok
	}
	if err := coolers.Load(); err != nil {
		slog.Warn("fan settings not readable from /config", "err", err)
	}
	go coolers.Run(ctx)
	upkeep.Disabled = opts.dev
	mediaImports := media.New(ctx, volumes.Mounted, shares.UsersGID)
	mediaImports.Disabled = opts.dev
	defer mediaImports.Close()
	upkeep.Mounted = volumes.Mounted
	upkeep.ExternalBusy = func(job maintenance.Job) bool {
		if appBackups.Busy() || containers.Busy() || backups.Busy() || volumes.TestsRunning() || volumes.SnapshotRestoresRunning() || volumes.GrowthPending() || volumes.Reshaping() || replicas.Busy() || machines.Busy() || systemContainers.Busy() || fileManager.Busy() || extensions.Busy() || mediaImports.Working() || folders.PurgeBusy() {
			return true
		}
		check, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		status, err := volumes.Status(check)
		if err != nil {
			return true
		}
		return slices.ContainsFunc(status.Volumes, func(v storage.Volume) bool {
			return v.State == storage.VolumeRebuilding || v.State == storage.VolumeSyncing || v.State == storage.VolumeChecking
		})
	}
	upkeep.Conflict = func(job maintenance.Job) string {
		// The callback must not call upkeep.Status: admission already holds its lock.
		if backups.DueSoon(time.Now(), 5*time.Minute) {
			return "backup-priority"
		}
		return ""
	}
	upkeep.OnResult = func(result maintenance.Result) {
		if result.Error != "" {
			notifier.Notify(notify.Error, "maintenance_failed", result.Name)
		}
	}
	upkeep.Execute = func(work context.Context, job maintenance.Job) error {
		switch job.Kind {
		case "raid-check":
			return volumes.MaintenanceCheck(work, job.Target)
		case "smart-long":
			return volumes.LongSMART(work, job.Target)
		case "backup-check", "restore-probe":
			return backups.MaintenanceRun(work, job.Target, job.Kind, job.ReadData, backup.ProbeRequest{MaxBytes: job.MaxBytes, MaxFiles: job.MaxFiles})
		case "snapshot":
			if err := volumes.ScheduledSnapshot(work, job.Target, job.ID, job.Keep, job.LockDays); err != nil {
				return err
			}
			volumes.SyncShadowCopies(work, folders.PreviousVersionVolumes())
			return nil
		}
		return errors.New("unknown maintenance kind")
	}
	if err := upkeep.Load(); err != nil {
		slog.Warn("maintenance settings unavailable", "err", err)
	}
	go upkeep.Run(ctx)

	// An app update restarts titand: it waits for work that would break off.
	updates.Working = func() bool {
		return upkeep.Busy() || appBackups.Busy() || volumes.ActionsRunning() || volumes.SnapshotRestoresRunning() || volumes.GrowthPending() || volumes.Reshaping() || replicas.Busy() || fileManager.Busy() || backups.Busy() || volumes.TestsRunning() || machines.Busy() || systemContainers.Busy() || extensions.Busy() || mediaImports.Working() || folders.PurgeBusy()
	}
	go machines.Run(ctx, volumes.Mounted)
	go systemContainers.Run(ctx, volumes.Mounted)
	go updates.Run(ctx)
	// Logins outlive a restart of titand in RAM, as after an app update.
	sessions := auth.NewSessions()
	if !opts.dev {
		if err := sessions.Persist("/run/titanos/sessions.json"); err != nil {
			slog.Warn("logins of before not taken over", "err", err)
		}
	}
	diagnostics := diagnose.NewManager(store, version.Version)
	diagnostics.SMART = func(ctx context.Context) any { return volumes.SmartDumps(ctx) }
	diagnostics.IntelGPU = intelGuc.Report
	handler := api.New(api.Options{
		Media:              mediaImports,
		UI:                 webui.FS(),
		Store:              store,
		Users:              users,
		Monitor:            monitor,
		Monitoring:         history,
		Maintenance:        upkeep,
		ApplicationBackups: appBackups,
		Storage:            volumes,
		Shares:             folders,
		Files:              fileManager,
		Thumbs:             previews,
		FileIndex:          fullText,
		Docker:             containers,
		AppProxy:           routes,
		Catalog:            appCatalog,
		Terminal:           shells,
		VMs:                machines,
		LXC:                systemContainers,
		Hardware:           devices,
		Nvidia:             nvidia,
		IntelGuc:           intelGuc,
		Accelerators:       hardware.NewAcceleratorMonitor(ctx, devices),
		Plugins:            extensions,
		Tailscale:          tailnet,
		Netbird:            mesh,
		Network:            connection,
		Firewall:           packetFilter,
		IPv64:              account64,
		IPv64Reserved:      reservedPorts(opts),
		Notify:             notifier,
		WebPush:            pushes,
		WireGuard:          vpnServer,
		HealthReport:       report,
		Fans:               coolers,
		HomeAssistant:      home,
		ConfigBackup:       stickBackup,
		USB:                drives,
		BackupTarget:       backupTarget,
		Replication:        replicas,
		ReplicaReceiver:    replicaReceiver,
		Diagnose:           diagnostics,
		SSH:                access,
		Update:             updates,
		Backup:             backups,
		UPS:                supply,
		Certificate:        certs,
		LetsEncrypt:        acmeCerts,
		DDNS:               dynamicDNS,
		Settings:           settings,
		Energy:             energy,
		PowerSchedule:      schedule,
		PowerButton:        powerButton,
		MemoryPolicy:       memoryPolicy,
		Power:              power,
		Version:            version.Version,
		ShareDir:           shareDir,
		SecureCookies:      !opts.dev,
		Sessions:           sessions,
		Assistant:          chat,
		Activity:           audit,
	})

	agentDone := make(chan struct{})
	go func() { defer close(agentDone); handler.RunAssistant(ctx) }()
	go home.RunMQTT(ctx, handler.HASnapshot, handler.HACommand)
	schedule.Busy = handler.PowerBusy
	go schedule.Run(ctx)

	var servers []*http.Server
	errc := make(chan error, 2)
	serve := func(srv *http.Server, useTLS bool) {
		servers = append(servers, srv)
		go func() {
			if useTLS {
				errc <- srv.ListenAndServeTLS("", "")
			} else {
				errc <- srv.ListenAndServe()
			}
		}()
	}

	// Requests for the HTTPS name of an app go to the app, all others to
	// the web UI; see package appproxy.
	site := routes.Handler(handler)
	if opts.dev {
		serve(newServer(opts.devAddr, site), false)
		slog.Info("titand started in development mode", "version", version.Version, "listen", opts.devAddr, "config", opts.configDir, "webui", webui.Embedded)
	} else {
		https := newServer(opts.httpsAddr, site)
		https.TLSConfig = &tls.Config{GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			if names := routes.CertificateNames(hello.ServerName); names != nil {
				return certs.ForNames(hello.ServerName, names)
			}
			return certs.GetCertificate(hello)
		}, MinVersion: tls.VersionTLS12}
		serve(https, true)
		if opts.httpAddr != "" {
			// Port 80 also answers the HTTP challenge of Let's Encrypt.
			serve(newServer(opts.httpAddr, acmeCerts.HTTPHandler(redirectToHTTPS(opts.httpsAddr))), false)
		}
		slog.Info("titand started", "version", version.Version, "https", opts.httpsAddr, "http", opts.httpAddr, "config", opts.configDir, "webui", webui.Embedded)
	}

	var err error
	select {
	case err = <-errc:
	case <-ctx.Done():
		slog.Info("shutting down")
	}

	stop()
	sessions.Flush()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, srv := range servers {
		if shutdownErr := srv.Shutdown(shutdownCtx); shutdownErr != nil && err == nil {
			err = shutdownErr
		}
	}
	select {
	case <-agentDone:
	case <-shutdownCtx.Done():
	}
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return err
}

// waitReady asks the running titand until it says it is ready. A system
// that was just updated is only marked good after that.
func waitReady(httpsAddr string, patience time.Duration) error {
	_, port, err := net.SplitHostPort(httpsAddr)
	if err != nil {
		return err
	}
	// The certificate is the NAS's own, and this never leaves the machine.
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	deadline := time.Now().Add(patience)
	last := errors.New("no answer")
	for time.Now().Before(deadline) {
		res, err := client.Get("https://" + net.JoinHostPort("127.0.0.1", port) + "/api/v1/health")
		if err == nil {
			var health struct{ Ready bool }
			err = json.NewDecoder(res.Body).Decode(&health)
			res.Body.Close()
			if err == nil && health.Ready {
				return nil
			}
			if err == nil {
				err = errors.New("the configuration or a volume of before is not there yet")
			}
		}
		last = err
		time.Sleep(3 * time.Second)
	}
	return last
}

// watchCertificate warns once when an own certificate is about to run
// out: unlike the one TitanOS makes, nobody renews it by itself.
func watchCertificate(ctx context.Context, certs *tlscert.Manager, notifier *notify.Manager) {
	ticker := time.NewTicker(12 * time.Hour)
	defer ticker.Stop()
	warned := ""
	for {
		info := certs.Info()
		if info.Kind == tlscert.KindCustom && time.Until(info.NotAfter) < 14*24*time.Hour && warned != info.Fingerprint {
			warned = info.Fingerprint
			notifier.Notify(notify.Warning, "certificate_expiring", info.NotAfter.Format(time.DateOnly))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// proxyPorts returns the HTTPS port for links to apps (0 for the usual
// 443) and the ports of titand an app route must not point at.
func proxyPorts(opts options) (int, []int) {
	var https int
	var reserved []int
	addrs := []string{opts.httpsAddr, opts.httpAddr}
	if opts.dev {
		addrs = []string{opts.devAddr}
	}
	for _, addr := range addrs {
		_, p, err := net.SplitHostPort(addr)
		if err != nil {
			continue
		}
		if port, err := strconv.Atoi(p); err == nil {
			reserved = append(reserved, port)
			if addr == opts.httpsAddr && port != 443 {
				https = port
			}
		}
	}
	return https, reserved
}

// reservedPorts are the ports of titand that are never published.
func reservedPorts(opts options) []int {
	_, reserved := proxyPorts(opts)
	return reserved
}

// webPorts are the ports of the web UI for the firewall.
func webPorts(opts options) firewall.WebPorts {
	ports := firewall.WebPorts{HTTP: -1}
	if _, p, err := net.SplitHostPort(opts.httpsAddr); err == nil {
		ports.HTTPS, _ = strconv.Atoi(p)
	}
	if opts.httpAddr != "" {
		if _, p, err := net.SplitHostPort(opts.httpAddr); err == nil {
			ports.HTTP, _ = strconv.Atoi(p)
		}
	}
	return ports
}

// firewallNetworks lists the networks of the NAS for own firewall zones.
func firewallNetworks(connection *network.Manager, engine []docker.EngineNetwork) []firewall.Network {
	out := []firewall.Network{}
	for _, n := range connection.Connections() {
		dev, ok := connection.Device(n.ID, false)
		if !ok {
			continue
		}
		name := n.Name
		if name == "" {
			name = n.ID
		}
		out = append(out, firewall.Network{ID: "net:" + n.ID, Kind: firewall.KindNetwork, Name: name, Device: dev, Subnets: interfaceSubnets(dev)})
	}
	for _, n := range engine {
		kind := firewall.KindDocker
		if n.Device == "" {
			kind = firewall.KindDockerLAN
		}
		out = append(out, firewall.Network{ID: "docker:" + n.Name, Kind: kind, Name: n.Name, Device: n.Device, Parent: n.Parent, Subnets: n.Subnets})
	}
	// NAT bridges of libvirt or LXC, where someone set them up by hand:
	// TitanOS's own VMs use the QEMU user network or a bridge of the LAN.
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if strings.HasPrefix(iface.Name, "virbr") || strings.HasPrefix(iface.Name, "lxcbr") {
				out = append(out, firewall.Network{ID: "vm:" + iface.Name, Kind: firewall.KindVM, Name: iface.Name, Device: iface.Name, Subnets: interfaceSubnets(iface.Name)})
			}
		}
	}
	return out
}

// interfaceSubnets are the networks of the addresses of an interface,
// without link-local ones.
func interfaceSubnets(name string) []string {
	out := []string{}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return out
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLinkLocalUnicast() {
			continue
		}
		masked := (&net.IPNet{IP: ipnet.IP.Mask(ipnet.Mask), Mask: ipnet.Mask}).String()
		if !slices.Contains(out, masked) {
			out = append(out, masked)
		}
	}
	return out
}

// firewallApps groups the published ports of containers by app: the
// Compose project, or the container itself.
func firewallApps(containers []docker.Container) []firewall.App {
	byKey := map[string]*firewall.App{}
	var keys []string
	for _, c := range containers {
		key, name := c.App, c.App
		if key == "" {
			key, name = "container:"+c.Name, c.Name
		}
		app := byKey[key]
		if app == nil {
			app = &firewall.App{Key: key, Name: name, Ports: []firewall.AppPort{}}
			byKey[key] = app
			keys = append(keys, key)
		}
		app.HostNetwork = app.HostNetwork || c.HostNetwork
		for _, p := range c.Ports {
			port := firewall.AppPort{Port: p.Host, Protocol: p.Protocol}
			if !slices.Contains(app.Ports, port) {
				app.Ports = append(app.Ports, port)
			}
		}
	}
	sort.Strings(keys)
	out := make([]firewall.App, 0, len(keys))
	for _, key := range keys {
		app := byKey[key]
		sort.Slice(app.Ports, func(i, j int) bool {
			if app.Ports[i].Port != app.Ports[j].Port {
				return app.Ports[i].Port < app.Ports[j].Port
			}
			return app.Ports[i].Protocol < app.Ports[j].Protocol
		})
		out = append(out, *app)
	}
	return out
}

func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
}

// redirectToHTTPS sends every plain HTTP request to the same path on the
// HTTPS address.
func redirectToHTTPS(httpsAddr string) http.Handler {
	_, port, _ := net.SplitHostPort(httpsAddr)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if port != "" && port != "443" {
			host = net.JoinHostPort(host, port)
		}
		http.Redirect(w, r, "https://"+host+r.URL.RequestURI(), http.StatusPermanentRedirect)
	})
}

// announcedPower runs before first, for planned shutdowns and restarts.
type announcedPower struct {
	system.Power
	before func()
}

func (p announcedPower) Reboot() error {
	p.before()
	return p.Power.Reboot()
}

func (p announcedPower) Poweroff() error {
	p.before()
	return p.Power.Poweroff()
}
