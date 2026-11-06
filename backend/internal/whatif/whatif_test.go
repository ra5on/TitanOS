package whatif

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

const tb8 = 8_000_000_000_000

var now = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

func ago(d time.Duration) *time.Time { t := now.Add(-d); return &t }

func healthy(name, volume string) Disk {
	return Disk{Name: name, Kind: "hdd", SizeBytes: tb8, Volume: volume, MemberState: "active", SmartKnown: true, PowerOnHours: 8000, WearPercent: -1}
}

// nas builds a model with one volume of the level on n healthy disks.
func nas(level, fs string, n int, states ...string) Model {
	m := Model{Now: now}
	v := Volume{Name: "Data", Level: level, Filesystem: fs, State: "ok", Mounted: true, SizeBytes: 10 * tb8, UsedBytes: 5_100_000_000_000, RaidDisks: n}
	for i := 0; i < n; i++ {
		name := "sd" + string(rune('a'+i))
		state := "active"
		if i < len(states) {
			state = states[i]
		}
		d := healthy(name, "Data")
		d.MemberState = state
		m.Disks = append(m.Disks, d)
		if state != "missing" {
			v.Members = append(v.Members, Member{Disk: name, State: state})
		}
	}
	m.Volumes = []Volume{v}
	return m
}

func TestDiskFailsByLevel(t *testing.T) {
	cases := []struct {
		name    string
		level   string
		fs      string
		n       int
		states  []string
		disk    string
		outcome string
		atRisk  bool
		rebuild bool
	}{
		{"raid0 loses all", "raid0", "ext4", 2, nil, "sda", OutcomeLost, true, false},
		{"raid1 two disks: no mirror left", "raid1", "ext4", 2, nil, "sdb", OutcomeNoRedundancy, true, true},
		{"raid1 three disks: still a copy", "raid1", "xfs", 3, nil, "sdb", OutcomeRedundant, false, true},
		{"raid5 one disk", "raid5", "ext4", 4, nil, "sdc", OutcomeNoRedundancy, true, true},
		{"raid5 degraded: second disk loses it", "raid5", "ext4", 4, []string{"active", "faulty"}, "sda", OutcomeLost, true, false},
		{"raid5 missing member counts", "raid5", "ext4", 3, []string{"active", "missing"}, "sda", OutcomeLost, true, false},
		{"raid6 one disk keeps one parity", "raid6", "ext4", 5, nil, "sda", OutcomeRedundant, false, true},
		{"raid6 degraded once", "raid6", "ext4", 5, []string{"faulty"}, "sdb", OutcomeNoRedundancy, true, true},
		{"raid10 first disk", "raid10", "ext4", 4, nil, "sda", OutcomeNoRedundancy, true, true},
		{"raid10 second disk: possibly lost", "raid10", "ext4", 4, []string{"faulty"}, "sdc", OutcomePossiblyLost, true, true},
		{"btrfs single loses all", "single", "btrfs", 2, nil, "sda", OutcomeLost, true, false},
		{"btrfs raid1 three disks: only two copies", "raid1", "btrfs", 3, nil, "sda", OutcomeNoRedundancy, true, true},
		{"btrfs raid1c3", "raid1c3", "btrfs", 3, nil, "sda", OutcomeRedundant, false, true},
		{"already faulty", "raid1", "ext4", 2, []string{"active", "faulty"}, "sdb", OutcomeAlreadyFailed, false, false},
		{"spare", "raid5", "ext4", 4, []string{"active", "active", "active", "spare"}, "sdd", OutcomeSpare, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := nas(c.level, c.fs, c.n, c.states...)
			if c.name == "spare" {
				m.Volumes[0].RaidDisks = 3
			}
			r, err := Run(m, Scenario{Kind: KindDisk, Disk: c.disk})
			if err != nil {
				t.Fatal(err)
			}
			if r.Outcome != c.outcome {
				t.Fatalf("outcome %q, want %q (failed %d)", r.Outcome, c.outcome, r.Failed)
			}
			if (r.DataAtRiskBytes > 0) != c.atRisk {
				t.Errorf("data at risk %d, want risk %v", r.DataAtRiskBytes, c.atRisk)
			}
			if (r.Rebuild != nil) != c.rebuild {
				t.Errorf("rebuild %+v, want %v", r.Rebuild, c.rebuild)
			}
			if !strings.Contains(Summary(r), "nothing on the NAS was changed") {
				t.Error("summary must say nothing was changed")
			}
		})
	}
}

