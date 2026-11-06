// Package procs lists the processes of the NAS for the task manager
// (PLAN 61, Monitor app) and ends single ones on request. Everything is
// read from procfs; nothing here touches a disk or the stick.
package procs

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// pfKthread marks a kernel thread in the flags of /proc/<pid>/stat.
const pfKthread = 0x00200000

// Owner is what a process belongs to, from its cgroup: a Docker
// container, a VM (libvirt), a system container (LXC) or a service of
// the NAS. Kind is empty for processes outside all of them.
type Owner struct {
	Kind string `json:"kind"` // "docker", "vm", "lxc", "service" or ""
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// Process is one row of the list.
type Process struct {
	PID         int     `json:"pid"`
	PPID        int     `json:"ppid"`
	Name        string  `json:"name"`
	Command     string  `json:"command"`
	User        string  `json:"user"`
	State       string  `json:"state"`
	CPUPercent  float64 `json:"cpuPercent"`
	MemoryBytes uint64  `json:"memoryBytes"`
	// Start is the start time in clock ticks after boot (field 22 of
	// stat); a signal must name it, so a reused pid is never hit.
	Start uint64 `json:"start"`
	Owner Owner  `json:"owner"`
	// Protected processes cannot be ended here (titand, its parent, init).
	Protected bool `json:"protected,omitempty"`
}

// List is the answer of GET /system/processes.
type List struct {
	Processes []Process `json:"processes"`
	// CPUs is the number of logical CPUs; CPU percentages are of the
	// whole machine (100 = all CPUs busy).
	CPUs             int     `json:"cpus"`
	MemoryTotalBytes uint64  `json:"memoryTotalBytes"`
	SampledSeconds   float64 `json:"sampledSeconds"`
}

// raw is what one read of /proc/<pid> gives.
type raw struct {
	pid, ppid, tgid int
	name, state     string
	cmdline         string
	uid             int
	flags           uint64
	ticks           uint64
	start           uint64
	rssPages        uint64
	cgroup          string
}

func (r raw) kernel() bool { return r.flags&pfKthread != 0 || r.ppid == 2 || r.pid == 2 }

type key struct {
	pid   int
	start uint64
}

type sample struct {
	at    time.Time
	total uint64
	ticks map[key]uint64
}

var (
	ErrNotFound  = errors.New("process not found")
	ErrProtected = errors.New("process is protected")
	ErrChanged   = errors.New("process changed")
	ErrDisabled  = errors.New("not available in development mode")
	ErrSignal    = errors.New("unknown signal")
)

// Sampler reads processes and keeps the previous read for CPU usage.
type Sampler struct {
	// Root is the procfs root, Passwd the account list for names.
	Root, Passwd string
	// Self and Parent are titand and the process that started it.
	Self, Parent int
	// Disabled refuses signals (development mode on someone's computer).
	Disabled bool
	// Kill sends the signal; tests replace it.
	Kill func(pid int, sig syscall.Signal) error
	// Wait is the pause between two reads when there is no recent one.
	Wait     time.Duration
	PageSize uint64

	mu   sync.Mutex
	prev *sample
}

// New returns a sampler of the real /proc.
func New() *Sampler {
	return &Sampler{Root: "/proc", Passwd: "/etc/passwd", Self: os.Getpid(), Parent: os.Getppid(), Kill: syscall.Kill, Wait: 300 * time.Millisecond, PageSize: uint64(os.Getpagesize())}
}

// List reads all user space processes. CPU usage is measured against the
// previous call when that was less than ten seconds ago (the page polls
// every two seconds); otherwise two reads are taken Wait apart.
func (s *Sampler) List() (List, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	procs, cur, err := s.read(now)
	if err != nil {
		return List{}, err
	}
	if s.prev == nil || now.Sub(s.prev.at) > 10*time.Second || now.Sub(s.prev.at) <= 0 || cur.total <= s.prev.total {
		time.Sleep(s.Wait)
		s.prev = cur
		now = time.Now()
		if procs, cur, err = s.read(now); err != nil {
			return List{}, err
		}
	}
	prev := s.prev
	s.prev = cur
	cpus := countCPUs(readFile(filepath.Join(s.Root, "stat")))
	users := readPasswd(s.Passwd)
	out := List{Processes: make([]Process, 0, len(procs)), CPUs: cpus, MemoryTotalBytes: memTotal(readFile(filepath.Join(s.Root, "meminfo"))), SampledSeconds: now.Sub(prev.at).Seconds()}
	dTotal := float64(cur.total - prev.total)
	for _, p := range procs {
		// Threads have no row of their own (a /proc listing hides them;
		// a fixture or a race may not).
		if p.kernel() || p.tgid != p.pid {
			continue
		}
		var cpu float64
		if before, ok := prev.ticks[key{p.pid, p.start}]; ok && dTotal > 0 && p.ticks >= before {
			cpu = float64(p.ticks-before) / dTotal * 100
		}
		user, ok := users[p.uid]
		if !ok {
			user = strconv.Itoa(p.uid)
		}
		command := p.cmdline
		if command == "" {
			command = p.name
		}
		out.Processes = append(out.Processes, Process{
			PID: p.pid, PPID: p.ppid, Name: p.name, Command: command, User: user, State: p.state,
			CPUPercent: round1(cpu), MemoryBytes: p.rssPages * s.pageSize(), Start: p.start,
			Owner: OwnerOf(p.cgroup), Protected: s.protected(p),
		})
	}
	sort.Slice(out.Processes, func(i, j int) bool { return out.Processes[i].PID < out.Processes[j].PID })
	return out, nil
}

func (s *Sampler) pageSize() uint64 {
	if s.PageSize == 0 {
		return 4096
	}
	return s.PageSize
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }

// protected says whether a process must not be ended from the web UI.
func (s *Sampler) protected(p raw) bool {
	return p.pid <= 2 || p.kernel() || p.pid == s.Self || p.pid == s.Parent || p.tgid == s.Self || p.tgid == s.Parent
}

// Signal sends SIGTERM ("term") or SIGKILL ("kill") to a process. start
// must be the start time the list showed; a different one means the pid
// now names another process. It returns the name of the process.
func (s *Sampler) Signal(pid int, start uint64, signal string) (string, error) {
	sig, ok := map[string]syscall.Signal{"term": syscall.SIGTERM, "kill": syscall.SIGKILL}[signal]
	if !ok {
		return "", ErrSignal
	}
	if pid <= 0 {
		return "", ErrNotFound
	}
	p, err := s.readOne(pid)
	if err != nil {
		return "", ErrNotFound
	}
	// /proc/<tid> resolves for threads too, and a signal to a thread
	// reaches its whole process: only process ids are accepted.
	if p.tgid != pid {
		return p.name, ErrNotFound
	}
	if s.protected(p) {
		return p.name, ErrProtected
	}
	if p.start != start {
		return p.name, ErrChanged
	}
	if s.Disabled {
		return p.name, ErrDisabled
	}
	kill := s.Kill
	if kill == nil {
		kill = syscall.Kill
	}
	if err := kill(pid, sig); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return p.name, ErrNotFound
		}
		return p.name, err
	}
	return p.name, nil
}

