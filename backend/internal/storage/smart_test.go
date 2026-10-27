package storage

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestParseSmartValuesSATA(t *testing.T) {
	values := parseSmartValues(readTestdata(t, "smart-sata-hdd.json"))
	if values == nil {
		t.Fatal("no values")
	}
	if values.Protocol != "ATA" || values.Firmware != "81.00B81" || values.ModelFamily != "Western Digital Red Plus" || !values.Passed || values.RotationRate != 5400 {
		t.Errorf("identity: %+v", values)
	}
	if values.TemperatureC == nil || *values.TemperatureC != 34 || values.PowerOnHours == nil || *values.PowerOnHours != 21408 || values.PowerCycles == nil || *values.PowerCycles != 97 {
		t.Errorf("temperature/hours/cycles: %+v", values)
	}
	for name, got := range map[string]*uint64{"reallocated": values.ReallocatedSectors, "pending": values.PendingSectors, "uncorrectable": values.UncorrectableSectors, "crc": values.CRCErrors} {
		want := map[string]uint64{"reallocated": 8, "pending": 1, "uncorrectable": 0, "crc": 3}[name]
		if got == nil || *got != want {
			t.Errorf("%s = %v, want %d", name, got, want)
		}
	}
	if len(values.Attributes) != 12 || values.Attributes[3].ID != 5 || values.Attributes[3].Name != "Reallocated_Sector_Ct" || values.Attributes[3].Threshold != 140 || !values.Attributes[3].Prefailure || values.Attributes[3].Raw != "8" || values.Attributes[3].Flags != "PO--CK" {
		t.Errorf("attributes: %+v", values.Attributes)
	}
	if values.NVMe != nil {
		t.Error("SATA disk with NVMe log")
	}

	state, warnings := judgeSmart(values, "hdd")
	if state != SmartWarning || !slices.Equal(warnings, []string{SmartWarnReallocated, SmartWarnPending, SmartWarnCRC}) {
		t.Errorf("judged %s %v", state, warnings)
	}
	var flagged []int
	for _, a := range values.Attributes {
		if a.Warn {
			flagged = append(flagged, a.ID)
		}
	}
	if !slices.Equal(flagged, []int{5, 197, 199}) {
		t.Errorf("flagged attributes %v", flagged)
	}
}

func TestParseSmartValuesNVMe(t *testing.T) {
	values := parseSmartValues(readTestdata(t, "smart-nvme.json"))
	if values == nil || values.Protocol != "NVMe" || values.NVMe == nil {
		t.Fatalf("values: %+v", values)
	}
	n := values.NVMe
	if n.PercentageUsed != 4 || n.AvailableSpare != 100 || n.AvailableSpareThreshold != 10 || n.MediaErrors != 0 || n.ErrorLogEntries != 5 || n.UnsafeShutdowns != 23 || n.DataUnitsWritten != 27182818 {
		t.Errorf("nvme: %+v", n)
	}
	if *values.TemperatureC != 41 || *values.PowerOnHours != 9812 || *values.PowerCycles != 412 {
		t.Errorf("values: %+v", values)
	}
	if values.ReallocatedSectors != nil || values.CRCErrors != nil {
		t.Error("NVMe has no ATA counters")
	}
	if len(values.Attributes) != len(nvmeFields) || values.Attributes[0].Name != "critical_warning" || values.Attributes[len(values.Attributes)-1].Raw != "[41,47]" {
		t.Errorf("health log table: %+v", values.Attributes)
	}
	if state, warnings := judgeSmart(values, "nvme"); state != SmartOK || len(warnings) != 0 {
		t.Errorf("judged %s %v", state, warnings)
	}
}

