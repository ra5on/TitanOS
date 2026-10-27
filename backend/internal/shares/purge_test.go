package shares

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// purgeRunner answers smbstatus and btrfs like the real tools would.
type purgeRunner struct {
	mu        sync.Mutex
	commands  []string
	sessions  string
	snapshots string
}

func (r *purgeRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	command := name + " " + strings.Join(args, " ")
	r.commands = append(r.commands, command)
	switch {
	case name == "getent":
		return "", errors.New("exit status 2")
	case name == "smbstatus":
		if r.sessions == "" {
			return `{"tcons":{}}`, nil
		}
		return r.sessions, nil
	case name == "btrfs" && args[1] == "show":
		return "Name: x\n\tSnapshot(s):\n" + r.snapshots + "\tQuota group: n/a\n", nil
	case name == "btrfs" && args[1] == "delete":
		return "", os.RemoveAll(args[len(args)-1])
	}
	return "", nil
}

func (r *purgeRunner) ran(prefix string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.ContainsFunc(r.commands, func(c string) bool { return strings.HasPrefix(c, prefix) })
}

// purgeSetup makes a volume "data" with the share Fotos holding files, a
// sibling folder and a link that leads out of the share.
func purgeSetup(t *testing.T) (*Manager, *purgeRunner, string) {
	t.Helper()
	m, _ := newTestManager(t)
	runner := &purgeRunner{}
	m.runner = runner
	m.Disabled = true
	m.purge.wait, m.purge.poll = 5*time.Second, 10*time.Millisecond
	dir := filepath.Join(m.mountRoot, "data", "Fotos")
	for _, p := range []string{"2024/Sommer", "2025", ".recycle/alex"} {
		if err := os.MkdirAll(filepath.Join(dir, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "2024/Sommer/a.jpg"), make([]byte, 1000), 0o644)
	os.WriteFile(filepath.Join(dir, "2025/b.jpg"), make([]byte, 24), 0o644)
	os.WriteFile(filepath.Join(dir, ".recycle/alex/old.txt"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(m.mountRoot, "data", "Other"), 0o755)
	outside := filepath.Join(filepath.Dir(m.mountRoot), "outside.txt")
	os.WriteFile(outside, []byte("keep"), 0o644)
	os.Symlink(outside, filepath.Join(dir, "link"))
	os.Symlink(filepath.Join(m.mountRoot, "data", "Other"), filepath.Join(dir, "dirlink"))
	if err := m.Create(Share{Name: "Fotos", Volume: "data", SMB: true}, volumes, users); err != nil {
		t.Fatal(err)
	}
	if err := m.Create(Share{Name: "Other", Volume: "data", SMB: true}, volumes, users); err != nil {
		t.Fatal(err)
	}
	return m, runner, dir
}

func mounted() []string { return []string{"data"} }

func waitPurge(t *testing.T, m *Manager) PurgeJob {
	t.Helper()
	for range 500 {
		if !m.PurgeBusy() {
			return m.PurgeJobs()[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("deletion did not end")
	return PurgeJob{}
}

func TestPurgePreviewCountsAndWarns(t *testing.T) {
	m, _, dir := purgeSetup(t)
	os.MkdirAll(filepath.Join(m.mountRoot, "data", ".titanos", "snapshots", "daily-1"), 0o700)
	p, err := m.PreviewPurge(context.Background(), "Fotos", mounted(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Three files and two links; links count as entries, never followed.
	if p.Path != dir || !p.Exists || !p.Complete || p.Files != 5 || p.Dirs != 5 || p.Bytes < 1025 || p.Subvolume || len(p.Blockers) != 0 {
		t.Fatalf("%+v", p)
	}
	if len(p.Warnings) != 1 || p.Warnings[0].Kind != UseVolumeSnapshots {
		t.Fatalf("snapshots of the volume not mentioned: %+v", p.Warnings)
	}
	old := previewEntries
	previewEntries = 3
	defer func() { previewEntries = old }()
	p, _ = m.PreviewPurge(context.Background(), "Fotos", mounted(), nil)
	if p.Complete || p.Limit != "entries" || !slices.ContainsFunc(p.Warnings, func(u Usage) bool { return u.Kind == UseLimit }) {
		t.Fatalf("cap not reported: %+v", p)
	}
	if _, err := m.PreviewPurge(context.Background(), "Nope", mounted(), nil); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestPurgeDeletesExactlyTheShareFolder(t *testing.T) {
	m, _, dir := purgeSetup(t)
	var done []PurgeJob
	var mu sync.Mutex
	job, err := m.StartPurge(context.Background(), PurgeRequest{Name: "Fotos", Path: dir, By: "alex", Mounted: mounted,
		Done: func(j PurgeJob) { mu.Lock(); done = append(done, j); mu.Unlock() }})
	if err != nil {
		t.Fatal(err)
	}
	if job.Phase != "checking" || job.Share != "Fotos" {
		t.Fatalf("%+v", job)
	}
	result := waitPurge(t, m)
	if result.Phase != "done" || result.Error != "" || !result.ShareRemoved || result.DataKept || result.Files != 5 || result.TotalFiles != 5 {
		t.Fatalf("%+v", result)
	}
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("folder still there")
	}
	if data, err := os.ReadFile(filepath.Join(filepath.Dir(m.mountRoot), "outside.txt")); err != nil || string(data) != "keep" {
		t.Fatal("a link was followed out of the share")
	}
	if _, err := os.Stat(filepath.Join(m.mountRoot, "data", "Other")); err != nil {
		t.Fatal("the folder a link pointed to was deleted")
	}
	if _, ok := m.share("Fotos"); ok {
		t.Fatal("share still configured")
	}
	if _, ok := m.share("Other"); !ok {
		t.Fatal("other share removed")
	}
	if entries, _ := os.ReadDir(filepath.Join(m.mountRoot, "data", ".titanos", purgeFeature)); len(entries) != 0 {
		t.Fatalf("staging left behind: %v", entries)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(done) != 1 || done[0].By != "alex" || done[0].Phase != "done" {
		t.Fatalf("%+v", done)
	}
}

func TestPurgeRefusesWhatIsNotSafe(t *testing.T) {
	m, runner, dir := purgeSetup(t)
	ctx := context.Background()
	start := func(req PurgeRequest) error {
		if req.Name == "" {
			req.Name = "Fotos"
		}
		if req.Path == "" {
			req.Path = dir
		}
		if req.Mounted == nil {
			req.Mounted = mounted
		}
		_, err := m.StartPurge(ctx, req)
		return err
	}
	if err := start(PurgeRequest{Path: filepath.Join(m.mountRoot, "data")}); !errors.Is(err, ErrPurgeChanged) {
		t.Fatalf("other path accepted: %v", err)
	}
	if err := start(PurgeRequest{Mounted: func() []string { return nil }}); !errors.Is(err, ErrPurgeUnsafe) {
		t.Fatalf("unmounted volume accepted: %v", err)
	}
	var blocked *BlockedError
	err := start(PurgeRequest{Check: func(context.Context, string) []Usage { return []Usage{{Kind: UseVM, Name: "win"}} }})
	if !errors.As(err, &blocked) || blocked.Usages[0].Name != "win" {
		t.Fatalf("user of the folder ignored: %v", err)
	}
	m.Disabled = false
	runner.sessions = `{"tcons":{"1":{"service":"fotos","machine":"192.168.1.20"}}}`
	if err := start(PurgeRequest{}); !errors.As(err, &blocked) || blocked.Usages[0].Kind != UseSMB || blocked.Usages[0].Name != "192.168.1.20" {
		t.Fatalf("open SMB session ignored: %v", err)
	}
	runner.sessions = "not json"
	if err := start(PurgeRequest{}); !errors.As(err, &blocked) || blocked.Usages[0].Kind != UseSMBUnknown {
		t.Fatalf("unknown sessions ignored: %v", err)
	}
	runner.sessions = ""
	m.Disabled = true
	m.mounts = func() map[string]bool { return map[string]bool{dir + "/2025": true} }
	if err := start(PurgeRequest{}); !errors.As(err, &blocked) || blocked.Usages[0].Kind != UseMount {
		t.Fatalf("nested mount ignored: %v", err)
	}
	m.mounts = mountPoints
	if _, ok := m.share("Fotos"); !ok {
		t.Fatal("a refused deletion removed the share")
	}
	if _, err := os.Stat(filepath.Join(dir, "2025/b.jpg")); err != nil {
		t.Fatal("a refused deletion touched the data")
	}
	// A folder that is a link is never deleted, nor what it points to.
	os.Rename(dir, dir+".real")
	os.Symlink(dir+".real", dir)
	if err := start(PurgeRequest{}); !errors.Is(err, ErrPurgeUnsafe) {
		t.Fatalf("linked folder accepted: %v", err)
	}
	p, _ := m.PreviewPurge(ctx, "Fotos", mounted(), nil)
	if len(p.Blockers) == 0 || p.Blockers[0].Kind != UsePath {
		t.Fatalf("%+v", p)
	}
	if _, err := os.Stat(dir + ".real/2025/b.jpg"); err != nil {
		t.Fatal(err)
	}
}

func TestPurgeCheckRunsAgainAfterTheShareIsGone(t *testing.T) {
	m, _, dir := purgeSetup(t)
	calls := 0
	var mu sync.Mutex
	_, err := m.StartPurge(context.Background(), PurgeRequest{Name: "Fotos", Path: dir, Mounted: mounted, Check: func(context.Context, string) []Usage {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls > 1 {
			return []Usage{{Kind: UseDocker, Name: "late"}}
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	result := waitPurge(t, m)
	if result.Phase != "failed" || !result.ShareRemoved || !result.DataKept || len(result.Blockers) != 1 {
		t.Fatalf("%+v", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "2025/b.jpg")); err != nil {
		t.Fatal("data deleted although something started to use it")
	}
	if err := func() error {
		_, err := m.StartPurge(context.Background(), PurgeRequest{Name: "Fotos", Path: dir, Mounted: mounted})
		return err
	}(); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestPurgeWaitsForSambaAndNFS(t *testing.T) {
	m, runner, dir := purgeSetup(t)
	m.Disabled = false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx, func() State { return State{Mounted: mounted()} })
	if _, err := m.StartPurge(context.Background(), PurgeRequest{Name: "Fotos", Path: dir, Mounted: mounted}); err != nil {
		t.Fatal(err)
	}
	result := waitPurge(t, m)
	if result.Phase != "done" {
		t.Fatalf("%+v", result)
	}
	if !runner.ran("smbcontrol smbd close-share Fotos") {
		t.Fatalf("SMB connections not closed: %v", runner.commands)
	}
	conf, _ := os.ReadFile(m.sambaConf)
	if len(conf) == 0 || strings.Contains(string(conf), "Fotos") {
		t.Fatalf("smb.conf: %s", conf)
	}
}

func TestPurgeKeepsDataWhenSambaIsNotUpdated(t *testing.T) {
	m, _, dir := purgeSetup(t)
	m.Disabled = false
	m.purge.wait = 50 * time.Millisecond
	// Nothing applies the configuration: the job must give up, data kept.
	if _, err := m.StartPurge(context.Background(), PurgeRequest{Name: "Fotos", Path: dir, Mounted: mounted}); err != nil {
		t.Fatal(err)
	}
	result := waitPurge(t, m)
	if result.Phase != "failed" || !result.ShareRemoved || !result.DataKept {
		t.Fatalf("%+v", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "2025/b.jpg")); err != nil {
		t.Fatal(err)
	}
}

func TestPurgeSubvolume(t *testing.T) {
	m, runner, dir := purgeSetup(t)
	m.purge.subvolume = func(f *os.File, _ *unix.Stat_t) (bool, error) {
		return filepath.Base(f.Name()) == "Fotos" || len(filepath.Base(f.Name())) == 16, nil
	}
	runner.snapshots = "\t\t\t.titanos/snapshots/fotos-1\n"
	p, err := m.PreviewPurge(context.Background(), "Fotos", mounted(), nil)
	if err != nil || !p.Subvolume || len(p.Blockers) != 1 || p.Blockers[0].Kind != UseSnapshots || p.Blockers[0].Path != ".titanos/snapshots/fotos-1" {
		t.Fatalf("%+v %v", p, err)
	}
	var blocked *BlockedError
	if _, err := m.StartPurge(context.Background(), PurgeRequest{Name: "Fotos", Path: dir, Mounted: mounted}); !errors.As(err, &blocked) {
		t.Fatalf("subvolume with snapshots accepted: %v", err)
	}
	runner.snapshots = ""
	if _, err := m.StartPurge(context.Background(), PurgeRequest{Name: "Fotos", Path: dir, Mounted: mounted}); err != nil {
		t.Fatal(err)
	}
	result := waitPurge(t, m)
	if result.Phase != "done" || !result.Subvolume {
		t.Fatalf("%+v", result)
	}
	staged := filepath.Join(m.mountRoot, "data", ".titanos", purgeFeature, result.ID)
	if !runner.ran("btrfs subvolume delete --commit-after " + staged) {
		t.Fatalf("not deleted as a subvolume: %v", runner.commands)
	}
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("folder still there")
	}
}

func TestPurgeResumesAfterACrash(t *testing.T) {
	m, _, _ := purgeSetup(t)
	private := filepath.Join(m.mountRoot, "data", ".titanos", purgeFeature)
	os.MkdirAll(filepath.Join(private, "0123456789abcdef", "deep"), 0o700)
	os.Chmod(filepath.Join(m.mountRoot, "data", ".titanos"), 0o700)
	os.WriteFile(filepath.Join(private, "0123456789abcdef", "deep", "f"), []byte("abc"), 0o600)
	os.WriteFile(filepath.Join(private, "0123456789abcdef.json"), []byte(`{"share":"Alt","volume":"data","by":"alex"}`), 0o600)
	// A note without its folder is what a finished deletion left.
	os.WriteFile(filepath.Join(private, "fedcba9876543210.json"), []byte(`{"share":"Gone","volume":"data"}`), 0o600)
	var got []PurgeJob
	var mu sync.Mutex
	m.OnPurgeResumed(func(j PurgeJob) { mu.Lock(); got = append(got, j); mu.Unlock() })
	m.resumePurges(mounted())
	result := waitPurge(t, m)
	if result.Phase != "done" || !result.Resumed || result.Share != "Alt" || result.Files != 1 {
		t.Fatalf("%+v", result)
	}
	m.resumePurges(mounted())
	waitPurge(t, m)
	entries, _ := os.ReadDir(private)
	if len(entries) != 0 {
		t.Fatalf("left: %v", entries)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0].By != "alex" {
		t.Fatalf("%+v", got)
	}
	if _, err := os.Stat(filepath.Join(m.mountRoot, "data", "Fotos", "2025", "b.jpg")); err != nil {
		t.Fatal("resuming touched a share")
	}
}

func TestPurgeOneAtATime(t *testing.T) {
	m, _, dir := purgeSetup(t)
	m.purge.mu.Lock()
	m.purge.running = &PurgeJob{ID: "x"}
	m.purge.mu.Unlock()
	if _, err := m.StartPurge(context.Background(), PurgeRequest{Name: "Fotos", Path: dir, Mounted: mounted}); !errors.Is(err, ErrPurgeRunning) {
		t.Fatal(err)
	}
	p, _ := m.PreviewPurge(context.Background(), "Fotos", mounted(), nil)
	if p.Blockers[0].Kind != UseRunning {
		t.Fatalf("%+v", p)
	}
	if !m.PurgeBusy() {
		t.Fatal("not busy")
	}
}

func TestParsers(t *testing.T) {
	out := "Name: Fotos\n\tUUID: x\n\tSnapshot(s):\n\t\t\t\t.titanos/snapshots/a\n\t\t\t\tbackup/b\n\tQuota group: 0/257\n"
	if got := parseSnapshots(out); len(got) != 2 || got[1].Path != "backup/b" {
		t.Fatalf("%+v", got)
	}
	if got := parseSnapshots("Name: x\n\tSnapshot(s):\n\tQuota group: n/a\n"); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
	sessions, err := parseSessions(`{"tcons":{"a":{"service":"Fotos","machine":"pc"},"b":{"service":"IPC$","machine":"pc"}}}`, "fotos")
	if err != nil || len(sessions) != 1 || sessions[0].Name != "pc" {
		t.Fatalf("%+v %v", sessions, err)
	}
	for _, c := range []struct {
		a, b string
		want bool
	}{{"/mnt/a/x", "/mnt/a/x", true}, {"/mnt/a/x/y.iso", "/mnt/a/x", true}, {"/mnt/a", "/mnt/a/x", true}, {"/mnt/a/xy", "/mnt/a/x", false}, {"/mnt/b/x", "/mnt/a/x", false}} {
		if Overlaps(c.a, c.b) != c.want {
			t.Errorf("%v", c)
		}
	}
}