func (s *Sampler) read(now time.Time) ([]raw, *sample, error) {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return nil, nil, err
	}
	smp := &sample{at: now, total: totalJiffies(readFile(filepath.Join(s.Root, "stat"))), ticks: map[key]uint64{}}
	var out []raw
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		p, err := s.readOne(pid)
		if err != nil {
			continue // gone in the meantime
		}
		smp.ticks[key{p.pid, p.start}] = p.ticks
		out = append(out, p)
	}
	return out, smp, nil
}

func (s *Sampler) readOne(pid int) (raw, error) {
	dir := filepath.Join(s.Root, strconv.Itoa(pid))
	stat, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return raw{}, err
	}
	p, err := parseStat(string(stat))
	if err != nil {
		return raw{}, err
	}
	p.tgid, p.uid = parseStatus(readFile(filepath.Join(dir, "status")))
	if p.tgid == 0 {
		p.tgid = p.pid
	}
	p.cmdline = parseCmdline(readFile(filepath.Join(dir, "cmdline")))
	p.cgroup = parseCgroup(readFile(filepath.Join(dir, "cgroup")))
	return p, nil
}

func readFile(path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}

// parseStat reads /proc/<pid>/stat. The name stands in parentheses and
// may itself hold spaces and parentheses, so the fields after it are
// counted from the last ")".
func parseStat(data string) (raw, error) {
	open, close := strings.IndexByte(data, '('), strings.LastIndexByte(data, ')')
	if open < 0 || close < open {
		return raw{}, errors.New("invalid stat")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(data[:open]))
	if err != nil {
		return raw{}, err
	}
	// fields[0] is field 3 (state) of proc(5).
	fields := strings.Fields(data[close+1:])
	if len(fields) < 22 {
		return raw{}, errors.New("short stat")
	}
	num := func(field int) uint64 {
		v, _ := strconv.ParseUint(fields[field-3], 10, 64)
		return v
	}
	ppid, _ := strconv.Atoi(fields[1])
	return raw{
		pid: pid, ppid: ppid, name: data[open+1 : close], state: fields[0],
		flags: num(9), ticks: num(14) + num(15), start: num(22), rssPages: num(24),
	}, nil
}