func TestJudgeSmartThresholds(t *testing.T) {
	temp := func(c int) *int { return &c }
	zero := uint64(0)
	for _, tc := range []struct {
		name   string
		values *SmartValues
		kind   string
		state  string
		warns  []string
	}{
		{"none", nil, "hdd", SmartUnavailable, nil},
		{"healthy", &SmartValues{Passed: true, TemperatureC: temp(54), ReallocatedSectors: &zero}, "hdd", SmartOK, nil},
		{"failed", &SmartValues{Passed: false}, "hdd", SmartFailed, []string{SmartWarnFailed}},
		{"hot hdd", &SmartValues{Passed: true, TemperatureC: temp(55)}, "hdd", SmartWarning, []string{SmartWarnTemperature}},
		{"warm nvme", &SmartValues{Passed: true, TemperatureC: temp(69), NVMe: &SmartNVMe{}}, "nvme", SmartOK, nil},
		{"hot nvme", &SmartValues{Passed: true, TemperatureC: temp(70), NVMe: &SmartNVMe{}}, "nvme", SmartWarning, []string{SmartWarnTemperature}},
		{"worn", &SmartValues{Passed: true, NVMe: &SmartNVMe{PercentageUsed: 90}}, "nvme", SmartWarning, []string{SmartWarnWear}},
		{"media errors", &SmartValues{Passed: true, NVMe: &SmartNVMe{MediaErrors: 1}}, "nvme", SmartWarning, []string{SmartWarnMediaErrors}},
		{"spare", &SmartValues{Passed: true, NVMe: &SmartNVMe{AvailableSpare: 10, AvailableSpareThreshold: 10}}, "nvme", SmartWarning, []string{SmartWarnSpare}},
		{"critical temperature", &SmartValues{Passed: true, NVMe: &SmartNVMe{CriticalWarning: 0x02}}, "nvme", SmartWarning, []string{SmartWarnNVMeCritical}},
		{"read only", &SmartValues{Passed: true, NVMe: &SmartNVMe{CriticalWarning: 0x08}}, "nvme", SmartFailed, []string{SmartWarnNVMeCritical}},
		{"failing now", &SmartValues{Passed: true, Attributes: []SmartAttribute{{ID: 3, Raw: "1", WhenFailed: "now"}}}, "hdd", SmartFailed, []string{SmartWarnAttribute}},
		{"failed in the past", &SmartValues{Passed: true, Attributes: []SmartAttribute{{ID: 3, Raw: "1", WhenFailed: "past"}}}, "hdd", SmartWarning, []string{SmartWarnAttributePast}},
	} {
		state, warnings := judgeSmart(tc.values, tc.kind)
		if state != tc.state || !slices.Equal(warnings, append([]string{}, tc.warns...)) {
			t.Errorf("%s: %s %v, want %s %v", tc.name, state, warnings, tc.state, tc.warns)
		}
	}
}

func TestSmartSleeping(t *testing.T) {
	if !smartSleeping(readTestdata(t, "smart-standby.json")) {
		t.Error("standby answer not recognized")
	}
	for _, awake := range []string{readTestdata(t, "smart-sata-hdd.json"), "", "garbage",
		`{"smartctl":{"exit_status":2,"messages":[{"string":"Smartctl open device: /dev/sdx failed: No such device"}]}}`} {
		if smartSleeping(awake) {
			t.Errorf("taken for sleeping: %.60q", awake)
		}
	}
	if parseSmartValues(readTestdata(t, "smart-standby.json")) != nil {
		t.Error("values from a skipped disk")
	}
}

// smartRunner answers smartctl per disk from a table; it counts calls.
type smartRunner struct {
	*fakeRunner
	answers map[string]string
	calls   int
}

func (r *smartRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if name == "smartctl" {
		r.calls++
		if !slices.Contains(args, "--nocheck=standby") {
			panic("SMART query may wake a disk: " + strings.Join(args, " "))
		}
		return r.answers[args[len(args)-1]], nil
	}
	return r.fakeRunner.Run(ctx, stdin, name, args...)
}

