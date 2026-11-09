package shares

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// Bringing the system up to date with the shares: services, accounts,
// SMB sessions, bind mounts (split from shares.go, PLAN 75-12).

// State is what the system is derived from.
type State struct {
	Users []auth.User
	// Mounted are the volumes that are mounted. Shares on other volumes are
	// not offered: their folder would be an empty one in RAM.
	Mounted []string
	// Quotas tells about project quotas on a volume; nil leaves them alone.
	Quotas VolumeQuotas
	// Discovery keeps avahi running without SMB shares, for the .local
	// names of apps (package appproxy).
	Discovery bool
}

// Run keeps the system in line with the configuration until ctx is done.
// state is asked for the accounts and mounted volumes each time.
func (m *Manager) Run(ctx context.Context, state func() State) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	sweep := time.NewTicker(time.Hour)
	defer sweep.Stop()
	for {
		m.mu.Lock()
		loaded := m.loaded
		m.mu.Unlock()
		if !loaded {
			if err := m.Load(); err == nil {
				slog.Info("shares read from /config")
			}
		} else {
			current := state()
			if err := m.Apply(ctx, current); err != nil {
				slog.Error("apply shares", "err", err)
			}
			m.ApplyQuotas(current.Quotas, current.Mounted)
			m.resumePurges(current.Mounted)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-m.kick:
		case <-sweep.C:
			m.sweepRecycle(state().Mounted, time.Now())
		}
	}
}

// Apply makes accounts, configuration files and services match the shares.
// It does nothing if nothing changed since the last time.
func (m *Manager) Apply(ctx context.Context, state State) error {
	m.mu.Lock()
	gen := m.gen
	m.mu.Unlock()
	if m.Disabled {
		m.markApplied(gen)
		return nil
	}
	shares := m.List()
	var offered []Share
	for _, share := range shares {
		if slices.Contains(state.Mounted, share.Volume) {
			offered = append(offered, share)
		}
	}
	users := slices.Clone(state.Users)
	sort.Slice(users, func(i, j int) bool { return users[i].Name < users[j].Name })

	smbConf := renderSMBConf(offered, m.Path, m.shadowDir)
	exports := renderExports(offered, m.nfsRoot)
	passwords := renderSMBPasswd(users)
	discovery := ""
	if state.Discovery {
		discovery = "avahi"
	}
	signature := sha256.Sum256([]byte(smbConf + "\x00" + exports + "\x00" + passwords + "\x00" + discovery))
	m.mu.Lock()
	unchanged := signature == m.applied
	m.mu.Unlock()
	if unchanged {
		// A share taken off NFS may still have been busy when its mount
		// was to be removed; try again until it is gone.
		m.unbindShares(ctx, offered)
		m.markApplied(gen)
		return nil
	}

	if err := m.syncAccounts(ctx, users, passwords); err != nil {
		return err
	}
	for _, share := range offered {
		path := m.Path(share)
		if err := os.MkdirAll(path, 0o770); err != nil {
			return err
		}
		// Group "users" with the setgid bit: everything created inside
		// belongs to the group, whoever wrote it and over which protocol.
		if err := os.Chown(path, -1, m.gid); err != nil {
			return err
		}
		if err := os.Chmod(path, 0o770|os.ModeSetgid); err != nil {
			return err
		}
		if share.PerUser {
			if err := m.userFolders(share, path, users); err != nil {
				return err
			}
		}
	}
	if err := writeFile(m.sambaConf, smbConf); err != nil {
		return err
	}
	if err := writeFile(m.exports, exports); err != nil {
		return err
	}

	smb := slices.ContainsFunc(offered, func(s Share) bool { return s.SMB && offeredOverSMB(s) })
	nfs := slices.ContainsFunc(offered, func(s Share) bool { return s.NFS.Enabled })
	if err := m.service(ctx, smb, "smbd.service", "reload-or-restart"); err != nil {
		return err
	}
	// Discovery: wsdd2 for the Windows network view, avahi for macOS and Linux.
	for _, unit := range []string{"wsdd2.service", "avahi-daemon.service"} {
		if err := m.service(ctx, smb || (unit == "avahi-daemon.service" && state.Discovery), unit, "start"); err != nil {
			slog.Warn("network discovery", "unit", unit, "err", err)
		}
	}
	if err := m.bindShares(ctx, offered); err != nil {
		return err
	}
	if err := m.service(ctx, nfs, "nfs-server.service", "start"); err != nil {
		return err
	}
	if nfs {
		if _, err := m.runner.Run(ctx, "", "exportfs", "-ra"); err != nil {
			return err
		}
	}
	// Only now, when nothing exports them any more.
	m.unbindShares(ctx, offered)

	m.mu.Lock()
	m.applied = signature
	m.mu.Unlock()
	m.markApplied(gen)
	slog.Info("shares applied", "shares", len(offered), "accounts", len(users), "smb", smb, "nfs", nfs)
	return nil
}

