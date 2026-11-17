package nvr

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/docker"
)

// promptly fails the test if call does not return promptly.
func promptly(t *testing.T, what string, call func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { call(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("%s blocked", what)
	}
}

// N-102-25: every request to Frigate carries the secret; it does not wait
// for m.mu, which status, media and the loop share.
func TestFrigateSecretDoesNotWaitForTheManager(t *testing.T) {
	r := seeded(t)
	want := r.m.Settings().ProxySecret
	r.m.mu.Lock()
	promptly(t, "secret for Frigate", func() {
		if got := r.m.proxySecret(); got != want || got == "" {
			t.Errorf("secret %q, want %q", got, want)
		}
	})
	r.m.mu.Unlock()
}

// N-102-25: the cameras are written to the stick without m.mu held, so a
// slow stick does not hold status and media up.
func TestCamerasAreWrittenWithoutTheManagerLock(t *testing.T) {
	r := seeded(t)
	held := atomic.Int32{}
	r.store.OnWriteError = func(error) {
		if r.m.mu.TryLock() {
			r.m.mu.Unlock()
		} else {
			held.Add(1)
		}
	}
	dir := filepath.Dir(filepath.Join(r.store.Dir(), camerasFile))
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	c := r.m.Cameras()[0]
	c.Name = "Neuer Name"
	if _, err := r.m.UpdateCamera(context.Background(), c.ID, c, CameraOptions{}); err == nil {
		t.Fatal("a camera was stored on a stick that cannot be written")
	}
	if held.Load() != 0 {
		t.Fatal("the stick was written with m.mu held")
	}
	if r.m.Cameras()[0].Name == "Neuer Name" {
		t.Fatal("a change that was not stored is in use")
	}
}

// N-102-26: deleting recordings may take Frigate longer than the 15 s of
// its client; titand waits for the answer (its own context bounds it)
// instead of reporting a failure while Frigate goes on deleting.
func TestSlowDeletesOfFrigateAreWaitedFor(t *testing.T) {
	limit := frigateRequestLimit
	frigateRequestLimit = 50 * time.Millisecond
	t.Cleanup(func() { frigateRequestLimit = limit })
	r, _ := capRig(t)
	r.frigate.Set(func(f *DevFrigate) { f.SlowDelete = 200 * time.Millisecond })
	r.limit(15)
	st := r.storageLook()
	if st.Cap != CapOK || len(r.deleted()) == 0 {
		t.Fatalf("cap after slow deletes: %s, deleted %v", st.Cap, r.deleted())
	}
	// The recordings of a removed camera, deleted in steps.
	if err := r.m.RemoveCamera("einfahrt"); err != nil {
		t.Fatal(err)
	}
	r.look()
	r.look()
	if err := r.m.PurgeDeletedCamera(context.Background(), "einfahrt"); err != nil {
		t.Fatalf("purge with slow deletes: %v", err)
	}
}

// N-102-27: a kept event that began long before the hour and more kept
// events than one answer of Frigate holds are all seen.
func TestEveryKeptEventIsSeen(t *testing.T) {
	r := seeded(t)
	base, _ := r.m.base()
	from, to := 100000.0, 103600.0
	long := from + 600
	events := []devEvent{}
	// 600 kept events after the hour, newest first as Frigate answers.
	for i := range 600 {
		end := to + 2000 - float64(i)
		events = append(events, devEvent{ID: "n" + strings.Repeat("x", i%3) + string(rune('a'+i%26)), Camera: "haustuer", Start: end - 1, End: &end, Kept: true})
	}
	// Kept from eight hours before the hour until ten minutes into it.
	events = append(events, devEvent{ID: "long", Camera: "haustuer", Start: from - 8*3600, End: &long, Kept: true})
	r.frigate.Set(func(f *DevFrigate) { f.data.events = events })
	spans, known := r.m.withoutKept(context.Background(), base, from, to)
	if !known {
		t.Fatal("kept events not known")
	}
	if len(spans) != 1 || spans[0][0] != long+keptMargin || spans[0][1] != to {
		t.Fatalf("free spans %v", spans)
	}
}

// N-102-28: Enable and Disable change the settings as they are when the
// change is made, not as they were when the call began.
func TestEnableKeepsAChangeMadeMeanwhile(t *testing.T) {
	r := newRig(t, true)
	r.m.settingsMu.Lock()
	done := make(chan error, 1)
	go func() { done <- r.m.Enable(context.Background(), "", SetOptions{}) }()
	time.Sleep(50 * time.Millisecond)
	// Another change was stored while Enable waited.
	r.m.mu.Lock()
	r.m.settings.Storage.LimitGB = 7
	r.m.mu.Unlock()
	r.m.settingsMu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s := r.m.Settings(); !s.Enabled || s.Storage.LimitGB != 7 {
		t.Fatalf("settings after Enable: enabled %v, limit %d", s.Enabled, s.Storage.LimitGB)
	}
}

// N-102-28: a camera answering with an endless header line is cut off.
func TestRTSPAnswerLinesAreBounded(t *testing.T) {
	answer := "RTSP/1.0 200 OK\r\nX-Junk: " + strings.Repeat("a", 1<<20) + "\r\n\r\n"
	if _, _, _, err := readRTSPResponse(bufio.NewReader(strings.NewReader(answer))); err == nil {
		t.Fatal("a header line of 1 MiB was read")
	}
	ok := "RTSP/1.0 401 Unauthorized\r\nWWW-Authenticate: Digest realm=\"x\", nonce=\"y\"\r\nCSeq: 1\r\n\r\n"
	status, head, _, err := readRTSPResponse(bufio.NewReader(strings.NewReader(ok)))
	if err != nil || status != 401 || !strings.HasPrefix(head["www-authenticate"], "Digest") {
		t.Fatalf("%d %v %v", status, head, err)
	}
}

// N-102-28: a seen.json that could not be read is read again later, not
// only after a restart of titand.
func TestUnreadableSeenIsReadAgainLater(t *testing.T) {
	r := seeded(t)
	if err := os.WriteFile(filepath.Join(r.dir, seenFile), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.m.MarkSeen("anna", Event{ID: "x", Start: 1}, true); !errors.Is(err, ErrSeenUnreadable) {
		t.Fatalf("unreadable: %v", err)
	}
	os.Remove(filepath.Join(r.dir, seenFile))
	r.advance(10 * time.Minute)
	if err := r.m.MarkSeen("anna", Event{ID: "x", Start: 1}, true); err != nil {
		t.Fatalf("after the file was fixed: %v", err)
	}
}

// N-102-28: a container named like the NVR's that is not its own is
// reported once, not in every round.
func TestForeignContainerIsReportedOnce(t *testing.T) {
	r := newRig(t, true)
	calls := 0
	r.engine.RemoveError = func() error { calls++; return docker.ErrNotNVR }
	for range 4 {
		r.advance(time.Minute)
		r.look()
	}
	if calls != 1 {
		t.Fatalf("the foreign container was looked at %d times", calls)
	}
}

// N-102-28: the look at the space (up to two minutes) runs beside the
// loop, which goes on with its rounds meanwhile.
func TestStorageLookDoesNotHoldTheLoop(t *testing.T) {
	r, _ := capRig(t)
	release := make(chan struct{})
	r.frigate.Set(func(f *DevFrigate) { f.HoldPath, f.HoldUntil = "/api/recordings/storage", release })
	r.advance(storageEvery + time.Second)
	promptly(t, "round of the loop", func() { r.m.reconcile(context.Background()) })
	promptly(t, "next round of the loop", func() { r.m.reconcile(context.Background()) })
	close(release)
	r.storageDone()
}
