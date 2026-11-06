package main

import (
	"errors"
	"slices"
	"testing"
	"time"
)

type recordPower struct {
	log *[]string
	err error
}

func (p recordPower) Reboot() error   { *p.log = append(*p.log, "systemctl reboot"); return p.err }
func (p recordPower) Poweroff() error { *p.log = append(*p.log, "systemctl poweroff"); return p.err }

// A planned restart: announce, record the end of this start, then systemd.
// If systemd refuses, the start is marked as running again.
func TestAnnouncedPowerSealsRightBeforeSystemd(t *testing.T) {
	for _, fail := range []bool{false, true} {
		var log []string
		var err error
		if fail {
			err = errors.New("refused")
		}
		p := announcedPower{Power: recordPower{&log, err},
			before: func() { log = append(log, "before") },
			seal:   func() { log = append(log, "seal") },
			rearm:  func() { log = append(log, "rearm") }}
		if got := p.Reboot(); !errors.Is(got, err) {
			t.Fatalf("err %v", got)
		}
		want := []string{"before", "seal", "systemctl reboot"}
		if fail {
			want = append(want, "rearm")
		}
		if !slices.Equal(log, want) {
			t.Errorf("fail=%v: %v", fail, log)
		}
	}
}

// titand waits for the kept journal with its own deadline, and no longer.
func TestWaitLogsIsBounded(t *testing.T) {
	done := make(chan struct{})
	close(done)
	start := time.Now()
	waitLogs(done, time.Now().Add(time.Hour))
	waitLogs(make(chan struct{}), time.Now().Add(30*time.Millisecond))
	if d := time.Since(start); d > time.Second {
		t.Fatalf("waited %v", d)
	}
}
