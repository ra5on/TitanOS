package hardware

// GuC and HuC are microcontrollers of Intel graphics. The i915 driver of
// the kernel (Linux 6.12, intel_uc.c) only loads their firmware by itself
// from Alder Lake on (Alder Lake-S: HuC; Alder Lake-P, DG2 and later also
// GuC submission). On Gen9 to Gen11 (Skylake to Ice Lake, Jasper Lake and
// Elkhart Lake) and on Tiger and Rocket Lake enable_guc defaults to 0, so
// the HuC that low-power (VDEnc) encoding with bitrate control needs is
// not loaded. This file finds those GPUs, shows what the
// kernel loaded and lets the owner ask for i915.enable_guc on the kernel
// command line: i915 is in the initramfs and loads before anything of the
// running system could write a modprobe.d file.

import (
	"bufio"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/run"
	"github.com/ra5on/TitanOS/backend/internal/update"
)

const (
	intelGucConfig = "system/intel-gpu.json"
	// GucParam is the kernel parameter the setting writes.
	GucParam = "i915.enable_guc"
)

var (
	ErrGucMode = errors.New("hardware: unknown GuC/HuC mode")
	// ErrNoIntelGen9 means there is no Intel GPU for which the setting
	// does anything: none from Gen9 to Tiger/Rocket Lake on the i915 driver.
	ErrNoIntelGen9 = errors.New("hardware: no Intel GPU from Gen9 to Tiger/Rocket Lake found")
	// ErrBootUnavailable means the boot entries cannot be written here:
	// development mode, or titand did not start from the stick.
	ErrBootUnavailable = errors.New("hardware: the boot entries cannot be changed here")
)

// Modes of the setting and the value of i915.enable_guc they write.
// "auto" leaves the kernel default: no parameter at all.
var gucModes = map[string]string{"auto": "", "huc": "2", "guc": "3"}

// IntelGucSettings is what /config holds.
type IntelGucSettings struct {
	Mode string `json:"mode"`
}

// intelPlatform is a platform of the i915 driver that does not load GuC
// and HuC by default.
type intelPlatform struct {
	Key, Name, Generation string
	// firmware is the prefix of the files in /usr/lib/firmware/i915 the
	// kernel asks for (Coffee and Comet Lake use those of Kaby Lake).
	firmware   string
	guc, huc   string
	submission bool // GuC submission exists from Gen11 on
}

var intelPlatforms = map[string]intelPlatform{
	"skylake":     {"skylake", "Skylake", "9", "skl", "70.1.1", "2.0.0", false},
	"apollolake":  {"apollolake", "Apollo Lake", "9", "bxt", "70.1.1", "2.0.0", false},
	"kabylake":    {"kabylake", "Kaby Lake", "9.5", "kbl", "70.1.1", "4.0.0", false},
	"geminilake":  {"geminilake", "Gemini Lake", "9.5", "glk", "70.1.1", "4.0.0", false},
	"coffeelake":  {"coffeelake", "Coffee Lake", "9.5", "kbl", "70.1.1", "4.0.0", false},
	"whiskeylake": {"whiskeylake", "Whiskey Lake", "9.5", "kbl", "70.1.1", "4.0.0", false},
	"cometlake":   {"cometlake", "Comet Lake", "9.5", "kbl", "70.1.1", "4.0.0", false},
	"icelake":     {"icelake", "Ice Lake", "11", "icl", "70.1.1", "9.0.0", true},
	"elkhartlake": {"elkhartlake", "Elkhart Lake", "11", "ehl", "70.1.1", "9.0.0", true},
	"jasperlake":  {"jasperlake", "Jasper Lake", "11", "ehl", "70.1.1", "9.0.0", true},
	// Tiger and Rocket Lake are Gen12, but the kernel still leaves them at 0.
	"tigerlake":  {"tigerlake", "Tiger Lake", "12", "tgl", "70.1.1", "7.9.3", true},
	"rocketlake": {"rocketlake", "Rocket Lake", "12", "tgl", "70.1.1", "7.9.3", true},
}

