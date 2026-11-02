package hardware

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/update"
)

func TestIntelPlatformsByPCIID(t *testing.T) {
	cases := map[string]string{
		"1912": "skylake", "0x191B": "skylake", "193d": "skylake",
		"5a85": "apollolake", "3185": "geminilake",
		"5912": "kabylake", "591c": "kabylake", "87c0": "kabylake",
		"3e92": "coffeelake", "3e9b": "coffeelake", "87ca": "coffeelake",
		"3ea0": "whiskeylake", "9bc8": "cometlake", "9b41": "cometlake",
		"8a52": "icelake", "4571": "elkhartlake", "4e61": "jasperlake",
		"9a49": "tigerlake", "4c8a": "rocketlake",
	}
	for id, want := range cases {
		if got, ok := intelPlatformOf(id); !ok || got.Key != want {
			t.Errorf("%s: %+v %v, want %s", id, got, ok, want)
		}
	}
	// Gen8, Alder Lake and later (the kernel loads GuC/HuC itself) and Xe2 (xe)
	// are not part of the setting.
	for _, id := range []string{"1616", "4680", "46a6", "a780", "56a0", "7d55", "64a0", "e20b", "1234", ""} {
		if got, ok := intelPlatformOf(id); ok {
			t.Errorf("%s is %+v", id, got)
		}
	}
	if p := intelPlatforms["skylake"]; p.Generation != "9" || p.submission {
		t.Errorf("skylake: %+v", p)
	}
	if p := intelPlatforms["icelake"]; p.Generation != "11" || !p.submission {
		t.Errorf("icelake: %+v", p)
	}
	// No ID belongs to two platforms; every platform has IDs.
	seen := map[string]string{}
	for key, ids := range intelDeviceIDs {
		if _, ok := intelPlatforms[key]; !ok || len(ids) == 0 {
			t.Errorf("platform %s", key)
		}
		for _, id := range ids {
			if other, dup := seen[id]; dup {
				t.Errorf("%s in %s and %s", id, other, key)
			}
			seen[id] = key
		}
	}
}

func TestFirmwareFilesPerPlatform(t *testing.T) {
	cases := []struct {
		key      string
		revision int
		guc, huc string
	}{
		{"skylake", 0, "i915/skl_guc_70.1.1.bin", "i915/skl_huc_2.0.0.bin"},
		{"apollolake", 0, "i915/bxt_guc_70.1.1.bin", "i915/bxt_huc_2.0.0.bin"},
		{"kabylake", 0, "i915/kbl_guc_70.1.1.bin", "i915/kbl_huc_4.0.0.bin"},
		{"coffeelake", 0, "i915/kbl_guc_70.1.1.bin", "i915/kbl_huc_4.0.0.bin"},
		{"cometlake", 0, "i915/kbl_guc_70.1.1.bin", "i915/kbl_huc_4.0.0.bin"},
		{"cometlake", 5, "i915/cml_guc_70.1.1.bin", "i915/cml_huc_4.0.0.bin"},
		{"geminilake", 0, "i915/glk_guc_70.1.1.bin", "i915/glk_huc_4.0.0.bin"},
		{"icelake", 0, "i915/icl_guc_70.1.1.bin", "i915/icl_huc_9.0.0.bin"},
		{"jasperlake", 0, "i915/ehl_guc_70.1.1.bin", "i915/ehl_huc_9.0.0.bin"},
		{"tigerlake", 0, "i915/tgl_guc_70.1.1.bin", "i915/tgl_huc_7.9.3.bin"},
	}
	for _, c := range cases {
		guc, huc := intelPlatforms[c.key].firmwareFiles(c.revision)
		if guc != c.guc || huc != c.huc {
			t.Errorf("%s rev %d: %s %s", c.key, c.revision, guc, huc)
		}
	}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata/intel-uc", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestParseUCInfoFromDebugfs(t *testing.T) {
	var huc, guc UCFirmware
	parseUCInfo(fixture(t, "huc_info.running"), "HuC", &huc)
	parseUCInfo(fixture(t, "guc_info.running"), "GuC", &guc)
	if huc.Loaded != "i915/kbl_huc_4.0.0.bin" || huc.State != "RUNNING" || huc.Version != "4.0.0" || !huc.Authenticated {
		t.Errorf("huc: %+v", huc)
	}
	if guc.Loaded != "i915/kbl_guc_70.1.1.bin" || guc.State != "RUNNING" || guc.Version != "70.1.1" || guc.Authenticated {
		t.Errorf("guc: %+v", guc)
	}
	var off UCFirmware
	parseUCInfo(fixture(t, "huc_info.disabled"), "HuC", &off)
	if off.State != "disabled" || off.Authenticated {
		t.Errorf("disabled: %+v", off)
	}
	var missing UCFirmware
	parseUCInfo(fixture(t, "guc_info.missing"), "GuC", &missing)
	if missing.State != "MISSING" || missing.Version != "0.0.0" {
		t.Errorf("missing: %+v", missing)
	}

	gpu := IntelGPU{}
	parseUCUsage(fixture(t, "usage.huc"), &gpu)
	if gpu.SubmissionUsed == nil || *gpu.SubmissionUsed || gpu.HuC.State != "" {
		t.Errorf("usage: %+v", gpu)
	}
	gpu = IntelGPU{}
	parseUCUsage(fixture(t, "usage.disabled"), &gpu)
	if gpu.GuC.State != "disabled" || gpu.HuC.State != "disabled" {
		t.Errorf("usage disabled: %+v", gpu)
	}
}