func TestRebuildTime(t *testing.T) {
	m := nas("raid1", "ext4", 2)
	r, _ := Run(m, Scenario{Kind: KindDisk, Disk: "sdb"})
	// 8 TB at the estimated 150 MB/s ≈ 14.8 h.
	if r.Rebuild.Measured || r.Rebuild.Bps != speedHDD || r.Rebuild.Seconds != 53334 {
		t.Fatalf("estimate %+v", r.Rebuild)
	}
	m.Volumes[0].MeasuredBps = 205 * mb
	r, _ = Run(m, Scenario{Kind: KindDisk, Disk: "sdb"})
	if !r.Rebuild.Measured || r.Rebuild.Seconds/3600 != 10 {
		t.Fatalf("measured %+v", r.Rebuild)
	}
	m.Volumes[0].MeasuredBps = 0
	m.Volumes[0].SyncSpeed = "gentle"
	r, _ = Run(m, Scenario{Kind: KindDisk, Disk: "sdb"})
	if r.Rebuild.Bps != speedGentle {
		t.Fatalf("gentle %+v", r.Rebuild)
	}
	// Btrfs copies only the used data: raid1 on 2 disks = all used.
	b := nas("raid1", "btrfs", 2)
	r, _ = Run(b, Scenario{Kind: KindDisk, Disk: "sda"})
	if r.Rebuild.Bytes != b.Volumes[0].UsedBytes || r.Rebuild.Bps != speedBtrfsHDD {
		t.Fatalf("btrfs %+v", r.Rebuild)
	}
	// A spare starts by itself.
	s := nas("raid5", "ext4", 4, "active", "active", "active", "spare")
	s.Volumes[0].RaidDisks = 3
	r, _ = Run(s, Scenario{Kind: KindDisk, Disk: "sda"})
	if !r.Rebuild.Auto {
		t.Fatal("spare should rebuild by itself")
	}
}

func TestRiskDuringRebuild(t *testing.T) {
	m := nas("raid1", "ext4", 2)
	r, _ := Run(m, Scenario{Kind: KindDisk, Disk: "sdb"})
	if r.Risk.Level != RiskNormal {
		t.Fatalf("healthy partner: %+v", r.Risk)
	}
	m.Disks[0].Reallocated = 4
	m.Disks[0].PowerOnHours = 4 * hoursYear
	r, _ = Run(m, Scenario{Kind: KindDisk, Disk: "sdb"})
	if r.Risk.Level != RiskHigh {
		t.Fatalf("sda with sectors and no mirror: %+v", r.Risk)
	}
	codes := []string{}
	for _, x := range r.Risk.Reasons {
		codes = append(codes, x.Code)
	}
	if !slices.Contains(codes, ReasonReallocated) || !slices.Contains(codes, ReasonAging) {
		t.Fatalf("reasons %v", codes)
	}
	// With a copy left, only elevated.
	m3 := nas("raid6", "ext4", 5)
	m3.Disks[1].Pending = 2
	r, _ = Run(m3, Scenario{Kind: KindDisk, Disk: "sda"})
	if r.Risk.Level != RiskElevated {
		t.Fatalf("raid6 with pending: %+v", r.Risk)
	}
	// Unknown SMART alone is no risk.
	m.Disks[0] = healthy("sda", "Data")
	m.Disks[0].SmartKnown = false
	r, _ = Run(m, Scenario{Kind: KindDisk, Disk: "sdb"})
	if r.Risk.Level != RiskNormal || r.Risk.Reasons[0].Code != ReasonUnknownSmart {
		t.Fatalf("unknown: %+v", r.Risk)
	}
	// A risky failing disk itself gets a replacement advice.
	m3.Disks[0].CRC = 3
	r, _ = Run(m3, Scenario{Kind: KindDisk, Disk: "sda"})
	if !hasAdvice(r, AdviceReplacement) || !hasAdvice(r, AdviceCheckCable) {
		t.Fatalf("advice %+v", r.Advice)
	}
}

func hasAdvice(r Result, code string) bool {
	return slices.ContainsFunc(r.Advice, func(a Advice) bool { return a.Code == code })
}

