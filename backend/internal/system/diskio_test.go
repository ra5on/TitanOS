package system

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiskRates(t *testing.T) {
	dir := t.TempDir()
	stat := func(name, line string) {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name, "stat"), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stat("sda", "  100 0 2000 0 50 0 4000 0 0 300 0 0 0 0 0 0 0\n")
	stat("loop0", "1 0 10 0 1 0 10 0 0 1 0\n")
	stat("md127", "garbage\n")
	before := readDiskCounters(dir)
	if len(before) != 1 {
		t.Fatalf("read %v", before)
	}
	stat("sda", "  100 0 6000 0 50 0 8096 0 0 1300 0 0 0 0 0 0 0\n")
	got := diskRates(before, readDiskCounters(dir), 2)
	if len(got) != 1 || got[0].Name != "sda" || got[0].ReadBytesPerSecond != 4000*512/2 || got[0].WriteBytesPerSecond != 4096*512/2 || got[0].BusyPercent != 50 {
		t.Fatalf("rates %+v", got)
	}
	// A counter that went back (new device, same name) gives no value.
	if got := diskRates(map[string]diskCounters{"sda": {9e9, 0, 0}}, readDiskCounters(dir), 2); len(got) != 0 {
		t.Fatalf("went back: %+v", got)
	}
}
