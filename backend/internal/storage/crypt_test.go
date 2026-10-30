package storage

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// cryptRunner plays cryptsetup on top of another fake runner: LUKS devices
// accept the passwords in keys, open creates /dev/mapper entries in a
// temporary directory, and every secret that is passed is recorded apart
// from the command lines.
type cryptRunner struct {
	base      Runner
	mapperDir string
	keys      map[string][]string // device → passwords (key slots)
	crypt     []string            // cryptsetup and dmsetup commands
	secrets   []string            // stdin and descriptors of cryptsetup
	dm        *fakeDM
}

func newCryptRunner(base Runner, dir string) *cryptRunner {
	return &cryptRunner{base: base, mapperDir: dir, keys: map[string][]string{}, dm: newFakeDM()}
}

func (r *cryptRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if name == "cryptsetup" || name == "dmsetup" {
		return r.RunSecret(ctx, []byte(stdin), nil, name, args...)
	}
	if name == "btrfs" || name == "wipefs" || name == "mdadm" {
		r.crypt = append(r.crypt, name+" "+strings.Join(args, " "))
	}
	if _, tree := r.base.(*treeRunner); tree && name == "blkid" && slices.Contains(args, "UUID") {
		return testBtrfsUUID + "\n", nil
	}
	return r.base.Run(ctx, stdin, name, args...)
}

// testBtrfsUUID is the UUID blkid reports for new Btrfs file systems here.
const testBtrfsUUID = "b7f5b7f5-0000-4000-8000-000000000001"

// fakeUUID is a stable, well-formed UUID for a name, as the fake tools
// print them.
func fakeUUID(name string) string {
	sum := sha256.Sum256([]byte(name))
	h := hex.EncodeToString(sum[:16])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// poolMapper is the mapper name of a new member of the encrypted Btrfs
// volume "pool" on the given partition.
func poolMapper(part string) string {
	return cryptName(VolumeConfig{Name: "pool", Filesystem: "btrfs"}, fakeUUID("luks-"+part))
}

var errNoKey = &CommandError{Command: "cryptsetup", Output: "No key available with this passphrase.", Err: errors.New("exit status 2")}

func (r *cryptRunner) RunSecret(ctx context.Context, stdin []byte, extra [][]byte, name string, args ...string) (string, error) {
	if name != "cryptsetup" && name != "dmsetup" {
		return r.base.Run(ctx, string(stdin), name, args...)
	}
	command := name + " " + strings.Join(args, " ")
	r.crypt = append(r.crypt, command)
	if len(stdin) > 0 {
		r.secrets = append(r.secrets, string(stdin))
	}
	for _, e := range extra {
		r.secrets = append(r.secrets, string(e))
	}
	if name == "dmsetup" {
		return r.dm.run(args)
	}
	last := args[len(args)-1]
	has := func(device, key string) bool { return slices.Contains(r.keys[device], key) }
	switch args[0] {
	case "luksFormat":
		r.keys[last] = []string{string(stdin)}
	case "luksUUID":
		return fakeUUID("luks-"+strings.ReplaceAll(strings.TrimPrefix(last, "/dev/"), "/", "-")) + "\n", nil
	case "open":
		if slices.Contains(args, "--test-passphrase") {
			if !has(last, string(stdin)) {
				return "", errNoKey
			}
			return "", nil
		}
		device := args[len(args)-2]
		if !has(device, string(stdin)) {
			return "", errNoKey
		}
		os.WriteFile(filepath.Join(r.mapperDir, last), nil, 0o600)
	case "close":
		os.Remove(filepath.Join(r.mapperDir, last))
	case "luksAddKey":
		device := args[len(args)-2]
		if !has(device, string(stdin)) {
			return "", errNoKey
		}
		r.keys[device] = append(r.keys[device], string(extra[0]))
	case "luksRemoveKey":
		if !has(last, string(stdin)) {
			return "", errNoKey
		}
		r.keys[last] = slices.DeleteFunc(r.keys[last], func(k string) bool { return k == string(stdin) })
	case "luksDump":
		var slots []string
		for i := range r.keys[last] {
			slots = append(slots, fmt.Sprintf(`"%d":{"type":"luks2"}`, i))
		}
		return `{"keyslots":{` + strings.Join(slots, ",") + `}}`, nil
	case "luksHeaderBackup":
		os.WriteFile(last, []byte("LUKS\xba\xbe header of "+args[1]), 0o600)
	}
	return "", nil
}

func (r *cryptRunner) commands() string { return strings.Join(r.crypt, "\n") }

const (
	goodPassword  = "correct horse battery"
	otherPassword = "staple and more words"
)

// checkNoSecret fails if a password shows up on a command line or in the
// stored configuration.
func checkNoSecret(t *testing.T, m *Manager, lines ...[]string) {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(m.store.Dir(), configFile))
	for _, secret := range []string{goodPassword, otherPassword} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("password stored in volumes.json: %s", data)
		}
		for _, list := range lines {
			for _, line := range list {
				if strings.Contains(line, secret) {
					t.Fatalf("password on a command line: %q", line)
				}
			}
		}
	}
}

