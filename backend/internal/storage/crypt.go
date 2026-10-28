package storage

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Volume encryption with LUKS2 (dm-crypt), chosen per volume when it is
// created and off by default (docs/ENCRYPTION.md).
//
//   - mdadm: one LUKS2 container on the array, the file system inside it:
//     md → [dm-cache] → dm-crypt → ext4/XFS. The SSD cache sits below the
//     encryption, so the SSD only ever holds encrypted blocks.
//   - Btrfs: one LUKS2 container per member partition, all with the same
//     password; Btrfs spans the opened devices.
//
// After a start the volume stays locked until someone enters its password
// on the storage page; nothing on it (shares, Docker, VMs, LXC, backups to
// it) runs before. There is no key file and no stored key: the password is
// never written anywhere, never put on a command line (cryptsetup reads it
// from stdin or a pipe descriptor) and never logged. volumes.json holds the
// LUKS UUIDs only.
//
// Devices are opened with --disable-keyring: the volume key stays in the
// dm-crypt table instead of the kernel keyring, so that `cryptsetup resize`
// after growing an array works without asking for the password again.

// EncryptionConfig is the encryption of a volume in volumes.json.
type EncryptionConfig struct {
	// UUIDs are the LUKS UUIDs: one for an mdadm volume, one per member
	// partition of a Btrfs volume.
	UUIDs []string `json:"uuids"`
}

// EncryptionView is what the page shows about an encrypted volume.
type EncryptionView struct {
	// Locked is set while the volume waits for its password.
	Locked bool `json:"locked"`
	// Members is how many LUKS devices the volume has, Unlocked how many of
	// them are open.
	Members  int `json:"members"`
	Unlocked int `json:"unlocked"`
	// Passwords is the number of key slots in use, 0 while not known (it
	// is read when the volume is unlocked or a password changes).
	Passwords int `json:"passwords"`
	// RetryAfterSeconds is set after too many wrong passwords.
	RetryAfterSeconds int `json:"retryAfterSeconds,omitempty"`
}

// Errors of encryption.
var (
	ErrVolumeLocked      = errors.New("storage: the volume is locked; unlock it with its password")
	ErrWrongPassword     = errors.New("storage: wrong password")
	ErrTooManyAttempts   = errors.New("storage: too many wrong passwords; wait before trying again")
	ErrWeakPassword      = errors.New("storage: the password must have 8 to 512 characters")
	ErrPasswordRequired  = errors.New("storage: the password of the encrypted volume is required")
	ErrLastPassword      = errors.New("storage: the last password of a volume cannot be removed")
	ErrNotEncrypted      = errors.New("storage: the volume is not encrypted")
	ErrEncryptionMembers = errors.New("storage: every disk of the encrypted volume must be present")
)

// AttemptsError says how long to wait after too many wrong passwords.
type AttemptsError struct{ Wait time.Duration }

func (e AttemptsError) Error() string {
	return fmt.Sprintf("%s (%d s)", ErrTooManyAttempts, int(e.Wait.Seconds()+0.5))
}
func (e AttemptsError) Unwrap() error { return ErrTooManyAttempts }

const (
	minPassword = 8
	maxPassword = 512
	// freeAttempts wrong passwords cost nothing extra (each one already
	// costs the time of the key derivation); after that every further
	// attempt waits twice as long as the one before, up to maxAttemptWait.
	freeAttempts   = 5
	firstWait      = 30 * time.Second
	maxAttemptWait = 15 * time.Minute
	// luksHeaderMiB is what the LUKS2 header takes at the start of a device.
	luksHeaderMiB = 16
	// lockWait is how long Lock waits for the volume to be let go.
	lockWait = 3 * time.Minute
)

// pbkdfArgs choose the key derivation of new key slots: Argon2id with
// 256 MiB of memory, four lanes, and as many iterations as take about two
// seconds on this NAS. 256 MiB instead of cryptsetup's 1 GiB default because
// TitanOS runs in RAM and NAS boards often have 4–8 GiB; it is used only for
// the moment of unlocking. cryptsetup lowers it further when less memory
// is free.
var pbkdfArgs = []string{"--pbkdf", "argon2id", "--pbkdf-memory", "262144", "--pbkdf-parallel", "4", "--iter-time", "2000"}