// intelDeviceIDs are the PCI device IDs per platform, as the kernel's
// include/drm/intel/i915_pciids.h (Linux 6.12) lists them. Coffee Lake
// includes Amber Lake CFL, Kaby Lake includes Amber Lake KBL.
var intelDeviceIDs = map[string][]string{
	"skylake":     {"1902", "1906", "190a", "190b", "190e", "1912", "1913", "1915", "1916", "1917", "191a", "191b", "191d", "191e", "1921", "1923", "1926", "1927", "192a", "192b", "192d", "1932", "193a", "193b", "193d"},
	"apollolake":  {"0a84", "1a84", "1a85", "5a84", "5a85"},
	"geminilake":  {"3184", "3185"},
	"kabylake":    {"5902", "5906", "5908", "590a", "590b", "590e", "5912", "5913", "5915", "5916", "5917", "591a", "591b", "591c", "591d", "591e", "5921", "5923", "5926", "5927", "593b", "87c0"},
	"coffeelake":  {"3e90", "3e91", "3e92", "3e93", "3e94", "3e96", "3e98", "3e99", "3e9a", "3e9b", "3e9c", "3ea5", "3ea6", "3ea7", "3ea8", "3ea9", "87ca"},
	"whiskeylake": {"3ea0", "3ea1", "3ea2", "3ea3", "3ea4"},
	"cometlake":   {"9b21", "9b41", "9ba2", "9ba4", "9ba5", "9ba8", "9baa", "9bac", "9bc2", "9bc4", "9bc5", "9bc6", "9bc8", "9bca", "9bcc", "9be6", "9bf6"},
	"icelake":     {"8a50", "8a51", "8a52", "8a53", "8a54", "8a56", "8a57", "8a58", "8a59", "8a5a", "8a5b", "8a5c", "8a5d", "8a70", "8a71"},
	"elkhartlake": {"4541", "4551", "4555", "4557", "4570", "4571"},
	"jasperlake":  {"4e51", "4e55", "4e57", "4e61", "4e71"},
	"tigerlake":   {"9a40", "9a49", "9a59", "9a60", "9a68", "9a70", "9a78", "9ac0", "9ac9", "9ad9", "9af8"},
	"rocketlake":  {"4c80", "4c8a", "4c8b", "4c8c", "4c90", "4c9a"},
}

var platformByID = func() map[string]string {
	ids := map[string]string{}
	for key, list := range intelDeviceIDs {
		for _, id := range list {
			ids[id] = key
		}
	}
	return ids
}()

// intelPlatformOf returns the platform of an Intel device ID (four hex
// digits, any case); ok is false for anything else: older ones have no HuC, newer ones get GuC/HuC from the kernel by default.
func intelPlatformOf(device string) (intelPlatform, bool) {
	key, ok := platformByID[strings.ToLower(strings.TrimPrefix(device, "0x"))]
	if !ok {
		return intelPlatform{}, false
	}
	return intelPlatforms[key], true
}

// firmwareFiles returns the GuC and HuC files the kernel asks for. Comet
// Lake from revision 5 on has files of its own.
func (p intelPlatform) firmwareFiles(revision int) (guc, huc string) {
	prefix := p.firmware
	if p.Key == "cometlake" && revision >= 5 {
		prefix = "cml"
	}
	return "i915/" + prefix + "_guc_" + p.guc + ".bin", "i915/" + prefix + "_huc_" + p.huc + ".bin"
}

// UCFirmware is what is known of the GuC or HuC of a GPU.
type UCFirmware struct {
	// File is the firmware file the kernel asks for, Present whether it is
	// in the image (also compressed).
	File    string `json:"file"`
	Present bool   `json:"present"`
	// Loaded, Version and State are what the kernel reports: the file it
	// took, its version and RUNNING, LOAD FAIL ...; State is "disabled" if
	// the kernel does not want it and "" if unknown.
	Loaded  string `json:"loaded"`
	Version string `json:"version"`
	State   string `json:"state"`
	// Authenticated is set once the kernel reported the HuC as authenticated.
	Authenticated bool `json:"authenticated"`
}