func TestCreateEncryptedArrayPutsFileSystemInsideLUKS(t *testing.T) {
	m, base, _ := newTestManager(t)
	runner := newCryptRunner(base, t.TempDir())
	m.runner, m.mapperDir = runner, runner.mapperDir
	ctx := context.Background()
	disks := []string{"sdd", "sde", "sdf"}
	// A short password is refused before any disk is touched.
	if err := createVolume(m, ctx, CreateRequest{Name: "Safe", Level: "raid5", Filesystem: "ext4", Disks: disks, Erase: disks, Encrypt: true, Password: "short"}); !errors.Is(err, ErrWeakPassword) {
		t.Fatal(err)
	}
	if len(base.commands) != 0 {
		t.Fatalf("disks touched for a weak password: %v", base.commands)
	}
	if err := createVolume(m, ctx, CreateRequest{Name: "Safe", Level: "raid5", Filesystem: "ext4", Disks: disks, Erase: disks, Encrypt: true, Password: goodPassword}); err != nil {
		t.Fatal(err)
	}
	crypt := runner.commands()
	if !strings.Contains(crypt, "cryptsetup luksFormat --batch-mode --type luks2 --pbkdf argon2id --pbkdf-memory 262144 --pbkdf-parallel 4 --iter-time 2000 --label titanos-Safe --key-file=- /dev/md/Safe") {
		t.Fatal(crypt)
	}
	if !strings.Contains(crypt, "cryptsetup open --type luks --disable-keyring --key-file=- /dev/md/Safe titanos-crypt-Safe") {
		t.Fatal(crypt)
	}
	all := strings.Join(base.commands, "\n")
	mapper := filepath.Join(m.mapperDir, "titanos-crypt-Safe")
	if !strings.Contains(all, "mkfs.ext4 -F -q -L Safe -m 0 "+mapper) || !strings.Contains(all, "mount -t ext4 -o noatime "+mapper+" ") {
		t.Fatalf("file system not inside LUKS:\n%s", all)
	}
	if strings.Contains(all, "mkfs.ext4 -F -q -L Safe -m 0 /dev/md/Safe") {
		t.Fatal("file system on the bare array")
	}
	cfg, _ := m.find("Safe")
	if cfg.Encryption == nil || !slices.Equal(cfg.Encryption.UUIDs, []string{fakeUUID("luks-md-Safe")}) {
		t.Fatalf("encryption not recorded: %+v", cfg.Encryption)
	}
	if !m.Owns(fakeUUID("luks-md-Safe")) {
		t.Fatal("LUKS UUID must count as the volume's")
	}
	checkNoSecret(t, m, base.commands, runner.crypt)
	if !slices.Contains(runner.secrets, goodPassword) {
		t.Fatal("password not passed on stdin")
	}
}

