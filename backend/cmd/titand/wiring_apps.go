package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/ra5on/TitanOS/backend/internal/startup"
	"log/slog"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/catalog"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/lxc"
	"github.com/ra5on/TitanOS/backend/internal/netbird"
	"github.com/ra5on/TitanOS/backend/internal/network"
	"github.com/ra5on/TitanOS/backend/internal/plugins"
	command "github.com/ra5on/TitanOS/backend/internal/run"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/tailscale"
	"github.com/ra5on/TitanOS/backend/internal/update"
	"github.com/ra5on/TitanOS/backend/internal/version"
	"github.com/ra5on/TitanOS/backend/internal/vm"
)

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
	// Both kernel variants of one version keep their plugins apart.
	if update.ReadVariant(update.VariantFile) == update.VariantNew {
		w.extensions.Variant = update.VariantNew
	}
	w.extensions.Disabled = w.opts.dev
	if err := w.extensions.Load(); err != nil {
		slog.Warn("plugin settings not readable from /config", "err", err)
	}
	// PLAN 73: Docker and VM autostart wait until TitanOS is ready – the
	// volumes settled and the shares served – plus the owner's extra wait.
	w.start = startup.NewManager(w.store)
	w.start.Settled = w.volumes.Settled
	w.start.SharesApplied = w.folders.LastApplied
	w.containers.Ready = func() bool { return w.extensions.Healthy() && w.start.Ready() }
	w.machines.AutoReady = func() bool {
		return w.extensions.Healthy() && w.containers.StartupReady() && w.start.Ready()
	}
	w.machines.Ordered = func(name string) bool { return w.start.Ordered("vm", name) }
	// The owner's order: VMs and Docker apps one after the other, with waits.
	w.background(func() {
		w.start.Run(ctx, startup.Steps{
			EngineReady: func() bool { return w.extensions.Healthy() && w.containers.StartupReady() },
			StartVM: func(ctx context.Context, name string) error {
				return w.machines.StartOrdered(ctx, name, w.volumes.Mounted())
			},
			StartApp: w.containers.StartApp,
			Restore: func(ctx context.Context, held map[string]startup.Policy) error {
				return w.containers.RestorePolicies(ctx, dockerPolicies(held))
			},
			DoneFile: "/run/titanos/startup-order-done",
		})
	})
	return nil
}

func dockerPolicies(held map[string]startup.Policy) map[string]docker.HeldPolicy {
	out := make(map[string]docker.HeldPolicy, len(held))
	for id, p := range held {
		out[id] = docker.HeldPolicy{Name: p.Name, MaxRetries: p.MaxRetries}
	}
	return out
}

// holdOrderedApps runs just before the engine stops for a shutdown or
// restart of the NAS (PLAN 73): the apps of the start order get restart
// policy "no" so that titand can start them in order at boot. Not while a
// system update or a step back waits: that version may not know the order.
func (w *titand) holdOrderedApps(ctx context.Context) {
	apps := w.start.Apps()
	if len(apps) == 0 || w.containers.Disabled {
		return
	}
	if w.updates != nil && w.updates.Status().Pending != nil {
		slog.Info("start order left out: a system change waits for the restart")
		return
	}
	held, err := w.containers.HoldApps(ctx, apps)
	policies := make(map[string]startup.Policy, len(held))
	for id, p := range held {
		policies[id] = startup.Policy{Name: p.Name, MaxRetries: p.MaxRetries}
	}
	// Written even after an error: what was changed must be given back.
	if saveErr := w.start.Hold(policies); saveErr != nil {
		slog.Error("start order: policies not noted, giving them back", "err", saveErr)
		_ = w.containers.RestorePolicies(ctx, held)
		return
	}
	if err != nil {
		slog.Warn("start order: not every app held", "err", err)
	}
}