func TestSmartReportsKeepSleepingDisksAsleep(t *testing.T) {
	m, runner, _ := newTestManager(t)
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	m.clock = func() time.Time { return now }
	smart := &smartRunner{fakeRunner: runner, answers: map[string]string{
		"/dev/sda":     readTestdata(t, "smart-sata-hdd.json"),
		"/dev/nvme0n1": readTestdata(t, "smart-nvme.json"),
	}}
	m.runner = smart
	ctx := context.Background()

	reports, err := m.SmartReports(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 7 || reports[0].State != SmartUnknown || reports[0].CheckedAt != nil {
		t.Fatalf("before the first read: %+v", reports)
	}

	m.RefreshSmart(ctx)
	first := smart.calls
	byDisk := func() map[string]SmartReport {
		reports, err := m.SmartReports(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]SmartReport{}
		for _, r := range reports {
			got[r.Disk] = r
		}
		return got
	}
	got := byDisk()
	if r := got["sda"]; r.State != SmartWarning || r.Sleeping || r.ReadAt == nil || r.Values == nil || r.Values.Attributes != nil || r.Volume != "" || r.Serial != "A1" {
		t.Errorf("sda: %+v", r)
	}
	if r := got["nvme0n1"]; r.State != SmartOK || r.Values.NVMe == nil {
		t.Errorf("nvme: %+v", r)
	}
	if r := got["sde"]; r.State != SmartUnavailable || r.Values != nil || r.CheckedAt == nil {
		t.Errorf("disk without SMART: %+v", r)
	}

	// sda falls asleep: it is not woken, the values of before stay.
	smart.answers["/dev/sda"] = readTestdata(t, "smart-standby.json")
	smart.answers["/dev/sde"] = readTestdata(t, "smart-standby.json")
	now = now.Add(time.Minute)
	m.RefreshSmart(ctx)
	got = byDisk()
	if r := got["sda"]; !r.Sleeping || r.State != SmartWarning || !r.ReadAt.Equal(now.Add(-time.Minute)) || !r.CheckedAt.Equal(now) || r.Values == nil {
		t.Errorf("sleeping sda: %+v", r)
	}
	if r := got["sde"]; !r.Sleeping || r.State != SmartUnknown || r.ReadAt != nil {
		t.Errorf("asleep since start: %+v", r)
	}
	status, _ := m.Status(ctx)
	for _, disk := range status.Disks {
		if disk.Name == "sda" && (disk.Smart == nil || disk.Smart.TemperatureC != 34) {
			t.Errorf("status lost the values of the sleeping disk: %+v", disk.Smart)
		}
	}

	detail, err := m.SmartReport(ctx, "sda")
	if err != nil || detail.Values == nil || len(detail.Values.Attributes) != 12 {
		t.Fatalf("detail: %+v %v", detail, err)
	}
	if _, err := m.SmartReport(ctx, "sdq"); err != ErrNoDisk {
		t.Errorf("unknown disk: %v", err)
	}

	// A second refresh within 30 seconds reuses the cache.
	calls := smart.calls
	m.RefreshSmartSoon(ctx)
	if smart.calls != calls {
		t.Error("refreshed again within 30 seconds")
	}
	now = now.Add(31 * time.Second)
	m.RefreshSmartSoon(ctx)
	if smart.calls-calls != first {
		t.Errorf("%d smartctl calls, want %d", smart.calls-calls, first)
	}
}

func TestSmartDumpsHideIdentity(t *testing.T) {
	m, runner, _ := newTestManager(t)
	m.runner = &smartRunner{fakeRunner: runner, answers: map[string]string{
		"/dev/sda":     readTestdata(t, "smart-sata-hdd.json"),
		"/dev/nvme0n1": readTestdata(t, "smart-nvme.json"),
		"/dev/sdb":     readTestdata(t, "smart-standby.json"),
	}}
	dumps := m.SmartDumps(context.Background())
	if len(dumps) != 7 {
		t.Fatalf("%d dumps", len(dumps))
	}
	data, _ := json.Marshal(dumps)
	text := string(data)
	for _, secret := range []string{"WD-WX12D3456789", "S5GXNX0T123456A", "123456789012", "12345678901", "serial_number", "wwn", "eui64"} {
		if strings.Contains(text, secret) {
			t.Errorf("bundle shows %q", secret)
		}
	}
	for _, kept := range []string{"Reallocated_Sector_Ct", "percentage_used", `"sleeping":true`} {
		if !strings.Contains(text, kept) {
			t.Errorf("bundle misses %q", kept)
		}
	}
}
