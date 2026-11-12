package lxc

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/diskwake"
)

// Changing, starting, stopping and deleting containers, consoles
// (split from lxc.go, PLAN 75-12).

// Change is what can be changed in a container after it was made.
type Change struct {
	DisplayName *string `json:"displayName"`
	Description string  `json:"description"`
	CPUs        int     `json:"cpus"`
	MemoryMiB   int     `json:"memoryMiB"`
	Network     string  `json:"network"`
	MAC         string  `json:"mac"`
	Mounts      []Mount `json:"mounts"`
	Autostart   bool    `json:"autostart"`
}

// Update changes the settings of a container. A running container gets
// them with its next start.
func (m *Manager) Update(name string, change Change) error {
	lock, err := m.operation(name)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	c, err := m.get(name)
	if err != nil {
		return err
	}
	if change.DisplayName != nil {
		c.DisplayName = *change.DisplayName
	}
	c.Description, c.CPUs, c.MemoryMiB, c.Network, c.MAC, c.Mounts, c.Autostart = change.Description, change.CPUs, change.MemoryMiB, change.Network, change.MAC, change.Mounts, change.Autostart
	if err := m.check(&c); err != nil {
		return err
	}
	if err := m.store.WriteJSON(configDir+"/"+name+".json", c); err != nil {
		return err
	}
	m.mu.Lock()
	m.containers[name] = c
	m.mu.Unlock()
	return nil
}

// Start writes the configuration of a container and starts it as an
// instance of titanos-lxc@.service.
func (m *Manager) Start(ctx context.Context, name string, mounted []string) error {
	if m.off.Load() {
		return ErrServiceOff
	}
	if m.Disabled {
		return ErrDisabled
	}
	lock, err := m.operation(name)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	c, err := m.get(name)
	if err != nil {
		return err
	}
	if m.creating(name) {
		return ErrCreating
	}
	if !available(c, mounted) {
		return ErrUnknownVolume
	}
	if m.running(name) != "" {
		return ErrRunning
	}
	diskwake.Note(c.Volume, diskwake.LXCStart, name)
	if c.Network != "none" {
		if !m.networkReady(c) {
			return ErrNoBridge
		}
		if m.NetworkLease != nil {
			release, err := m.NetworkLease()
			if err != nil {
				return ErrNoBridge
			}
			defer release()
		}
	}
	// The folders may have gone since they were chosen.
	if err := m.checkMounts(&c); err != nil {
		return err
	}
	if err := m.writeConfig(c); err != nil {
		return err
	}
	if err := os.MkdirAll(m.envDir, 0o700); err != nil {
		return err
	}
	env := "LXC_PATH=" + m.lxcPath(c.Volume) + "\n"
	if err := os.WriteFile(filepath.Join(m.envDir, name+".env"), []byte(env), 0o600); err != nil {
		return err
	}
	_, err = m.runner.Run(ctx, "", "systemctl", "start", m.unit(name))
	return err
}

// Action stops ("stop"), forcibly stops ("kill") or restarts ("restart") a container.
func (m *Manager) Action(ctx context.Context, name, action string, mounted []string) error {
	if m.Disabled {
		return ErrDisabled
	}
	if action == "start" {
		return m.Start(ctx, name, mounted)
	}
	if action != "stop" && action != "kill" && action != "restart" {
		return ErrNotFound
	}
	lock, err := m.operation(name)
	if err != nil {
		return err
	}
	c, err := m.get(name)
	if err != nil {
		lock.Unlock()
		return err
	}
	if m.running(name) == "" {
		lock.Unlock()
		return ErrNotRunning
	}
	if action == "kill" {
		// Pulling the plug, then the unit notices its end.
		if _, err := m.runner.Run(ctx, "", "lxc-stop", "-P", m.lxcPath(c.Volume), "-n", name, "-k"); err != nil {
			lock.Unlock()
			return err
		}
	}
	// The unit's ExecStop asks the container to shut down, for up to a minute.
	_, err = m.runner.Run(ctx, "", "systemctl", "stop", m.unit(name))
	lock.Unlock()
	if err != nil || action != "restart" {
		return err
	}
	return m.Start(ctx, name, mounted)
}