func cryptPrefix(volume string) string { return "titanos-crypt-" + volume }

// cryptName is the device-mapper name of one LUKS device of a volume:
// titanos-crypt-<volume> on an array, titanos-crypt-<volume>-<uuid8> for
// a Btrfs member (stable however the disks are numbered).
func cryptName(volume VolumeConfig, uuid string) string {
	if volume.Filesystem != "btrfs" {
		return cryptPrefix(volume.Name)
	}
	short := strings.ReplaceAll(strings.ToLower(uuid), "-", "")
	if len(short) > 8 {
		short = short[:8]
	}
	return cryptPrefix(volume.Name) + "-" + short
}

func validPassword(password []byte) error {
	if len(password) < minPassword || len(password) > maxPassword {
		return ErrWeakPassword
	}
	return nil
}

// wipe overwrites a secret once it is no longer needed.
func wipe(secret []byte) {
	for i := range secret {
		secret[i] = 0
	}
}

func (m *Manager) mapperPath(name string) string {
	dir := m.mapperDir
	if dir == "" {
		dir = "/dev/mapper"
	}
	return filepath.Join(dir, name)
}

func (m *Manager) mapperOpen(name string) bool {
	_, err := os.Stat(m.mapperPath(name))
	return err == nil
}

// openMembers counts the open LUKS devices of a volume.
func (m *Manager) openMembers(volume VolumeConfig) int {
	if volume.Encryption == nil {
		return 0
	}
	if volume.Filesystem != "btrfs" {
		if m.mapperOpen(cryptName(volume, "")) {
			return 1
		}
		return 0
	}
	open := 0
	for _, uuid := range volume.Encryption.UUIDs {
		if m.mapperOpen(cryptName(volume, uuid)) {
			open++
		}
	}
	return open
}

// isLocked reports an encrypted volume none of whose devices is open.
func (m *Manager) isLocked(volume VolumeConfig) bool {
	return volume.Encryption != nil && m.openMembers(volume) == 0
}

// Locked reports whether a volume waits for its password.
func (m *Manager) Locked(name string) bool {
	volume, ok := m.find(name)
	return ok && m.isLocked(volume)
}

// LockedNames returns the volumes that wait for their password.
func (m *Manager) LockedNames() []string {
	var names []string
	for _, volume := range m.configured() {
		if m.isLocked(volume) {
			names = append(names, volume.Name)
		}
	}
	return names
}

// secretRunner is implemented by run.Exec: secrets go to stdin and pipe
// descriptors as bytes, never as part of the command line.
type secretRunner interface {
	RunSecret(ctx context.Context, stdin []byte, extra [][]byte, name string, args ...string) (string, error)
}

func (m *Manager) runSecret(ctx context.Context, stdin []byte, extra [][]byte, name string, args ...string) (string, error) {
	if runner, ok := m.runner.(secretRunner); ok {
		return runner.RunSecret(ctx, stdin, extra, name, args...)
	}
	if len(extra) > 0 {
		return "", errors.New("storage: this runner cannot pass secrets on descriptors")
	}
	return m.runner.Run(ctx, string(stdin), name, args...)
}

// wrongPassphrase recognizes cryptsetup refusing a passphrase (exit code 2,
// "No key available with this passphrase").
func wrongPassphrase(err error) bool {
	var command *CommandError
	if errors.As(err, &command) && strings.Contains(command.Output, "No key available") {
		return true
	}
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 2
}

func cryptError(err error) error {
	if err != nil && wrongPassphrase(err) {
		return ErrWrongPassword
	}
	return err
}