// IntelGPU is an Intel graphics device and what its GuC/HuC are doing.
type IntelGPU struct {
	Address  string `json:"address"`
	Device   string `json:"device"`
	Revision int    `json:"revision"`
	// Platform, Name and Generation are set for Gen9 to Tiger/Rocket Lake.
	Platform   string `json:"platform"`
	Name       string `json:"name"`
	Generation string `json:"generation"`
	Driver     string `json:"driver"`
	// RenderNode is e.g. renderD128; empty without a driver.
	RenderNode string `json:"renderNode"`
	Card       string `json:"card"`
	// Applies says whether the setting does something for this GPU:
	// a platform of the table on the i915 driver.
	Applies bool `json:"applies"`
	// Submission says whether GuC submission can be used (Gen11 on).
	Submission bool `json:"submission"`
	// SubmissionUsed is what the kernel reports, nil if unknown.
	SubmissionUsed *bool      `json:"submissionUsed"`
	GuC            UCFirmware `json:"guc"`
	HuC            UCFirmware `json:"huc"`
	// Source says where the state came from: debugfs, journal or "".
	Source string `json:"source"`
	// Messages are kernel lines worth showing, such as failures.
	Messages []string `json:"messages"`
}

// IntelGucStatus is what the hardware page shows.
type IntelGucStatus struct {
	// Mode is the setting; Managed is false while it was never set: then
	// TitanOS does not touch the boot entries at all.
	Mode    string `json:"mode"`
	Managed bool   `json:"managed"`
	// Active is the value on the command line the NAS started with, ""
	// for none; Param what the i915 module reports (-1 is automatic).
	Active string `json:"active"`
	Param  string `json:"param"`
	// Entries is the value per boot entry on the stick ("" for none).
	Entries  map[string]string `json:"entries"`
	Deferred bool              `json:"deferred"`
	// RestartNeeded is set while the setting differs from what is active.
	RestartNeeded bool `json:"restartNeeded"`
	// Writable says whether the setting can be changed here.
	Writable bool       `json:"writable"`
	GPUs     []IntelGPU `json:"gpus"`
	Problem  string     `json:"problem"`
}

// BootParams writes kernel parameters into the boot entries.
type BootParams interface {
	SetKernelParam(name, value string) (update.ParamState, error)
	KernelParam(name string) update.ParamState
}

// IntelGuc keeps the GuC/HuC setting.
type IntelGuc struct {
	store   *config.Store
	devices *Manager
	boot    BootParams
	runner  run.Runner
	// Disabled leaves the stick alone: development mode.
	Disabled bool
	// sys, proc, debug and firmware are where the kernel shows things;
	// tests replace them.
	sys, proc, debug, firmware string
	retryEvery                 time.Duration

	mu          sync.Mutex
	loaded      bool
	settings    *IntelGucSettings
	entries     update.ParamState
	entriesRead bool
	problem     string
	wake        chan struct{}
}

// NewIntelGuc returns the setting for the real system.
func NewIntelGuc(store *config.Store, devices *Manager, boot BootParams, disabled bool) *IntelGuc {
	return &IntelGuc{store: store, devices: devices, boot: boot, runner: run.Exec{}, Disabled: disabled,
		sys: "/sys", proc: "/proc", debug: "/sys/kernel/debug", firmware: "/usr/lib/firmware",
		retryEvery: 30 * time.Second, wake: make(chan struct{}, 1)}
}

// Load reads the setting from /config, once at the start.
func (g *IntelGuc) Load() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	var settings IntelGucSettings
	err := g.store.ReadJSON(intelGucConfig, &settings)
	if errors.Is(err, fs.ErrNotExist) {
		g.loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	if _, ok := gucModes[settings.Mode]; !ok {
		return ErrGucMode
	}
	g.settings, g.loaded = &settings, true
	return nil
}

// Run makes the boot entries follow the setting: right after the start
// (another system may have written an entry without it, e.g. an older
// version installing an update) and whenever the running entry was on
// trial. Entries that already match are not written.
func (g *IntelGuc) Run(ctx context.Context) {
	if g.Disabled || g.boot == nil {
		return
	}
	tries := 0
	for {
		g.mu.Lock()
		settings := g.settings
		g.mu.Unlock()
		deferred := false
		if settings != nil {
			state, err := g.boot.SetKernelParam(GucParam, gucModes[settings.Mode])
			g.mu.Lock()
			if errors.Is(err, update.ErrBusy) {
				deferred = true // an update is writing the stick; later
			} else if err != nil && !errors.Is(err, update.ErrDisabled) {
				g.problem = err.Error()
				slog.Warn("GuC/HuC setting not written to the boot entries", "err", err)
			} else if err == nil {
				g.entries, g.entriesRead, g.problem = state, true, ""
				deferred = state.Deferred
			}
			g.mu.Unlock()
		}
		// The boot check confirms a system within minutes; no endless
		// looking at the stick if it never does.
		var retry <-chan time.Time
		if deferred && tries < 40 {
			tries++
			retry = time.After(g.retryEvery)
		}
		select {
		case <-ctx.Done():
			return
		case <-g.wake:
			tries = 0
		case <-retry:
		}
	}
}