// mountedBelow reports whether anything is mounted in or below a folder.
func (m *Manager) mountedBelow(dir string) bool {
	data, err := os.ReadFile(m.mountInfo)
	if err != nil {
		// Unknown: assume the worst.
		return true
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		point := strings.ReplaceAll(fields[4], `\040`, " ")
		if point == dir || strings.HasPrefix(point, dir+"/") {
			return true
		}
	}
	return false
}

// Delete forgets a container. Its folder stays unless deleteData is set,
// which needs the name of the container once more as confirmation.
func (m *Manager) Delete(ctx context.Context, name string, deleteData bool, confirm string) error {
	if m.Disabled {
		return ErrDisabled
	}
	lock, err := m.operation(name)
	if err != nil {
		if m.creating(name) {
			return ErrCreating
		}
		return err
	}
	defer lock.Unlock()
	c, err := m.get(name)
	if err != nil {
		return err
	}
	if m.running(name) != "" {
		return ErrRunning
	}
	if deleteData && confirm != name && confirm != c.Label() {
		return ErrNotConfirmed
	}
	dir := m.dir(c)
	if deleteData {
		// A stopped container has nothing mounted; anything that still is
		// could be a folder of the NAS, which must not be deleted with it.
		if m.mountedBelow(dir) {
			return ErrMounted
		}
	}
	if err := m.store.Remove(configDir + "/" + name + ".json"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.mu.Lock()
	delete(m.containers, name)
	delete(m.jobs, name)
	delete(m.samples, name)
	delete(m.autostarted, name)
	m.mu.Unlock()
	os.Remove(filepath.Join(m.envDir, name+".env"))
	if !deleteData {
		return nil
	}
	if c.Btrfs {
		// Snapshots and the root filesystem are subvolumes.
		snapshots, _ := m.snapshotNames(c)
		for _, id := range snapshots {
			_, _ = m.runner.Run(ctx, "", "btrfs", "subvolume", "delete", filepath.Join(m.snapshotDir(c), id)) // cleanup, best effort: nothing more can be done if it fails
		}
		if _, err := os.Lstat(m.rootfs(c)); err == nil {
			if _, err := m.runner.Run(ctx, "", "btrfs", "subvolume", "delete", m.rootfs(c)); err != nil {
				slog.Warn("root filesystem of a removed container is no subvolume", "container", name, "err", err)
			}
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		slog.Warn("folder of a removed container not deleted", "container", name, "err", err)
		return err
	}
	return nil
}

// ConsoleCommand returns the command for a console of a running
// container: a login shell inside it ("shell"), or its own text console
// ("console", leave with Ctrl+a q).
func (m *Manager) ConsoleCommand(name, mode string) ([]string, error) {
	if m.Disabled {
		return nil, ErrDisabled
	}
	c, err := m.get(name)
	if err != nil {
		return nil, err
	}
	if m.running(name) == "" {
		return nil, ErrNotRunning
	}
	path := m.lxcPath(c.Volume)
	if mode == "console" {
		return []string{"lxc-console", "-P", path, "-n", name, "-t", "0"}, nil
	}
	return []string{"lxc-attach", "-P", path, "-n", name, "--clear-env",
		"--set-var", "TERM=xterm-256color", "--set-var", "LANG=C.UTF-8",
		"--set-var", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"--", "/bin/sh", "-c", "cd /root 2>/dev/null; if command -v bash >/dev/null; then exec bash -l; fi; exec sh -l"}, nil
}

// GeneratedConfig returns the configuration LXC gets with the next start.
func (m *Manager) GeneratedConfig(name string) (string, error) {
	c, err := m.get(name)
	if err != nil {
		return "", err
	}
	return m.Config(c, false), nil
}