// parseStatus returns the thread group id and the real user id.
func parseStatus(data string) (tgid, uid int) {
	uid = -1
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		if v, ok := strings.CutPrefix(line, "Tgid:"); ok {
			tgid, _ = strconv.Atoi(strings.TrimSpace(v))
		} else if v, ok := strings.CutPrefix(line, "Uid:"); ok {
			if f := strings.Fields(v); len(f) > 0 {
				uid, _ = strconv.Atoi(f[0])
			}
		}
	}
	return tgid, uid
}

// parseCmdline joins the NUL separated arguments; long ones are cut.
func parseCmdline(data string) string {
	text := strings.TrimSpace(strings.Join(strings.Split(strings.TrimRight(data, "\x00"), "\x00"), " "))
	const most = 512
	if len(text) > most {
		cut := most
		for cut > 0 && !utf8Start(text[cut]) {
			cut--
		}
		text = text[:cut] + "…"
	}
	return text
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// parseCgroup returns the path of the unified hierarchy ("0::/…").
func parseCgroup(data string) string {
	for _, line := range strings.Split(data, "\n") {
		if v, ok := strings.CutPrefix(line, "0::"); ok {
			return v
		}
	}
	return ""
}

func totalJiffies(stat string) uint64 {
	for _, line := range strings.Split(stat, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "cpu" {
			continue
		}
		var total uint64
		// user nice system idle iowait irq softirq steal; guest time is
		// already part of user.
		for _, f := range fields[1:min(len(fields), 9)] {
			v, _ := strconv.ParseUint(f, 10, 64)
			total += v
		}
		return total
	}
	return 0
}

func countCPUs(stat string) int {
	n := 0
	for _, line := range strings.Split(stat, "\n") {
		if strings.HasPrefix(line, "cpu") && !strings.HasPrefix(line, "cpu ") {
			n++
		}
	}
	return max(n, 1)
}

func memTotal(meminfo string) uint64 {
	for _, line := range strings.Split(meminfo, "\n") {
		if v, ok := strings.CutPrefix(line, "MemTotal:"); ok {
			f := strings.Fields(v)
			if len(f) > 0 {
				kb, _ := strconv.ParseUint(f[0], 10, 64)
				return kb * 1024
			}
		}
	}
	return 0
}

func readPasswd(path string) map[int]string {
	users := map[int]string{}
	for _, line := range strings.Split(readFile(path), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 3 {
			continue
		}
		if uid, err := strconv.Atoi(f[2]); err == nil {
			if _, dup := users[uid]; !dup {
				users[uid] = f[0]
			}
		}
	}
	return users
}
