package advisor

import (
	"slices"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 6, 3, 30, 0, 0, time.UTC)

func p64(v uint64) *uint64 { return &v }
func pi(v int) *int        { return &v }
func ph(v int64) *int64    { return &v }

// history makes one sample a day for days days, ending yesterday; fill
// sets the values of day i (0 = oldest).
func history(days int, fill func(i int, s *Sample)) []Sample {
	out := make([]Sample, days)
	for i := range out {
		out[i].Day = now.AddDate(0, 0, i-days).Format(time.DateOnly)
		fill(i, &out[i])
	}
	return out
}

func hdd(name string, hist []Sample, cur Sample) Disk {
	return Disk{Name: name, Kind: "hdd", Model: "WD80EFZZ", SizeBytes: 8e12, Known: true, Current: cur, History: hist}
}

func TestDiskPrediction(t *testing.T) {
	cases := []struct {
		name  string
		disk  Disk
		level string
		code  string
		also  string
	}{
		{"healthy flat", hdd("sda", history(60, func(i int, s *Sample) { s.Reallocated, s.Pending = p64(0), p64(0) }), Sample{Reallocated: p64(0), Pending: p64(0), PowerOnHours: ph(9000)}), LevelOK, CodeDiskOK, ""},
		{"sectors rising with pending (mockup)", hdd("sda", history(42, func(i int, s *Sample) {
			s.Reallocated, s.Pending = p64(uint64(i/10)), p64(1)
		}), Sample{Reallocated: p64(4), Pending: p64(2), PowerOnHours: ph(36800)}), LevelFail, CodeSectorsRising, CodePendingRising},
		{"many new sectors alone", hdd("sdb", history(30, func(i int, s *Sample) { s.Reallocated = p64(uint64(i)) }), Sample{Reallocated: p64(40)}), LevelFail, CodeSectorsRising, ""},
		{"one new sector: watch", hdd("sdc", history(30, func(i int, s *Sample) { s.Reallocated = p64(0) }), Sample{Reallocated: p64(1)}), LevelWatch, CodeSectorsRising, ""},
		{"old sectors stable", hdd("sdd", history(60, func(i int, s *Sample) { s.Reallocated = p64(8) }), Sample{Reallocated: p64(8)}), LevelWatch, CodeSectorsStable, ""},
		{"rising outside the window does not count", hdd("sde", append(history(1, func(i int, s *Sample) { s.Reallocated = p64(0); s.Day = "2026-01-01" }),
			history(40, func(i int, s *Sample) { s.Reallocated = p64(8) })...), Sample{Reallocated: p64(8)}), LevelWatch, CodeSectorsStable, ""},
		{"pending went back", hdd("sdf", history(20, func(i int, s *Sample) { s.Pending = p64(uint64(5 - min(i, 5))) }), Sample{Pending: p64(0)}), LevelOK, CodeDiskOK, ""},
		{"crc rising is the cable", hdd("sdg", history(20, func(i int, s *Sample) { s.CRC = p64(uint64(i)) }), Sample{CRC: p64(30)}), LevelWatch, CodeCable, ""},
		{"old disk", hdd("sdh", nil, Sample{PowerOnHours: ph(50000), Reallocated: p64(0)}), LevelWatch, CodeOld, ""},
		{"smart failed", Disk{Name: "sdi", Kind: "hdd", Known: true, SmartFailed: true}, LevelFail, CodeSmartFailed, ""},
		{"never read", Disk{Name: "sdj", Kind: "hdd"}, LevelWatch, CodeNoSmart, ""},
		{"uncorrectable rising", hdd("sdk", history(10, func(i int, s *Sample) { s.Uncorrectable = p64(uint64(i / 3)) }), Sample{Uncorrectable: p64(5)}), LevelFail, CodeUncorrectable, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := JudgeDisk(c.disk, now)
			if f.Level != c.level || f.Code != c.code {
				t.Fatalf("got %s/%s (also %v), want %s/%s", f.Level, f.Code, f.Also, c.level, c.code)
			}
			if c.also != "" && !slices.Contains(f.Also, c.also) {
				t.Fatalf("also %v, want %s", f.Also, c.also)
			}
		})
	}
	// The mockup's words: 0 → 4 in 6 weeks.
	f := JudgeDisk(cases[1].disk, now)
	if *f.From != 0 || *f.To != 4 || *f.Weeks != 6 || *f.Hours != 36800 {
		t.Fatalf("values %v %v %v %v", *f.From, *f.To, *f.Weeks, *f.Hours)
	}
}

func TestSSDWear(t *testing.T) {
	nvme := func(hist []Sample, cur Sample) Disk {
		return Disk{Name: "nvme0n1", Kind: "nvme", Known: true, Current: cur, History: hist}
	}
	// 1 % a week from 70 %: 30 weeks left → not soon.
	slow := history(42, func(i int, s *Sample) { s.PercentageUsed = pi(64 + i/7) })
	if f := JudgeDisk(nvme(slow, Sample{PercentageUsed: pi(70)}), now); f.Level != LevelOK {
		t.Fatalf("slow wear %+v", f)
	}
	// 1 % a day from 50 %: 50 days left → worn out soon.
	fast := history(30, func(i int, s *Sample) { s.PercentageUsed = pi(20 + i) })
	f := JudgeDisk(nvme(fast, Sample{PercentageUsed: pi(50)}), now)
	if f.Level != LevelFail || f.Code != CodeWornOut || f.Days == nil || *f.Days > 60 {
		t.Fatalf("fast wear %+v", f)
	}
	if f := JudgeDisk(nvme(nil, Sample{PercentageUsed: pi(85)}), now); f.Code != CodeWear || f.Level != LevelWatch {
		t.Fatalf("85 %% %+v", f)
	}
	if f := JudgeDisk(nvme(nil, Sample{AvailableSpare: pi(5), SpareThreshold: pi(10)}), now); f.Code != CodeSpareLow {
		t.Fatalf("spare %+v", f)
	}
	media := history(20, func(i int, s *Sample) { s.MediaErrors = p64(uint64(i / 5)) })
	if f := JudgeDisk(nvme(media, Sample{MediaErrors: p64(4)}), now); f.Code != CodeMediaRising {
		t.Fatalf("media %+v", f)
	}
}