// Set changes the setting. It takes effect with the next start of the NAS.
func (g *IntelGuc) Set(mode string) error {
	value, ok := gucModes[mode]
	if !ok {
		return ErrGucMode
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.loaded {
		return errors.New("hardware: the setting could not be read from /config")
	}
	if g.Disabled || g.boot == nil {
		return ErrBootUnavailable
	}
	if mode != "auto" && !slices.ContainsFunc(g.gpus(false), func(gpu IntelGPU) bool { return gpu.Applies }) {
		return ErrNoIntelGen9
	}
	if g.settings != nil && g.settings.Mode == mode {
		return nil // nothing changes, nothing is written
	}
	before := ""
	if g.settings != nil {
		before = gucModes[g.settings.Mode]
	}
	state, err := g.boot.SetKernelParam(GucParam, value)
	if errors.Is(err, update.ErrDisabled) {
		return ErrBootUnavailable
	}
	if err != nil {
		return err
	}
	settings := IntelGucSettings{Mode: mode}
	if err := g.store.WriteJSON(intelGucConfig, settings); err != nil {
		// The entries follow the setting that is still stored.
		if state, back := g.boot.SetKernelParam(GucParam, before); back == nil {
			g.entries = state
		}
		return err
	}
	g.settings, g.entries, g.entriesRead, g.problem = &settings, state, true, ""
	if state.Deferred {
		select {
		case g.wake <- struct{}{}:
		default:
		}
	}
	return nil
}

func (g *IntelGuc) read(parts ...string) string {
	data, _ := os.ReadFile(filepath.Join(parts...))
	return strings.TrimSpace(string(data))
}

// Status reports the setting, the boot entries and the Intel GPUs. It is
// meant for a page being opened: it may read the stick once and the
// kernel log.
func (g *IntelGuc) Status(ctx context.Context) IntelGucStatus {
	g.mu.Lock()
	defer g.mu.Unlock()
	status := IntelGucStatus{Mode: "auto", Entries: map[string]string{}, Writable: !g.Disabled && g.boot != nil, Problem: g.problem}
	if g.settings != nil {
		status.Mode, status.Managed = g.settings.Mode, true
	}
	cmdline := g.read(g.proc, "cmdline")
	status.Active = activeGuc(cmdline)
	status.Param = g.read(g.sys, "module/i915/parameters/enable_guc")
	if status.Writable && !g.entriesRead {
		g.entries, g.entriesRead = g.boot.KernelParam(GucParam), true
	}
	if g.entries.Entries != nil {
		status.Entries, status.Deferred = g.entries.Entries, g.entries.Deferred
	}
	status.RestartNeeded = status.Managed && gucModes[status.Mode] != status.Active
	status.GPUs = g.gpus(true)
	if len(status.GPUs) > 0 && status.GPUs[0].Source == "" {
		g.fromJournal(ctx, status.GPUs)
	}
	return status
}

// Report is the state for the diagnostic bundle.
func (g *IntelGuc) Report(ctx context.Context) any {
	return g.Status(ctx)
}

// activeGuc returns the value of i915.enable_guc on a kernel command line.
func activeGuc(cmdline string) string {
	value := ""
	for _, field := range strings.Fields(cmdline) {
		if key, v, _ := strings.Cut(field, "="); key == GucParam {
			value = v
		}
	}
	return value
}

// gpus lists the Intel graphics devices. With state it also reads what
// debugfs tells about GuC and HuC.
func (g *IntelGuc) gpus(state bool) []IntelGPU {
	gpus := []IntelGPU{}
	root := filepath.Join(g.sys, "bus/pci/devices")
	for _, device := range g.devices.Status().PCI {
		if device.Vendor != "8086" || device.Kind != "igpu" && device.Kind != "gpu" {
			continue
		}
		gpu := IntelGPU{Address: device.Address, Device: device.Device, Driver: device.Driver, Messages: []string{}}
		revision, _ := strconv.ParseInt(strings.TrimPrefix(g.read(root, device.Address, "revision"), "0x"), 16, 32)
		gpu.Revision = int(revision)
		nodes, _ := os.ReadDir(filepath.Join(root, device.Address, "drm"))
		for _, node := range nodes {
			switch name := node.Name(); {
			case strings.HasPrefix(name, "renderD"):
				gpu.RenderNode = name
			case strings.HasPrefix(name, "card"):
				gpu.Card = name
			}
		}
		if platform, ok := intelPlatformOf(device.Device); ok {
			gpu.Platform, gpu.Name, gpu.Generation, gpu.Submission = platform.Key, platform.Name, platform.Generation, platform.submission
			gpu.Applies = device.Driver == "i915"
			guc, huc := platform.firmwareFiles(gpu.Revision)
			gpu.GuC = UCFirmware{File: guc, Present: g.firmwarePresent(guc)}
			gpu.HuC = UCFirmware{File: huc, Present: g.firmwarePresent(huc)}
		}
		if state && device.Driver == "i915" {
			g.fromDebugfs(&gpu)
		}
		gpus = append(gpus, gpu)
	}
	return gpus
}

func (g *IntelGuc) firmwarePresent(file string) bool {
	for _, suffix := range []string{"", ".zst", ".xz"} {
		if info, err := os.Stat(filepath.Join(g.firmware, file+suffix)); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			return true
		}
	}
	return false
}

