package hardware

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func uasSystem(t *testing.T, cmdline, entry string) (*UASQuirks, *fakeBoot, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "proc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "proc", "cmdline"), []byte(cmdline+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	boot := &fakeBoot{values: map[string]string{UASParam: entry}}
	q := NewUASQuirks(config.Open(filepath.Join(root, "config")), boot, false)
	q.proc = filepath.Join(root, "proc")
	if err := q.Load(); err != nil {
		t.Fatal(err)
	}
	return q, boot, root
}

func TestUASQuirkSetKeepsForeignItems(t *testing.T) {
	q, boot, root := uasSystem(t, "quiet usb-storage.quirks=0bc2:2312:u", "0bc2:2312:u,174c:55aa:k")
	if err := q.Set("152D:0578", true); err != nil {
		t.Fatal(err)
	}
	if got := boot.values[UASParam]; got != "0bc2:2312:u,174c:55aa:k,152d:0578:u" {
		t.Fatalf("entry = %q", got)
	}
	status := q.Status()
	if !slices.Equal(status.Devices, []string{"152d:0578"}) || !slices.Equal(status.Pending, []string{"152d:0578"}) || !slices.Equal(status.Active, []string{"0bc2:2312"}) {
		t.Fatalf("status = %+v", status)
	}
	// The setting survives a restart of titand.
	again := NewUASQuirks(config.Open(filepath.Join(root, "config")), boot, false)
	if err := again.Load(); err != nil || !slices.Equal(again.settings.Devices, []string{"152d:0578"}) {
		t.Fatalf("loaded %+v, %v", again.settings, err)
	}
	if err := q.Set("152d:0578", false); err != nil {
		t.Fatal(err)
	}
	if got := boot.values[UASParam]; got != "0bc2:2312:u,174c:55aa:k" {
		t.Fatalf("entry after taking it out = %q", got)
	}
}

func TestUASQuirkReplacesHandSetItemOfSameDevice(t *testing.T) {
	q, boot, _ := uasSystem(t, "", "152d:0578:uk")
	if err := q.Set("152d:0578", true); err != nil {
		t.Fatal(err)
	}
	if got := boot.values[UASParam]; got != "152d:0578:u" {
		t.Fatalf("entry = %q", got)
	}
}

func TestUASQuirkRejects(t *testing.T) {
	q, boot, _ := uasSystem(t, "", "")
	for _, bad := range []string{"", "152d", "152d:0578:u", "zzzz:0578", "152d:0578 x"} {
		if err := q.Set(bad, true); err != ErrUASDevice {
			t.Fatalf("%q: %v", bad, err)
		}
	}
	for i := range maxUASDevices {
		if err := q.Set(fmt.Sprintf("1234:%04x", i), true); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.Set("1234:ffff", true); err != ErrUASFull {
		t.Fatalf("one too many: %v", err)
	}
	if boot.writes != maxUASDevices {
		t.Fatalf("writes = %d", boot.writes)
	}
	off := NewUASQuirks(q.store, nil, true)
	_ = off.Load()
	if err := off.Set("152d:0578", true); err != ErrBootUnavailable {
		t.Fatalf("development mode: %v", err)
	}
}

func TestActiveQuirks(t *testing.T) {
	got := activeQuirks("a usb-storage.quirks=152d:0578:u b usb_storage.quirks=0bda:9210:u,174c:55aa:k,152d:0578:u")
	if !slices.Equal(got, []string{"0bda:9210", "152d:0578"}) {
		t.Fatalf("active = %v", got)
	}
	if got := activeQuirks("quiet"); len(got) != 0 {
		t.Fatalf("none: %v", got)
	}
}
