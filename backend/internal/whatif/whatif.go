// Package whatif plays through what would happen to the NAS (PLAN 54-V,
// "Was-wäre-wenn"): a disk fails, a volume runs full, another RAID level.
// It only computes on a model of the NAS that the caller builds from
// values already in RAM (storage status, cached SMART values, cached backup
// states); it never changes anything and never touches a disk.
package whatif

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
)

// Kinds of a scenario.
const (
	KindDisk  = "disk"  // a disk fails
	KindFull  = "full"  // a volume runs full
	KindLevel = "level" // what another RAID level would bring
)

// Outcomes of a disk failure.
const (
	OutcomeRedundant     = "redundant"     // still a copy left
	OutcomeNoRedundancy  = "noRedundancy"  // readable, but no further disk may fail
	OutcomeLost          = "lost"          // the volume is gone; restore from backup
	OutcomePossiblyLost  = "possiblyLost"  // RAID 10: depends on which mirror pair
	OutcomeNotMember     = "notMember"     // the disk holds no volume
	OutcomeAlreadyFailed = "alreadyFailed" // the disk has failed already
	OutcomeSpare         = "spare"         // a spare: nothing changes but the spare is gone
	OutcomeCacheLost     = "cacheLost"     // write cache: what was not written back is lost
	OutcomeCacheNoLoss   = "cacheNoLoss"   // read cache: slower, nothing lost
)

// Risk levels.
const (
	RiskNormal   = "normal"
	RiskElevated = "elevated"
	RiskHigh     = "high"
)

// Reason codes of the risk during a rebuild.
const (
	ReasonReallocated   = "reallocated"
	ReasonPending       = "pending"
	ReasonUncorrectable = "uncorrectable"
	ReasonSmartFailed   = "smartFailed"
	ReasonOld           = "old"   // ≥ 5 years in service
	ReasonAging         = "aging" // ≥ 3 years in service
	ReasonWear          = "wear"  // SSD ≥ 80 % worn
	ReasonLongRebuild   = "longRebuild"
	ReasonDegraded      = "degraded" // the volume is degraded already
	ReasonUnknownSmart  = "unknownSmart"
)

// Advice codes; the page turns them into links to existing pages.
const (
	AdviceStartBackup  = "startBackup"    // Job: POST /backups/{id}/run
	AdviceCreateBackup = "createBackup"   // Share: /backups
	AdviceReplacement  = "replacement"    // Bytes, Kind: same size or larger
	AdviceRestore      = "restore"        // from the backup
	AdviceMirror       = "mirror"         // a level with redundancy
	AdviceGrow         = "grow"           // add a disk
	AdviceCleanSnaps   = "cleanSnapshots" // Btrfs snapshots hold space
	AdviceCheckUsage   = "checkUsage"     // what takes the space
	AdviceCheckCable   = "checkCable"     // CRC errors: the cable, not the disk
	AdviceWriteBack    = "cacheReadOnly"  // switch the cache to read cache
	AdviceSpare        = "addSpare"
	AdviceAddDisks     = "addDisks"  // Count more disks for a level
	AdviceProfile      = "profile"   // Btrfs: change the profile in place
	AdviceNewVolume    = "newVolume" // mdadm: new volume and copy
)

// Effects of a full volume.
const (
	EffectWritesFail    = "writesFail"
	EffectSnapshotsStop = "snapshotsStop"
	EffectBackupsFail   = "backupsFail"
	EffectAgent         = "agentStops"
)

// Disk is a physical disk with its cached SMART values.
type Disk struct {
	Name        string `json:"name"`
	Model       string `json:"model,omitempty"`
	Serial      string `json:"serial,omitempty"`
	Kind        string `json:"kind"` // hdd, ssd, nvme, usb
	SizeBytes   uint64 `json:"sizeBytes"`
	Volume      string `json:"volume,omitempty"`
	MemberState string `json:"memberState,omitempty"`
	// SmartKnown is false when SMART was never read (asleep since start).
	SmartKnown    bool   `json:"smartKnown"`
	SmartFailed   bool   `json:"smartFailed,omitempty"`
	PowerOnHours  int64  `json:"powerOnHours,omitempty"`
	Reallocated   uint64 `json:"reallocated,omitempty"`
	Pending       uint64 `json:"pending,omitempty"`
	Uncorrectable uint64 `json:"uncorrectable,omitempty"`
	CRC           uint64 `json:"crc,omitempty"`
	// WearPercent of an SSD; -1 unknown.
	WearPercent int `json:"wearPercent"`
}