// fromDebugfs reads the uc directory of every GT of the GPU in debugfs. The
// DRM directory is named after the minor of the card; older kernels have no
// gtN level. From Meteor Lake on the HuC belongs to the media GT (gt1) and
// gt0 reports it as not supported, so each firmware is taken from the GT
// where it does the most.
func (g *IntelGuc) fromDebugfs(gpu *IntelGPU) {
	if gpu.Card == "" {
		return
	}
	minor := strings.TrimPrefix(gpu.Card, "card")
	var dirs []string
	for _, base := range []string{filepath.Join(g.debug, "dri", minor), filepath.Join(g.debug, "dri", gpu.Address)} {
		found, _ := filepath.Glob(filepath.Join(base, "gt[0-9]*", "uc"))
		sort.Strings(found)
		if len(found) > 0 {
			dirs = found
			break
		}
	}
	if len(dirs) == 0 {
		dirs = []string{filepath.Join(g.debug, "dri", minor, "gt/uc")}
	}
	guc, huc := gpu.GuC, gpu.HuC
	for i, dir := range dirs {
		hucText, hucErr := os.ReadFile(filepath.Join(dir, "huc_info"))
		gucText, gucErr := os.ReadFile(filepath.Join(dir, "guc_info"))
		usage, usageErr := os.ReadFile(filepath.Join(dir, "usage"))
		if hucErr != nil && gucErr != nil && usageErr != nil {
			continue
		}
		gpu.Source = "debugfs"
		gtGuC, gtHuC := gpu.GuC, gpu.HuC
		parseUCInfo(string(hucText), "HuC", &gtHuC)
		parseUCInfo(string(gucText), "GuC", &gtGuC)
		gt := IntelGPU{GuC: gtGuC, HuC: gtHuC}
		parseUCUsage(string(usage), &gt)
		// Submission is a matter of the primary GT.
		if i == 0 || gpu.SubmissionUsed == nil {
			gpu.SubmissionUsed = gt.SubmissionUsed
		}
		guc, huc = betterUC(guc, gt.GuC), betterUC(huc, gt.HuC)
	}
	gpu.GuC, gpu.HuC = guc, huc
}

// betterUC returns the firmware state that says more: running and
// authenticated before any other state, any state before "disabled" and
// "disabled" before unknown.
func betterUC(current, next UCFirmware) UCFirmware {
	rank := func(fw UCFirmware) int {
		switch {
		case fw.Authenticated:
			return 4
		case fw.State == "RUNNING":
			return 3
		case fw.State != "" && fw.State != "disabled":
			return 2
		case fw.State == "disabled":
			return 1
		}
		return 0
	}
	if rank(next) > rank(current) {
		return next
	}
	return current
}

var (
	ucFirmwareLine = regexp.MustCompile(`^(GuC|HuC) firmware: (\S*)$`)
	ucStatusLine   = regexp.MustCompile(`^status: (.+)$`)
	ucVersionLine  = regexp.MustCompile(`^version: (?:wanted \S+, )?found (\S+)$`)
	ucUsageLine    = regexp.MustCompile(`^\[(guc|huc|submission)\] supported:(yes|no) wanted:(yes|no) used:(yes|no)`)
)

