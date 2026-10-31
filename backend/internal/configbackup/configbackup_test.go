package configbackup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"github.com/ra5on/TitanOS/backend/internal/retention"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// fakeSystem answers blkid and takes mount and umount without doing anything:
// the test puts the "mounted" files where the mount would show them.
type fakeSystem struct {
	kind  string
	calls []string
}

func (f *fakeSystem) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if name == "blkid" {
		return "DEVNAME=" + args[len(args)-1] + "\nLABEL=tank\nTYPE=" + f.kind + "\n", nil
	}
	return "", nil
}

type env struct {
	m      *Manager
	store  *config.Store
	root   string
	system *fakeSystem
	clock  time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	root := t.TempDir()
	e := &env{root: root, system: &fakeSystem{kind: "ext4"}, clock: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)}
	for _, dir := range []string{"config", "mnt/tank", "mnt/fast", "run", "sys"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e.store = config.Open(filepath.Join(root, "config"))
	e.m = NewManager(e.store, "1.2.3")
	e.m.runner, e.m.mountRoot, e.m.scanRoot, e.m.sysBlock = e.system, filepath.Join(root, "mnt"), filepath.Join(root, "run"), filepath.Join(root, "sys")
	e.m.now = func() time.Time { return e.clock }
	if err := e.m.Load(); err != nil {
		t.Fatal(err)
	}
	e.write(t, "users/alex/account.json", `{"name":"alex"}`)
	e.write(t, "storage/volumes.json", `[]`)
	return e
}

func (e *env) write(t *testing.T, name, content string) {
	t.Helper()
	if err := e.store.Write(name, []byte(content)); err != nil {
		t.Fatal(err)
	}
}

func (e *env) create(t *testing.T) string {
	t.Helper()
	name, err := e.m.Create([]string{"tank", "fast"})
	if err != nil {
		t.Fatal(err)
	}
	return name
}

func TestCreateOnlyWhenSomethingChanged(t *testing.T) {
	e := newEnv(t)
	first := e.create(t)
	// "fast" sorts first and is the target while none is chosen.
	if _, err := os.Stat(filepath.Join(e.root, "mnt/fast", folder, first)); err != nil {
		t.Fatalf("backup not on the first volume: %v", err)
	}
	if info, _ := os.Stat(filepath.Join(e.root, "mnt/fast", folder)); info.Mode().Perm() != 0o700 {
		t.Errorf("the folder is open to others: %v", info.Mode())
	}

	// A backup that was cut short leaves a part file; the next one clears it.
	os.WriteFile(filepath.Join(e.root, "mnt/fast", folder, ".config-123.part"), []byte("half"), 0o600)
	e.clock = e.clock.Add(time.Hour)
	if again := e.create(t); again != first {
		t.Errorf("unchanged configuration saved again: %s", again)
	}
	e.write(t, "shares/shares.json", `[{"name":"media"}]`)
	second := e.create(t)
	if second == first {
		t.Fatal("changed configuration not saved")
	}
	list := e.m.List([]string{"tank", "fast"})
	if len(list) != 2 || list[0].Name != second || list[0].Volume != "fast" || list[0].Version != "1.2.3" || !list[0].Time.Equal(e.clock) {
		t.Errorf("list: %+v", list)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(e.root, "mnt/fast", folder, ".config-*")); len(leftovers) != 0 {
		t.Errorf("part files left behind: %v", leftovers)
	}
}

func TestKeepsOnlyTheNewest(t *testing.T) {
	e := newEnv(t)
	if err := e.m.SetSettings(Settings{Enabled: true, Volume: "tank", Keep: 3}, []string{"tank", "fast"}); err != nil {
		t.Fatal(err)
	}
	var names []string
	for i := range 5 {
		e.clock = e.clock.Add(time.Hour)
		e.write(t, "system/settings.json", strings.Repeat("x", i+1))
		names = append(names, e.create(t))
	}
	list := e.m.List([]string{"tank"})
	if len(list) != 3 || list[0].Name != names[4] || list[2].Name != names[2] {
		t.Errorf("kept: %+v", list)
	}
}

