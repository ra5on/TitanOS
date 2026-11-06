// Package advisor is the hardware adviser of the agent (PLAN 54-V,
// "Hardware-Berater"): from the SMART history of the disks it says which
// disk will probably fail (trends of reallocated and pending sectors, media
// errors, wear, time in service), and from samples of memory, CPU, volumes
// and graphics card where the NAS runs tight. It is pure computation on
// values the caller collected without waking a disk.
package advisor

import (
	"math"
	"sort"
	"time"
)

// Areas of a finding.
const (
	AreaDisk        = "disk"
	AreaMemory      = "memory"
	AreaVolume      = "volume"
	AreaCPU         = "cpu"
	AreaGPU         = "gpu"
	AreaTemperature = "temperature"
)

// Levels of a finding; never shown by colour alone.
const (
	LevelOK    = "ok"
	LevelWatch = "watch" // worth a look
	LevelTight = "tight" // a bottleneck ("eng")
	LevelFail  = "fail"  // a disk will probably fail soon
)

// Codes of a finding; the page words them.
const (
	CodeDiskOK          = "diskOK"
	CodeSmartFailed     = "smartFailed"
	CodeSectorsRising   = "sectorsRising" // reallocated rising
	CodePendingRising   = "pendingRising" // pending sectors rising
	CodeMediaRising     = "mediaRising"   // NVMe media errors rising
	CodeWornOut         = "wornOut"       // wear ≥ 95 % or soon at 100 %
	CodeSectorsStable   = "sectorsStable" // some, but not rising
	CodePending         = "pending"       // pending, not rising
	CodeUncorrectable   = "uncorrectable" // uncorrectable errors
	CodeWear            = "wear"          // ≥ 80 % worn
	CodeOld             = "old"           // ≥ 5 years in service
	CodeCable           = "cable"         // CRC errors rising: cable
	CodeSpareLow        = "spareLow"      // NVMe spare below threshold
	CodeNoSmart         = "noSmart"       // not read (asleep since start)
	CodeMemoryOK        = "memoryOK"      // fine
	CodeMemoryTight     = "memoryTight"   // little available, pressure
	CodeMemorySwap      = "memorySwap"    // swaps
	CodeVolumeOK        = "volumeOK"      // fine
	CodeVolumeFilling   = "volumeFilling" // full in N days
	CodeVolumeFull      = "volumeFull"    // ≥ 95 %
	CodeVolumeHigh      = "volumeHigh"    // ≥ 85 %, growth unknown
	CodeCPUOK           = "cpuOK"         // fine
	CodeCPUBusy         = "cpuBusy"       // sustained high
	CodeGPUOK           = "gpuOK"         // fine
	CodeGPUFull         = "gpuFull"       // VRAM full
	CodeTempsOK         = "tempsOK"       // all fine
	CodeTempHot         = "tempHot"       // a sensor hot
	CodeTempWarm        = "tempWarm"      // a sensor warm
	defaultTrendWindow  = 42 * 24 * time.Hour
	wearSoonDays        = 180
	sustainedCPUPercent = 85.0
	busyCPUPercent      = 60.0
)

// Sample is one day of SMART values of a disk (nil: not reported).
type Sample struct {
	Day            string  `json:"day"`
	PowerOnHours   *int64  `json:"powerOnHours,omitempty"`
	TemperatureC   *int    `json:"temperatureC,omitempty"`
	Reallocated    *uint64 `json:"reallocated,omitempty"`
	Pending        *uint64 `json:"pending,omitempty"`
	Uncorrectable  *uint64 `json:"uncorrectable,omitempty"`
	CRC            *uint64 `json:"crc,omitempty"`
	MediaErrors    *uint64 `json:"mediaErrors,omitempty"`
	PercentageUsed *int    `json:"percentageUsed,omitempty"`
	AvailableSpare *int    `json:"availableSpare,omitempty"`
	SpareThreshold *int    `json:"spareThreshold,omitempty"`
}

// Disk is a disk with its current values and its kept history.
type Disk struct {
	Name   string `json:"name"`
	Model  string `json:"model,omitempty"`
	Serial string `json:"serial,omitempty"`
	Kind   string `json:"kind"`
	// SizeBytes for the replacement suggestion.
	SizeBytes uint64 `json:"sizeBytes"`
	Volume    string `json:"volume,omitempty"`
	// Known: SMART values were read since the start.
	Known       bool     `json:"known"`
	SmartFailed bool     `json:"smartFailed,omitempty"`
	Current     Sample   `json:"current"`
	History     []Sample `json:"history,omitempty"`
}