// luksFormat creates a LUKS2 container on a device and returns its UUID.
func (m *Manager) luksFormat(ctx context.Context, password []byte, device, label string) (string, error) {
	args := append([]string{"luksFormat", "--batch-mode", "--type", "luks2"}, pbkdfArgs...)
	args = append(args, "--label", label, "--key-file=-", device)
	if _, err := m.runSecret(ctx, password, nil, "cryptsetup", args...); err != nil {
		return "", err
	}
	out, err := m.runner.Run(ctx, "", "cryptsetup", "luksUUID", device)
	uuid := strings.TrimSpace(out)
	if err != nil || uuid == "" {
		return "", fmt.Errorf("%w: new LUKS container on %s has no UUID", ErrNotPossible, device)
	}
	return uuid, nil
}

// luksOpen opens a LUKS device as /dev/mapper/<name>.
func (m *Manager) luksOpen(ctx context.Context, password []byte, device, name string, readOnly bool) error {
	args := []string{"open", "--type", "luks", "--disable-keyring", "--key-file=-"}
	if readOnly {
		args = append(args, "--readonly")
	}
	_, err := m.runSecret(ctx, password, nil, "cryptsetup", append(args, device, name)...)
	return cryptError(err)
}

func (m *Manager) luksClose(ctx context.Context, name string) error {
	if !m.mapperOpen(name) {
		return nil
	}
	_, err := m.runner.Run(ctx, "", "cryptsetup", "close", name)
	return err
}

// testPassphrase checks a password against a LUKS device without opening it.
func (m *Manager) testPassphrase(ctx context.Context, password []byte, device string) error {
	_, err := m.runSecret(ctx, password, nil, "cryptsetup", "open", "--test-passphrase", "--type", "luks", "--key-file=-", device)
	return cryptError(err)
}

// attemptWait is how long the next password attempt for key has to wait.
func (m *Manager) attemptWait(key string) time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.cryptAttempts[key]
	if !ok || a.failures < freeAttempts {
		return 0
	}
	wait := firstWait << min(a.failures-freeAttempts, 10)
	wait = min(wait, maxAttemptWait)
	return max(0, a.last.Add(wait).Sub(m.now()))
}

// noteAttempt counts a wrong password or forgets the failures after a
// right one.
func (m *Manager) noteAttempt(key string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cryptAttempts == nil {
		m.cryptAttempts = map[string]cryptAttempt{}
	}
	switch {
	case err == nil:
		delete(m.cryptAttempts, key)
	case errors.Is(err, ErrWrongPassword):
		a := m.cryptAttempts[key]
		a.failures++
		a.last = m.now()
		m.cryptAttempts[key] = a
		slog.Warn("wrong password for encrypted volume", "volume", key, "failures", a.failures)
	}
}

type cryptAttempt struct {
	failures int
	last     time.Time
}

// checkAttempt refuses while wrong passwords make key wait.
func (m *Manager) checkAttempt(key string) error {
	if wait := m.attemptWait(key); wait > 0 {
		return AttemptsError{Wait: wait}
	}
	return nil
}

// luksNodes finds the LUKS partitions of the given UUIDs: uuid → device.
func luksNodes(devices []blockDevice, uuids []string) map[string]string {
	nodes := map[string]string{}
	for _, device := range devices {
		if !isDisk(device) {
			continue
		}
		device.walk(func(node blockDevice) {
			if node.FSType != "crypto_LUKS" || !ownedByDisk(node) {
				return
			}
			for _, uuid := range uuids {
				if strings.EqualFold(node.UUID, uuid) {
					nodes[uuid] = node.Name
				}
			}
		})
	}
	return nodes
}