// encryptedArray is a manager with an encrypted RAID 5 volume "tank" whose
// array md127 runs and whose LUKS container takes goodPassword.
func encryptedArray(t *testing.T) (*Manager, *treeRunner, *cryptRunner) {
	t.Helper()
	m, tree := treeManager(t, []blockDevice{tdisk("sda", "A", raidPart("sda1")), tdisk("sdb", "B", raidPart("sdb1"))})
	runner := newCryptRunner(tree, t.TempDir())
	m.runner, m.mapperDir = runner, runner.mapperDir
	m.cryptTemp = t.TempDir()
	runningArray(t, m, "1000000", "sda1", "sdb1")
	runner.keys["/dev/md127"] = []string{goodPassword}
	m.volumes = []VolumeConfig{{Name: "tank", Level: "raid5", Filesystem: "ext4", ArrayUUID: runningArrayUUID, FilesystemUUID: "fs", Encryption: &EncryptionConfig{UUIDs: []string{"luks-tank"}}}}
	m.saveVolumes(m.volumes)
	m.loaded = true
	return m, tree, runner
}

func TestLockedVolumeWaitsForItsPassword(t *testing.T) {
	ctx := context.Background()
	m, tree, runner := encryptedArray(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	m.clock = func() time.Time { return now }
	var told []string
	m.OnLocked = func(v string) { told = append(told, v) }
	unlocked := ""
	m.OnUnlocked = func(v string) { unlocked = v }

	m.mountAll(ctx)
	m.mountAll(ctx)
	if len(tree.mounted) != 0 {
		t.Fatal("locked volume mounted")
	}
	if !slices.Equal(told, []string{"tank"}) {
		t.Fatalf("waiting for password told %v", told)
	}
	if !m.Locked("tank") || !slices.Equal(m.LockedNames(), []string{"tank"}) {
		t.Fatal("not locked")
	}
	status, _ := m.Status(ctx)
	if v := status.Volumes[0]; v.Encryption == nil || !v.Encryption.Locked || v.Mounted {
		t.Fatalf("status: %+v", v.Encryption)
	}
	if _, err := m.mountSource(ctx, m.volumes[0]); !errors.Is(err, ErrVolumeLocked) {
		t.Fatal(err)
	}

	// Wrong passwords are counted; after five, attempts have to wait.
	for i := 0; i < freeAttempts; i++ {
		if err := m.Unlock(ctx, "tank", []byte("wrong password")); !errors.Is(err, ErrWrongPassword) {
			t.Fatal(i, err)
		}
	}
	err := m.Unlock(ctx, "tank", []byte(goodPassword))
	var attempts AttemptsError
	if !errors.As(err, &attempts) || attempts.Wait != firstWait || !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("no waiting after five wrong passwords: %v", err)
	}
	now = now.Add(firstWait)
	if err := m.Unlock(ctx, "tank", []byte(goodPassword)); err != nil {
		t.Fatal(err)
	}
	if unlocked != "tank" || m.Locked("tank") {
		t.Fatal("not unlocked")
	}
	if !tree.mounted[m.MountPoint("tank")] {
		t.Fatal("not mounted after unlocking")
	}
	if !strings.Contains(strings.Join(tree.commands, "\n"), "mount -t ext4 -o noatime "+filepath.Join(m.mapperDir, "titanos-crypt-tank")) {
		t.Fatal(tree.commands)
	}
	if m.attemptWait("tank") != 0 {
		t.Fatal("failures not forgotten after the right password")
	}
	status, _ = m.Status(ctx)
	if v := status.Volumes[0]; v.Encryption.Locked || v.Encryption.Passwords != 1 {
		t.Fatalf("after unlock: %+v", v.Encryption)
	}
	checkNoSecret(t, m, tree.commands, runner.crypt)
}

func TestWaitAfterWrongPasswordsGrows(t *testing.T) {
	m, _, _ := newTestManager(t)
	now := time.Now()
	m.clock = func() time.Time { return now }
	for i := 0; i < freeAttempts+3; i++ {
		m.noteAttempt("v", ErrWrongPassword)
	}
	if got := m.attemptWait("v"); got != 8*firstWait {
		t.Fatal(got)
	}
	for i := 0; i < 20; i++ {
		m.noteAttempt("v", ErrWrongPassword)
	}
	if got := m.attemptWait("v"); got != maxAttemptWait {
		t.Fatal(got)
	}
	// Other errors (a missing disk) do not count.
	m.noteAttempt("w", ErrNotPossible)
	if m.attemptWait("w") != 0 {
		t.Fatal("counted")
	}
}

