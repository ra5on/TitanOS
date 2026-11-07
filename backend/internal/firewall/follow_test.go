package firewall

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeJournal puts a journalctl on PATH that prints a line longer than the
// reader takes and then hangs; it returns the file its PID is written to.
func fakeJournal(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	script := "#!/bin/sh\necho $$ > " + pidFile + "\nhead -c 2000000 /dev/zero | tr '\\0' a\necho\nsleep 60\n"
	if err := os.WriteFile(filepath.Join(dir, "journalctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return pidFile
}

// A line too long for the reader ends journalctl instead of leaving the
// follower waiting for it for good.
func TestFollowEndsJournalAfterLongLine(t *testing.T) {
	pidFile := fakeJournal(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	m, _, _ := newTestManager(t)
	go func() { m.FollowJournal(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(10 * time.Second)
	pid := 0
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil && pid == 0 {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		if pid > 0 && syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("journalctl (pid %d) still runs after the reader gave up", pid)
}