// headerDevices are the devices holding the LUKS headers of a volume, by
// UUID. On an array the header is reached through the SSD cache when there
// is one, so that a write cache never holds an older header than the array.
func (m *Manager) headerDevices(ctx context.Context, volume VolumeConfig, all bool) (map[string]string, error) {
	if volume.Encryption == nil || len(volume.Encryption.UUIDs) == 0 {
		return nil, ErrNotEncrypted
	}
	if volume.Filesystem != "btrfs" {
		a, ok := m.arrayOf(ctx, volume)
		if !ok || a.state == "inactive" || a.state == "clear" {
			return nil, fmt.Errorf("%w: the array of %s is not running", ErrNotPossible, volume.Name)
		}
		device, err := m.lowerDevice(ctx, volume, "/dev/"+a.device)
		if err != nil {
			return nil, err
		}
		return map[string]string{volume.Encryption.UUIDs[0]: device}, nil
	}
	devices, err := m.blockDevices(ctx)
	if err != nil {
		return nil, err
	}
	nodes := luksNodes(devices, volume.Encryption.UUIDs)
	if len(nodes) == 0 {
		return nil, fmt.Errorf("%w: no disk of %s found", ErrNotPossible, volume.Name)
	}
	if all && len(nodes) < len(volume.Encryption.UUIDs) {
		return nil, ErrEncryptionMembers
	}
	return nodes, nil
}

// orderedDevices lists the devices of headerDevices in the order of the
// configuration.
func orderedDevices(volume VolumeConfig, nodes map[string]string) [][2]string {
	var list [][2]string
	for _, uuid := range volume.Encryption.UUIDs {
		if device, ok := nodes[uuid]; ok {
			list = append(list, [2]string{uuid, device})
		}
	}
	return list
}

// Unlock opens an encrypted volume with its password and mounts it; what
// lives on it starts as after a normal mount. Wrong passwords are counted
// per volume and slow further attempts down.
func (m *Manager) Unlock(ctx context.Context, name string, password []byte) error {
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
	var err error
	if volume.Filesystem == "btrfs" {
		err = m.unlockBtrfs(ctx, volume, password)
	} else {
		err = m.unlockArray(ctx, volume, password)
	}
	m.noteAttempt(name, err)
	if err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.lockNotified, name)
	m.mu.Unlock()
	slog.Info("encrypted volume unlocked", "volume", name)
	m.readPasswords(ctx, volume)
	if m.OnUnlocked != nil {
		m.OnUnlocked(name)
	}
	return nil
}

func (m *Manager) unlockArray(ctx context.Context, volume VolumeConfig, password []byte) error {
	name := cryptName(volume, "")
	if !m.mapperOpen(name) {
		nodes, err := m.headerDevices(ctx, volume, true)
		if err != nil {
			return err
		}
		if err := m.luksOpen(ctx, password, nodes[volume.Encryption.UUIDs[0]], name, false); err != nil {
			return err
		}
	}
	if m.readMounts()[m.MountPoint(volume.Name)] {
		return nil
	}
	return m.mount(ctx, volume)
}

func (m *Manager) unlockBtrfs(ctx context.Context, volume VolumeConfig, password []byte) error {
	nodes, err := m.headerDevices(ctx, volume, false)
	if err != nil {
		return err
	}
	var opened []string
	for _, entry := range orderedDevices(volume, nodes) {
		mapper := cryptName(volume, entry[0])
		if m.mapperOpen(mapper) {
			continue
		}
		if err := m.luksOpen(ctx, password, entry[1], mapper, false); err != nil {
			// All members share the password: close what this call opened.
			for _, done := range opened {
				m.luksClose(ctx, done)
			}
			return err
		}
		opened = append(opened, mapper)
		m.runner.Run(ctx, "", "btrfs", "device", "scan", m.mapperPath(mapper))
	}
	if m.readMounts()[m.MountPoint(volume.Name)] {
		return nil
	}
	m.runner.Run(ctx, "", "udevadm", "settle")
	devices, err := m.blockDevices(ctx)
	if err != nil {
		return err
	}
	// Mounts at once with every member, otherwise after the usual wait
	// (mountAll goes on trying).
	m.mountBtrfs(ctx, volume, devices)
	return nil
}

