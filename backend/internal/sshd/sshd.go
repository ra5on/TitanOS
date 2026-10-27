// Package sshd switches the SSH access to the NAS on and off.
//
// SSH is off unless an administrator turns it on. Then owners and
// administrators log in under their own name with a key, never with a
// password and never as root, and use sudo. The key of the NAS itself
// lives in /config, so that it stays the same across restarts.
package sshd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

const (
	settingsFile = "system/ssh.json"
	hostKeyFile  = "ssh/host_ed25519"
	// group is who sshd lets in and who may use sudo; see the image.
	group   = "titanos-ssh"
	service = "ssh.service"
)

var (
	// ErrDisabled means titand runs in development mode.
	ErrDisabled = errors.New("sshd: not available in development mode")
	// ErrNotLoaded means /config could not be read.
	ErrNotLoaded = errors.New("sshd: settings not loaded from /config")
)

// Settings is /config/system/ssh.json.
type Settings struct {
	Enabled bool `json:"enabled"`
}

// Status is what the settings page shows.
type Status struct {
	Enabled bool `json:"enabled"`
	// Running says whether the service is up as it should be.
	Running bool `json:"running"`
	// Fingerprint of the key of the NAS, to compare at the first login.
	Fingerprint string `json:"fingerprint"`
	// Accounts are those that can log in right now.
	Accounts []string `json:"accounts"`
}

// Manager keeps sshd in line with the settings and the accounts.
type Manager struct {
	store  *config.Store
	runner run.Runner
	// etcDir is /etc/ssh, homeRoot /home, workDir where the key of the
	// NAS is made.
	etcDir, homeRoot, workDir string
	// gid is the group that owns the home directories (users).
	gid int
	// chown is replaced in tests, which do not run as root.
	chown func(path string, uid, gid int) error

	// Disabled leaves the system alone: in development mode titand runs
	// on the developer's own machine.
	Disabled bool

	kick chan struct{}

	mu          sync.Mutex
	loaded      bool
	settings    Settings
	fingerprint string
	running     bool
	accounts    []string
}

// NewManager returns a manager working on the real system. usersGID is
// the group of all accounts.
func NewManager(store *config.Store, usersGID int) *Manager {
	return &Manager{
		store: store, runner: run.Exec{}, etcDir: "/etc/ssh", homeRoot: "/home", workDir: "/run/titanos-ssh",
		gid: usersGID, chown: os.Chown, kick: make(chan struct{}, 1),
	}
}

// Load reads the settings from /config.
func (m *Manager) Load() error {
	var settings Settings
	err := m.store.ReadJSON(settingsFile, &settings)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings, m.loaded = settings, true
	return nil
}

// SetEnabled switches the SSH access on or off.
func (m *Manager) SetEnabled(enabled bool) error {
	if m.Disabled {
		return ErrDisabled
	}
	m.mu.Lock()
	if !m.loaded {
		m.mu.Unlock()
		return ErrNotLoaded
	}
	settings := Settings{Enabled: enabled}
	if err := m.store.WriteJSON(settingsFile, settings); err != nil {
		m.mu.Unlock()
		return err
	}
	m.settings = settings
	m.mu.Unlock()
	m.Kick()
	return nil
}

// Kick has the state applied again soon: after an account, a role or a
// key changed.
func (m *Manager) Kick() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// Status returns what is set and what is the case.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Status{Enabled: m.settings.Enabled, Running: m.running, Fingerprint: m.fingerprint, Accounts: append([]string{}, m.accounts...)}
}

// Run applies the settings at the start and whenever something changed,
// until ctx is done.
func (m *Manager) Run(ctx context.Context, users func() []auth.User) {
	if m.Disabled {
		return
	}
	retry := time.NewTimer(time.Second)
	defer retry.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.kick:
		case <-retry.C:
		}
		m.mu.Lock()
		loaded := m.loaded
		m.mu.Unlock()
		if !loaded && m.Load() != nil {
			retry.Reset(10 * time.Second)
			continue
		}
		complete, err := m.Apply(ctx, users())
		if err != nil {
			slog.Warn("SSH access not put in place", "err", err)
		}
		// An account whose Linux user does not exist yet comes with the
		// next round.
		if err != nil || !complete {
			retry.Reset(10 * time.Second)
		}
	}
}

// eligible returns the accounts that may log in: owners and
// administrators that have a key and are not locked. A locked account is
// taken out like one that lost its role, which also ends its sessions.
func eligible(users []auth.User) []auth.User {
	var list []auth.User
	for _, user := range users {
		if user.Role.CanAdminister() && !user.Disabled && len(user.DescribeSSHKeys()) > 0 {
			list = append(list, user)
		}
	}
	return list
}

func (m *Manager) members(ctx context.Context) []string {
	output, err := m.runner.Run(ctx, "", "getent", "group", group)
	if err != nil {
		return nil
	}
	fields := strings.Split(strings.TrimSpace(output), ":")
	if len(fields) < 4 || fields[3] == "" {
		return nil
	}
	return strings.Split(fields[3], ",")
}

