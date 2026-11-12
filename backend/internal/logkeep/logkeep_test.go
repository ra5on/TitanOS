package logkeep

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type journal struct {
	mu    sync.Mutex
	calls []string
	out   string
}

func (j *journal) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.calls = append(j.calls, strings.Join(args, " "))
	return j.out, nil
}

func manager(t *testing.T, base, boot string, j *journal) *Manager {
	m := New(j)
	m.boot = boot
	m.pstore = filepath.Join(base, "pstore")
	m.Target = func() (string, bool) { return base, true }
	m.started = time.Date(2026, 10, 31, 8, 0, 0, 0, time.UTC)
	return m
}

func TestAppendsWithCursorAndFindsUncleanStart(t *testing.T) {
	base := t.TempDir()
	j := &journal{out: "2026-10-31T09:00:00+01:00 nas titand[1]: started\n-- cursor: s=1;i=1\n"}
	first := manager(t, base, "aaa", j)
	first.flush(context.Background())
	j.out = "2026-10-31T09:01:00+01:00 nas kernel: something\n-- cursor: s=1;i=2\n"
	first.flush(context.Background())
	if !strings.Contains(j.calls[1], "--after-cursor=s=1;i=1") {
		t.Fatalf("second read without cursor: %s", j.calls[1])
	}
	data, _ := os.ReadFile(filepath.Join(base, Dir, "aaa.log"))
	if strings.Count(string(data), "\n") != 2 || strings.Contains(string(data), "cursor") {
		t.Fatalf("log: %q", data)
	}
	// The power goes: no finish. The next start finds the marker.
	os.MkdirAll(filepath.Join(base, "pstore", "dmesg"), 0o700)
	os.WriteFile(filepath.Join(base, "pstore", "dmesg", "dmesg-1"), []byte("Kernel panic"), 0o600)
	var got []Unclean
	var mu sync.Mutex
	second := manager(t, base, "bbb", &journal{out: "-- No entries --\n"})
	second.OnUnclean = func(u Unclean) { mu.Lock(); got = append(got, u); mu.Unlock() }
	second.flush(context.Background())
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	if len(got) != 1 || got[0].Boot != "aaa" || !got[0].Crash || !got[0].Started.Equal(first.started) {
		t.Fatalf("unclean: %+v", got)
	}
	mu.Unlock()
	if text, err := second.Previous(); err != nil || !strings.Contains(text, "started") {
		t.Fatalf("previous: %q %v", text, err)
	}
	// A clean end leaves no marker; the start after it reports nothing.
	second.finish()
	third := manager(t, base, "ccc", &journal{})
	third.OnUnclean = func(u Unclean) { t.Errorf("clean start reported: %+v", u) }
	third.flush(context.Background())
	time.Sleep(20 * time.Millisecond)
}

func TestRestartOfTitandIsNoUncleanStart(t *testing.T) {
	base := t.TempDir()
	a := manager(t, base, "same", &journal{})
	a.flush(context.Background())
	// An app update restarts titand within the same start of the system.
	b := manager(t, base, "same", &journal{})
	b.OnUnclean = func(u Unclean) { t.Errorf("reported: %+v", u) }
	b.flush(context.Background())
	time.Sleep(20 * time.Millisecond)
}

func TestPruneKeepsTheCurrentStart(t *testing.T) {
	dir := t.TempDir()
	for i := range maxStarts + 5 {
		name := filepath.Join(dir, strings.Repeat("x", 3)+string(rune('a'+i%26))+string(rune('a'+i/26))+".log")
		os.WriteFile(name, []byte("line\n"), 0o600)
		os.Chtimes(name, time.Now(), time.Now().Add(time.Duration(i)*time.Minute))
	}
	os.WriteFile(filepath.Join(dir, "now.log"), []byte("x"), 0o600)
	os.Chtimes(filepath.Join(dir, "now.log"), time.Now(), time.Now().Add(-time.Hour))
	prune(dir, "now")
	entries, _ := os.ReadDir(dir)
	if len(entries) != maxStarts+1 {
		t.Fatalf("kept %d", len(entries))
	}
	if _, err := os.Stat(filepath.Join(dir, "now.log")); err != nil {
		t.Fatal("current start removed")
	}
}