// Lock stops what uses an encrypted volume, unmounts it and closes its
// LUKS devices. BeforeLock stops VMs, LXC and shares; the volume counts as
// unmounted for everything else from the start, so Docker, shares and
// background work let go of it. If the volume cannot be unmounted in
// time, everything is put back and ErrBusy says so.
func (m *Manager) Lock(ctx context.Context, name string) error {
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
	if m.GrowthPending() {
		return ErrBusy
	}
	if m.restoreUses(name, "") {
		return fmt.Errorf("%w: a snapshot folder restore is running", ErrBusy)
	}
	m.mu.RLock()
	removing := m.btrfsRemoving[name] != ""
	reshaping := m.btrfsProgress[name].State == VolumeReshaping
	m.mu.RUnlock()
	if removing || reshaping {
		return fmt.Errorf("%w: a disk is being added or removed", ErrBusy)
	}
	m.setLocking(name, true)
	defer m.setLocking(name, false)
	var undo func()
	if m.BeforeLock != nil {
		var err error
		if undo, err = m.BeforeLock(ctx, name); err != nil {
			return err
		}
	}
	fail := func(err error) error {
		if undo != nil {
			undo()
		}
		return err
	}
	point := m.MountPoint(name)
	if m.readMounts()[point] {
		wait := m.lockWait
		if wait == 0 {
			wait = lockWait
		}
		deadline := m.now().Add(wait)
		for {
			_, err := m.runner.Run(ctx, "", "umount", point)
			if err == nil {
				break
			}
			if !m.now().Before(deadline) {
				return fail(fmt.Errorf("%w: %s is still in use: %v", ErrBusy, name, err))
			}
			select {
			case <-ctx.Done():
				return fail(ctx.Err())
			case <-time.After(m.lockPoll()):
			}
		}
	}
	var closeErr error
	if volume.Filesystem == "btrfs" {
		for _, uuid := range volume.Encryption.UUIDs {
			closeErr = errors.Join(closeErr, m.luksClose(ctx, cryptName(volume, uuid)))
		}
	} else {
		closeErr = m.luksClose(ctx, cryptName(volume, ""))
	}
	if closeErr != nil {
		return closeErr
	}
	m.mu.Lock()
	if m.lockNotified == nil {
		m.lockNotified = map[string]bool{}
	}
	// Locked on purpose: no "waiting for password" notice.
	m.lockNotified[name] = true
	m.mu.Unlock()
	slog.Info("encrypted volume locked", "volume", name)
	return nil
}

func (m *Manager) lockPoll() time.Duration {
	if m.lockWait > 0 && m.lockWait < 10*time.Second {
		return m.lockWait / 10
	}
	return 3 * time.Second
}

func (m *Manager) setLocking(name string, on bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locking == nil {
		m.locking = map[string]bool{}
	}
	if on {
		m.locking[name] = true
	} else {
		delete(m.locking, name)
	}
}

func (m *Manager) isLocking(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.locking[name]
}

// unlockable reports a locked volume whose disks are there: the array runs,
// or at least one Btrfs member shows its LUKS container. It sets nothing up
// (no cache device), so it can be asked every round.
func (m *Manager) unlockable(ctx context.Context, volume VolumeConfig) bool {
	if volume.Encryption == nil {
		return false
	}
	if volume.Filesystem != "btrfs" {
		a, ok := m.arrayOf(ctx, volume)
		return ok && a.state != "inactive" && a.state != "clear"
	}
	devices, err := m.blockDevices(ctx)
	return err == nil && len(luksNodes(devices, volume.Encryption.UUIDs)) > 0
}

// Encrypted returns the names of the configured volumes with LUKS.
func (m *Manager) Encrypted() []string {
	var names []string
	for _, volume := range m.configured() {
		if volume.Encryption != nil {
			names = append(names, volume.Name)
		}
	}
	return names
}

// WaitingForPassword returns the locked volumes whose disks are there.
func (m *Manager) WaitingForPassword(ctx context.Context) []string {
	var names []string
	for _, volume := range m.configured() {
		if m.isLocked(volume) && m.unlockable(ctx, volume) {
			names = append(names, volume.Name)
		}
	}
	return names
}

