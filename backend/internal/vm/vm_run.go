package vm

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Run starts the VMs marked for autostart once their volumes are there and
// samples the running ones, until ctx is done.
func (m *Manager) Run(ctx context.Context, mounted func() []string) {
	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()
	// After a restart of titand (an app update) exclusive VMs may still
	// run: what is reserved for them is taken up again, so that it is
	// given back when they stop.
	if !m.Disabled {
		m.reserve(ctx)
	}
	for {
		m.mu.Lock()
		loaded := m.loaded
		m.mu.Unlock()
		if !loaded {
			_ = m.Load() // Load keeps its own state; a failure shows up as not loaded
		} else if !m.Disabled && !m.off.Load() {
			m.runSnapshotSchedules(ctx, mounted(), time.Now())
			m.runPowerSchedules(ctx, mounted(), time.Now())
			m.refreshVolumeCopies(mounted())
			m.autostart(ctx, mounted())
			m.sample()
			m.checkIOErrors(ctx)
			if m.AfterPoll != nil {
				if err := m.AfterPoll(ctx, m.RunningNames()); err != nil {
					slog.Warn("return VM hardware", "err", err)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) autostart(ctx context.Context, mounted []string) {
	if m.AutoReady != nil && !m.AutoReady() {
		return
	}
	m.mu.Lock()
	var due []string
	for name, vm := range m.vms {
		if vm.Autostart && !m.autostarted[name] && m.available(vm, mounted) {
			// One attempt per boot: a VM someone shut down stays down.
			m.autostarted[name] = true
			due = append(due, name)
		}
	}
	if len(due) > 0 {
		m.noteAutostarted()
	}
	m.mu.Unlock()
	sort.Strings(due)
	for _, name := range due {
		if err := m.Start(ctx, name, mounted); err != nil && !errors.Is(err, ErrRunning) {
			slog.Error("autostart of a VM failed", "vm", name, "err", err)
		}
	}
}

// sample reads CPU time and memory of the running VMs from /proc.
func (m *Manager) sample() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	defer m.release()
	for name, vm := range m.vms {
		pid := m.pid(name)
		if pid == 0 {
			delete(m.samples, name)
			continue
		}
		ticks, started := readStat(filepath.Join(m.procDir, strconv.Itoa(pid), "stat"))
		s := m.samples[name]
		if s == nil {
			s = &sample{since: bootTime(m.procDir).Add(time.Duration(started) * time.Second / clockTicks)}
			m.samples[name] = s
		} else if elapsed := now.Sub(s.at).Seconds(); elapsed > 0 && ticks >= s.ticks {
			// Seconds of CPU per second, as a share of what the vCPUs can do.
			s.percent = min(100, 100*float64(ticks-s.ticks)/clockTicks/elapsed/float64(max(vm.VCPUs, 1)))
		}
		s.ticks, s.at = ticks, now
		s.memory = readRSS(filepath.Join(m.procDir, strconv.Itoa(pid), "status"))
	}
}

// clockTicks is the unit of the times in /proc/<pid>/stat on Linux.
const clockTicks = 100

// readStat returns the CPU time a process used and when it started, both
// in clock ticks.
func readStat(path string) (cpu, started uint64) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0
	}
	// The name of the program is in brackets and may hold spaces.
	_, rest, found := strings.Cut(string(data), ") ")
	fields := strings.Fields(rest)
	if !found || len(fields) < 20 {
		return 0, 0
	}
	// After the name: state is field 3 of the line, utime 14, stime 15, starttime 22.
	utime, _ := strconv.ParseUint(fields[11], 10, 64)
	stime, _ := strconv.ParseUint(fields[12], 10, 64)
	started, _ = strconv.ParseUint(fields[19], 10, 64)
	return utime + stime, started
}

func readRSS(path string) uint64 {
	data, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "VmRSS:"); ok {
			kb, _ := strconv.ParseUint(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "kB")), 10, 64)
			return kb * 1024
		}
	}
	return 0
}

func bootTime(procDir string) time.Time {
	data, _ := os.ReadFile(filepath.Join(procDir, "stat"))
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "btime "); ok {
			seconds, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			return time.Unix(seconds, 0)
		}
	}
	return time.Time{}
}