func reports(m *Manager) func() []Unclean {
	var mu sync.Mutex
	var got []Unclean
	m.OnUnclean = func(u Unclean) { mu.Lock(); got = append(got, u); mu.Unlock() }
	return func() []Unclean {
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(got)
	}
}

// A restart asked for in TitanOS seals the start: the next start reports
// nothing, even if titand is killed afterwards before its own last write.
func TestRequestedRestartIsClean(t *testing.T) {
	base := t.TempDir()
	first := manager(t, base, "aaa", &journal{out: "line\n-- cursor: c1\n"})
	first.flush(context.Background())
	if err := first.Seal(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Lines that come later still go to the log, but no marker comes back.
	first.flush(context.Background())
	if _, err := os.Stat(filepath.Join(base, Dir, marker)); err == nil {
		t.Fatal("marker after Seal")
	}
	second := manager(t, base, "bbb", &journal{})
	got := reports(second)
	second.flush(context.Background())
	if u := got(); len(u) != 0 {
		t.Fatalf("clean restart reported: %+v", u)
	}
}

// A power cut is still a power cut, also after a restart that failed.
func TestPowerCutAfterAFailedRestartIsReported(t *testing.T) {
	base := t.TempDir()
	first := manager(t, base, "aaa", &journal{})
	first.flush(context.Background())
	first.Seal(context.Background())
	first.Rearm() // systemctl refused: the NAS runs on
	second := manager(t, base, "bbb", &journal{})
	got := reports(second)
	second.flush(context.Background())
	if u := got(); len(u) != 1 || u[0].Boot != "aaa" {
		t.Fatalf("power cut not reported: %+v", u)
	}
}

// Before 0.59.2 the marker was removed in whatever folder was current at
// the end, silently. A volume that is no longer mounted (the folder is
// gone) must not count as cleanly ended: the next start reports it.
func TestSealFailsWhenTheVolumeIsGone(t *testing.T) {
	disk, empty := t.TempDir(), t.TempDir()
	first := manager(t, disk, "aaa", &journal{})
	first.flush(context.Background())
	// Unmounted: the folder below the mount point is gone.
	hidden := filepath.Join(empty, "hidden")
	if err := os.Rename(filepath.Join(disk, ".titanos"), hidden); err != nil {
		t.Fatal(err)
	}
	first.Target = func() (string, bool) { return "", false }
	if err := first.Seal(context.Background()); err == nil {
		t.Fatal("Seal claims a clean end without a volume")
	}
	first.finish()
	os.Rename(hidden, filepath.Join(disk, ".titanos"))
	second := manager(t, disk, "bbb", &journal{})
	got := reports(second)
	second.flush(context.Background())
	if u := got(); len(u) != 1 {
		t.Fatalf("not reported: %+v", u)
	}
}

// The volume for the logs can change while the NAS runs: "archive" is
// mounted first, "media" (encrypted, sorts first) only once it is opened.
// Before 0.59.2 the marker on "archive" stayed, and every restart was
// reported as unclean when "archive" came up first the next time.
func TestMarkerFollowsTheVolume(t *testing.T) {
	archive, media := t.TempDir(), t.TempDir()
	first := manager(t, archive, "aaa", &journal{})
	first.flush(context.Background())
	first.Target = func() (string, bool) { return media, true }
	first.flush(context.Background())
	if _, err := os.Stat(filepath.Join(archive, Dir, marker)); err == nil {
		t.Fatal("marker left on the volume used before")
	}
	if err := first.Seal(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, base := range []string{archive, media} {
		next := manager(t, base, "bbb", &journal{})
		got := reports(next)
		next.flush(context.Background())
		if u := got(); len(u) != 0 {
			t.Fatalf("%s: clean restart reported: %+v", base, u)
		}
	}
}

// Without a request (power button, UPS, systemctl in a console) the end is
// recorded when titand stops: Run writes, removes the marker and says so.
func TestStopOfTitandEndsTheStartCleanly(t *testing.T) {
	base := t.TempDir()
	j := &journal{out: "last line\n-- cursor: c9\n"}
	m := manager(t, base, "aaa", j)
	ctx, cancel := context.WithCancel(context.Background())
	go m.Run(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(base, Dir, marker)); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no marker")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-m.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not finish")
	}
	if _, err := os.Stat(filepath.Join(base, Dir, marker)); err == nil {
		t.Fatal("marker after a clean stop")
	}
}