func TestBackupGaps(t *testing.T) {
	m := nas("raid1", "ext4", 2)
	m.Volumes = append(m.Volumes, Volume{Name: "Backup", Level: "raid0", Filesystem: "ext4", Mounted: true})
	m.Shares = []Share{{"Documents", "Data"}, {"Fotos", "Data"}, {"Musik", "Data"}, {"Filme", "Data"}, {"Neu", "Data"}, {"Other", "Backup"}}
	m.Backups = []Backup{
		{ID: "docs", Enabled: true, Known: true, Shares: []string{"Documents"}, DestKind: "usb", LastOK: ago(9 * time.Hour)},
		{ID: "fotos", Enabled: true, Known: true, Shares: []string{"Fotos"}, DestKind: "s3", LastOK: ago(23 * 24 * time.Hour), Bytes: 1_900_000_000_000},
		// On the same volume: does not protect.
		{ID: "musik-local", Enabled: true, Known: true, Shares: []string{"Musik"}, DestKind: "local", DestVolume: "Data", LastOK: ago(time.Hour)},
		// Switched off: does not protect.
		{ID: "musik-off", Enabled: false, Known: true, Shares: []string{"Musik"}, LastOK: ago(time.Hour)},
		{ID: "filme", Enabled: true, Known: false, Shares: []string{"Filme"}},
		{ID: "neu", Enabled: true, Known: true, Shares: []string{"Neu"}},
	}
	r, _ := Run(m, Scenario{Kind: KindDisk, Disk: "sda"})
	if len(r.Safe) != 1 || r.Safe[0].Share != "Documents" {
		t.Fatalf("safe %+v", r.Safe)
	}
	want := map[string]func(Gap) bool{
		"Filme": func(g Gap) bool { return g.Unknown },
		"Fotos": func(g Gap) bool { return g.Days == 23 && g.Job == "fotos" && g.Bytes > 0 },
		"Musik": func(g Gap) bool { return g.Never },
		"Neu":   func(g Gap) bool { return g.NotYet },
	}
	if len(r.Gaps) != len(want) {
		t.Fatalf("gaps %+v", r.Gaps)
	}
	for _, g := range r.Gaps {
		if !want[g.Share](g) {
			t.Errorf("gap %+v", g)
		}
	}
	if !slices.ContainsFunc(r.Advice, func(a Advice) bool { return a.Code == AdviceStartBackup && a.Job == "fotos" }) ||
		!slices.ContainsFunc(r.Advice, func(a Advice) bool { return a.Code == AdviceCreateBackup && a.Share == "Musik" }) {
		t.Fatalf("advice %+v", r.Advice)
	}
	// A whole-volume backup covers every share.
	m.Backups = []Backup{{ID: "all", Enabled: true, Known: true, Volumes: []string{"Data"}, LastOK: ago(time.Hour)}}
	r, _ = Run(m, Scenario{Kind: KindDisk, Disk: "sda"})
	if len(r.Gaps) != 0 || len(r.Safe) != 5 {
		t.Fatalf("volume backup: gaps %+v safe %+v", r.Gaps, r.Safe)
	}
}

func TestCacheAndNonMember(t *testing.T) {
	m := nas("raid1", "ext4", 2)
	m.Disks = append(m.Disks, Disk{Name: "nvme0n1", Kind: "nvme", Volume: "Data", SmartKnown: true}, Disk{Name: "sdz", Kind: "hdd"})
	m.Volumes[0].Cache = &Cache{Disk: "nvme0n1", Mode: "writeback"}
	r, _ := Run(m, Scenario{Kind: KindDisk, Disk: "nvme0n1"})
	if r.Outcome != OutcomeCacheLost || !hasAdvice(r, AdviceWriteBack) {
		t.Fatalf("write cache %+v", r)
	}
	m.Volumes[0].Cache.Mode = "writethrough"
	r, _ = Run(m, Scenario{Kind: KindDisk, Disk: "nvme0n1"})
	if r.Outcome != OutcomeCacheNoLoss {
		t.Fatalf("read cache %+v", r)
	}
	r, _ = Run(m, Scenario{Kind: KindDisk, Disk: "sdz"})
	if r.Outcome != OutcomeNotMember || r.DataAtRiskBytes != 0 {
		t.Fatalf("free disk %+v", r)
	}
	if _, err := Run(m, Scenario{Kind: KindDisk, Disk: "nope"}); !errors.Is(err, ErrNoDisk) {
		t.Fatal(err)
	}
	if _, err := Run(m, Scenario{Kind: "boom"}); !errors.Is(err, ErrScenario) {
		t.Fatal(err)
	}
}