func TestLockStopsUsersUnmountsAndCloses(t *testing.T) {
	ctx := context.Background()
	m, tree, runner := encryptedArray(t)
	if err := m.Unlock(ctx, "tank", []byte(goodPassword)); err != nil {
		t.Fatal(err)
	}
	stoppedWhileHidden := false
	m.BeforeLock = func(_ context.Context, v string) (func(), error) {
		// What asks for mounted volumes lets go of it from now on.
		stoppedWhileHidden = v == "tank" && !slices.Contains(m.Mounted(), "tank")
		return func() { t.Fatal("undone after a lock that worked") }, nil
	}
	told := 0
	m.OnLocked = func(string) { told++ }
	if err := m.Lock(ctx, "tank"); err != nil {
		t.Fatal(err)
	}
	if !stoppedWhileHidden {
		t.Fatal("BeforeLock did not run while the volume was hidden")
	}
	all := strings.Join(tree.commands, "\n")
	if !strings.Contains(all, "umount "+m.MountPoint("tank")) || !strings.Contains(runner.commands(), "cryptsetup close titanos-crypt-tank") {
		t.Fatalf("%s\n%s", all, runner.commands())
	}
	if !m.Locked("tank") {
		t.Fatal("still open")
	}
	// Locked on purpose: no "waiting for password" notice.
	m.mountAll(ctx)
	if told != 0 {
		t.Fatal("notified after a manual lock")
	}
}

// busyRunner fails umount.
type busyRunner struct{ *cryptRunner }

func (r busyRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if name == "umount" {
		return "", &CommandError{Command: "umount", Output: "target is busy", Err: errors.New("exit status 32")}
	}
	return r.cryptRunner.Run(ctx, stdin, name, args...)
}

func TestLockThatCannotUnmountPutsEverythingBack(t *testing.T) {
	ctx := context.Background()
	m, _, runner := encryptedArray(t)
	if err := m.Unlock(ctx, "tank", []byte(goodPassword)); err != nil {
		t.Fatal(err)
	}
	m.runner = busyRunner{runner}
	m.lockWait = 50 * time.Millisecond
	undone := false
	m.BeforeLock = func(context.Context, string) (func(), error) { return func() { undone = true }, nil }
	if err := m.Lock(ctx, "tank"); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if !undone || m.Locked("tank") || !slices.Contains(m.Mounted(), "tank") {
		t.Fatal("not put back")
	}
	if strings.Contains(runner.commands(), "close") {
		t.Fatal("closed while mounted")
	}
}

func TestEncryptionSitsAboveTheSSDCache(t *testing.T) {
	ctx := context.Background()
	m, _, runner := encryptedArray(t)
	m.partUUIDDir = t.TempDir()
	for _, uuid := range []string{"aaaa-meta", "bbbb-data"} {
		os.WriteFile(filepath.Join(m.partUUIDDir, uuid), nil, 0o600)
	}
	m.volumes[0].Cache = &CacheConfig{Mode: "writeback", MetaUUID: "aaaa-meta", DataUUID: "bbbb-data"}
	runner.keys["/dev/mapper/titanos-cache-tank"] = []string{goodPassword}
	if err := m.Unlock(ctx, "tank", []byte(goodPassword)); err != nil {
		t.Fatal(err)
	}
	crypt := runner.commands()
	create := strings.Index(crypt, "dmsetup create titanos-cache-tank")
	open := strings.Index(crypt, "cryptsetup open --type luks --disable-keyring --key-file=- /dev/mapper/titanos-cache-tank titanos-crypt-tank")
	if create < 0 || open < create {
		t.Fatalf("LUKS must be opened on the cache device, after it is set up:\n%s", crypt)
	}
	if strings.Contains(crypt, "/dev/md127 titanos-crypt-tank") {
		t.Fatal("opened on the bare array, past the cache")
	}
}