// MemorySample is memory at one time.
type MemorySample struct {
	At        time.Time `json:"at"`
	Total     uint64    `json:"total"`
	Available uint64    `json:"available"`
	// PressureSome is the PSI "some avg10" in percent.
	PressureSome float64 `json:"pressureSome"`
	SwapTotal    uint64  `json:"swapTotal,omitempty"`
	SwapFree     uint64  `json:"swapFree,omitempty"`
}

// CPUSample is the CPU use at one time, in percent.
type CPUSample struct {
	At      time.Time `json:"at"`
	Percent float64   `json:"percent"`
}

// Volume is a volume with its growth when known.
type Volume struct {
	Name       string   `json:"name"`
	Size       uint64   `json:"size"`
	Used       uint64   `json:"used"`
	DailyBytes *float64 `json:"dailyBytes,omitempty"`
}

// GPU is a graphics card; nil values are not reported, never zero.
type GPU struct {
	Name      string   `json:"name"`
	UsedBytes *float64 `json:"usedBytes,omitempty"`
	Total     *float64 `json:"totalBytes,omitempty"`
	// Ollama is what the agent's models take of it, when known.
	OllamaBytes *int64 `json:"ollamaBytes,omitempty"`
}

// Temperature is one sensor. Disk is set for disks (with its kind).
type Temperature struct {
	Label    string  `json:"label"`
	Celsius  float64 `json:"celsius"`
	DiskKind string  `json:"diskKind,omitempty"`
}

// Input is everything the adviser judges.
type Input struct {
	Now         time.Time      `json:"now"`
	Predict     bool           `json:"predict"`
	Bottlenecks bool           `json:"bottlenecks"`
	Disks       []Disk         `json:"disks"`
	Memory      []MemorySample `json:"memory"`
	CPU         []CPUSample    `json:"cpu"`
	Volumes     []Volume       `json:"volumes"`
	GPUs        []GPU          `json:"gpus"`
	Temps       []Temperature  `json:"temps"`
}

// Finding is one line of the report.
type Finding struct {
	Area    string `json:"area"`
	Subject string `json:"subject"`
	Level   string `json:"level"`
	Code    string `json:"code"`
	// Further codes of the same subject (e.g. old and wear).
	Also []string `json:"also,omitempty"`
	// Values for the words: From/To (counter), Weeks, Hours (in service),
	// Percent, Days, Bytes, TotalBytes, SuggestBytes, Celsius.
	From         *float64 `json:"from,omitempty"`
	To           *float64 `json:"to,omitempty"`
	Weeks        *float64 `json:"weeks,omitempty"`
	Hours        *float64 `json:"hours,omitempty"`
	Percent      *float64 `json:"percent,omitempty"`
	Days         *float64 `json:"days,omitempty"`
	Bytes        *float64 `json:"bytes,omitempty"`
	TotalBytes   *float64 `json:"totalBytes,omitempty"`
	SuggestBytes *float64 `json:"suggestBytes,omitempty"`
	Celsius      *float64 `json:"celsius,omitempty"`
	// Model, Kind and SizeBytes of a disk, for the replacement.
	Model     string `json:"model,omitempty"`
	Kind      string `json:"kind,omitempty"`
	SizeBytes uint64 `json:"sizeBytes,omitempty"`
	Volume    string `json:"volume,omitempty"`
	// Label is the sensor of a temperature.
	Label string `json:"label,omitempty"`
}

// Report is the result of one run, worst first.
type Report struct {
	At       time.Time `json:"at"`
	Findings []Finding `json:"findings"`
}

func f64(v float64) *float64 { return &v }

var rank = map[string]int{LevelFail: 0, LevelTight: 1, LevelWatch: 2, LevelOK: 3}

// Judge makes the report.
func Judge(in Input) Report {
	r := Report{At: in.Now, Findings: []Finding{}}
	if in.Predict {
		for _, d := range in.Disks {
			r.Findings = append(r.Findings, JudgeDisk(d, in.Now))
		}
	}
	if in.Bottlenecks {
		if f, ok := judgeMemory(in.Memory); ok {
			r.Findings = append(r.Findings, f)
		}
		for _, v := range in.Volumes {
			r.Findings = append(r.Findings, judgeVolume(v))
		}
		if f, ok := judgeCPU(in.CPU, in.Now); ok {
			r.Findings = append(r.Findings, f)
		}
		for _, g := range in.GPUs {
			if f, ok := judgeGPU(g); ok {
				r.Findings = append(r.Findings, f)
			}
		}
		if f, ok := judgeTemps(in.Temps, in.Disks); ok {
			r.Findings = append(r.Findings, f)
		}
	}
	sort.SliceStable(r.Findings, func(i, j int) bool { return rank[r.Findings[i].Level] < rank[r.Findings[j].Level] })
	return r
}