func TestVolumeFull(t *testing.T) {
	m := nas("raid1", "btrfs", 2)
	m.Volumes[0].SizeBytes, m.Volumes[0].UsedBytes = 1000, 870
	m.Growth = map[string]float64{"Data": 10}
	m.AgentVolume = "Data"
	m.Backups = []Backup{{ID: "x", Enabled: true, DestVolume: "Data"}}
	r, err := Run(m, Scenario{Kind: KindFull, Volume: "Data"})
	if err != nil {
		t.Fatal(err)
	}
	f := r.Full
	if f.FreeBytes != 130 || f.UsedPct != 87 || f.DaysToFull == nil || *f.DaysToFull != 13 {
		t.Fatalf("full %+v", f)
	}
	for _, e := range []string{EffectWritesFail, EffectSnapshotsStop, EffectBackupsFail, EffectAgent} {
		if !slices.Contains(f.Effects, e) {
			t.Errorf("missing effect %s in %v", e, f.Effects)
		}
	}
	if !hasAdvice(r, AdviceCleanSnaps) || !hasAdvice(r, AdviceGrow) {
		t.Fatalf("advice %+v", r.Advice)
	}
	// Without growth no forecast.
	m.Growth = nil
	r, _ = Run(m, Scenario{Kind: KindFull, Volume: "Data"})
	if r.Full.DaysToFull != nil {
		t.Fatal("no growth, no forecast")
	}
	if _, err := Run(m, Scenario{Kind: KindFull, Volume: "Nope"}); !errors.Is(err, ErrNoVolume) {
		t.Fatal(err)
	}
}

func TestOtherLevel(t *testing.T) {
	cases := []struct {
		name           string
		level, fs, to  string
		n              int
		tolFrom, tolTo int
		need           int
		usableTo       uint64
		way            string
		fits           bool
	}{
		{"raid5 → raid6 on 4 disks", "raid5", "ext4", "raid6", 4, 1, 2, 0, 2 * tb8, AdviceNewVolume, true},
		{"raid1 → raid6 needs 2 disks", "raid1", "ext4", "raid6", 2, 1, 2, 2, 2 * tb8, AdviceNewVolume, true},
		{"raid0 → mirror", "raid0", "ext4", "mirror", 2, 0, 1, 0, tb8, AdviceNewVolume, true},
		{"btrfs raid1 → raid1c3", "raid1", "btrfs", "raid1c3", 2, 1, 2, 1, tb8, AdviceProfile, true},
		{"btrfs single → raid1", "single", "btrfs", "raid1", 2, 0, 1, 0, tb8, AdviceProfile, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := nas(c.level, c.fs, c.n)
			r, err := Run(m, Scenario{Kind: KindLevel, Volume: "Data", Level: c.to})
			if err != nil {
				t.Fatal(err)
			}
			x := r.Compare
			if x.From.Tolerates != c.tolFrom || x.To.Tolerates != c.tolTo || x.NeedDisks != c.need || x.To.UsableBytes != c.usableTo || x.Way != c.way || x.Fits != c.fits {
				t.Fatalf("%+v", x)
			}
			if c.need > 0 && !hasAdvice(r, AdviceAddDisks) {
				t.Fatalf("advice %+v", r.Advice)
			}
		})
	}
	m := nas("raid1", "ext4", 2)
	m.Volumes[0].UsedBytes = 3 * tb8
	r, _ := Run(m, Scenario{Kind: KindLevel, Volume: "Data", Level: "raid1"})
	if r.Compare.Fits {
		t.Fatal("3 TB×8 do not fit a mirror of 8 TB")
	}
	if _, err := Run(m, Scenario{Kind: KindLevel, Volume: "Data", Level: "raid7"}); !errors.Is(err, ErrLevel) {
		t.Fatal(err)
	}
}

func TestUsable(t *testing.T) {
	sizes := []uint64{4, 4, 8}
	cases := map[string]uint64{"raid0": 12, "raid1": 4, "raid5": 8, "raid6": 0, "raid10": 0}
	for level, want := range cases {
		if got := Usable(level, false, sizes); got != want {
			t.Errorf("%s: %d, want %d", level, got, want)
		}
	}
	if got := Usable("raid1", true, sizes); got != 8 {
		t.Errorf("btrfs raid1: %d", got)
	}
	if got := Usable("raid1", true, []uint64{2, 10}); got != 2 {
		t.Errorf("btrfs raid1 uneven: %d", got)
	}
	if got := Usable("single", true, sizes); got != 16 {
		t.Errorf("btrfs single: %d", got)
	}
}