func TestTargetMustBeMounted(t *testing.T) {
	e := newEnv(t)
	if _, err := e.m.Create(nil); !errors.Is(err, ErrNoVolume) {
		t.Errorf("without a volume: %v", err)
	}
	if err := e.m.SetSettings(Settings{Enabled: true, Volume: "tank", Keep: 14}, []string{"tank"}); err != nil {
		t.Fatal(err)
	}
	// The chosen volume is not mounted: nothing may land in its empty
	// mount point, and no other volume is taken instead.
	if _, err := e.m.Create([]string{"fast"}); !errors.Is(err, ErrNoVolume) {
		t.Errorf("chosen volume not mounted: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(e.root, "mnt/tank")); len(entries) != 0 {
		t.Errorf("written below a mount point that is not mounted: %v", entries)
	}
	if err := e.m.SetSettings(Settings{Enabled: true, Volume: "nowhere", Keep: 14}, []string{"tank"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown volume: %v", err)
	}
	if err := e.m.SetSettings(Settings{Enabled: true, Keep: 0}, nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("keep 0: %v", err)
	}
}

func TestResultIsReported(t *testing.T) {
	e := newEnv(t)
	var results []string
	e.m.OnResult = func(volume string, err error) {
		if err != nil {
			results = append(results, volume+": failed")
		} else {
			results = append(results, volume+": ok")
		}
	}
	e.m.Create(nil) // no volume yet is not a failure
	// A file where the folder should be.
	os.MkdirAll(filepath.Join(e.root, "mnt/fast/.titanos"), 0o755)
	os.WriteFile(filepath.Join(e.root, "mnt/fast", folder), nil, 0o600)
	e.m.Create([]string{"fast"})
	os.Remove(filepath.Join(e.root, "mnt/fast", folder))
	e.m.Create([]string{"fast"})
	if got := strings.Join(results, ", "); got != "fast: failed, fast: ok" {
		t.Errorf("results: %s", got)
	}
	state := e.m.State([]string{"fast"})
	if state.LastError != "" || state.LastCheck == nil || state.Target != "fast" || len(state.Backups) != 1 {
		t.Errorf("state: %+v", state)
	}
}

// newStick returns an environment with an empty /config and the backups of
// e on an array that is not mounted as a volume.
func newStick(t *testing.T, e *env) *env {
	t.Helper()
	stick := newEnv(t)
	os.RemoveAll(filepath.Join(stick.root, "config"))
	os.MkdirAll(filepath.Join(stick.root, "config"), 0o755)
	os.MkdirAll(filepath.Join(stick.root, "sys/md127"), 0o755)
	os.MkdirAll(filepath.Join(stick.root, "sys/sda"), 0o755)
	// What mounting the array would show.
	if err := os.CopyFS(filepath.Join(stick.root, "run/md127"), os.DirFS(filepath.Join(e.root, "mnt/fast"))); err != nil {
		t.Fatal(err)
	}
	return stick
}

func TestRestoreOnANewStick(t *testing.T) {
	e := newEnv(t)
	e.write(t, "apps/jellyfin/.env", "SECRET=1\n")
	e.write(t, "notify/settings.json", `{"language":"de"}`)
	name := e.create(t)

	stick := newStick(t, e)
	found := stick.m.Find(context.Background())
	if len(found) != 1 || found[0].Name != name || found[0].Device != "md127" || found[0].Label != "tank" || found[0].Volume != "" {
		t.Fatalf("found: %+v", found)
	}
	calls := strings.Join(stick.system.calls, "\n")
	if !strings.Contains(calls, "mount -t ext4 -o ro,nodev,nosuid,noexec,noload /dev/md127") || !strings.Contains(calls, "umount ") {
		t.Errorf("not mounted read-only without replaying the journal, or not unmounted:\n%s", calls)
	}

	if _, err := stick.m.RestoreFound(context.Background(), "md127", "config-20200101T000000Z-000000000000.tar.gz", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown backup: %v", err)
	}
	if _, err := stick.m.RestoreFound(context.Background(), "../md127", name, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("odd device name: %v", err)
	}
	manifest, err := stick.m.RestoreFound(context.Background(), "md127", name, "")
	if err != nil || manifest.Version != "1.2.3" {
		t.Fatalf("restore: %+v %v", manifest, err)
	}
	for file, want := range map[string]string{"users/alex/account.json": `{"name":"alex"}`, "apps/jellyfin/.env": "SECRET=1\n", "notify/settings.json": `{"language":"de"}`} {
		if got, err := stick.store.Read(file); err != nil || string(got) != want {
			t.Errorf("%s: %q %v", file, got, err)
		}
	}
	if _, err := stick.store.Read(manifestName); err == nil {
		t.Error("the manifest was written to /config")
	}
}

func TestXFSIsMountedWithoutRecovery(t *testing.T) {
	e := newEnv(t)
	e.create(t)
	stick := newStick(t, e)
	stick.system.kind = "xfs"
	if len(stick.m.Find(context.Background())) != 1 || !strings.Contains(strings.Join(stick.system.calls, "\n"), "-t xfs -o ro,nodev,nosuid,noexec,norecovery") {
		t.Errorf("calls: %v", stick.system.calls)
	}
	stick.system.kind, stick.system.calls = "ntfs", nil
	if len(stick.m.Find(context.Background())) != 0 || strings.Contains(strings.Join(stick.system.calls, "\n"), "mount") {
		t.Errorf("a foreign file system was mounted: %v", stick.system.calls)
	}
}

// archive builds a tar.gz from entries of name, type and content.
func archive(t *testing.T, entries ...[3]string) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	zip := gzip.NewWriter(&buf)
	w := tar.NewWriter(zip)
	for _, entry := range entries {
		header := &tar.Header{Name: entry[0], Mode: 0o600, Size: int64(len(entry[2])), Typeflag: tar.TypeReg}
		switch entry[1] {
		case "link":
			header.Typeflag, header.Linkname, header.Size = tar.TypeSymlink, entry[2], 0
		case "dir":
			header.Typeflag, header.Size = tar.TypeDir, 0
		}
		if err := w.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			w.Write([]byte(entry[2]))
		}
	}
	w.Close()
	zip.Close()
	return bytes.NewReader(buf.Bytes())
}

