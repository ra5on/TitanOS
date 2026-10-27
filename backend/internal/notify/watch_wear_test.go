package notify

import (
	"path/filepath"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/storage"
)

func TestDiskWearIsReportedWhenItGrows(t *testing.T) {
	m, _ := newManager(t)
	w := NewWatcher(m)
	w.wornFile = filepath.Join(t.TempDir(), "wear.json")
	look := func(warnings []string, defects uint64) {
		w.Observe(storage.Status{Disks: []storage.Disk{{Name: "sda", Model: "HDD", Serial: "S1", Smart: &storage.Smart{Healthy: true}, SmartWarnings: warnings, SmartDefects: defects}}})
	}
	count := func() int {
		n := 0
		for _, message := range m.Messages() {
			if message.Kind == "disk_wear" {
				n++
			}
		}
		return n
	}
	look(nil, 0)
	look([]string{storage.SmartWarnTemperature}, 0) // warmth has its own message
	if count() != 0 {
		t.Fatal("reported without wear")
	}
	look([]string{storage.SmartWarnReallocated}, 8)
	look([]string{storage.SmartWarnReallocated}, 8)
	if count() != 1 {
		t.Fatalf("first sign: %d messages", count())
	}
	look([]string{storage.SmartWarnReallocated}, 12) // grew
	look([]string{storage.SmartWarnReallocated, storage.SmartWarnPending}, 12)
	if count() != 3 {
		t.Fatalf("growth and new warning: %d messages", count())
	}
	// A restart of titand remembers what was reported.
	w2 := NewWatcher(m)
	w2.wornFile = w.wornFile
	w2.Observe(storage.Status{Disks: []storage.Disk{{Name: "sda", Serial: "S1", Smart: &storage.Smart{Healthy: true}, SmartWarnings: []string{storage.SmartWarnReallocated, storage.SmartWarnPending}, SmartDefects: 12}}})
	if count() != 3 {
		t.Fatal("repeated after a restart")
	}
}
