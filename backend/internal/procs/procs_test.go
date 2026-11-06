package procs

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// fixture copies testdata (a /proc tree and a passwd) into a temporary
// directory, so a test may change it. Nothing reads the host's /proc.
func fixture(t *testing.T) (*Sampler, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("testdata")); err != nil {
		t.Fatal(err)
	}
	s := &Sampler{Root: filepath.Join(dir, "proc"), Passwd: filepath.Join(dir, "etc", "passwd"), Self: 100, Parent: 1, PageSize: 4096,
		// Fixture pids may be real processes on the host: never open or
		// signal them.
		OpenPID:  func(int) (int, error) { t.Fatal("unexpected pidfd"); return -1, nil },
		SendPID:  func(int, syscall.Signal) error { t.Fatal("unexpected signal"); return nil },
		ClosePID: func(int) error { return nil }}
	return s, dir
}

func find(l List, pid int) (Process, bool) {
	for _, p := range l.Processes {
		if p.PID == pid {
			return p, true
		}
	}
	return Process{}, false
}

func TestListParsesFixture(t *testing.T) {
	s, _ := fixture(t)
	l, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if l.CPUs != 2 || l.MemoryTotalBytes != 16384000*1024 {
		t.Fatalf("cpus %d mem %d", l.CPUs, l.MemoryTotalBytes)
	}
	var pids []int
	for _, p := range l.Processes {
		pids = append(pids, p.PID)
	}
	// Kernel threads (2, 15) and the thread 101 of titand have no row.
	if want := []int{1, 100, 200, 300, 400, 500, 600}; !equal(pids, want) {
		t.Fatalf("pids %v, want %v", pids, want)
	}
	p, _ := find(l, 200)
	if p.Name != "tricky) (name" || p.Command != "/usr/bin/immich --port 2283" || p.User != "root" || p.MemoryBytes != 5000*4096 || p.Start != 900 {
		t.Fatalf("docker process %+v", p)
	}
	if p.Owner != (Owner{Kind: "docker", ID: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}) {
		t.Fatalf("owner %+v", p.Owner)
	}
	if q, _ := find(l, 300); q.Owner != (Owner{Kind: "vm", ID: "3", Name: "win-11"}) || q.User != "nobody" {
		t.Fatalf("vm %+v", q)
	}
	if q, _ := find(l, 400); q.Owner != (Owner{Kind: "lxc", Name: "pihole"}) {
		t.Fatalf("lxc %+v", q)
	}
	if q, _ := find(l, 500); q.Owner != (Owner{Kind: "service", Name: "smbd"}) {
		t.Fatalf("service %+v", q)
	}
	if q, _ := find(l, 600); q.User != "alex" || q.Owner.Kind != "" || q.Protected {
		t.Fatalf("user process %+v", q)
	}
	for _, pid := range []int{1, 100} {
		if q, _ := find(l, pid); !q.Protected {
			t.Fatalf("%d not protected", pid)
		}
	}
}

func TestCPUPercentAgainstPreviousRead(t *testing.T) {
	s, dir := fixture(t)
	if _, err := s.List(); err != nil {
		t.Fatal(err)
	}
	// 200 jiffies passed on the machine; process 200 used 50 of them.
	write(t, filepath.Join(dir, "proc", "stat"), "cpu  1100 0 600 8000 100 0 0 0 0 0\ncpu0 1 1 1 1\ncpu1 1 1 1 1\n")
	stat := readFile(filepath.Join(dir, "proc", "200", "stat"))
	write(t, filepath.Join(dir, "proc", "200", "stat"), strings.Replace(stat, " 100 20 0 0 ", " 140 30 0 0 ", 1))
	l, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := find(l, 200); p.CPUPercent != 25 {
		t.Fatalf("cpu %v", p.CPUPercent)
	}
	if p, _ := find(l, 500); p.CPUPercent != 0 {
		t.Fatalf("idle process cpu %v", p.CPUPercent)
	}
}

func TestSignalRefusals(t *testing.T) {
	s, _ := fixture(t)
	cases := []struct {
		pid   int
		start uint64
		sig   string
		want  error
	}{
		{1, 1, "term", ErrProtected},     // init
		{2, 2, "kill", ErrProtected},     // kthreadd
		{15, 20, "kill", ErrProtected},   // kernel thread
		{100, 500, "kill", ErrProtected}, // titand itself
		{101, 501, "kill", ErrNotFound},  // a thread id of titand
		{999, 1, "term", ErrNotFound},
		{0, 0, "term", ErrNotFound},
		{-5, 0, "term", ErrNotFound},
		{600, 1, "term", ErrChanged}, // pid reused since the list
		{600, 1300, "hup", ErrSignal},
	}
	for _, c := range cases {
		if _, err := s.Signal(c.pid, c.start, c.sig); !errors.Is(err, c.want) {
			t.Errorf("pid %d: %v, want %v", c.pid, err, c.want)
		}
	}
	// The parent of titand is protected too.
	s.Parent = 500
	if _, err := s.Signal(500, 1200, "term"); !errors.Is(err, ErrProtected) {
		t.Fatalf("parent: %v", err)
	}
	s.Disabled = true
	if _, err := s.Signal(600, 1300, "term"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("dev mode: %v", err)
	}
	s.Disabled = false
	var got []any
	closed := 0
	s.OpenPID = func(pid int) (int, error) { got = append(got, pid); return 42, nil }
	s.SendPID = func(fd int, sig syscall.Signal) error { got = append(got, fd, sig); return nil }
	s.ClosePID = func(fd int) error { closed++; return nil }
	if name, err := s.Signal(600, 1300, "kill"); err != nil || name != "bash" {
		t.Fatalf("kill: %q %v", name, err)
	}
	if len(got) != 3 || got[0] != 600 || got[1] != 42 || got[2] != syscall.SIGKILL || closed != 1 {
		t.Fatalf("sent %v, closed %d", got, closed)
	}
	s.SendPID = func(int, syscall.Signal) error { return syscall.ESRCH }
	if _, err := s.Signal(600, 1300, "term"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("gone: %v", err)
	}
	s.OpenPID = func(int) (int, error) { return -1, syscall.ESRCH }
	if _, err := s.Signal(600, 1300, "term"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("gone before open: %v", err)
	}
}