func u(p *uint64) (float64, bool) {
	if p == nil {
		return 0, false
	}
	return float64(*p), true
}

// series are the samples of the window plus the current values, oldest
// first.
func series(d Disk, now time.Time) []Sample {
	start := now.Add(-defaultTrendWindow).Format(time.DateOnly)
	var out []Sample
	for _, s := range d.History {
		if s.Day >= start {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Day < out[j].Day })
	cur := d.Current
	if cur.Day == "" {
		cur.Day = now.Format(time.DateOnly)
	}
	if len(out) == 0 || out[len(out)-1].Day <= cur.Day {
		out = append(out, cur)
	}
	return out
}

// rise is the first and the last value of a counter in the series.
func rise(s []Sample, get func(Sample) *uint64) (from, to float64, ok bool) {
	first := true
	for _, x := range s {
		v, has := u(get(x))
		if !has {
			continue
		}
		if first {
			from, first = v, false
		}
		to, ok = v, true
	}
	return
}

func weeksOf(s []Sample) float64 {
	if len(s) < 2 {
		return 0
	}
	a, errA := time.Parse(time.DateOnly, s[0].Day)
	b, errB := time.Parse(time.DateOnly, s[len(s)-1].Day)
	if errA != nil || errB != nil {
		return 0
	}
	return math.Round(b.Sub(a).Hours()/24/7*10) / 10
}

// wearDaysLeft fits the wear of the series and says when it reaches 100 %.
func wearDaysLeft(s []Sample) (float64, bool) {
	var xs, ys []float64
	for _, x := range s {
		if x.PercentageUsed == nil {
			continue
		}
		t, err := time.Parse(time.DateOnly, x.Day)
		if err != nil {
			continue
		}
		xs = append(xs, float64(t.Unix())/86400)
		ys = append(ys, float64(*x.PercentageUsed))
	}
	if len(xs) < 7 {
		return 0, false
	}
	slope := slopeOf(xs, ys)
	if slope <= 0 {
		return 0, false
	}
	return math.Max(0, (100-ys[len(ys)-1])/slope), true
}

func slopeOf(xs, ys []float64) float64 {
	n := float64(len(xs))
	var sx, sy, sxx, sxy float64
	for i := range xs {
		sx += xs[i]
		sy += ys[i]
		sxx += xs[i] * xs[i]
		sxy += xs[i] * ys[i]
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return 0
	}
	return (n*sxy - sx*sy) / den
}

const hoursOld = 5 * 24 * 365

