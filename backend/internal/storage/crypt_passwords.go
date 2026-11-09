package storage

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
)

// Passwords of encrypted volumes: add, change, remove, key slots, header
// backups (split from crypt.go, PLAN 75-12).

// checkPasswordAll checks a password against every LUKS device of a volume.
func (m *Manager) checkPasswordAll(ctx context.Context, volume VolumeConfig, password []byte) ([][2]string, error) {
	nodes, err := m.headerDevices(ctx, volume, true)
	if err != nil {
		return nil, err
	}
	list := orderedDevices(volume, nodes)
	for _, entry := range list {
		if err := m.testPassphrase(ctx, password, entry[1]); err != nil {
			return nil, err
		}
	}
	return list, nil
}

// passwordChange runs one change of the key slots with the attempt limit.
func (m *Manager) passwordChange(ctx context.Context, name string, current []byte, change func(VolumeConfig, [][2]string) error) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return ErrReadOnly
	}
	volume, ok := m.find(name)
	if !ok {
		return ErrNotFound
	}
	if volume.Encryption == nil {
		return ErrNotEncrypted
	}
	if err := m.checkAttempt(name); err != nil {
		return err
	}
	list, err := m.checkPasswordAll(ctx, volume, current)
	m.noteAttempt(name, err)
	if err != nil {
		return err
	}
	err = change(volume, list)
	m.readPasswords(ctx, volume)
	return err
}

func (m *Manager) addKey(ctx context.Context, current, password []byte, device string) error {
	args := append([]string{"luksAddKey"}, pbkdfArgs...)
	args = append(args, "--key-file=-", device, "/dev/fd/3")
	_, err := m.runSecret(ctx, current, [][]byte{password}, "cryptsetup", args...)
	return cryptError(err)
}

// AddPassword adds a further password (key slot) to every LUKS device of
// a volume; current is one that works now.
func (m *Manager) AddPassword(ctx context.Context, name string, current, password []byte) error {
	if err := validPassword(password); err != nil {
		return err
	}
	return m.passwordChange(ctx, name, current, func(volume VolumeConfig, list [][2]string) error {
		for _, entry := range list {
			if err := m.addKey(ctx, current, password, entry[1]); err != nil {
				return err
			}
		}
		slog.Info("password added to encrypted volume", "volume", name)
		return nil
	})
}

// ChangePassword replaces a password: the new one gets a key slot on every
// device first, only then the old one is removed.
func (m *Manager) ChangePassword(ctx context.Context, name string, current, password []byte) error {
	if err := validPassword(password); err != nil {
		return err
	}
	if string(current) == string(password) {
		return fmt.Errorf("%w: the new password is the old one", ErrWeakPassword)
	}
	return m.passwordChange(ctx, name, current, func(volume VolumeConfig, list [][2]string) error {
		for _, entry := range list {
			if err := m.addKey(ctx, current, password, entry[1]); err != nil {
				return err
			}
		}
		for _, entry := range list {
			if _, err := m.runSecret(ctx, current, nil, "cryptsetup", "luksRemoveKey", "--key-file=-", entry[1]); err != nil {
				return cryptError(err)
			}
		}
		slog.Info("password of encrypted volume changed", "volume", name)
		return nil
	})
}

// RemovePassword removes one password from every device, never the last.
func (m *Manager) RemovePassword(ctx context.Context, name string, password []byte) error {
	return m.passwordChange(ctx, name, password, func(volume VolumeConfig, list [][2]string) error {
		for _, entry := range list {
			if n := m.keySlots(ctx, entry[1]); n < 2 {
				return ErrLastPassword
			}
		}
		for _, entry := range list {
			if _, err := m.runSecret(ctx, password, nil, "cryptsetup", "luksRemoveKey", "--key-file=-", entry[1]); err != nil {
				return cryptError(err)
			}
		}
		slog.Info("password removed from encrypted volume", "volume", name)
		return nil
	})
}

var luks1Slot = regexp.MustCompile(`(?m)^Key Slot \d+: ENABLED`)

// keySlots counts the key slots in use on a LUKS device; 0 if unknown.
func (m *Manager) keySlots(ctx context.Context, device string) int {
	out, err := m.runner.Run(ctx, "", "cryptsetup", "luksDump", "--dump-json-metadata", device)
	if err == nil {
		var dump struct {
			Keyslots map[string]json.RawMessage `json:"keyslots"`
		}
		if json.Unmarshal([]byte(out), &dump) == nil {
			return len(dump.Keyslots)
		}
	}
	// LUKS1 (an imported volume) has no JSON metadata.
	if out, err := m.runner.Run(ctx, "", "cryptsetup", "luksDump", device); err == nil {
		return len(luks1Slot.FindAllString(out, -1))
	}
	return 0
}