// noticeLocked tells once per start that an encrypted volume whose disks
// are there waits for its password.
func (m *Manager) noticeLocked(ctx context.Context, volume VolumeConfig) {
	m.mu.RLock()
	told := m.lockNotified[volume.Name]
	m.mu.RUnlock()
	if told {
		return
	}
	if !m.unlockable(ctx, volume) {
		return // its disks are not there (yet)
	}
	m.mu.Lock()
	if m.lockNotified == nil {
		m.lockNotified = map[string]bool{}
	}
	m.lockNotified[volume.Name] = true
	m.mu.Unlock()
	slog.Info("encrypted volume waits for its password", "volume", volume.Name)
	if m.OnLocked != nil {
		m.OnLocked(volume.Name)
	}
}

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

// encryptArray puts a LUKS container on a new array and opens it; the
// file system goes onto the returned device.
func (m *Manager) encryptArray(ctx context.Context, name, array string, password []byte) (string, *EncryptionConfig, error) {
	uuid, err := m.luksFormat(ctx, password, array, "titanos-"+name)
	if err != nil {
		return "", nil, err
	}
	volume := VolumeConfig{Name: name}
	mapper := cryptName(volume, "")
	if err := m.luksOpen(ctx, password, array, mapper, false); err != nil {
		return "", nil, err
	}
	return m.mapperPath(mapper), &EncryptionConfig{UUIDs: []string{uuid}}, nil
}

// encryptMember puts a LUKS container on a Btrfs member partition and opens
// it; Btrfs goes onto the returned device.
func (m *Manager) encryptMember(ctx context.Context, name, part string, password []byte) (string, string, error) {
	uuid, err := m.luksFormat(ctx, password, part, "titanos-"+name)
	if err != nil {
		return "", "", err
	}
	mapper := cryptName(VolumeConfig{Name: name, Filesystem: "btrfs"}, uuid)
	if err := m.luksOpen(ctx, password, part, mapper, false); err != nil {
		return "", "", err
	}
	return m.mapperPath(mapper), uuid, nil
}

// encryptNewMember is the step of adding or replacing a disk of an
// encrypted Btrfs volume, after checkNewMemberPassword: the new partition
// gets a LUKS container with the password, and the configuration names the
// new UUID before Btrfs starts using the device. keep filters the UUIDs
// that stay (a replaced member is dropped).
func (m *Manager) encryptNewMember(ctx context.Context, volume VolumeConfig, part string, password []byte, keep func(uuid string) bool) (string, error) {
	device, uuid, err := m.encryptMember(ctx, volume.Name, part, password)
	if err != nil {
		return "", err
	}
	if err := m.updateVolume(volume.Name, func(v *VolumeConfig) {
		uuids := []string{}
		for _, existing := range v.Encryption.UUIDs {
			if keep == nil || keep(existing) {
				uuids = append(uuids, existing)
			}
		}
		v.Encryption = &EncryptionConfig{UUIDs: append(uuids, uuid)}
	}); err != nil {
		m.luksClose(ctx, cryptName(volume, uuid))
		return "", err
	}
	return device, nil
}

// checkNewMemberPassword checks the password before a disk is erased.
func (m *Manager) checkNewMemberPassword(ctx context.Context, volume VolumeConfig, password []byte) error {
	if volume.Encryption == nil {
		return nil
	}
	if len(password) == 0 {
		return ErrPasswordRequired
	}
	if err := m.checkAttempt(volume.Name); err != nil {
		return err
	}
	nodes, err := m.headerDevices(ctx, volume, false)
	if err != nil {
		return err
	}
	list := orderedDevices(volume, nodes)
	err = m.testPassphrase(ctx, password, list[0][1])
	m.noteAttempt(volume.Name, err)
	return err
}

// eraseLUKS destroys the key slots of a LUKS device (cryptsetup erase): its
// contents can never be read again, whatever password someone has.
func (m *Manager) eraseLUKS(ctx context.Context, device string) {
	if _, err := m.runner.Run(ctx, "", "cryptsetup", "erase", "--batch-mode", device); err != nil {
		slog.Warn("erase LUKS header", "device", device, "err", err)
	}
}

// uuidsWithout returns the LUKS UUIDs of a volume without one.
func uuidsWithout(uuids []string, uuid string) []string {
	return slices.DeleteFunc(slices.Clone(uuids), func(u string) bool { return strings.EqualFold(u, uuid) })
}
