package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/naming"
	"golang.org/x/sys/unix"
)

const smbTargetsFile = "backup/smb-targets.json"

// SMBTarget is a reusable connection to a second NAS. Password is only accepted
// on writes; public responses always clear it. Credentials remain in /config.
type SMBTarget struct {
	ID       string `json:"id"`
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	Share    string `json:"share"`
	Folder   string `json:"folder"`
}

func validateSMB(t SMBTarget, requireShare bool) error {
	if !naming.ValidBackup(t.ID) || !hostForm.MatchString(t.Host) || !userForm.MatchString(t.Username) || len(t.Password) == 0 || len(t.Password) > 1024 || strings.ContainsAny(t.Password, "\x00\r\n") {
		return invalid("SMB name, server or credentials")
	}
	if requireShare && (t.Share == "" || len(t.Share) > 255 || strings.ContainsAny(t.Share, "/\\\x00\r\n|,") || t.Share == "." || t.Share == ".." || t.Folder != "" && !relative(t.Folder)) {
		return invalid("SMB share or folder")
	}
	return nil
}

func (m *Manager) SMBTargets() []SMBTarget {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := slices.Clone(m.smbTargets)
	if result == nil {
		result = []SMBTarget{}
	}
	for i := range result {
		result[i].Password = ""
	}
	return result
}

func (m *Manager) smbTarget(id string) (SMBTarget, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.smbTargets {
		if t.ID == id {
			return t, nil
		}
	}
	return SMBTarget{}, ErrNotFound
}

func (m *Manager) smbCredentials(t SMBTarget) SMBTarget {
	if t.Password == "" {
		if old, err := m.smbTarget(t.ID); err == nil && old.Host == t.Host && old.Username == t.Username {
			t.Password = old.Password
		}
	}
	return t
}

// Reject this NAS as its own destination (including DNS aliases). Besides not
// surviving a NAS failure, a share below a source could recursively copy itself.
func remoteSMB(ctx context.Context, host string) error {
	_, err := remoteSMBAddress(ctx, host)
	return err
}

func remoteSMBAddress(ctx context.Context, host string) (string, error) {
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, strings.Trim(host, "[]"))
	if err != nil || len(addresses) == 0 {
		return "", fmt.Errorf("SMB server cannot be resolved")
	}
	local, err := net.InterfaceAddrs()
	if err != nil {
		return "", fmt.Errorf("cannot check SMB server address")
	}
	for _, addr := range addresses {
		if addr.IP.IsLoopback() || addr.IP.IsUnspecified() || addr.IP.IsMulticast() {
			return "", invalid("SMB target must be a second NAS")
		}
		for _, a := range local {
			ip, _, _ := net.ParseCIDR(a.String())
			if ip.Equal(addr.IP) {
				return "", invalid("SMB target must be a second NAS")
			}
		}
	}
	return addresses[0].IP.String(), nil
}

// authDir contains only a credential file and an empty mount point. Backup data
// goes straight to the remote filesystem, never into the RAM overlay.
func (m *Manager) smbAuth(t SMBTarget) (string, error) {
	if err := os.MkdirAll(m.workDir, 0700); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(m.workDir, "smb-")
	if err != nil {
		return "", err
	}
	if err = os.WriteFile(filepath.Join(dir, "credentials"), []byte("username="+t.Username+"\npassword="+t.Password+"\n"), 0600); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

type boundedOutput struct{ buffer bytes.Buffer }

func (b *boundedOutput) String() string { return b.buffer.String() }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > 1<<20 {
		return 0, fmt.Errorf("SMB response too large")
	}
	return b.buffer.Write(p)
}

func (m *Manager) SMBDiscover(ctx context.Context, target SMBTarget) ([]string, error) {
	if m.Disabled {
		return nil, ErrDisabled
	}
	target = m.smbCredentials(target)
	if err := validateSMB(target, false); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	address, err := remoteSMBAddress(ctx, target.Host)
	if err != nil {
		return nil, err
	}
	dir, err := m.smbAuth(target)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, "smbclient", "-g", "-L", target.Host, "-I", address, "-A", filepath.Join(dir, "credentials"), "--option=client min protocol=SMB3_00", "--client-protection=encrypt", "-t", "15")
	var out boundedOutput
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("SMB connection failed: check server, credentials and SMB3 encryption support")
	}
	shares := []string{}
	for _, line := range strings.Split(out.String(), "\n") {
		fields := strings.Split(line, "|")
		if len(fields) >= 2 && fields[0] == "Disk" {
			candidate := target
			candidate.Share = fields[1]
			if validateSMB(candidate, true) == nil {
				shares = append(shares, fields[1])
			}
		}
	}
	slices.Sort(shares)
	return slices.Compact(shares), nil
}

