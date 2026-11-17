package ups

import (
	"context"
	"strings"
	"testing"
	"time"
)

// N-102-37: right after NUT was started anew (settings saved), upsc does
// not answer yet; that is no "UPS lost". Still no answer after the grace
// time is.
func TestFreshlyStartedNUTIsNoLostUPS(t *testing.T) {
	m, system, events := newManager(t)
	clock := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return clock }
	m.SetSettings(Settings{Mode: "local", ShutdownAfter: 5})
	online := "device.mfr: APC\ndevice.model: Back-UPS\nups.status: OL\n"
	system.upsc = online
	m.look(context.Background())
	// The owner saves other settings: NUT starts anew and does not answer
	// for a few seconds.
	m.SetSettings(Settings{Mode: "local", ShutdownAfter: 10})
	if err := m.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	system.upsc = ""
	clock = clock.Add(5 * time.Second)
	m.look(context.Background())
	if got := strings.Join(*events, ","); got != "" {
		t.Fatalf("events right after the start of NUT: %s", got)
	}
	if !m.Status().Connected {
		t.Fatal("shown as disconnected right after the start of NUT")
	}
	clock = clock.Add(time.Minute)
	m.look(context.Background())
	if got := strings.Join(*events, ","); got != "ups_lost" {
		t.Fatalf("events when NUT still does not answer: %s", got)
	}
}