func TestBottlenecks(t *testing.T) {
	const gib = 1 << 30
	var mem []MemorySample
	for i := range 24 {
		avail := uint64(8 * gib)
		if i >= 16 { // evenings: 92 % used
			avail = uint64(128 * gib / 100)
		}
		mem = append(mem, MemorySample{At: now.Add(time.Duration(i-24) * time.Hour), Total: 16 * gib, Available: avail})
	}
	in := Input{Now: now, Bottlenecks: true, Memory: mem,
		Volumes: []Volume{{Name: "Fast", Size: 1000, Used: 870, DailyBytes: func() *float64 { v := 3.7; return &v }()}, {Name: "Data", Size: 1000, Used: 300}},
		GPUs:    []GPU{{Name: "RTX 5060 Ti", Total: func() *float64 { v := 16e9; return &v }(), OllamaBytes: ph(12_600_000_000)}},
		Temps:   []Temperature{{Label: "Package id 0", Celsius: 52}},
	}
	for i := range 24 * 60 {
		in.CPU = append(in.CPU, CPUSample{At: now.Add(-time.Duration(i) * time.Minute), Percent: 20})
	}
	r := Judge(in)
	byArea := map[string]Finding{}
	for _, f := range r.Findings {
		if f.Area != AreaVolume || f.Subject == "Fast" {
			byArea[f.Area] = f
		}
	}
	if m := byArea[AreaMemory]; m.Level != LevelTight || m.Code != CodeMemoryTight || *m.Percent != 92 || *m.SuggestBytes != 16*gib {
		t.Fatalf("memory %+v", m)
	}
	if v := byArea[AreaVolume]; v.Level != LevelTight || v.Code != CodeVolumeFilling || *v.Days != 35 {
		t.Fatalf("volume %+v %v", v, *v.Days)
	}
	if g := byArea[AreaGPU]; g.Level != LevelOK || *g.Bytes != 12.6e9 {
		t.Fatalf("gpu %+v", g)
	}
	if c := byArea[AreaCPU]; c.Level != LevelOK {
		t.Fatalf("cpu %+v", c)
	}
	if tp := byArea[AreaTemperature]; tp.Level != LevelOK {
		t.Fatalf("temps %+v", tp)
	}
	if r.Findings[0].Level != LevelTight {
		t.Fatal("worst first")
	}
	// Predict off: no disks; bottlenecks off: none of the rest.
	in.Disks = []Disk{{Name: "sda", Known: true, SmartFailed: true}}
	if r := Judge(Input{Now: now, Disks: in.Disks}); len(r.Findings) != 0 {
		t.Fatalf("all off %+v", r.Findings)
	}
	if r := Judge(Input{Now: now, Predict: true, Disks: in.Disks, Memory: mem}); len(r.Findings) != 1 || r.Findings[0].Area != AreaDisk {
		t.Fatalf("predict only %+v", r.Findings)
	}
}

func TestCPUAndHeat(t *testing.T) {
	var cpu []CPUSample
	for i := range 24 * 60 {
		p := 30.0
		if i < 8*60 {
			p = 95
		}
		cpu = append(cpu, CPUSample{At: now.Add(-time.Duration(i) * time.Minute), Percent: p})
	}
	f, ok := judgeCPU(cpu, now)
	if !ok || f.Level != LevelTight || f.Code != CodeCPUBusy || *f.Hours < 6 {
		t.Fatalf("cpu %+v", f)
	}
	if _, ok := judgeCPU(nil, now); ok {
		t.Fatal("no samples, no finding")
	}
	disks := []Disk{{Name: "sda", Kind: "hdd", Known: true, Current: Sample{TemperatureC: pi(57)}}, {Name: "nvme0n1", Kind: "nvme", Known: true, Current: Sample{TemperatureC: pi(60)}}}
	tf, _ := judgeTemps(nil, disks)
	if tf.Level != LevelTight || tf.Label != "sda" || *tf.Celsius != 57 {
		t.Fatalf("temps %+v", tf)
	}
	// Swap in use most of the time.
	var mem []MemorySample
	for range 10 {
		mem = append(mem, MemorySample{Total: 8 << 30, Available: 4 << 30, SwapTotal: 4 << 30, SwapFree: 1 << 30})
	}
	if m, _ := judgeMemory(mem); m.Code != CodeMemorySwap {
		t.Fatalf("swap %+v", m)
	}
	// GPU full, reported without Ollama.
	total, used := 8e9, 7.9e9
	if g, _ := judgeGPU(GPU{Name: "x", Total: &total, UsedBytes: &used}); g.Code != CodeGPUFull {
		t.Fatalf("gpu %+v", g)
	}
	// A GPU without memory figures says nothing (never zero).
	if _, ok := judgeGPU(GPU{Name: "y"}); ok {
		t.Fatal("unknown GPU memory must not be judged")
	}
	if v := judgeVolume(Volume{Name: "x", Size: 100, Used: 90}); v.Code != CodeVolumeHigh {
		t.Fatalf("volume %+v", v)
	}
}