// Apply puts sshd into the state the settings and accounts ask for. It
// returns false if an account has to wait for its Linux user.
func (m *Manager) Apply(ctx context.Context, users []auth.User) (bool, error) {
	if m.Disabled {
		return true, nil
	}
	m.mu.Lock()
	enabled := m.settings.Enabled
	m.mu.Unlock()

	keysDir := filepath.Join(m.etcDir, "authorized_keys")
	if err := os.MkdirAll(keysDir, 0o755); err != nil {
		return false, err
	}
	// sshd ignores keys in a directory that others than root can write to.
	if err := os.Chmod(keysDir, 0o755); err != nil {
		return false, err
	}
	var allowed []auth.User
	if enabled {
		allowed = eligible(users)
	}

	// First take away what is no longer due.
	for _, name := range m.members(ctx) {
		if slices.ContainsFunc(allowed, func(user auth.User) bool { return user.Name == name }) {
			continue
		}
		os.Remove(filepath.Join(keysDir, name))
		if _, err := m.runner.Run(ctx, "", "gpasswd", "--delete", name, group); err != nil {
			return false, err
		}
		m.runner.Run(ctx, "", "usermod", "--shell", "/usr/sbin/nologin", name)
		// Whoever is logged in over SSH right now is logged out.
		m.runner.Run(ctx, "", "loginctl", "terminate-user", name)
		slog.Info("SSH access taken away", "user", name)
	}
	if entries, err := os.ReadDir(keysDir); err == nil {
		for _, entry := range entries {
			if !slices.ContainsFunc(allowed, func(user auth.User) bool { return user.Name == entry.Name() }) {
				os.Remove(filepath.Join(keysDir, entry.Name()))
			}
		}
	}

	if !enabled {
		m.runner.Run(ctx, "", "systemctl", "stop", service)
		m.mu.Lock()
		m.running, m.accounts = false, nil
		m.mu.Unlock()
		return true, nil
	}

	fingerprint, err := m.hostKey(ctx)
	if err != nil {
		return false, fmt.Errorf("key of the NAS: %w", err)
	}

	complete := true
	var names []string
	for _, user := range allowed {
		if _, err := m.runner.Run(ctx, "", "getent", "passwd", user.Name); err != nil {
			complete = false // its Linux user is made with the next sync of the accounts
			continue
		}
		home := filepath.Join(m.homeRoot, user.Name)
		if err := os.MkdirAll(home, 0o700); err != nil {
			return false, err
		}
		if err := m.chown(home, user.UID, m.gid); err != nil {
			return false, err
		}
		if _, err := m.runner.Run(ctx, "", "usermod", "--shell", "/bin/bash", "--home", home, "--append", "--groups", group, user.Name); err != nil {
			return false, err
		}
		// Owned by root: the account cannot add keys behind TitanOS's back.
		file := filepath.Join(keysDir, user.Name)
		before, _ := os.ReadFile(file)
		if err := os.WriteFile(file, []byte(strings.Join(user.SSHKeys, "\n")+"\n"), 0o644); err != nil {
			return false, err
		}
		// A key that was taken away may be the one a session came in with
		// (a lost laptop): such sessions end, the account logs in again.
		for line := range strings.SplitSeq(string(before), "\n") {
			if line = strings.TrimSpace(line); line != "" && !slices.Contains(user.SSHKeys, line) {
				m.runner.Run(ctx, "", "loginctl", "terminate-user", user.Name)
				slog.Info("SSH sessions ended after a key was removed", "user", user.Name)
				break
			}
		}
		names = append(names, user.Name)
	}

	_, startErr := m.runner.Run(ctx, "", "systemctl", "start", service)
	m.mu.Lock()
	m.fingerprint, m.running, m.accounts = fingerprint, startErr == nil, names
	m.mu.Unlock()
	return complete, startErr
}

// hostKey puts the key of the NAS in place, making and storing one first
// if /config has none, and returns its fingerprint.
func (m *Manager) hostKey(ctx context.Context) (string, error) {
	private, err := m.store.Read(hostKeyFile)
	public, pubErr := m.store.Read(hostKeyFile + ".pub")
	// Only a key that is not there is made anew. A stick that cannot be
	// read right now must not change the fingerprint clients know.
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err == nil && pubErr != nil {
		if !errors.Is(pubErr, fs.ErrNotExist) {
			return "", pubErr
		}
		signer, parseErr := ssh.ParsePrivateKey(private)
		if parseErr != nil {
			return "", parseErr
		}
		public = append(bytes.TrimSpace(ssh.MarshalAuthorizedKey(signer.PublicKey())), " titanos\n"...)
		if err := m.store.Write(hostKeyFile+".pub", public); err != nil {
			return "", err
		}
		pubErr = nil
	}
	if err != nil || pubErr != nil {
		if err := os.MkdirAll(m.workDir, 0o700); err != nil {
			return "", err
		}
		file := filepath.Join(m.workDir, "host_ed25519")
		os.Remove(file)
		os.Remove(file + ".pub")
		defer os.Remove(file)
		defer os.Remove(file + ".pub")
		if _, err := m.runner.Run(ctx, "", "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "titanos", "-f", file); err != nil {
			return "", err
		}
		if private, err = os.ReadFile(file); err != nil {
			return "", err
		}
		if public, err = os.ReadFile(file + ".pub"); err != nil {
			return "", err
		}
		if err := m.store.Write(hostKeyFile, private); err != nil {
			return "", err
		}
		if err := m.store.Write(hostKeyFile+".pub", public); err != nil {
			return "", err
		}
		slog.Info("made a key for SSH and stored it in /config")
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey(public)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(m.etcDir, "ssh_host_ed25519_key"), private, 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(m.etcDir, "ssh_host_ed25519_key.pub"), public, 0o644); err != nil {
		return "", err
	}
	return ssh.FingerprintSHA256(key), nil
}