func (m *Manager) withSMB(ctx context.Context, t SMBTarget, work func(*os.Root) error) (result error) {
	if m.Disabled {
		return ErrDisabled
	}
	if err := validateSMB(t, true); err != nil {
		return err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	address, err := remoteSMBAddress(checkCtx, t.Host)
	if err != nil {
		return err
	}
	dir, err := m.smbAuth(t)
	if err != nil {
		return err
	}
	// Never RemoveAll a directory which could still contain a mounted share.
	defer func() {
		os.Remove(filepath.Join(dir, "credentials"))
		os.Remove(filepath.Join(dir, "share"))
		os.Remove(dir)
	}()
	mount := filepath.Join(dir, "share")
	if err := os.Mkdir(mount, 0700); err != nil {
		return err
	}
	cmd := exec.CommandContext(checkCtx, "mount.cifs", "//"+t.Host+"/"+t.Share, mount, "-o", "credentials="+filepath.Join(dir, "credentials")+",ip="+address+",vers=3,seal,nosuid,nodev,noexec,soft,echo_interval=5,cache=none,file_mode=0600,dir_mode=0700")
	// mount can have completed just before the helper was cancelled. Always try
	// to unmount the private mountpoint, even when the helper reports failure.
	defer func() {
		if err := unix.Unmount(mount, 0); err != nil && !errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.ENOENT) {
			unix.Unmount(mount, unix.MNT_DETACH)
			if result == nil {
				result = fmt.Errorf("SMB backup finished but unmount failed")
			}
		}
	}()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("SMB share could not be mounted: check access rights and SMB3 encryption support")
	}
	root, err := os.OpenRoot(mount)
	if err != nil {
		return err
	}
	defer root.Close()
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	var stat unix.Statfs_t
	err = unix.Fstatfs(int(f.Fd()), &stat)
	f.Close()
	// Linux reports the SMB2 magic for modern SMB2/SMB3 mounts.
	// The mount command above still requires encrypted SMB3.
	if err != nil || (stat.Type != unix.CIFS_SUPER_MAGIC && stat.Type != unix.SMB2_SUPER_MAGIC) {
		return fmt.Errorf("SMB destination is not mounted")
	}
	if t.Folder != "" {
		if err := copyDirectories(root, t.Folder); err != nil {
			return err
		}
		sub, err := root.OpenRoot(t.Folder)
		if err != nil {
			return err
		}
		defer sub.Close()
		return work(sub)
	}
	return work(root)
}

func (m *Manager) SaveSMBTarget(ctx context.Context, target SMBTarget) error {
	target = m.smbCredentials(target)
	if err := validateSMB(target, true); err != nil {
		return err
	}
	// The same queue protects jobs and connection changes. No credentials or
	// destination can change while a backup is using it.
	if !m.queue.TryLock() {
		return ErrRunning
	}
	defer m.queue.Unlock()
	if err := m.withSMB(ctx, target, func(root *os.Root) error {
		name := ".titanos-write-test-" + rand.Text()
		f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("SMB destination is not writable")
		}
		defer root.Remove(name)
		_, err = f.Write([]byte("TitanOS backup destination test\n"))
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if err := root.Remove(name); err != nil {
			return fmt.Errorf("SMB destination does not permit removing temporary files")
		}
		return nil
	}); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	targets := slices.Clone(m.smbTargets)
	at := slices.IndexFunc(targets, func(t SMBTarget) bool { return t.ID == target.ID })
	if at < 0 {
		if slices.ContainsFunc(targets, func(t SMBTarget) bool { return strings.EqualFold(t.ID, target.ID) }) {
			return ErrExists
		}
		targets = append(targets, target)
	} else {
		targets[at] = target
	}
	if err := m.store.WriteJSON(smbTargetsFile, targets); err != nil {
		return err
	}
	m.smbTargets = targets
	return nil
}

func (m *Manager) DeleteSMBTarget(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return ErrNotLoaded
	}
	for _, j := range m.jobs {
		if j.Destination.Type == "smb-copy" && j.Destination.TargetID == id {
			return invalid("SMB target is still used by a backup job")
		}
	}
	targets := slices.DeleteFunc(slices.Clone(m.smbTargets), func(t SMBTarget) bool { return t.ID == id })
	if len(targets) == len(m.smbTargets) {
		return ErrNotFound
	}
	if err := m.store.WriteJSON(smbTargetsFile, targets); err != nil {
		return err
	}
	m.smbTargets = targets
	return nil
}

func (m *Manager) loadSMBTargets() ([]SMBTarget, error) {
	var targets []SMBTarget
	if err := m.store.ReadJSON(smbTargetsFile, &targets); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return targets, nil
}