func TestUploadIsCheckedBeforeAnythingIsWritten(t *testing.T) {
	manifest := [3]string{manifestName, "", `{"format":1,"time":"2026-10-13T12:00:00Z","hostname":"nas","version":"1.0.0"}`}
	account := [3]string{"users/alex/account.json", "", `{}`}
	bad := map[string]*bytes.Reader{
		"not an archive":      bytes.NewReader([]byte("hello")),
		"no manifest":         archive(t, account),
		"manifest not first":  archive(t, account, manifest),
		"unknown format":      archive(t, [3]string{manifestName, "", `{"format":9}`}, account),
		"no account":          archive(t, manifest, [3]string{"shares/shares.json", "", `[]`}),
		"leaves the folder":   archive(t, manifest, account, [3]string{"../etc/passwd", "", "x"}),
		"absolute path":       archive(t, manifest, account, [3]string{"/etc/passwd", "", "x"}),
		"symbolic link":       archive(t, manifest, account, [3]string{"shares/link", "link", "/etc/shadow"}),
		"a second manifest":   archive(t, manifest, account, manifest),
		"backslash in a name": archive(t, manifest, account, [3]string{`shares\..\x`, "", "x"}),
	}
	for name, upload := range bad {
		stick := newEnv(t)
		os.RemoveAll(filepath.Join(stick.root, "config"))
		os.MkdirAll(filepath.Join(stick.root, "config"), 0o755)
		if _, err := stick.m.RestoreUpload(upload, ""); !errors.Is(err, ErrNotABackup) {
			t.Errorf("%s: %v", name, err)
		}
		if entries, _ := os.ReadDir(filepath.Join(stick.root, "config")); len(entries) != 0 {
			t.Errorf("%s: something was written to /config: %v", name, entries)
		}
		if spooled, _ := filepath.Glob(filepath.Join(stick.root, "run/upload-*")); len(spooled) != 0 {
			t.Errorf("%s: the upload was left behind", name)
		}
	}

	stick := newEnv(t)
	os.RemoveAll(filepath.Join(stick.root, "config"))
	os.MkdirAll(filepath.Join(stick.root, "config"), 0o755)
	good := archive(t, manifest, [3]string{"users", "dir", ""}, account, [3]string{"network/network.json", "", `{"dhcp":true}`})
	got, err := stick.m.RestoreUpload(good, "")
	if err != nil || got.Hostname != "nas" {
		t.Fatalf("restore: %+v %v", got, err)
	}
	if data, _ := stick.store.Read("network/network.json"); string(data) != `{"dhcp":true}` {
		t.Errorf("network.json: %q", data)
	}
}

func TestDevelopmentModeTouchesNothing(t *testing.T) {
	e := newEnv(t)
	e.m.Disabled = true
	if _, err := e.m.Create([]string{"tank"}); !errors.Is(err, ErrDisabled) {
		t.Errorf("create: %v", err)
	}
	if _, err := e.m.RestoreUpload(strings.NewReader(""), ""); !errors.Is(err, ErrDisabled) {
		t.Errorf("restore: %v", err)
	}
	if len(e.m.Find(context.Background())) != 0 || len(e.system.calls) != 0 {
		t.Errorf("looked at the arrays: %v", e.system.calls)
	}
}

type btrfsScanRunner struct{ calls []string }

