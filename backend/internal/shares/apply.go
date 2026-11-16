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
	// A file that cannot be read is tried again less and less often (up to
	// every five minutes), not every round: no cyclic reads of the stick.
	retry, wait := time.Time{}, 5*time.Second
	for {
		m.mu.Lock()
		loaded := m.loaded
		m.mu.Unlock()
		if !loaded {
			if now := time.Now(); !now.Before(retry) {
				if err := m.Load(); err == nil {
					slog.Info("shares read from /config")
				} else {
					slog.Error("shares not readable from /config; no share is offered", "err", err, "retry", wait)
					retry, wait = now.Add(wait), min(2*wait, 5*time.Minute)
				}
			}
		} else {
			current := state()
			if err := m.Apply(ctx, current); err != nil {
				slog.Error("apply shares", "err", err)
			}
			m.ApplyQuotas(current.Quotas, current.Mounted)
			m.resumePurges(current.Mounted)
			// Recycle bins, once an hour per volume; sleeping disks wait.
			m.sweepRecycleDue(current.Mounted, time.Now())
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-m.kick:
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
	// An account that could not be made is tried again now and then, not
	// every round.
	retry := !m.accountsRetry.IsZero() && !m.now().Before(m.accountsRetry)
	unchanged := signature == m.applied && !retry
	m.mu.Unlock()
	if unchanged {
		// A share taken off NFS may still have been busy when its mount
		// was to be removed; try again until it is gone.
		m.unbindShares(ctx, offered)
		m.markApplied(gen)
		return nil
	}

	// Accounts whose Linux user cannot be made are left out of everything
	// below; the others and the shares are applied all the same.
	users, err := m.syncAccounts(ctx, users)
	if err != nil {
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
	m.closeLockedShares(ctx, shares)
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
		if _, err := m.run(ctx, "exportfs", "-ra"); err != nil {
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
	_, err := m.run(ctx, "systemctl", verb, unit)
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

// accountRetry is how often an account whose Linux user could not be made
// is tried again (the whole Apply runs again then).
var accountRetry = 10 * time.Minute

// syncAccounts creates the Linux user of every account and gives Samba
// their password hashes. Accounts that are gone are removed from both. It
// returns the accounts that have their Linux user now.
//
// An account whose Linux user cannot be made (its UID belongs to another
// user of the system, say) is skipped and reported, instead of failing
// the whole round: it is left out of the Samba accounts and the folders
// per account (its UID would belong to that other user), while every
// other account, the shares and the locks of the guard are applied. A
// Samba entry it had is removed, so it cannot log in over SMB meanwhile.
func (m *Manager) syncAccounts(ctx context.Context, all []auth.User) ([]auth.User, error) {
	users := make([]auth.User, 0, len(all))
	skipped := map[string]bool{}
	for _, user := range all {
		if _, err := m.run(ctx, "getent", "passwd", user.Name); err != nil {
			// No login shell and no home: the account is for file access.
			if _, err := m.run(ctx, "useradd", "--uid", fmt.Sprint(user.UID), "--gid", "users",
				"--no-create-home", "--no-user-group", "--shell", "/usr/sbin/nologin", user.Name); err != nil {
				skipped[user.Name] = true
				m.accountSkipped(user.Name, err)
				continue
			}
		}
		users = append(users, user)
	}
	m.mu.Lock()
	for name := range m.skipped {
		if !skipped[name] {
			slog.Info("account for shares made after all", "user", name)
		}
	}
	m.skipped = skipped
	m.accountsRetry = time.Time{}
	if len(skipped) > 0 {
		m.accountsRetry = m.now().Add(accountRetry)
	}
	m.mu.Unlock()

	if err := os.MkdirAll(m.runDir, 0o700); err != nil {
		return nil, err
	}
	passwords := renderSMBPasswd(users)
	file := filepath.Join(m.runDir, "smbpasswd")
	if err := os.WriteFile(file, []byte(passwords), 0o600); err != nil {
		return nil, err
	}
	defer os.Remove(file)
	if passwords != "" {
		if _, err := m.run(ctx, "pdbedit", "--import=smbpasswd:"+file); err != nil {
			return nil, err
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
		if _, err := m.run(ctx, "smbpasswd", flag, user.Name); err != nil {
			if user.Disabled {
				return nil, fmt.Errorf("lock Samba account %s: %w", user.Name, err)
			}
			slog.Warn("unlock Samba account", "user", user.Name, "err", err)
		}
	}
	if locked {
		m.endSMBSessions(ctx, users)
	}

	known, err := m.run(ctx, "pdbedit", "--list")
	if err != nil {
		return nil, err
	}
	for line := range strings.SplitSeq(known, "\n") {
		name, _, ok := strings.Cut(line, ":")
		if !ok || slices.ContainsFunc(users, func(u auth.User) bool { return u.Name == name && u.SMBHash != "" }) {
			continue
		}
		if _, err := m.run(ctx, "pdbedit", "--delete", "--user="+name); err != nil {
			slog.Warn("remove Samba account", "user", name, "err", err)
		}
		// A skipped account is no Linux user to remove: its name may not
		// even be one.
		if !skipped[name] && !slices.ContainsFunc(users, func(u auth.User) bool { return u.Name == name }) {
			if _, err := m.run(ctx, "userdel", name); err != nil {
				slog.Warn("remove Linux user", "user", name, "err", err)
			}
		}
	}
	return users, nil
}

// accountSkipped reports an account whose Linux user could not be made:
// in the journal every time, as a message once until it works again.
func (m *Manager) accountSkipped(name string, err error) {
	slog.Error("account left out of the shares: its Linux user cannot be made", "user", name, "err", err)
	m.mu.Lock()
	reported, tell := m.skipped[name], m.onEvent
	m.mu.Unlock()
	if !reported && tell != nil {
		tell("error", "share_account_skipped", name)
	}
}

// SkippedAccounts are the accounts left out of the shares because their
// Linux user could not be made, sorted.
func (m *Manager) SkippedAccounts() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.skipped))
	for name := range m.skipped {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// endSMBSessions ends the SMB connections of locked accounts. A disabled
// Samba account only stops new logins; a connection that is open keeps its
// session until it ends. Each connection is one smbd process, which is told
// to stop; the client then has to log in again and is refused.
func (m *Manager) endSMBSessions(ctx context.Context, users []auth.User) {
	output, err := m.run(ctx, "smbstatus", "--processes", "--json")
	if err != nil {
		slog.Debug("SMB sessions not readable", "err", err) // smbd not running
		return
	}
	for _, pid := range lockedSMBProcesses(output, users) {
		if _, err := m.run(ctx, "kill", "-TERM", pid); err != nil {
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