// Locking the encrypted volume that holds the logs takes this start's
// marker away first; the logs go on on another volume.
func TestLockingTheLogVolumeTakesTheMarkerAlong(t *testing.T) {
	media, archive := t.TempDir(), t.TempDir()
	m := manager(t, media, "aaa", &journal{})
	m.flush(context.Background())
	m.LetGo(media)
	if _, err := os.Stat(filepath.Join(media, Dir, marker)); err == nil {
		t.Fatal("marker left on the locked volume")
	}
	m.Target = func() (string, bool) { return archive, true }
	m.flush(context.Background())
	if _, err := os.Stat(filepath.Join(archive, Dir, marker)); err != nil {
		t.Fatal("no marker on the volume used now")
	}
	if err := m.Seal(context.Background()); err != nil {
		t.Fatal(err)
	}
	next := manager(t, media, "bbb", &journal{})
	got := reports(next)
	next.flush(context.Background())
	if u := got(); len(u) != 0 {
		t.Fatalf("reported: %+v", u)
	}
}

// While the folder is being moved to another volume nothing is written
// (the lines come with the next write), and after the move the marker of
// this start is known only at the new place: sealing does not fail for a
// folder that is gone.
func TestHoldsTheFolderAndForgetsAMovedOne(t *testing.T) {
	old, now := t.TempDir(), t.TempDir()
	j := &journal{out: "2026-10-31T09:00:00+01:00 nas titand[1]: one\n-- cursor: s=1;i=1\n"}
	m := manager(t, old, "aaa", j)
	base, held, released := old, true, 0
	m.Target = func() (string, bool) { return base, true }
	m.Hold = func() (func(), bool) { return func() { released++ }, held }
	m.flush(context.Background())
	if released != 1 {
		t.Fatalf("folder let go %d times after a write", released)
	}
	// The move runs: no write, the cursor stays.
	held = false
	j.out = "2026-10-31T09:01:00+01:00 nas titand[1]: two\n-- cursor: s=1;i=2\n"
	m.flush(context.Background())
	if data, _ := os.ReadFile(filepath.Join(old, Dir, "aaa.log")); strings.Contains(string(data), "two") || len(j.calls) != 1 {
		t.Fatalf("written while the folder was being moved: %q", data)
	}
	// Moved: log and marker are at the new place, the old folder is gone.
	if err := os.MkdirAll(filepath.Join(now, ".titanos"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(old, Dir), filepath.Join(now, Dir)); err != nil {
		t.Fatal(err)
	}
	m.Forget(old)
	base, held = now, true
	m.flush(context.Background())
	data, _ := os.ReadFile(filepath.Join(now, Dir, "aaa.log"))
	if !strings.Contains(string(data), "one") || !strings.Contains(string(data), "two") {
		t.Fatalf("log after the move: %q", data)
	}
	if err := m.Seal(context.Background()); err != nil {
		t.Fatalf("sealing failed for the folder that was moved away: %v", err)
	}
	if _, err := os.Stat(filepath.Join(now, Dir, marker)); !os.IsNotExist(err) {
		t.Fatal("marker left at the new place after sealing")
	}
}