func (r *btrfsScanRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	line := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, line)
	if name == "blkid" && len(args) > 0 && args[0] == "-t" {
		return "DEVNAME=/dev/vda1\nUUID=mirror\nTYPE=btrfs\n\nDEVNAME=/dev/vdb1\nUUID=mirror\nTYPE=btrfs\n\nDEVNAME=/dev/../../escape\nUUID=other\nTYPE=btrfs\n", nil
	}
	if name == "blkid" {
		return "TYPE=btrfs\nLABEL=mirror\n", nil
	}
	return "", nil
}
func TestBtrfsRecoveryScansMirrorOnceWithoutLogReplay(t *testing.T) {
	e := newEnv(t)
	r := &btrfsScanRunner{}
	e.m.runner = r
	devices := e.m.candidates(context.Background())
	if len(devices) != 1 || devices[0] != "vda1" {
		t.Fatal(devices)
	}
	if err := e.m.look(context.Background(), devices[0], func(string, string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	commands := strings.Join(r.calls, "\n")
	if !strings.Contains(commands, "ro,nodev,nosuid,noexec,nologreplay,subvolid=5,degraded /dev/vda1") {
		t.Fatal(commands)
	}
}

// Saving that keeps finding no volume is told once a day has passed, not on
// every attempt and not again until a backup worked.
func TestMissingVolumeIsToldOnce(t *testing.T) {
	start := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	var o outage
	if o.long(start) || o.long(start.Add(23*time.Hour)) {
		t.Fatal("told before a day passed")
	}
	if !o.long(start.Add(24 * time.Hour)) {
		t.Fatal("not told after a day")
	}
	if o.long(start.Add(48 * time.Hour)) {
		t.Fatal("told twice")
	}
}

// A configuration that restoring would refuse is not saved as if it were a
// backup; the failure is reported now.
func TestBackupBeyondTheRestoreLimitsFails(t *testing.T) {
	e := newEnv(t)
	old := maxFiles
	maxFiles = 1
	defer func() { maxFiles = old }()
	var reported error
	e.m.OnResult = func(volume string, err error) { reported = err }
	if _, err := e.m.Create([]string{"tank"}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("too many files: %v", err)
	}
	if !errors.Is(reported, ErrTooLarge) {
		t.Errorf("not reported: %v", reported)
	}
	if backups := e.m.List([]string{"tank"}); len(backups) != 0 {
		t.Errorf("a backup that cannot be restored was kept: %v", backups)
	}
	maxFiles = old
	if _, err := e.m.Create([]string{"tank"}); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreLeavesOutThePendingUpdate(t *testing.T) {
	e := newEnv(t)
	e.write(t, "system/update-pending.json", `{"bootId":"x"}`)
	e.write(t, ".accounts.lock", "")
	e.write(t, "system/update.json", `{"auto":false}`)
	name := e.create(t)
	stick := newStick(t, e)
	if _, err := stick.m.RestoreFound(context.Background(), "md127", name, ""); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"system/update-pending.json", ".accounts.lock"} {
		if _, err := stick.store.Read(file); err == nil {
			t.Errorf("%s came back from the backup", file)
		}
	}
	if _, err := stick.store.Read("system/update.json"); err != nil {
		t.Errorf("the update settings did not come back: %v", err)
	}
}

func TestNewerBackupIsMarked(t *testing.T) {
	e := newEnv(t)
	e.create(t) // made by 1.2.3
	stick := newStick(t, e)
	for running, newer := range map[string]bool{"1.2.2": true, "v1.1.9": true, "1.2.3": false, "1.10.0": false, "v0.23.2-2-gb36cbc4-dirty": true, "dev": false} {
		stick.m.version = running
		found := stick.m.Find(context.Background())
		if len(found) != 1 || found[0].Newer != newer {
			t.Errorf("running %s: %+v", running, found)
		}
	}
}

func TestWholeDiskBtrfsIsADevice(t *testing.T) {
	for name, ok := range map[string]bool{"sdb": true, "sdb1": true, "vdc": true, "nvme0n1": true, "nvme0n1p2": true, "md127": true, "../sdb": false, "loop0": false, "sr0": false} {
		if deviceName.MatchString(name) != ok {
			t.Errorf("%s: want %v", name, ok)
		}
	}
}

func TestKeepsByDayWeekMonth(t *testing.T) {
	e := newEnv(t)
	if err := e.m.SetSettings(Settings{Enabled: true, Volume: "tank", Keep: 14, Versions: &retention.Keep{Daily: 2}}, []string{"tank"}); err != nil {
		t.Fatal(err)
	}
	var names []string
	for i := range 4 {
		e.clock = e.clock.Add(12 * time.Hour)
		e.write(t, "system/settings.json", strings.Repeat("y", i+1))
		names = append(names, e.create(t))
	}
	// Two runs a day for two days: the newest of each day stays.
	list := e.m.List([]string{"tank"})
	if len(list) != 2 || list[0].Name != names[3] {
		t.Errorf("kept: %+v (made %v)", list, names)
	}
	if err := e.m.SetSettings(Settings{Enabled: true, Keep: 3, Versions: &retention.Keep{Mode: "rolling"}}, nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("rolling accepted: %v", err)
	}
}
