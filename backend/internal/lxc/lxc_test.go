package lxc

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

	"github.com/ra5on/TitanOS/backend/internal/config"
)

const testIndex = `debian;bookworm;amd64;default;20261020_05:24;/images/debian/bookworm/amd64/default/20261020_05:24/
debian;trixie;amd64;default;20261026_05:24;/images/debian/trixie/amd64/default/20261026_05:24/
debian;trixie;amd64;default;20261027_05:24;/images/debian/trixie/amd64/default/20261027_05:24/
debian;trixie;amd64;cloud;20261027_05:24;/images/debian/trixie/amd64/cloud/20261027_05:24/
debian;trixie;arm64;default;20261027_05:24;/images/debian/trixie/arm64/default/20261027_05:24/
alpine;3.22;amd64;default;20261027_13:00;/images/alpine/3.22/amd64/default/20261027_13:00/
bad name;1;amd64;default;1;/x/
`

// fakeRunner stands in for lxc-create, systemctl, lxc-stop and btrfs. It
// plays the part of the system: a started container gets a populated cgroup.
type fakeRunner struct {
	mu       sync.Mutex
	commands []string
	fail     string
	cgroups  string
	block    chan struct{}
}

func (r *fakeRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	line := name + " " + strings.Join(args, " ")
	r.mu.Lock()
	r.commands = append(r.commands, line)
	r.mu.Unlock()
	if r.fail != "" && strings.Contains(line, r.fail) {
		return "", errors.New("failed: " + line)
	}
	switch {
	case name == "env" && slices.Contains(args, "lxc-create"):
		if r.block != nil {
			<-r.block
		}
		path, container := args[slices.Index(args, "-P")+1], args[slices.Index(args, "-n")+1]
		return "", os.MkdirAll(filepath.Join(path, container, "rootfs", "etc"), 0o755)
	case name == "systemctl" && args[0] == "start":
		container := strings.TrimSuffix(strings.TrimPrefix(args[1], "titanos-lxc@"), ".service")
		dir := filepath.Join(r.cgroups, "lxc.payload."+container)
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "cpu.stat"), []byte("usage_usec 1000000\nuser_usec 1\n"), 0o644)
		os.WriteFile(filepath.Join(dir, "memory.current"), []byte("52428800\n"), 0o644)
		return "", os.WriteFile(filepath.Join(dir, "cgroup.events"), []byte("populated 1\nfrozen 0\n"), 0o644)
	case name == "systemctl" && args[0] == "stop":
		container := strings.TrimSuffix(strings.TrimPrefix(args[1], "titanos-lxc@"), ".service")
		return "", os.RemoveAll(filepath.Join(r.cgroups, "lxc.payload."+container))
	case name == "btrfs" && args[1] == "snapshot":
		return "", os.MkdirAll(args[len(args)-1], 0o755)
	case name == "btrfs" && args[1] == "delete":
		return "", os.RemoveAll(args[2])
	}
	return "", nil
}

func (r *fakeRunner) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.commands...)
}

func (r *fakeRunner) reset() {
	r.mu.Lock()
	r.commands = nil
	r.mu.Unlock()
}

type testEnv struct {
	m      *Manager
	runner *fakeRunner
	root   string
	btrfs  bool
}

