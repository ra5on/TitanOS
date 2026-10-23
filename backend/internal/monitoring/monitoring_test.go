package monitoring

import (
	"context"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordRetentionCounterResetAndCrashTail(t *testing.T) {
	m := New(config.Open(t.TempDir()))
	m.MountRoot = t.TempDir()
	m.sysBlock = t.TempDir()
	os.Mkdir(filepath.Join(m.MountRoot, "data"), 0700)
	os.Mkdir(filepath.Join(m.sysBlock, "sda"), 0700)
	stat := filepath.Join(m.sysBlock, "sda", "stat")
	os.WriteFile(stat, []byte("10 0 100 30 20 0 200 60 0 0 0"), 0600)
	m.Mounted = func() []string { return []string{"data"} }
	m.Snapshot = func() (system.Info, storage.Status) { return system.Info{}, storage.Status{} }
	if err := m.Set(Settings{true, "data", 7, 8}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Minute)
	if err := m.Record(now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(m.MountRoot, "data", ".titanos", "monitoring", now.Format("2006-01-02")+".jsonl")
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString(`{"at":`)
	f.Close()
	os.WriteFile(stat, []byte("11 0 220 35 22 0 260 70 0 0 0"), 0600)
	if err := m.Record(now); err != nil {
		t.Fatal(err)
	}
	rows, err := m.History(context.Background(), 24)
	if err != nil || len(rows) != 2 {
		t.Fatalf("%v %+v", err, rows)
	}
	if len(rows[1].Disks) != 1 || rows[1].Disks[0].ReadBPS != 1024 || *rows[1].Disks[0].LatencyMS != 5 {
		t.Fatalf("%+v", rows[1].Disks)
	}
	os.WriteFile(stat, []byte("0 0 0 0 0 0 0 0"), 0600)
	m.Record(now.Add(time.Minute))
	rows, err = m.History(context.Background(), 24)
	if err != nil || len(rows[2].Disks) != 0 {
		t.Fatal("reset counters reported as IO")
	}
	m.Mounted = func() []string { return nil }
	if err := m.Record(now.Add(2 * time.Minute)); err == nil {
		t.Fatal("missing volume accepted")
	}
}
func TestMonitoringRejectsInvalidSettingsAndCompleteCorruption(t *testing.T) {
	m := New(config.Open(t.TempDir()))
	for _, s := range []Settings{{true, "../bad", 7, 8}, {false, "", 365, 8}, {false, "", 7, 999}} {
		if err := m.Set(s); err == nil {
			t.Fatal(s)
		}
	}
	f, err := os.CreateTemp(t.TempDir(), "tail")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.WriteString("bad\npartial")
	if err := repairTail(f); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(f.Name())
	if string(data) != "bad\n" {
		t.Fatal(string(data))
	}
}