func TestChangePasswordAddsBeforeItRemoves(t *testing.T) {
	ctx := context.Background()
	m, tree, runner := encryptedArray(t)
	if err := m.ChangePassword(ctx, "tank", []byte("not the password"), []byte(otherPassword)); !errors.Is(err, ErrWrongPassword) {
		t.Fatal(err)
	}
	if strings.Contains(runner.commands(), "luksAddKey") {
		t.Fatal("changed with a wrong password")
	}
	if err := m.ChangePassword(ctx, "tank", []byte(goodPassword), []byte("tiny")); !errors.Is(err, ErrWeakPassword) {
		t.Fatal(err)
	}
	if err := m.ChangePassword(ctx, "tank", []byte(goodPassword), []byte(otherPassword)); err != nil {
		t.Fatal(err)
	}
	crypt := runner.commands()
	add, remove := strings.Index(crypt, "cryptsetup luksAddKey"), strings.Index(crypt, "cryptsetup luksRemoveKey --key-file=- /dev/md127")
	if add < 0 || remove < add {
		t.Fatalf("new key slot must come first:\n%s", crypt)
	}
	if !strings.Contains(crypt, "luksAddKey --pbkdf argon2id --pbkdf-memory 262144 --pbkdf-parallel 4 --iter-time 2000 --key-file=- /dev/md127 /dev/fd/3") {
		t.Fatal(crypt)
	}
	if !slices.Equal(runner.keys["/dev/md127"], []string{otherPassword}) {
		t.Fatalf("key slots: %v", runner.keys["/dev/md127"])
	}
	checkNoSecret(t, m, tree.commands, runner.crypt)

	// A second password, then removing one; never the last.
	if err := m.AddPassword(ctx, "tank", []byte(otherPassword), []byte(goodPassword)); err != nil {
		t.Fatal(err)
	}
	if err := m.RemovePassword(ctx, "tank", []byte(otherPassword)); err != nil {
		t.Fatal(err)
	}
	if err := m.RemovePassword(ctx, "tank", []byte(goodPassword)); !errors.Is(err, ErrLastPassword) {
		t.Fatal(err)
	}
	if !slices.Equal(runner.keys["/dev/md127"], []string{goodPassword}) {
		t.Fatalf("key slots: %v", runner.keys["/dev/md127"])
	}
}

func TestHeaderBackupIsATarOfEveryHeader(t *testing.T) {
	m, _, _ := encryptedArray(t)
	var out bytes.Buffer
	if err := m.HeaderBackup(context.Background(), "tank", &out); err != nil {
		t.Fatal(err)
	}
	archive := tar.NewReader(&out)
	var names []string
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(archive)
		if strings.HasSuffix(header.Name, ".img") && !strings.Contains(string(data), "/dev/md127") {
			t.Fatalf("header of the wrong device: %q", data)
		}
		names = append(names, header.Name)
	}
	if !slices.Equal(names, []string{"README.txt", "tank-luks-luks-tank.img"}) {
		t.Fatal(names)
	}
	// Nothing stays behind in RAM.
	if left, _ := os.ReadDir(m.cryptTemp); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

func TestDeleteEncryptedArrayDestroysTheKeySlots(t *testing.T) {
	ctx := context.Background()
	m, _, runner := encryptedArray(t)
	if err := m.Unlock(ctx, "tank", []byte(goodPassword)); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, "tank", "tank"); err != nil {
		t.Fatal(err)
	}
	crypt := runner.commands()
	if !strings.Contains(crypt, "cryptsetup close titanos-crypt-tank") || !strings.Contains(crypt, "cryptsetup erase --batch-mode /dev/md127") {
		t.Fatal(crypt)
	}
	// The header signatures go as well, before the array is stopped.
	wipe := strings.Index(crypt, "wipefs --all --force /dev/md127")
	if wipe < 0 || wipe > strings.Index(crypt, "mdadm --stop") {
		t.Fatal(crypt)
	}
}

// luksPart is a Btrfs member partition with LUKS; open adds the opened
// device with Btrfs on it below.
func luksPart(name, uuid string, open bool) blockDevice {
	part := blockDevice{Name: "/dev/" + name, KName: "/dev/" + name, Type: "part", Size: 7 << 30, FSType: "crypto_LUKS", UUID: uuid}
	if open {
		part.Children = []blockDevice{{Name: "/dev/mapper/titanos-crypt-pool-" + uuid[:8], KName: "/dev/dm-" + name, Type: "crypt", Size: 7<<30 - 16<<20, FSType: "btrfs", UUID: testBtrfsUUID}}
	}
	return part
}