// Member is a disk of a volume with its state (active, rebuilding, faulty,
// spare, removing).
type Member struct {
	Disk  string `json:"disk"`
	State string `json:"state"`
}

// Cache is the SSD cache of a volume; Mode writeback or writethrough.
type Cache struct {
	Disk string `json:"disk"`
	Mode string `json:"mode"`
}

// Volume is a configured volume. For Btrfs Level is the profile (single,
// raid1, raid1c3).
type Volume struct {
	Name       string   `json:"name"`
	Level      string   `json:"level"`
	Filesystem string   `json:"filesystem"`
	State      string   `json:"state"`
	Mounted    bool     `json:"mounted"`
	SizeBytes  uint64   `json:"sizeBytes"`
	UsedBytes  uint64   `json:"usedBytes"`
	RaidDisks  int      `json:"raidDisks"`
	Members    []Member `json:"members"`
	Cache      *Cache   `json:"cache,omitempty"`
	// SyncSpeed is the rebuild preset (gentle, normal, fast); empty normal.
	SyncSpeed string `json:"syncSpeed,omitempty"`
	// MeasuredBps is the last sync speed seen on this volume (per disk);
	// 0 when none was seen.
	MeasuredBps uint64 `json:"measuredBps,omitempty"`
}

// Share is a share and the volume it lies on.
type Share struct {
	Name   string `json:"name"`
	Volume string `json:"volume"`
}

// Backup is a backup job as far as it protects shares against the loss of
// a volume.
type Backup struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	// Known is false when the state of the job is not in RAM; then
	// nothing is said about its last run.
	Known   bool     `json:"known"`
	Shares  []string `json:"shares,omitempty"`
	Volumes []string `json:"volumes,omitempty"` // whole volumes
	// DestVolume is set when the backup lies on a volume of this NAS.
	DestVolume string     `json:"destVolume,omitempty"`
	DestKind   string     `json:"destKind,omitempty"`
	LastOK     *time.Time `json:"lastOK,omitempty"`
	Bytes      uint64     `json:"bytes,omitempty"`
}

// Model is the picture of the NAS a scenario is played on.
type Model struct {
	Now     time.Time `json:"now"`
	Disks   []Disk    `json:"disks"`
	Volumes []Volume  `json:"volumes"`
	Shares  []Share   `json:"shares"`
	Backups []Backup  `json:"backups"`
	// Growth is the measured daily growth per volume in bytes, when known.
	Growth map[string]float64 `json:"growth,omitempty"`
	// AgentVolume holds the agent's data.
	AgentVolume string `json:"agentVolume,omitempty"`
	// MaxBackupAge is how old the last backup may be before it counts as
	// a gap; 0 means 7 days.
	MaxBackupAge time.Duration `json:"-"`
}

// Scenario is what is played through.
type Scenario struct {
	Kind   string `json:"kind"`
	Disk   string `json:"disk,omitempty"`
	Volume string `json:"volume,omitempty"`
	// Level is the target of KindLevel: raid1 ("Spiegel"), raid5, raid6,
	// raid10, raid1c3.
	Level string `json:"level,omitempty"`
}

// Rebuild is the estimated rebuild after replacing a disk.
type Rebuild struct {
	Seconds int64  `json:"seconds"`
	Bytes   uint64 `json:"bytes"`
	Bps     uint64 `json:"bps"`
	// Measured: the speed was seen on this volume; else estimated.
	Measured bool `json:"measured"`
	// Auto: a spare is there, the rebuild starts by itself.
	Auto bool `json:"auto,omitempty"`
}

// Reason is one reason for the risk.
type Reason struct {
	Code  string `json:"code"`
	Disk  string `json:"disk,omitempty"`
	Value int64  `json:"value,omitempty"`
}

// Risk is the risk while the volume has less redundancy.
type Risk struct {
	Level   string   `json:"level"`
	Reasons []Reason `json:"reasons"`
}

// Gap is a share whose data would be lost without a recent backup.
type Gap struct {
	Share string `json:"share"`
	// Never: no backup job covers it (outside this volume).
	Never bool `json:"never,omitempty"`
	// Unknown: a job covers it but its state is not known right now.
	Unknown bool `json:"unknown,omitempty"`
	Days    int  `json:"days,omitempty"`
	// NotYet: the job never succeeded.
	NotYet bool   `json:"notYet,omitempty"`
	Job    string `json:"job,omitempty"`
	Bytes  uint64 `json:"bytes,omitempty"`
}