func (m *Manager) service(ctx context.Context, wanted bool, unit, startVerb string) error {
	verb := "stop"
	if wanted {
		verb = startVerb
	}
	_, err := m.runner.Run(ctx, "", "systemctl", verb, unit)
	return err
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".titanos-new"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// syncAccounts creates the Linux user of every account and gives Samba
// their password hashes. Accounts that are gone are removed from both.
func (m *Manager) syncAccounts(ctx context.Context, users []auth.User, passwords string) error {
	for _, user := range users {
		if _, err := m.runner.Run(ctx, "", "getent", "passwd", user.Name); err == nil {
			continue
		}
		// No login shell and no home: the account is for file access.
		if _, err := m.runner.Run(ctx, "", "useradd", "--uid", fmt.Sprint(user.UID), "--gid", "users",
			"--no-create-home", "--no-user-group", "--shell", "/usr/sbin/nologin", user.Name); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(m.runDir, 0o700); err != nil {
		return err
	}
	file := filepath.Join(m.runDir, "smbpasswd")
	if err := os.WriteFile(file, []byte(passwords), 0o600); err != nil {
		return err
	}
	defer os.Remove(file)
	if passwords != "" {
		if _, err := m.runner.Run(ctx, "", "pdbedit", "--import=smbpasswd:"+file); err != nil {
			return err
		}
	}
	// The file already carries the D flag of a locked account; smbpasswd
	// sets the flag on an entry Samba knew before as well. A lock that does
	// not take fails the round, which is tried again.
	locked := false
	for _, user := range users {
		if user.SMBHash == "" {
			continue
		}
		flag := "-e"
		if user.Disabled {
			flag, locked = "-d", true
		}
		if _, err := m.runner.Run(ctx, "", "smbpasswd", flag, user.Name); err != nil {
			if user.Disabled {
				return fmt.Errorf("lock Samba account %s: %w", user.Name, err)
			}
			slog.Warn("unlock Samba account", "user", user.Name, "err", err)
		}
	}
	if locked {
		m.endSMBSessions(ctx, users)
	}

	known, err := m.runner.Run(ctx, "", "pdbedit", "--list")
	if err != nil {
		return err
	}
	for line := range strings.SplitSeq(known, "\n") {
		name, _, ok := strings.Cut(line, ":")
		if !ok || slices.ContainsFunc(users, func(u auth.User) bool { return u.Name == name && u.SMBHash != "" }) {
			continue
		}
		if _, err := m.runner.Run(ctx, "", "pdbedit", "--delete", "--user="+name); err != nil {
			slog.Warn("remove Samba account", "user", name, "err", err)
		}
		if !slices.ContainsFunc(users, func(u auth.User) bool { return u.Name == name }) {
			if _, err := m.runner.Run(ctx, "", "userdel", name); err != nil {
				slog.Warn("remove Linux user", "user", name, "err", err)
			}
		}
	}
	return nil
}

// endSMBSessions ends the SMB connections of locked accounts. A disabled
// Samba account only stops new logins; a connection that is open keeps its
// session until it ends. Each connection is one smbd process, which is told
// to stop; the client then has to log in again and is refused.
func (m *Manager) endSMBSessions(ctx context.Context, users []auth.User) {
	output, err := m.runner.Run(ctx, "", "smbstatus", "--processes", "--json")
	if err != nil {
		slog.Debug("SMB sessions not readable", "err", err) // smbd not running
		return
	}
	for _, pid := range lockedSMBProcesses(output, users) {
		if _, err := m.runner.Run(ctx, "", "kill", "-TERM", pid); err != nil {
			slog.Warn("end SMB connection of a locked account", "pid", pid, "err", err)
		} else {
			slog.Info("SMB connection of a locked account ended", "pid", pid)
		}
	}
}

// lockedSMBProcesses reads `smbstatus --processes --json` and returns the
// smbd processes serving a locked account, matched by name or UID.
func lockedSMBProcesses(output string, users []auth.User) []string {
	var status struct {
		Sessions map[string]struct {
			Username string `json:"username"`
			UID      *int   `json:"uid"`
			ServerID struct {
				PID json.RawMessage `json:"pid"`
			} `json:"server_id"`
		} `json:"sessions"`
	}
	if json.Unmarshal([]byte(output), &status) != nil {
		return nil
	}
	var pids []string
	for _, session := range status.Sessions {
		locked := slices.ContainsFunc(users, func(u auth.User) bool {
			return u.Disabled && (strings.EqualFold(session.Username, u.Name) || session.UID != nil && *session.UID == u.UID)
		})
		pid := strings.Trim(string(session.ServerID.PID), `"`)
		if n, err := strconv.Atoi(pid); !locked || err != nil || n <= 1 {
			continue
		}
		if !slices.Contains(pids, pid) {
			pids = append(pids, pid)
		}
	}
	sort.Strings(pids)
	return pids
}