func newTestManager(t *testing.T) *testEnv {
	t.Helper()
	root := t.TempDir()
	env := &testEnv{root: root}
	m := NewManager(config.Open(filepath.Join(root, "config")), 100)
	m.mountRoot = filepath.Join(root, "mnt")
	m.cgroupRoot = filepath.Join(root, "cgroup")
	m.envDir = filepath.Join(root, "run", "titanos-lxc")
	m.templateDir = filepath.Join(root, "lxc-config")
	m.apparmorParser = filepath.Join(root, "no-apparmor")
	m.lxcStart = filepath.Join(root, "lxc-start")
	m.mountInfo = filepath.Join(root, "mountinfo")
	m.autostartFile = filepath.Join(root, "run", "lxc-autostarted")
	m.isBtrfs = func(string) bool { return env.btrfs }
	m.fetchIndex = func(context.Context) ([]byte, error) { return []byte(testIndex), nil }
	env.runner = &fakeRunner{cgroups: m.cgroupRoot}
	m.runner = env.runner
	for _, dir := range []string{filepath.Join(m.mountRoot, "data", "media"), filepath.Join(m.mountRoot, "data", ".titanos"), filepath.Join(m.mountRoot, "fast"), m.templateDir, m.cgroupRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{m.lxcStart, filepath.Join(m.templateDir, "common.conf"), filepath.Join(m.templateDir, "userns.conf"), filepath.Join(m.templateDir, "debian.common.conf"), m.mountInfo} {
		if err := os.WriteFile(file, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	env.m = m
	return env
}

var mounted = []string{"data", "fast"}

// create makes a container and waits for the background job.
func (env *testEnv) create(t *testing.T, c Container) {
	t.Helper()
	if err := env.m.Create(context.Background(), c, mounted); err != nil {
		t.Fatal(err)
	}
	env.waitJob(t, c.Name)
}

func (env *testEnv) waitJob(t *testing.T, name string) *Job {
	t.Helper()
	for range 500 {
		for _, s := range env.m.List(mounted) {
			if s.Name == name && (s.Job == nil || s.Job.State != "running") {
				return s.Job
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job did not end")
	return nil
}

func TestParseIndexKeepsNewestDefaultBuildForTheNAS(t *testing.T) {
	images := parseIndex([]byte(testIndex), "amd64")
	want := []Image{
		{Distribution: "alpine", Release: "3.22", Arch: "amd64", Build: "20261027_13:00"},
		{Distribution: "debian", Release: "bookworm", Arch: "amd64", Build: "20261020_05:24"},
		{Distribution: "debian", Release: "trixie", Arch: "amd64", Build: "20261027_05:24"},
	}
	if !slices.Equal(images, want) {
		t.Fatalf("%+v", images)
	}
}

func TestImagesAreCachedAndFailuresKeepTheLastList(t *testing.T) {
	env := newTestManager(t)
	calls := 0
	clock := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	env.m.now = func() time.Time { return clock }
	env.m.fetchIndex = func(context.Context) ([]byte, error) {
		calls++
		if calls > 1 {
			return nil, errors.New("offline")
		}
		return []byte(testIndex), nil
	}
	for range 3 {
		if images, err := env.m.Images(context.Background()); err != nil || len(images) != 3 {
			t.Fatal(images, err)
		}
	}
	clock = clock.Add(7 * time.Hour)
	images, err := env.m.Images(context.Background())
	if err == nil || len(images) != 3 || calls != 2 {
		t.Fatal(images, err, calls)
	}
	// Not retried right away.
	env.m.Images(context.Background())
	if calls != 2 {
		t.Fatal(calls)
	}
}

func TestSettingsAreValidated(t *testing.T) {
	env := newTestManager(t)
	env.m.BridgeReady = func() bool { return false }
	ctx := context.Background()
	base := Container{Name: "web", Volume: "data", Distribution: "debian", Release: "trixie", Network: "none"}
	cases := []struct {
		change func(*Container)
		want   error
	}{
		{func(c *Container) { c.Name = "Web" }, ErrInvalidName},
		{func(c *Container) { c.Name = "-web" }, ErrInvalidName},
		{func(c *Container) { c.Name = strings.Repeat("a", 32) }, ErrInvalidName},
		{func(c *Container) { c.Volume = "gone" }, ErrUnknownVolume},
		{func(c *Container) { c.Release = "sid" }, ErrUnknownImage},
		{func(c *Container) { c.Distribution = "../debian" }, ErrUnknownImage},
		{func(c *Container) { c.Arch = "arm64" }, ErrUnknownImage},
		{func(c *Container) { c.DisplayName = "a\u202eb" }, ErrInvalidDisplayName},
		{func(c *Container) { c.CPUs = -1 }, ErrInvalid},
		{func(c *Container) { c.CPUs = 100000 }, ErrInvalid},
		{func(c *Container) { c.MemoryMiB = 10 }, ErrInvalid},
		{func(c *Container) { c.Network = "host" }, ErrInvalid},
		{func(c *Container) { c.MAC = "01:16:3e:00:00:01" }, ErrInvalid},
		{func(c *Container) { c.MAC = "00:16:3e:00:00" }, ErrInvalid},
		{func(c *Container) { c.Network = "bridge" }, ErrNoBridge},
		{func(c *Container) { c.Network = "network:lan" }, ErrNoBridge},
		{func(c *Container) { c.Mounts = []Mount{{Path: "/etc", Target: "/mnt/etc"}} }, ErrBadFolder},
		{func(c *Container) {
			c.Mounts = []Mount{{Path: filepath.Join(env.m.mountRoot, "data"), Target: "/mnt/all"}}
		}, ErrBadFolder},
		{func(c *Container) {
			c.Mounts = []Mount{{Path: filepath.Join(env.m.mountRoot, "data", ".titanos"), Target: "/mnt/x"}}
		}, ErrBadFolder},
		{func(c *Container) {
			c.Mounts = []Mount{{Path: env.m.mountRoot + "/data/media/../media", Target: "/mnt/x"}}
		}, ErrBadFolder},
		{func(c *Container) {
			c.Mounts = []Mount{{Path: filepath.Join(env.m.mountRoot, "data", "media"), Target: "/proc/x"}}
		}, ErrInvalid},
		{func(c *Container) {
			c.Mounts = []Mount{{Path: filepath.Join(env.m.mountRoot, "data", "media"), Target: "/etc"}}
		}, ErrInvalid},
		{func(c *Container) {
			c.Mounts = []Mount{{Path: filepath.Join(env.m.mountRoot, "data", "media"), Target: "mnt/x"}}
		}, ErrInvalid},
		{func(c *Container) {
			c.Mounts = []Mount{{Path: filepath.Join(env.m.mountRoot, "data", "media"), Target: "/mnt/a b"}}
		}, ErrInvalid},
		{func(c *Container) {
			media := filepath.Join(env.m.mountRoot, "data", "media")
			c.Mounts = []Mount{{Path: media, Target: "/mnt/x"}, {Path: media, Target: "/mnt/x"}}
		}, ErrInvalid},
	}
	for i, tc := range cases {
		c := base
		tc.change(&c)
		if err := env.m.Create(ctx, c, mounted); !errors.Is(err, tc.want) {
			t.Fatalf("case %d: %v, want %v", i, err, tc.want)
		}
	}
	// A link out of the volumes counts as what it points at.
	link := filepath.Join(env.m.mountRoot, "data", "media", "out")
	os.Symlink("/etc", link)
	c := base
	c.Mounts = []Mount{{Path: link, Target: "/mnt/x"}}
	if err := env.m.Create(ctx, c, mounted); !errors.Is(err, ErrBadFolder) {
		t.Fatal(err)
	}
	if len(env.runner.lines()) != 0 || len(env.m.List(mounted)) != 0 {
		t.Fatal("rejected settings ran something", env.runner.lines())
	}

	env.m.Disabled = true
	if err := env.m.Create(ctx, base, mounted); !errors.Is(err, ErrDisabled) {
		t.Fatal(err)
	}
	env.m.Disabled = false
	os.Remove(env.m.lxcStart)
	if err := env.m.Create(ctx, base, mounted); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}

func TestGeneratedConfiguration(t *testing.T) {
	env := newTestManager(t)
	m := env.m
	m.BridgeReady = func() bool { return true }
	m.NetworkBridge = func(id string) (string, bool) { return "br-" + id, id == "lan" }
	media := filepath.Join(m.mountRoot, "data", "media")
	c := Container{Name: "web", Volume: "data", Distribution: "debian", Release: "trixie", Arch: "amd64", CPUs: 2, MemoryMiB: 512,
		Network: "bridge", MAC: "00:16:3e:aa:bb:cc", Mounts: []Mount{{Path: media, Target: "/srv/media", ReadOnly: true}, {Path: media, Target: "/mnt/rw"}}}
	text := m.Config(c, false)
	for _, want := range []string{
		"lxc.include = " + filepath.Join(m.templateDir, "debian.common.conf"),
		// No debian.userns.conf in the test: the generic one.
		"lxc.include = " + filepath.Join(m.templateDir, "userns.conf"),
		"lxc.idmap = u 0 100000 65536",
		"lxc.idmap = g 0 100000 100",
		"lxc.idmap = g 100 100 1",
		"lxc.idmap = g 101 100101 65435",
		"lxc.arch = linux64",
		"lxc.apparmor.profile = unconfined",
		"lxc.uts.name = web",
		"lxc.rootfs.path = dir:" + filepath.Join(m.mountRoot, "data", "lxc", "web", "rootfs"),
		"lxc.cgroup2.memory.max = 536870912",
		"lxc.cgroup2.cpu.max = 200000 100000",
		"lxc.net.0.type = veth",
		"lxc.net.0.link = br0",
		"lxc.net.0.hwaddr = 00:16:3e:aa:bb:cc",
		"lxc.mount.entry = " + media + " srv/media none bind,create=dir,ro 0 0",
		"lxc.mount.entry = " + media + " mnt/rw none bind,create=dir 0 0",
	} {
		if !strings.Contains(text, want+"\n") {
			t.Fatalf("missing %q in\n%s", want, text)
		}
	}
	// Every mapped range has the same size on both sides.
	c.Network = "network:lan"
	if text := m.Config(c, false); !strings.Contains(text, "lxc.net.0.link = br-lan\n") {
		t.Fatal(text)
	}
	c.Network, c.CPUs, c.MemoryMiB, c.Mounts, c.Privileged, c.Distribution = "none", 0, 0, nil, true, "alpine"
	os.WriteFile(m.apparmorParser, nil, 0o755)
	text = m.Config(c, false)
	for _, unwanted := range []string{"lxc.idmap", "userns", "lxc.cgroup2", "lxc.mount.entry", "veth"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("privileged container without limits has %q:\n%s", unwanted, text)
		}
	}
	for _, want := range []string{"lxc.include = " + filepath.Join(m.templateDir, "common.conf"), "lxc.net.0.type = empty", "lxc.apparmor.profile = generated"} {
		if !strings.Contains(text, want+"\n") {
			t.Fatalf("missing %q in\n%s", want, text)
		}
	}
	// For lxc-create: only what the template needs.
	text = m.Config(Container{Name: "web", Volume: "data", Distribution: "debian", Arch: "amd64"}, true)
	if strings.Contains(text, "rootfs") || strings.Contains(text, "lxc.net") || !strings.Contains(text, "lxc.idmap") {
		t.Fatal(text)
	}
}

func TestCreateStartStopDelete(t *testing.T) {
	env := newTestManager(t)
	m, ctx := env.m, context.Background()
	media := filepath.Join(m.mountRoot, "data", "media")
	block := make(chan struct{})
	env.runner.block = block
	if err := m.Create(ctx, Container{Name: "web", DisplayName: "Web server", Volume: "data", Distribution: "debian", Release: "trixie", Network: "none",
		Mounts: []Mount{{Path: media, Target: "/srv/media"}}}, mounted); err != nil {
		t.Fatal(err)
	}
	// While the image downloads: listed with its job, busy, nothing else may happen.
	for range 200 {
		if l := m.List(mounted); len(l) == 1 && l[0].Job != nil && l[0].Job.Phase == "download" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	list := m.List(mounted)
	if len(list) != 1 || list[0].Job == nil || list[0].Job.State != "running" || list[0].Job.Phase != "download" || !m.Busy() {
		t.Fatalf("%+v", list)
	}
	if err := m.Start(ctx, "web", mounted); !errors.Is(err, ErrBusy) && !errors.Is(err, ErrCreating) {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, "web", false, ""); !errors.Is(err, ErrCreating) {
		t.Fatal(err)
	}
	if err := m.Create(ctx, Container{Name: "web", Volume: "fast", Distribution: "debian", Release: "trixie"}, mounted); !errors.Is(err, ErrBusy) && !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	close(block)
	if job := env.waitJob(t, "web"); job != nil {
		t.Fatalf("%+v", job)
	}
	if m.Busy() {
		t.Fatal("busy after creation")
	}
	path := filepath.Join(m.mountRoot, "data", "lxc")
	commands := env.runner.lines()
	want := "env LXC_CACHE_PATH=" + path + "/.cache TMPDIR=" + path + "/.tmp lxc-create -P " + path + " -n web -B dir -f " + path + "/.tmp/web.conf -t download -- --dist debian --release trixie --arch amd64"
	if len(commands) != 1 || commands[0] != want {
		t.Fatalf("%q", commands)
	}
	if _, err := os.Stat(path + "/.tmp/web.conf"); err == nil {
		t.Fatal("the first configuration stays behind")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o711 {
		t.Fatal(info, err)
	}
	// Stored in /config, with a MAC address of LXC's range.
	var stored Container
	if err := m.store.ReadJSON("lxc/web.json", &stored); err != nil || stored.DisplayName != "Web server" || !strings.HasPrefix(stored.MAC, "00:16:3e:") || stored.Arch != "amd64" || stored.Btrfs {
		t.Fatalf("%+v %v", stored, err)
	}
	if text, _ := os.ReadFile(filepath.Join(path, "web", "config")); !strings.Contains(string(text), "lxc.mount.entry = "+media) {
		t.Fatalf("config after creation: %s", text)
	}

	// Starting: configuration, environment of the unit, then the unit.
	env.runner.reset()
	if err := m.Start(ctx, "web", []string{"fast"}); !errors.Is(err, ErrUnknownVolume) {
		t.Fatal(err)
	}
	if err := m.Start(ctx, "web", mounted); err != nil {
		t.Fatal(err)
	}
	if got := env.runner.lines(); !slices.Equal(got, []string{"systemctl start titanos-lxc@web.service"}) {
		t.Fatalf("%q", got)
	}
	if data, _ := os.ReadFile(filepath.Join(m.envDir, "web.env")); string(data) != "LXC_PATH="+path+"\n" {
		t.Fatalf("%q", data)
	}
	if err := m.Start(ctx, "web", mounted); !errors.Is(err, ErrRunning) {
		t.Fatal(err)
	}
	// CPU and RAM come from the cgroup.
	m.sample()
	dir := filepath.Join(m.cgroupRoot, "lxc.payload.web")
	os.WriteFile(filepath.Join(dir, "cpu.stat"), []byte("usage_usec 3000000\n"), 0o644)
	m.now = func() time.Time { return time.Now().Add(10 * time.Second) }
	m.sample()
	list = m.List(mounted)
	if list[0].State != "running" || list[0].MemoryBytes != 50<<20 || list[0].CPUPercent <= 0 {
		t.Fatalf("%+v", list[0])
	}
	if names := m.RunningNames(); !names["web"] {
		t.Fatal(names)
	}
	command, err := m.ConsoleCommand("web", "shell")
	if err != nil || command[0] != "lxc-attach" || !slices.Contains(command, "--clear-env") || command[slices.Index(command, "-P")+1] != path {
		t.Fatal(command, err)
	}
	if command, _ := m.ConsoleCommand("web", "console"); !slices.Equal(command, []string{"lxc-console", "-P", path, "-n", "web", "-t", "0"}) {
		t.Fatal(command)
	}
	if err := m.Delete(ctx, "web", true, "web"); !errors.Is(err, ErrRunning) {
		t.Fatal(err)
	}

	env.runner.reset()
	if err := m.Action(ctx, "web", "restart", mounted); err != nil {
		t.Fatal(err)
	}
	if err := m.Action(ctx, "web", "kill", mounted); err != nil {
		t.Fatal(err)
	}
	want2 := []string{"systemctl stop titanos-lxc@web.service", "systemctl start titanos-lxc@web.service", "lxc-stop -P " + path + " -n web -k", "systemctl stop titanos-lxc@web.service"}
	if got := env.runner.lines(); !slices.Equal(got, want2) {
		t.Fatalf("%q", got)
	}
	if err := m.Action(ctx, "web", "stop", mounted); !errors.Is(err, ErrNotRunning) {
		t.Fatal(err)
	}
	if _, err := m.ConsoleCommand("web", ""); !errors.Is(err, ErrNotRunning) {
		t.Fatal(err)
	}
	if err := m.Action(ctx, "web", "explode", mounted); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}

	// Changing settings; a link turns into the real folder.
	name := "Web"
	if err := m.Update("web", Change{DisplayName: &name, CPUs: 1, MemoryMiB: 256, Network: "none", MAC: "00:16:3E:00:00:02", Autostart: true}); err != nil {
		t.Fatal(err)
	}
	stored = Container{}
	if err := m.store.ReadJSON("lxc/web.json", &stored); err != nil || stored.MAC != "00:16:3e:00:00:02" || stored.Mounts != nil || !stored.Autostart || stored.Distribution != "debian" {
		t.Fatalf("%+v", stored)
	}

	// Deleting the data needs the name, and nothing may be mounted below.
	if err := m.Delete(ctx, "web", true, ""); !errors.Is(err, ErrNotConfirmed) {
		t.Fatal(err)
	}
	os.WriteFile(m.mountInfo, []byte("36 35 98:0 / "+filepath.Join(path, "web", "rootfs", "srv")+" rw - ext4 /dev/x rw\n"), 0o644)
	if err := m.Delete(ctx, "web", true, "Web"); !errors.Is(err, ErrMounted) {
		t.Fatal(err)
	}
	os.WriteFile(m.mountInfo, nil, 0o644)
	if err := m.Delete(ctx, "web", true, "Web"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path, "web")); !os.IsNotExist(err) {
		t.Fatal("data stayed", err)
	}
	if _, err := os.Stat(media); err != nil {
		t.Fatal("the shared folder went with it", err)
	}
	if len(m.List(mounted)) != 0 {
		t.Fatal("still listed")
	}
	if _, err := m.store.Read("lxc/web.json"); err == nil {
		t.Fatal("still in /config")
	}
}

func TestDeleteKeepsDataAndAFailedCreationCanBeRemoved(t *testing.T) {
	env := newTestManager(t)
	m, ctx := env.m, context.Background()
	env.runner.fail = "lxc-create"
	if err := m.Create(ctx, Container{Name: "box", Volume: "data", Distribution: "alpine", Release: "3.22"}, mounted); err != nil {
		t.Fatal(err)
	}
	job := env.waitJob(t, "box")
	if job == nil || job.State != "failed" || !strings.Contains(job.Text, "lxc-create") || m.Busy() {
		t.Fatalf("%+v", job)
	}
	if err := m.Start(ctx, "box", mounted); err == nil {
		t.Fatal("a failed container started")
	}
	os.MkdirAll(filepath.Join(m.mountRoot, "data", "lxc", "box", "rootfs"), 0o755)
	if err := m.Delete(ctx, "box", false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.mountRoot, "data", "lxc", "box", "rootfs")); err != nil {
		t.Fatal("data deleted without being asked", err)
	}
	// The folder left behind is never written over by a new container.
	env.runner.fail = ""
	if err := m.Create(ctx, Container{Name: "box", Volume: "data", Distribution: "alpine", Release: "3.22"}, mounted); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
}

func TestAutostartOncePerBootWhenTheNetworkIsThere(t *testing.T) {
	env := newTestManager(t)
	m, ctx := env.m, context.Background()
	ready := false
	m.BridgeReady = func() bool { return ready }
	for _, c := range []Container{
		{Name: "a", Volume: "data", Network: "bridge", MAC: "00:16:3e:00:00:0a", Autostart: true},
		{Name: "b", Volume: "fast", Network: "none", MAC: "00:16:3e:00:00:0b", Autostart: true},
		{Name: "c", Volume: "data", Network: "none", MAC: "00:16:3e:00:00:0c"},
	} {
		os.MkdirAll(filepath.Join(m.mountRoot, c.Volume, "lxc", c.Name, "rootfs"), 0o755)
		m.store.WriteJSON("lxc/"+c.Name+".json", c)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	m.autostart(ctx, []string{"data"})
	if got := env.runner.lines(); !slices.Equal(got, nil) {
		t.Fatalf("started without its volume or network: %q", got)
	}
	ready = true
	m.autostart(ctx, mounted)
	m.autostart(ctx, mounted)
	if got := env.runner.lines(); !slices.Equal(got, []string{"systemctl start titanos-lxc@a.service", "systemctl start titanos-lxc@b.service"}) {
		t.Fatalf("%q", got)
	}
	// Someone stops it: a restart of titand does not start it again.
	m.Action(ctx, "a", "stop", mounted)
	env.runner.reset()
	again := NewManager(m.store, 100)
	again.runner, again.autostartFile, again.mountRoot, again.cgroupRoot, again.envDir, again.BridgeReady = m.runner, m.autostartFile, m.mountRoot, m.cgroupRoot, m.envDir, m.BridgeReady
	again.Load()
	again.autostart(ctx, mounted)
	if got := env.runner.lines(); len(got) != 0 {
		t.Fatalf("%q", got)
	}
	if refs := m.NetworkReferences(); !slices.Equal(refs, []string{"bridge"}) {
		t.Fatal(refs)
	}
}

func TestSnapshotsOnBtrfs(t *testing.T) {
	env := newTestManager(t)
	m, ctx := env.m, context.Background()
	env.create(t, Container{Name: "plain", Volume: "fast", Distribution: "debian", Release: "trixie"})
	if _, err := m.CreateSnapshot(ctx, "plain"); !errors.Is(err, ErrNoSnapshots) {
		t.Fatal(err)
	}
	env.btrfs = true
	env.create(t, Container{Name: "web", Volume: "data", Distribution: "debian", Release: "trixie"})
	if got := env.runner.lines(); !strings.Contains(got[len(got)-1], " -B btrfs ") {
		t.Fatalf("%q", got)
	}
	env.runner.reset()
	m.now = func() time.Time { return time.Date(2026, 10, 27, 14, 30, 0, 0, time.UTC) }
	snapshot, err := m.CreateSnapshot(ctx, "web")
	if err != nil || snapshot.ID != "20261027-143000" {
		t.Fatal(snapshot, err)
	}
	dir := filepath.Join(m.mountRoot, "data", "lxc", "web")
	if got := env.runner.lines(); !slices.Equal(got, []string{"btrfs subvolume snapshot -r " + dir + "/rootfs " + dir + "/snapshots/20261027-143000"}) {
		t.Fatalf("%q", got)
	}
	if list, err := m.Snapshots("web"); err != nil || len(list) != 1 || list[0].ID != snapshot.ID || !list[0].Created.Equal(m.now()) {
		t.Fatal(list, err)
	}
	if _, err := m.CreateSnapshot(ctx, "web"); !errors.Is(err, ErrBusy) {
		t.Fatal("a second snapshot in the same second", err)
	}

	// Restoring only while stopped, from a copy, the current state goes last.
	m.Start(ctx, "web", mounted)
	if err := m.RestoreSnapshot(ctx, "web", snapshot.ID); !errors.Is(err, ErrRunning) {
		t.Fatal(err)
	}
	m.Action(ctx, "web", "stop", mounted)
	if err := m.RestoreSnapshot(ctx, "web", "../../etc"); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "rootfs", "marker"), nil, 0o644)
	env.runner.reset()
	if err := m.RestoreSnapshot(ctx, "web", snapshot.ID); err != nil {
		t.Fatal(err)
	}
	want := []string{"btrfs subvolume snapshot " + dir + "/snapshots/" + snapshot.ID + " " + dir + "/rootfs.restore", "btrfs subvolume delete " + dir + "/rootfs.old"}
	if got := env.runner.lines(); !slices.Equal(got, want) {
		t.Fatalf("%q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "rootfs", "marker")); !os.IsNotExist(err) {
		t.Fatal("old state is still there")
	}
	if m.Busy() {
		t.Fatal("busy after the restore")
	}
	if err := m.DeleteSnapshot(ctx, "web", snapshot.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := m.Snapshots("web"); len(list) != 0 {
		t.Fatal(list)
	}
	// Removing the data removes the subvolumes with btrfs.
	m.CreateSnapshot(ctx, "web")
	env.runner.reset()
	if err := m.Delete(ctx, "web", true, "web"); err != nil {
		t.Fatal(err)
	}
	if got := env.runner.lines(); len(got) != 2 || !strings.HasPrefix(got[0], "btrfs subvolume delete "+dir+"/snapshots/") || got[1] != "btrfs subvolume delete "+dir+"/rootfs" {
		t.Fatalf("%q", got)
	}
}

func TestDevelopmentModeChangesNothing(t *testing.T) {
	env := newTestManager(t)
	m, ctx := env.m, context.Background()
	m.store.WriteJSON("lxc/web.json", Container{Name: "web", Volume: "data", Network: "none", MAC: "00:16:3e:00:00:01", Autostart: true})
	m.Load()
	m.Disabled = true
	m.autostart(ctx, mounted)
	for _, err := range []error{
		m.Start(ctx, "web", mounted),
		m.Action(ctx, "web", "stop", mounted),
		m.Delete(ctx, "web", true, "web"),
	} {
		if !errors.Is(err, ErrDisabled) {
			t.Fatal(err)
		}
	}
	if _, err := m.CreateSnapshot(ctx, "web"); !errors.Is(err, ErrDisabled) {
		t.Fatal(err)
	}
	if len(env.runner.lines()) != 0 || len(m.List(mounted)) != 1 {
		t.Fatal(env.runner.lines())
	}
}
