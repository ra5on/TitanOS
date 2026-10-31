package logkeep

import (
	"context"
	"os"
	"path/filepath"
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
