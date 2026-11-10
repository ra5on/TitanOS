package stickwatch

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOnlyErrorsOfTheStickAreReported(t *testing.T) {
	now := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)
	var got []string
	w := &Watcher{resolve: func() []string { return []string{"sdb1", "sdb"} }, now: func() time.Time { return now }, Report: func(what string) { got = append(got, what) }}
	w.read(strings.NewReader(`usb 2-1: new high-speed USB device number 3
I/O error, dev sdc, sector 2048 op 0x0:(READ)
sd 2:0:0:0: [sdb] tag#0 Sense Key : Medium Error [current]
I/O error, dev sdb, sector 4096 op 0x1:(WRITE)
FAT-fs (sdb1): Directory bread(block 8192) failed
`))
	if len(got) != 1 || !strings.Contains(got[0], "sdb") {
		t.Fatalf("reports: %q", got)
	}
	w.Failed(errors.New("input/output error"))
	if len(got) != 1 {
		t.Fatal("reported again within the quiet time")
	}
	now = now.Add(quiet)
	w.Failed(errors.New("input/output error"))
	if len(got) != 2 || !strings.Contains(got[1], "configuration") {
		t.Fatalf("after the quiet time: %q", got)
	}
}

// The stick drops out: its label is gone by the time the kernel reports
// the errors. They still belong to the stick.
func TestErrorsAfterTheStickDroppedOutAreReported(t *testing.T) {
	var got []string
	present := true
	w := &Watcher{now: time.Now, Report: func(what string) { got = append(got, what) }, resolve: func() []string {
		if present {
			return []string{"sdb1", "sdb"}
		}
		return nil
	}}
	w.read(strings.NewReader("usb 2-1: new high-speed USB device number 3\n"))
	present = false
	w.read(strings.NewReader("usb 2-1: USB disconnect, device number 3\nI/O error, dev sdb, sector 4096 op 0x1:(WRITE)\n"))
	if len(got) != 1 || !strings.Contains(got[0], "sdb") {
		t.Fatalf("reports: %q", got)
	}
}