func TestParseKernelLog(t *testing.T) {
	gpu := IntelGPU{Address: "0000:00:02.0", Messages: []string{}}
	parseKernelLog(fixture(t, "journal.kbl"), &gpu)
	if gpu.Source != "journal" || gpu.GuC.Loaded != "i915/kbl_guc_70.1.1.bin" || gpu.GuC.Version != "70.1.1" ||
		gpu.HuC.Loaded != "i915/kbl_huc_4.0.0.bin" || gpu.HuC.Version != "4.0.0" || !gpu.HuC.Authenticated {
		t.Errorf("gpu: %+v", gpu)
	}
	if gpu.SubmissionUsed == nil || *gpu.SubmissionUsed {
		t.Errorf("submission: %v", gpu.SubmissionUsed)
	}
	// Gen9 has no GuC submission: the kernel says so. The failure of the
	// other GPU is not this one's.
	if len(gpu.Messages) != 1 || gpu.Messages[0] != "i915 0000:00:02.0: [drm] GT0: Incompatible option enable_guc=3 - GuC submission is N/A" {
		t.Errorf("messages: %q", gpu.Messages)
	}
	other := IntelGPU{Address: "0000:03:00.0", Messages: []string{}}
	parseKernelLog(fixture(t, "journal.kbl"), &other)
	if other.HuC.Authenticated || len(other.Messages) != 1 {
		t.Errorf("other: %+v", other)
	}
	none := IntelGPU{Address: "0000:00:02.0", Messages: []string{}}
	parseKernelLog("Linux version 6.12\n", &none)
	if none.Source != "" {
		t.Errorf("empty log: %+v", none)
	}
}

func TestActiveGuc(t *testing.T) {
	for cmdline, want := range map[string]string{
		"boot=live quiet":                       "",
		"boot=live i915.enable_guc=2":           "2",
		"i915.enable_guc=3 i915.enable_guc=2 x": "2",
		"i915.enable_gucx=2":                    "",
	} {
		if got := activeGuc(cmdline); got != want {
			t.Errorf("%q: %q", cmdline, got)
		}
	}
}

// fakeBoot stands in for the boot entries.
type fakeBoot struct {
	values   map[string]string
	writes   int
	deferred bool
	err      error
}

func (f *fakeBoot) SetKernelParam(name, value string) (update.ParamState, error) {
	if f.err != nil {
		return update.ParamState{}, f.err
	}
	if f.values[name] != value {
		f.writes++
	}
	f.values[name] = value
	return update.ParamState{Entries: map[string]string{"a": value}, Deferred: f.deferred}, nil
}

