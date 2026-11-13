package docker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The start of dockerd is read from /proc: field 22 of its stat (clock
// ticks since boot), with the time since boot.
func TestProcessStarted(t *testing.T) {
	proc := t.TempDir()
	pidFile := filepath.Join(t.TempDir(), "docker.pid")
	if err := os.WriteFile(pidFile, []byte("921\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proc, "921"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The command holds a space and a parenthesis; starttime 12345 ticks.
	stat := "921 (dock) erd) S 1 921 921 0 -1 4194560 1 2 3 4 5 6 7 8 20 0 30 0 12345 100 200\n"
	if err := os.WriteFile(filepath.Join(proc, "921", "stat"), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proc, "uptime"), []byte("1000.50 4000.00\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 13, 2, 0, 0, 0, time.UTC)
	got, err := processStarted(proc, pidFile, now)
	if err != nil {
		t.Fatal(err)
	}
	want := now.Add(-1000500 * time.Millisecond).Add(123450 * time.Millisecond)
	if !got.Equal(want) {
		t.Errorf("started %v, want %v", got, want)
	}
	if _, err := processStarted(proc, filepath.Join(t.TempDir(), "none"), now); err == nil {
		t.Error("no pid file, no error")
	}
}

// On this machine, if Docker runs: the engine started before now.
func TestEngineStartedHere(t *testing.T) {
	got, err := processStarted("/proc", enginePIDFile, time.Now())
	if err != nil {
		t.Skipf("no dockerd here: %v", err)
	}
	if got.After(time.Now()) || got.Before(time.Now().Add(-365*24*time.Hour)) {
		t.Errorf("dockerd started %v", got)
	}
}