func encryptedBtrfs(t *testing.T, open bool) (*Manager, *treeRunner, *cryptRunner) {
	t.Helper()
	m, tree := treeManager(t, []blockDevice{
		tdisk("sda", "A", luksPart("sda1", "aaaaaaaa-1111", open)),
		tdisk("sdb", "B", luksPart("sdb1", "bbbbbbbb-2222", open)),
	})
	runner := newCryptRunner(tree, t.TempDir())
	m.runner, m.mapperDir = runner, runner.mapperDir
	runner.keys["/dev/sda1"] = []string{goodPassword}
	runner.keys["/dev/sdb1"] = []string{goodPassword}
	m.volumes = []VolumeConfig{{Name: "pool", Level: "raid1", Profile: "raid1", Devices: 2, Filesystem: "btrfs", FilesystemUUID: testBtrfsUUID, Encryption: &EncryptionConfig{UUIDs: []string{"aaaaaaaa-1111", "bbbbbbbb-2222"}}}}
	m.saveVolumes(m.volumes)
	m.loaded = true
	return m, tree, runner
}

func TestLockedBtrfsMembersBelongToTheVolume(t *testing.T) {
	m, _, _ := encryptedBtrfs(t, false)
	status, err := m.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, disk := range status.Disks {
		if disk.Role != RoleMember || disk.Volume != "pool" {
			t.Fatalf("locked member offered elsewhere: %+v", disk)
		}
	}
	if v := status.Volumes[0]; v.Encryption == nil || !v.Encryption.Locked || v.Encryption.Members != 2 {
		t.Fatalf("%+v", v.Encryption)
	}
	if !m.Owns("bbbbbbbb-2222") {
		t.Fatal("LUKS member UUID not reserved")
	}
}

func TestUnlockBtrfsOpensEveryMemberWithOnePassword(t *testing.T) {
	ctx := context.Background()
	m, tree, runner := encryptedBtrfs(t, false)
	// One member with a different password: nothing stays open.
	runner.keys["/dev/sdb1"] = []string{otherPassword}
	if err := m.Unlock(ctx, "pool", []byte(goodPassword)); !errors.Is(err, ErrWrongPassword) {
		t.Fatal(err)
	}
	if m.openMembers(m.volumes[0]) != 0 {
		t.Fatal("half open after a failed unlock")
	}
	runner.keys["/dev/sdb1"] = []string{goodPassword}
	// The opened devices show up with Btrfs on them.
	tree.devices = []blockDevice{tdisk("sda", "A", luksPart("sda1", "aaaaaaaa-1111", true)), tdisk("sdb", "B", luksPart("sdb1", "bbbbbbbb-2222", true))}
	if err := m.Unlock(ctx, "pool", []byte(goodPassword)); err != nil {
		t.Fatal(err)
	}
	crypt := runner.commands()
	for _, want := range []string{"/dev/sda1 titanos-crypt-pool-aaaaaaaa", "/dev/sdb1 titanos-crypt-pool-bbbbbbbb"} {
		if !strings.Contains(crypt, want) {
			t.Fatalf("missing %q:\n%s", want, crypt)
		}
	}
	if !tree.mounted[m.MountPoint("pool")] {
		t.Fatalf("not mounted: %v", tree.commands)
	}
}

