package lxc

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ra5on/TitanOS/backend/internal/run"
)

// Creating a container: checks, build, config (split from lxc.go, PLAN 75-12).

func validDisplayName(name string) bool {
	if name == "" {
		return true
	}
	if strings.TrimSpace(name) != name || len([]rune(name)) > 80 {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || (r >= 0x200b && r <= 0x200f) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) || r == 0xfeff {
			return false
		}
	}
	return true
}

func newMAC() string {
	b := make([]byte, 3)
	rand.Read(b)
	// 00:16:3e is the prefix LXC itself uses for containers.
	return fmt.Sprintf("00:16:3e:%02x:%02x:%02x", b[0], b[1], b[2])
}

// bridgeOf returns the bridge a container joins.
func (m *Manager) bridgeOf(c Container) (string, bool) {
	if c.Network == "bridge" {
		return bridge, true
	}
	if id, ok := strings.CutPrefix(c.Network, "network:"); ok && m.NetworkBridge != nil {
		return m.NetworkBridge(id)
	}
	return "", false
}

// check validates the settings that can change after creation and
// normalizes them. The folders are resolved to their real paths.
func (m *Manager) check(c *Container) error {
	if !validDisplayName(c.DisplayName) {
		return ErrInvalidDisplayName
	}
	if len(c.Description) > 2000 {
		return fmt.Errorf("%w: description too long", ErrInvalid)
	}
	if c.CPUs < 0 || c.CPUs > runtime.NumCPU() {
		return fmt.Errorf("%w: CPUs", ErrInvalid)
	}
	if c.MemoryMiB != 0 && (c.MemoryMiB < 64 || c.MemoryMiB > 1<<22) {
		return fmt.Errorf("%w: memory", ErrInvalid)
	}
	switch {
	case c.Network == "" || c.Network == "none":
		c.Network = "none"
	case c.Network == "bridge":
	case strings.HasPrefix(c.Network, "network:") && len(c.Network) > len("network:"):
	default:
		return fmt.Errorf("%w: network", ErrInvalid)
	}
	c.MAC = strings.ToLower(strings.TrimSpace(c.MAC))
	if c.MAC == "" {
		c.MAC = newMAC()
	}
	// A unicast address: the lowest bit of the first byte is clear.
	if !macRE.MatchString(c.MAC) || c.MAC == "00:00:00:00:00:00" {
		return fmt.Errorf("%w: MAC address", ErrInvalid)
	}
	if first, _ := strconv.ParseUint(c.MAC[:2], 16, 8); first&1 != 0 {
		return fmt.Errorf("%w: MAC address", ErrInvalid)
	}
	return m.checkMounts(c)
}

// systemTargets are places in the container no folder of the NAS may cover.
var systemTargets = []string{"/bin", "/boot", "/dev", "/etc", "/lib", "/lib64", "/proc", "/run", "/sbin", "/sys", "/usr", "/var"}

func (m *Manager) checkMounts(c *Container) error {
	if len(c.Mounts) > maxMounts {
		return fmt.Errorf("%w: too many folders", ErrInvalid)
	}
	targets := map[string]bool{}
	for i := range c.Mounts {
		mount := &c.Mounts[i]
		real, err := m.onVolume(mount.Path)
		if err != nil {
			return err
		}
		mount.Path = real
		target := filepath.Clean(mount.Target)
		if !targetRE.MatchString(mount.Target) || target != mount.Target || target == "/" || strings.Contains(target, "/../") || strings.HasSuffix(target, "/..") {
			return fmt.Errorf("%w: the place in the container must be an absolute path without spaces", ErrInvalid)
		}
		for _, system := range systemTargets {
			if target == system || strings.HasPrefix(target, system+"/") && (system == "/dev" || system == "/proc" || system == "/sys" || system == "/run") {
				return fmt.Errorf("%w: %s is part of the container's system", ErrInvalid, target)
			}
		}
		if targets[target] {
			return fmt.Errorf("%w: two folders at %s", ErrInvalid, target)
		}
		targets[target] = true
	}
	if len(c.Mounts) == 0 {
		c.Mounts = nil
	}
	return nil
}

