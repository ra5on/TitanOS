package api

import (
	"bufio"
	"context"
	"errors"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/advisor"
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/monitoring"
	"github.com/ra5on/TitanOS/backend/internal/naming"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/whatif"
)

// foresightState is what the group keeps in RAM per server: short rings of
// samples (CPU each minute, memory every ten minutes, volume usage each
// hour), the last rebuild speed seen per volume and the last report. It
// lives outside Server so that the shared struct stays as it is.
type foresightState struct {
	mu       sync.Mutex
	cpu      []advisor.CPUSample
	mem      []advisor.MemorySample
	vols     []monitoring.Sample
	measured map[string]uint64
	memAt    time.Time
	statAt   time.Time
	volAt    time.Time
	saved    advisorSaved
	loaded   bool
	running  bool
	tried    time.Time
	// Tests replace what is read from the NAS.
	model func(context.Context) (whatif.Model, error)
	input func(context.Context) advisor.Input
}

var foresightStates sync.Map // *Server → *foresightState

func (s *Server) foresight() *foresightState {
	v, _ := foresightStates.LoadOrStore(s, &foresightState{measured: map[string]uint64{}})
	return v.(*foresightState)
}

const (
	cpuRing  = 24 * 60    // one day of minutes
	memRing  = 7 * 24 * 6 // a week of ten minutes
	volsRing = 30 * 24    // a month of hours
	memEvery = 10 * time.Minute
	volEvery = time.Hour
	// statEvery: how often the storage status is read for rebuild speeds.
	statEvery = 5 * time.Minute
)

// procMeminfo is read for the swap; tests point it elsewhere.
var procMeminfo = "/proc/meminfo"

// swap reads SwapTotal and SwapFree in bytes.
func swap() (total, free uint64) {
	f, err := os.Open(procMeminfo)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "SwapTotal:":
			total = n * 1024
		case "SwapFree:":
			free = n * 1024
		}
	}
	return total, free
}

func ring[T any](list []T, v T, max int) []T {
	list = append(list, v)
	if len(list) > max {
		list = slices.Clone(list[len(list)-max:])
	}
	return list
}