// JudgeDisk predicts from the SMART trend whether a disk will fail.
func JudgeDisk(d Disk, now time.Time) Finding {
	f := Finding{Area: AreaDisk, Subject: d.Name, Level: LevelOK, Code: CodeDiskOK, Model: d.Model, Kind: d.Kind, SizeBytes: d.SizeBytes, Volume: d.Volume}
	if !d.Known {
		f.Level, f.Code = LevelWatch, CodeNoSmart
		return f
	}
	s := series(d, now)
	weeks := weeksOf(s)
	cur := d.Current
	if cur.PowerOnHours != nil {
		f.Hours = f64(float64(*cur.PowerOnHours))
	}
	set := func(level, code string) {
		if rank[level] < rank[f.Level] {
			if f.Code != CodeDiskOK {
				f.Also = append(f.Also, f.Code)
			}
			f.Level, f.Code = level, code
		} else if code != f.Code {
			f.Also = append(f.Also, code)
		}
	}
	counter := func(get func(Sample) *uint64) (float64, float64, bool, bool) {
		from, to, ok := rise(s, get)
		return from, to, ok, ok && to > from
	}
	reFrom, reTo, reOK, reUp := counter(func(x Sample) *uint64 { return x.Reallocated })
	peFrom, peTo, peOK, peUp := counter(func(x Sample) *uint64 { return x.Pending })
	_, unTo, unOK, unUp := counter(func(x Sample) *uint64 { return x.Uncorrectable })
	_, _, _, crcUp := counter(func(x Sample) *uint64 { return x.CRC })
	meFrom, meTo, _, meUp := counter(func(x Sample) *uint64 { return x.MediaErrors })

	if d.SmartFailed {
		set(LevelFail, CodeSmartFailed)
	}
	switch {
	case reUp && (peTo > 0 || unUp || reTo-reFrom >= 10 || peUp):
		set(LevelFail, CodeSectorsRising)
		f.From, f.To, f.Weeks = f64(reFrom), f64(reTo), f64(weeks)
	case reUp:
		// One step can be a single bad spot; worth watching closely.
		set(LevelWatch, CodeSectorsRising)
		f.From, f.To, f.Weeks = f64(reFrom), f64(reTo), f64(weeks)
	case reOK && reTo > 0:
		set(LevelWatch, CodeSectorsStable)
		f.To = f64(reTo)
	}
	switch {
	case peUp && peTo > 0:
		set(LevelFail, CodePendingRising)
		if f.From == nil {
			f.From, f.To, f.Weeks = f64(peFrom), f64(peTo), f64(weeks)
		}
	case peOK && peTo > 0:
		// Pending sectors may go back when they are rewritten.
		set(LevelWatch, CodePending)
	}
	if unUp {
		set(LevelFail, CodeUncorrectable)
	} else if unOK && unTo > 0 {
		set(LevelWatch, CodeUncorrectable)
	}
	if meUp {
		set(LevelFail, CodeMediaRising)
		if f.From == nil {
			f.From, f.To, f.Weeks = f64(meFrom), f64(meTo), f64(weeks)
		}
	}
	if cur.PercentageUsed != nil {
		wear := float64(*cur.PercentageUsed)
		f.Percent = f64(wear)
		left, ok := wearDaysLeft(s)
		switch {
		case wear >= 95 || ok && left < wearSoonDays:
			set(LevelFail, CodeWornOut)
			if ok {
				f.Days = f64(math.Round(left))
			}
		case wear >= 80:
			set(LevelWatch, CodeWear)
		}
	}
	if cur.AvailableSpare != nil && cur.SpareThreshold != nil && *cur.AvailableSpare <= *cur.SpareThreshold {
		set(LevelFail, CodeSpareLow)
	}
	if crcUp {
		// A rising CRC count is the cable or the port, not the disk.
		set(LevelWatch, CodeCable)
	}
	if cur.PowerOnHours != nil && *cur.PowerOnHours >= hoursOld {
		set(LevelWatch, CodeOld)
	}
	return f
}

func judgeMemory(samples []MemorySample) (Finding, bool) {
	if len(samples) == 0 {
		return Finding{}, false
	}
	last := samples[len(samples)-1]
	f := Finding{Area: AreaMemory, Subject: "memory", Level: LevelOK, Code: CodeMemoryOK, TotalBytes: f64(float64(last.Total))}
	var low, pressured, swapping int
	var worst float64
	for _, s := range samples {
		if s.Total == 0 {
			continue
		}
		used := 1 - float64(s.Available)/float64(s.Total)
		worst = math.Max(worst, used)
		if used >= 0.9 {
			low++
		}
		if s.PressureSome >= 10 {
			pressured++
		}
		if s.SwapTotal > 0 && s.SwapTotal-s.SwapFree > s.SwapTotal/4 {
			swapping++
		}
	}
	f.Percent = f64(math.Round(worst * 100))
	n := len(samples)
	switch {
	case swapping*4 >= n && swapping > 0:
		f.Level, f.Code = LevelTight, CodeMemorySwap
	case low*4 >= n && low > 0 || pressured*4 >= n && pressured > 0:
		f.Level, f.Code = LevelTight, CodeMemoryTight
	case low > 0 || pressured > 0:
		f.Level, f.Code = LevelWatch, CodeMemoryTight
	}
	if f.Level != LevelOK && last.Total > 0 {
		f.SuggestBytes = f64(float64(suggestMore(last.Total)))
	}
	return f, true
}

// suggestMore is how much memory to add: up to the next of 8, 16, 32, 64,
// 128 GiB that is at least 1.5 times today's.
func suggestMore(total uint64) uint64 {
	const gib = 1 << 30
	want := uint64(float64(total) * 1.5)
	for _, step := range []uint64{8, 16, 32, 64, 128, 256} {
		if step*gib >= want {
			return step*gib - total
		}
	}
	return total
}

