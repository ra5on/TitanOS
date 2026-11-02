package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWaitForConfig(t *testing.T) {
	dir := t.TempDir()
	start := time.Now()
	// Readable at once, and a missing directory (new stick) is not waited for.
	waitForConfig(context.Background(), dir, time.Minute)
	waitForConfig(context.Background(), filepath.Join(dir, "missing"), time.Minute)
	if time.Since(start) > time.Second {
		t.Errorf("waited %v", time.Since(start))
	}
	// Not readable (a file in the way gives ENOTDIR): gives up after the patience.
	file := filepath.Join(dir, "file")
	os.WriteFile(file, nil, 0o644)
	start = time.Now()
	waitForConfig(context.Background(), filepath.Join(file, "config"), 100*time.Millisecond)
	if waited := time.Since(start); waited < 100*time.Millisecond || waited > 5*time.Second {
		t.Errorf("waited %v", waited)
	}
}