// onVolume makes sure a folder is a folder of a volume, not one of the
// private folders of TitanOS or of LXC, and returns its real path.
func (m *Manager) onVolume(path string) (string, error) {
	// A control character could end the line of the LXC configuration
	// the folder is written to and smuggle in further keys.
	if strings.ContainsFunc(path, unicode.IsControl) {
		return "", ErrBadFolder
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", ErrBadFolder
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil || strings.ContainsFunc(real, unicode.IsControl) {
		return "", ErrBadFolder
	}
	relative, found := strings.CutPrefix(real, m.mountRoot+"/")
	parts := strings.Split(relative, "/")
	if !found || len(parts) < 2 {
		// The whole volume is not handed to a container.
		return "", ErrBadFolder
	}
	for _, part := range parts[1:] {
		if strings.HasPrefix(part, ".") {
			return "", ErrBadFolder
		}
	}
	if parts[1] == "lxc" {
		return "", ErrBadFolder
	}
	if info, err := os.Stat(real); err != nil || !info.IsDir() {
		return "", ErrBadFolder
	}
	return real, nil
}

// Create adds a container and makes its root filesystem from an image of
// images.linuxcontainers.org in the background.
func (m *Manager) Create(ctx context.Context, c Container, mounted []string) error {
	if m.Disabled {
		return ErrDisabled
	}
	if !m.Installed() {
		return ErrUnavailable
	}
	if !nameRE.MatchString(c.Name) {
		return ErrInvalidName
	}
	if !slices.Contains(mounted, c.Volume) {
		return ErrUnknownVolume
	}
	if c.Arch == "" {
		c.Arch = hostArch()
	}
	if !imageRE.MatchString(c.Distribution) || !imageRE.MatchString(c.Release) || !imageRE.MatchString(c.Arch) {
		return ErrUnknownImage
	}
	images, err := m.Images(ctx)
	if err != nil && len(images) == 0 {
		return err
	}
	if !slices.ContainsFunc(images, func(image Image) bool {
		return image.Distribution == c.Distribution && image.Release == c.Release && image.Arch == c.Arch
	}) {
		return ErrUnknownImage
	}
	if err := m.check(&c); err != nil {
		return err
	}
	if c.Network != "none" && !m.networkReady(c) {
		return ErrNoBridge
	}
	c.Created = m.now().UTC().Truncate(time.Second)
	c.Btrfs = m.isBtrfs(filepath.Join(m.mountRoot, c.Volume))

	lock, err := m.operation(c.Name)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if _, exists := m.containers[c.Name]; exists {
		m.mu.Unlock()
		lock.Unlock()
		return ErrExists
	}
	if _, err := os.Lstat(m.dir(c)); err == nil {
		// A folder left behind, e.g. of a container removed with its data
		// kept: never write over it.
		m.mu.Unlock()
		lock.Unlock()
		return fmt.Errorf("%w: %s already exists", ErrExists, m.dir(c))
	}
	if err := m.store.WriteJSON(configDir+"/"+c.Name+".json", c); err != nil {
		m.mu.Unlock()
		lock.Unlock()
		return err
	}
	m.containers[c.Name] = c
	job := &Job{State: "running", Phase: "prepare", Started: m.now()}
	m.jobs[c.Name] = job
	m.mu.Unlock()

	go func() {
		defer lock.Unlock()
		// The request that asked for it ends long before the download does.
		err := m.build(context.WithoutCancel(ctx), c, job)
		m.mu.Lock()
		defer m.mu.Unlock()
		if err != nil {
			slog.Error("container not created", "container", c.Name, "err", err)
			job.State, job.Text = "failed", errorText(err)
			return
		}
		slog.Info("container created", "container", c.Name, "image", c.Distribution+"/"+c.Release)
		delete(m.jobs, c.Name)
	}()
	return nil
}

func errorText(err error) string {
	var command *run.CommandError
	if errors.As(err, &command) && command.Output != "" {
		output := command.Output
		if len(output) > 4000 {
			output = output[len(output)-4000:]
		}
		return output
	}
	return err.Error()
}

func (m *Manager) setPhase(job *Job, phase string) {
	m.mu.Lock()
	job.Phase = phase
	m.mu.Unlock()
}

// build runs lxc-create with the download template. Its cache and
// temporary files lie on the volume, never in the RAM of the NAS.
func (m *Manager) build(ctx context.Context, c Container, job *Job) error {
	path := m.lxcPath(c.Volume)
	cache, tmp := filepath.Join(path, ".cache"), filepath.Join(path, ".tmp")
	for _, dir := range []string{path, cache, tmp} {
		// Unprivileged containers' root must pass through the lxcpath.
		if err := os.Mkdir(dir, 0o711); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
			return ErrBadFolder
		}
	}
	_ = os.Chmod(tmp, 0o700)   // permissions and times are a convenience; the data are there either way
	_ = os.Chmod(cache, 0o700) // permissions and times are a convenience; the data are there either way
	initial := filepath.Join(tmp, c.Name+".conf")
	if err := os.WriteFile(initial, []byte(m.Config(c, true)), 0o600); err != nil {
		return err
	}
	defer os.Remove(initial)
	m.setPhase(job, "download")
	backing := "dir"
	if c.Btrfs {
		backing = "btrfs"
	}
	_, err := m.runner.Run(ctx, "", "env", "LXC_CACHE_PATH="+cache, "TMPDIR="+tmp,
		"lxc-create", "-P", path, "-n", c.Name, "-B", backing, "-f", initial, "-t", "download", "--",
		"--dist", c.Distribution, "--release", c.Release, "--arch", c.Arch)
	if err != nil {
		return err
	}
	m.setPhase(job, "configure")
	// The template wrote a configuration of its own; from now on the one
	// made from /config is used.
	return m.writeConfig(c)
}

func hostArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "amd64"
}

// writeConfig writes the configuration LXC reads for a container.
func (m *Manager) writeConfig(c Container) error {
	dir := m.dir(c)
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("%w: %s is missing", ErrBadFolder, dir)
	}
	path := filepath.Join(dir, "config")
	tmp := path + ".titanos-new"
	if err := os.WriteFile(tmp, []byte(m.Config(c, false)), 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