func judgeVolume(v Volume) Finding {
	f := Finding{Area: AreaVolume, Subject: v.Name, Level: LevelOK, Code: CodeVolumeOK, Volume: v.Name}
	if v.Size == 0 {
		return f
	}
	pct := float64(v.Used) / float64(v.Size) * 100
	f.Percent = f64(math.Round(pct))
	if v.DailyBytes != nil && *v.DailyBytes > 0 {
		days := float64(v.Size-min(v.Used, v.Size)) / *v.DailyBytes
		f.Days = f64(math.Round(days))
		f.Bytes = f64(*v.DailyBytes)
		switch {
		case pct >= 95:
			f.Level, f.Code = LevelTight, CodeVolumeFull
		case days < 60:
			f.Level, f.Code = LevelTight, CodeVolumeFilling
		case days < 180:
			f.Level, f.Code = LevelWatch, CodeVolumeFilling
		}
		return f
	}
	switch {
	case pct >= 95:
		f.Level, f.Code = LevelTight, CodeVolumeFull
	case pct >= 85:
		f.Level, f.Code = LevelWatch, CodeVolumeHigh
	}
	return f
}

// judgeCPU looks at the last day: hours with an average above 85 % count
// as sustained.
func judgeCPU(samples []CPUSample, now time.Time) (Finding, bool) {
	hours := map[int64][]float64{}
	for _, s := range samples {
		if now.Sub(s.At) > 24*time.Hour || s.At.After(now) {
			continue
		}
		h := s.At.Unix() / 3600
		hours[h] = append(hours[h], s.Percent)
	}
	if len(hours) == 0 {
		return Finding{}, false
	}
	var busy, high int
	var total float64
	for _, list := range hours {
		var sum float64
		for _, p := range list {
			sum += p
		}
		avg := sum / float64(len(list))
		total += avg
		if avg >= sustainedCPUPercent {
			high++
		}
		if avg >= busyCPUPercent {
			busy++
		}
	}
	f := Finding{Area: AreaCPU, Subject: "cpu", Level: LevelOK, Code: CodeCPUOK, Percent: f64(math.Round(total / float64(len(hours))))}
	switch {
	case high >= 6:
		f.Level, f.Code, f.Hours = LevelTight, CodeCPUBusy, f64(float64(high))
	case busy >= 6:
		f.Level, f.Code, f.Hours = LevelWatch, CodeCPUBusy, f64(float64(busy))
	}
	return f, true
}

func judgeGPU(g GPU) (Finding, bool) {
	if g.Total == nil || *g.Total <= 0 {
		return Finding{}, false
	}
	f := Finding{Area: AreaGPU, Subject: g.Name, Level: LevelOK, Code: CodeGPUOK, TotalBytes: f64(*g.Total)}
	used := g.UsedBytes
	if used == nil && g.OllamaBytes != nil {
		used = f64(float64(*g.OllamaBytes))
	}
	if g.OllamaBytes != nil {
		f.Bytes = f64(float64(*g.OllamaBytes))
	} else if used != nil {
		f.Bytes = f64(*used)
	}
	if used != nil {
		pct := *used / *g.Total * 100
		f.Percent = f64(math.Round(pct))
		if pct >= 95 {
			f.Level, f.Code = LevelTight, CodeGPUFull
		}
	}
	return f, true
}

// Disk temperature limits, the same as the SMART warning.
const (
	hotDisk  = 55.0
	warmDisk = 50.0
	hotNVMe  = 70.0
	warmNVMe = 65.0
	hotChip  = 90.0
	warmChip = 80.0
)

func judgeTemps(temps []Temperature, disks []Disk) (Finding, bool) {
	all := append([]Temperature{}, temps...)
	for _, d := range disks {
		if d.Known && d.Current.TemperatureC != nil {
			all = append(all, Temperature{Label: d.Name, Celsius: float64(*d.Current.TemperatureC), DiskKind: d.Kind})
		}
	}
	if len(all) == 0 {
		return Finding{}, false
	}
	f := Finding{Area: AreaTemperature, Subject: "temperatures", Level: LevelOK, Code: CodeTempsOK}
	worst := -1.0
	for _, t := range all {
		hot, warm := hotChip, warmChip
		switch t.DiskKind {
		case "":
		case "nvme":
			hot, warm = hotNVMe, warmNVMe
		default:
			hot, warm = hotDisk, warmDisk
		}
		level, code := "", ""
		switch {
		case t.Celsius >= hot:
			level, code = LevelTight, CodeTempHot
		case t.Celsius >= warm:
			level, code = LevelWatch, CodeTempWarm
		}
		if level != "" && (rank[level] < rank[f.Level] || rank[level] == rank[f.Level] && t.Celsius > worst) {
			f.Level, f.Code, f.Celsius, f.Label = level, code, f64(t.Celsius), t.Label
			worst = t.Celsius
		}
	}
	return f, true
}