// Safe is a share with a recent backup outside the volume.
type Safe struct {
	Share string    `json:"share"`
	Job   string    `json:"job"`
	At    time.Time `json:"at"`
}

// Advice is something the person may do; the page links it.
type Advice struct {
	Code   string `json:"code"`
	Share  string `json:"share,omitempty"`
	Job    string `json:"job,omitempty"`
	Volume string `json:"volume,omitempty"`
	Disk   string `json:"disk,omitempty"`
	Bytes  uint64 `json:"bytes,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Count  int    `json:"count,omitempty"`
	Level  string `json:"level,omitempty"`
}

// Full is the outcome of a full volume.
type Full struct {
	FreeBytes  uint64   `json:"freeBytes"`
	UsedPct    float64  `json:"usedPct"`
	DailyBytes float64  `json:"dailyBytes,omitempty"`
	DaysToFull *float64 `json:"daysToFull,omitempty"`
	Effects    []string `json:"effects"`
}

// LevelFacts are what a level gives with the volume's disks.
type LevelFacts struct {
	Level       string `json:"level"`
	Disks       int    `json:"disks"`
	Tolerates   int    `json:"tolerates"`
	UsableBytes uint64 `json:"usableBytes"`
}

// Compare is the outcome of KindLevel.
type Compare struct {
	From      LevelFacts `json:"from"`
	To        LevelFacts `json:"to"`
	NeedDisks int        `json:"needDisks"`
	// UsedBytes is what the volume holds now.
	UsedBytes uint64 `json:"usedBytes"`
	// Fits: the used data fits the new level.
	Fits bool `json:"fits"`
	// Way is profile (Btrfs, in place) or newVolume (new volume, copy).
	Way string `json:"way"`
}

// Result is what a scenario leads to. Nothing on the NAS was changed.
type Result struct {
	Scenario   Scenario `json:"scenario"`
	Volume     string   `json:"volume,omitempty"`
	Level      string   `json:"level,omitempty"`
	Filesystem string   `json:"filesystem,omitempty"`
	Disks      int      `json:"disks,omitempty"`
	Disk       string   `json:"disk,omitempty"`
	Serial     string   `json:"serial,omitempty"`
	Outcome    string   `json:"outcome,omitempty"`
	// Failed is how many disks of the volume are missing already.
	Failed          int      `json:"failed,omitempty"`
	DataAtRiskBytes uint64   `json:"dataAtRiskBytes"`
	Rebuild         *Rebuild `json:"rebuild,omitempty"`
	Risk            *Risk    `json:"risk,omitempty"`
	Gaps            []Gap    `json:"gaps"`
	Safe            []Safe   `json:"safe"`
	Advice          []Advice `json:"advice"`
	Full            *Full    `json:"full,omitempty"`
	Compare         *Compare `json:"compare,omitempty"`
}

// Errors of Run.
var (
	ErrScenario = errors.New("whatif: unknown scenario")
	ErrNoDisk   = errors.New("whatif: no such disk")
	ErrNoVolume = errors.New("whatif: no such volume")
	ErrLevel    = errors.New("whatif: unknown level")
)

// Run plays a scenario through on the model.
func Run(m Model, sc Scenario) (Result, error) {
	switch sc.Kind {
	case KindDisk:
		return diskFails(m, sc)
	case KindFull:
		return volumeFull(m, sc)
	case KindLevel:
		return otherLevel(m, sc)
	}
	return Result{}, ErrScenario
}

func (m Model) disk(name string) (Disk, bool) {
	for _, d := range m.Disks {
		if d.Name == name {
			return d, true
		}
	}
	return Disk{}, false
}

func (m Model) volume(name string) (Volume, bool) {
	for _, v := range m.Volumes {
		if v.Name == name {
			return v, true
		}
	}
	return Volume{}, false
}

func isBtrfs(v Volume) bool { return v.Filesystem == "btrfs" }

// Tolerates is how many disks of a level may fail without losing data;
// RAID 10 only guarantees one (it depends on the mirror pairs).
func Tolerates(level string, disks int) int {
	switch level {
	case "raid1":
		if disks < 2 {
			return 0
		}
		return disks - 1
	case "raid5", "raid10":
		return 1
	case "raid6", "raid1c3":
		return 2
	}
	return 0 // raid0, single
}

// btrfsTolerates: a Btrfs raid1 keeps two copies, whatever the disk count.
func tolerates(v Volume) int {
	n := v.RaidDisks
	if n == 0 {
		n = len(active(v)) + failedCount(v)
	}
	if isBtrfs(v) && v.Level == "raid1" {
		return min(1, n-1)
	}
	return Tolerates(v.Level, n)
}

// active are the members that hold their full share of the data.
func active(v Volume) []Member {
	var out []Member
	for _, mb := range v.Members {
		if mb.State == "active" || mb.State == "removing" || mb.State == "" {
			out = append(out, mb)
		}
	}
	return out
}

// failedCount is how many disks of the volume are missing or not yet
// rebuilt.
func failedCount(v Volume) int {
	if v.RaidDisks == 0 {
		n := 0
		for _, mb := range v.Members {
			if mb.State == "faulty" || mb.State == "rebuilding" {
				n++
			}
		}
		return n
	}
	return max(0, v.RaidDisks-len(active(v)))
}

func hasSpare(v Volume) bool {
	return slices.ContainsFunc(v.Members, func(mb Member) bool { return mb.State == "spare" })
}

func diskFails(m Model, sc Scenario) (Result, error) {
	d, ok := m.disk(sc.Disk)
	if !ok {
		return Result{}, ErrNoDisk
	}
	r := Result{Scenario: sc, Disk: d.Name, Serial: d.Serial, Gaps: []Gap{}, Safe: []Safe{}, Advice: []Advice{}}
	if d.Volume == "" {
		r.Outcome = OutcomeNotMember
		return r, nil
	}
	v, ok := m.volume(d.Volume)
	if !ok {
		r.Outcome = OutcomeNotMember
		return r, nil
	}
	r.Volume, r.Level, r.Filesystem, r.Disks = v.Name, v.Level, v.Filesystem, v.RaidDisks
	r.Failed = failedCount(v)
	// The cache SSD is not a member.
	if v.Cache != nil && v.Cache.Disk == d.Name && !slices.ContainsFunc(v.Members, func(mb Member) bool { return mb.Disk == d.Name }) {
		if v.Cache.Mode == "writeback" {
			r.Outcome = OutcomeCacheLost
			r.Advice = append(r.Advice, Advice{Code: AdviceWriteBack, Volume: v.Name})
			gaps(m, v, &r)
		} else {
			r.Outcome = OutcomeCacheNoLoss
		}
		return r, nil
	}
	state := d.MemberState
	for _, mb := range v.Members {
		if mb.Disk == d.Name {
			state = mb.State
		}
	}
	switch state {
	case "faulty":
		r.Outcome = OutcomeAlreadyFailed
		r.Advice = append(r.Advice, Advice{Code: AdviceReplacement, Disk: d.Name, Bytes: d.SizeBytes, Kind: d.Kind})
		gaps(m, v, &r)
		return r, nil
	case "spare":
		r.Outcome = OutcomeSpare
		return r, nil
	}
	left := tolerates(v) - r.Failed - 1
	used := v.UsedBytes
	if used == 0 || !v.Mounted {
		used = v.SizeBytes
	}
	switch {
	case v.Level == "raid10" && left < 0 && r.Failed > 0:
		r.Outcome = OutcomePossiblyLost
		r.DataAtRiskBytes = used
	case left < 0:
		r.Outcome = OutcomeLost
		r.DataAtRiskBytes = used
	case left == 0:
		r.Outcome = OutcomeNoRedundancy
		r.DataAtRiskBytes = used
	default:
		r.Outcome = OutcomeRedundant
	}
	if r.Outcome != OutcomeLost {
		r.Rebuild = rebuild(m, v, d)
		r.Risk = risk(m, v, d, r.Rebuild, left)
	}
	gaps(m, v, &r)
	switch r.Outcome {
	case OutcomeLost:
		r.Advice = append(r.Advice, Advice{Code: AdviceRestore, Volume: v.Name})
		if tolerates(v) == 0 {
			r.Advice = append(r.Advice, Advice{Code: AdviceMirror, Volume: v.Name})
		}
	case OutcomePossiblyLost, OutcomeNoRedundancy:
		r.Advice = append(r.Advice, Advice{Code: AdviceReplacement, Disk: d.Name, Bytes: d.SizeBytes, Kind: d.Kind})
		if !hasSpare(v) && !isBtrfs(v) && v.Level != "raid0" {
			r.Advice = append(r.Advice, Advice{Code: AdviceSpare, Volume: v.Name})
		}
	case OutcomeRedundant:
		if r.Risk != nil && r.Risk.Level != RiskNormal {
			r.Advice = append(r.Advice, Advice{Code: AdviceReplacement, Disk: d.Name, Bytes: d.SizeBytes, Kind: d.Kind})
		}
	}
	if d.CRC > 0 {
		r.Advice = append(r.Advice, Advice{Code: AdviceCheckCable, Disk: d.Name})
	}
	return r, nil
}

// Estimated rebuild speeds per disk in bytes per second.
const (
	mb             = 1000 * 1000
	speedGentle    = 30 * mb
	speedHDD       = 150 * mb
	speedSSD       = 400 * mb
	speedNVMe      = 800 * mb
	speedBtrfsHDD  = 100 * mb
	longRebuildSec = 24 * 3600
)

func kindSpeed(kind string) uint64 {
	switch kind {
	case "nvme":
		return speedNVMe
	case "ssd":
		return speedSSD
	}
	return speedHDD
}

// rebuild estimates how long filling a new disk takes.
func rebuild(m Model, v Volume, failed Disk) *Rebuild {
	if tolerates(v) == 0 {
		return nil
	}
	r := &Rebuild{Auto: hasSpare(v)}
	// The slowest disk sets the pace.
	speed := kindSpeed(failed.Kind)
	for _, mb := range active(v) {
		if d, ok := m.disk(mb.Disk); ok && mb.Disk != failed.Name {
			speed = min(speed, kindSpeed(d.Kind))
		}
	}
	if isBtrfs(v) {
		// Btrfs copies only the used data of the missing disk.
		n := max(1, v.RaidDisks)
		copies := uint64(2)
		if v.Level == "raid1c3" {
			copies = 3
		}
		r.Bytes = v.UsedBytes * copies / uint64(n)
		if speed == speedHDD {
			speed = speedBtrfsHDD
		}
	} else {
		r.Bytes = failed.SizeBytes
		if v.SyncSpeed == "gentle" {
			speed = min(speed, speedGentle)
		}
	}
	if v.MeasuredBps > 0 {
		speed, r.Measured = v.MeasuredBps, true
	}
	r.Bps = speed
	if speed > 0 {
		r.Seconds = int64(math.Ceil(float64(r.Bytes) / float64(speed)))
	}
	return r
}

const (
	hoursYear = 24 * 365
	hoursOld  = 5 * hoursYear
	hoursAged = 3 * hoursYear
)

// DiskReasons are the SMART reasons a disk may fail soon.
func DiskReasons(d Disk) []Reason {
	if !d.SmartKnown {
		return []Reason{{Code: ReasonUnknownSmart, Disk: d.Name}}
	}
	var out []Reason
	add := func(code string, value int64) { out = append(out, Reason{Code: code, Disk: d.Name, Value: value}) }
	if d.SmartFailed {
		add(ReasonSmartFailed, 0)
	}
	if d.Reallocated > 0 {
		add(ReasonReallocated, int64(d.Reallocated))
	}
	if d.Pending > 0 {
		add(ReasonPending, int64(d.Pending))
	}
	if d.Uncorrectable > 0 {
		add(ReasonUncorrectable, int64(d.Uncorrectable))
	}
	if d.WearPercent >= 80 {
		add(ReasonWear, int64(d.WearPercent))
	}
	switch {
	case d.PowerOnHours >= hoursOld:
		add(ReasonOld, d.PowerOnHours)
	case d.PowerOnHours >= hoursAged:
		add(ReasonAging, d.PowerOnHours)
	}
	return out
}

// risk judges the disks that must hold out while the volume rebuilds.
func risk(m Model, v Volume, failed Disk, rb *Rebuild, left int) *Risk {
	r := &Risk{Level: RiskNormal, Reasons: []Reason{}}
	serious := false
	for _, mb := range active(v) {
		if mb.Disk == failed.Name {
			continue
		}
		d, ok := m.disk(mb.Disk)
		if !ok {
			continue
		}
		for _, reason := range DiskReasons(d) {
			r.Reasons = append(r.Reasons, reason)
			switch reason.Code {
			case ReasonSmartFailed, ReasonPending, ReasonUncorrectable, ReasonOld, ReasonReallocated:
				serious = true
			}
		}
	}
	if failedCount(v) > 0 {
		r.Reasons = append(r.Reasons, Reason{Code: ReasonDegraded, Value: int64(failedCount(v))})
		serious = true
	}
	if rb != nil && rb.Seconds > longRebuildSec {
		r.Reasons = append(r.Reasons, Reason{Code: ReasonLongRebuild, Value: rb.Seconds})
	}
	// Unknown SMART alone says nothing about the risk.
	known := slices.ContainsFunc(r.Reasons, func(x Reason) bool { return x.Code != ReasonUnknownSmart })
	switch {
	case serious && left <= 0:
		r.Level = RiskHigh
	case known:
		r.Level = RiskElevated
	}
	return r
}

// covers reports whether a job saves the share (or the volume) and lies
// outside the volume, so that it survives its loss.
func covers(b Backup, share Share) bool {
	if !b.Enabled || b.DestVolume == share.Volume {
		return false
	}
	return slices.Contains(b.Shares, share.Name) || slices.Contains(b.Volumes, share.Volume)
}

func (m Model) maxAge() time.Duration {
	if m.MaxBackupAge > 0 {
		return m.MaxBackupAge
	}
	return 7 * 24 * time.Hour
}

// gaps sorts the shares of a volume into gaps and safe ones.
func gaps(m Model, v Volume, r *Result) {
	for _, share := range m.Shares {
		if share.Volume != v.Name {
			continue
		}
		var best *Backup
		unknown, any := "", ""
		for i := range m.Backups {
			b := &m.Backups[i]
			if !covers(*b, share) {
				continue
			}
			any = b.ID
			if !b.Known {
				unknown = b.ID
				continue
			}
			if b.LastOK != nil && (best == nil || best.LastOK == nil || b.LastOK.After(*best.LastOK)) {
				best = b
			}
		}
		switch {
		case best != nil && m.Now.Sub(*best.LastOK) <= m.maxAge():
			r.Safe = append(r.Safe, Safe{Share: share.Name, Job: best.ID, At: *best.LastOK})
		case best != nil:
			days := int(m.Now.Sub(*best.LastOK).Hours() / 24)
			r.Gaps = append(r.Gaps, Gap{Share: share.Name, Days: days, Job: best.ID, Bytes: best.Bytes})
		case unknown != "":
			r.Gaps = append(r.Gaps, Gap{Share: share.Name, Unknown: true, Job: unknown})
		case any != "":
			r.Gaps = append(r.Gaps, Gap{Share: share.Name, NotYet: true, Job: any})
		default:
			r.Gaps = append(r.Gaps, Gap{Share: share.Name, Never: true})
		}
	}
	sort.SliceStable(r.Gaps, func(i, j int) bool { return r.Gaps[i].Share < r.Gaps[j].Share })
	sort.SliceStable(r.Safe, func(i, j int) bool { return r.Safe[i].Share < r.Safe[j].Share })
	for _, g := range r.Gaps {
		if g.Unknown {
			continue
		}
		if g.Job != "" {
			if !slices.ContainsFunc(r.Advice, func(a Advice) bool { return a.Code == AdviceStartBackup && a.Job == g.Job }) {
				r.Advice = append(r.Advice, Advice{Code: AdviceStartBackup, Job: g.Job, Share: g.Share})
			}
		} else {
			r.Advice = append(r.Advice, Advice{Code: AdviceCreateBackup, Share: g.Share})
		}
	}
}

func volumeFull(m Model, sc Scenario) (Result, error) {
	v, ok := m.volume(sc.Volume)
	if !ok {
		return Result{}, ErrNoVolume
	}
	r := Result{Scenario: sc, Volume: v.Name, Level: v.Level, Filesystem: v.Filesystem, Disks: v.RaidDisks, Gaps: []Gap{}, Safe: []Safe{}, Advice: []Advice{}}
	f := &Full{Effects: []string{EffectWritesFail}}
	if v.SizeBytes > 0 {
		f.FreeBytes = v.SizeBytes - min(v.UsedBytes, v.SizeBytes)
		f.UsedPct = math.Round(float64(v.UsedBytes)/float64(v.SizeBytes)*1000) / 10
	}
	if g, ok := m.Growth[v.Name]; ok {
		f.DailyBytes = g
		if g > 0 {
			days := float64(f.FreeBytes) / g
			f.DaysToFull = &days
		}
	}
	if isBtrfs(v) {
		f.Effects = append(f.Effects, EffectSnapshotsStop)
	}
	for _, b := range m.Backups {
		if b.Enabled && b.DestVolume == v.Name {
			f.Effects = append(f.Effects, EffectBackupsFail)
			break
		}
	}
	if m.AgentVolume == v.Name {
		f.Effects = append(f.Effects, EffectAgent)
	}
	r.Full = f
	r.Advice = append(r.Advice, Advice{Code: AdviceCheckUsage, Volume: v.Name})
	if isBtrfs(v) {
		r.Advice = append(r.Advice, Advice{Code: AdviceCleanSnaps, Volume: v.Name})
	}
	if v.Level != "raid0" || isBtrfs(v) {
		r.Advice = append(r.Advice, Advice{Code: AdviceGrow, Volume: v.Name})
	}
	return r, nil
}

// Usable is the space of a level on disks of these sizes.
func Usable(level string, btrfs bool, sizes []uint64) uint64 {
	n := uint64(len(sizes))
	if n == 0 {
		return 0
	}
	smallest, sum := sizes[0], uint64(0)
	for _, s := range sizes {
		smallest = min(smallest, s)
		sum += s
	}
	if btrfs {
		largest := slices.Max(sizes)
		switch level {
		case "single":
			return sum
		case "raid1":
			return min(sum/2, sum-largest)
		case "raid1c3":
			if n < 3 {
				return 0
			}
			return sum / 3
		}
	}
	switch level {
	case "raid0":
		return n * smallest
	case "raid1":
		return smallest
	case "raid5":
		if n < 3 {
			return 0
		}
		return (n - 1) * smallest
	case "raid6":
		if n < 4 {
			return 0
		}
		return (n - 2) * smallest
	case "raid10":
		if n < 4 {
			return 0
		}
		return n / 2 * smallest
	case "raid1c3":
		if n < 3 {
			return 0
		}
		return sum / 3
	}
	return 0
}

// MinDisks is the smallest number of disks of a level.
func MinDisks(level string) int {
	switch level {
	case "raid5", "raid1c3":
		return 3
	case "raid6", "raid10":
		return 4
	case "single":
		return 1
	}
	return 2
}

var levels = []string{"raid0", "raid1", "raid5", "raid6", "raid10", "raid1c3", "single"}

func otherLevel(m Model, sc Scenario) (Result, error) {
	v, ok := m.volume(sc.Volume)
	if !ok {
		return Result{}, ErrNoVolume
	}
	target := strings.ToLower(sc.Level)
	if target == "mirror" {
		target = "raid1"
	}
	if !slices.Contains(levels, target) {
		return Result{}, ErrLevel
	}
	r := Result{Scenario: sc, Volume: v.Name, Level: v.Level, Filesystem: v.Filesystem, Disks: v.RaidDisks, Gaps: []Gap{}, Safe: []Safe{}, Advice: []Advice{}}
	var sizes []uint64
	for _, mb := range v.Members {
		if mb.State == "spare" {
			continue
		}
		if d, ok := m.disk(mb.Disk); ok {
			sizes = append(sizes, d.SizeBytes)
		}
	}
	n := max(len(sizes), v.RaidDisks)
	// Missing disks count with the size of the smallest one there.
	for len(sizes) < n && len(sizes) > 0 {
		sizes = append(sizes, slices.Min(sizes))
	}
	btrfsTarget := isBtrfs(v) && slices.Contains([]string{"single", "raid1", "raid1c3"}, target)
	c := &Compare{
		From: LevelFacts{Level: v.Level, Disks: n, Tolerates: tolerates(v), UsableBytes: Usable(v.Level, isBtrfs(v), sizes)},
		To:   LevelFacts{Level: target, Disks: max(n, MinDisks(target))},
	}
	c.NeedDisks = max(0, MinDisks(target)-n)
	c.UsedBytes = v.UsedBytes
	toSizes := slices.Clone(sizes)
	for i := 0; i < c.NeedDisks && len(sizes) > 0; i++ {
		toSizes = append(toSizes, slices.Min(sizes))
	}
	c.To.UsableBytes = Usable(target, btrfsTarget, toSizes)
	c.To.Tolerates = Tolerates(target, c.To.Disks)
	if btrfsTarget && target == "raid1" {
		c.To.Tolerates = 1
	}
	c.Fits = c.To.UsableBytes >= v.UsedBytes
	if btrfsTarget {
		c.Way = AdviceProfile
	} else {
		c.Way = AdviceNewVolume
	}
	r.Compare = c
	if c.NeedDisks > 0 {
		r.Advice = append(r.Advice, Advice{Code: AdviceAddDisks, Volume: v.Name, Count: c.NeedDisks, Level: target})
	}
	r.Advice = append(r.Advice, Advice{Code: c.Way, Volume: v.Name, Level: target})
	if c.Way == AdviceNewVolume {
		gaps(m, v, &r)
	}
	return r, nil
}

// Summary is a short English text of a result, for the agent.
func Summary(r Result) string {
	var b strings.Builder
	tb := func(n uint64) string { return fmt.Sprintf("%.1f TB", float64(n)/1e12) }
	switch r.Scenario.Kind {
	case KindDisk:
		fmt.Fprintf(&b, "If disk %s fails: ", r.Disk)
		switch r.Outcome {
		case OutcomeNotMember:
			b.WriteString("it holds no volume, nothing is lost.")
		case OutcomeAlreadyFailed:
			fmt.Fprintf(&b, "it has failed already; volume %s waits for a replacement.", r.Volume)
		case OutcomeSpare:
			fmt.Fprintf(&b, "it is the spare of %s; nothing is lost, but the spare is gone.", r.Volume)
		case OutcomeCacheLost:
			fmt.Fprintf(&b, "it is the read/write cache of %s; data not yet written back is lost.", r.Volume)
		case OutcomeCacheNoLoss:
			fmt.Fprintf(&b, "it is the read cache of %s; it gets slower, nothing is lost.", r.Volume)
		case OutcomeRedundant:
			fmt.Fprintf(&b, "volume %s (%s) keeps a copy of all data.", r.Volume, r.Level)
		case OutcomeNoRedundancy:
			fmt.Fprintf(&b, "volume %s (%s) stays readable but without redundancy; %s depend on the remaining disks.", r.Volume, r.Level, tb(r.DataAtRiskBytes))
		case OutcomePossiblyLost:
			fmt.Fprintf(&b, "volume %s (RAID 10) may be lost, depending on the mirror pair (%s).", r.Volume, tb(r.DataAtRiskBytes))
		case OutcomeLost:
			fmt.Fprintf(&b, "volume %s (%s) is lost (%s); only backups bring it back.", r.Volume, r.Level, tb(r.DataAtRiskBytes))
		}
		if r.Rebuild != nil {
			how := "estimated"
			if r.Rebuild.Measured {
				how = "measured"
			}
			fmt.Fprintf(&b, " Rebuild about %.1f hours (%s %d MB/s).", float64(r.Rebuild.Seconds)/3600, how, r.Rebuild.Bps/mb)
		}
		if r.Risk != nil && r.Risk.Level != RiskNormal {
			fmt.Fprintf(&b, " Risk meanwhile: %s (", r.Risk.Level)
			for i, x := range r.Risk.Reasons {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "%s %s", x.Disk, x.Code)
			}
			b.WriteString(").")
		}
	case KindFull:
		f := r.Full
		fmt.Fprintf(&b, "Volume %s is %.1f %% used, %s free.", r.Volume, f.UsedPct, tb(f.FreeBytes))
		if f.DaysToFull != nil {
			fmt.Fprintf(&b, " At the measured growth it is full in about %.0f days.", *f.DaysToFull)
		}
		fmt.Fprintf(&b, " When full: %s.", strings.Join(f.Effects, ", "))
	case KindLevel:
		c := r.Compare
		fmt.Fprintf(&b, "Volume %s: %s with %d disks tolerates %d failed disks and holds %s; %s would tolerate %d and hold %s",
			r.Volume, c.From.Level, c.From.Disks, c.From.Tolerates, tb(c.From.UsableBytes), c.To.Level, c.To.Tolerates, tb(c.To.UsableBytes))
		if c.NeedDisks > 0 {
			fmt.Fprintf(&b, " with %d more disk(s)", c.NeedDisks)
		}
		fmt.Fprintf(&b, ". Way: %s.", c.Way)
		if !c.Fits {
			b.WriteString(" The used data would not fit.")
		}
	}
	for _, g := range r.Gaps {
		switch {
		case g.Never:
			fmt.Fprintf(&b, " Share %s has no backup outside the volume.", g.Share)
		case g.Unknown:
			fmt.Fprintf(&b, " State of the backup of %s is unknown right now.", g.Share)
		case g.NotYet:
			fmt.Fprintf(&b, " Backup %s of %s never succeeded.", g.Job, g.Share)
		default:
			fmt.Fprintf(&b, " Share %s: last backup %d days ago.", g.Share, g.Days)
		}
	}
	if len(r.Safe) > 0 {
		names := make([]string, 0, len(r.Safe))
		for _, s := range r.Safe {
			names = append(names, s.Share)
		}
		fmt.Fprintf(&b, " Backed up recently: %s.", strings.Join(names, ", "))
	}
	b.WriteString(" (Computed only; nothing on the NAS was changed.)")
	return b.String()
}