func TestCreateEncryptedBtrfsHasLUKSOnEveryMember(t *testing.T) {
	m, tree := treeManager(t, []blockDevice{tdisk("sdc", "C"), tdisk("sdd", "D")})
	runner := newCryptRunner(tree, t.TempDir())
	m.runner, m.mapperDir = runner, runner.mapperDir
	m.loaded = true
	disks := []string{"sdc", "sdd"}
	if err := createVolume(m, context.Background(), CreateRequest{Name: "pool", Level: "raid1", Filesystem: "btrfs", Disks: disks, Erase: disks, Encrypt: true, Password: goodPassword}); err != nil {
		t.Fatal(err)
	}
	crypt := runner.commands()
	for _, want := range []string{"--key-file=- /dev/sdc1", "--key-file=- /dev/sdd1", "open --type luks --disable-keyring --key-file=- /dev/sdc1 " + poolMapper("sdc1")} {
		if !strings.Contains(crypt, want) {
			t.Fatalf("missing %q:\n%s", want, crypt)
		}
	}
	all := strings.Join(tree.commands, "\n")
	if !strings.Contains(all, "mkfs.btrfs -f -L pool -d raid1 -m raid1 "+filepath.Join(m.mapperDir, poolMapper("sdc1"))+" "+filepath.Join(m.mapperDir, poolMapper("sdd1"))) {
		t.Fatalf("Btrfs not on the opened devices:\n%s", all)
	}
	cfg, _ := m.find("pool")
	if cfg.Encryption == nil || !slices.Equal(cfg.Encryption.UUIDs, []string{fakeUUID("luks-sdc1"), fakeUUID("luks-sdd1")}) {
		t.Fatalf("%+v", cfg.Encryption)
	}
	checkNoSecret(t, m, tree.commands, runner.crypt)
}

func TestAddDiskToEncryptedBtrfsNeedsThePassword(t *testing.T) {
	ctx := context.Background()
	m, tree, runner := encryptedBtrfs(t, true)
	tree.devices = append(tree.devices, tdisk("sdc", "C"))
	tree.mounted[m.MountPoint("pool")] = true
	if err := m.AddBtrfsDiskWithPassword(ctx, "pool", "sdc", "", idOf(m, "sdc"), true, nil); !errors.Is(err, ErrPasswordRequired) {
		t.Fatal(err)
	}
	if err := m.AddBtrfsDiskWithPassword(ctx, "pool", "sdc", "", idOf(m, "sdc"), true, []byte("wrong password")); !errors.Is(err, ErrWrongPassword) {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(tree.commands, "\n"), "sdc") {
		t.Fatal("disk erased before the password was checked")
	}
	if err := m.AddBtrfsDiskWithPassword(ctx, "pool", "sdc", "", idOf(m, "sdc"), true, []byte(goodPassword)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runner.commands(), "--key-file=- /dev/sdc1") {
		t.Fatal(runner.commands())
	}
	if !strings.Contains(runner.commands(), "btrfs device add -f "+filepath.Join(m.mapperDir, poolMapper("sdc1"))+" "+m.MountPoint("pool")) {
		t.Fatal(runner.commands())
	}
	cfg, _ := m.find("pool")
	if len(cfg.Encryption.UUIDs) != 3 || cfg.Encryption.UUIDs[2] != fakeUUID("luks-sdc1") {
		t.Fatalf("%+v", cfg.Encryption)
	}
}

func TestImportListsEncryptedArraysAndRefusesLooseLUKS(t *testing.T) {
	md := blockDevice{Name: "/dev/md127", KName: "/dev/md127", Type: "raid5", Size: 4 << 30, FSType: "crypto_LUKS", UUID: "luks-import"}
	m, _ := treeManager(t, []blockDevice{
		tdisk("sda", "A", raidPart("sda1", md)), tdisk("sdb", "B", raidPart("sdb1", md)),
		tdisk("sdc", "C", blockDevice{Name: "/dev/sdc1", KName: "/dev/sdc1", Type: "part", Size: 1 << 30, FSType: "crypto_LUKS", UUID: "loose"}),
	})
	runningArray(t, m, "1000000", "sda1", "sdb1")
	writeSys(t, filepath.Join(m.sysBlock, "md127", "md", "raid_disks"), "2")
	list, err := m.ImportCandidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]ImportCandidate{}
	for _, c := range list {
		found[c.ID] = c
	}
	if c := found["md:"+normalizeUUID(runningArrayUUID)]; !c.Encrypted || c.Reason != "" || c.EncryptionUUID != "luks-import" {
		t.Fatalf("encrypted array: %+v", c)
	}
	if c := found["luks:loose"]; c.Reason != "encrypted-partition" {
		t.Fatalf("loose LUKS: %+v", c)
	}
}

