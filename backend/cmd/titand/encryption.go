package main

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/lxc"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/vm"
)

// vmStopWait is how long locking a volume waits for its VMs to shut down
// before it switches them off.
const vmStopWait = 2 * time.Minute

// wireEncryption connects encrypted volumes (storage/crypt.go) with what
// lives on them. Shares, Docker, backups and the rest follow
// volumes.Mounted by themselves: a locked volume is not mounted, and one
// being locked no longer counts as mounted. VMs and LXC containers do not
// stop on their own, so locking stops them first, and unlocking starts
// again the ones locking stopped. Autostart after a start of the NAS
// happens as usual once the volume is unlocked.
func wireEncryption(volumes *storage.Manager, folders *shares.Manager, machines *vm.Manager, containers *lxc.Manager, notifier *notify.Manager) {
	var mu sync.Mutex
	stopped := map[string][]string{} // volume → "vm:<name>" / "lxc:<name>"

	volumes.OnLocked = func(volume string) { notifier.Notify(notify.Warning, "volume_locked", volume) }
	volumes.BeforeLock = func(ctx context.Context, volume string) (func(), error) {
		folders.LetGo(ctx, volume)
		mounted := volumes.Mounted() // without volume already
		with := append(slices.Clone(mounted), volume)
		var names []string
		running := machines.RunningNames()
		without := machinesAvailable(machines, mounted)
		for _, status := range machines.List(with) {
			// Running and needing this volume.
			if !running[status.Name] || !status.Available || slices.Contains(without, status.Name) {
				continue
			}
			slog.Info("shutting down VM before locking its volume", "vm", status.Name, "volume", volume)
			if err := machines.Action(ctx, status.Name, "shutdown"); err != nil {
				slog.Warn("VM shutdown before lock", "vm", status.Name, "err", err)
			}
			names = append(names, "vm:"+status.Name)
		}
		for _, status := range containers.List(with) {
			if status.State != "running" || status.Volume != volume {
				continue
			}
			slog.Info("stopping LXC container before locking its volume", "container", status.Name, "volume", volume)
			if err := containers.Action(ctx, status.Name, "stop", with); err != nil {
				go restart(context.WithoutCancel(ctx), volumes, machines, containers, names)
				return nil, fmt.Errorf("%w: container %s did not stop: %v", storage.ErrBusy, status.Name, err)
			}
			names = append(names, "lxc:"+status.Name)
		}
		// Wait for the VMs, then switch off what is left.
		deadline := time.Now().Add(vmStopWait)
		for _, entry := range names {
			name, isVM := strings.CutPrefix(entry, "vm:")
			if !isVM {
				continue
			}
			for machines.RunningNames()[name] && time.Now().Before(deadline) {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(2 * time.Second):
				}
			}
			if machines.RunningNames()[name] {
				slog.Warn("VM did not shut down, switching it off before locking", "vm", name)
				if err := machines.Action(ctx, name, "poweroff"); err != nil {
					go restart(context.WithoutCancel(ctx), volumes, machines, containers, names)
					return nil, fmt.Errorf("%w: VM %s still runs: %v", storage.ErrBusy, name, err)
				}
			}
		}
		mu.Lock()
		stopped[volume] = names
		mu.Unlock()
		// In the background: the volume counts as mounted again only once
		// Lock has returned.
		undo := func() {
			mu.Lock()
			delete(stopped, volume)
			mu.Unlock()
			go restart(context.WithoutCancel(ctx), volumes, machines, containers, names)
		}
		return undo, nil
	}
	volumes.OnUnlocked = func(volume string) {
		mu.Lock()
		names := stopped[volume]
		delete(stopped, volume)
		mu.Unlock()
		folders.Kick()
		if len(names) > 0 {
			go restart(context.Background(), volumes, machines, containers, names)
		}
	}
}

func machinesAvailable(machines *vm.Manager, mounted []string) []string {
	var names []string
	for _, status := range machines.List(mounted) {
		if status.Available {
			names = append(names, status.Name)
		}
	}
	return names
}

// restart starts the VMs and containers a lock stopped.
func restart(ctx context.Context, volumes *storage.Manager, machines *vm.Manager, containers *lxc.Manager, names []string) {
	// The volume is mounted again before this runs; give shares and the
	// rest a moment.
	time.Sleep(2 * time.Second)
	for _, entry := range names {
		if name, ok := strings.CutPrefix(entry, "vm:"); ok {
			if err := machines.Start(ctx, name, volumes.Mounted()); err != nil {
				slog.Warn("VM not started again after unlocking", "vm", name, "err", err)
			}
		} else if name, ok := strings.CutPrefix(entry, "lxc:"); ok {
			if err := containers.Action(ctx, name, "start", volumes.Mounted()); err != nil {
				slog.Warn("container not started again after unlocking", "container", name, "err", err)
			}
		}
	}
}