// parseUCInfo reads huc_info or guc_info of debugfs (intel_uc_fw_dump in
// drivers/gpu/drm/i915/gt/uc/intel_uc_fw.c).
func parseUCInfo(text, kind string, fw *UCFirmware) {
	inside := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == kind+" disabled" || line == kind+" not supported":
			fw.State = "disabled"
			return
		case ucFirmwareLine.MatchString(line):
			match := ucFirmwareLine.FindStringSubmatch(line)
			inside = match[1] == kind
			if inside {
				fw.Loaded = match[2]
			}
		case !inside:
		case ucStatusLine.MatchString(line):
			if fw.State == "" {
				fw.State = ucStatusLine.FindStringSubmatch(line)[1]
			}
		case ucVersionLine.MatchString(line):
			if fw.Version == "" {
				fw.Version = ucVersionLine.FindStringSubmatch(line)[1]
			}
		}
	}
	// A HuC is only RUNNING once it is authenticated.
	if kind == "HuC" && fw.State == "RUNNING" {
		fw.Authenticated = true
	}
}

// parseUCUsage reads the usage file of debugfs.
func parseUCUsage(text string, gpu *IntelGPU) {
	for _, line := range strings.Split(text, "\n") {
		match := ucUsageLine.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		used := match[4] == "yes"
		switch match[1] {
		case "submission":
			gpu.SubmissionUsed = &used
		case "guc":
			if match[3] == "no" && gpu.GuC.State == "" {
				gpu.GuC.State = "disabled"
			}
		case "huc":
			if match[3] == "no" && gpu.HuC.State == "" {
				gpu.HuC.State = "disabled"
			}
		}
	}
}

// fromJournal reads the kernel messages of this boot when debugfs gives
// nothing (not mounted, or no root).
func (g *IntelGuc) fromJournal(ctx context.Context, gpus []IntelGPU) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := g.runner.Run(ctx, "", "journalctl", "-k", "-b", "0", "-o", "cat", "--no-pager", "-n", "20000")
	if err != nil && output == "" {
		return
	}
	for i := range gpus {
		if gpus[i].Driver == "i915" {
			parseKernelLog(output, &gpus[i])
		}
	}
}

var (
	logFirmware  = regexp.MustCompile(`(GuC|HuC) firmware (i915/\S+) version (\d+\.\d+\.\d+)`)
	logHucAuth   = regexp.MustCompile(`(?i)HuC: authenticated`)
	logSubmit    = regexp.MustCompile(`(?i)GuC: submission (enabled|disabled)`)
	logProblem   = regexp.MustCompile(`(?i)(GuC|HuC|GSC).*(fail|error|missing|timeout)|Incompatible option enable_guc`)
	logAddress   = regexp.MustCompile(`\b[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-9a-f]\b`)
	maxMessages  = 8
	maxLineBytes = 300
)

// parseKernelLog picks the GuC/HuC lines of a GPU from the kernel log:
// those that name its address, or no address at all.
func parseKernelLog(log string, gpu *IntelGPU) {
	scanner := bufio.NewScanner(strings.NewReader(log))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	found := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.Contains(line, "GuC") && !strings.Contains(line, "HuC") && !strings.Contains(line, "GUC") && !strings.Contains(line, "GSC") && !strings.Contains(line, "enable_guc") {
			continue
		}
		if address := logAddress.FindString(line); address != "" && address != gpu.Address {
			continue
		}
		found = true
		if match := logFirmware.FindStringSubmatch(line); match != nil {
			fw := &gpu.GuC
			if match[1] == "HuC" {
				fw = &gpu.HuC
			}
			fw.Loaded, fw.Version = match[2], match[3]
		}
		if logHucAuth.MatchString(line) {
			gpu.HuC.Authenticated = true
		}
		if match := logSubmit.FindStringSubmatch(line); match != nil {
			used := strings.EqualFold(match[1], "enabled")
			gpu.SubmissionUsed = &used
		}
		if logProblem.MatchString(line) && len(gpu.Messages) < maxMessages {
			if len(line) > maxLineBytes {
				line = line[:maxLineBytes]
			}
			gpu.Messages = append(gpu.Messages, line)
		}
	}
	if found {
		gpu.Source = "journal"
	}
}
