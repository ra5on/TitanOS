package smarthistory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/storage"
)

func ptr[T any](v T) *T { return &v }

func setup(t *testing.T) (*Recorder, string, *[]storage.SmartReport, *time.Time) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := New(root)
	r.Mounted = func() []string { return []string{"zz", "data"} }
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.Local)
	r.now = func() time.Time { return now }
	reports := []storage.SmartReport{}
	r.Reports = func(context.Context) ([]storage.SmartReport, error) { return reports, nil }
	return r, root, &reports, &now
}

func hdd(read time.Time, temp int, realloc uint64) storage.SmartReport {
	return storage.SmartReport{Disk: "sda", Model: "WDC WD80 EFAX", Serial: "ABC/123", ReadAt: &read, Values: &storage.SmartValues{
		TemperatureC: ptr(temp), PowerOnHours: ptr(int64(1000)), ReallocatedSectors: ptr(realloc), PendingSectors: ptr(uint64(0)), CRCErrors: ptr(uint64(0)),
	}}
}

func TestOneSamplePerDayOnTheVolume(t *testing.T) {
	ctx := context.Background()
	r, root, reports, now := setup(t)
	read := *now
	nvme := storage.SmartReport{Disk: "nvme0n1", Model: "Samsung 990", Serial: "S1", ReadAt: &read, Values: &storage.SmartValues{
		TemperatureC: ptr(40), NVMe: &storage.SmartNVMe{PercentageUsed: 3, AvailableSpare: 100, MediaErrors: 0},
	}}
	noSerial := storage.SmartReport{Disk: "vda", ReadAt: &read, Values: &storage.SmartValues{}}
	*reports = []storage.SmartReport{hdd(read, 35, 0), nvme, noSerial, {Disk: "sdb", Serial: "X"}}
	if err := r.Record(ctx); err != nil {
		t.Fatal(err)
	}
	// Same day again: nothing new, even with new values.
	(*reports)[0] = hdd(read.Add(time.Hour), 50, 1)
	if err := r.Record(ctx); err != nil {
		t.Fatal(err)
	}
	// The next day the disk was read again: a second sample.
	*now = now.Add(24 * time.Hour)
	if err := r.Record(ctx); err != nil {
		t.Fatal(err)
	}
	h, err := r.History(hdd(read, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if h.Volume != "data" || len(h.Samples) != 2 || h.Samples[0].Day != "2026-10-31" || *h.Samples[0].TemperatureC != 35 || *h.Samples[1].Reallocated != 1 {
		t.Fatalf("history %+v", h)
	}
	// The NVMe SSD was not read again (asleep): no second sample.
	h, err = r.History(nvme)
	if err != nil || len(h.Samples) != 1 || *h.Samples[0].PercentageUsed != 3 || h.Samples[0].Reallocated != nil {
		t.Fatalf("nvme history %+v %v", h, err)
	}
	// Files lie in .titanos/smart of the first volume, private.
	entries, err := os.ReadDir(filepath.Join(root, "data", ".titanos", Feature))
	if err != nil || len(entries) != 2 {
		t.Fatalf("files %v %v", entries, err)
	}
	if entries[1].Name() != "wdc_wd80_efax_abc_123.json" {
		t.Fatalf("file name %s", entries[1].Name())
	}
	info, _ := os.Stat(filepath.Join(root, "data", ".titanos", Feature))
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("folder mode %v", info.Mode())
	}
	if h, _ := r.History(noSerial); len(h.Samples) != 0 {
		t.Fatal("history for a disk without serial")
	}
}

func TestHistoryIsBoundedAndSurvivesDamage(t *testing.T) {
	ctx := context.Background()
	r, root, reports, now := setup(t)
	dir := filepath.Join(root, "data", ".titanos", Feature)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	f := file{Model: "WDC WD80 EFAX", Serial: "ABC/123"}
	start := now.AddDate(0, 0, -MaxSamples)
	for i := range MaxSamples {
		day := start.AddDate(0, 0, i)
		f.Samples = append(f.Samples, Sample{Day: day.Format(time.DateOnly), ReadAt: day})
	}
	data, _ := json.Marshal(f)
	name := filepath.Join(dir, Key(f.Model, f.Serial)+".json")
	if err := os.WriteFile(name, data, 0o600); err != nil {
		t.Fatal(err)
	}
	*reports = []storage.SmartReport{hdd(*now, 30, 0)}
	if err := r.Record(ctx); err != nil {
		t.Fatal(err)
	}
	h, _ := r.History((*reports)[0])
	if len(h.Samples) != MaxSamples || h.Samples[len(h.Samples)-1].Day != "2026-10-31" || h.Samples[0].Day == start.Format(time.DateOnly) {
		t.Fatalf("%d samples, first %s", len(h.Samples), h.Samples[0].Day)
	}
	// A damaged file is started anew.
	if err := os.WriteFile(name, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(24 * time.Hour)
	(*reports)[0] = hdd(*now, 31, 0)
	if err := r.Record(ctx); err != nil {
		t.Fatal(err)
	}
	if h, _ := r.History((*reports)[0]); len(h.Samples) != 1 {
		t.Fatalf("after damage: %d samples", len(h.Samples))
	}
}

func TestNothingWithoutVolumeOrInDevelopmentMode(t *testing.T) {
	ctx := context.Background()
	r, root, reports, now := setup(t)
	*reports = []storage.SmartReport{hdd(*now, 30, 0)}
	r.Disabled = true
	if err := r.Record(ctx); err != nil {
		t.Fatal(err)
	}
	r.Disabled = false
	r.Mounted = func() []string { return nil }
	if err := r.Record(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "data", ".titanos")); !os.IsNotExist(err) {
		t.Fatal("written without a volume or in development mode")
	}
	if h, err := r.History((*reports)[0]); err != nil || h.Volume != "" || h.Samples == nil {
		t.Fatalf("history without volume %+v %v", h, err)
	}
}

func TestKey(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"ST8000", "ZA1"}:       "st8000_za1",
		{"", "ZA1"}:             "za1",
		{"ST8000", ""}:          "",
		{"../x", "../../etc"}:   "x_.._.._etc",
		{"Samsung SSD", " S1 "}: "samsung_ssd_s1",
	} {
		if got := Key(in[0], in[1]); got != want {
			t.Errorf("Key(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}