func (f *fakeBoot) KernelParam(name string) update.ParamState {
	return update.ParamState{Entries: map[string]string{"a": f.values[name]}}
}

// intelSystem pretends a Kaby Lake NAS running on i915.
func intelSystem(t *testing.T, device, driver string) (*IntelGuc, *fakeBoot, string) {
	t.Helper()
	root := t.TempDir()
	sys := filepath.Join(root, "sys")
	dev := filepath.Join(sys, "bus/pci/devices/0000:00:02.0")
	os.MkdirAll(filepath.Join(dev, "drm/card0"), 0o755)
	os.MkdirAll(filepath.Join(dev, "drm/renderD128"), 0o755)
	for name, value := range map[string]string{"class": "0x030000", "vendor": "0x8086", "device": "0x" + device, "revision": "0x02"} {
		os.WriteFile(filepath.Join(dev, name), []byte(value+"\n"), 0o644)
	}
	if driver != "" {
		os.MkdirAll(filepath.Join(sys, "bus/pci/drivers", driver), 0o755)
		os.Symlink(filepath.Join(sys, "bus/pci/drivers", driver), filepath.Join(dev, "driver"))
	}
	os.MkdirAll(filepath.Join(sys, "module/i915/parameters"), 0o755)
	os.WriteFile(filepath.Join(sys, "module/i915/parameters/enable_guc"), []byte("-1\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "proc"), 0o755)
	os.WriteFile(filepath.Join(root, "proc/cmdline"), []byte("boot=live live-media-path=/titanos/a\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "firmware/i915"), 0o755)
	os.WriteFile(filepath.Join(root, "firmware/i915/kbl_huc_4.0.0.bin"), []byte("huc"), 0o644)
	os.WriteFile(filepath.Join(root, "firmware/i915/kbl_guc_70.1.1.bin.zst"), []byte("guc"), 0o644)
	devices := &Manager{sys: sys, proc: filepath.Join(root, "proc"), state: filepath.Join(root, "run"), stickLink: filepath.Join(root, "none")}
	boot := &fakeBoot{values: map[string]string{}}
	g := NewIntelGuc(config.Open(filepath.Join(root, "config")), devices, boot, false)
	g.sys, g.proc, g.debug, g.firmware = sys, filepath.Join(root, "proc"), filepath.Join(root, "debug"), filepath.Join(root, "firmware")
	g.runner = journalRunner{}
	return g, boot, root
}

type journalRunner struct{ log string }

func (r journalRunner) Run(context.Context, string, string, ...string) (string, error) {
	return r.log, nil
}

func TestIntelGucStatusAndSetting(t *testing.T) {
	g, boot, root := intelSystem(t, "5912", "i915")
	if err := g.Load(); err != nil {
		t.Fatal(err)
	}
	status := g.Status(context.Background())
	if status.Mode != "auto" || status.Managed || status.RestartNeeded || status.Param != "-1" || status.Active != "" || !status.Writable || len(status.GPUs) != 1 {
		t.Fatalf("status: %+v", status)
	}
	gpu := status.GPUs[0]
	if gpu.Platform != "kabylake" || gpu.Name != "Kaby Lake" || gpu.Generation != "9.5" || !gpu.Applies || gpu.Submission ||
		gpu.RenderNode != "renderD128" || gpu.Card != "card0" || gpu.Revision != 2 || gpu.Driver != "i915" {
		t.Errorf("gpu: %+v", gpu)
	}
	if !gpu.HuC.Present || !gpu.GuC.Present || gpu.HuC.File != "i915/kbl_huc_4.0.0.bin" {
		t.Errorf("firmware: %+v %+v", gpu.GuC, gpu.HuC)
	}

	// debugfs gives the state of the firmware.
	uc := filepath.Join(root, "debug/dri/0/gt0/uc")
	os.MkdirAll(uc, 0o755)
	os.WriteFile(filepath.Join(uc, "huc_info"), []byte(fixture(t, "huc_info.running")), 0o644)
	os.WriteFile(filepath.Join(uc, "guc_info"), []byte(fixture(t, "guc_info.running")), 0o644)
	os.WriteFile(filepath.Join(uc, "usage"), []byte(fixture(t, "usage.huc")), 0o644)
	gpu = g.Status(context.Background()).GPUs[0]
	if gpu.Source != "debugfs" || !gpu.HuC.Authenticated || gpu.GuC.Version != "70.1.1" {
		t.Errorf("debugfs: %+v", gpu)
	}

	if err := g.Set("bogus"); !errors.Is(err, ErrGucMode) {
		t.Errorf("bogus: %v", err)
	}
	if err := g.Set("huc"); err != nil {
		t.Fatal(err)
	}
	if boot.values[GucParam] != "2" || boot.writes != 1 {
		t.Errorf("boot: %+v", boot)
	}
	var stored IntelGucSettings
	if err := g.store.ReadJSON(intelGucConfig, &stored); err != nil || stored.Mode != "huc" {
		t.Errorf("stored: %+v %v", stored, err)
	}
	status = g.Status(context.Background())
	if status.Mode != "huc" || !status.Managed || !status.RestartNeeded || status.Entries["a"] != "2" {
		t.Errorf("after set: %+v", status)
	}
	// The same mode again writes nothing.
	if err := g.Set("huc"); err != nil || boot.writes != 1 {
		t.Errorf("again: %v %d", err, boot.writes)
	}
	// After the restart the parameter is active.
	os.WriteFile(filepath.Join(root, "proc/cmdline"), []byte("boot=live i915.enable_guc=2\n"), 0o644)
	if status := g.Status(context.Background()); status.Active != "2" || status.RestartNeeded {
		t.Errorf("after restart: %+v", status)
	}
	if err := g.Set("guc"); err != nil || boot.values[GucParam] != "3" {
		t.Errorf("guc: %v %+v", err, boot)
	}
	if err := g.Set("auto"); err != nil || boot.values[GucParam] != "" {
		t.Errorf("auto: %v %+v", err, boot)
	}

	// A new titand reads the setting and makes the entries follow it.
	next, nextBoot, _ := intelSystem(t, "5912", "i915")
	next.store = g.store
	if err := next.Load(); err != nil || next.settings == nil || next.settings.Mode != "auto" {
		t.Fatalf("load: %v %+v", err, next.settings)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	nextBoot.values[GucParam] = "3" // e.g. written by hand or an older system
	go func() { next.Run(ctx); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		next.mu.Lock()
		read := next.entriesRead
		next.mu.Unlock()
		if read {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if nextBoot.values[GucParam] != "" {
		t.Errorf("not reconciled: %+v", nextBoot)
	}
}

func TestIntelGucOnlyForGen9To11OnI915(t *testing.T) {
	// Alder Lake: the kernel loads GuC and HuC itself.
	g, boot, _ := intelSystem(t, "4680", "i915")
	g.Load()
	status := g.Status(context.Background())
	if len(status.GPUs) != 1 || status.GPUs[0].Applies || status.GPUs[0].Platform != "" {
		t.Errorf("alder lake: %+v", status.GPUs)
	}
	if err := g.Set("huc"); !errors.Is(err, ErrNoIntelGen9) || boot.writes != 0 {
		t.Errorf("set: %v %d", err, boot.writes)
	}
	// Automatic is always allowed: it only takes the parameter out.
	if err := g.Set("auto"); err != nil {
		t.Errorf("auto: %v", err)
	}

	// Skylake handed to a VM: known, but the setting does nothing for it.
	g, _, _ = intelSystem(t, "1912", "vfio-pci")
	g.Load()
	if gpu := g.Status(context.Background()).GPUs[0]; gpu.Applies || gpu.Name != "Skylake" || gpu.Source != "" {
		t.Errorf("vfio: %+v", gpu)
	}
	if err := g.Set("huc"); !errors.Is(err, ErrNoIntelGen9) {
		t.Errorf("vfio set: %v", err)
	}

	// Development mode never writes.
	g, boot, _ = intelSystem(t, "1912", "i915")
	g.Disabled = true
	g.Load()
	if err := g.Set("huc"); !errors.Is(err, ErrBootUnavailable) || boot.writes != 0 {
		t.Errorf("dev: %v", err)
	}
	if g.Status(context.Background()).Writable {
		t.Error("writable in development mode")
	}
}

func TestIntelGucKeepsTheOldSettingIfTheStickFails(t *testing.T) {
	g, boot, _ := intelSystem(t, "1912", "i915")
	g.Load()
	boot.err = errors.New("stick gone")
	if err := g.Set("huc"); err == nil {
		t.Fatal("no error")
	}
	if g.settings != nil {
		t.Errorf("setting changed: %+v", g.settings)
	}
	var stored IntelGucSettings
	if err := g.store.ReadJSON(intelGucConfig, &stored); err == nil {
		t.Errorf("stored although the entries were not written: %+v", stored)
	}
}

func TestIntelGucFallsBackToTheKernelLog(t *testing.T) {
	g, _, _ := intelSystem(t, "591c", "i915")
	g.runner = journalRunner{log: fixture(t, "journal.kbl")}
	g.Load()
	gpu := g.Status(context.Background()).GPUs[0]
	if gpu.Source != "journal" || !gpu.HuC.Authenticated || gpu.HuC.Version != "4.0.0" || len(gpu.Messages) != 1 {
		t.Errorf("journal: %+v", gpu)
	}
}

// From Meteor Lake on the HuC belongs to the media GT: gt0 calls it not
// supported, gt1 runs it (titan, Arrow Lake, 01.11.2026).
func TestIntelGucReadsEveryGT(t *testing.T) {
	g, _, root := intelSystem(t, "7d67", "i915")
	if err := g.Load(); err != nil {
		t.Fatal(err)
	}
	for _, gt := range []string{"gt0", "gt1"} {
		uc := filepath.Join(root, "debug/dri/0", gt, "uc")
		os.MkdirAll(uc, 0o755)
		for _, name := range []string{"huc_info", "guc_info", "usage"} {
			os.WriteFile(filepath.Join(uc, name), []byte(fixture(t, filepath.Join("arl-titan", gt, name))), 0o644)
		}
	}
	gpu := g.Status(context.Background()).GPUs[0]
	if gpu.Source != "debugfs" || gpu.HuC.State != "RUNNING" || !gpu.HuC.Authenticated ||
		gpu.HuC.Loaded != "i915/mtl_huc_gsc.bin" || gpu.HuC.Version != "8.5.4" {
		t.Errorf("huc: %+v", gpu.HuC)
	}
	if gpu.GuC.State != "RUNNING" || gpu.GuC.Version != "70.36.0" || gpu.SubmissionUsed == nil || !*gpu.SubmissionUsed {
		t.Errorf("guc: %+v %v", gpu.GuC, gpu.SubmissionUsed)
	}

	// Only gt0 (older kernels and Gen9 to Gen12): as before.
	os.RemoveAll(filepath.Join(root, "debug/dri/0/gt1"))
	if gpu := g.Status(context.Background()).GPUs[0]; gpu.HuC.State != "disabled" || gpu.HuC.Authenticated {
		t.Errorf("gt0 only: %+v", gpu.HuC)
	}
}

// titan's kernel log: the HuC runs; the late GSC proxy is worth showing.
// The mei line belongs to the MEI device it names first.
func TestParseKernelLogArrowLake(t *testing.T) {
	gpu := IntelGPU{Address: "0000:00:02.0", Messages: []string{}}
	parseKernelLog(fixture(t, "journal.arl"), &gpu)
	if gpu.Source != "journal" || !gpu.HuC.Authenticated || gpu.HuC.Loaded != "i915/mtl_huc_gsc.bin" || gpu.HuC.Version != "8.5.4" {
		t.Errorf("huc: %+v", gpu)
	}
	if len(gpu.Messages) != 2 || !strings.Contains(gpu.Messages[0], "GSC proxy component didn't bind") {
		t.Errorf("messages: %q", gpu.Messages)
	}
}