// encryptedCachedArray is encryptedArray with a running write cache below
// the encryption, holding dirty blocks.
func encryptedCachedArray(t *testing.T, dirty uint64) (*Manager, *treeRunner, *cryptRunner) {
	t.Helper()
	ctx := context.Background()
	m, tree, runner := encryptedArray(t)
	m.partUUIDDir = t.TempDir()
	for _, uuid := range []string{testMetaUUID, testDataUUID} {
		os.WriteFile(filepath.Join(m.partUUIDDir, uuid), nil, 0o600)
	}
	m.zero = func(string) error { return nil }
	m.volumes[0].Cache = &CacheConfig{Mode: "writeback", MetaUUID: testMetaUUID, DataUUID: testDataUUID}
	m.saveVolumes(m.volumes)
	runner.keys["/dev/mapper/titanos-cache-tank"] = []string{goodPassword}
	if err := m.Unlock(ctx, "tank", []byte(goodPassword)); err != nil {
		t.Fatal(err)
	}
	runner.dm.dirty["titanos-cache-tank"] = dirty
	runner.crypt = nil
	return m, tree, runner
}

func TestDeleteEncryptedArrayWritesTheCacheBackBeforeErasing(t *testing.T) {
	old := cacheFlushPoll
	cacheFlushPoll = time.Millisecond
	defer func() { cacheFlushPoll = old }()
	m, _, runner := encryptedCachedArray(t, 9)
	runner.dm.cleanAfter = 2
	if err := m.Delete(context.Background(), "tank", "tank"); err != nil {
		t.Fatal(err)
	}
	crypt := runner.commands()
	order := []string{
		"cryptsetup close titanos-crypt-tank",
		"writeback cleaner 0",
		"writethrough cleaner 0",
		"cryptsetup erase --batch-mode /dev/mapper/titanos-cache-tank",
		"wipefs --all --force /dev/mapper/titanos-cache-tank",
		"dmsetup remove titanos-cache-tank",
		"mdadm --stop",
	}
	at := -1
	for _, want := range order {
		i := strings.Index(crypt, want)
		if i < at {
			t.Fatalf("%q out of order or missing:\n%s", want, crypt)
		}
		at = i
	}
	if runner.dm.dirty["titanos-cache-tank"] != 0 {
		t.Fatal("dirty blocks left")
	}
}

func TestDeleteEncryptedArrayWaitsForTheCacheOrRefuses(t *testing.T) {
	oldWait, oldPoll := cacheFlushWait, cacheFlushPoll
	cacheFlushWait, cacheFlushPoll = 5*time.Millisecond, time.Millisecond
	defer func() { cacheFlushWait, cacheFlushPoll = oldWait, oldPoll }()
	m, _, runner := encryptedCachedArray(t, 9)
	runner.dm.cleanAfter = -1
	if err := m.Delete(context.Background(), "tank", "tank"); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	crypt := runner.commands()
	if strings.Contains(crypt, "erase") || strings.Contains(crypt, "dmsetup remove") || strings.Contains(crypt, "mdadm --stop") {
		t.Fatalf("went on with dirty blocks:\n%s", crypt)
	}
	if _, ok := m.find("tank"); !ok {
		t.Fatal("volume forgotten")
	}
}

func TestDeleteEncryptedArrayStopsWhenEraseFails(t *testing.T) {
	ctx := context.Background()
	m, _, runner := encryptedArray(t)
	if err := m.Unlock(ctx, "tank", []byte(goodPassword)); err != nil {
		t.Fatal(err)
	}
	m.runner = &failingErase{runner}
	if err := m.Delete(ctx, "tank", "tank"); err == nil {
		t.Fatal("deleted although the key slots are still there")
	}
	if strings.Contains(runner.commands(), "mdadm --stop") {
		t.Fatal("array stopped")
	}
}

type failingErase struct{ *cryptRunner }

func (r *failingErase) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if name == "cryptsetup" && args[0] == "erase" {
		return "", &CommandError{Command: "cryptsetup erase", Output: "Device busy", Err: errors.New("exit status 5")}
	}
	return r.cryptRunner.Run(ctx, stdin, name, args...)
}