func (m *Manager) knowsPasswords(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.cryptSlots[name]
	return ok
}

// readPasswords remembers the number of key slots of the first device.
func (m *Manager) readPasswords(ctx context.Context, volume VolumeConfig) {
	nodes, err := m.headerDevices(ctx, volume, false)
	if err != nil {
		return
	}
	list := orderedDevices(volume, nodes)
	if len(list) == 0 {
		return
	}
	n := m.keySlots(ctx, list[0][1])
	m.mu.Lock()
	if m.cryptSlots == nil {
		m.cryptSlots = map[string]int{}
	}
	m.cryptSlots[volume.Name] = n
	m.mu.Unlock()
}

// encryptionView is the state of a volume's encryption for the page. It
// only looks at /dev/mapper and RAM, never at the disks.
func (m *Manager) encryptionView(cfg VolumeConfig) *EncryptionView {
	if cfg.Encryption == nil {
		return nil
	}
	members := len(cfg.Encryption.UUIDs)
	if cfg.Filesystem != "btrfs" {
		members = 1
	}
	view := &EncryptionView{Members: members, Unlocked: m.openMembers(cfg)}
	view.Locked = view.Unlocked == 0
	m.mu.RLock()
	view.Passwords = m.cryptSlots[cfg.Name]
	m.mu.RUnlock()
	if wait := m.attemptWait(cfg.Name); wait > 0 {
		view.RetryAfterSeconds = int(wait.Seconds() + 0.5)
	}
	return view
}

// HeaderBackup writes a tar archive with a backup of every LUKS header of
// a volume (cryptsetup luksHeaderBackup). Such a backup opens the volume
// with every password it had at the time, also after a password change;
// it is no copy of the data.
func (m *Manager) HeaderBackup(ctx context.Context, name string, w io.Writer) error {
	dir, files, err := m.headerBackupFiles(ctx, name)
	if dir != "" {
		defer os.RemoveAll(dir)
	}
	if err != nil {
		return err
	}
	archive := tar.NewWriter(w)
	readme := fmt.Sprintf(headerReadme, name)
	if err := archive.WriteHeader(&tar.Header{Name: "README.txt", Mode: 0o600, Size: int64(len(readme)), ModTime: m.now()}); err != nil {
		return err
	}
	if _, err := io.WriteString(archive, readme); err != nil {
		return err
	}
	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		info, err := f.Stat()
		if err == nil {
			err = archive.WriteHeader(&tar.Header{Name: filepath.Base(file), Mode: 0o600, Size: info.Size(), ModTime: m.now()})
		}
		if err == nil {
			_, err = io.Copy(archive, f)
		}
		f.Close()
		if err != nil {
			return err
		}
	}
	return archive.Close()
}

const headerReadme = `LUKS header backup of the TitanOS volume %s.

Every file is the header of one LUKS device, named after its UUID. With
the password the volume had when this backup was made, a damaged header
can be written back (only onto the very same device):

    cryptsetup luksHeaderRestore <device> --header-backup-file <file>

Keep this archive safe: it opens the volume with every password it had at
the time, even after a password was changed or removed. It holds no data.
Without a password neither this backup nor anything else helps.
`

func (m *Manager) headerBackupFiles(ctx context.Context, name string) (string, []string, error) {
	m.actions.Lock()
	defer m.actions.Unlock()
	volume, ok := m.find(name)
	if !ok {
		return "", nil, ErrNotFound
	}
	nodes, err := m.headerDevices(ctx, volume, true)
	if err != nil {
		return "", nil, err
	}
	base := m.cryptTemp
	if base == "" {
		base = "/run"
	}
	dir, err := os.MkdirTemp(base, "titanos-luks-")
	if err != nil {
		return "", nil, err
	}
	var files []string
	for _, entry := range orderedDevices(volume, nodes) {
		file := filepath.Join(dir, fmt.Sprintf("%s-luks-%s.img", volume.Name, entry[0]))
		if _, err := m.runner.Run(ctx, "", "cryptsetup", "luksHeaderBackup", entry[1], "--header-backup-file", file); err != nil {
			return dir, nil, err
		}
		files = append(files, file)
	}
	slog.Info("LUKS header backup made", "volume", name)
	return dir, files, nil
}