func TestSignalPIDReusedWhileOpening(t *testing.T) {
	s, dir := fixture(t)
	stat := filepath.Join(dir, "proc", "600", "stat")
	// Between the first look and the pidfd, bash ended and its pid went to
	// another process: the pidfd holds the new one, which gets nothing.
	s.OpenPID = func(int) (int, error) {
		write(t, stat, strings.Replace(readFile(stat), " 1300 ", " 1400 ", 1))
		return 42, nil
	}
	s.SendPID = func(int, syscall.Signal) error { t.Fatal("signal to a reused pid"); return nil }
	if _, err := s.Signal(600, 1300, "kill"); !errors.Is(err, ErrChanged) {
		t.Fatalf("reused pid: %v", err)
	}
}

func TestProtectedServicesAndChildrenOfTitand(t *testing.T) {
	s, dir := fixture(t)
	stat := filepath.Join(dir, "proc", "600", "stat")
	orig := readFile(stat)
	for _, name := range []string{"dockerd", "containerd", "containerd-shim", "sshd", "sshd-session", "systemd-journal", "systemd-udevd", "smbd", "nmbd", "mdadm", "libvirtd", "virtqemud"} {
		write(t, stat, strings.Replace(orig, "(bash)", "("+name+")", 1))
		if _, err := s.Signal(600, 1300, "kill"); !errors.Is(err, ErrProtected) {
			t.Errorf("%s: %v", name, err)
		}
		l, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		if p, _ := find(l, 600); !p.Protected {
			t.Errorf("%s not marked protected", name)
		}
	}
	for _, name := range []string{"dockerd-helper", "bash", "containerd2", "sshdx"} {
		if protectedName(name) {
			t.Errorf("%s protected", name)
		}
	}
	// A shell titand started, and what that shell started, stay too.
	write(t, stat, strings.Replace(orig, " S 500 ", " S 100 ", 1))
	if _, err := s.Signal(600, 1300, "kill"); !errors.Is(err, ErrProtected) {
		t.Errorf("child of titand: %v", err)
	}
	write(t, filepath.Join(dir, "proc", "400", "stat"), strings.Replace(readFile(filepath.Join(dir, "proc", "400", "stat")), " S 1 ", " S 600 ", 1))
	l, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := find(l, 400); !p.Protected {
		t.Error("grandchild of titand not marked protected")
	}
}

// TestSignalThroughPidfd ends a real child process of the test through a
// pidfd; nothing else on the host is touched.
func TestSignalThroughPidfd(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Skip("no sleep:", err)
	}
	defer cmd.Process.Kill()
	s := &Sampler{Root: "/proc", Passwd: "/etc/passwd", Self: -1, Parent: -1}
	p, err := s.readOne(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Signal(cmd.Process.Pid, p.start+1, "term"); !errors.Is(err, ErrChanged) {
		t.Fatalf("wrong start time: %v", err)
	}
	if _, err := s.Signal(cmd.Process.Pid, p.start, "term"); err != nil {
		if errors.Is(err, syscall.ENOSYS) || errors.Is(err, syscall.EPERM) {
			t.Skip("pidfd not available:", err)
		}
		t.Fatal(err)
	}
	err = cmd.Wait()
	if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != syscall.SIGTERM {
		t.Fatalf("child not ended by SIGTERM: %v", err)
	}
}

func TestOwnerOf(t *testing.T) {
	id := strings.Repeat("ab", 32)
	cases := map[string]Owner{
		"/system.slice/docker-" + id + ".scope": {Kind: "docker", ID: id},
		"/docker/" + id + "/sub":                {Kind: "docker", ID: id},
		`/machine.slice/machine-qemu\x2d12\x2dhome\x2dassistant.scope/libvirt/vcpu0`: {Kind: "vm", ID: "12", Name: "home-assistant"},
		"/lxc.payload.web/init.scope": {Kind: "lxc", Name: "web"},
		"/lxc.monitor.web":            {Kind: "lxc", Name: "web"},
		`/system.slice/system-titanos\x2dlxc.slice/titanos-lxc@my\x2dbox.service`: {Kind: "lxc", Name: "my-box"},
		"/system.slice/nfs-server.service":                                        {Kind: "service", Name: "nfs-server"},
		`/system.slice/system-getty.slice/getty@tty1.service`:                     {Kind: "service", Name: "getty@tty1"},
		"/user.slice/user-1000.slice/session-3.scope":                             {},
		"/init.scope":                      {},
		"":                                 {},
		"/system.slice/docker-short.scope": {},
	}
	for path, want := range cases {
		if got := OwnerOf(path); got != want {
			t.Errorf("%q: %+v, want %+v", path, got, want)
		}
	}
}

func TestParseStatRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "12 no parens", "x (a) S 1", "12 (a) S 1 2"} {
		if _, err := parseStat(s); err == nil {
			t.Errorf("%q parsed", s)
		}
	}
	if got := parseCmdline(strings.Repeat("ä", 400)); !strings.HasSuffix(got, "…") || len(got) > 520 {
		t.Errorf("long command %d", len(got))
	}
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