// mergeSamples puts the kept hourly samples before the ones taken since
// the start, oldest first, at most volsRing.
func mergeSamples(saved, ram []monitoring.Sample) []monitoring.Sample {
	var out []monitoring.Sample
	for _, x := range saved {
		if len(ram) == 0 || x.At.Before(ram[0].At) {
			out = append(out, x)
		}
	}
	out = append(out, ram...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	if len(out) > volsRing {
		out = slices.Clone(out[len(out)-volsRing:])
	}
	return out
}

// foresightSample takes the samples of this minute, from RAM only: the
// monitor's values, /proc and the storage status (lsblk, mdstat, statfs of
// mounted volumes), none of which wakes a disk.
func (s *Server) foresightSample(now time.Time, f agentFeatures) {
	env := s.agentEnv()
	st := s.foresight()
	st.mu.Lock()
	if env.monitor != nil && f.Advisor.Enabled {
		info := env.monitor.Info()
		st.cpu = ring(st.cpu, advisor.CPUSample{At: now, Percent: info.CPU.UsagePercent}, cpuRing)
		if now.Sub(st.memAt) >= memEvery {
			st.memAt = now
			mem := env.monitor.Memory()
			sample := advisor.MemorySample{At: now, Total: mem.TotalBytes, Available: mem.AvailableBytes}
			if mem.Pressure != nil {
				sample.PressureSome = mem.Pressure.SomeAvg10
			}
			sample.SwapTotal, sample.SwapFree = swap()
			st.mem = ring(st.mem, sample, memRing)
		}
	}
	if env.storage == nil || now.Sub(st.statAt) < statEvery {
		st.mu.Unlock()
		return
	}
	st.statAt = now
	st.mu.Unlock()
	// The status is read without the lock: it may take a while.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	status, err := env.storage.Status(ctx)
	if err != nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, v := range status.Volumes {
		if v.Sync != nil && v.Sync.SpeedBytesPerSecond > 0 && (v.State == storage.VolumeRebuilding || v.State == storage.VolumeSyncing || v.State == storage.VolumeChecking) {
			st.measured[v.Name] = v.Sync.SpeedBytesPerSecond
		}
	}
	if f.Advisor.Enabled && now.Sub(st.volAt) >= volEvery {
		st.volAt = now
		sample := monitoring.Sample{At: now}
		for _, v := range status.Volumes {
			if v.Mounted && v.SizeBytes > 0 {
				sample.Volumes = append(sample.Volumes, monitoring.Volume{Name: v.Name, Total: v.SizeBytes, Used: v.UsedBytes})
			}
		}
		st.vols = ring(st.vols, sample, volsRing)
	}
}

// growth is the measured daily growth per volume: from the longer
// monitoring history when it is on, else from the adviser's own hourly
// samples. The regression is monitoring's.
func (s *Server) growth(now time.Time) map[string]float64 {
	env := s.agentEnv()
	st := s.foresight()
	st.mu.Lock()
	samples := slices.Clone(st.vols)
	st.mu.Unlock()
	var trends []monitoring.Trend
	if env.monitoring != nil {
		trends = env.monitoring.CachedTrends(now)
	}
	known := map[string]bool{}
	out := map[string]float64{}
	for _, t := range trends {
		if t.Known {
			out[t.Volume], known[t.Volume] = t.DailyBytes, true
		}
	}
	for _, t := range monitoring.CapacityTrends(samples, now) {
		if t.Known && !known[t.Volume] {
			out[t.Volume] = t.DailyBytes
		}
	}
	return out
}

var errNoStorage = errors.New("storage is not available")

// smartOf is the SMART report of each disk, from the RAM cache.
func (s *Server) smartOf(ctx context.Context) map[string]storage.SmartReport {
	env := s.agentEnv()
	out := map[string]storage.SmartReport{}
	if env.storage == nil {
		return out
	}
	reports, err := env.storage.SmartReports(ctx, false)
	if err != nil {
		return out
	}
	for _, r := range reports {
		out[r.Disk] = r
	}
	return out
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// whatifModel is the picture of the NAS the scenarios are played on.
func (s *Server) whatifModel(ctx context.Context) (whatif.Model, error) {
	env := s.agentEnv()
	st := s.foresight()
	if st.model != nil {
		return st.model(ctx)
	}
	if env.storage == nil {
		return whatif.Model{}, errNoStorage
	}
	status, err := env.storage.Status(ctx)
	if err != nil {
		return whatif.Model{}, err
	}
	now := time.Now()
	m := whatif.Model{Now: now, Growth: s.growth(now), AgentVolume: s.agentSettings().Volume, Disks: []whatif.Disk{}, Volumes: []whatif.Volume{}, Shares: []whatif.Share{}, Backups: []whatif.Backup{}}
	smart := s.smartOf(ctx)
	for _, d := range status.Disks {
		if d.Role == storage.RoleStick {
			continue
		}
		wd := whatif.Disk{Name: d.Name, Model: d.Model, Serial: d.Serial, Kind: d.Kind, SizeBytes: d.SizeBytes, Volume: d.Volume, MemberState: d.MemberState, WearPercent: -1}
		if d.Smart != nil {
			wd.WearPercent = d.Smart.WearPercent
		}
		if r, ok := smart[d.Name]; ok && r.Values != nil {
			v := r.Values
			wd.SmartKnown = true
			wd.SmartFailed = r.State == storage.SmartFailed
			wd.PowerOnHours = deref(v.PowerOnHours)
			wd.Reallocated, wd.Pending = deref(v.ReallocatedSectors), deref(v.PendingSectors)
			wd.Uncorrectable, wd.CRC = deref(v.UncorrectableSectors), deref(v.CRCErrors)
			if v.NVMe != nil {
				wd.WearPercent = v.NVMe.PercentageUsed
				wd.Uncorrectable += v.NVMe.MediaErrors
			}
		}
		m.Disks = append(m.Disks, wd)
	}
	st.mu.Lock()
	measured := map[string]uint64{}
	for k, v := range st.measured {
		measured[k] = v
	}
	st.mu.Unlock()
	for _, v := range status.Volumes {
		wv := whatif.Volume{Name: v.Name, Level: v.Level, Filesystem: v.Filesystem, State: v.State, Mounted: v.Mounted, SizeBytes: v.SizeBytes, UsedBytes: v.UsedBytes, RaidDisks: v.RaidDisks, SyncSpeed: v.SyncSpeed, MeasuredBps: measured[v.Name]}
		for _, mb := range v.Members {
			wv.Members = append(wv.Members, whatif.Member{Disk: mb.Disk, State: mb.State})
		}
		if v.Cache != nil {
			wv.Cache = &whatif.Cache{Disk: v.Cache.Disk, Mode: v.Cache.Mode}
		}
		if v.Sync != nil && v.Sync.SpeedBytesPerSecond > 0 {
			wv.MeasuredBps = v.Sync.SpeedBytesPerSecond
		}
		m.Volumes = append(m.Volumes, wv)
	}
	if env.shares != nil {
		for _, sh := range env.shares.List() {
			m.Shares = append(m.Shares, whatif.Share{Name: sh.Name, Volume: sh.Volume})
		}
	}
	if env.backup != nil {
		// Cached states only: never opens a repository or a state file.
		for _, view := range env.backup.CachedViews() {
			m.Backups = append(m.Backups, backupOf(view))
		}
	}
	return m, nil
}

// backupOf is what a backup job means for the loss of a volume.
func backupOf(view backup.View) whatif.Backup {
	j := view.Job
	b := whatif.Backup{ID: j.ID, Enabled: j.Enabled, Known: view.StateKnown, DestKind: j.Destination.Type,
		Shares: slices.Clone(j.Sources), Volumes: slices.Clone(j.Volumes), LastOK: view.State.LastOK, Bytes: view.State.Bytes}
	if j.CoordinatedSelection != nil {
		b.Shares = append(b.Shares, j.CoordinatedSelection.Sources...)
	}
	if j.Destination.Type == "local" || j.Destination.Type == "local-copy" {
		b.DestVolume = j.Destination.Volume
	}
	return b
}

// volumeAsleep reports whether a disk of the volume sleeps; reading or
// writing there would wake it.
func (s *Server) volumeAsleep(vol string) bool {
	env := s.agentEnv()
	if env.storage == nil || vol == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, err := env.storage.DiskSleepStatus(ctx)
	if err != nil {
		return true
	}
	for _, d := range st.DiskStatus {
		if d.Volume == vol && d.State == "asleep" {
			return true
		}
	}
	return false
}

// smartHistoryVolume is where internal/smarthistory keeps the history: the
// first mounted volume by name (its own rule).
func (s *Server) smartHistoryVolume() string {
	env := s.agentEnv()
	if env.storage == nil {
		return ""
	}
	for _, v := range slices.Sorted(slices.Values(env.storage.Mounted())) {
		if naming.ValidVolume(v) {
			return v
		}
	}
	return ""
}

func sampleOfValues(day string, v *storage.SmartValues) advisor.Sample {
	if v == nil {
		return advisor.Sample{Day: day}
	}
	out := advisor.Sample{Day: day, PowerOnHours: v.PowerOnHours, TemperatureC: v.TemperatureC, Reallocated: v.ReallocatedSectors,
		Pending: v.PendingSectors, Uncorrectable: v.UncorrectableSectors, CRC: v.CRCErrors}
	if v.NVMe != nil {
		media, used, spare, threshold := v.NVMe.MediaErrors, v.NVMe.PercentageUsed, v.NVMe.AvailableSpare, v.NVMe.AvailableSpareThreshold
		out.MediaErrors, out.PercentageUsed, out.AvailableSpare, out.SpareThreshold = &media, &used, &spare, &threshold
	}
	return out
}

// advisorInput collects what the adviser judges. withHistory reads the
// kept SMART history (callers check that its volume is awake).
func (s *Server) advisorInput(ctx context.Context, a agentAdvisor, now time.Time, withHistory bool) advisor.Input {
	env := s.agentEnv()
	st := s.foresight()
	if st.input != nil {
		in := st.input(ctx)
		in.Predict, in.Bottlenecks = a.predict(), a.bottlenecks()
		return in
	}
	in := advisor.Input{Now: now, Predict: a.predict(), Bottlenecks: a.bottlenecks()}
	var status storage.Status
	if env.storage != nil {
		status, _ = env.storage.Status(ctx)
	}
	sizes := map[string]storage.Disk{}
	for _, d := range status.Disks {
		sizes[d.Name] = d
	}
	if env.storage != nil {
		reports, _ := env.storage.SmartReports(ctx, false)
		for _, r := range reports {
			if r.Role == storage.RoleStick || r.State == storage.SmartUnavailable {
				continue
			}
			d := advisor.Disk{Name: r.Disk, Model: r.Model, Serial: r.Serial, Kind: r.Kind, SizeBytes: sizes[r.Disk].SizeBytes, Volume: r.Volume,
				Known: r.Values != nil, SmartFailed: r.State == storage.SmartFailed, Current: sampleOfValues(now.Format(time.DateOnly), r.Values)}
			if withHistory && env.smartHistory != nil {
				if h, err := env.smartHistory.History(r); err == nil {
					for _, x := range h.Samples {
						d.History = append(d.History, advisor.Sample{Day: x.Day, PowerOnHours: x.PowerOnHours, TemperatureC: x.TemperatureC, Reallocated: x.Reallocated,
							Pending: x.Pending, Uncorrectable: x.Uncorrectable, CRC: x.CRCErrors, MediaErrors: x.MediaErrors, PercentageUsed: x.PercentageUsed, AvailableSpare: x.AvailableSpare})
					}
				}
			}
			in.Disks = append(in.Disks, d)
		}
	}
	st.mu.Lock()
	in.CPU, in.Memory = slices.Clone(st.cpu), slices.Clone(st.mem)
	st.mu.Unlock()
	growth := s.growth(now)
	for _, v := range status.Volumes {
		if !v.Mounted || v.SizeBytes == 0 {
			continue
		}
		vol := advisor.Volume{Name: v.Name, Size: v.SizeBytes, Used: v.UsedBytes}
		if g, ok := growth[v.Name]; ok {
			vol.DailyBytes = &g
		}
		in.Volumes = append(in.Volumes, vol)
	}
	if env.monitor != nil {
		for _, t := range env.monitor.Info().Temperatures {
			if t.Celsius == nil || strings.HasPrefix(t.Chip, "drivetemp") || strings.HasPrefix(t.Chip, "nvme") {
				continue // disks come from SMART
			}
			in.Temps = append(in.Temps, advisor.Temperature{Label: strings.TrimSpace(t.Chip + " " + t.Label), Celsius: *t.Celsius})
		}
	}
	if env.accelerators != nil {
		ollama := s.ollamaVRAM(ctx)
		for _, acc := range env.accelerators.Snapshot() {
			if acc.MemoryTotalBytes == nil {
				continue
			}
			g := advisor.GPU{Name: acc.Name, UsedBytes: acc.MemoryUsedBytes, Total: acc.MemoryTotalBytes}
			if ollama != nil {
				g.OllamaBytes, ollama = ollama, nil // the first card with memory
			}
			in.GPUs = append(in.GPUs, g)
		}
	}
	return in
}

// ollamaVRAM is what the loaded models of the agent take of the graphics
// memory; nil when Ollama is not reachable.
func (s *Server) ollamaVRAM(ctx context.Context) *int64 {
	if !s.agentSettings().Enabled {
		return nil
	}
	client, err := s.agentOllama()
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	running, err := client.Running(ctx)
	if err != nil {
		return nil
	}
	var sum int64
	for _, r := range running {
		sum += r.VRAM
	}
	return &sum
}
